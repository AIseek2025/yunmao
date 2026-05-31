package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"yunmao.live/services/billing-svc/internal/pay"
	"yunmao.live/services/billing-svc/internal/service"
)

func newTestHandler(t *testing.T) (http.Handler, *service.BillingService) {
	t.Helper()
	svc := service.New(nil)
	reg := pay.NewRegistry()
	reg.Register(pay.NewMockChannel(pay.MockConfig{Secret: "k"}))
	h := NewWithDeps(svc, HandlerDeps{PayRegistry: reg})
	return h, svc
}

func TestPrepayWithMockChannel(t *testing.T) {
	h, svc := newTestHandler(t)

	order, err := svc.Create(context.Background(), service.CreateInput{
		UserID: "u1", Channel: "mock", BizType: "feed_ticket", AmountCny: 5,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(`{"amount_fen":500,"subject":"feed 5g"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+order.ID+"/prepay", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Pay-Channel", "mock")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201 got %d body=%s", w.Code, w.Body.String())
	}
	var out pay.PrepayResponse
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.PrepayID == "" || out.Channel != pay.ChannelMock {
		t.Fatalf("unexpected resp: %+v", out)
	}
}

func TestMockWebhookConfirmsOrder(t *testing.T) {
	h, svc := newTestHandler(t)
	order, err := svc.Create(context.Background(), service.CreateInput{
		UserID: "u1", Channel: "mock", BizType: "feed_ticket", AmountCny: 5,
	})
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"order_id":          order.ID,
		"external_trade_no": "mock_chain_" + order.ID,
		"amount_fen":        500,
		"status":            "paid",
	})
	tsv := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "n-1"
	sig := pay.MockSign("k", body, tsv, nonce)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pay/webhook/mock", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Yunmao-Pay-Ts", tsv)
	req.Header.Set("X-Yunmao-Pay-Nonce", nonce)
	req.Header.Set("X-Yunmao-Pay-Sig", sig)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d body=%s", w.Code, w.Body.String())
	}

	// 验证订单已 paid
	got, err := svc.Get(context.Background(), order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "paid" {
		t.Fatalf("want paid got %s", got.Status)
	}

	// 重放：同 nonce 拒绝
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/pay/webhook/mock", bytes.NewReader(body))
	req2.Header.Set("X-Yunmao-Pay-Ts", tsv)
	req2.Header.Set("X-Yunmao-Pay-Nonce", nonce)
	req2.Header.Set("X-Yunmao-Pay-Sig", sig)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code == http.StatusOK {
		// 重放仍 200 也可，但 body 中应包含 replay 错误。
		if !strings.Contains(w2.Body.String(), "replay") {
			t.Fatalf("replay should be rejected, got body=%s", w2.Body.String())
		}
	}
}

func TestListChannels(t *testing.T) {
	h, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pay/channels", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "mock") {
		t.Fatalf("want mock channel listed got %s", w.Body.String())
	}
}

func TestDiagnoseCredentialsAllReady(t *testing.T) {
	svc := service.New(nil)
	reg := pay.NewRegistry()
	cr := &pay.CredentialReadiness{
		Checks: []pay.CredentialCheck{
			{Channel: pay.ChannelMock, Field: "mode", Status: pay.CredentialMock, Detail: "MockMode=true"},
			{Channel: pay.ChannelWeChat, Field: "mch_id", Status: pay.CredentialReady, Detail: "set"},
			{Channel: pay.ChannelAlipay, Field: "app_id", Status: pay.CredentialReady, Detail: "set"},
		},
	}
	h := NewWithDeps(svc, HandlerDeps{PayRegistry: reg, CredentialReadiness: cr})

	req := httptest.NewRequest(http.MethodGet, "/internal/diagnose/credentials", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if allReady, ok := out["all_ready"].(bool); !ok || !allReady {
		t.Fatalf("want all_ready=true, got %v", out["all_ready"])
	}
	checks, ok := out["checks"].([]any)
	if !ok || len(checks) != 3 {
		t.Fatalf("want 3 checks, got %v", out["checks"])
	}
}

func TestDiagnoseCredentialsMissing(t *testing.T) {
	svc := service.New(nil)
	cr := &pay.CredentialReadiness{
		Checks: []pay.CredentialCheck{
			{Channel: pay.ChannelWeChat, Field: "mch_id", Status: pay.CredentialMissing, Detail: "not set"},
			{Channel: pay.ChannelWeChat, Field: "api_v3_key", Status: pay.CredentialMissing, Detail: "not set"},
		},
	}
	h := NewWithDeps(svc, HandlerDeps{CredentialReadiness: cr})

	req := httptest.NewRequest(http.MethodGet, "/internal/diagnose/credentials", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if allReady, ok := out["all_ready"].(bool); !ok || allReady {
		t.Fatalf("want all_ready=false when credentials missing, got %v", out["all_ready"])
	}
}

func TestDiagnoseCredentialsNotMounted(t *testing.T) {
	svc := service.New(nil)
	h := NewWithDeps(svc, HandlerDeps{})

	req := httptest.NewRequest(http.MethodGet, "/internal/diagnose/credentials", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 when CredentialReadiness is nil, got %d", w.Code)
	}
}
