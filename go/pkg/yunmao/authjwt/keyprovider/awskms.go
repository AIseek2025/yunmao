package keyprovider

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"

	"yunmao.live/pkg/yunmao/authjwt"
)

// AwsKmsSigner 与 pkg/yunmao/kms.AwsKmsSigner 同义；放在本子包是为了让
// authjwt 不依赖 kms 包，同时让本子包不强依赖 aws-sdk-go-v2。
//
// 调用者负责用 aws-sdk-go-v2/service/kms 实现该接口，参考 awskms_real.go。
type AwsKmsSigner interface {
	// SignDigest 输入 SHA-256 摘要，返回 RSASSA_PKCS1_V1_5_SHA_256 签名（DER 解码后的原始字节）。
	SignDigest(ctx context.Context, digest []byte) ([]byte, error)
	// PublicKey 返回 (*rsa.PublicKey, kid)；kid 一般是 AWS KMS 的 KeyId 或 Arn。
	PublicKey(ctx context.Context) (*rsa.PublicKey, string, error)
}

// AwsKms 实现 authjwt.KeyProvider，签名委托给 AwsKmsSigner。
type AwsKms struct {
	signer AwsKmsSigner
	mu     sync.RWMutex
	pub    *rsa.PublicKey
	kid    string
}

// NewAwsKms 构造；Bootstrap 拉取公钥。
func NewAwsKms(ctx context.Context, signer AwsKmsSigner) (*AwsKms, error) {
	if signer == nil {
		return nil, errors.New("awskms: signer required")
	}
	a := &AwsKms{signer: signer}
	if err := a.Bootstrap(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// Bootstrap 拉公钥；可重复调用以滚动更新。
func (a *AwsKms) Bootstrap(ctx context.Context) error {
	pub, kid, err := a.signer.PublicKey(ctx)
	if err != nil {
		return err
	}
	if kid == "" {
		kid = "awskms:active"
	}
	a.mu.Lock()
	a.pub = pub
	a.kid = kid
	a.mu.Unlock()
	return nil
}

// Active 返回 SigningKey；Material 是 AwsSigningRef，jwt.SigningMethod 走 SignDigest。
func (a *AwsKms) Active() (*authjwt.SigningKey, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.pub == nil {
		return nil, errors.New("awskms: public key not bootstrapped")
	}
	return &authjwt.SigningKey{
		Kid:      a.kid,
		Alg:      authjwt.AlgRS256,
		Material: &AwsSigningRef{Provider: a, Kid: a.kid},
	}, nil
}

// PublicJWKS 单 key JWKS。
func (a *AwsKms) PublicJWKS() map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.pub == nil {
		return map[string]any{"keys": []any{}}
	}
	return map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": a.kid,
		"n": base64URLBigEnd(a.pub.N.Bytes()), "e": base64URLBigEnd(intToBytes(a.pub.E)),
	}}}
}

// VerifyingByKid 当前实现支持单 kid（与 KMS Sign API 对齐）。
func (a *AwsKms) VerifyingByKid(kid string) (*authjwt.VerifyingKey, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.pub == nil {
		return nil, errors.New("awskms: public key not bootstrapped")
	}
	if kid != "" && kid != a.kid {
		return nil, fmt.Errorf("awskms: unknown kid %q (expected %q)", kid, a.kid)
	}
	return &authjwt.VerifyingKey{Kid: a.kid, Alg: authjwt.AlgRS256, Material: a.pub}, nil
}

// Health 健康端点输出。
func (a *AwsKms) Health() map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return map[string]any{
		"backend": "awskms",
		"active":  a.kid,
		"loaded":  a.pub != nil,
	}
}

// AwsSigningRef Material；jwt.SigningMethod 用 SignSHA256Digest 走 KMS Sign。
type AwsSigningRef struct {
	Provider *AwsKms
	Kid      string
}

// SignSHA256Digest 提供 jwt SigningMethod 的内部 hook。
func (r *AwsSigningRef) SignSHA256Digest(ctx context.Context, digest []byte) ([]byte, error) {
	return r.Provider.signer.SignDigest(ctx, digest)
}

// ParsePKIXPublicKey 提供给 aws-sdk-go-v2 adapter 解析 GetPublicKey 返回的 DER。
func ParsePKIXPublicKey(der []byte) (*rsa.PublicKey, error) {
	k, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	rk, ok := k.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not RSA public key")
	}
	return rk, nil
}
