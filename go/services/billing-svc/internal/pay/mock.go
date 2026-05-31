package pay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"
)

// MockConfig mock 渠道配置；secret 用于 webhook HMAC。
type MockConfig struct {
	Secret string
	// ReplayWindow 允许的最大时间戳偏差（秒）；默认 300。
	ReplayWindowSeconds int64
}

// MockChannel 内置 mock 实现：
//   - CreatePrepay：返回 dummy prepay_id。
//   - VerifyWebhook：HMAC-SHA256 + 时间戳窗口 + nonce 防重放。
//   - QueryStatus：从内部 map 读最后一次 webhook 状态。
//   - Refund：直接返回成功。
//
// 用于 CI / 集成测试默认渠道。
type MockChannel struct {
	cfg    MockConfig
	mu     sync.Mutex
	status map[string]string // orderID → status
	nonces map[string]int64  // nonce → ts；防重放
}

// NewMockChannel 构造；secret 空时使用 "mock-secret"。
func NewMockChannel(cfg MockConfig) *MockChannel {
	if cfg.Secret == "" {
		cfg.Secret = "mock-secret"
	}
	if cfg.ReplayWindowSeconds <= 0 {
		cfg.ReplayWindowSeconds = 300
	}
	return &MockChannel{
		cfg:    cfg,
		status: map[string]string{},
		nonces: map[string]int64{},
	}
}

// Name returns channel name.
func (m *MockChannel) Name() Channel { return ChannelMock }

// CreatePrepay 返回 dummy prepay。
func (m *MockChannel) CreatePrepay(_ context.Context, req PrepayRequest) (*PrepayResponse, error) {
	if req.OrderID == "" {
		return nil, errors.New("mock: order_id required")
	}
	if req.AmountFen <= 0 {
		return nil, errors.New("mock: amount_fen must be > 0")
	}
	return &PrepayResponse{
		Channel:   ChannelMock,
		PrepayID:  "mockprepay_" + req.OrderID,
		PayURL:    "https://mock.pay.local/pay?prepay_id=mockprepay_" + req.OrderID,
		QRContent: "weixin://wxpay/mock/" + req.OrderID,
		ClientHints: map[string]string{
			"amount_fen": strconv.FormatInt(req.AmountFen, 10),
		},
	}, nil
}

// MockSign 用 HMAC-SHA256 对 body+ts+nonce 签名（便于测试构造合法回包）。
func MockSign(secret string, body []byte, ts, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("\n"))
	mac.Write([]byte(nonce))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyWebhook 校验签名 + 防重放。
func (m *MockChannel) VerifyWebhook(_ context.Context, raw []byte, headers map[string]string) (*WebhookEvent, error) {
	ts := headers["X-Yunmao-Pay-Ts"]
	nonce := headers["X-Yunmao-Pay-Nonce"]
	sig := headers["X-Yunmao-Pay-Sig"]
	if ts == "" || nonce == "" || sig == "" {
		return nil, errors.New("mock: missing X-Yunmao-Pay-Ts/Nonce/Sig")
	}
	now := time.Now().Unix()
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, errors.New("mock: invalid ts")
	}
	if abs(now-tsInt) > m.cfg.ReplayWindowSeconds {
		return nil, errors.New("mock: timestamp outside window")
	}
	expected := MockSign(m.cfg.Secret, raw, ts, nonce)
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return nil, errors.New("mock: signature mismatch")
	}
	m.mu.Lock()
	if _, ok := m.nonces[nonce]; ok {
		m.mu.Unlock()
		return nil, errors.New("mock: nonce replayed")
	}
	m.nonces[nonce] = tsInt
	m.mu.Unlock()

	var body struct {
		OrderID         string `json:"order_id"`
		ExternalTradeNo string `json:"external_trade_no"`
		AmountFen       int64  `json:"amount_fen"`
		Status          string `json:"status"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, errors.New("mock: invalid json body")
	}

	m.mu.Lock()
	m.status[body.OrderID] = body.Status
	m.mu.Unlock()

	return &WebhookEvent{
		Channel:         ChannelMock,
		OrderID:         body.OrderID,
		ExternalTradeNo: body.ExternalTradeNo,
		AmountFen:       body.AmountFen,
		Status:          body.Status,
		OccurredAt:      tsInt,
	}, nil
}

// QueryStatus 从内部状态表读取。
func (m *MockChannel) QueryStatus(_ context.Context, orderID string) (*QueryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.status[orderID]
	if !ok {
		st = "pending"
	}
	return &QueryResult{
		OrderID:         orderID,
		ExternalTradeNo: "mocktrade_" + orderID,
		Status:          st,
		Channel:         ChannelMock,
	}, nil
}

// Refund 直接成功（保留与真实渠道相同的接口）。
func (m *MockChannel) Refund(_ context.Context, req RefundRequest) (*RefundResult, error) {
	return &RefundResult{
		OrderID:   req.OrderID,
		RefundID:  "mockrefund_" + req.OrderID,
		Status:    "ok",
		AmountFen: req.AmountFen,
		Channel:   ChannelMock,
	}, nil
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
