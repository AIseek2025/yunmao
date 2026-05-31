package pay

// 第八轮（B）：验证 wechat / alipay / appleiap 真实模式（非 MockMode）的
// 签名 + 验签链路（不依赖外部 SDK，全部 stdlib crypto）。

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// 生成 2048 bit RSA keypair → PEM-encoded private key + public key。
func genRSAKeyPair(t *testing.T) (privPEM, pubPEM []byte, priv *rsa.PrivateKey) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	privPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	pubDER, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return privPEM, pubPEM, k
}

func TestWeChatRealModeSignAndWebhookRSA(t *testing.T) {
	privPEM, pubPEM, priv := genRSAKeyPair(t)
	apiV3 := "0123456789abcdef0123456789abcdef" // 32 byte
	ch, err := NewWeChatChannel(WeChatConfig{
		MchID:             "TEST_MCH",
		APIv3Key:          apiV3,
		SerialNo:          "SERIAL123",
		APIClientKey:      privPEM,
		PlatformPublicKey: pubPEM,
		MockMode:          false,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if !ch.IsRealMode() {
		t.Fatal("expected real mode")
	}
	pp, err := ch.CreatePrepay(context.Background(), PrepayRequest{
		OrderID: "wxreal1", AmountFen: 100, Subject: "test", ClientType: "native",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pp.ClientHints["authorization"] == "" {
		t.Fatal("authorization header missing")
	}
	// 模拟微信平台回调：用 priv 来签名 webhook（生产用 platform pub），
	// 这里 platform priv 就是 priv（同密钥对）—— 测试目的：验证我们的
	// rsaVerifySHA256 + ts/nonce 拼接逻辑正确。
	body := []byte(`{"out_trade_no":"wxreal1","transaction_id":"tx_real","trade_state":"SUCCESS","amount":{"total":100}}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "noncereal1"
	signing := ts + "\n" + nonce + "\n" + string(body) + "\n"
	sig, err := rsaSignSHA256(priv, []byte(signing))
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{
		"Wechatpay-Timestamp": ts,
		"Wechatpay-Nonce":     nonce,
		"Wechatpay-Signature": base64.StdEncoding.EncodeToString(sig),
	}
	ev, err := ch.VerifyWebhook(context.Background(), body, headers)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Status != "paid" {
		t.Fatalf("status got %s", ev.Status)
	}
	if ev.AmountFen != 100 {
		t.Fatalf("amount got %d", ev.AmountFen)
	}
}

func TestWeChatRealModeAESGCMResourceDecrypt(t *testing.T) {
	privPEM, pubPEM, priv := genRSAKeyPair(t)
	apiV3 := "abcdefghijklmnop0123456789abcdef"
	ch, err := NewWeChatChannel(WeChatConfig{
		MchID:             "TEST_MCH",
		APIv3Key:          apiV3,
		SerialNo:          "SERIAL123",
		APIClientKey:      privPEM,
		PlatformPublicKey: pubPEM,
		MockMode:          false,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 构造 resource ciphertext。
	plainJSON := []byte(`{"out_trade_no":"wxenc1","transaction_id":"tx_enc","trade_state":"SUCCESS","amount":{"total":250}}`)
	nonce := "1234567890ab" // 12 byte
	associated := "transaction"
	block, _ := aes.NewCipher([]byte(apiV3))
	gcm, _ := cipher.NewGCM(block)
	ct := gcm.Seal(nil, []byte(nonce), plainJSON, []byte(associated))
	resource := map[string]string{
		"algorithm":       "AEAD_AES_256_GCM",
		"ciphertext":      base64.StdEncoding.EncodeToString(ct),
		"associated_data": associated,
		"nonce":           nonce,
	}
	body, _ := json.Marshal(map[string]any{
		"resource": resource,
	})
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonceHdr := "headerNonce1"
	signing := ts + "\n" + nonceHdr + "\n" + string(body) + "\n"
	sig, _ := rsaSignSHA256(priv, []byte(signing))
	headers := map[string]string{
		"Wechatpay-Timestamp": ts,
		"Wechatpay-Nonce":     nonceHdr,
		"Wechatpay-Signature": base64.StdEncoding.EncodeToString(sig),
	}
	ev, err := ch.VerifyWebhook(context.Background(), body, headers)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ev.OrderID != "wxenc1" {
		t.Fatalf("order_id got %s", ev.OrderID)
	}
	if ev.AmountFen != 250 {
		t.Fatalf("amount got %d", ev.AmountFen)
	}
}

func TestAlipayRealModeSignVerify(t *testing.T) {
	privPEM, pubPEM, priv := genRSAKeyPair(t)
	ch, err := NewAlipayChannel(AlipayConfig{
		AppID:           "yunmao-app",
		PrivateKeyPEM:   privPEM,
		AlipayPublicKey: pubPEM,
		MockMode:        false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ch.IsRealMode() {
		t.Fatal("expected real mode")
	}
	pp, err := ch.CreatePrepay(context.Background(), PrepayRequest{
		OrderID: "alireal1", AmountFen: 1234, Subject: "real test", ClientType: "pc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pp.PayURL == "" {
		t.Fatal("pay_url empty")
	}
	// 真实 alipay notify：form-encoded body with sign
	values := url.Values{}
	values.Set("app_id", "yunmao-app")
	values.Set("out_trade_no", "alireal1")
	values.Set("trade_no", "alitx_real")
	values.Set("trade_status", "TRADE_SUCCESS")
	values.Set("total_amount", "12.34")
	values.Set("sign_type", "RSA2")
	// 计算签名（用 alipayRSA2Sign 复用同算法）
	params := map[string]string{}
	for k, v := range values {
		params[k] = v[0]
	}
	sig, err := alipayRSA2Sign(priv, params)
	if err != nil {
		t.Fatal(err)
	}
	values.Set("sign", sig)
	rawForm := []byte(values.Encode())
	headers := map[string]string{
		"Alipay-Timestamp": strconv.FormatInt(time.Now().Unix(), 10),
	}
	ev, err := ch.VerifyWebhook(context.Background(), rawForm, headers)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Status != "paid" || ev.AmountFen != 1234 {
		t.Fatalf("status=%s amount=%d", ev.Status, ev.AmountFen)
	}
}

// TestAppleIAPRealModeJWS：自签 ES256 leaf cert + 构造 signedPayload + 验证。
func TestAppleIAPRealModeJWS(t *testing.T) {
	// 1. 生成 ECDSA P-256 keypair + self-signed cert
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Apple Test Leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}

	// 2. 构造 JWS：先签 transaction info，再签 outer notification
	txClaims := map[string]any{
		"transactionId":         "tx-jws-1",
		"originalTransactionId": "orig-jws-1",
		"productId":             "yunmao.feed.5g",
		"price":                 1_000_000, // 1.0 元（micro 单位 → 1元 = 10000 micros，1.0 元 = 1000000 micros…apple 用 cents micro）
		"currency":              "CNY",
		"purchaseDate":          time.Now().UnixMilli(),
	}
	txJWS := signAppleJWS(t, txClaims, der, priv)

	outerClaims := map[string]any{
		"notificationType": "ONE_TIME_PURCHASE",
		"notificationUUID": "uuid-1",
		"signedDate":       time.Now().UnixMilli(),
		"data": map[string]any{
			"bundleId":              "live.yunmao.app",
			"appAppleId":            123456,
			"signedTransactionInfo": txJWS,
		},
	}
	outerJWS := signAppleJWS(t, outerClaims, der, priv)
	body, _ := json.Marshal(map[string]string{"signedPayload": outerJWS})

	ch, err := NewAppleIAPChannel(AppleIAPConfig{
		BundleID: "live.yunmao.app",
		MockMode: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ch.IsRealMode() {
		t.Fatal("expected real mode")
	}
	ev, err := ch.VerifyWebhook(context.Background(), body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ev.OrderID != "orig-jws-1" {
		t.Fatalf("order_id=%s", ev.OrderID)
	}
	if ev.Status != "paid" {
		t.Fatalf("status=%s", ev.Status)
	}
}

func TestAppleIAPRealModeBundleIDMismatch(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Apple Test Leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, _ := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)

	outerClaims := map[string]any{
		"notificationType": "ONE_TIME_PURCHASE",
		"data":             map[string]any{"bundleId": "evil.app"},
	}
	outerJWS := signAppleJWS(t, outerClaims, der, priv)
	body, _ := json.Marshal(map[string]string{"signedPayload": outerJWS})

	ch, _ := NewAppleIAPChannel(AppleIAPConfig{
		BundleID: "live.yunmao.app",
		MockMode: false,
	})
	if _, err := ch.VerifyWebhook(context.Background(), body, nil); err == nil {
		t.Fatal("expect bundle_id mismatch")
	}
}

// signAppleJWS 自签一个 ES256 JWS（带 x5c）。
func signAppleJWS(t *testing.T, claims map[string]any, leafDER []byte, priv *ecdsa.PrivateKey) string {
	t.Helper()
	hdr := map[string]any{
		"alg": "ES256",
		"x5c": []string{base64.StdEncoding.EncodeToString(leafDER)},
	}
	hdrJSON, _ := json.Marshal(hdr)
	payloadJSON, _ := json.Marshal(claims)
	hdrB64 := base64.RawURLEncoding.EncodeToString(hdrJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signing := hdrB64 + "." + payloadB64
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	rb := r.Bytes()
	sb := s.Bytes()
	// pad to 32 bytes
	rPad := make([]byte, 32)
	sPad := make([]byte, 32)
	copy(rPad[32-len(rb):], rb)
	copy(sPad[32-len(sb):], sb)
	sig := append(rPad, sPad...)
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}
