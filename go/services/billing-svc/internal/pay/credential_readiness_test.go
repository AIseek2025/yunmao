package pay

import (
	"strings"
	"testing"
)

func TestCheckWeChatCredentialsMockMode(t *testing.T) {
	cfg := WeChatConfig{MockMode: true}
	checks := CheckWeChatCredentials(cfg)
	if len(checks) != 1 {
		t.Fatalf("expected 1 check in mock mode, got %d", len(checks))
	}
	if checks[0].Status != CredentialMock {
		t.Fatalf("expected mock status, got %s", checks[0].Status)
	}
}

func TestCheckWeChatCredentialsMissing(t *testing.T) {
	cfg := WeChatConfig{MockMode: false}
	checks := CheckWeChatCredentials(cfg)
	missingCount := 0
	for _, c := range checks {
		if c.Status == CredentialMissing {
			missingCount++
		}
	}
	if missingCount < 3 {
		t.Fatalf("expected at least 3 missing fields with empty config, got %d", missingCount)
	}
}

func TestCheckWeChatCredentialsReady(t *testing.T) {
	cfg := WeChatConfig{
		MockMode:          false,
		MchID:             "1234567890",
		APIv3Key:          "test-v3-key-32-bytes-long-pad-pad",
		SerialNo:          "SERIAL001",
		APIClientKey:      []byte("-----BEGIN RSA PRIVATE KEY-----\n-----END RSA PRIVATE KEY-----"),
		PlatformPublicKey: []byte("-----BEGIN PUBLIC KEY-----\n-----END PUBLIC KEY-----"),
	}
	checks := CheckWeChatCredentials(cfg)
	for _, c := range checks {
		if c.Status == CredentialMissing {
			t.Fatalf("unexpected missing field: %s (%s)", c.Field, c.Detail)
		}
	}
}

func TestCheckAlipayCredentialsMockMode(t *testing.T) {
	cfg := AlipayConfig{MockMode: true}
	checks := CheckAlipayCredentials(cfg)
	if len(checks) != 1 {
		t.Fatalf("expected 1 check in mock mode, got %d", len(checks))
	}
	if checks[0].Status != CredentialMock {
		t.Fatalf("expected mock status, got %s", checks[0].Status)
	}
}

func TestCheckAlipayCredentialsMissing(t *testing.T) {
	cfg := AlipayConfig{MockMode: false}
	checks := CheckAlipayCredentials(cfg)
	missingCount := 0
	for _, c := range checks {
		if c.Status == CredentialMissing {
			missingCount++
		}
	}
	if missingCount < 2 {
		t.Fatalf("expected at least 2 missing fields, got %d", missingCount)
	}
}

func TestCheckAppleIAPCredentialsMockMode(t *testing.T) {
	cfg := AppleIAPConfig{MockMode: true}
	checks := CheckAppleIAPCredentials(cfg)
	if len(checks) != 1 {
		t.Fatalf("expected 1 check in mock mode, got %d", len(checks))
	}
	if checks[0].Status != CredentialMock {
		t.Fatalf("expected mock status, got %s", checks[0].Status)
	}
}

func TestCheckAppleIAPCredentialsReady(t *testing.T) {
	cfg := AppleIAPConfig{
		MockMode:     false,
		BundleID:     "live.yunmao.app",
		AppAppleID:   "1234567890",
		TrustedRoots: [][]byte{[]byte("Apple Root CA PEM")},
	}
	checks := CheckAppleIAPCredentials(cfg)
	for _, c := range checks {
		if c.Status == CredentialMissing {
			t.Fatalf("unexpected missing field: %s (%s)", c.Field, c.Detail)
		}
	}
}

func TestCheckAllCredentialsAllMock(t *testing.T) {
	wc := WeChatConfig{MockMode: true}
	ac := AlipayConfig{MockMode: true}
	ic := AppleIAPConfig{MockMode: true}
	cr := CheckAllCredentials(wc, ac, ic)
	if !cr.AllReady() {
		t.Fatal("all-mock should be AllReady")
	}
	if cr.HasRealMode() {
		t.Fatal("all-mock should not have real mode")
	}
}

func TestCredentialReadinessSummary(t *testing.T) {
	wc := WeChatConfig{MockMode: true}
	ac := AlipayConfig{MockMode: true}
	ic := AppleIAPConfig{MockMode: true}
	cr := CheckAllCredentials(wc, ac, ic)
	summary := cr.Summary()
	if !strings.Contains(summary, "mock") {
		t.Fatalf("summary should contain 'mock', got: %s", summary)
	}
}
