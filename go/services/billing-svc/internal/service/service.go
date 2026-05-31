// Package service billing-svc 订单与权益最小骨架（PoC + 第三轮 outbox 接入）。
//
// 状态机：created → paid → refunded（任一状态可独立查询）。
//
// 第三轮：
//
//   - 引入 store.Store 抽象；默认走 memory，YUNMAO_DB_URL 非空时切到 PG。
//   - PG 模式下订单写入与 outbox 行同事务；relay 异步把
//     `order.created` / `order.paid` / `order.refunded` 投递 Kafka。
//   - 不接真实支付通道；MarkPaid 仍是平台内调用。
package service

import (
	"context"
	"errors"
	"time"

	"yunmao.live/pkg/yunmao/cloudevents"
	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/eventbus"
	"yunmao.live/pkg/yunmao/ids"

	"yunmao.live/services/billing-svc/internal/store"
)

// Order 与上一轮对外字段对齐（保持 HTTP API 不变）。
type Order struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	Channel        string    `json:"channel"`  // wechat / alipay / apple_iap / manual
	BizType        string    `json:"biz_type"` // feed_ticket / membership / sponsorship
	AmountCny      uint32    `json:"amount_cny"`
	Status         string    `json:"status"` // created / paid / refunded
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	RegionID       string    `json:"region_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	PaidAt         time.Time `json:"paid_at,omitempty"`
	RefundedAt     time.Time `json:"refunded_at,omitempty"`
}

// BillingService 订单业务。
type BillingService struct {
	store  store.Store
	source string
}

// New 构造；store=nil 时回退到 memory。
func New(s store.Store) *BillingService {
	if s == nil {
		s = store.NewMemoryStore()
	}
	return &BillingService{store: s, source: "billing-svc"}
}

// SetSource 覆盖 CloudEvents source。
func (s *BillingService) SetSource(src string) {
	if src != "" {
		s.source = src
	}
}

// CreateInput POST /api/v1/orders 入参。
type CreateInput struct {
	UserID         string `json:"user_id"`
	Channel        string `json:"channel"`
	BizType        string `json:"biz_type"`
	AmountCny      uint32 `json:"amount_cny"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// Create 创建订单。
func (s *BillingService) Create(ctx context.Context, in CreateInput) (*Order, error) {
	if in.UserID == "" || in.Channel == "" || in.BizType == "" || in.AmountCny == 0 {
		return nil, yerr.New(yerr.SystemInternal, "missing required field")
	}
	now := time.Now().UTC()
	o := store.Order{
		ID:             ids.New(ids.PrefixOrder),
		UserID:         in.UserID,
		Channel:        in.Channel,
		BizType:        in.BizType,
		AmountCny:      in.AmountCny,
		Status:         "created",
		IdempotencyKey: in.IdempotencyKey,
		RegionID:       "global",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	ev := []store.OutboxEvent{ceFor(s.source, eventbus.TopicOrderCreated, o.UserID, o.ID, map[string]any{
		"order_id":   o.ID,
		"user_id":    o.UserID,
		"channel":    o.Channel,
		"biz_type":   o.BizType,
		"amount_cny": o.AmountCny,
		"created_at": o.CreatedAt.Format(time.RFC3339),
		"region_id":  o.RegionID,
	})}
	if err := s.store.CreateOrder(ctx, o, ev); err != nil {
		return nil, yerr.New(yerr.SystemInternal, "create order: "+err.Error())
	}
	return toExternal(&o), nil
}

// MarkPaid 标记订单已付款。
func (s *BillingService) MarkPaid(ctx context.Context, id string) (*Order, error) {
	now := time.Now().UTC()
	ev := []store.OutboxEvent{ceFor(s.source, eventbus.TopicOrderPaid, id, id, map[string]any{
		"order_id": id,
		"paid_at":  now.Format(time.RFC3339),
	})}
	o, err := s.store.MarkPaid(ctx, id, now, ev)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			return nil, yerr.New(yerr.SystemInternal, "order not found")
		case errors.Is(err, store.ErrAlreadyPaid):
			return nil, yerr.New(yerr.PayOrderPaid, "already paid")
		default:
			return nil, yerr.New(yerr.SystemInternal, "mark paid: "+err.Error())
		}
	}
	return toExternal(o), nil
}

// Refund 标记订单已退款。
func (s *BillingService) Refund(ctx context.Context, id string) (*Order, error) {
	now := time.Now().UTC()
	ev := []store.OutboxEvent{ceFor(s.source, eventbus.TopicOrderRefunded, id, id, map[string]any{
		"order_id":    id,
		"refunded_at": now.Format(time.RFC3339),
	})}
	o, err := s.store.Refund(ctx, id, now, ev)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, yerr.New(yerr.SystemInternal, "order not found")
		}
		return nil, yerr.New(yerr.SystemInternal, "refund: "+err.Error())
	}
	return toExternal(o), nil
}

// Get 查询订单。
func (s *BillingService) Get(ctx context.Context, id string) (*Order, error) {
	o, err := s.store.GetOrder(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, yerr.New(yerr.SystemInternal, "order not found")
		}
		return nil, yerr.New(yerr.SystemInternal, "get order: "+err.Error())
	}
	return toExternal(o), nil
}

func toExternal(o *store.Order) *Order {
	if o == nil {
		return nil
	}
	return &Order{
		ID:             o.ID,
		UserID:         o.UserID,
		Channel:        o.Channel,
		BizType:        o.BizType,
		AmountCny:      o.AmountCny,
		Status:         o.Status,
		IdempotencyKey: o.IdempotencyKey,
		RegionID:       o.RegionID,
		CreatedAt:      o.CreatedAt,
		PaidAt:         o.PaidAt,
		RefundedAt:     o.RefundedAt,
	}
}

func ceFor(source string, topic eventbus.Topic, partitionKey, subject string, payload any) store.OutboxEvent {
	return store.OutboxEvent{
		Topic:        topic,
		PartitionKey: partitionKey,
		CloudEvent:   cloudevents.New[any](string(topic), source, subject, payload),
		Headers: map[string]string{
			"content-type": "application/cloudevents+json",
			"ce-type":      string(topic),
		},
	}
}
