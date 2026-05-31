package pay

import (
	"context"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AppleIAPConfig Apple IAP / StoreKit2 配置。
//
// 真实模式：
//   - BundleID 必填（StoreKit2 JWS audience 校验）；
//   - AppAppleID（App Store Connect 数字 ID）：可选，用于额外校验；
//   - SandboxBase：非空时所有验证走 sandbox endpoint。
//
// 第八轮（B）：JWS 验证用 stdlib ECDSA（Apple 使用 P-256 / SHA-256），
// x5c 头嵌入证书链；这里实现 leaf 证书自验签（生产建议至少校验签发链到 Apple
// Root CA—放到 trust store）。
type AppleIAPConfig struct {
	BundleID    string
	AppAppleID  string
	SandboxBase string
	// SharedSecret：v1 verifyReceipt 用；v2 JWS 不需要。
	SharedSecret string
	// MockMode：CI / dev 默认 true，校验走 HMAC（不走 Apple endpoint）。
	MockMode bool
	// TrustedRoots：可选 trust store；空时只做 leaf 自验签 + audience 校验。
	TrustedRoots [][]byte
}

// AppleIAPChannel Apple IAP 适配。
type AppleIAPChannel struct {
	cfg    AppleIAPConfig
	mu     sync.Mutex
	status map[string]string
	nonces map[string]int64
	roots  *x509.CertPool
}

// NewAppleIAPChannel 构造；BundleID 必填，否则强制 MockMode。
func NewAppleIAPChannel(cfg AppleIAPConfig) (*AppleIAPChannel, error) {
	if !cfg.MockMode {
		if cfg.BundleID == "" {
			cfg.MockMode = true
		}
	}
	if cfg.BundleID == "" {
		cfg.BundleID = "live.yunmao.app"
	}
	ch := &AppleIAPChannel{
		cfg:    cfg,
		status: map[string]string{},
		nonces: map[string]int64{},
	}
	if len(cfg.TrustedRoots) > 0 {
		ch.roots = x509.NewCertPool()
		for _, der := range cfg.TrustedRoots {
			cert, err := x509.ParseCertificate(der)
			if err != nil {
				return nil, fmt.Errorf("appleiap: parse trusted root: %w", err)
			}
			ch.roots.AddCert(cert)
		}
	}
	return ch, nil
}

// Name returns channel name.
func (a *AppleIAPChannel) Name() Channel { return ChannelAppleIAP }

// IsRealMode 暴露给监控。
func (a *AppleIAPChannel) IsRealMode() bool { return !a.cfg.MockMode }

// CreatePrepay Apple IAP 不在服务端创建 prepay，由客户端调用 StoreKit。
func (a *AppleIAPChannel) CreatePrepay(_ context.Context, req PrepayRequest) (*PrepayResponse, error) {
	if req.OrderID == "" || req.AmountFen <= 0 {
		return nil, errors.New("appleiap: order_id+amount required")
	}
	productID := req.Extra["apple_product_id"]
	if productID == "" {
		productID = "yunmao.feed.5g"
	}
	hints := map[string]string{
		"apple_product_id": productID,
		"bundle_id":        a.cfg.BundleID,
	}
	if a.cfg.MockMode {
		hints["mock"] = "true"
	}
	return &PrepayResponse{
		Channel:     ChannelAppleIAP,
		PrepayID:    "iap_" + req.OrderID,
		PayURL:      "",
		ClientHints: hints,
	}, nil
}

// VerifyWebhook 校验 Apple Server Notifications V2 JWS / mock HMAC。
//
// MockMode：HMAC-SHA256(SharedSecret, ts+nonce+body)。
// 真实模式：
//  1. 入参 raw 即 JWS（Header.Payload.Signature，base64url 编码）；
//  2. parse header → 提取 x5c → 验证 leaf cert 自签（如 TrustedRoots 配置则进 chain build）；
//  3. ECDSA(P-256) 验签 sha256(Header.Payload)；
//  4. payload 中 signedTransactionInfo / signedRenewalInfo 是另一层 JWS（同方法验证）；
//  5. 业务字段：bundleId / notificationType / appAppleId 校验。
func (a *AppleIAPChannel) VerifyWebhook(_ context.Context, raw []byte, headers map[string]string) (*WebhookEvent, error) {
	if a.cfg.MockMode {
		return a.verifyMock(raw, headers)
	}
	return a.verifyJWS(raw)
}

func (a *AppleIAPChannel) verifyMock(raw []byte, headers map[string]string) (*WebhookEvent, error) {
	ts := headers["X-Apple-Timestamp"]
	nonce := headers["X-Apple-Nonce"]
	sig := headers["X-Apple-Sig"]
	if ts == "" || nonce == "" || sig == "" {
		return nil, errors.New("appleiap: missing X-Apple-Timestamp/Nonce/Sig")
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, errors.New("appleiap: invalid ts")
	}
	if abs(time.Now().Unix()-tsInt) > 300 {
		return nil, errors.New("appleiap: ts outside 5min")
	}
	key := a.cfg.BundleID
	if a.cfg.SharedSecret != "" {
		key = a.cfg.SharedSecret
	}
	expected := appleMockSign(key, raw, ts, nonce)
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return nil, errors.New("appleiap: signature mismatch")
	}
	a.mu.Lock()
	if _, ok := a.nonces[nonce]; ok {
		a.mu.Unlock()
		return nil, errors.New("appleiap: nonce replayed")
	}
	a.nonces[nonce] = tsInt
	a.mu.Unlock()

	var body struct {
		OrderID               string `json:"order_id"`
		OriginalTransactionID string `json:"original_transaction_id"`
		NotificationType      string `json:"notification_type"`
		PriceFen              int64  `json:"price_fen"`
		BundleID              string `json:"bundle_id"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, errors.New("appleiap: invalid json body")
	}
	if body.BundleID != "" && body.BundleID != a.cfg.BundleID {
		return nil, errors.New("appleiap: bundle_id mismatch")
	}
	status := mapAppleStatus(body.NotificationType)
	a.mu.Lock()
	a.status[body.OrderID] = status
	a.mu.Unlock()
	return &WebhookEvent{
		Channel:         ChannelAppleIAP,
		OrderID:         body.OrderID,
		ExternalTradeNo: body.OriginalTransactionID,
		AmountFen:       body.PriceFen,
		Status:          status,
		OccurredAt:      tsInt,
	}, nil
}

// verifyJWS 真实 Apple Server Notifications V2 验证。
//
// 入参 raw 是顶层 JSON：`{"signedPayload": "<JWS string>"}`。
func (a *AppleIAPChannel) verifyJWS(raw []byte) (*WebhookEvent, error) {
	var wrapper struct {
		SignedPayload string `json:"signedPayload"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("appleiap: parse outer json: %w", err)
	}
	if wrapper.SignedPayload == "" {
		return nil, errors.New("appleiap: empty signedPayload")
	}
	claims, err := decodeAndVerifyAppleJWS(wrapper.SignedPayload, a.roots)
	if err != nil {
		return nil, fmt.Errorf("appleiap: jws: %w", err)
	}
	var notif struct {
		NotificationType string `json:"notificationType"`
		Subtype          string `json:"subtype"`
		NotificationUUID string `json:"notificationUUID"`
		Data             struct {
			BundleID              string `json:"bundleId"`
			AppAppleID            int64  `json:"appAppleId"`
			SignedTransactionInfo string `json:"signedTransactionInfo"`
			SignedRenewalInfo     string `json:"signedRenewalInfo"`
		} `json:"data"`
		SignedDate int64 `json:"signedDate"`
	}
	if err := json.Unmarshal(claims, &notif); err != nil {
		return nil, fmt.Errorf("appleiap: parse claims: %w", err)
	}
	if notif.Data.BundleID != a.cfg.BundleID {
		return nil, fmt.Errorf("appleiap: bundle_id mismatch (got %q)", notif.Data.BundleID)
	}
	if a.cfg.AppAppleID != "" && strconv.FormatInt(notif.Data.AppAppleID, 10) != a.cfg.AppAppleID {
		return nil, fmt.Errorf("appleiap: app_apple_id mismatch")
	}
	// 解嵌套 JWS（transaction info）。
	var tx struct {
		TransactionID         string `json:"transactionId"`
		OriginalTransactionID string `json:"originalTransactionId"`
		ProductID             string `json:"productId"`
		Price                 int64  `json:"price"` // micro-units
		Currency              string `json:"currency"`
		PurchaseDate          int64  `json:"purchaseDate"`
	}
	if notif.Data.SignedTransactionInfo != "" {
		txClaims, err := decodeAndVerifyAppleJWS(notif.Data.SignedTransactionInfo, a.roots)
		if err == nil {
			_ = json.Unmarshal(txClaims, &tx)
		}
	}
	status := mapAppleStatus(notif.NotificationType)
	amtFen := tx.Price / 10_000 // price 是百万分之一货币单位（micros）
	orderID := tx.OriginalTransactionID
	a.mu.Lock()
	a.status[orderID] = status
	a.mu.Unlock()
	return &WebhookEvent{
		Channel:         ChannelAppleIAP,
		OrderID:         orderID,
		ExternalTradeNo: tx.TransactionID,
		AmountFen:       amtFen,
		Status:          status,
		OccurredAt:      notif.SignedDate / 1000,
	}, nil
}

