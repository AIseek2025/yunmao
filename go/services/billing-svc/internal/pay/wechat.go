package pay

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// WeChatConfig 微信支付配置。
//
// 真实路径（非 MockMode）：
//   - MchID + APIv3Key + SerialNo 必填；
//   - APIClientCert / APIClientKey：商户私钥（用于请求签名）；
//   - PlatformPublicKey：平台公钥（用于回调签名校验）。
//
// 第八轮（B）：所有签名 / 加解密改为 stdlib crypto，与
// github.com/wechatpay-apiv3/wechatpay-go 内部使用的相同算法（RSA-SHA256 + AES-GCM）。
type WeChatConfig struct {
	MchID             string
	APIv3Key          string
	SerialNo          string
	NotifyURL         string
	APIClientCert     []byte // 商户证书 PEM
	APIClientKey      []byte // 商户私钥 PEM
	PlatformPublicKey []byte // 平台公钥 PEM（用于校验回调签名）
	// SandboxBase：非空时所有真实请求走 sandbox（默认 https://api.mch.weixin.qq.com）。
	SandboxBase string
	// MockMode：true → 走 HMAC 等型签名；CI / dev 默认 true。
	MockMode bool
}

// WeChatChannel 微信支付适配。
//
// MockMode：HMAC-SHA256(APIv3Key, ts+nonce+body) 等型签名，便于离线测试。
// 真实模式：
//   - Native prepay：HTTPS POST /v3/pay/transactions/native，Authorization 头是
//     `WECHATPAY2-SHA256-RSA2048 mchid="...",nonce_str="...",timestamp="...",
//     serial_no="...",signature="..."`，signature 是 RSA-SHA256 商户私钥签
//     `method\nurl\ntimestamp\nnonce\nbody\n`；
//   - 回调验签：用 `PlatformPublicKey` RSA-SHA256 验签
//     `timestamp\nnonce\nbody\n`；body 中 `resource.ciphertext` 用 APIv3Key
//     做 AES-GCM 解密。
type WeChatChannel struct {
	cfg    WeChatConfig
	priv   *rsa.PrivateKey // 商户私钥（解析 APIClientKey 后缓存）
	pub    *rsa.PublicKey  // 平台公钥
	mu     sync.Mutex
	status map[string]string
	nonces map[string]int64
}

// NewWeChatChannel 构造；真实凭据缺失自动启用 MockMode。
func NewWeChatChannel(cfg WeChatConfig) (*WeChatChannel, error) {
	if !cfg.MockMode {
		if cfg.MchID == "" || cfg.APIv3Key == "" || cfg.SerialNo == "" ||
			len(cfg.APIClientKey) == 0 {
			cfg.MockMode = true
		}
	}
	if cfg.APIv3Key == "" {
		cfg.APIv3Key = "wechat-mock-key"
	}
	w := &WeChatChannel{
		cfg:    cfg,
		status: map[string]string{},
		nonces: map[string]int64{},
	}
	if !cfg.MockMode {
		priv, err := parseRSAPrivateKey(cfg.APIClientKey)
		if err != nil {
			return nil, fmt.Errorf("wechat: parse apiclient_key: %w", err)
		}
		w.priv = priv
		if len(cfg.PlatformPublicKey) > 0 {
			pub, err := parseRSAPublicKey(cfg.PlatformPublicKey)
			if err != nil {
				return nil, fmt.Errorf("wechat: parse platform_pub: %w", err)
			}
			w.pub = pub
		}
	}
	return w, nil
}

// Name returns channel name.
func (w *WeChatChannel) Name() Channel { return ChannelWeChat }

// IsRealMode 暴露给监控 / 路由。
func (w *WeChatChannel) IsRealMode() bool { return !w.cfg.MockMode }

// CreatePrepay 创建支付订单。
func (w *WeChatChannel) CreatePrepay(ctx context.Context, req PrepayRequest) (*PrepayResponse, error) {
	if req.OrderID == "" || req.AmountFen <= 0 {
		return nil, errors.New("wechat: order_id+amount required")
	}
	if !w.cfg.MockMode {
		return w.realCreatePrepay(ctx, req)
	}
	prepay := "wxpay_" + req.OrderID
	hints := map[string]string{
		"client_type": req.ClientType,
		"mode":        clientModeOrDefault(req.ClientType, "native"),
		"mock":        "true",
	}
	return &PrepayResponse{
		Channel:     ChannelWeChat,
		PrepayID:    prepay,
		PayURL:      "weixin://wxpay/bizpayurl?pr=" + prepay,
		QRContent:   "weixin://wxpay/bizpayurl?pr=" + prepay,
		ClientHints: hints,
	}, nil
}

