// Package pay billing-svc 多支付渠道抽象。
//
// 第七轮（E 落地）：
//   - PayChannel：单一接口，所有渠道实现 4 个方法；
//   - 4 个适配器：mock（CI 默认）/ wechat / alipay / appleiap；
//   - 真实 SDK 引入留到生产编译路径，本轮 mock 闭环跑通；
//   - webhook 校验在 Verifier 内部完成；上层 Confirm 走 saga。
//
// 决策见 ADR-0021 支付渠道抽象与 webhook 安全模型。
package pay

import (
	"context"
	"errors"
	"sync"
)

// Channel 支付渠道枚举。
type Channel string

const (
	ChannelMock     Channel = "mock"
	ChannelWeChat   Channel = "wechat"
	ChannelAlipay   Channel = "alipay"
	ChannelAppleIAP Channel = "appleiap"
)

// Validate 简单合法性检查。
func (c Channel) Validate() error {
	switch c {
	case ChannelMock, ChannelWeChat, ChannelAlipay, ChannelAppleIAP:
		return nil
	}
	return errors.New("pay: unknown channel " + string(c))
}

// PrepayRequest 创建支付请求入参。
type PrepayRequest struct {
	OrderID    string            `json:"order_id"`
	AmountFen  int64             `json:"amount_fen"`
	Subject    string            `json:"subject"`
	Currency   string            `json:"currency,omitempty"` // 默认 CNY
	UserID     string            `json:"user_id,omitempty"`
	ReturnURL  string            `json:"return_url,omitempty"`
	NotifyURL  string            `json:"notify_url,omitempty"`
	ClientType string            `json:"client_type,omitempty"` // web|h5|app|ios|android|miniprog
	Extra      map[string]string `json:"extra,omitempty"`
}

// PrepayResponse 创建支付响应。
//
// 不同渠道字段含义不同：WeChat = prepay_id（Native: code_url；JSAPI: paySign）；
// Alipay = trade_no + qr_code（PC qrPay）/ form_html（Web）；
// AppleIAP = 不下发 prepay（IAP 在客户端完成，仅返回 receipt 校验地址）；
// Mock = 直接返回 dummy prepay_id。
type PrepayResponse struct {
	Channel     Channel           `json:"channel"`
	PrepayID    string            `json:"prepay_id"`
	PayURL      string            `json:"pay_url,omitempty"`
	QRContent   string            `json:"qr_content,omitempty"`
	ClientHints map[string]string `json:"client_hints,omitempty"`
	RawResponse map[string]any    `json:"raw_response,omitempty"`
}

// WebhookEvent 解析后的回调事件。
type WebhookEvent struct {
	Channel         Channel           `json:"channel"`
	OrderID         string            `json:"order_id"`
	ExternalTradeNo string            `json:"external_trade_no"`
	AmountFen       int64             `json:"amount_fen"`
	Status          string            `json:"status"` // paid / refunded / closed / failed
	OccurredAt      int64             `json:"occurred_at"`
	Raw             map[string]any    `json:"raw,omitempty"`
	Headers         map[string]string `json:"-"`
}

// QueryResult 查询支付状态。
type QueryResult struct {
	OrderID         string         `json:"order_id"`
	ExternalTradeNo string         `json:"external_trade_no"`
	Status          string         `json:"status"`
	AmountFen       int64          `json:"amount_fen"`
	Raw             map[string]any `json:"raw,omitempty"`
	Channel         Channel        `json:"channel"`
}

// RefundRequest 退款入参。
type RefundRequest struct {
	OrderID         string `json:"order_id"`
	ExternalTradeNo string `json:"external_trade_no"`
	AmountFen       int64  `json:"amount_fen"`
	Reason          string `json:"reason,omitempty"`
}

// RefundResult 退款结果。
type RefundResult struct {
	OrderID   string         `json:"order_id"`
	RefundID  string         `json:"refund_id"`
	Status    string         `json:"status"` // ok / pending / failed
	AmountFen int64          `json:"amount_fen"`
	Channel   Channel        `json:"channel"`
	Raw       map[string]any `json:"raw,omitempty"`
}

// PayChannel 渠道抽象。
type PayChannel interface {
	Name() Channel
	CreatePrepay(ctx context.Context, req PrepayRequest) (*PrepayResponse, error)
	// VerifyWebhook 入参 raw body + headers（含签名/timestamp/nonce）。
	VerifyWebhook(ctx context.Context, raw []byte, headers map[string]string) (*WebhookEvent, error)
	QueryStatus(ctx context.Context, orderID string) (*QueryResult, error)
	Refund(ctx context.Context, req RefundRequest) (*RefundResult, error)
}

// Registry 多渠道路由表；按 channel 名取实现。
type Registry struct {
	mu       sync.RWMutex
	channels map[Channel]PayChannel
}

// NewRegistry 构造。
func NewRegistry() *Registry { return &Registry{channels: map[Channel]PayChannel{}} }

// Register 注册渠道实现。
func (r *Registry) Register(c PayChannel) {
	r.mu.Lock()
	r.channels[c.Name()] = c
	r.mu.Unlock()
}

// Get 取渠道实现；不存在则返回 error。
func (r *Registry) Get(c Channel) (PayChannel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.channels[c]
	if !ok {
		return nil, errors.New("pay: channel not registered: " + string(c))
	}
	return v, nil
}

// Names 列出已注册渠道（用于 /internal/diagnose）。
func (r *Registry) Names() []Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Channel, 0, len(r.channels))
	for k := range r.channels {
		out = append(out, k)
	}
	return out
}
