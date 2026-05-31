// Package service 实现 feeding-svc 的核心业务：
//
//   - 投喂请求的资格校验（房间、设备、冷却、动物福利）
//   - 状态机驱动：每次状态变更同步 EmitEvent（事件溯源）
//   - 通过 publisher.EventPublisher 把指令转发给 device-edge（Kafka / HTTP / memory）
//   - 接收 ack 并推进状态
//
// 第三轮在原有基础上引入 **outbox mode**：当 [`SetOutboxMode`] 启用时，本服务不再直接
// 调用 publisher 投递 Kafka，而是把每次状态变更（含事件 payload）写入 `feeding_request_events`
// + `outbox`，由 `db.Relay` worker 异步推送到 Kafka。这是 ADR-0010 的标准事务性 outbox 模型。
package service

import (
	"context"
	"sync"
	"time"

	"yunmao.live/pkg/yunmao/cache"
	"yunmao.live/pkg/yunmao/cloudevents"
	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/featureflags"
	"yunmao.live/pkg/yunmao/feedingsafety"
	"yunmao.live/pkg/yunmao/feedstate"
	"yunmao.live/pkg/yunmao/ids"

	"yunmao.live/services/feeding-svc/publisher"
)

// Room 内存版房间元数据。生产应替换为 room-svc gRPC 查询 / Postgres 读。
type Room struct {
	ID           string
	CatID        string
	DeviceID     string
	FeedingOpen  bool
	NoFeedWindow bool
}

