package keyprovider

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
)

// 起一个 fake Vault：
//   - POST /v1/auth/approle/login → 返回 token
//   - GET  /v1/transit/keys/<name> → 返回公钥列表 + latest_version
//   - POST /v1/transit/sign/<name>/sha2-256 → 用本地 rsa private 真签
func fakeVault(t *testing.T, priv *rsa.PrivateKey, keyName string, latest int) *httptest.Server {
	t.Helper()
	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPem := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth": map[string]any{"client_token": "test-token"},
		})
	})
	mux.HandleFunc("/v1/transit/keys/"+keyName, func(w http.ResponseWriter, r *http.Request) {
		keys := map[string]map[string]any{}
		for v := 1; v <= latest; v++ {
			keys[itoa(v)] = map[string]any{
				"public_key":   string(pubPem),
				"creation_time": time.Now().Format(time.RFC3339),
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"latest_version": latest,
				"keys":           keys,
			},
		})
	})
	mux.HandleFunc("/v1/transit/sign/"+keyName+"/sha2-256", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input              string `json:"input"`
			Prehashed          bool   `json:"prehashed"`
			SignatureAlgorithm string `json:"signature_algorithm"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		digest, err := base64.StdEncoding.DecodeString(body.Input)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		sig, err := signSHA256(priv, digest)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"signature": "vault:v1:" + base64.StdEncoding.EncodeToString(sig),
			},
		})
	})
	return httptest.NewServer(mux)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	out := []byte{}
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	if neg {
		out = append([]byte{'-'}, out...)
	}
	return string(out)
}

func signSHA256(priv *rsa.PrivateKey, digest []byte) ([]byte, error) {
	return rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest)
}

func TestVaultTransit_SignAndVerify(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := fakeVault(t, priv, "user-svc", 2)
	defer srv.Close()

	ctx := context.Background()
	v, err := NewVaultTransit(ctx, VaultConfig{
		Addr:    srv.URL,
		Token:   "dev-token",
		KeyName: "user-svc",
	})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authjwt.NewSignerFromProvider(v, "yunmao.user-svc")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := signer.SignLogin("usr_test", authjwt.ScopeUser, "yunmao.gateway", 30*time.Second)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !strings.HasPrefix(tok, "eyJ") {
		t.Fatalf("not JWT: %q", tok)
	}
	verifier, err := authjwt.NewVerifierFromProvider(v)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifier.Parse(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "usr_test" {
		t.Fatalf("subject mismatch: %s", claims.Subject)
	}
	// 验证 SHA256 helper
	_ = sha256.Sum256(nil)
}
