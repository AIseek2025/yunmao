// billing.go 把"投喂金额/积分扣减"以 saga 占位（Reserve -> Confirm/Cancel）形式串到 service 主流程。
//
// 设计目标（业务暂不算钱时仍保留可观察性）：
//
//   - feeding-svc 持有 BillingHook 接口，默认 NoopBilling 不收费；
//   - 当业务需要时切到 HTTP / gRPC 实现，调用 billing-svc 的预留 + 确认接口；
//   - 失败时使用 saga 补偿：Reject -> 取消保留 -> 状态退回 failed。
//
// 与 ADR-0010 outbox 兼容：billing 失败也是一次状态机迁移，记入 feeding_request_events。
package service

import (
	"context"
)

// BillingHook saga 占位接口。
//
//   - Reserve：在 Create 进入前调用；成功返回保留 ID（Receipt），失败短路；
//   - Confirm：在终态 Succeeded 时调用，把保留扣实账；
//   - Cancel：在终态 Failed / Rejected 时调用，归还保留额度。
//
// 业务暂不收钱时使用 NoopBilling，Reserve 永远返回 ("noop", nil)。
type BillingHook interface {
	Reserve(ctx context.Context, in BillingReserveInput) (string, error)
	Confirm(ctx context.Context, receiptID string) error
	Cancel(ctx context.Context, receiptID string) error
}

// BillingReserveInput 保留输入。
type BillingReserveInput struct {
	UserID         string
	RoomID         string
	CatID          string
	AmountGrams    uint32
	IdempotencyKey string
}

// NoopBilling 业务不收钱时的占位实现。
type NoopBilling struct{}

// Reserve 永远成功。
func (NoopBilling) Reserve(_ context.Context, _ BillingReserveInput) (string, error) {
	return "noop", nil
}

// Confirm 永远成功。
func (NoopBilling) Confirm(_ context.Context, _ string) error { return nil }

// Cancel 永远成功。
func (NoopBilling) Cancel(_ context.Context, _ string) error { return nil }

// SetBilling 注入 BillingHook（默认 NoopBilling）。
func (s *FeedingService) SetBilling(h BillingHook) {
	if h != nil {
		s.billing = h
	}
}
