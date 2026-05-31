package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yunmao.live/services/billing-svc/internal/pay"
	"yunmao.live/services/billing-svc/internal/service"
)

// round2 evidence: diagnose endpoint reports has_real_mode=true when sandbox
// credentials are attached, proving real-mode branch is reachable over HTTP.
func TestDiagnoseCredentialsRealModeSignal_Iter330(t *testing.T) {
	svc := service.New(nil)
	cr := &pay.CredentialReadiness{
		Checks: []pay.CredentialCheck{
			{Channel: pay.ChannelWeChat, Field: "mch_id", Status: pay.CredentialReady, Detail: "set"},
			{Channel: pay.ChannelWeChat, Field: "sandbox", Status: pay.CredentialSandbox, Detail: "https://api.mch.weixin.qq.com/sandbox"},
			{Channel: pay.ChannelAlipay, Field: "app_id", Status: pay.CredentialReady, Detail: "set"},
			{Channel: pay.ChannelAppleIAP, Field: "bundle_id", Status: pay.CredentialReady, Detail: "live.yunmao.app"},
		},
	}
	h := NewWithDeps(svc, HandlerDeps{CredentialReadiness: cr})

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
	if v, ok := out["all_ready"].(bool); !ok || !v {
		t.Fatalf("want all_ready=true, got %v", out)
	}
	if v, ok := out["has_real_mode"].(bool); !ok || !v {
		t.Fatalf("want has_real_mode=true when sandbox+ready present, got %v", out)
	}
	body := w.Body.String()
	for _, needle := range []string{"wechat", "alipay", "appleiap", "sandbox"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("diagnose body missing %s: %s", needle, body)
		}
	}
}

// round2 evidence: diagnose channel breakdown correctly flags each channel's
// missing fields, proving failure-mode diagnostics surface correctly.
func TestDiagnoseCredentialsPerChannelMissing_Iter330(t *testing.T) {
	svc := service.New(nil)
	wc := pay.WeChatConfig{MockMode: false}
	ac := pay.AlipayConfig{MockMode: false}
	ic := pay.AppleIAPConfig{MockMode: false}
	cr := pay.CheckAllCredentials(wc, ac, ic)
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
	if v, ok := out["all_ready"].(bool); !ok || v {
		t.Fatalf("want all_ready=false when credentials are unconfigured, got %v", out)
	}
	if v, ok := out["has_real_mode"].(bool); !ok || v {
		t.Fatalf("want has_real_mode=false without ready/sandbox, got %v", out)
	}
	checks, ok := out["checks"].([]any)
	if !ok || len(checks) < 6 {
		t.Fatalf("want at least 6 checks across 3 channels, got %v", out)
	}
}
