// Package service device-svc：设备绑定 + 在线状态聚合 + MQTT 凭证签发。
//
// 第四轮变化：
//
//   - 抽 DeviceStore 接口（参见 internal/store）；落 PG。
//   - 端点：Register / Update / BindRoom / UnbindRoom / IssueMqttCredential /
//     UpdateFirmware / List / SetStatus。
//   - MQTT 凭证：用 KeyProvider（HS256 短期 token）作为 password；后续可平滑切到 KMS。
package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"yunmao.live/pkg/yunmao/authjwt"
	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/ids"

	"yunmao.live/services/device-svc/internal/store"
)

// Device 业务层 DTO。
type Device struct {
	ID                  string                 `json:"id"`
	RoomID              string                 `json:"room_id"`
	OwnerID             string                 `json:"owner_id"`
	DeviceType          string                 `json:"device_type"`
	HardwareModel       string                 `json:"hardware_model"`
	FirmwareVersion     string                 `json:"firmware_version"`
	FirmwareTarget      string                 `json:"firmware_target,omitempty"`
	OnlineStatus        string                 `json:"online_status"`
	LastSeenAt          time.Time              `json:"last_seen_at"`
	RemainingFoodGrams  uint32                 `json:"remaining_food_grams"`
	HealthStatus        string                 `json:"health_status"`
	RegionID            string                 `json:"region_id"`
	MqttUsername        string                 `json:"mqtt_username,omitempty"`
	MqttExpiresAt       time.Time              `json:"mqtt_expires_at,omitempty"`
	Capability          map[string]any         `json:"capability,omitempty"`
	CreatedAt           time.Time              `json:"created_at"`
}

// Config 配置。
type Config struct {
	Store              store.Store
	KeyProvider        authjwt.KeyProvider
	MqttCredentialTTL  time.Duration
	MqttIssuer         string
	MqttAudience       string
	RegionID           string
}

// DeviceService 业务。
type DeviceService struct {
	store       store.Store
	kp          authjwt.KeyProvider
	credTTL     time.Duration
	issuer      string
	audience    string
	regionID    string
}

// New 构造。
func New(cfg Config) *DeviceService {
	if cfg.Store == nil {
		cfg.Store = store.NewMemoryStore()
	}
	if cfg.MqttCredentialTTL == 0 {
		cfg.MqttCredentialTTL = 12 * time.Hour
	}
	if cfg.MqttIssuer == "" {
		cfg.MqttIssuer = "yunmao.device-svc"
	}
	if cfg.MqttAudience == "" {
		cfg.MqttAudience = "yunmao.emqx"
	}
	if cfg.RegionID == "" {
		cfg.RegionID = "global"
	}
	return &DeviceService{
		store:    cfg.Store,
		kp:       cfg.KeyProvider,
		credTTL:  cfg.MqttCredentialTTL,
		issuer:   cfg.MqttIssuer,
		audience: cfg.MqttAudience,
		regionID: cfg.RegionID,
	}
}

// Get 读单行。
func (s *DeviceService) Get(ctx context.Context, id string) (*Device, error) {
	d, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return toServiceDevice(d), nil
}

