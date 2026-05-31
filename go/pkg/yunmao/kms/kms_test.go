package kms

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
)

func TestMockKmsRotation(t *testing.T) {
	ctx := context.Background()
	provider, err := NewMockKmsProvider(ctx, "test", VersionPolicy{RotateEvery: time.Hour, RetirePeriod: time.Hour}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := provider.Active()
	if err != nil {
		t.Fatal(err)
	}
	if first.Alg != authjwt.AlgRS256 {
		t.Fatalf("expected RS256, got %s", first.Alg)
	}
	jwks := provider.PublicJWKS()
	keys, ok := jwks["keys"].([]map[string]any)
	if !ok || len(keys) != 1 {
		t.Fatalf("expected 1 JWKS entry, got %v", jwks)
	}
	if _, err := provider.RotateNow(ctx); err != nil {
		t.Fatal(err)
	}
	second, _ := provider.Active()
	if second.Kid == first.Kid {
		t.Fatalf("expected new kid after rotation")
	}
	jwks2 := provider.PublicJWKS()
	keys2 := jwks2["keys"].([]map[string]any)
	if len(keys2) != 2 {
		t.Fatalf("expected 2 keys (active + retiring), got %d", len(keys2))
	}
	// 旧 kid 仍可校验
	if _, err := provider.VerifyingByKid(first.Kid); err != nil {
		t.Fatalf("retiring kid should still verify: %v", err)
	}
}

func TestMockKmsSignAndVerify(t *testing.T) {
	ctx := context.Background()
	provider, _ := NewMockKmsProvider(ctx, "test-sv", DefaultVersionPolicy(), nil)
	sk, _ := provider.Active()
	priv := sk.Material.(*rsa.PrivateKey)
	digest := sha256.Sum256([]byte("hello"))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	vk, err := provider.VerifyingByKid(sk.Kid)
	if err != nil {
		t.Fatal(err)
	}
	pub := vk.Material.(*rsa.PublicKey)
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

func TestMockKmsPersistsViaStore(t *testing.T) {
	ctx := context.Background()
	mem := &memStore{}
	p1, _ := NewMockKmsProvider(ctx, "test-p", DefaultVersionPolicy(), mem)
	sk, _ := p1.Active()
	// 用相同 store 重启
	p2, err := NewMockKmsProvider(ctx, "test-p", DefaultVersionPolicy(), mem)
	if err != nil {
		t.Fatal(err)
	}
	sk2, _ := p2.Active()
	if sk.Kid != sk2.Kid {
		t.Fatalf("expected persisted active kid, got %s vs %s", sk.Kid, sk2.Kid)
	}
}

func TestVaultKeyProviderCachedPubVerifies(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	v, err := NewVaultKeyProvider("http://localhost:8200", "tok", "yunmao-platform")
	if err != nil {
		t.Fatal(err)
	}
	v.CachePublicKey("vault:yunmao-platform-1", &priv.PublicKey)
	vk, err := v.VerifyingByKid("vault:yunmao-platform-1")
	if err != nil {
		t.Fatal(err)
	}
	if vk.Alg != authjwt.AlgRS256 {
		t.Fatalf("expected RS256")
	}
	// Active 应当报错（占位）
	if _, err := v.Active(); err == nil {
		t.Fatalf("expected not implemented")
	}
}

func TestAwsKmsBootstrapAndVerify(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer := &stubAwsSigner{pub: &priv.PublicKey, kid: "awskms:alias/yunmao"}
	provider, err := NewAwsKmsKeyProvider(signer)
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	vk, err := provider.VerifyingByKid("awskms:alias/yunmao")
	if err != nil {
		t.Fatal(err)
	}
	if vk.Material == nil {
		t.Fatal("missing public key")
	}
	if _, err := provider.Active(); err == nil {
		t.Fatal("expected Active not implemented")
	}
}

// ---------- helpers ----------

type memStore struct {
	rows map[string]KeyRecord
}

func (m *memStore) List(_ context.Context) ([]KeyRecord, error) {
	out := make([]KeyRecord, 0, len(m.rows))
	for _, r := range m.rows {
		out = append(out, r)
	}
	return out, nil
}

func (m *memStore) Save(_ context.Context, k KeyRecord) error {
	if m.rows == nil {
		m.rows = map[string]KeyRecord{}
	}
	m.rows[k.Kid] = k
	return nil
}

func (m *memStore) UpdateState(_ context.Context, kid string, state KeyState) error {
	if r, ok := m.rows[kid]; ok {
		r.State = state
		m.rows[kid] = r
	}
	return nil
}

type stubAwsSigner struct {
	pub *rsa.PublicKey
	kid string
}

func (s *stubAwsSigner) SignDigest(_ context.Context, _ []byte) ([]byte, error) {
	return nil, nil
}

func (s *stubAwsSigner) PublicKey(_ context.Context) (*rsa.PublicKey, string, error) {
	return s.pub, s.kid, nil
}
