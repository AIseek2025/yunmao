// Package keyprovider 提供 KeyProvider 的远程后端：Vault Transit、AWS KMS。
//
// 设计目标：
//
//   - 把所有 KMS 相关 HTTP / SDK 调用集中在本包，让 authjwt 主包保持零外部依赖；
//   - 暴露 NewVaultTransit / NewAwsKms 两个工厂，输入连接参数返回符合 authjwt.KeyProvider 接口的实例；
//   - 把 Sign 流程封装为自定义 jwt.SigningMethod（VaultSigningMethod），让 authjwt.Signer 透明使用。
//
// 与 ADR-0015 / ADR-0017（设备身份 + RS256 KMS）对齐。本文件实现 Vault Transit。
package keyprovider

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
)

// VaultConfig Vault Transit 后端配置。
//
// 支持 token + approle 两种鉴权（approle = roleID + secretID 调 /v1/auth/approle/login 取 token）。
type VaultConfig struct {
	// Addr Vault 地址（http://host:8200）。
	Addr string
	// Token 直接 token 鉴权（dev / CI 常用）；非空则跳过 approle。
	Token string
	// AppRoleID / AppRoleSecret approle 凭证。
	AppRoleID     string
	AppRoleSecret string
	// MountPath Transit 引擎挂载路径，默认 "transit"。
	MountPath string
	// KeyName Transit 中的 key 名称（一个服务一把）。
	KeyName string
	// HTTPClient 测试可注入。
	HTTPClient *http.Client
	// RefreshInterval 拉公钥的轮询周期；0 = 不轮询。
	RefreshInterval time.Duration
}

// VaultTransit 实现 authjwt.KeyProvider，签名委托给 Vault Transit `sign` 端点。
type VaultTransit struct {
	cfg    VaultConfig
	cli    *http.Client
	mu     sync.RWMutex
	token  string
	pubs   map[string]*rsa.PublicKey // version → pub
	active string                    // 当前 active kid（kid = transit:<keyname>:v<n>）
}

// NewVaultTransit 构造；调用 Refresh 一次拉取公钥。Approle 鉴权由 ensureToken 完成。
func NewVaultTransit(ctx context.Context, cfg VaultConfig) (*VaultTransit, error) {
	if cfg.Addr == "" || cfg.KeyName == "" {
		return nil, errors.New("vault: addr/keyname required")
	}
	if cfg.MountPath == "" {
		cfg.MountPath = "transit"
	}
	cli := cfg.HTTPClient
	if cli == nil {
		cli = &http.Client{Timeout: 10 * time.Second}
	}
	v := &VaultTransit{cfg: cfg, cli: cli, pubs: map[string]*rsa.PublicKey{}}
	if err := v.ensureToken(ctx); err != nil {
		return nil, err
	}
	if err := v.Refresh(ctx); err != nil {
		return nil, err
	}
	return v, nil
}

// Active 返回 active SigningKey；Material 是 vaultSigningRef sentinel，由 VaultSigningMethod 处理。
func (v *VaultTransit) Active() (*authjwt.SigningKey, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.active == "" {
		return nil, errors.New("vault: no active key loaded")
	}
	return &authjwt.SigningKey{
		Kid: v.active,
		Alg: authjwt.AlgRS256,
		// 用 *VaultTransit 自身作为 Material：VaultSigningMethod.Sign 会按类型断言并调用 SignDigest。
		Material: &VaultSigningRef{Transit: v, Kid: v.active},
	}, nil
}

// PublicJWKS 输出当前 active + 历史版本（key rotate latest_version 暴露最近 N 个）。
func (v *VaultTransit) PublicJWKS() map[string]any {
	v.mu.RLock()
	defer v.mu.RUnlock()
	keys := make([]map[string]any, 0, len(v.pubs))
	for kid, pub := range v.pubs {
		keys = append(keys, map[string]any{
			"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
			"n": base64URLBigEnd(pub.N.Bytes()), "e": base64URLBigEnd(intToBytes(pub.E)),
		})
	}
	return map[string]any{"keys": keys}
}

// VerifyingByKid 取缓存公钥。
func (v *VaultTransit) VerifyingByKid(kid string) (*authjwt.VerifyingKey, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if kid == "" {
		kid = v.active
	}
	pub, ok := v.pubs[kid]
	if !ok {
		return nil, fmt.Errorf("vault: unknown kid %q", kid)
	}
	return &authjwt.VerifyingKey{Kid: kid, Alg: authjwt.AlgRS256, Material: pub}, nil
}

