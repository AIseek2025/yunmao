// Package service 房间元数据 + 短期订阅 token 签发 + 流密钥轮换。
//
// 第四轮变化：
//
//   - 抽 Store 接口，落 PG（migration 0001 + 0003 + 0004 字段）；
//   - 补齐 Update / List / SetStatus / RotateStreamKey 端点；
//   - stream_key 使用 KeyProvider 派生 HMAC（生产可换 KMS sign）。
package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/featureflags"
	"yunmao.live/pkg/yunmao/ids"

	"yunmao.live/services/room-svc/internal/store"
	"yunmao.live/services/room-svc/internal/turn"
)

// Room 业务层视图（与 store.Room 字段对齐 + JSON tags）。
type Room struct {
	ID                  string    `json:"id"`
	CatID               string    `json:"cat_id"`
	DeviceID            string    `json:"device_id"`
	OwnerID             string    `json:"owner_id"`
	DisplayName         string    `json:"display_name"`
	Description         string    `json:"description,omitempty"`
	City                string    `json:"city"`
	RegionID            string    `json:"region_id"`
	Visibility          string    `json:"visibility"`
	LiveStatus          string    `json:"live_status"`
	FeedingStatus       string    `json:"feeding_status"`
	Status              string    `json:"status"`
	FeedCooldownSeconds uint32    `json:"feed_cooldown_seconds"`
	NoFeedWindowStart   string    `json:"no_feed_window_start,omitempty"`
	NoFeedWindowEnd     string    `json:"no_feed_window_end,omitempty"`
	StreamKey           string    `json:"stream_key,omitempty"`
	CatIDs              []string  `json:"cat_ids,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`

	// 第七轮：协议偏好（ll-hls | webrtc），从灰度策略 / 主播 override 解析。
	ProtocolPref     string `json:"protocol_pref,omitempty"`
	GrayHitWebRtc    bool   `json:"gray_hit_webrtc,omitempty"`
	GrayHitWebRtcPct int    `json:"gray_hit_webrtc_percent,omitempty"`
}

// Config 配置。
type Config struct {
	Signer    *authjwt.Signer
	Verifier  *authjwt.Verifier
	TokenTTL  time.Duration
	Audience  string
	Store     store.Store
	RegionID  string // 用于 stream_key 前缀（如 cn-east-1）
	StreamHMACSecret []byte // 用于 stream_key HMAC；空则用 ephemeral 随机
	// TURN 凭证签发；nil 时 GetIceServers 仅返回 STUN。
	TurnSigner   *turn.Signer
	TurnHosts    []string // e.g. ["turn.yunmao.live"]
	TurnPorts    []int    // e.g. [3478, 5349]
	StunUrls     []string // 默认 stun:stun.l.google.com:19302
	TurnTTL      time.Duration
	// Flags 灰度开关访问入口；为空时禁用灰度（默认 protocol_pref=ll-hls）。
	Flags *featureflags.Manager
	// ProtocolFlagName 默认 "room.webrtc.enabled"；可在测试中覆写。
	ProtocolFlagName string
}

// RoomService 业务。
type RoomService struct {
	store      store.Store
	signer     *authjwt.Signer
	ver        *authjwt.Verifier
	ttl        time.Duration
	aud        string
	regionID   string
	hmacKey    []byte
	turnSigner *turn.Signer
	turnHosts  []string
	turnPorts  []int
	stunUrls   []string
	turnTTL    time.Duration
	flags      *featureflags.Manager
	flagName   string
}

// New 构造。
func New(cfg Config) *RoomService {
	if cfg.TokenTTL == 0 {
		cfg.TokenTTL = 10 * time.Minute
	}
	if cfg.Audience == "" {
		cfg.Audience = "yunmao.gateway"
	}
	if cfg.RegionID == "" {
		cfg.RegionID = "global"
	}
	if cfg.Store == nil {
		cfg.Store = store.NewMemoryStore()
	}
	hmacKey := cfg.StreamHMACSecret
	if len(hmacKey) == 0 {
		hmacKey = make([]byte, 32)
		_, _ = rand.Read(hmacKey)
	}
	stunUrls := cfg.StunUrls
	if len(stunUrls) == 0 {
		stunUrls = []string{"stun:stun.l.google.com:19302"}
	}
	ttl := cfg.TurnTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	flagName := cfg.ProtocolFlagName
	if flagName == "" {
		flagName = "room.webrtc.enabled"
	}
	return &RoomService{
		store:      cfg.Store,
		signer:     cfg.Signer,
		ver:        cfg.Verifier,
		ttl:        cfg.TokenTTL,
		aud:        cfg.Audience,
		regionID:   cfg.RegionID,
		hmacKey:    hmacKey,
		turnSigner: cfg.TurnSigner,
		turnHosts:  cfg.TurnHosts,
		turnPorts:  cfg.TurnPorts,
		stunUrls:   stunUrls,
		turnTTL:    ttl,
		flags:      cfg.Flags,
		flagName:   flagName,
	}
}

