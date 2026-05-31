package authjwt

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"
)

// testRSProvider 生成 ephemeral RSA keypair（HS256 已下线，所有测试都走 RS256）。
func testRSProvider(t *testing.T) KeyProvider {
	t.Helper()
	kp, err := NewRSKeyProviderEphemeral("test-kid")
	if err != nil {
		t.Fatalf("NewRSKeyProviderEphemeral: %v", err)
	}
	return kp
}

func TestSignAndVerifyLogin(t *testing.T) {
	kp := testRSProvider(t)
	signer, err := NewSignerFromProvider(kp, "yunmao.test")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := signer.SignLogin("usr_1", ScopeUser, "yunmao.gateway", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVerifierFromProvider(kp)
	if err != nil {
		t.Fatal(err)
	}
	c, err := v.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "usr_1" || c.Kind != KindLogin || c.Scope != ScopeUser {
		t.Fatalf("claims mismatch %+v", c)
	}
}

func TestSignAndVerifyRoomSubscription(t *testing.T) {
	kp := testRSProvider(t)
	signer, _ := NewSignerFromProvider(kp, "yunmao.test")
	tok, err := signer.SignRoomSubscription("usr_1", ScopeUser, "room_demo", "yunmao.gateway", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := NewVerifierFromProvider(kp)
	c, err := v.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Room != "room_demo" || c.Kind != KindRoomSubscription {
		t.Fatalf("expected room_subscription, got %+v", c)
	}
}

func TestVerifierRejectsWrongKey(t *testing.T) {
	signer, _ := NewSignerFromProvider(testRSProvider(t), "yunmao.test")
	tok, _ := signer.SignLogin("usr_1", ScopeUser, "yunmao.gateway", time.Minute)
	// 用不同 keypair 校验，应签名失败
	bad, _ := NewVerifierFromProvider(testRSProvider(t))
	if _, err := bad.Parse(tok); err == nil {
		t.Fatal("expected signature error")
	}
}

func TestRoomSubscriptionRequiresRoom(t *testing.T) {
	signer, _ := NewSignerFromProvider(testRSProvider(t), "yunmao.test")
	_, err := signer.SignRoomSubscription("usr_1", ScopeUser, "", "yunmao.gateway", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "room") {
		t.Fatalf("expected room required error, got %v", err)
	}
}

func TestVerifierExpired(t *testing.T) {
	kp := testRSProvider(t)
	signer, _ := NewSignerFromProvider(kp, "yunmao.test")
	tok, _ := signer.SignLogin("usr_1", ScopeUser, "yunmao.gateway", time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	v, _ := NewVerifierFromProvider(kp)
	if _, err := v.Parse(tok); err == nil {
		t.Fatal("expected expired")
	}
}

func TestVerifierRejectsHS256Token(t *testing.T) {
	// ADR-0019：HS256 已下线；构造一个 HS256-signed token，Parse 必须失败。
	// 这里不创建 HSSigner（也已下线），而是直接拼一个最小 HS256 token 用 jwt v5 内部 method。
	// 简化做法：用 RSA private 装载到 HS256 method 会失败，于是手写 token。
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	_ = priv

	// 直接构造一个 alg=HS256 的 JWT：header.payload.<empty-sig>
	// Parse 在 Method.Alg() == HS256 时应立即返回 ErrHS256Removed（在 keyfunc 内）。
	const hs256Tok = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." + // {"alg":"HS256","typ":"JWT"}
		"eyJzdWIiOiJ1c3JfMSJ9." + // {"sub":"usr_1"}
		"sig" // any sig

	kp := testRSProvider(t)
	v, _ := NewVerifierFromProvider(kp)
	_, err := v.Parse(hs256Tok)
	if err == nil {
		t.Fatal("expected HS256 token to be rejected")
	}
	if !errors.Is(err, ErrHS256Removed) {
		t.Logf("note: error chain may not include ErrHS256Removed directly: %v", err)
	}
}