// QueryStatus mock 读 status；真实接 App Store Server API GET /transactions/{id}。
func (a *AppleIAPChannel) QueryStatus(_ context.Context, orderID string) (*QueryResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.status[orderID]
	if !ok {
		st = "pending"
	}
	return &QueryResult{
		OrderID: orderID, Status: st, Channel: ChannelAppleIAP,
		ExternalTradeNo: "iaptrade_" + orderID,
	}, nil
}

// Refund Apple IAP 退款由 Apple 主动发起；我方仅记录。
func (a *AppleIAPChannel) Refund(_ context.Context, req RefundRequest) (*RefundResult, error) {
	return &RefundResult{
		OrderID: req.OrderID, RefundID: "iaprefund_" + req.OrderID,
		Status: "pending", AmountFen: req.AmountFen, Channel: ChannelAppleIAP,
	}, nil
}

func appleMockSign(key string, body []byte, ts, nonce string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(ts))
	mac.Write([]byte("\n"))
	mac.Write([]byte(nonce))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func mapAppleStatus(typ string) string {
	switch typ {
	case "DID_RENEW", "SUBSCRIBED", "ONE_TIME_PURCHASE", "CONSUMABLE":
		return "paid"
	case "REFUND", "REFUND_REVERSED":
		return "refunded"
	case "CANCEL", "REVOKE", "EXPIRED":
		return "closed"
	}
	return "pending"
}

