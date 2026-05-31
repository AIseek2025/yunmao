// Package store 实现 device-svc 的持久化层。
//
// 提供两种实现：
//
//   - [`MemoryStore`]：进程内 map；默认（YUNMAO_DB_URL 为空）。
//   - [`PgStore`]：基于 pgxpool；落到 `devices` 表（migrations/0001+0003+0004）。
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	yerr "yunmao.live/pkg/yunmao/errors"
)

// Device 持久化层 DTO（与 migrations/0001 + 0003 + 0004 字段对齐）。
type Device struct {
	ID                       string
	RoomID                   string
	OwnerID                  string
	DeviceType               string
	HardwareModel            string
	FirmwareVersion          string
	FirmwareTarget           string
	CertificateID            string
	OnlineStatus             string  // online | offline | error
	LastSeenAt               time.Time
	RemainingFoodGrams       uint32
	HealthStatus             string
	MqttUsername             string
	MqttCredentialHash       string
	MqttCredentialExpiresAt  time.Time
	Capability               map[string]any
	RegionID                 string
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// ListFilter 列表过滤条件。
type ListFilter struct {
	OwnerID  string
	RoomID   string
	RegionID string
	Status   string
	Limit    int
	Offset   int
}

// Store 持久化接口。
type Store interface {
	Get(ctx context.Context, id string) (*Device, error)
	Create(ctx context.Context, d *Device) error
	Update(ctx context.Context, d *Device) error
	List(ctx context.Context, f ListFilter) ([]Device, error)
	SetStatus(ctx context.Context, id, status string) error
	MarkOnline(ctx context.Context, id string, remainingGrams uint32, at time.Time) error
	BindRoom(ctx context.Context, deviceID, roomID string) error
	UnbindRoom(ctx context.Context, deviceID string) error
	SetMqttCredential(ctx context.Context, deviceID, username, plaintextPassword string, expiresAt time.Time) error
	SetFirmwareTarget(ctx context.Context, deviceID, target string) error
}

// ErrNotFound 未找到。
var ErrNotFound = errors.New("device store: not found")

// HashPassword 用 SHA-256 + hex 派生 mqtt_credential_hash；EMQX ACL 校验时按此散列匹配。
func HashPassword(username, password string) string {
	if password == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(username + ":" + password))
	return hex.EncodeToString(sum[:])
}

// --------- MemoryStore ---------

