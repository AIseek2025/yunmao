// Package store 抽象 feeding-svc 的持久化层。
//
// 提供两种实现：
//
//   - [`MemoryStore`]：进程内 map；用于单测 / `YUNMAO_DB_URL` 为空的 PoC 模式。
//   - [`PgStore`]：基于 pgxpool；落到 `feed_requests` + `feeding_request_events` + `outbox`。
//
// 关键约定：
//
//  1. `SaveTransition` 必须在单一事务里写 feed_requests + feeding_request_events + outbox；
//     这是 ADR-0010（事务性 outbox + 事件溯源）的核心要求。
//  2. `LoadByID` / `ListByRoom` / `ListByDevice` 用于运营 / 调试 UI。
//  3. `MemoryStore` 不写 outbox（无 outbox 概念）：service 层在 memory 模式下走直发 publisher。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"yunmao.live/pkg/yunmao/cloudevents"
	"yunmao.live/pkg/yunmao/db"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/feedstate"

	"yunmao.live/services/feeding-svc/publisher"
)

// FeedRequest 持久化层快照（与 service.Request 字段对齐，但不耦合）。
type FeedRequest struct {
	ID              string
	UserID          string
	RoomID          string
	CatID           string
	DeviceID        string
	DeviceCommandID string
	AmountGrams     uint32
	Status          feedstate.State
	IdempotencyKey  string
	RejectReason    string
	RegionID        string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Transition 是一条事件溯源行 + outbox 行的输入。
type Transition struct {
	Request FeedRequest
	From    feedstate.State
	To      feedstate.State
	Reason  string
	Actor   string

	// OutboxEvents 当前事务里要写入的 outbox 行（按 topic 分组）；空则不写。
	OutboxEvents []OutboxEvent
}

// OutboxEvent 是 service 层准备好的 outbox 消息载荷。
type OutboxEvent struct {
	Topic        eventbus.Topic
	PartitionKey string
	CloudEvent   cloudevents.Event[any]
	Headers      map[string]string
}

// Store 是统一持久化接口。
type Store interface {
	// Create 在状态机 `created → accepted` 时调用，写入第一行。
	Create(ctx context.Context, t Transition) error
	// SaveTransition 在状态变更时调用，追加事件溯源 + 可选 outbox。
	SaveTransition(ctx context.Context, t Transition) error
	// LoadByID 读单行；找不到返回 ErrNotFound。
	LoadByID(ctx context.Context, id string) (*FeedRequest, error)
	// ListByRoom 按 room_id 倒序分页。
	ListByRoom(ctx context.Context, roomID string, limit int) ([]FeedRequest, error)
	// ListByDevice 按 device_id 倒序分页。
	ListByDevice(ctx context.Context, deviceID string, limit int) ([]FeedRequest, error)
}

// ErrNotFound 未找到。
var ErrNotFound = errors.New("feeding store: not found")

// --------------------------------------------------------------------------------------
// MemoryStore
// --------------------------------------------------------------------------------------

// MemoryStore 进程内实现。
type MemoryStore struct {
	mu sync.RWMutex
	m  map[string]*FeedRequest
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{m: map[string]*FeedRequest{}} }

func (s *MemoryStore) Create(_ context.Context, t Transition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := t.Request
	r.Status = t.To
	r.UpdatedAt = time.Now().UTC()
	cp := r
	s.m[r.ID] = &cp
	return nil
}

func (s *MemoryStore) SaveTransition(_ context.Context, t Transition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[t.Request.ID]
	if !ok {
		cp := t.Request
		cp.Status = t.To
		cp.UpdatedAt = time.Now().UTC()
		s.m[cp.ID] = &cp
		return nil
	}
	r.Status = t.To
	r.UpdatedAt = time.Now().UTC()
	if t.Reason != "" {
		r.RejectReason = t.Reason
	}
	return nil
}

func (s *MemoryStore) LoadByID(_ context.Context, id string) (*FeedRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.m[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s *MemoryStore) ListByRoom(_ context.Context, roomID string, limit int) ([]FeedRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]FeedRequest, 0, limit)
	for _, r := range s.m {
		if r.RoomID == roomID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (s *MemoryStore) ListByDevice(_ context.Context, deviceID string, limit int) ([]FeedRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]FeedRequest, 0, limit)
	for _, r := range s.m {
		if r.DeviceID == deviceID {
			out = append(out, *r)
		}
	}
	return out, nil
}

// --------------------------------------------------------------------------------------
// PgStore
// --------------------------------------------------------------------------------------

// PgStore 基于 pgxpool 的实现。
type PgStore struct {
	pool *pgxpool.Pool
}

// NewPgStore 构造。
func NewPgStore(pool *pgxpool.Pool) *PgStore { return &PgStore{pool: pool} }

