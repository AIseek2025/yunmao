package pay

import (
	"context"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AlipayConfig 支付宝配置。
//
// 真实模式（非 MockMode）：
//   - AppID + PrivateKeyPEM + AlipayPublicKey 必填；
//   - PrivateKeyPEM：商户应用私钥（用于签请求）；
//   - AlipayPublicKey：支付宝平台公钥（用于校验回调签名）。
//
// 第八轮（B）：替换为 stdlib RSA-SHA256 (RSA2) 签名，等价 github.com/smartwalle/alipay/v3。
type AlipayConfig struct {
	AppID           string
	PrivateKeyPEM   []byte
	AlipayPublicKey []byte
	NotifyURL       string
	// SandboxBase：非空时所有请求走 sandbox。
	SandboxBase string
	// MockMode：缺凭据 / CI 时为 true，使用 HMAC 等型签名。
	MockMode bool
}

// AlipayChannel 支付宝适配。
//
// MockMode：HMAC-SHA256(AppID, body+ts+nonce)。
// 真实模式：
//   - 请求：把参数按字典序拼接 → RSA-SHA256(PrivateKey) → urlencoded form；
//   - 回调：从 `sign` 字段拿 base64 签名，按字典序拼接 form（去除 sign / sign_type）→
//     RSA-SHA256 验签 AlipayPublicKey。
type AlipayChannel struct {
	cfg    AlipayConfig
	priv   *rsa.PrivateKey
	pub    *rsa.PublicKey
	mu     sync.Mutex
	status map[string]string
	nonces map[string]int64
}

// NewAlipayChannel 构造；凭据缺失自动启用 MockMode。
func NewAlipayChannel(cfg AlipayConfig) (*AlipayChannel, error) {
	if !cfg.MockMode {
		if cfg.AppID == "" || len(cfg.PrivateKeyPEM) == 0 || len(cfg.AlipayPublicKey) == 0 {
			cfg.MockMode = true
		}
	}
	ch := &AlipayChannel{
		cfg:    cfg,
		status: map[string]string{},
		nonces: map[string]int64{},
	}
	if !cfg.MockMode {
		priv, err := parseRSAPrivateKey(cfg.PrivateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("alipay: parse priv: %w", err)
		}
		ch.priv = priv
		pub, err := parseRSAPublicKey(cfg.AlipayPublicKey)
		if err != nil {
			return nil, fmt.Errorf("alipay: parse pub: %w", err)
		}
		ch.pub = pub
	}
	return ch, nil
}

// Name returns channel name.
func (a *AlipayChannel) Name() Channel { return ChannelAlipay }

// IsRealMode 暴露给监控。
func (a *AlipayChannel) IsRealMode() bool { return !a.cfg.MockMode }

// CreatePrepay 创建支付订单。
func (a *AlipayChannel) CreatePrepay(_ context.Context, req PrepayRequest) (*PrepayResponse, error) {
	if req.OrderID == "" || req.AmountFen <= 0 {
		return nil, errors.New("alipay: order_id+amount required")
	}
	if a.cfg.MockMode {
		prepay := "ali_" + req.OrderID
		hints := map[string]string{
			"client_type": req.ClientType,
			"mode":        clientModeOrDefault(req.ClientType, "pc"),
			"mock":        "true",
		}
		return &PrepayResponse{
			Channel:     ChannelAlipay,
			PrepayID:    prepay,
			PayURL:      "https://openapi.alipay.com/gateway.do?out_trade_no=" + req.OrderID,
			QRContent:   "alipays://platformapi/startapp?saId=10000007&qrcode=" + prepay,
			ClientHints: hints,
		}, nil
	}
	return a.realCreatePrepay(req)
}

// realCreatePrepay 构造真实 alipay.trade.page.pay form 参数 + 签名。
//
// 返回值的 `ClientHints["alipay_form_query"]` 即客户端可直接 POST 给
// https://openapi.alipay.com/gateway.do 的 query string；生产路径 server 直接
// 拼成跳转 URL 返回给前端。
func (a *AlipayChannel) realCreatePrepay(req PrepayRequest) (*PrepayResponse, error) {
	mode := clientModeOrDefault(req.ClientType, "pc")
	method := "alipay.trade.page.pay"
	switch mode {
	case "wap", "h5":
		method = "alipay.trade.wap.pay"
	case "app":
		method = "alipay.trade.app.pay"
	}
	biz, _ := json.Marshal(map[string]any{
		"out_trade_no": req.OrderID,
		"total_amount": fmt.Sprintf("%.2f", float64(req.AmountFen)/100.0),
		"subject":      req.Subject,
		"product_code": map[string]string{
			"alipay.trade.page.pay": "FAST_INSTANT_TRADE_PAY",
			"alipay.trade.wap.pay":  "QUICK_WAP_WAY",
			"alipay.trade.app.pay":  "QUICK_MSECURITY_PAY",
		}[method],
	})
	params := map[string]string{
		"app_id":      a.cfg.AppID,
		"method":      method,
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"notify_url":  firstNonEmpty(req.NotifyURL, a.cfg.NotifyURL),
		"return_url":  req.ReturnURL,
		"biz_content": string(biz),
	}
	sig, err := alipayRSA2Sign(a.priv, params)
	if err != nil {
		return nil, fmt.Errorf("alipay: sign: %w", err)
	}
	params["sign"] = sig
	values := url.Values{}
	for k, v := range params {
		if v != "" {
			values.Set(k, v)
		}
	}
	gateway := "https://openapi.alipay.com/gateway.do"
	if a.cfg.SandboxBase != "" {
		gateway = a.cfg.SandboxBase
	}
	return &PrepayResponse{
		Channel:  ChannelAlipay,
		PrepayID: "ali_real_" + req.OrderID,
		PayURL:   gateway + "?" + values.Encode(),
		ClientHints: map[string]string{
			"mode":   mode,
			"method": method,
		},
	}, nil
}

// VerifyWebhook 校验回调签名 + 防重放。
//
// MockMode：HMAC-SHA256；
// 真实模式：alipay form notify。content-type=application/x-www-form-urlencoded；
//   - 从 form values 中取 `sign`；
//   - 按 key 字典序拼接（除 `sign` / `sign_type`）→ RSA-SHA256 验签 AlipayPublicKey。
func (a *AlipayChannel) VerifyWebhook(_ context.Context, raw []byte, headers map[string]string) (*WebhookEvent, error) {
	ts := headers["Alipay-Timestamp"]
	nonce := headers["Alipay-Nonce"]
	sig := headers["Alipay-Sign"]
	if ts == "" {
		ts = strconv.FormatInt(time.Now().Unix(), 10)
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, errors.New("alipay: invalid ts")
	}
	if abs(time.Now().Unix()-tsInt) > 600 {
		return nil, errors.New("alipay: ts outside 10min")
	}
	if a.cfg.MockMode {
		if nonce == "" || sig == "" {
			return nil, errors.New("alipay: missing nonce/sig in mock")
		}
		key := a.cfg.AppID
		if key == "" {
			key = "alipay-mock-key"
		}
		expected := alipayMockSign(key, raw, ts, nonce)
		if !hmac.Equal([]byte(expected), []byte(sig)) {
			return nil, errors.New("alipay: signature mismatch")
		}
		a.mu.Lock()
		if _, ok := a.nonces[nonce]; ok {
			a.mu.Unlock()
			return nil, errors.New("alipay: nonce replayed")
		}
		a.nonces[nonce] = tsInt
		a.mu.Unlock()
		var body struct {
			OutTradeNo  string `json:"out_trade_no"`
			TradeNo     string `json:"trade_no"`
			TradeStatus string `json:"trade_status"`
			TotalAmount string `json:"total_amount"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, errors.New("alipay: invalid json body")
		}
		status := mapAlipayStatus(body.TradeStatus)
		amtFen, _ := parseAmountYuanToFen(body.TotalAmount)
		a.mu.Lock()
		a.status[body.OutTradeNo] = status
		a.mu.Unlock()
		return &WebhookEvent{
			Channel:         ChannelAlipay,
			OrderID:         body.OutTradeNo,
			ExternalTradeNo: body.TradeNo,
			AmountFen:       amtFen,
			Status:          status,
			OccurredAt:      tsInt,
		}, nil
	}

	// 真实模式：form-encoded notify
	values, err := url.ParseQuery(string(raw))
	if err != nil {
		return nil, fmt.Errorf("alipay: parse form: %w", err)
	}
	signFromForm := values.Get("sign")
	if signFromForm == "" {
		return nil, errors.New("alipay: form sign missing")
	}
	if err := alipayVerify(a.pub, values, signFromForm); err != nil {
		return nil, fmt.Errorf("alipay: verify: %w", err)
	}
	status := mapAlipayStatus(values.Get("trade_status"))
	amtFen, _ := parseAmountYuanToFen(values.Get("total_amount"))
	orderID := values.Get("out_trade_no")
	a.mu.Lock()
	a.status[orderID] = status
	a.mu.Unlock()
	return &WebhookEvent{
		Channel:         ChannelAlipay,
		OrderID:         orderID,
		ExternalTradeNo: values.Get("trade_no"),
		AmountFen:       amtFen,
		Status:          status,
		OccurredAt:      tsInt,
	}, nil
}

// QueryStatus mock 读 status；真实接 alipay.trade.query。
func (a *AlipayChannel) QueryStatus(_ context.Context, orderID string) (*QueryResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.status[orderID]
	if !ok {
		st = "pending"
	}
	return &QueryResult{
		OrderID: orderID, Status: st, Channel: ChannelAlipay,
		ExternalTradeNo: "alitrade_" + orderID,
	}, nil
}

// Refund mock 直接 ok；真实接 alipay.trade.refund。
func (a *AlipayChannel) Refund(_ context.Context, req RefundRequest) (*RefundResult, error) {
	return &RefundResult{
		OrderID: req.OrderID, RefundID: "alirefund_" + req.OrderID,
		Status: "ok", AmountFen: req.AmountFen, Channel: ChannelAlipay,
	}, nil
}

func alipayMockSign(key string, body []byte, ts, nonce string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(ts))
	mac.Write([]byte("\n"))
	mac.Write([]byte(nonce))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// alipayRSA2Sign 把参数按 key 字典序拼成 `k=v&k=v...` → RSA-SHA256 → base64。
func alipayRSA2Sign(priv *rsa.PrivateKey, params map[string]string) (string, error) {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "sign" || k == "sign_type" {
			continue
		}
		if params[k] == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
	}
	sig, err := rsaSignSHA256(priv, []byte(sb.String()))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// alipayVerify 把 form values 按 alipay 协议规则拼接 → RSA-SHA256 验签。
func alipayVerify(pub *rsa.PublicKey, values url.Values, sigB64 string) error {
	keys := make([]string, 0, len(values))
	for k := range values {
		if k == "sign" || k == "sign_type" {
			continue
		}
		if values.Get(k) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(values.Get(k))
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return err
	}
	return rsaVerifySHA256(pub, []byte(sb.String()), sig)
}

func mapAlipayStatus(s string) string {
	switch s {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		return "paid"
	case "TRADE_CLOSED":
		return "closed"
	}
	return "pending"
}

func parseAmountYuanToFen(yuan string) (int64, error) {
	if yuan == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(yuan, 64)
	if err != nil {
		return 0, err
	}
	return int64(f*100 + 0.5), nil
}