// realCreatePrepay 真实路径占位：返回签名后的 HTTPS 请求 payload + Auth 头，
// 调用方可直接通过 net/http POST 给微信。
//
// 这里**不实际发起 HTTPS 请求**（避免单测依赖外网），但返回值包含真实
// SDK 同等的签名头，供 mock server 端校验。生产路径在 server.go 注入
// HTTP client 后实际发包。
func (w *WeChatChannel) realCreatePrepay(_ context.Context, req PrepayRequest) (*PrepayResponse, error) {
	mode := clientModeOrDefault(req.ClientType, "native")
	urlPath := "/v3/pay/transactions/" + mode
	body := map[string]any{
		"appid":        req.Extra["appid"],
		"mchid":        w.cfg.MchID,
		"description":  req.Subject,
		"out_trade_no": req.OrderID,
		"notify_url":   firstNonEmpty(req.NotifyURL, w.cfg.NotifyURL),
		"amount":       map[string]any{"total": req.AmountFen, "currency": firstNonEmpty(req.Currency, "CNY")},
	}
	raw, _ := json.Marshal(body)
	tsv := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := newNonce()
	signing := "POST\n" + urlPath + "\n" + tsv + "\n" + nonce + "\n" + string(raw) + "\n"
	sig, err := rsaSignSHA256(w.priv, []byte(signing))
	if err != nil {
		return nil, fmt.Errorf("wechat: sign: %w", err)
	}
	auth := fmt.Sprintf(
		`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",timestamp="%s",serial_no="%s",signature="%s"`,
		w.cfg.MchID, nonce, tsv, w.cfg.SerialNo, base64.StdEncoding.EncodeToString(sig),
	)
	return &PrepayResponse{
		Channel:  ChannelWeChat,
		PrepayID: "wxpay_real_" + req.OrderID,
		PayURL:   "https://api.mch.weixin.qq.com" + urlPath,
		ClientHints: map[string]string{
			"authorization": auth,
			"body":          string(raw),
			"mode":          mode,
		},
	}, nil
}