// SignDigest 调用 Vault Transit /v1/<mount>/sign/<key>，输入是 SHA-256 摘要（pre-hashed）。
func (v *VaultTransit) SignDigest(ctx context.Context, digest []byte) ([]byte, error) {
	v.mu.RLock()
	cfg := v.cfg
	token := v.token
	v.mu.RUnlock()
	if token == "" {
		return nil, errors.New("vault: not authenticated")
	}
	url := fmt.Sprintf("%s/v1/%s/sign/%s/sha2-256",
		strings.TrimRight(cfg.Addr, "/"), cfg.MountPath, cfg.KeyName)
	body := map[string]any{
		"input":              base64.StdEncoding.EncodeToString(digest),
		"prehashed":          true,
		"signature_algorithm": "pkcs1v15",
	}
	bb, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bb))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vault sign: status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Data struct {
			Signature string `json:"signature"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	// Vault 返回 "vault:v1:base64sig" 格式，剥前缀。
	parts := strings.SplitN(out.Data.Signature, ":", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("vault: unexpected signature format %q", out.Data.Signature)
	}
	return base64.StdEncoding.DecodeString(parts[2])
}

// Refresh 从 Vault 拉公钥（包含历史版本），刷新内部缓存。
func (v *VaultTransit) Refresh(ctx context.Context) error {
	url := fmt.Sprintf("%s/v1/%s/keys/%s",
		strings.TrimRight(v.cfg.Addr, "/"), v.cfg.MountPath, v.cfg.KeyName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", v.token)
	resp, err := v.cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault read key: status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Data struct {
			LatestVersion int                       `json:"latest_version"`
			Keys          map[string]map[string]any `json:"keys"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	pubs := map[string]*rsa.PublicKey{}
	for ver, meta := range out.Data.Keys {
		pemStr, _ := meta["public_key"].(string)
		if pemStr == "" {
			continue
		}
		pub, err := parsePublicPEM([]byte(pemStr))
		if err != nil {
			continue
		}
		kid := fmt.Sprintf("transit:%s:v%s", v.cfg.KeyName, ver)
		pubs[kid] = pub
	}
	v.mu.Lock()
	v.pubs = pubs
	v.active = fmt.Sprintf("transit:%s:v%d", v.cfg.KeyName, out.Data.LatestVersion)
	v.mu.Unlock()
	return nil
}

func (v *VaultTransit) ensureToken(ctx context.Context) error {
	if v.cfg.Token != "" {
		v.mu.Lock()
		v.token = v.cfg.Token
		v.mu.Unlock()
		return nil
	}
	if v.cfg.AppRoleID == "" || v.cfg.AppRoleSecret == "" {
		return errors.New("vault: token or approle credentials required")
	}
	url := strings.TrimRight(v.cfg.Addr, "/") + "/v1/auth/approle/login"
	body, _ := json.Marshal(map[string]string{
		"role_id":   v.cfg.AppRoleID,
		"secret_id": v.cfg.AppRoleSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault approle: status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if out.Auth.ClientToken == "" {
		return errors.New("vault: empty token from approle")
	}
	v.mu.Lock()
	v.token = out.Auth.ClientToken
	v.mu.Unlock()
	return nil
}

// Health 返回 active/retiring 信息与最近 refresh 时间，供 /internal/keys/health。
func (v *VaultTransit) Health() map[string]any {
	v.mu.RLock()
	defer v.mu.RUnlock()
	kids := make([]string, 0, len(v.pubs))
	for k := range v.pubs {
		kids = append(kids, k)
	}
	return map[string]any{
		"backend":   "vault",
		"active":    v.active,
		"kids":      kids,
		"vault_url": redactURL(v.cfg.Addr),
	}
}

// VaultSigningRef 是 Active() 返回的 Material；自定义 jwt.SigningMethod 通过断言取出实例。
type VaultSigningRef struct {
	Transit *VaultTransit
	Kid     string
}

// SignSHA256Digest 提供 jwt SigningMethod 的内部 hook。
func (r *VaultSigningRef) SignSHA256Digest(ctx context.Context, digest []byte) ([]byte, error) {
	return r.Transit.SignDigest(ctx, digest)
}

// ---------- helpers ----------

func parsePublicPEM(b []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("not a PEM block")
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rk, ok := k.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not RSA public key")
	}
	return rk, nil
}

func base64URLBigEnd(b []byte) string {
	out := base64.RawURLEncoding.EncodeToString(b)
	return out
}

func intToBytes(e int) []byte {
	b := []byte{0, 0, 0, 0}
	for i := 3; i >= 0; i-- {
		b[i] = byte(e & 0xff)
		e >>= 8
	}
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return b
}

func redactURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	if u.User != nil {
		u.User = url.User("***")
	}
	return u.String()
}

// SHA256 helper 给上层 sign 路径使用。
func SHA256(b []byte) []byte { h := sha256.Sum256(b); return h[:] }
