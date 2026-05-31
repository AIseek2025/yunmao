// Package store 提供 billing-svc 的持久化层。
//
// 实现：
//   - MemoryStore：进程内 map（与 service.BillingService 原行为对齐，默认）；
//   - PgStore：基于 pgxpool，落到 `orders`/`coupons`/`wallets` 表。
//
// 第三轮目标：把订单写入与 outbox 写入放在同一事务里；relay 负责异步把
// `order.created` / `order.paid` / `order.refunded` 事件推到 Kafka，下游
// （feeding-svc 的 feed_credits 扣减、admin 风控）按需消费。
//
// 与 migrations/0001_init.sql (orders) + 0003_third_iteration.sql
// (coupons / wallets + orders idempotency_key) 对齐。
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
)

// Order 订单 DTO（与 migration orders 表对齐）。
type Order struct {
	ID             string
	UserID         string
	Channel        string
	BizType        string
	AmountCny      uint32
	Status         string
	IdempotencyKey string
	RegionID       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	PaidAt         time.Time
	RefundedAt     time.Time
}

// OutboxEvent 与 feeding-svc/store 同形：一行待发布的事件。
type OutboxEvent struct {
	Topic        eventbus.Topic
	PartitionKey string
	CloudEvent   cloudevents.Event[any]
	Headers      map[string]string
}

// ErrNotFound 未找到。
var ErrNotFound = errors.New("billing store: order not found")

// ErrAlreadyPaid 订单已支付（重复 pay）。
var ErrAlreadyPaid = errors.New("billing store: order already paid")

// ErrInsufficient 钱包余额不足。
var ErrInsufficient = errors.New("billing store: insufficient funds")

// ErrAlreadyTerminal 钱包冻结记录已是终态。
var ErrAlreadyTerminal = errors.New("billing store: hold already terminal")

// WalletBalance 钱包余额。
type WalletBalance struct {
	UserID      string
	BalanceFen  int64
	ReservedFen int64
	RegionID    string
	UpdatedAt   time.Time
}

// WalletHold 钱包冻结记录。
type WalletHold struct {
	ID             string
	UserID         string
	RoomID         string
	CatID          string
	AmountFen      int64
	AmountGrams    uint32
	IdempotencyKey string
	Status         string // reserved | confirmed | cancelled | expired
	FeedRequestID  string
	RegionID       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ExpiresAt      time.Time
}

// Store billing-svc 持久层抽象。
type Store interface {
	CreateOrder(ctx context.Context, o Order, ev []OutboxEvent) error
	MarkPaid(ctx context.Context, id string, paidAt time.Time, ev []OutboxEvent) (*Order, error)
	Refund(ctx context.Context, id string, at time.Time, ev []OutboxEvent) (*Order, error)
	GetOrder(ctx context.Context, id string) (*Order, error)

	// 钱包 saga。第六轮新增。
	ReserveHold(ctx context.Context, h WalletHold, ev []OutboxEvent) (*WalletHold, error)
	ConfirmHold(ctx context.Context, id string, at time.Time, ev []OutboxEvent) (*WalletHold, error)
	CancelHold(ctx context.Context, id string, at time.Time, ev []OutboxEvent) (*WalletHold, error)
	GetHold(ctx context.Context, id string) (*WalletHold, error)
	GetWallet(ctx context.Context, userID string) (*WalletBalance, error)
	TopUpWallet(ctx context.Context, userID string, amountFen int64) error
}

// ---------- MemoryStore ----------

type memoryStore struct {
	mu       sync.Mutex
	m        map[string]*Order
	wallets  map[string]*WalletBalance
	holds    map[string]*WalletHold
	holdsIdx map[string]string // key: user_id|idempotency_key → hold_id
}

// NewMemoryStore 进程内实现（PoC / 单测）；outbox 行被丢弃。
func NewMemoryStore() Store {
	return &memoryStore{
		m:        map[string]*Order{},
		wallets:  map[string]*WalletBalance{},
		holds:    map[string]*WalletHold{},
		holdsIdx: map[string]string{},
	}
}