// Request 投喂请求快照。
type Request struct {
	ID              string          `json:"feed_request_id"`
	UserID          string          `json:"user_id"`
	RoomID          string          `json:"room_id"`
	CatID           string          `json:"cat_id"`
	DeviceID        string          `json:"device_id"`
	DeviceCommandID string          `json:"device_command_id,omitempty"`
	AmountGrams     uint32          `json:"amount_grams"`
	Status          feedstate.State `json:"status"`
	IdempotencyKey  string          `json:"idempotency_key"`
	RejectReason    string          `json:"reject_reason,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// FeedingService 投喂业务。
type FeedingService struct {
	mu             sync.RWMutex
	requests       map[string]*Request
	idem           *cache.Idempotent
	cooldownStore  cache.Store
	safetyManager  *feedingsafety.Manager
	flags          *featureflags.Manager
	rooms          map[string]Room
	publisher      publisher.EventPublisher
	source         string
	eventListeners []EventListener
	billing        BillingHook
	receipts       map[string]string // feed_request_id → billing receipt_id
	// outboxMode 为 true 表示状态变更走 outbox（不直接调用 publisher）。
	// 由 SetOutboxMode 设置。
	outboxMode bool

	// region region 标签，用于 region 级限流（feature_flags 中读取 per_region 数）。
	region string
	// regionQpsBucket 简单计数器（按当前秒分桶）：
	//   key=YYYYmmddHHMMSS, value=count
	regionQpsBucket map[int64]int
}

// EventListener 在每次状态变更时被同步调用；用于追加 feeding_request_events 行 / outbox 行。
type EventListener func(ctx context.Context, ev StateChangeEvent)

// StateChangeEvent 状态变更通知。
type StateChangeEvent struct {
	FeedRequestID string
	From          feedstate.State
	To            feedstate.State
	Reason        string
	Snapshot      Request
}

// New 构造服务；store 用于幂等 + 冷却（memory 或 redis）；safety 用于热加载阈值。
func New(p publisher.EventPublisher, store cache.Store, safety *feedingsafety.Manager) *FeedingService {
	svc := &FeedingService{
		requests:        make(map[string]*Request),
		idem:            cache.NewIdempotent(store, 48*time.Hour),
		cooldownStore:   store,
		safetyManager:   safety,
		rooms:           make(map[string]Room),
		publisher:       p,
		source:          "feeding-svc@dev",
		region:          "global",
		regionQpsBucket: map[int64]int{},
		billing:         NoopBilling{},
		receipts:        map[string]string{},
	}
	svc.rooms["room_demo"] = Room{
		ID:          "room_demo",
		CatID:       "cat_demo",
		DeviceID:    "dev_demo",
		FeedingOpen: true,
	}
	return svc
}

// SetFlags 注入 featureflags manager（dev 默认 nil，行为 = 全开放）。
func (s *FeedingService) SetFlags(m *featureflags.Manager) { s.flags = m }

// SetRegion 设置当前 region 标签（用于灰度规则）。
func (s *FeedingService) SetRegion(r string) {
	if r != "" {
		s.region = r
	}
}

// AddEventListener 注册状态变更回调。
func (s *FeedingService) AddEventListener(l EventListener) {
	s.eventListeners = append(s.eventListeners, l)
}

// SetOutboxMode 启用 / 关闭 outbox 模式。
// 启用后 Create / HandleAck 同步推进状态机，所有 Kafka 发布通过 outbox 行 + relay 完成。
func (s *FeedingService) SetOutboxMode(on bool) {
	s.outboxMode = on
}

// IsOutboxMode 返回当前是否启用 outbox。
func (s *FeedingService) IsOutboxMode() bool { return s.outboxMode }

// SetSource 覆盖事件 source（默认 "feeding-svc@dev"）。
func (s *FeedingService) SetSource(src string) {
	if src != "" {
		s.source = src
	}
}

// RegisterRoom 在内存中注册房间。
func (s *FeedingService) RegisterRoom(r Room) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rooms[r.ID] = r
}

// CreateInput POST /api/v1/feed-requests 入参。
type CreateInput struct {
	UserID         string `json:"user_id"`
	RoomID         string `json:"room_id"`
	AmountGrams    uint32 `json:"amount_grams"`
	IdempotencyKey string `json:"idempotency_key"`
}

// Create 创建投喂请求；状态机：created → accepted → queued → dispatched...
func (s *FeedingService) Create(ctx context.Context, in CreateInput) (*Request, error) {
	if in.UserID == "" || in.RoomID == "" || in.AmountGrams == 0 || in.IdempotencyKey == "" {
		return nil, yerr.New(yerr.SystemInternal, "missing required field")
	}

	// 灰度开关检查（feature_flags 表，10s 缓存）
	if s.flags != nil {
		if !s.flags.Bool("feeding.allow_new_rooms", true) {
			feedRequestsTotal.WithLabelValues("flag_blocked").Inc()
			return nil, yerr.New(yerr.RiskActionBlocked, "feeding currently disabled by feature flag")
		}
		// 维护模式：检查设备是否在 maintenance 列表
		s.mu.RLock()
		room, ok := s.rooms[in.RoomID]
		s.mu.RUnlock()
		if ok && room.DeviceID != "" {
			maintenance := s.flags.StringSlice("feeding.device_maintenance", "device_ids")
			for _, did := range maintenance {
				if did == room.DeviceID {
					feedRequestsTotal.WithLabelValues("device_maintenance").Inc()
					return nil, yerr.New(yerr.FeedDeviceOffline, "device under maintenance")
				}
			}
		}
		// region QPS 限流（简化版按秒分桶）
		if s.flags.Bool("feeding.region_qps_limit", true) {
			limit := s.flags.Int("feeding.region_qps_limit", "per_region", 200)
			if limit > 0 && !s.allowRegionQps(limit) {
				feedRequestsTotal.WithLabelValues("rate_limited").Inc()
				return nil, yerr.New(yerr.SystemRateLimited, "region qps limit reached")
			}
		}
	}

	first, err := s.idem.Insert(ctx, "feed", in.IdempotencyKey)
	if err != nil {
		return nil, yerr.New(yerr.SystemDependencyUnavailable, "idem check: "+err.Error())
	}
	if !first {
		s.mu.RLock()
		for _, r := range s.requests {
			if r.IdempotencyKey == in.IdempotencyKey {
				snap := *r
				s.mu.RUnlock()
				return &snap, nil
			}
		}
		s.mu.RUnlock()
		return nil, yerr.New(yerr.FeedDuplicateRequest, "duplicate request without record")
	}

	s.mu.RLock()
	room, ok := s.rooms[in.RoomID]
	s.mu.RUnlock()
	if !ok {
		return nil, yerr.New(yerr.RoomNotFound, "room not found")
	}
	if !room.FeedingOpen {
		return nil, yerr.New(yerr.FeedNoFeedWindow, "feeding closed")
	}
	if room.NoFeedWindow {
		return nil, yerr.New(yerr.FeedNoFeedWindow, "no-feed window")
	}

	limits, err := s.safetyManager.Resolve(ctx, in.RoomID)
	if err != nil {
		return nil, yerr.New(yerr.SystemDependencyUnavailable, "safety: "+err.Error())
	}
	cd := cache.NewCooldown(s.cooldownStore, limits.RoomCooldown, limits.UserRoomCooldown, limits.CatDailyLimit)

	out, _, err := cd.Check(ctx, in.RoomID, in.UserID, room.CatID, in.AmountGrams)
	if err != nil {
		return nil, yerr.New(yerr.SystemDependencyUnavailable, "cooldown check: "+err.Error())
	}
	switch out {
	case cache.BlockedRoom:
		feedRequestsTotal.WithLabelValues("blocked_room").Inc()
		feedCooldownBlockedTotal.WithLabelValues("room").Inc()
		return nil, yerr.New(yerr.FeedCooldownNotFinished, "cooldown not finished")
	case cache.BlockedUser:
		feedRequestsTotal.WithLabelValues("blocked_user").Inc()
		feedCooldownBlockedTotal.WithLabelValues("user").Inc()
		return nil, yerr.New(yerr.FeedCooldownNotFinished, "cooldown not finished")
	case cache.BlockedDaily:
		feedRequestsTotal.WithLabelValues("blocked_daily").Inc()
		feedCooldownBlockedTotal.WithLabelValues("daily").Inc()
		return nil, yerr.New(yerr.FeedHealthLimitHit, "daily limit reached")
	}

	c, err := cd.Consume(ctx, in.RoomID, in.UserID, room.CatID, in.AmountGrams)
	if err != nil {
		return nil, yerr.New(yerr.SystemDependencyUnavailable, "cooldown consume: "+err.Error())
	}
	if c != cache.OK {
		return nil, yerr.New(yerr.FeedCooldownNotFinished, "lost race on cooldown")
	}

	// billing saga 预留：在状态机 created → accepted 前预留额度（NoopBilling 永远成功）。
	receiptID, err := s.billing.Reserve(ctx, BillingReserveInput{
		UserID:         in.UserID,
		RoomID:         in.RoomID,
		CatID:          room.CatID,
		AmountGrams:    in.AmountGrams,
		IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		feedRequestsTotal.WithLabelValues("billing_rejected").Inc()
		return nil, yerr.New(yerr.PayChannelFailed, "billing reserve: "+err.Error())
	}

	now := time.Now().UTC()
	req := &Request{
		ID:              ids.New(ids.PrefixFeed),
		UserID:          in.UserID,
		RoomID:          in.RoomID,
		CatID:           room.CatID,
		DeviceID:        room.DeviceID,
		DeviceCommandID: ids.New(ids.PrefixCmd),
		AmountGrams:     in.AmountGrams,
		Status:          feedstate.Created,
		IdempotencyKey:  in.IdempotencyKey,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	s.mu.Lock()
	s.receipts[req.ID] = receiptID
	s.mu.Unlock()
	s.transition(ctx, req, feedstate.Accepted, "validated")

	s.mu.Lock()
	s.requests[req.ID] = req
	s.mu.Unlock()
	feedRequestsTotal.WithLabelValues("accepted").Inc()

	cmd := publisher.FeedCommandRequested{
		FeedRequestID:   req.ID,
		DeviceCommandID: req.DeviceCommandID,
		DeviceID:        req.DeviceID,
		RoomID:          req.RoomID,
		AmountGrams:     req.AmountGrams,
		MotorDurationMs: 1200,
		ExpiresAt:       now.Add(30 * time.Second).Format(time.RFC3339),
	}

	s.transition(ctx, req, feedstate.Queued, "queued to device-edge")

	if s.outboxMode {
		// outbox 模式：状态机同步推进到 dispatched；listener 已经在 transition 内写入
		// feeding_request_events + outbox 行（feed.command.requested / dispatched）。
		// relay worker 异步把 outbox 投递到 Kafka，不在请求路径阻塞。
		s.transition(ctx, req, feedstate.Dispatched, "dispatched via outbox")
	} else {
		// 内存 PoC 路径：异步走 publisher，与上一轮一致。
		go func() {
			if err := s.publisher.PublishFeedCommandRequested(context.Background(), cmd); err != nil {
				s.mu.Lock()
				s.transitionLocked(context.Background(), req, feedstate.Failed, "publish failed: "+err.Error())
				s.mu.Unlock()
				return
			}
			s.mu.Lock()
			s.transitionLocked(context.Background(), req, feedstate.Dispatched, "dispatched")
			s.mu.Unlock()
			_ = s.publisher.PublishGatewayEvent(context.Background(), "feed.command.dispatched", req.RoomID,
				map[string]any{
					"feed_request_id":   req.ID,
					"device_command_id": req.DeviceCommandID,
					"device_id":         req.DeviceID,
					"amount_grams":      req.AmountGrams,
				})
		}()
	}

	snapshot := *req
	return &snapshot, nil
}

// transition 必须在不持有 mu 的情形下调用。
func (s *FeedingService) transition(ctx context.Context, r *Request, to feedstate.State, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transitionLocked(ctx, r, to, reason)
}

// transitionLocked 调用方持有 s.mu。
func (s *FeedingService) transitionLocked(ctx context.Context, r *Request, to feedstate.State, reason string) {
	from := r.Status
	next, err := feedstate.Transition(from, to)
	if err != nil {
		return
	}
	r.Status = next
	r.UpdatedAt = time.Now().UTC()
	feedStateTransitionsTotal.WithLabelValues(string(next)).Inc()
	snapshot := *r
	for _, l := range s.eventListeners {
		l(ctx, StateChangeEvent{
			FeedRequestID: r.ID,
			From:          from,
			To:            next,
			Reason:        reason,
			Snapshot:      snapshot,
		})
	}
}

// Get 查询投喂请求。
func (s *FeedingService) Get(_ context.Context, id string) (*Request, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.requests[id]
	if !ok {
		return nil, yerr.New(yerr.SystemInternal, "feed request not found")
	}
	rcopy := *r
	return &rcopy, nil
}

// HandleAck 处理 ack（来自 device-edge HTTP 回调或 Kafka 消费）。
func (s *FeedingService) HandleAck(ctx context.Context, ack publisher.FeedCommandAcked) error {
	first, err := s.idem.Insert(ctx, "ack", ack.DeviceCommandID)
	if err != nil {
		return err
	}
	if !first {
		return nil
	}

	s.mu.Lock()
	r, ok := s.requests[ack.FeedRequestID]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if r.Status == feedstate.Dispatched {
		s.transitionLocked(ctx, r, feedstate.Acknowledged, "device ack received")
	}
	target := feedstate.Failed
	if ack.Status == "succeeded" {
		target = feedstate.Succeeded
	}
	if r.Status == feedstate.Acknowledged {
		s.transitionLocked(ctx, r, target, "ack: "+ack.Status)
	}
	roomID := r.RoomID
	receiptID := s.receipts[r.ID]
	delete(s.receipts, r.ID)
	finalStatus := r.Status
	s.mu.Unlock()

	// billing saga confirm / cancel：根据终态执行。NoopBilling 直接返回。
	if receiptID != "" {
		if finalStatus == feedstate.Succeeded {
			_ = s.billing.Confirm(ctx, receiptID)
		} else if finalStatus == feedstate.Failed {
			_ = s.billing.Cancel(ctx, receiptID)
		}
	}

	if s.outboxMode {
		// outbox 模式：feed.command.acked / completed 已经通过 listener 写入 outbox；
		// 不再直接调用 publisher。
		return nil
	}
	return s.publisher.PublishGatewayEvent(ctx, "feed.command.acked", roomID, ack)
}

// Cancel 取消投喂；仅 queued/dispatched 可取消，写 reject_reason="cancelled"。
// 状态机：queued/dispatched → rejected；触发 feed.command.cancelled 事件，由 device-svc 下发取消。
func (s *FeedingService) Cancel(ctx context.Context, feedRequestID, reason string) (*Request, error) {
	if feedRequestID == "" {
		return nil, yerr.New(yerr.SystemInternal, "feed_request_id required")
	}
	s.mu.Lock()
	r, ok := s.requests[feedRequestID]
	if !ok {
		s.mu.Unlock()
		return nil, yerr.New(yerr.SystemInternal, "feed request not found")
	}
	if r.Status != feedstate.Queued && r.Status != feedstate.Dispatched {
		s.mu.Unlock()
		return nil, yerr.New(yerr.FeedDuplicateRequest, "not cancellable in current state: "+string(r.Status))
	}
	if reason == "" {
		reason = "cancelled"
	}
	r.RejectReason = reason
	s.transitionLocked(ctx, r, feedstate.Rejected, reason)
	snap := *r
	receiptID := s.receipts[r.ID]
	delete(s.receipts, r.ID)
	s.mu.Unlock()

	feedRequestsTotal.WithLabelValues("cancelled").Inc()

	// billing cancel 补偿。
	if receiptID != "" {
		_ = s.billing.Cancel(ctx, receiptID)
	}

	// 非 outbox 模式下：直接通过 publisher 通知 device-svc / gateway。
	if !s.outboxMode {
		_ = s.publisher.PublishGatewayEvent(ctx, "feed.command.cancelled", snap.RoomID, map[string]any{
			"feed_request_id":   snap.ID,
			"device_command_id": snap.DeviceCommandID,
			"device_id":         snap.DeviceID,
			"reason":            reason,
		})
	}
	return &snap, nil
}

// TimeoutScanRun 扫描进程内 dispatched/queued 状态超过 ttl 的请求，标记为 failed(timeout)。
// 真实 PG 模式下另有 worker（参见 cmd/feeding-svc/main.go）。
// 返回被扫描出的过期请求数。
func (s *FeedingService) TimeoutScanRun(ctx context.Context, ttl time.Duration) int {
	if ttl <= 0 {
		return 0
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	hits := 0
	for _, r := range s.requests {
		if r.Status != feedstate.Dispatched && r.Status != feedstate.Queued {
			continue
		}
		if now.Sub(r.UpdatedAt) < ttl {
			continue
		}
		r.RejectReason = "timeout"
		s.transitionLocked(ctx, r, feedstate.Failed, "timeout")
		hits++
		feedRequestsTotal.WithLabelValues("timeout").Inc()
		if rcpt := s.receipts[r.ID]; rcpt != "" {
			delete(s.receipts, r.ID)
			_ = s.billing.Cancel(ctx, rcpt)
		}
	}
	return hits
}

// StartTimeoutWorker 启动一个 goroutine，按 interval 周期扫描超时。ctx 取消即退出。
func (s *FeedingService) StartTimeoutWorker(ctx context.Context, interval time.Duration, getTTL func() time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				ttl := 30 * time.Second
				if getTTL != nil {
					if v := getTTL(); v > 0 {
						ttl = v
					}
				}
				s.TimeoutScanRun(ctx, ttl)
			}
		}
	}()
}

// allowRegionQps 简单按秒分桶（dev 默认）。生产请改 Redis token bucket。
func (s *FeedingService) allowRegionQps(perSecond int) bool {
	now := time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	// 清理 5s 前的桶
	for k := range s.regionQpsBucket {
		if k < now-5 {
			delete(s.regionQpsBucket, k)
		}
	}
	c := s.regionQpsBucket[now]
	if c >= perSecond {
		return false
	}
	s.regionQpsBucket[now] = c + 1
	return true
}

// SnapshotEvent 把请求包装成 CloudEvents（调试 / 审计）。
func (r *Request) SnapshotEvent(source string) cloudevents.Event[*Request] {
	return cloudevents.New[*Request]("feed.request.snapshot", source, r.ID, r)
}