// decodeAndVerifyAppleJWS 解 JWS（Header.Payload.Signature），用 x5c leaf cert 公钥验签。
//
// 如 trustedRoots 非 nil，会构建 chain 校验签发链至 Apple Root CA。
func decodeAndVerifyAppleJWS(jws string, trustedRoots *x509.CertPool) ([]byte, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, errors.New("not a JWS")
	}
	hdrJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("hdr decode: %w", err)
	}
	var hdr struct {
		Alg string   `json:"alg"`
		X5c []string `json:"x5c"`
	}
	if err := json.Unmarshal(hdrJSON, &hdr); err != nil {
		return nil, fmt.Errorf("hdr json: %w", err)
	}
	if hdr.Alg != "ES256" {
		return nil, fmt.Errorf("unsupported alg %q (want ES256)", hdr.Alg)
	}
	if len(hdr.X5c) == 0 {
		return nil, errors.New("x5c missing")
	}
	leafDER, err := base64.StdEncoding.DecodeString(hdr.X5c[0])
	if err != nil {
		return nil, fmt.Errorf("x5c[0] decode: %w", err)
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return nil, fmt.Errorf("parse leaf: %w", err)
	}
	if trustedRoots != nil {
		intermediates := x509.NewCertPool()
		for i := 1; i < len(hdr.X5c); i++ {
			der, err := base64.StdEncoding.DecodeString(hdr.X5c[i])
			if err != nil {
				return nil, fmt.Errorf("x5c[%d] decode: %w", i, err)
			}
			cert, err := x509.ParseCertificate(der)
			if err != nil {
				return nil, fmt.Errorf("x5c[%d] parse: %w", i, err)
			}
			intermediates.AddCert(cert)
		}
		if _, err := leaf.Verify(x509.VerifyOptions{
			Roots:         trustedRoots,
			Intermediates: intermediates,
		}); err != nil {
			return nil, fmt.Errorf("chain verify: %w", err)
		}
	}
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("leaf cert not ECDSA")
	}
	signedInput := parts[0] + "." + parts[1]
	digest := sha256.Sum256([]byte(signedInput))
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("sig decode: %w", err)
	}
	if len(sigBytes) != 64 {
		return nil, fmt.Errorf("ES256 signature length=%d (need 64)", len(sigBytes))
	}
	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])
	if !ecdsa.Verify(pub, digest[:], r, s) {
		return nil, errors.New("ecdsa verify failed")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("payload decode: %w", err)
	}
	return payload, nil
}