// ResolveProtocolPref 计算房间协议偏好：
//
//   - 主播显式 override：room.ProtocolPref 已被赋值（e.g. "webrtc"）→ 直接返回；
//   - 否则 flag "room.webrtc.enabled" + gray_percent 命中 → "webrtc"；
//   - 否则 "ll-hls"。
//
// 同步把 GrayHitWebRtc / GrayHitWebRtcPct 写入 Room，便于客户端调试展示。
func (s *RoomService) ResolveProtocolPref(r *Room) {
	if r == nil {
		return
	}
	pct := 0
	if s.flags != nil {
		f := s.flags.Get(s.flagName)
		if v, ok := f.Value["gray_percent"]; ok {
			switch t := v.(type) {
			case float64:
				pct = int(t)
			case int:
				pct = t
			case int64:
				pct = int(t)
			}
		}
		r.GrayHitWebRtcPct = pct
		hit := s.flags.IsRoomInGrayPercent(s.flagName, r.ID)
		r.GrayHitWebRtc = hit
	}
	// override：如果 store 里已经返回非空 ProtocolPref，则尊重之；否则按 gray hit 推断。
	if r.ProtocolPref == "" {
		if r.GrayHitWebRtc {
			r.ProtocolPref = "webrtc"
		} else {
			r.ProtocolPref = "ll-hls"
		}
	}
}

// SimulateGrayDistribution 给 admin 调试用：返回 N 个 room_id 模拟样本的命中比例。
// 用于 admin-svc /admin/webrtc/gray-sim。
func (s *RoomService) SimulateGrayDistribution(n int) GrayDistribution {
	if n <= 0 {
		n = 1000
	}
	hits := 0
	for i := 0; i < n; i++ {
		rid := fmt.Sprintf("room_sim_%d", i)
		if s.flags != nil && s.flags.IsRoomInGrayPercent(s.flagName, rid) {
			hits++
		}
	}
	pct := 0.0
	if n > 0 {
		pct = float64(hits) / float64(n) * 100
	}
	cfgPct := 0
	if s.flags != nil {
		f := s.flags.Get(s.flagName)
		if v, ok := f.Value["gray_percent"]; ok {
			if vv, ok := v.(float64); ok {
				cfgPct = int(vv)
			}
		}
	}
	return GrayDistribution{
		Samples:            n,
		HitWebrtc:          hits,
		HitPct:             pct,
		ConfiguredGrayPct:  cfgPct,
		Flag:               s.flagName,
	}
}

// GrayDistribution admin 调试输出。
type GrayDistribution struct {
	Samples           int     `json:"samples"`
	HitWebrtc         int     `json:"hit_webrtc"`
	HitPct            float64 `json:"hit_pct"`
	ConfiguredGrayPct int     `json:"configured_gray_pct"`
	Flag              string  `json:"flag"`
}

// IssueIceServers 给客户端 / WHEP /whep/ice 路径用：返回 STUN + 可选短期 TURN credential。
// userID 不能为空（用于绑定 expiry）；TTL 走 service 配置的 turnTTL。
func (s *RoomService) IssueIceServers(userID string) (*IceServersResponse, error) {
	if userID == "" {
		return nil, yerr.New(yerr.SystemInternal, "user_id required")
	}
	resp := &IceServersResponse{
		IceServers: []IceServer{{Urls: s.stunUrls}},
	}
	if s.turnSigner == nil || len(s.turnHosts) == 0 {
		return resp, nil
	}
	cred, err := s.turnSigner.Issue(userID, s.turnTTL)
	if err != nil {
		return nil, yerr.New(yerr.SystemInternal, "turn issue: "+err.Error())
	}
	resp.IceServers = append(resp.IceServers, IceServer{
		Urls:       turn.ICEEndpoints(s.turnHosts, s.turnPorts),
		Username:   cred.Username,
		Credential: cred.Credential,
	})
	resp.Username = cred.Username
	resp.Credential = cred.Credential
	resp.TTLSeconds = cred.TTLSeconds
	resp.ExpiresAt = cred.ExpiresAt
	return resp, nil
}

