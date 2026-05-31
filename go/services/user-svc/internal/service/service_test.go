package service

import (
	"context"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
)

// testKP 为每个测试生成一份 ephemeral RSA keypair（HS256 已下线，ADR-0019 第七轮）。
func testKP(t *testing.T) authjwt.KeyProvider {
	t.Helper()
	kp, err := authjwt.NewRSKeyProviderEphemeral("user-svc-test")
	if err != nil {
		t.Fatal(err)
	}
	return kp
}

func newSvc(t *testing.T) (*UserService, authjwt.KeyProvider) {
	t.Helper()
	kp := testKP(t)
	signer, err := authjwt.NewSignerFromProvider(kp, "yunmao.user-svc")
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{Signer: signer, TokenTTL: time.Hour}), kp
}

func TestSmsLoginHappyPath(t *testing.T) {
	s, kp := newSvc(t)
	now := time.Unix(1700000000, 0)
	s.now = func() time.Time { return now }

	id, code, _, err := s.StartSmsLogin(context.Background(), "+8613800000000")
	if err != nil {
		t.Fatal(err)
	}
	user, token, err := s.CompleteSmsLogin(context.Background(), id, code)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == "" || token == "" {
		t.Fatal("missing user/token")
	}

	// token 必须可被同 KeyProvider 的 verifier 解析
	v, _ := authjwt.NewVerifierFromProvider(kp)
	cl, err := v.Parse(token)
	if err != nil {
		t.Fatalf("parse jwt: %v", err)
	}
	if cl.Subject != user.ID {
		t.Fatalf("subject mismatch %s", cl.Subject)
	}

	id2, code2, _, _ := s.StartSmsLogin(context.Background(), "+8613800000000")
	user2, _, _ := s.CompleteSmsLogin(context.Background(), id2, code2)
	if user2.ID != user.ID {
		t.Fatalf("expected same user, got %s vs %s", user2.ID, user.ID)
	}
}

func TestSmsLoginRejectsBadCode(t *testing.T) {
	s, _ := newSvc(t)
	id, _, _, _ := s.StartSmsLogin(context.Background(), "+86138")
	if _, _, err := s.CompleteSmsLogin(context.Background(), id, "000000"); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestSmsLoginExpires(t *testing.T) {
	s, _ := newSvc(t)
	now := time.Unix(1700000000, 0)
	s.now = func() time.Time { return now }
	id, code, _, _ := s.StartSmsLogin(context.Background(), "+86138")
	now = now.Add(10 * time.Minute)
	if _, _, err := s.CompleteSmsLogin(context.Background(), id, code); err == nil {
		t.Fatal("expected expired error")
	}
}

func TestDevLoginIssuesToken(t *testing.T) {
	s, kp := newSvc(t)
	user, tok, err := s.DevLogin(context.Background(), LoginInput{PhoneE164: "+861380000000"})
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == "" || tok == "" {
		t.Fatal("missing user/token")
	}
	v, _ := authjwt.NewVerifierFromProvider(kp)
	cl, err := v.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if cl.Subject != user.ID {
		t.Fatalf("subject mismatch")
	}
}