func (s *PgStore) Create(ctx context.Context, t Transition) error {
	return s.runTx(ctx, func(tx pgx.Tx) error {
		r := t.Request
		region := r.RegionID
		if region == "" {
			region = "global"
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO feed_requests
			(id, user_id, room_id, cat_id, device_id, device_command_id, amount_grams,
			 status, idempotency_key, region_id, created_at, updated_at)
			VALUES ($1,$2,$3,NULLIF($4, ''),NULLIF($5, ''),$6,$7,$8,$9,$10,$11,$11)
			ON CONFLICT (user_id, idempotency_key) DO NOTHING
		`,
			r.ID, r.UserID, r.RoomID, r.CatID, r.DeviceID,
			r.DeviceCommandID, r.AmountGrams, string(t.To),
			r.IdempotencyKey, region, r.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert feed_requests: %w", err)
		}
		if err := appendEvent(ctx, tx, r.ID, string(t.From), string(t.To), t.Reason, t.Actor, region, r); err != nil {
			return err
		}
		return writeOutbox(ctx, tx, region, t.OutboxEvents)
	})
}

func (s *PgStore) SaveTransition(ctx context.Context, t Transition) error {
	return s.runTx(ctx, func(tx pgx.Tx) error {
		r := t.Request
		region := r.RegionID
		if region == "" {
			region = "global"
		}
		_, err := tx.Exec(ctx, `
			UPDATE feed_requests
			SET status      = $1,
			    reject_reason = COALESCE(NULLIF($2, ''), reject_reason),
			    updated_at  = NOW()
			WHERE id = $3
		`, string(t.To), t.Reason, r.ID)
		if err != nil {
			return fmt.Errorf("update feed_requests: %w", err)
		}
		if err := appendEvent(ctx, tx, r.ID, string(t.From), string(t.To), t.Reason, t.Actor, region, r); err != nil {
			return err
		}
		return writeOutbox(ctx, tx, region, t.OutboxEvents)
	})
}

func (s *PgStore) LoadByID(ctx context.Context, id string) (*FeedRequest, error) {
	r := FeedRequest{}
	var catID, devID, cmdID, rejectReason *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, room_id, cat_id, device_id, device_command_id, amount_grams,
		       status, idempotency_key, reject_reason, region_id, created_at, updated_at
		FROM feed_requests WHERE id = $1
	`, id).Scan(
		&r.ID, &r.UserID, &r.RoomID, &catID, &devID, &cmdID, &r.AmountGrams,
		&r.Status, &r.IdempotencyKey, &rejectReason, &r.RegionID, &r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.CatID = derefStr(catID)
	r.DeviceID = derefStr(devID)
	r.DeviceCommandID = derefStr(cmdID)
	r.RejectReason = derefStr(rejectReason)
	return &r, nil
}

func (s *PgStore) ListByRoom(ctx context.Context, roomID string, limit int) ([]FeedRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, room_id, COALESCE(cat_id, ''), COALESCE(device_id, ''),
		       COALESCE(device_command_id, ''), amount_grams, status, idempotency_key,
		       COALESCE(reject_reason, ''), region_id, created_at, updated_at
		FROM feed_requests WHERE room_id = $1 ORDER BY created_at DESC LIMIT $2
	`, roomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanList(rows)
}

func (s *PgStore) ListByDevice(ctx context.Context, deviceID string, limit int) ([]FeedRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, room_id, COALESCE(cat_id, ''), COALESCE(device_id, ''),
		       COALESCE(device_command_id, ''), amount_grams, status, idempotency_key,
		       COALESCE(reject_reason, ''), region_id, created_at, updated_at
		FROM feed_requests WHERE device_id = $1 ORDER BY created_at DESC LIMIT $2
	`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanList(rows)
}

func scanList(rows pgx.Rows) ([]FeedRequest, error) {
	var out []FeedRequest
	for rows.Next() {
		var r FeedRequest
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.RoomID, &r.CatID, &r.DeviceID, &r.DeviceCommandID,
			&r.AmountGrams, &r.Status, &r.IdempotencyKey, &r.RejectReason,
			&r.RegionID, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func appendEvent(ctx context.Context, tx pgx.Tx, reqID, from, to, reason, actor, region string, snapshot FeedRequest) error {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if actor == "" {
		actor = "feeding-svc"
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO feeding_request_events
		(feed_request_id, from_state, to_state, reason, actor, region_id, payload)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, reqID, from, to, reason, actor, region, payload)
	return err
}

func writeOutbox(ctx context.Context, tx pgx.Tx, region string, events []OutboxEvent) error {
	for _, ev := range events {
		payload, err := ev.CloudEvent.MarshalJSON()
		if err != nil {
			return fmt.Errorf("marshal cloudevent: %w", err)
		}
		headers := ev.Headers
		if headers == nil {
			headers = map[string]string{
				"content-type": "application/cloudevents+json",
				"ce-type":      string(ev.Topic),
			}
		}
		if _, err := db.InsertOutbox(ctx, tx, db.OutboxRow{
			Topic:        string(ev.Topic),
			PartitionKey: ev.PartitionKey,
			Payload:      payload,
			Headers:      headers,
			RegionID:     region,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *PgStore) runTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// OutboxKafkaPublisher 是 outbox.Relay 的 Publisher 适配器：把 OutboxRow 直接推到 Kafka。
type OutboxKafkaPublisher struct {
	Bus eventbus.Bus
}

// Publish 实现 db.Publisher。
func (p *OutboxKafkaPublisher) Publish(ctx context.Context, row db.OutboxRow) error {
	return p.Bus.Publish(ctx, eventbus.Envelope{
		Topic:   eventbus.Topic(row.Topic),
		Key:     row.PartitionKey,
		Headers: row.Headers,
		Payload: row.Payload,
	})
}

// FeedCommandRequestedFromCloudEvent 帮助 device-svc 把 outbox payload 还原出业务字段。
func FeedCommandRequestedFromCloudEvent(b []byte) (publisher.FeedCommandRequested, error) {
	type wrapper struct {
		Data publisher.FeedCommandRequested `json:"data"`
	}
	w := wrapper{}
	if err := json.Unmarshal(b, &w); err != nil {
		return publisher.FeedCommandRequested{}, err
	}
	return w.Data, nil
}
