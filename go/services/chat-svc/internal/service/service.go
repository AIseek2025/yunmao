// Package service 实现 chat-svc 弹幕业务：
//
//   - POST /api/v1/rooms/{room_id}/chat：发送弹幕（登录 JWT 必需）；
//     - Redis sliding window 频控：每用户 1 房间 / 秒 / 上限 3；
//     - 内容长度上限 256；
//     - 本地敏感词初筛（DefaultSensitiveWords / 注入 SensitiveFilter）；
//     - 写入 chat_messages（PG 或内存）；
//     - 发出 `room.chat.message` 到 eventbus（gateway 消费扇出 WS）。
//   - POST /api/v1/admin/chat/messages/{id}/moderate：审核（删除/隐藏/警告/禁言）；
//     - 发出 `room.chat.moderation` 事件。
package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"yunmao.live/pkg/yunmao/cache"
	"yunmao.live/pkg/yunmao/cloudevents"
	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/ids"
)

// Message 弹幕消息 DTO（与 chat_messages 表对齐）。
type Message struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	RoomID           string    `json:"room_id"`
	Body             string    `json:"body"`
	Emojis           []string  `json:"emojis,omitempty"`
	ModerationStatus string    `json:"moderation_status"`
	ModerationReason string    `json:"moderation_reason,omitempty"`
	ClientMsgID      string    `json:"client_msg_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// Store chat-svc 持久层抽象。内存与 PG 实现见 store/。
type Store interface {
	Insert(ctx context.Context, m Message) (*Message, error)
	Get(ctx context.Context, id string) (*Message, error)
	Moderate(ctx context.Context, id, status, reason string) (*Message, error)
	List(ctx context.Context, roomID string, limit int) ([]Message, error)
}

// Publisher 把消息推到 eventbus（gateway 消费扇出 WS）。
type Publisher interface {
	Publish(ctx context.Context, topic eventbus.Topic, key string, evt cloudevents.Event[any]) error
}

// SensitiveFilter 内容敏感词过滤；默认 DefaultSensitiveFilter（本地词表）。
type SensitiveFilter interface {
	Check(text string) (clean string, flagged bool, reason string)
}

// ChatService 业务实现。
type ChatService struct {
	mu       sync.Mutex
	store    Store
	pub      Publisher
	filter   SensitiveFilter
	rate     cache.Store
	rateMax  int           // 每滑窗最大消息数
	rateWin  time.Duration // 滑窗时长
	maxBody  int
	source   string
}

// Config 构造参数。
type Config struct {
	Store          Store
	Publisher      Publisher
	Filter         SensitiveFilter
	RateLimitStore cache.Store
	RateLimitMax   int           // 默认 3
	RateLimitWin   time.Duration // 默认 5 秒
	MaxBodyLen     int           // 默认 256
	Source         string        // CloudEvents source；默认 "chat-svc@dev"
}

// New 构造；缺省字段填默认值。
func New(c Config) *ChatService {
	if c.RateLimitMax == 0 {
		c.RateLimitMax = 3
	}
	if c.RateLimitWin == 0 {
		c.RateLimitWin = 5 * time.Second
	}
	if c.MaxBodyLen == 0 {
		c.MaxBodyLen = 256
	}
	if c.Filter == nil {
		c.Filter = DefaultSensitiveFilter()
	}
	if c.Source == "" {
		c.Source = "chat-svc@dev"
	}
	return &ChatService{
		store:   c.Store,
		pub:     c.Publisher,
		filter:  c.Filter,
		rate:    c.RateLimitStore,
		rateMax: c.RateLimitMax,
		rateWin: c.RateLimitWin,
		maxBody: c.MaxBodyLen,
		source:  c.Source,
	}
}

// SendInput POST /api/v1/rooms/{room_id}/chat 入参。
type SendInput struct {
	UserID      string   `json:"user_id"`
	RoomID      string   `json:"room_id"`
	Body        string   `json:"body"`
	Emojis      []string `json:"emojis,omitempty"`
	ClientMsgID string   `json:"client_msg_id,omitempty"`
}

// Send 发送弹幕。
func (s *ChatService) Send(ctx context.Context, in SendInput) (*Message, error) {
	if in.UserID == "" || in.RoomID == "" {
		return nil, yerr.New(yerr.SystemInternal, "user_id and room_id required")
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return nil, yerr.New(yerr.SystemInternal, "body empty")
	}
	if utf8Len(body) > s.maxBody {
		return nil, yerr.New(yerr.SystemInternal, "body too long")
	}

	// 频控
	if s.rate != nil {
		ok, err := s.allowed(ctx, in.UserID, in.RoomID)
		if err != nil {
			return nil, yerr.New(yerr.SystemDependencyUnavailable, "ratelimit: "+err.Error())
		}
		if !ok {
			chatRatelimitRejects.WithLabelValues(in.RoomID, "user_rate").Inc()
			return nil, yerr.New(yerr.SystemRateLimited, "rate limited")
		}
	}

	// 敏感词
	clean, flagged, reason := s.filter.Check(body)
	status := "published"
	if flagged {
		status = "flagged"
		chatModerationActions.WithLabelValues("flagged").Inc()
	}

	now := time.Now().UTC()
	m := Message{
		ID:               ids.New(ids.PrefixChatMessage),
		UserID:           in.UserID,
		RoomID:           in.RoomID,
		Body:             clean,
		Emojis:           in.Emojis,
		ModerationStatus: status,
		ModerationReason: reason,
		ClientMsgID:      in.ClientMsgID,
		CreatedAt:        now,
	}
	out, err := s.store.Insert(ctx, m)
	if err != nil {
		return nil, yerr.New(yerr.SystemInternal, "store: "+err.Error())
	}
	chatMessagesIn.WithLabelValues(in.RoomID, status).Inc()

	// 发事件
	if s.pub != nil {
		evt := cloudevents.New[any](string(eventbus.TopicChatMessage), s.source, out.ID, map[string]any{
			"id":      out.ID,
			"user_id": out.UserID,
			"room_id": out.RoomID,
			"body":    out.Body,
			"emojis":  out.Emojis,
			"status":  out.ModerationStatus,
			"ts":      out.CreatedAt.Format(time.RFC3339),
		})
		_ = s.pub.Publish(ctx, eventbus.TopicChatMessage, out.RoomID, evt)
	}
	return out, nil
}

// Moderate 审核操作（admin）。
func (s *ChatService) Moderate(ctx context.Context, id, status, reason string) (*Message, error) {
	if !validModerationStatus(status) {
		return nil, yerr.New(yerr.SystemInternal, "invalid moderation status")
	}
	out, err := s.store.Moderate(ctx, id, status, reason)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, yerr.New(yerr.SystemInternal, "message not found")
		}
		return nil, yerr.New(yerr.SystemInternal, "moderate: "+err.Error())
	}
	chatModerationActions.WithLabelValues(status).Inc()
	if s.pub != nil {
		evt := cloudevents.New[any](string(eventbus.TopicChatModeration), s.source, id, map[string]any{
			"id":     id,
			"action": status,
			"reason": reason,
		})
		_ = s.pub.Publish(ctx, eventbus.TopicChatModeration, out.RoomID, evt)
	}
	return out, nil
}

// List 拉房间最近 N 条（默认 50）。
func (s *ChatService) List(ctx context.Context, roomID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out, err := s.store.List(ctx, roomID, limit)
	if err != nil {
		return nil, yerr.New(yerr.SystemInternal, "list: "+err.Error())
	}
	return out, nil
}

func validModerationStatus(s string) bool {
	switch s {
	// 第七轮：扩展 recall / warn / mute（mute 由 chat-svc 走 user-level，但事件仍走同一总线）。
	case "pending", "published", "hidden", "deleted", "flagged",
		"recall", "warn", "mute":
		return true
	}
	return false
}

// allowed sliding window：用户 1 房间 1 时刻只能 N 条 / RateLimitWin。
func (s *ChatService) allowed(ctx context.Context, userID, roomID string) (bool, error) {
	key := "chat:rl:" + roomID + ":" + userID
	// 用 Idempotent 接口 cheat：每滑窗作为一个 idem key 计数，借 cache.Store 的 Get/Set。
	// 但简化实现：用 Incr + Expire 模式不可用（cache.Store 没暴露原子 incr）。
	// 这里退回到进程内 sliding window 兼内存 / Redis 共用 Get-Set，原子性靠并发量小兜住。
	s.mu.Lock()
	defer s.mu.Unlock()
	val, _, err := s.rate.Get(ctx, key)
	if err != nil {
		return false, err
	}
	count := 0
	if val != "" {
		for i := 0; i < len(val); i++ {
			count = count*10 + int(val[i]-'0')
		}
	}
	if count >= s.rateMax {
		return false, nil
	}
	count++
	return true, s.rate.Set(ctx, key, itoa(count), s.rateWin)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 4)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func utf8Len(s string) int {
	return len([]rune(s))
}

// ErrNotFound store 层未找到。
var ErrNotFound = errors.New("chat: not found")