// NewMemoryStore 构造内存实现，预置 dev_demo 行供 PoC。
func NewMemoryStore() Store {
	now := time.Now().UTC()
	m := &memoryStore{m: map[string]*Device{}}
	m.m["dev_demo"] = &Device{
		ID: "dev_demo", RoomID: "room_demo",
		OwnerID:            "usr_demo",
		DeviceType:         "feeder",
		HardwareModel:      "demo-feeder-v1",
		FirmwareVersion:    "1.0.0",
		OnlineStatus:       "online",
		LastSeenAt:         now,
		RemainingFoodGrams: 1000,
		HealthStatus:       "ok",
		RegionID:           "global",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	return m
}

type memoryStore struct {
	mu sync.RWMutex
	m  map[string]*Device
}

func (s *memoryStore) Get(_ context.Context, id string) (*Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.m[id]
	if !ok {
		return nil, yerr.New(yerr.DeviceUnbound, "device not found")
	}
	c := *d
	return &c, nil
}

func (s *memoryStore) Create(_ context.Context, d *Device) error {
	if d == nil || d.ID == "" {
		return errors.New("device.id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	d.UpdatedAt = time.Now().UTC()
	c := *d
	s.m[d.ID] = &c
	return nil
}

func (s *memoryStore) Update(_ context.Context, d *Device) error {
	if d == nil || d.ID == "" {
		return errors.New("device.id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.m[d.ID]
	if !ok {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	d.CreatedAt = existing.CreatedAt
	d.UpdatedAt = time.Now().UTC()
	c := *d
	s.m[d.ID] = &c
	return nil
}

func (s *memoryStore) List(_ context.Context, f ListFilter) ([]Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Device, 0, len(s.m))
	for _, d := range s.m {
		if f.OwnerID != "" && d.OwnerID != f.OwnerID {
			continue
		}
		if f.RoomID != "" && d.RoomID != f.RoomID {
			continue
		}
		if f.RegionID != "" && d.RegionID != f.RegionID {
			continue
		}
		if f.Status != "" && d.OnlineStatus != f.Status {
			continue
		}
		out = append(out, *d)
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (s *memoryStore) SetStatus(_ context.Context, id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[id]
	if !ok {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	d.OnlineStatus = status
	d.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) MarkOnline(_ context.Context, id string, remainingGrams uint32, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[id]
	if !ok {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	d.OnlineStatus = "online"
	d.LastSeenAt = at
	if remainingGrams > 0 {
		d.RemainingFoodGrams = remainingGrams
	}
	d.UpdatedAt = at
	return nil
}

func (s *memoryStore) BindRoom(_ context.Context, deviceID, roomID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[deviceID]
	if !ok {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	d.RoomID = roomID
	d.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) UnbindRoom(_ context.Context, deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[deviceID]
	if !ok {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	d.RoomID = ""
	d.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) SetMqttCredential(_ context.Context, deviceID, username, plaintextPassword string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[deviceID]
	if !ok {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	d.MqttUsername = username
	d.MqttCredentialHash = HashPassword(username, plaintextPassword)
	d.MqttCredentialExpiresAt = expiresAt
	d.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) SetFirmwareTarget(_ context.Context, deviceID, target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[deviceID]
	if !ok {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	d.FirmwareTarget = target
	d.UpdatedAt = time.Now().UTC()
	return nil
}

// --------- PgStore ---------

// NewPgStore 构造 PG 实现。
func NewPgStore(pool *pgxpool.Pool) Store {
	return &pgStore{pool: pool}
}

type pgStore struct {
	pool *pgxpool.Pool
}

const sqlGetDevice = `
SELECT id, COALESCE(room_id, ''), COALESCE(owner_id, ''), device_type, hardware_model,
       COALESCE(firmware_version, ''), COALESCE(firmware_target, ''), COALESCE(certificate_id, ''),
       online_status, last_seen_at, remaining_food_grams, health_status,
       COALESCE(mqtt_username, ''), COALESCE(mqtt_credential_hash, ''), mqtt_credential_expires_at,
       COALESCE(capability, '{}'::jsonb), region_id, created_at, updated_at
FROM devices WHERE id = $1`

const sqlInsertDevice = `
INSERT INTO devices
(id, room_id, owner_id, device_type, hardware_model, firmware_version, firmware_target, certificate_id,
 online_status, last_seen_at, remaining_food_grams, health_status,
 mqtt_username, mqtt_credential_hash, mqtt_credential_expires_at,
 capability, region_id, created_at, updated_at)
VALUES
($1, NULLIF($2,''), NULLIF($3,''), $4, $5, NULLIF($6,''), NULLIF($7,''), NULLIF($8,''),
 $9, $10, $11, $12,
 NULLIF($13,''), NULLIF($14,''), $15,
 $16::jsonb, $17, $18, $18)
ON CONFLICT (id) DO NOTHING`

const sqlUpdateDevice = `
UPDATE devices SET
  room_id = NULLIF($2,''),
  owner_id = NULLIF($3,''),
  device_type = $4,
  hardware_model = $5,
  firmware_version = NULLIF($6,''),
  firmware_target = NULLIF($7,''),
  certificate_id = NULLIF($8,''),
  online_status = $9,
  health_status = $10,
  capability = $11::jsonb,
  region_id = $12,
  updated_at = NOW()
WHERE id = $1`

const sqlMarkOnline = `
UPDATE devices SET
  online_status = 'online',
  last_seen_at = $2,
  remaining_food_grams = CASE WHEN $3 > 0 THEN $3 ELSE remaining_food_grams END,
  updated_at = $2
WHERE id = $1`

const sqlBindRoom = `
UPDATE devices SET room_id = $2, updated_at = NOW() WHERE id = $1`

const sqlUnbindRoom = `
UPDATE devices SET room_id = NULL, updated_at = NOW() WHERE id = $1`

const sqlSetMqttCred = `
UPDATE devices SET
  mqtt_username = $2,
  mqtt_credential_hash = $3,
  mqtt_credential_expires_at = $4,
  updated_at = NOW()
WHERE id = $1`

const sqlSetFirmwareTarget = `
UPDATE devices SET firmware_target = NULLIF($2,''), updated_at = NOW() WHERE id = $1`

const sqlSetDeviceStatus = `
UPDATE devices SET online_status = $2, updated_at = NOW() WHERE id = $1`

func (p *pgStore) Get(ctx context.Context, id string) (*Device, error) {
	return scanDevice(p.pool.QueryRow(ctx, sqlGetDevice, id))
}

func (p *pgStore) Create(ctx context.Context, d *Device) error {
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	if d.LastSeenAt.IsZero() {
		d.LastSeenAt = d.CreatedAt
	}
	if d.RegionID == "" {
		d.RegionID = "global"
	}
	if d.DeviceType == "" {
		d.DeviceType = "feeder"
	}
	if d.OnlineStatus == "" {
		d.OnlineStatus = "offline"
	}
	if d.HealthStatus == "" {
		d.HealthStatus = "unknown"
	}
	capJSON, _ := json.Marshal(coalesceMap(d.Capability))
	var expiresPtr *time.Time
	if !d.MqttCredentialExpiresAt.IsZero() {
		expiresPtr = &d.MqttCredentialExpiresAt
	}
	_, err := p.pool.Exec(ctx, sqlInsertDevice,
		d.ID, d.RoomID, d.OwnerID, d.DeviceType, d.HardwareModel,
		d.FirmwareVersion, d.FirmwareTarget, d.CertificateID,
		d.OnlineStatus, d.LastSeenAt, int(d.RemainingFoodGrams), d.HealthStatus,
		d.MqttUsername, d.MqttCredentialHash, expiresPtr,
		string(capJSON), d.RegionID, d.CreatedAt,
	)
	return err
}

func (p *pgStore) Update(ctx context.Context, d *Device) error {
	if d.RegionID == "" {
		d.RegionID = "global"
	}
	capJSON, _ := json.Marshal(coalesceMap(d.Capability))
	cmd, err := p.pool.Exec(ctx, sqlUpdateDevice,
		d.ID, d.RoomID, d.OwnerID, d.DeviceType, d.HardwareModel,
		d.FirmwareVersion, d.FirmwareTarget, d.CertificateID,
		d.OnlineStatus, d.HealthStatus, string(capJSON), d.RegionID,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	return nil
}

func (p *pgStore) List(ctx context.Context, f ListFilter) ([]Device, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	clauses := []string{}
	args := []any{}
	if f.OwnerID != "" {
		args = append(args, f.OwnerID)
		clauses = append(clauses, "owner_id = $"+itoa(len(args)))
	}
	if f.RoomID != "" {
		args = append(args, f.RoomID)
		clauses = append(clauses, "room_id = $"+itoa(len(args)))
	}
	if f.RegionID != "" {
		args = append(args, f.RegionID)
		clauses = append(clauses, "region_id = $"+itoa(len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		clauses = append(clauses, "online_status = $"+itoa(len(args)))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, f.Limit, f.Offset)
	q := `
		SELECT id, COALESCE(room_id, ''), COALESCE(owner_id, ''), device_type, hardware_model,
		       COALESCE(firmware_version, ''), COALESCE(firmware_target, ''), COALESCE(certificate_id, ''),
		       online_status, last_seen_at, remaining_food_grams, health_status,
		       COALESCE(mqtt_username, ''), COALESCE(mqtt_credential_hash, ''), mqtt_credential_expires_at,
		       COALESCE(capability, '{}'::jsonb), region_id, created_at, updated_at
		FROM devices` + where + ` ORDER BY created_at DESC LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args))
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Device, 0, f.Limit)
	for rows.Next() {
		d, err := scanDeviceRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (p *pgStore) SetStatus(ctx context.Context, id, status string) error {
	cmd, err := p.pool.Exec(ctx, sqlSetDeviceStatus, id, status)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	return nil
}

func (p *pgStore) MarkOnline(ctx context.Context, id string, remainingGrams uint32, at time.Time) error {
	_, err := p.pool.Exec(ctx, sqlMarkOnline, id, at, int(remainingGrams))
	return err
}

func (p *pgStore) BindRoom(ctx context.Context, deviceID, roomID string) error {
	cmd, err := p.pool.Exec(ctx, sqlBindRoom, deviceID, roomID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	return nil
}

func (p *pgStore) UnbindRoom(ctx context.Context, deviceID string) error {
	cmd, err := p.pool.Exec(ctx, sqlUnbindRoom, deviceID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	return nil
}

func (p *pgStore) SetMqttCredential(ctx context.Context, deviceID, username, plaintextPassword string, expiresAt time.Time) error {
	hash := HashPassword(username, plaintextPassword)
	cmd, err := p.pool.Exec(ctx, sqlSetMqttCred, deviceID, username, hash, expiresAt)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	return nil
}

func (p *pgStore) SetFirmwareTarget(ctx context.Context, deviceID, target string) error {
	cmd, err := p.pool.Exec(ctx, sqlSetFirmwareTarget, deviceID, target)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return yerr.New(yerr.DeviceUnbound, "device not found")
	}
	return nil
}

// ---------- helpers ----------

func scanDevice(row pgx.Row) (*Device, error) {
	d := &Device{}
	var capJSON []byte
	var remaining int
	var expiresAt *time.Time
	if err := row.Scan(
		&d.ID, &d.RoomID, &d.OwnerID, &d.DeviceType, &d.HardwareModel,
		&d.FirmwareVersion, &d.FirmwareTarget, &d.CertificateID,
		&d.OnlineStatus, &d.LastSeenAt, &remaining, &d.HealthStatus,
		&d.MqttUsername, &d.MqttCredentialHash, &expiresAt,
		&capJSON, &d.RegionID, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, yerr.New(yerr.DeviceUnbound, "device not found")
		}
		return nil, err
	}
	d.RemainingFoodGrams = uint32(remaining)
	if expiresAt != nil {
		d.MqttCredentialExpiresAt = *expiresAt
	}
	if len(capJSON) > 0 {
		_ = json.Unmarshal(capJSON, &d.Capability)
	}
	return d, nil
}

func scanDeviceRows(rows pgx.Rows) (*Device, error) {
	d := &Device{}
	var capJSON []byte
	var remaining int
	var expiresAt *time.Time
	if err := rows.Scan(
		&d.ID, &d.RoomID, &d.OwnerID, &d.DeviceType, &d.HardwareModel,
		&d.FirmwareVersion, &d.FirmwareTarget, &d.CertificateID,
		&d.OnlineStatus, &d.LastSeenAt, &remaining, &d.HealthStatus,
		&d.MqttUsername, &d.MqttCredentialHash, &expiresAt,
		&capJSON, &d.RegionID, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, err
	}
	d.RemainingFoodGrams = uint32(remaining)
	if expiresAt != nil {
		d.MqttCredentialExpiresAt = *expiresAt
	}
	if len(capJSON) > 0 {
		_ = json.Unmarshal(capJSON, &d.Capability)
	}
	return d, nil
}

func coalesceMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