// List 列出设备。
func (s *DeviceService) List(ctx context.Context, f ListFilter) ([]*Device, error) {
	rows, err := s.store.List(ctx, store.ListFilter{
		OwnerID:  f.OwnerID,
		RoomID:   f.RoomID,
		RegionID: f.RegionID,
		Status:   f.Status,
		Limit:    f.Limit,
		Offset:   f.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*Device, 0, len(rows))
	for i := range rows {
		out = append(out, toServiceDevice(&rows[i]))
	}
	return out, nil
}

// ListFilter 设备过滤。
type ListFilter struct {
	OwnerID  string
	RoomID   string
	RegionID string
	Status   string
	Limit    int
	Offset   int
}

// Register 注册新设备。
func (s *DeviceService) Register(ctx context.Context, d Device) (*Device, error) {
	if d.HardwareModel == "" {
		return nil, yerr.New(yerr.SystemInternal, "hardware_model required")
	}
	if d.ID == "" {
		d.ID = ids.New(ids.PrefixDevice)
	}
	if d.DeviceType == "" {
		d.DeviceType = "feeder"
	}
	if d.RegionID == "" {
		d.RegionID = s.regionID
	}
	if d.OnlineStatus == "" {
		d.OnlineStatus = "offline"
	}
	if d.HealthStatus == "" {
		d.HealthStatus = "unknown"
	}
	now := time.Now().UTC()
	d.CreatedAt = now
	d.LastSeenAt = now
	if err := s.store.Create(ctx, toStoreDevice(&d)); err != nil {
		return nil, err
	}
	return &d, nil
}

// Update 更新设备（合并）。
func (s *DeviceService) Update(ctx context.Context, d Device) (*Device, error) {
	if d.ID == "" {
		return nil, yerr.New(yerr.SystemInternal, "id required")
	}
	existing, err := s.store.Get(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	merged := *existing
	if d.RoomID != "" {
		merged.RoomID = d.RoomID
	}
	if d.OwnerID != "" {
		merged.OwnerID = d.OwnerID
	}
	if d.DeviceType != "" {
		merged.DeviceType = d.DeviceType
	}
	if d.HardwareModel != "" {
		merged.HardwareModel = d.HardwareModel
	}
	if d.FirmwareVersion != "" {
		merged.FirmwareVersion = d.FirmwareVersion
	}
	if d.FirmwareTarget != "" {
		merged.FirmwareTarget = d.FirmwareTarget
	}
	if d.OnlineStatus != "" {
		merged.OnlineStatus = d.OnlineStatus
	}
	if d.HealthStatus != "" {
		merged.HealthStatus = d.HealthStatus
	}
	if d.RegionID != "" {
		merged.RegionID = d.RegionID
	}
	if d.Capability != nil {
		merged.Capability = d.Capability
	}
	if err := s.store.Update(ctx, &merged); err != nil {
		return nil, err
	}
	return toServiceDevice(&merged), nil
}

// BindRoom 绑定房间。
func (s *DeviceService) BindRoom(ctx context.Context, deviceID, roomID string) error {
	if roomID == "" {
		return yerr.New(yerr.SystemInternal, "room_id required")
	}
	return s.store.BindRoom(ctx, deviceID, roomID)
}

// UnbindRoom 解绑房间。
func (s *DeviceService) UnbindRoom(ctx context.Context, deviceID string) error {
	return s.store.UnbindRoom(ctx, deviceID)
}

// SetStatus 切换在线状态。
func (s *DeviceService) SetStatus(ctx context.Context, id, status string) error {
	switch status {
	case "online", "offline", "error":
	default:
		return yerr.New(yerr.SystemInternal, "invalid status: "+status)
	}
	return s.store.SetStatus(ctx, id, status)
}

// MqttCredential 签发结果。
type MqttCredential struct {
	DeviceID  string    `json:"device_id"`
	Username  string    `json:"username"`
	Password  string    `json:"password"`
	ExpiresAt time.Time `json:"expires_at"`
	Algorithm string    `json:"algorithm"`
	KeyKid    string    `json:"key_kid"`
}

// IssueMqttCredential 用 KeyProvider 签发短期 MQTT password（JWT）。
// 设备拿到后用 device_id 作为 MQTT username + token 作为 password 连 EMQX；
// EMQX JWT 插件按 jwks_url 校验。MockKms / Vault / AWS KMS 都通过 KeyProvider 抽象支持。
func (s *DeviceService) IssueMqttCredential(ctx context.Context, deviceID string) (*MqttCredential, error) {
	if s.kp == nil {
		return nil, yerr.New(yerr.SystemDependencyUnavailable, "key provider not configured")
	}
	if _, err := s.store.Get(ctx, deviceID); err != nil {
		return nil, err
	}
	sk, err := s.kp.Active()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	exp := now.Add(s.credTTL)
	username := "device:" + deviceID
	claims := jwt.MapClaims{
		"sub":   deviceID,
		"iss":   s.issuer,
		"aud":   s.audience,
		"iat":   now.Unix(),
		"exp":   exp.Unix(),
		"scope": "device",
		"jti":   randomJTI(),
	}
	var method jwt.SigningMethod
	switch sk.Alg {
	case authjwt.AlgHS256:
		// ADR-0019：HS256 已下线；device-svc 不应再签发 HS256 MQTT 凭证。
		return nil, fmt.Errorf("device-svc: %w", authjwt.ErrHS256Removed)
	case authjwt.AlgRS256:
		if _, isRemote := sk.Material.(authjwt.RemoteSigner); isRemote {
			method = authjwt.RemoteRS256SigningMethod()
		} else {
			method = jwt.SigningMethodRS256
		}
	default:
		return nil, fmt.Errorf("device-svc: unsupported alg %s", sk.Alg)
	}
	tok := jwt.NewWithClaims(method, claims)
	tok.Header["kid"] = sk.Kid
	password, err := tok.SignedString(sk.Material)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetMqttCredential(ctx, deviceID, username, password, exp); err != nil {
		return nil, err
	}
	return &MqttCredential{
		DeviceID:  deviceID,
		Username:  username,
		Password:  password,
		ExpiresAt: exp,
		Algorithm: string(sk.Alg),
		KeyKid:    sk.Kid,
	}, nil
}

// UpdateFirmware 设置目标固件版本（OTA 标记）。
func (s *DeviceService) UpdateFirmware(ctx context.Context, deviceID, target string) error {
	if target == "" {
		return yerr.New(yerr.SystemInternal, "target required")
	}
	return s.store.SetFirmwareTarget(ctx, deviceID, target)
}

// MarkOnline 在 device-edge 心跳上报时调用。
func (s *DeviceService) MarkOnline(ctx context.Context, id string, remainingGrams uint32) {
	_ = s.store.MarkOnline(ctx, id, remainingGrams, time.Now().UTC())
}

// HeartbeatAt 在 MQTT bridge 收到 heartbeat 事件时调用。
func (s *DeviceService) HeartbeatAt(ctx context.Context, id string, at time.Time) {
	_ = s.store.MarkOnline(ctx, id, 0, at)
}

// JWKS 暴露 device-svc 的 MQTT 凭证签名公钥（EMQX 拉取校验）。
func (s *DeviceService) JWKS() map[string]any {
	if s.kp == nil {
		return map[string]any{"keys": []any{}}
	}
	return s.kp.PublicJWKS()
}

// KeysHealth /internal/keys/health 内容。
func (s *DeviceService) KeysHealth() map[string]any {
	out := map[string]any{
		"service":   "device-svc",
		"now":       time.Now().UTC().Format(time.RFC3339),
		"cred_ttl":  s.credTTL.String(),
		"issuer":    s.issuer,
		"audience":  s.audience,
	}
	if s.kp == nil {
		out["loaded"] = false
		return out
	}
	if h, ok := s.kp.(interface{ Health() map[string]any }); ok {
		for k, v := range h.Health() {
			out[k] = v
		}
		return out
	}
	if sk, err := s.kp.Active(); err == nil && sk != nil {
		out["active"] = sk.Kid
		out["alg"] = string(sk.Alg)
	}
	return out
}

// --- mapping ---

func toServiceDevice(d *store.Device) *Device {
	return &Device{
		ID:                 d.ID,
		RoomID:             d.RoomID,
		OwnerID:            d.OwnerID,
		DeviceType:         d.DeviceType,
		HardwareModel:      d.HardwareModel,
		FirmwareVersion:    d.FirmwareVersion,
		FirmwareTarget:     d.FirmwareTarget,
		OnlineStatus:       d.OnlineStatus,
		LastSeenAt:         d.LastSeenAt,
		RemainingFoodGrams: d.RemainingFoodGrams,
		HealthStatus:       d.HealthStatus,
		RegionID:           d.RegionID,
		MqttUsername:       d.MqttUsername,
		MqttExpiresAt:      d.MqttCredentialExpiresAt,
		Capability:         d.Capability,
		CreatedAt:          d.CreatedAt,
	}
}

func toStoreDevice(d *Device) *store.Device {
	return &store.Device{
		ID:                      d.ID,
		RoomID:                  d.RoomID,
		OwnerID:                 d.OwnerID,
		DeviceType:              d.DeviceType,
		HardwareModel:           d.HardwareModel,
		FirmwareVersion:         d.FirmwareVersion,
		FirmwareTarget:          d.FirmwareTarget,
		OnlineStatus:            d.OnlineStatus,
		LastSeenAt:              d.LastSeenAt,
		RemainingFoodGrams:      d.RemainingFoodGrams,
		HealthStatus:            d.HealthStatus,
		RegionID:                d.RegionID,
		MqttUsername:            d.MqttUsername,
		MqttCredentialExpiresAt: d.MqttExpiresAt,
		Capability:              d.Capability,
		CreatedAt:               d.CreatedAt,
	}
}

func randomJTI() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ErrMissing 端点参数缺失。
var ErrMissing = errors.New("missing required field")
