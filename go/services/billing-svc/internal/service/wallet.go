// wallet.go：billing-svc 钱包余额 + Reserve/Confirm/Cancel saga 状态机。
//
// 与 ADR-0010 outbox 一致：所有持久化写入与 outbox 行同事务；relay 异步推 Kafka。
//
// 业务约束：
//   - Reserve 在可用余额（balance - reserved）>= amount 时通过；否则 ErrInsufficient。
//   - Confirm 把 hold 从 'reserved' 转 'confirmed'，并把 balance -= amount, reserved -= amount。
//   - Cancel 把 hold 从 'reserved' 转 'cancelled'，并 reserved -= amount。
//   - 重复调用相同 idempotency_key 的 Reserve：返回已有 hold（read-only）。
//   - 重复 Confirm/Cancel 相同 hold_id：no-op 返回当前 hold。
//   - 终态 hold 不可再 Confirm/Cancel；返回 ErrTerminalStatus。
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

// 钱包域错误码。
var (
	ErrInsufficient   = errors.New("billing: insufficient funds")
	ErrHoldNotFound   = errors.New("billing: hold not found")
	ErrTerminalStatus = errors.New("billing: hold already in terminal status")
)

// WalletReserveInput Reserve 入参。
type WalletReserveInput struct {
	UserID         string `json:"user_id"`
	RoomID         string `json:"room_id"`
	CatID          string `json:"cat_id"`
	FeedRequestID  string `json:"feed_request_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
	// AmountGrams 是猫粮克数；service 层按 ConvertGramsToFen 折算费用。
	AmountGrams uint32 `json:"amount_grams"`
	// TTLSeconds Reserve 自动过期时长，默认 60s。
	TTLSeconds int `json:"ttl_seconds,omitempty"`
}

// WalletHold 钱包冻结记录的对外 DTO（与 store.WalletHold 字段对齐）。
type WalletHold struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	RoomID        string    `json:"room_id"`
	CatID         string    `json:"cat_id"`
	AmountFen     int64     `json:"amount_fen"`
	AmountGrams   uint32    `json:"amount_grams"`
	Status        string    `json:"status"`
	FeedRequestID string    `json:"feed_request_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// ConvertGramsToFen 把"克"折算到"分"；当前阶段固定 1g = 5分（业务可配）。
// 第七轮接入价目表前的默认值。
func ConvertGramsToFen(grams uint32) int64 {
	const fenPerGram = 5
	return int64(grams) * int64(fenPerGram)
}

// Reserve 冻结一笔余额；幂等。
func (s *BillingService) Reserve(ctx context.Context, in WalletReserveInput) (*WalletHold, error) {
	if in.UserID == "" || in.IdempotencyKey == "" || in.AmountGrams == 0 {
		return nil, yerr.New(yerr.SystemInternal, "missing required field")
	}
	if in.TTLSeconds <= 0 {
		in.TTLSeconds = 60
	}
	now := time.Now().UTC()
	hold := store.WalletHold{
		ID:             ids.New(ids.PrefixWalletHold),
		UserID:         in.UserID,
		RoomID:         in.RoomID,
		CatID:          in.CatID,
		AmountFen:      ConvertGramsToFen(in.AmountGrams),
		AmountGrams:    in.AmountGrams,
		IdempotencyKey: in.IdempotencyKey,
		Status:         "reserved",
		FeedRequestID:  in.FeedRequestID,
		RegionID:       "global",
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(time.Duration(in.TTLSeconds) * time.Second),
	}
	ev := []store.OutboxEvent{ceFor(s.source, eventbus.TopicWalletReserved, hold.UserID, hold.ID, map[string]any{
		"hold_id":         hold.ID,
		"user_id":         hold.UserID,
		"room_id":         hold.RoomID,
		"cat_id":          hold.CatID,
		"feed_request_id": hold.FeedRequestID,
		"amount_fen":      hold.AmountFen,
		"amount_grams":    hold.AmountGrams,
		"created_at":      hold.CreatedAt.Format(time.RFC3339),
		"expires_at":      hold.ExpiresAt.Format(time.RFC3339),
	})}
	out, err := s.store.ReserveHold(ctx, hold, ev)
	if err != nil {
		if errors.Is(err, store.ErrInsufficient) {
			return nil, yerr.New(yerr.PayChannelFailed, "insufficient funds")
		}
		return nil, yerr.New(yerr.SystemInternal, "reserve: "+err.Error())
	}
	return walletHoldExternal(out), nil
}

// Confirm 把 hold 转为已扣账。
func (s *BillingService) Confirm(ctx context.Context, holdID string) (*WalletHold, error) {
	if holdID == "" {
		return nil, yerr.New(yerr.SystemInternal, "hold_id required")
	}
	now := time.Now().UTC()
	ev := []store.OutboxEvent{ceFor(s.source, eventbus.TopicWalletConfirmed, "hold", holdID, map[string]any{
		"hold_id":      holdID,
		"confirmed_at": now.Format(time.RFC3339),
	})}
	out, err := s.store.ConfirmHold(ctx, holdID, now, ev)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			return nil, yerr.New(yerr.SystemInternal, "hold not found")
		case errors.Is(err, store.ErrAlreadyTerminal):
			return nil, yerr.New(yerr.SystemInternal, "hold already in terminal status")
		default:
			return nil, yerr.New(yerr.SystemInternal, "confirm: "+err.Error())
		}
	}
	return walletHoldExternal(out), nil
}