// IceServersResponse 给 web client RTCConfiguration。
type IceServersResponse struct {
	IceServers []IceServer `json:"ice_servers"`
	// 下面三字段 = 主 TURN credential（也包含在 IceServers 里），方便客户端单独拉取。
	Username   string    `json:"username,omitempty"`
	Credential string    `json:"credential,omitempty"`
	TTLSeconds int       `json:"ttl_seconds,omitempty"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
}

// IceServer 一组 STUN/TURN URL。
type IceServer struct {
	Urls       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// Get 读单行。
func (s *RoomService) Get(ctx context.Context, id string) (*Room, error) {
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	out := toServiceRoom(r)
	s.ResolveProtocolPref(out)
	return out, nil
}

// List 列表，支持 owner_id/region_id/status 过滤。
func (s *RoomService) List(ctx context.Context, f ListFilter) ([]*Room, error) {
	rows, err := s.store.List(ctx, store.ListFilter{
		OwnerID:  f.OwnerID,
		RegionID: f.RegionID,
		Status:   f.Status,
		Limit:    f.Limit,
		Offset:   f.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*Room, 0, len(rows))
	for i := range rows {
		out = append(out, toServiceRoom(&rows[i]))
	}
	return out, nil
}

// ListFilter 列表过滤条件。
type ListFilter struct {
	OwnerID  string
	RegionID string
	Status   string
	Limit    int
	Offset   int
}

// Create 创建。
func (s *RoomService) Create(ctx context.Context, r Room) (*Room, error) {
	if r.DisplayName == "" {
		return nil, yerr.New(yerr.SystemInternal, "display_name required")
	}
	if r.ID == "" {
		r.ID = ids.New(ids.PrefixRoom)
	}
	if r.LiveStatus == "" {
		r.LiveStatus = "offline"
	}
	if r.FeedingStatus == "" {
		r.FeedingStatus = "closed"
	}
	if r.Status == "" {
		r.Status = "offline"
	}
	if r.FeedCooldownSeconds == 0 {
		r.FeedCooldownSeconds = 30
	}
	if r.Visibility == "" {
		r.Visibility = "public"
	}
	if r.RegionID == "" {
		r.RegionID = s.regionID
	}
	now := time.Now().UTC()
	r.CreatedAt = now
	r.UpdatedAt = now
	if r.StreamKey == "" {
		r.StreamKey = s.deriveStreamKey(r.ID, now)
	}
	if r.CatID != "" && len(r.CatIDs) == 0 {
		r.CatIDs = []string{r.CatID}
	}
	if err := s.store.Create(ctx, toStoreRoom(&r)); err != nil {
		return nil, err
	}
	return &r, nil
}

// Update 更新。
func (s *RoomService) Update(ctx context.Context, r Room) (*Room, error) {
	if r.ID == "" {
		return nil, yerr.New(yerr.SystemInternal, "id required")
	}
	existing, err := s.store.Get(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	// 合并：未传字段保持原值（只更新明显改变的字段）。
	merged := *existing
	if r.DisplayName != "" {
		merged.DisplayName = r.DisplayName
	}
	if r.Description != "" {
		merged.Description = r.Description
	}
	if r.City != "" {
		merged.City = r.City
	}
	if r.RegionID != "" {
		merged.RegionID = r.RegionID
	}
	if r.Visibility != "" {
		merged.Visibility = r.Visibility
	}
	if r.CatID != "" {
		merged.CatID = r.CatID
	}
	if r.DeviceID != "" {
		merged.DeviceID = r.DeviceID
	}
	if r.OwnerID != "" {
		merged.OwnerID = r.OwnerID
	}
	if r.FeedCooldownSeconds != 0 {
		merged.FeedCooldownSeconds = r.FeedCooldownSeconds
	}
	if len(r.CatIDs) > 0 {
		merged.CatIDs = r.CatIDs
	}
	if r.NoFeedWindowStart != "" {
		merged.NoFeedWindowStart = r.NoFeedWindowStart
	}
	if r.NoFeedWindowEnd != "" {
		merged.NoFeedWindowEnd = r.NoFeedWindowEnd
	}
	if err := s.store.Update(ctx, &merged); err != nil {
		return nil, err
	}
	return toServiceRoom(&merged), nil
}

// SetStatus 切换 status（live | offline | banned）。
func (s *RoomService) SetStatus(ctx context.Context, id, status string) error {
	switch status {
	case "live", "offline", "banned":
	default:
		return yerr.New(yerr.SystemInternal, "invalid status: "+status)
	}
	return s.store.SetStatus(ctx, id, status)
}

// RotateStreamKey 重新派生 stream_key，并返回新值。
func (s *RoomService) RotateStreamKey(ctx context.Context, id string) (string, error) {
	now := time.Now().UTC()
	sk := s.deriveStreamKey(id, now)
	if err := s.store.SetStreamKey(ctx, id, sk, now); err != nil {
		return "", err
	}
	return sk, nil
}

// deriveStreamKey 用 HMAC-SHA256 派生：region_id + room_id + timestamp。
// 生产建议把 hmacKey 替换为 KeyProvider 签名的 nonce（KMS sign）。
func (s *RoomService) deriveStreamKey(roomID string, now time.Time) string {
	mac := hmac.New(sha256.New, s.hmacKey)
	fmt.Fprintf(mac, "%s|%s|%d", s.regionID, roomID, now.UnixNano())
	sum := mac.Sum(nil)
	return s.regionID + "_" + hex.EncodeToString(sum[:16])
}

// SubscriptionRequest 申请房间订阅 token 的入参。
type SubscriptionRequest struct {
	RoomID    string
	UserToken string
}

// SubscriptionResponse 房间订阅响应。
type SubscriptionResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Room      *Room     `json:"room"`
}

// IssueSubscription 校验登录 JWT 后签发短期房间订阅 token。
func (s *RoomService) IssueSubscription(ctx context.Context, req SubscriptionRequest, allowGuest bool) (*SubscriptionResponse, error) {
	room, err := s.Get(ctx, req.RoomID)
	if err != nil {
		return nil, err
	}
	if room.Visibility == "private" && req.UserToken == "" {
		return nil, yerr.New(yerr.AuthLoginRequired, "private room requires login")
	}

	scope := authjwt.ScopeUser
	subject := ""
	if req.UserToken != "" {
		if s.ver == nil {
			return nil, errors.New("room-svc: verifier not configured")
		}
		cl, err := s.ver.Parse(req.UserToken)
		if err != nil {
			return nil, yerr.New(yerr.AuthTokenExpired, "user token invalid: "+err.Error())
		}
		subject = cl.Subject
		if cl.Scope != "" {
			scope = cl.Scope
		}
	} else if allowGuest {
		scope = authjwt.ScopeGuest
		subject = "guest"
	} else {
		return nil, yerr.New(yerr.AuthLoginRequired, "login required")
	}

	tok, err := s.signer.SignRoomSubscription(subject, scope, room.ID, s.aud, s.ttl)
	if err != nil {
		return nil, yerr.New(yerr.SystemInternal, "sign jwt: "+err.Error())
	}
	return &SubscriptionResponse{
		Token:     tok,
		ExpiresAt: time.Now().UTC().Add(s.ttl),
		Room:      room,
	}, nil
}

// JWKS 暴露给网关。
func (s *RoomService) JWKS() map[string]any {
	return s.signer.JWKS()
}

// KeysHealth /internal/keys/health。
func (s *RoomService) KeysHealth() map[string]any {
	out := map[string]any{
		"service": "room-svc",
		"now":     time.Now().UTC().Format(time.RFC3339),
	}
	kp := s.signer.KeyProvider()
	if h, ok := kp.(interface{ Health() map[string]any }); ok {
		for k, v := range h.Health() {
			out[k] = v
		}
		return out
	}
	if sk, err := kp.Active(); err == nil && sk != nil {
		out["active"] = sk.Kid
		out["alg"] = string(sk.Alg)
	}
	return out
}

// --- mapping helpers ---

func toStoreRoom(r *Room) *store.Room {
	return &store.Room{
		ID:                  r.ID,
		CatID:               r.CatID,
		DeviceID:            r.DeviceID,
		OwnerID:             r.OwnerID,
		DisplayName:         r.DisplayName,
		Description:         r.Description,
		City:                r.City,
		RegionID:            r.RegionID,
		Visibility:          r.Visibility,
		LiveStatus:          r.LiveStatus,
		FeedingStatus:       r.FeedingStatus,
		Status:              r.Status,
		FeedCooldownSeconds: r.FeedCooldownSeconds,
		NoFeedWindowStart:   r.NoFeedWindowStart,
		NoFeedWindowEnd:     r.NoFeedWindowEnd,
		StreamKey:           r.StreamKey,
		CatIDs:              r.CatIDs,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
	}
}

func toServiceRoom(r *store.Room) *Room {
	return &Room{
		ID:                  r.ID,
		CatID:               r.CatID,
		DeviceID:            r.DeviceID,
		OwnerID:             r.OwnerID,
		DisplayName:         r.DisplayName,
		Description:         r.Description,
		City:                r.City,
		RegionID:            r.RegionID,
		Visibility:          r.Visibility,
		LiveStatus:          r.LiveStatus,
		FeedingStatus:       r.FeedingStatus,
		Status:              r.Status,
		FeedCooldownSeconds: r.FeedCooldownSeconds,
		NoFeedWindowStart:   r.NoFeedWindowStart,
		NoFeedWindowEnd:     r.NoFeedWindowEnd,
		StreamKey:           r.StreamKey,
		CatIDs:              r.CatIDs,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
	}
}