func (s *memoryStore) CreateOrder(_ context.Context, o Order, _ []OutboxEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[o.ID]; ok {
		return errors.New("duplicate order id")
	}
	cp := o
	s.m[o.ID] = &cp
	return nil
}

func (s *memoryStore) MarkPaid(_ context.Context, id string, t time.Time, _ []OutboxEvent) (*Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.m[id]
	if !ok {
		return nil, ErrNotFound
	}
	if o.Status == "paid" {
		return nil, ErrAlreadyPaid
	}
	o.Status = "paid"
	o.PaidAt = t
	o.UpdatedAt = t
	cp := *o
	return &cp, nil
}

func (s *memoryStore) Refund(_ context.Context, id string, t time.Time, _ []OutboxEvent) (*Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.m[id]
	if !ok {
		return nil, ErrNotFound
	}
	o.Status = "refunded"
	o.RefundedAt = t
	o.UpdatedAt = t
	cp := *o
	return &cp, nil
}

func (s *memoryStore) GetOrder(_ context.Context, id string) (*Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.m[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *o
	return &cp, nil
}

// ---- 钱包 saga（memory）----

func (s *memoryStore) ensureWallet(userID, region string) *WalletBalance {
	w, ok := s.wallets[userID]
	if !ok {
		w = &WalletBalance{UserID: userID, BalanceFen: 0, ReservedFen: 0, RegionID: region, UpdatedAt: time.Now().UTC()}
		s.wallets[userID] = w
	}
	return w
}

func (s *memoryStore) ReserveHold(_ context.Context, h WalletHold, _ []OutboxEvent) (*WalletHold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := h.UserID + "|" + h.IdempotencyKey
	if existing, ok := s.holdsIdx[key]; ok {
		cp := *s.holds[existing]
		return &cp, nil
	}
	w := s.ensureWallet(h.UserID, h.RegionID)
	available := w.BalanceFen - w.ReservedFen
	if available < h.AmountFen {
		return nil, ErrInsufficient
	}
	w.ReservedFen += h.AmountFen
	w.UpdatedAt = time.Now().UTC()
	cp := h
	s.holds[h.ID] = &cp
	s.holdsIdx[key] = h.ID
	out := cp
	return &out, nil
}

func (s *memoryStore) ConfirmHold(_ context.Context, id string, at time.Time, _ []OutboxEvent) (*WalletHold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.holds[id]
	if !ok {
		return nil, ErrNotFound
	}
	if h.Status == "confirmed" {
		cp := *h
		return &cp, nil
	}
	if h.Status != "reserved" {
		return nil, ErrAlreadyTerminal
	}
	w := s.ensureWallet(h.UserID, h.RegionID)
	w.BalanceFen -= h.AmountFen
	w.ReservedFen -= h.AmountFen
	w.UpdatedAt = at
	h.Status = "confirmed"
	h.UpdatedAt = at
	cp := *h
	return &cp, nil
}

func (s *memoryStore) CancelHold(_ context.Context, id string, at time.Time, _ []OutboxEvent) (*WalletHold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.holds[id]
	if !ok {
		return nil, ErrNotFound
	}
	if h.Status == "cancelled" {
		cp := *h
		return &cp, nil
	}
	if h.Status != "reserved" {
		return nil, ErrAlreadyTerminal
	}
	w := s.ensureWallet(h.UserID, h.RegionID)
	w.ReservedFen -= h.AmountFen
	w.UpdatedAt = at
	h.Status = "cancelled"
	h.UpdatedAt = at
	cp := *h
	return &cp, nil
}

func (s *memoryStore) GetHold(_ context.Context, id string) (*WalletHold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.holds[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *h
	return &cp, nil
}

func (s *memoryStore) GetWallet(_ context.Context, userID string) (*WalletBalance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.wallets[userID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *w
	return &cp, nil
}

func (s *memoryStore) TopUpWallet(_ context.Context, userID string, amountFen int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.ensureWallet(userID, "global")
	w.BalanceFen += amountFen
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// ---------- PgStore ----------

type pgStore struct {
	pool *pgxpool.Pool
}

// NewPgStore 基于 pgxpool 的实现；订单写入与 outbox 同事务。
func NewPgStore(pool *pgxpool.Pool) Store { return &pgStore{pool: pool} }

func (p *pgStore) CreateOrder(ctx context.Context, o Order, events []OutboxEvent) error {
	return runTx(ctx, p.pool, func(tx pgx.Tx) error {
		region := o.RegionID
		if region == "" {
			region = "global"
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO orders
			(id, user_id, channel, biz_type, amount_cny, status, idempotency_key, region_id, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7, ''),$8,$9,$9)
			ON CONFLICT (user_id, idempotency_key)
				WHERE idempotency_key IS NOT NULL DO NOTHING
		`,
			o.ID, o.UserID, o.Channel, o.BizType, o.AmountCny,
			o.Status, o.IdempotencyKey, region, o.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert orders: %w", err)
		}
		return writeOutbox(ctx, tx, region, events)
	})
}

func (p *pgStore) MarkPaid(ctx context.Context, id string, paidAt time.Time, events []OutboxEvent) (*Order, error) {
	var out *Order
	err := runTx(ctx, p.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE orders
			SET status = 'paid', paid_at = $1, updated_at = $1
			WHERE id = $2 AND status <> 'paid'
		`, paidAt, id)
		if err != nil {
			return fmt.Errorf("update orders paid: %w", err)
		}
		if tag.RowsAffected() == 0 {
			// 已经 paid 或 id 不存在；区分二者
			if _, err := loadOrderTx(ctx, tx, id); errors.Is(err, ErrNotFound) {
				return ErrNotFound
			}
			return ErrAlreadyPaid
		}
		o, err := loadOrderTx(ctx, tx, id)
		if err != nil {
			return err
		}
		out = o
		return writeOutbox(ctx, tx, o.RegionID, events)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *pgStore) Refund(ctx context.Context, id string, at time.Time, events []OutboxEvent) (*Order, error) {
	var out *Order
	err := runTx(ctx, p.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE orders SET status = 'refunded', refunded_at = $1, updated_at = $1
			WHERE id = $2 AND status <> 'refunded'
		`, at, id)
		if err != nil {
			return fmt.Errorf("update orders refund: %w", err)
		}
		o, err := loadOrderTx(ctx, tx, id)
		if err != nil {
			return err
		}
		out = o
		return writeOutbox(ctx, tx, o.RegionID, events)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *pgStore) GetOrder(ctx context.Context, id string) (*Order, error) {
	o := &Order{}
	var paidAt, refundedAt, updatedAt *time.Time
	var idem *string
	err := p.pool.QueryRow(ctx, `
		SELECT id, user_id, channel, biz_type, amount_cny, status,
		       idempotency_key, region_id, created_at, paid_at, refunded_at, updated_at
		FROM orders WHERE id = $1`, id).Scan(
		&o.ID, &o.UserID, &o.Channel, &o.BizType, &o.AmountCny, &o.Status,
		&idem, &o.RegionID, &o.CreatedAt, &paidAt, &refundedAt, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if idem != nil {
		o.IdempotencyKey = *idem
	}
	if paidAt != nil {
		o.PaidAt = *paidAt
	}
	if refundedAt != nil {
		o.RefundedAt = *refundedAt
	}
	if updatedAt != nil {
		o.UpdatedAt = *updatedAt
	}
	return o, nil
}

func loadOrderTx(ctx context.Context, tx pgx.Tx, id string) (*Order, error) {
	o := &Order{}
	var paidAt, refundedAt, updatedAt *time.Time
	var idem *string
	err := tx.QueryRow(ctx, `
		SELECT id, user_id, channel, biz_type, amount_cny, status,
		       idempotency_key, region_id, created_at, paid_at, refunded_at, updated_at
		FROM orders WHERE id = $1`, id).Scan(
		&o.ID, &o.UserID, &o.Channel, &o.BizType, &o.AmountCny, &o.Status,
		&idem, &o.RegionID, &o.CreatedAt, &paidAt, &refundedAt, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if idem != nil {
		o.IdempotencyKey = *idem
	}
	if paidAt != nil {
		o.PaidAt = *paidAt
	}
	if refundedAt != nil {
		o.RefundedAt = *refundedAt
	}
	if updatedAt != nil {
		o.UpdatedAt = *updatedAt
	}
	return o, nil
}

// ---- 钱包 saga（pgxpool）----

func (p *pgStore) ReserveHold(ctx context.Context, h WalletHold, events []OutboxEvent) (*WalletHold, error) {
	var out *WalletHold
	err := runTx(ctx, p.pool, func(tx pgx.Tx) error {
		// 1) 幂等：相同 (user_id, idempotency_key) 已存在则直接返回
		existing, errLoad := loadHoldByIdemTx(ctx, tx, h.UserID, h.IdempotencyKey)
		if errLoad == nil {
			out = existing
			return nil
		}
		if !errors.Is(errLoad, ErrNotFound) {
			return errLoad
		}
		// 2) 加锁取当前余额；如果钱包不存在则 upsert 一行（balance=0）
		region := h.RegionID
		if region == "" {
			region = "global"
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO wallet_balances (user_id, balance_fen, reserved_fen, region_id)
			VALUES ($1, 0, 0, $2)
			ON CONFLICT (user_id) DO NOTHING
		`, h.UserID, region)
		if err != nil {
			return fmt.Errorf("upsert wallet_balance: %w", err)
		}
		var balance, reserved int64
		if err := tx.QueryRow(ctx,
			`SELECT balance_fen, reserved_fen FROM wallet_balances WHERE user_id = $1 FOR UPDATE`,
			h.UserID,
		).Scan(&balance, &reserved); err != nil {
			return fmt.Errorf("select wallet for update: %w", err)
		}
		if balance-reserved < h.AmountFen {
			return ErrInsufficient
		}
		// 3) 插入 hold（unique 约束保护并发竞争）
		_, err = tx.Exec(ctx, `
			INSERT INTO wallet_holds
			(id, user_id, room_id, cat_id, amount_fen, amount_grams,
			 idempotency_key, status, feed_request_id, region_id,
			 created_at, updated_at, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'reserved',NULLIF($8,''),$9,$10,$10,$11)
		`,
			h.ID, h.UserID, h.RoomID, h.CatID, h.AmountFen, h.AmountGrams,
			h.IdempotencyKey, h.FeedRequestID, region, h.CreatedAt, h.ExpiresAt,
		)
		if err != nil {
			return fmt.Errorf("insert wallet_hold: %w", err)
		}
		// 4) 更新 reserved
		if _, err := tx.Exec(ctx,
			`UPDATE wallet_balances SET reserved_fen = reserved_fen + $1, updated_at = $2 WHERE user_id = $3`,
			h.AmountFen, h.CreatedAt, h.UserID,
		); err != nil {
			return fmt.Errorf("update wallet_balance reserved: %w", err)
		}
		out = &h
		return writeOutbox(ctx, tx, region, events)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *pgStore) ConfirmHold(ctx context.Context, id string, at time.Time, events []OutboxEvent) (*WalletHold, error) {
	var out *WalletHold
	err := runTx(ctx, p.pool, func(tx pgx.Tx) error {
		h, err := loadHoldTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if h.Status == "confirmed" {
			out = h
			return nil
		}
		if h.Status != "reserved" {
			return ErrAlreadyTerminal
		}
		if _, err := tx.Exec(ctx,
			`UPDATE wallet_holds SET status='confirmed', updated_at=$1 WHERE id=$2`,
			at, id,
		); err != nil {
			return fmt.Errorf("update hold confirm: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE wallet_balances
			 SET balance_fen = balance_fen - $1, reserved_fen = reserved_fen - $1, updated_at=$2
			 WHERE user_id = $3`,
			h.AmountFen, at, h.UserID,
		); err != nil {
			return fmt.Errorf("update wallet confirm: %w", err)
		}
		h.Status = "confirmed"
		h.UpdatedAt = at
		out = h
		return writeOutbox(ctx, tx, h.RegionID, events)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *pgStore) CancelHold(ctx context.Context, id string, at time.Time, events []OutboxEvent) (*WalletHold, error) {
	var out *WalletHold
	err := runTx(ctx, p.pool, func(tx pgx.Tx) error {
		h, err := loadHoldTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if h.Status == "cancelled" {
			out = h
			return nil
		}
		if h.Status != "reserved" {
			return ErrAlreadyTerminal
		}
		if _, err := tx.Exec(ctx,
			`UPDATE wallet_holds SET status='cancelled', updated_at=$1 WHERE id=$2`,
			at, id,
		); err != nil {
			return fmt.Errorf("update hold cancel: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE wallet_balances
			 SET reserved_fen = reserved_fen - $1, updated_at=$2
			 WHERE user_id = $3`,
			h.AmountFen, at, h.UserID,
		); err != nil {
			return fmt.Errorf("update wallet cancel: %w", err)
		}
		h.Status = "cancelled"
		h.UpdatedAt = at
		out = h
		return writeOutbox(ctx, tx, h.RegionID, events)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *pgStore) GetHold(ctx context.Context, id string) (*WalletHold, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return loadHoldTx(ctx, tx, id)
}

func (p *pgStore) GetWallet(ctx context.Context, userID string) (*WalletBalance, error) {
	w := &WalletBalance{}
	var updatedAt *time.Time
	err := p.pool.QueryRow(ctx,
		`SELECT user_id, balance_fen, reserved_fen, region_id, updated_at FROM wallet_balances WHERE user_id=$1`,
		userID,
	).Scan(&w.UserID, &w.BalanceFen, &w.ReservedFen, &w.RegionID, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if updatedAt != nil {
		w.UpdatedAt = *updatedAt
	}
	return w, nil
}

func (p *pgStore) TopUpWallet(ctx context.Context, userID string, amountFen int64) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO wallet_balances (user_id, balance_fen, reserved_fen, region_id)
		VALUES ($1, $2, 0, 'global')
		ON CONFLICT (user_id)
		DO UPDATE SET balance_fen = wallet_balances.balance_fen + EXCLUDED.balance_fen,
		              updated_at = NOW()
	`, userID, amountFen)
	return err
}

func loadHoldByIdemTx(ctx context.Context, tx pgx.Tx, userID, idem string) (*WalletHold, error) {
	h := &WalletHold{}
	var feedReq *string
	var updatedAt *time.Time
	err := tx.QueryRow(ctx, `
		SELECT id, user_id, room_id, cat_id, amount_fen, amount_grams,
		       idempotency_key, status, feed_request_id, region_id,
		       created_at, updated_at, expires_at
		FROM wallet_holds WHERE user_id=$1 AND idempotency_key=$2
	`, userID, idem).Scan(
		&h.ID, &h.UserID, &h.RoomID, &h.CatID, &h.AmountFen, &h.AmountGrams,
		&h.IdempotencyKey, &h.Status, &feedReq, &h.RegionID,
		&h.CreatedAt, &updatedAt, &h.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if feedReq != nil {
		h.FeedRequestID = *feedReq
	}
	if updatedAt != nil {
		h.UpdatedAt = *updatedAt
	}
	return h, nil
}

func loadHoldTx(ctx context.Context, tx pgx.Tx, id string) (*WalletHold, error) {
	h := &WalletHold{}
	var feedReq *string
	var updatedAt *time.Time
	err := tx.QueryRow(ctx, `
		SELECT id, user_id, room_id, cat_id, amount_fen, amount_grams,
		       idempotency_key, status, feed_request_id, region_id,
		       created_at, updated_at, expires_at
		FROM wallet_holds WHERE id=$1 FOR UPDATE
	`, id).Scan(
		&h.ID, &h.UserID, &h.RoomID, &h.CatID, &h.AmountFen, &h.AmountGrams,
		&h.IdempotencyKey, &h.Status, &feedReq, &h.RegionID,
		&h.CreatedAt, &updatedAt, &h.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if feedReq != nil {
		h.FeedRequestID = *feedReq
	}
	if updatedAt != nil {
		h.UpdatedAt = *updatedAt
	}
	return h, nil
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

func runTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MarshalEvent helper：给 service 层方便构造事件 payload。
func MarshalEvent(v any) ([]byte, error) { return json.Marshal(v) }