// Cancel 释放 hold。
func (s *BillingService) Cancel(ctx context.Context, holdID, reason string) (*WalletHold, error) {
	if holdID == "" {
		return nil, yerr.New(yerr.SystemInternal, "hold_id required")
	}
	now := time.Now().UTC()
	if reason == "" {
		reason = "cancelled"
	}
	ev := []store.OutboxEvent{ceFor(s.source, eventbus.TopicWalletCancelled, "hold", holdID, map[string]any{
		"hold_id":      holdID,
		"reason":       reason,
		"cancelled_at": now.Format(time.RFC3339),
	})}
	out, err := s.store.CancelHold(ctx, holdID, now, ev)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			return nil, yerr.New(yerr.SystemInternal, "hold not found")
		case errors.Is(err, store.ErrAlreadyTerminal):
			return nil, yerr.New(yerr.SystemInternal, "hold already in terminal status")
		default:
			return nil, yerr.New(yerr.SystemInternal, "cancel: "+err.Error())
		}
	}
	return walletHoldExternal(out), nil
}

// GetHold 查询 hold（admin / debug）。
func (s *BillingService) GetHold(ctx context.Context, holdID string) (*WalletHold, error) {
	h, err := s.store.GetHold(ctx, holdID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, yerr.New(yerr.SystemInternal, "hold not found")
		}
		return nil, yerr.New(yerr.SystemInternal, "get hold: "+err.Error())
	}
	return walletHoldExternal(h), nil
}

// Wallet 查询用户钱包余额。
func (s *BillingService) Wallet(ctx context.Context, userID string) (*store.WalletBalance, error) {
	if userID == "" {
		return nil, yerr.New(yerr.SystemInternal, "user_id required")
	}
	w, err := s.store.GetWallet(ctx, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &store.WalletBalance{UserID: userID, BalanceFen: 0, ReservedFen: 0}, nil
		}
		return nil, yerr.New(yerr.SystemInternal, "get wallet: "+err.Error())
	}
	return w, nil
}

// TopUp（dev / test）：充值钱包。
func (s *BillingService) TopUp(ctx context.Context, userID string, amountFen int64) error {
	if userID == "" || amountFen <= 0 {
		return yerr.New(yerr.SystemInternal, "user_id and positive amount required")
	}
	return s.store.TopUpWallet(ctx, userID, amountFen)
}

func walletHoldExternal(h *store.WalletHold) *WalletHold {
	if h == nil {
		return nil
	}
	return &WalletHold{
		ID:            h.ID,
		UserID:        h.UserID,
		RoomID:        h.RoomID,
		CatID:         h.CatID,
		AmountFen:     h.AmountFen,
		AmountGrams:   h.AmountGrams,
		Status:        h.Status,
		FeedRequestID: h.FeedRequestID,
		CreatedAt:     h.CreatedAt,
		ExpiresAt:     h.ExpiresAt,
	}
}

// 占位以避免 cloudevents import 未用（与 ceFor 一同导出）。
var _ = cloudevents.New[any]
