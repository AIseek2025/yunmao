package keyprovider

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
)

type mockKmsSigner struct {
	priv *rsa.PrivateKey
	kid  string
}

func (m *mockKmsSigner) SignDigest(ctx context.Context, digest []byte) ([]byte, error) {
	return rsa.SignPKCS1v15(rand.Reader, m.priv, crypto.SHA256, digest)
}

func (m *mockKmsSigner) PublicKey(ctx context.Context) (*rsa.PublicKey, string, error) {
	return &m.priv.PublicKey, m.kid, nil
}

func TestAwsKms_SignAndVerify(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	mock := &mockKmsSigner{priv: priv, kid: "awskms:arn:aws:kms:us-east-1:000000000000:key/abc"}
	a, err := NewAwsKms(context.Background(), mock)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authjwt.NewSignerFromProvider(a, "yunmao.device-svc")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := signer.SignLogin("dev_test", authjwt.ScopeUser, "yunmao.emqx", 30*time.Second)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !strings.HasPrefix(tok, "eyJ") {
		t.Fatalf("not JWT: %q", tok)
	}
	verifier, err := authjwt.NewVerifierFromProvider(a)
	if err != nil {
		t.Fatal(err)
	}
	c, err := verifier.Parse(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Subject != "dev_test" {
		t.Fatalf("subject mismatch: %s", c.Subject)
	}
	jwks := a.PublicJWKS()
	keys, _ := jwks["keys"].([]map[string]any)
	if len(keys) != 1 {
		t.Fatalf("expect 1 key in JWKS, got %d", len(keys))
	}
}
