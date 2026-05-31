package authjwt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"
)

// Algorithm 当前支持 HS256 与 RS256；切换到云 KMS 后扩展更多。
type Algorithm string

const (
	AlgHS256 Algorithm = "HS256"
	AlgRS256 Algorithm = "RS256"
)

// SigningKey 是签发方一次签发使用的密钥句柄；包含其 KID 与算法。
type SigningKey struct {
	Kid string
	Alg Algorithm
	// HS：HMAC secret；RS：*rsa.PrivateKey
	Material any
}

// VerifyingKey 公钥（HS 模式下回退到 secret；RS 模式下是 *rsa.PublicKey）。
type VerifyingKey struct {
	Kid string
	Alg Algorithm
	// HS：HMAC secret；RS：*rsa.PublicKey
	Material any
}

// KeyProvider 是 user-svc / room-svc / device-svc 共享的密钥访问层。
//
// 设计目标：
//   - 本地 dev/PoC：从环境变量 / PEM 文件加载。
//   - 生产：HashiCorp Vault / 云 KMS（trait 占位，TODO 写在 NewVaultKeyProvider 等）。
//
// 校验侧（gateway）通常持有 JWKSClient 而不是 KeyProvider，但保留这条接口以便
// 服务可同时签发与自校验。
type KeyProvider interface {
	// Active 当前默认签发密钥。
	Active() (*SigningKey, error)
	// PublicJWKS 当前公开的所有公钥（JWKS 输出）；HS 模式只返回占位。
	PublicJWKS() map[string]any
	// VerifyingByKid 根据 token 中 kid 取出校验密钥。
	VerifyingByKid(kid string) (*VerifyingKey, error)
}

// ------- HS256 路径已下线（ADR-0019，第七轮） -------

// NewHSKeyProvider 历史入口；ADR-0019 第七轮起永远返回 [`ErrHS256Removed`]。
//
// 保留函数签名只为避免外部 callers 编译断裂；调用方应迁移到
// [`NewRSKeyProviderFromPEM`] / [`LoadOrCreateRSKeyProvider`] / KMS。
func NewHSKeyProvider(_ string, _ []byte) (KeyProvider, error) {
	return nil, EnsureHS256Allowed()
}

// ------- RS256 Provider（PEM 文件 / Env） -------

type rsProvider struct {
	mu    sync.RWMutex
	kid   string
	priv  *rsa.PrivateKey
	pubs  map[string]*rsa.PublicKey
	prims []string
}

// NewRSKeyProviderFromPEM 从 PEM 字节加载私钥。可选 extraPublics 注册旧 kid（轮换期）。
func NewRSKeyProviderFromPEM(kid string, privPEM []byte, extraPublics map[string]*rsa.PublicKey) (KeyProvider, error) {
	if kid == "" {
		return nil, errors.New("authjwt: kid required for RS256")
	}
	priv, err := parseRSAPrivateKey(privPEM)
	if err != nil {
		return nil, err
	}
	pubs := map[string]*rsa.PublicKey{kid: &priv.PublicKey}
	for k, v := range extraPublics {
		if k != "" && v != nil {
			pubs[k] = v
		}
	}
	prims := make([]string, 0, len(pubs))
	for k := range pubs {
		prims = append(prims, k)
	}
	return &rsProvider{kid: kid, priv: priv, pubs: pubs, prims: prims}, nil
}

// NewRSKeyProviderFromFiles 从文件路径加载（dev 常用）。
func NewRSKeyProviderFromFiles(kid, privPath string) (KeyProvider, error) {
	b, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("authjwt: read RSA private key %s: %w", privPath, err)
	}
	return NewRSKeyProviderFromPEM(kid, b, nil)
}

// NewRSKeyProviderEphemeral dev 模式下生成临时 keypair；重启后失效，仅供 PoC。
func NewRSKeyProviderEphemeral(kid string) (KeyProvider, error) {
	if kid == "" {
		kid = "dev-ephemeral"
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	pubs := map[string]*rsa.PublicKey{kid: &priv.PublicKey}
	return &rsProvider{kid: kid, priv: priv, pubs: pubs, prims: []string{kid}}, nil
}

func (r *rsProvider) Active() (*SigningKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return &SigningKey{Kid: r.kid, Alg: AlgRS256, Material: r.priv}, nil
}

func (r *rsProvider) PublicJWKS() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]map[string]any, 0, len(r.pubs))
	for k, p := range r.pubs {
		keys = append(keys, map[string]any{
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"kid": k,
			"n":   base64UrlBig(p.N),
			"e":   base64UrlInt(p.E),
		})
	}
	return map[string]any{"keys": keys}
}

func (r *rsProvider) VerifyingByKid(kid string) (*VerifyingKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if kid == "" {
		kid = r.kid
	}
	pub, ok := r.pubs[kid]
	if !ok {
		return nil, fmt.Errorf("authjwt: unknown kid %q", kid)
	}
	return &VerifyingKey{Kid: kid, Alg: AlgRS256, Material: pub}, nil
}

// Rotate 加入新的当前签发 key（旧 kid 仍可校验直到过期）。
func (r *rsProvider) Rotate(newKid string, newPriv *rsa.PrivateKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kid = newKid
	r.priv = newPriv
	r.pubs[newKid] = &newPriv.PublicKey
}

// ------- helpers -------

func parseRSAPrivateKey(b []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("authjwt: not a PEM block")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("authjwt: not an RSA key")
		}
		return rk, nil
	default:
		return nil, fmt.Errorf("authjwt: unsupported PEM block %s", block.Type)
	}
}

func base64UrlBig(n *big.Int) string {
	return base64URLEncode(n.Bytes())
}

func base64UrlInt(e int) string {
	bi := big.NewInt(int64(e))
	return base64URLEncode(bi.Bytes())
}

func base64URLEncode(b []byte) string {
	// 不使用 std encoding/base64 的 URL+padding，因为 JWK 要求无 padding。
	// 自实现避免引入额外依赖。
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	out := make([]byte, 0, (len(b)*4+2)/3)
	var buf uint32
	var bits int
	for _, by := range b {
		buf = (buf << 8) | uint32(by)
		bits += 8
		for bits >= 6 {
			bits -= 6
			out = append(out, alphabet[(buf>>bits)&0x3f])
		}
	}
	if bits > 0 {
		out = append(out, alphabet[(buf<<(6-bits))&0x3f])
	}
	return string(out)
}

// LoadOrCreateRSKeyProvider 是 dev 便捷工厂：
//   - 如设置 YUNMAO_JWT_RS_PRIVATE_KEY_PATH，则从文件加载；
//   - 否则生成临时 ephemeral keypair（重启后失效）。
//
// kid 默认 = "dev-{name}-1"，可通过 envKid 覆写。
func LoadOrCreateRSKeyProvider(name string, envKid string, envPath string) (KeyProvider, error) {
	kid := os.Getenv(envKid)
	if kid == "" {
		kid = fmt.Sprintf("dev-%s-%d", name, time.Now().Unix())
	}
	path := os.Getenv(envPath)
	if path != "" {
		return NewRSKeyProviderFromFiles(kid, path)
	}
	return NewRSKeyProviderEphemeral(kid)
}