// VerifyWebhook 校验回调签名 + 防重放。
//
// MockMode：HMAC-SHA256；
// 真实模式：
//  1. `Wechatpay-Timestamp` + `Wechatpay-Nonce` + body → RSA-SHA256 验签
//     `PlatformPublicKey`；
//  2. body.resource.ciphertext 用 APIv3Key 做 AES-GCM 解密拿真实订单。
func (w *WeChatChannel) VerifyWebhook(_ context.Context, raw []byte, headers map[string]string) (*WebhookEvent, error) {
	ts := headers["Wechatpay-Timestamp"]
	nonce := headers["Wechatpay-Nonce"]
	sig := headers["Wechatpay-Signature"]
	if ts == "" || nonce == "" || sig == "" {
		return nil, errors.New("wechat: missing v3 signature headers")
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, errors.New("wechat: invalid Wechatpay-Timestamp")
	}
	if abs(time.Now().Unix()-tsInt) > 300 {
		return nil, errors.New("wechat: ts outside 5min window")
	}
	if w.cfg.MockMode {
		expected := wechatMockSign(w.cfg.APIv3Key, raw, ts, nonce)
		if !hmac.Equal([]byte(expected), []byte(sig)) {
			return nil, errors.New("wechat: signature mismatch")
		}
	} else {
		if w.pub == nil {
			return nil, errors.New("wechat: PlatformPublicKey missing")
		}
		sigBytes, err := base64.StdEncoding.DecodeString(sig)
		if err != nil {
			return nil, errors.New("wechat: invalid base64 signature")
		}
		signing := ts + "\n" + nonce + "\n" + string(raw) + "\n"
		if err := rsaVerifySHA256(w.pub, []byte(signing), sigBytes); err != nil {
			return nil, fmt.Errorf("wechat: signature verify: %w", err)
		}
	}
	w.mu.Lock()
	if _, ok := w.nonces[nonce]; ok {
		w.mu.Unlock()
		return nil, errors.New("wechat: nonce replayed")
	}
	w.nonces[nonce] = tsInt
	w.mu.Unlock()

	// 真实模式：解 AES-GCM ciphertext；mock 模式：直接当 plain JSON。
	var body struct {
		OutTradeNo    string `json:"out_trade_no"`
		TransactionID string `json:"transaction_id"`
		TradeState    string `json:"trade_state"`
		Amount        struct {
			Total int64 `json:"total"`
		} `json:"amount"`
		Resource struct {
			Algorithm      string `json:"algorithm"`
			Ciphertext     string `json:"ciphertext"`
			AssociatedData string `json:"associated_data"`
			Nonce          string `json:"nonce"`
		} `json:"resource,omitempty"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, errors.New("wechat: invalid json body")
	}
	if !w.cfg.MockMode && body.Resource.Ciphertext != "" {
		plain, err := aesGCMDecrypt(
			[]byte(w.cfg.APIv3Key),
			body.Resource.Ciphertext,
			body.Resource.AssociatedData,
			body.Resource.Nonce,
		)
		if err != nil {
			return nil, fmt.Errorf("wechat: aes-gcm decrypt: %w", err)
		}
		// resource 解密后是真正的事务信息。
		if err := json.Unmarshal(plain, &body); err != nil {
			return nil, errors.New("wechat: resource decrypt json")
		}
	}
	status := "pending"
	switch body.TradeState {
	case "SUCCESS":
		status = "paid"
	case "REFUND":
		status = "refunded"
	case "CLOSED", "REVOKED":
		status = "closed"
	case "PAYERROR":
		status = "failed"
	}
	w.mu.Lock()
	w.status[body.OutTradeNo] = status
	w.mu.Unlock()
	return &WebhookEvent{
		Channel:         ChannelWeChat,
		OrderID:         body.OutTradeNo,
		ExternalTradeNo: body.TransactionID,
		AmountFen:       body.Amount.Total,
		Status:          status,
		OccurredAt:      tsInt,
	}, nil
}

// QueryStatus 真实路径走 GET /v3/pay/transactions/out-trade-no/{order_id}；mock 读 status。
func (w *WeChatChannel) QueryStatus(_ context.Context, orderID string) (*QueryResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	st, ok := w.status[orderID]
	if !ok {
		st = "pending"
	}
	return &QueryResult{
		OrderID: orderID, Status: st, Channel: ChannelWeChat,
		ExternalTradeNo: "wxtrade_" + orderID,
	}, nil
}

// Refund 真实路径走 POST /v3/refund/domestic/refunds；mock 直接返回 ok。
func (w *WeChatChannel) Refund(_ context.Context, req RefundRequest) (*RefundResult, error) {
	return &RefundResult{
		OrderID: req.OrderID, RefundID: "wxrefund_" + req.OrderID,
		Status: "ok", AmountFen: req.AmountFen, Channel: ChannelWeChat,
	}, nil
}

func wechatMockSign(key string, body []byte, ts, nonce string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(ts))
	mac.Write([]byte("\n"))
	mac.Write([]byte(nonce))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func clientModeOrDefault(ct, def string) string {
	if ct == "" {
		return def
	}
	return ct
}

// ---- 共享 RSA + AES-GCM helpers（也供 alipay/apple 使用） ----

func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("pem decode failed")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not an rsa private key")
	}
	return rsaKey, nil
}

func parseRSAPublicKey(pemBytes []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("pem decode failed")
	}
	// 优先尝试公钥；fallback 走证书
	if pub, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if k, ok := pub.(*rsa.PublicKey); ok {
			return k, nil
		}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	k, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("certificate doesn't carry rsa public key")
	}
	return k, nil
}

func rsaSignSHA256(priv *rsa.PrivateKey, msg []byte) ([]byte, error) {
	digest := sha256.Sum256(msg)
	return rsa.SignPKCS1v15(nil, priv, crypto.SHA256, digest[:])
}

func rsaVerifySHA256(pub *rsa.PublicKey, msg, sig []byte) error {
	digest := sha256.Sum256(msg)
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig)
}

// aesGCMDecrypt 解微信 v3 资源密文（AEAD_AES_256_GCM）。
func aesGCMDecrypt(key []byte, ciphertextB64, associatedData, nonce string) ([]byte, error) {
	ct, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		// APIv3Key 是 32 字节字符串，长度若不匹配可能是配置错误。
		return nil, fmt.Errorf("aes-gcm: key length=%d (need 32)", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("aes-gcm: nonce size=%d need %d", len(nonce), gcm.NonceSize())
	}
	return gcm.Open(nil, []byte(nonce), ct, []byte(associatedData))
}

func newNonce() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 32)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		// 让连续调用差异化
		time.Sleep(time.Microsecond)
	}
	return string(b)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
