package pay

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func ts() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

func TestMockChannelPrepayRefund(t *testing.T) {
	ch := NewMockChannel(MockConfig{Secret: "k"})
	got, err := ch.CreatePrepay(context.Background(), PrepayRequest{OrderID: "o1", AmountFen: 100})
	if err != nil {
		t.Fatal(err)
	}
	if got.PrepayID == "" {
		t.Fatal("prepay_id empty")
	}
	r, err := ch.Refund(context.Background(), RefundRequest{OrderID: "o1", AmountFen: 100})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "ok" {
		t.Fatalf("want ok got %s", r.Status)
	}
}

func TestMockChannelWebhookHappyAndReplay(t *testing.T) {
	ch := NewMockChannel(MockConfig{Secret: "k"})
	body, _ := json.Marshal(map[string]any{
		"order_id":          "o1",
		"external_trade_no": "x1",
		"amount_fen":        100,
		"status":            "paid",
	})
	tsv := ts()
	nonce := "n-1"
	sig := MockSign("k", body, tsv, nonce)
	headers := map[string]string{
		"X-Yunmao-Pay-Ts":    tsv,
		"X-Yunmao-Pay-Nonce": nonce,
		"X-Yunmao-Pay-Sig":   sig,
	}
	ev, err := ch.VerifyWebhook(context.Background(), body, headers)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Status != "paid" {
		t.Fatalf("want paid got %s", ev.Status)
	}
	// 重放：同 nonce
	_, err = ch.VerifyWebhook(context.Background(), body, headers)
	if err == nil {
		t.Fatal("want replay rejected")
	}
}

func TestMockChannelWebhookBadSig(t *testing.T) {
	ch := NewMockChannel(MockConfig{Secret: "k"})
	body := []byte(`{"order_id":"o1","status":"paid"}`)
	headers := map[string]string{
		"X-Yunmao-Pay-Ts":    ts(),
		"X-Yunmao-Pay-Nonce": "n-bad",
		"X-Yunmao-Pay-Sig":   "deadbeef",
	}
	_, err := ch.VerifyWebhook(context.Background(), body, headers)
	if err == nil {
		t.Fatal("want signature mismatch")
	}
}

func TestMockChannelWebhookStaleTs(t *testing.T) {
	ch := NewMockChannel(MockConfig{Secret: "k", ReplayWindowSeconds: 5})
	body := []byte(`{"order_id":"o1","status":"paid"}`)
	stale := strconv.FormatInt(time.Now().Add(-1*time.Hour).Unix(), 10)
	sig := MockSign("k", body, stale, "n-stale")
	headers := map[string]string{
		"X-Yunmao-Pay-Ts":    stale,
		"X-Yunmao-Pay-Nonce": "n-stale",
		"X-Yunmao-Pay-Sig":   sig,
	}
	_, err := ch.VerifyWebhook(context.Background(), body, headers)
	if err == nil {
		t.Fatal("want ts window error")
	}
}

func TestWeChatChannelPrepayAndWebhook(t *testing.T) {
	ch, err := NewWeChatChannel(WeChatConfig{MockMode: true})
	if err != nil {
		t.Fatal(err)
	}
	pp, err := ch.CreatePrepay(context.Background(), PrepayRequest{OrderID: "wx1", AmountFen: 50})
	if err != nil {
		t.Fatal(err)
	}
	if pp.PrepayID == "" {
		t.Fatal("prepay empty")
	}
	body, _ := json.Marshal(map[string]any{
		"out_trade_no":   "wx1",
		"transaction_id": "wxchain_1",
		"trade_state":    "SUCCESS",
		"amount":         map[string]int{"total": 50},
	})
	tsv := ts()
	nonce := "wxn1"
	sig := wechatMockSign("wechat-mock-key", body, tsv, nonce)
	headers := map[string]string{
		"Wechatpay-Timestamp": tsv,
		"Wechatpay-Nonce":     nonce,
		"Wechatpay-Signature": sig,
	}
	ev, err := ch.VerifyWebhook(context.Background(), body, headers)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Status != "paid" {
		t.Fatalf("want paid got %s", ev.Status)
	}
}

func TestAlipayChannelPrepayAndWebhook(t *testing.T) {
	ch, err := NewAlipayChannel(AlipayConfig{AppID: "ali-app", MockMode: true})
	if err != nil {
		t.Fatal(err)
	}
	pp, err := ch.CreatePrepay(context.Background(), PrepayRequest{OrderID: "ali1", AmountFen: 1234})
	if err != nil {
		t.Fatal(err)
	}
	if pp.PrepayID == "" {
		t.Fatal("prepay empty")
	}
	body, _ := json.Marshal(map[string]any{
		"out_trade_no": "ali1",
		"trade_no":     "alichain_1",
		"trade_status": "TRADE_SUCCESS",
		"total_amount": "12.34",
	})
	tsv := ts()
	nonce := "alin1"
	sig := alipayMockSign("ali-app", body, tsv, nonce)
	headers := map[string]string{
		"Alipay-Timestamp": tsv,
		"Alipay-Nonce":     nonce,
		"Alipay-Sign":      sig,
	}
	ev, err := ch.VerifyWebhook(context.Background(), body, headers)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Status != "paid" {
		t.Fatalf("want paid got %s", ev.Status)
	}
	if ev.AmountFen != 1234 {
		t.Fatalf("want amount 1234 got %d", ev.AmountFen)
	}
}

func TestAppleIAPChannelPrepayAndWebhook(t *testing.T) {
	ch, err := NewAppleIAPChannel(AppleIAPConfig{BundleID: "live.yunmao.app", SharedSecret: "iap-secret", MockMode: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ch.CreatePrepay(context.Background(), PrepayRequest{OrderID: "iap1", AmountFen: 600,
		Extra: map[string]string{"apple_product_id": "yunmao.feed.30g"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"order_id":                "iap1",
		"original_transaction_id": "iaporig_1",
		"notification_type":       "ONE_TIME_PURCHASE",
		"price_fen":               600,
		"bundle_id":               "live.yunmao.app",
	})
	tsv := ts()
	nonce := "iapn1"
	sig := appleMockSign("iap-secret", body, tsv, nonce)
	headers := map[string]string{
		"X-Apple-Timestamp": tsv,
		"X-Apple-Nonce":     nonce,
		"X-Apple-Sig":       sig,
	}
	ev, err := ch.VerifyWebhook(context.Background(), body, headers)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Status != "paid" {
		t.Fatalf("want paid got %s", ev.Status)
	}
}

func TestAppleIAPBundleIDMismatch(t *testing.T) {
	ch, _ := NewAppleIAPChannel(AppleIAPConfig{BundleID: "live.yunmao.app", SharedSecret: "iap-secret", MockMode: true})
	body, _ := json.Marshal(map[string]any{
		"order_id":  "iap1",
		"bundle_id": "evil.app",
	})
	tsv := ts()
	nonce := "iapn-bad"
	sig := appleMockSign("iap-secret", body, tsv, nonce)
	headers := map[string]string{
		"X-Apple-Timestamp": tsv,
		"X-Apple-Nonce":     nonce,
		"X-Apple-Sig":       sig,
	}
	_, err := ch.VerifyWebhook(context.Background(), body, headers)
	if err == nil {
		t.Fatal("want bundle_id mismatch")
	}
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()
	mock := NewMockChannel(MockConfig{})
	reg.Register(mock)
	got, err := reg.Get(ChannelMock)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name() != ChannelMock {
		t.Fatal("name mismatch")
	}
	if _, err := reg.Get(ChannelAlipay); err == nil {
		t.Fatal("want missing channel error")
	}
}
