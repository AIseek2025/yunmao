// Package store 实现 room-svc 的持久化层。
//
// 提供两种实现：
//
//   - [`MemoryStore`]：进程内 map；默认（YUNMAO_DB_URL 为空）。
//   - [`PgStore`]：基于 pgxpool；落到 `rooms` 表。
//
// 设计目标：与 service.RoomService 的内存实现 1:1 对齐；service 只持有 Store 接口。
package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	yerr "yunmao.live/pkg/yunmao/errors"
)

// Room 持久化层 DTO（与 migrations/0001 + 0003 + 0004 字段对齐）。
type Room struct {
	ID                  string
	CatID               string
	DeviceID            string
	OwnerID             string
	DisplayName         string
	Description         string
	City                string
	RegionID            string
	Visibility          string  // public | unlisted | private
	LiveStatus          string  // online | offline
	FeedingStatus       string  // open | closed
	Status              string  // live | offline | banned（第四轮新增）
	FeedCooldownSeconds uint32
	NoFeedWindowStart   string
	NoFeedWindowEnd     string
	StreamKey           string
	StreamKeyRotatedAt  time.Time
	CatIDs              []string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// ListFilter 列表过滤条件。
type ListFilter struct {
	OwnerID  string
	RegionID string
	Status   string
	Limit    int
	Offset   int
}

// Store 持久化接口。
type Store interface {
	Get(ctx context.Context, id string) (*Room, error)
	Create(ctx context.Context, r *Room) error
	Update(ctx context.Context, r *Room) error
	List(ctx context.Context, f ListFilter) ([]Room, error)
	SetStatus(ctx context.Context, id, status string) error
	SetStreamKey(ctx context.Context, id, streamKey string, rotatedAt time.Time) error
}

// ErrNotFound 未找到。
var ErrNotFound = errors.New("room store: not found")

// --------- MemoryStore ---------

// NewMemoryStore 构造内存实现。
func NewMemoryStore() Store {
	now := time.Now().UTC()
	m := &memoryStore{m: map[string]*Room{}}
	m.m["room_demo"] = &Room{
		ID: "room_demo", CatID: "cat_demo", DeviceID: "dev_demo",
		OwnerID:             "usr_demo",
		DisplayName:         "示例房间",
		City:                "上海",
		RegionID:            "global",
		Visibility:          "public",
		LiveStatus:          "online",
		FeedingStatus:       "open",
		Status:              "live",
		FeedCooldownSeconds: 30,
		CatIDs:              []string{"cat_demo"},
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	return m
}

type memoryStore struct {
	mu sync.RWMutex
	m  map[string]*Room
}

func (s *memoryStore) Get(_ context.Context, id string) (*Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.m[id]
	if !ok {
		return nil, yerr.New(yerr.RoomNotFound, "room not found")
	}
	c := *r
	return &c, nil
}

func (s *memoryStore) Create(_ context.Context, r *Room) error {
	if r == nil || r.ID == "" {
		return errors.New("room.id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	r.UpdatedAt = time.Now().UTC()
	c := *r
	s.m[r.ID] = &c
	return nil
}

func (s *memoryStore) Update(_ context.Context, r *Room) error {
	if r == nil || r.ID == "" {
		return errors.New("room.id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.m[r.ID]
	if !ok {
		return yerr.New(yerr.RoomNotFound, "room not found")
	}
	r.CreatedAt = existing.CreatedAt
	r.UpdatedAt = time.Now().UTC()
	c := *r
	s.m[r.ID] = &c
	return nil
}

func (s *memoryStore) List(_ context.Context, f ListFilter) ([]Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Room, 0, len(s.m))
	for _, r := range s.m {
		if f.OwnerID != "" && r.OwnerID != f.OwnerID {
			continue
		}
		if f.RegionID != "" && r.RegionID != f.RegionID {
			continue
		}
		if f.Status != "" && r.Status != f.Status {
			continue
		}
		out = append(out, *r)
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (s *memoryStore) SetStatus(_ context.Context, id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[id]
	if !ok {
		return yerr.New(yerr.RoomNotFound, "room not found")
	}
	r.Status = status
	r.LiveStatus = mapStatusToLive(status)
	r.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *memoryStore) SetStreamKey(_ context.Context, id, sk string, rotatedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[id]
	if !ok {
		return yerr.New(yerr.RoomNotFound, "room not found")
	}
	r.StreamKey = sk
	r.StreamKeyRotatedAt = rotatedAt
	r.UpdatedAt = time.Now().UTC()
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

// 等价于 internal/store/sql/queries.sql 中 GetRoom 的生成结果（手写 pgx，因 CI 无 sqlc 二进制）。
const sqlGetRoom = `
SELECT id, COALESCE(cat_id, ''), COALESCE(device_id, ''), COALESCE(owner_id, ''),
       display_name, COALESCE(description, ''), COALESCE(city, ''), region_id,
       visibility, live_status, feeding_status, COALESCE(status, 'offline'),
       feed_cooldown_seconds, COALESCE(no_feed_window_start, ''), COALESCE(no_feed_window_end, ''),
       COALESCE(stream_key, ''), stream_key_rotated_at,
       COALESCE(cat_ids, '{}'::text[]),
       created_at, updated_at
FROM rooms WHERE id = $1`

const sqlInsertRoom = `
INSERT INTO rooms
(id, cat_id, device_id, owner_id, display_name, description, city, region_id,
 visibility, live_status, feeding_status, status, feed_cooldown_seconds,
 no_feed_window_start, no_feed_window_end, stream_key, cat_ids, created_at, updated_at)
VALUES
($1, NULLIF($2,''), NULLIF($3,''), NULLIF($4,''), $5, NULLIF($6,''), NULLIF($7,''), $8,
 $9, $10, $11, $12, $13,
 NULLIF($14,''), NULLIF($15,''), NULLIF($16,''), $17, $18, $18)
ON CONFLICT (id) DO NOTHING`

const sqlUpdateRoom = `
UPDATE rooms SET
  cat_id = NULLIF($2,''),
  device_id = NULLIF($3,''),
  owner_id = NULLIF($4,''),
  display_name = $5,
  description = NULLIF($6,''),
  city = NULLIF($7,''),
  region_id = $8,
  visibility = $9,
  live_status = $10,
  feeding_status = $11,
  status = $12,
  feed_cooldown_seconds = $13,
  no_feed_window_start = NULLIF($14,''),
  no_feed_window_end = NULLIF($15,''),
  cat_ids = $16,
  updated_at = NOW()
WHERE id = $1`

const sqlSetRoomStatus = `
UPDATE rooms SET status = $2, live_status = $3, updated_at = NOW() WHERE id = $1`

const sqlSetStreamKey = `
UPDATE rooms SET stream_key = $2, stream_key_rotated_at = $3, updated_at = NOW() WHERE id = $1`

func (p *pgStore) Get(ctx context.Context, id string) (*Room, error) {
	row := p.pool.QueryRow(ctx, sqlGetRoom, id)
	return scanRoom(row)
}

func (p *pgStore) Create(ctx context.Context, r *Room) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	if r.RegionID == "" {
		r.RegionID = "global"
	}
	if r.Status == "" {
		r.Status = "offline"
	}
	if r.LiveStatus == "" {
		r.LiveStatus = "offline"
	}
	if r.FeedingStatus == "" {
		r.FeedingStatus = "closed"
	}
	if r.FeedCooldownSeconds == 0 {
		r.FeedCooldownSeconds = 30
	}
	if r.Visibility == "" {
		r.Visibility = "public"
	}
	_, err := p.pool.Exec(ctx, sqlInsertRoom,
		r.ID, r.CatID, r.DeviceID, r.OwnerID,
		r.DisplayName, r.Description, r.City, r.RegionID,
		r.Visibility, r.LiveStatus, r.FeedingStatus, r.Status, int(r.FeedCooldownSeconds),
		r.NoFeedWindowStart, r.NoFeedWindowEnd, r.StreamKey, r.CatIDs, r.CreatedAt,
	)
	return err
}

func (p *pgStore) Update(ctx context.Context, r *Room) error {
	if r.RegionID == "" {
		r.RegionID = "global"
	}
	cmd, err := p.pool.Exec(ctx, sqlUpdateRoom,
		r.ID, r.CatID, r.DeviceID, r.OwnerID,
		r.DisplayName, r.Description, r.City, r.RegionID,
		r.Visibility, r.LiveStatus, r.FeedingStatus, r.Status, int(r.FeedCooldownSeconds),
		r.NoFeedWindowStart, r.NoFeedWindowEnd, r.CatIDs,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return yerr.New(yerr.RoomNotFound, "room not found")
	}
	return nil
}

func (p *pgStore) List(ctx context.Context, f ListFilter) ([]Room, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	// 简单 where 拼接（参数化）。
	clauses := []string{}
	args := []any{}
	if f.OwnerID != "" {
		args = append(args, f.OwnerID)
		clauses = append(clauses, "owner_id = $"+itoa(len(args)))
	}
	if f.RegionID != "" {
		args = append(args, f.RegionID)
		clauses = append(clauses, "region_id = $"+itoa(len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		clauses = append(clauses, "COALESCE(status, 'offline') = $"+itoa(len(args)))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, f.Limit, f.Offset)
	q := `
		SELECT id, COALESCE(cat_id, ''), COALESCE(device_id, ''), COALESCE(owner_id, ''),
		       display_name, COALESCE(description, ''), COALESCE(city, ''), region_id,
		       visibility, live_status, feeding_status, COALESCE(status, 'offline'),
		       feed_cooldown_seconds, COALESCE(no_feed_window_start, ''), COALESCE(no_feed_window_end, ''),
		       COALESCE(stream_key, ''), stream_key_rotated_at,
		       COALESCE(cat_ids, '{}'::text[]),
		       created_at, updated_at
		FROM rooms` + where + ` ORDER BY created_at DESC LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args))
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Room, 0, f.Limit)
	for rows.Next() {
		r, err := scanRoomRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (p *pgStore) SetStatus(ctx context.Context, id, status string) error {
	cmd, err := p.pool.Exec(ctx, sqlSetRoomStatus, id, status, mapStatusToLive(status))
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return yerr.New(yerr.RoomNotFound, "room not found")
	}
	return nil
}

func (p *pgStore) SetStreamKey(ctx context.Context, id, sk string, rotatedAt time.Time) error {
	cmd, err := p.pool.Exec(ctx, sqlSetStreamKey, id, sk, rotatedAt)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return yerr.New(yerr.RoomNotFound, "room not found")
	}
	return nil
}

func scanRoom(row pgx.Row) (*Room, error) {
	r := &Room{}
	var streamRotatedAt *time.Time
	var cooldown int
	if err := row.Scan(
		&r.ID, &r.CatID, &r.DeviceID, &r.OwnerID,
		&r.DisplayName, &r.Description, &r.City, &r.RegionID,
		&r.Visibility, &r.LiveStatus, &r.FeedingStatus, &r.Status,
		&cooldown, &r.NoFeedWindowStart, &r.NoFeedWindowEnd,
		&r.StreamKey, &streamRotatedAt,
		&r.CatIDs,
		&r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, yerr.New(yerr.RoomNotFound, "room not found")
		}
		return nil, err
	}
	r.FeedCooldownSeconds = uint32(cooldown)
	if streamRotatedAt != nil {
		r.StreamKeyRotatedAt = *streamRotatedAt
	}
	return r, nil
}

func scanRoomRows(rows pgx.Rows) (*Room, error) {
	r := &Room{}
	var streamRotatedAt *time.Time
	var cooldown int
	if err := rows.Scan(
		&r.ID, &r.CatID, &r.DeviceID, &r.OwnerID,
		&r.DisplayName, &r.Description, &r.City, &r.RegionID,
		&r.Visibility, &r.LiveStatus, &r.FeedingStatus, &r.Status,
		&cooldown, &r.NoFeedWindowStart, &r.NoFeedWindowEnd,
		&r.StreamKey, &streamRotatedAt,
		&r.CatIDs,
		&r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return nil, err
	}
	r.FeedCooldownSeconds = uint32(cooldown)
	if streamRotatedAt != nil {
		r.StreamKeyRotatedAt = *streamRotatedAt
	}
	return r, nil
}

func mapStatusToLive(status string) string {
	switch status {
	case "live":
		return "online"
	case "banned", "offline":
		return "offline"
	default:
		return "offline"
	}
}

func itoa(n int) string {
	// 避免再 import strconv
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
