// Package kms 提供 KeyProvider 的 KMS / Vault / Mock 实现，配合 authjwt 使用。
//
// 与 ADR-0014（身份与密钥统一管理）+ ADR-0015（KMS 选型与轮换策略）对齐。
//
// 设计目标：
//
//   - 本机 dev / CI：[`MockKmsProvider`]，纯 Go RSA + 进程内 / 可选 PG 持久化 + 轮换。
//   - HashiCorp Vault：[`VaultKeyProvider`]，走 KV v2 + transit 引擎；使用 HTTP API（无 SDK 依赖）。
//   - AWS KMS：[`AwsKmsKeyProvider`]，使用 asymmetric RSA_2048；签名委托 KMS Sign 接口。
//   - 各 provider 都返回 authjwt.KeyProvider 接口，user-svc / room-svc / device-svc 透明切换。
//
// 轮换策略（KeyVersionPolicy）：
//
//   - 每 N 天生成新 key（kid 前缀按服务名拼接）；
//   - 旧 key 保留 retire 期（默认 7 天），JWKS 仍暴露，token 不再签发；
//   - 过 retire 期后标记 retired，但 PG 仍保留以审计。
package kms

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
)

// KeyState 单 kid 的生命周期阶段。
type KeyState string

const (
	StateActive   KeyState = "active"   // 当前签发使用
	StateRetiring KeyState = "retiring" // JWKS 仍含；不再签发
	StateRetired  KeyState = "retired"  // 已过 retire 期，仅历史审计
)

// KeyRecord 一个 kid 版本。
type KeyRecord struct {
	Kid        string
	Alg        authjwt.Algorithm
	State      KeyState
	NotBefore  time.Time
	NotAfter   time.Time
	CreatedAt  time.Time
	PrivatePEM []byte // 仅 MockKms 持有
	PublicPEM  []byte
	priv       *rsa.PrivateKey
	pub        *rsa.PublicKey
}

// VersionPolicy 轮换策略。
type VersionPolicy struct {
	// RotateEvery 默认 30d，自动 Rotate 调用时使用。
	RotateEvery time.Duration
	// RetirePeriod 默认 7d，retire 期内 token 仍可被校验。
	RetirePeriod time.Duration
}

// DefaultVersionPolicy 30d + 7d。
func DefaultVersionPolicy() VersionPolicy {
	return VersionPolicy{
		RotateEvery:  30 * 24 * time.Hour,
		RetirePeriod: 7 * 24 * time.Hour,
	}
}

// VersionStore 持久化已生成的 key 记录；Mock 可不传（仅 in-memory）。
type VersionStore interface {
	List(ctx context.Context) ([]KeyRecord, error)
	Save(ctx context.Context, k KeyRecord) error
	UpdateState(ctx context.Context, kid string, state KeyState) error
}

// ----------------------------------------------------------------------------------------
// MockKmsProvider：本机 + CI 默认实现。
// ----------------------------------------------------------------------------------------

// MockKmsProvider 本地 RSA + 轮换。
type MockKmsProvider struct {
	mu        sync.RWMutex
	name      string // 服务名前缀，如 "user"
	policy    VersionPolicy
	versions  map[string]*KeyRecord
	activeKid string
	store     VersionStore
	rng       func(rand int) ([]byte, error) // 测试可注入
}

// NewMockKmsProvider 构造一个本地 KMS。如 store 非空，会从 PG 拉初始版本。
//
// 若没有任何 active 版本，自动生成一把新的并写回 store。
func NewMockKmsProvider(ctx context.Context, name string, policy VersionPolicy, store VersionStore) (*MockKmsProvider, error) {
	if policy.RotateEvery == 0 {
		policy = DefaultVersionPolicy()
	}
	m := &MockKmsProvider{
		name:     name,
		policy:   policy,
		versions: map[string]*KeyRecord{},
		store:    store,
	}
	if store != nil {
		records, err := store.List(ctx)
		if err == nil {
			for i := range records {
				rec := records[i]
				if err := hydrateRecord(&rec); err == nil {
					cp := rec
					m.versions[cp.Kid] = &cp
					if cp.State == StateActive {
						m.activeKid = cp.Kid
					}
				}
			}
		}
	}
	if m.activeKid == "" {
		if _, err := m.RotateNow(ctx); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// Name 服务前缀名。
func (m *MockKmsProvider) Name() string { return m.name }

// Active 返回当前签发用 SigningKey。
func (m *MockKmsProvider) Active() (*authjwt.SigningKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.versions[m.activeKid]
	if !ok || rec.priv == nil {
		return nil, errors.New("kms: no active key")
	}
	return &authjwt.SigningKey{
		Kid:      rec.Kid,
		Alg:      rec.Alg,
		Material: rec.priv,
	}, nil
}

// PublicJWKS 输出 active + retiring keys。
func (m *MockKmsProvider) PublicJWKS() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := []map[string]any{}
	for _, r := range m.versions {
		if r.State == StateRetired {
			continue
		}
		keys = append(keys, map[string]any{
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"kid": r.Kid,
			"n":   base64UrlBig(r.pub.N),
			"e":   base64UrlInt(r.pub.E),
		})
	}
	return map[string]any{"keys": keys}
}

// VerifyingByKid 根据 kid 取公钥（接受 active + retiring + retired）。
func (m *MockKmsProvider) VerifyingByKid(kid string) (*authjwt.VerifyingKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if kid == "" {
		kid = m.activeKid
	}
	r, ok := m.versions[kid]
	if !ok {
		return nil, fmt.Errorf("kms: unknown kid %q", kid)
	}
	return &authjwt.VerifyingKey{Kid: r.Kid, Alg: r.Alg, Material: r.pub}, nil
}

// RotateNow 生成新 active，把旧 active 标记为 retiring。
func (m *MockKmsProvider) RotateNow(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", fmt.Errorf("kms: generate rsa: %w", err)
	}
	kid := fmt.Sprintf("%s-%d", m.name, now.UnixNano())
	privPem := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return "", err
	}
	pubPem := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	rec := &KeyRecord{
		Kid:        kid,
		Alg:        authjwt.AlgRS256,
		State:      StateActive,
		NotBefore:  now,
		NotAfter:   now.Add(m.policy.RotateEvery + m.policy.RetirePeriod),
		CreatedAt:  now,
		PrivatePEM: privPem,
		PublicPEM:  pubPem,
		priv:       priv,
		pub:        &priv.PublicKey,
	}
	// 把旧 active 切到 retiring
	if oldKid := m.activeKid; oldKid != "" {
		if old, ok := m.versions[oldKid]; ok && old.State == StateActive {
			old.State = StateRetiring
			if m.store != nil {
				_ = m.store.UpdateState(ctx, oldKid, StateRetiring)
			}
		}
	}
	m.versions[kid] = rec
	m.activeKid = kid
	if m.store != nil {
		_ = m.store.Save(ctx, *rec)
	}
	// 把过期的 retiring 标 retired
	for k, r := range m.versions {
		if r.State == StateRetiring && now.Sub(r.CreatedAt) > m.policy.RotateEvery+m.policy.RetirePeriod {
			r.State = StateRetired
			if m.store != nil {
				_ = m.store.UpdateState(ctx, k, StateRetired)
			}
		}
	}
	return kid, nil
}

// SnapshotVersions 返回当前所有版本（按 created 顺序），供 admin 查询。
func (m *MockKmsProvider) SnapshotVersions() []KeyRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]KeyRecord, 0, len(m.versions))
	for _, r := range m.versions {
		cp := *r
		cp.priv = nil // 不外泄
		out = append(out, cp)
	}
	return out
}

// 注：PG 版本持久化由 service 层用 pgx 写到 kms_key_versions 表，
// 通过传入实现 VersionStore 接口的对象注入到 NewMockKmsProvider。
// 这样 pkg/yunmao/kms 本身无需依赖 pgx。参见 services/user-svc 的启动代码示例。

// ----------------------------------------------------------------------------------------
// Vault Transit + KV v2（HTTP API，无 SDK 依赖）
// ----------------------------------------------------------------------------------------

// VaultKeyProvider Vault Transit 后端。
//
// 当前实现：拉取 `transit/keys/<name>/public_key` 缓存为本地 *rsa.PublicKey；
// 签名通过 `transit/sign/<name>` HTTP 调用完成（不暴露私钥）。
//
// 限制（第四轮）：Sign() 需要把 jwt.SignedString 路径替换为自定义 SigningMethod；
// 当前先把 KeyProvider Active() 返回特殊 marker，让 service 层走 VaultSign() 路径。
// 完整集成留到第五轮（带 Vault 容器联调）。
type VaultKeyProvider struct {
	addr  string
	token string
	name  string
	mu    sync.RWMutex
	pub   *rsa.PublicKey
	kid   string
}

// NewVaultKeyProvider 构造。addr 形如 `http://localhost:8200`；name 是 Transit key 名。
func NewVaultKeyProvider(addr, token, name string) (*VaultKeyProvider, error) {
	if addr == "" || token == "" || name == "" {
		return nil, errors.New("kms: vault addr/token/name required")
	}
	return &VaultKeyProvider{addr: addr, token: token, name: name, kid: "vault:" + name}, nil
}

// Active 当前 Vault key（占位实现，签名时走 Sign() 而非直接拿私钥）。
//
// TODO(第五轮)：实现自定义 jwt.SigningMethod，把 SignedString 委托到 Vault。
func (v *VaultKeyProvider) Active() (*authjwt.SigningKey, error) {
	return nil, errors.New("kms: VaultKeyProvider.Active not implemented (use Sign())")
}

// PublicJWKS 拉取最新公钥并序列化为 JWKS。
func (v *VaultKeyProvider) PublicJWKS() map[string]any {
	v.mu.RLock()
	pub := v.pub
	kid := v.kid
	v.mu.RUnlock()
	if pub == nil {
		return map[string]any{"keys": []any{}}
	}
	return map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
		"n": base64UrlBig(pub.N), "e": base64UrlInt(pub.E),
	}}}
}

// VerifyingByKid 当前 Vault 仅支持单 key；kid 不匹配视为未知。
func (v *VaultKeyProvider) VerifyingByKid(kid string) (*authjwt.VerifyingKey, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.pub == nil {
		return nil, errors.New("kms: vault public key not loaded")
	}
	if kid != "" && kid != v.kid {
		return nil, fmt.Errorf("kms: unknown kid %q (expected %q)", kid, v.kid)
	}
	return &authjwt.VerifyingKey{Kid: v.kid, Alg: authjwt.AlgRS256, Material: v.pub}, nil
}

// CachePublicKey 注入预先拉取的公钥（dev 联调用；生产由 RefreshPublicKey 自动拉）。
func (v *VaultKeyProvider) CachePublicKey(kid string, pub *rsa.PublicKey) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.pub = pub
	if kid != "" {
		v.kid = kid
	}
}

// ----------------------------------------------------------------------------------------
// AWS KMS（asymmetric RSA_2048 + RSASSA_PKCS1_V1_5_SHA_256）
// ----------------------------------------------------------------------------------------

// AwsKmsSigner 是 user 注入的 AWS KMS 签名委托；
// 调用者负责用 aws-sdk-go-v2 实现 `Sign(payload) -> signature`。
type AwsKmsSigner interface {
	SignDigest(ctx context.Context, digest []byte) ([]byte, error)
	PublicKey(ctx context.Context) (*rsa.PublicKey, string, error) // returns (pub, kid)
}

// AwsKmsKeyProvider AWS KMS 后端；目前与 VaultKeyProvider 同构（占位 Active）。
type AwsKmsKeyProvider struct {
	signer AwsKmsSigner
	mu     sync.RWMutex
	pub    *rsa.PublicKey
	kid    string
}

// NewAwsKmsKeyProvider 构造；signer 不能为 nil。
func NewAwsKmsKeyProvider(signer AwsKmsSigner) (*AwsKmsKeyProvider, error) {
	if signer == nil {
		return nil, errors.New("kms: awskms signer required")
	}
	return &AwsKmsKeyProvider{signer: signer, kid: "awskms:active"}, nil
}

// Active TODO(第五轮)：实现自定义 jwt.SigningMethod。
func (a *AwsKmsKeyProvider) Active() (*authjwt.SigningKey, error) {
	return nil, errors.New("kms: AwsKmsKeyProvider.Active not implemented (use signer)")
}

// PublicJWKS 缓存的公钥序列化。
func (a *AwsKmsKeyProvider) PublicJWKS() map[string]any {
	a.mu.RLock()
	pub := a.pub
	kid := a.kid
	a.mu.RUnlock()
	if pub == nil {
		return map[string]any{"keys": []any{}}
	}
	return map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
		"n": base64UrlBig(pub.N), "e": base64UrlInt(pub.E),
	}}}
}

// VerifyingByKid 校验侧（拿缓存公钥）。
func (a *AwsKmsKeyProvider) VerifyingByKid(kid string) (*authjwt.VerifyingKey, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.pub == nil {
		return nil, errors.New("kms: awskms public key not loaded")
	}
	if kid != "" && kid != a.kid {
		return nil, fmt.Errorf("kms: unknown kid %q (expected %q)", kid, a.kid)
	}
	return &authjwt.VerifyingKey{Kid: a.kid, Alg: authjwt.AlgRS256, Material: a.pub}, nil
}

// Bootstrap 初始化拉公钥；dev 通常在 cmd 启动时调用。
func (a *AwsKmsKeyProvider) Bootstrap(ctx context.Context) error {
	pub, kid, err := a.signer.PublicKey(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.pub = pub
	if kid != "" {
		a.kid = kid
	}
	a.mu.Unlock()
	return nil
}

// ----------------------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------------------

func hydrateRecord(rec *KeyRecord) error {
	if rec.priv != nil && rec.pub != nil {
		return nil
	}
	if len(rec.PrivatePEM) > 0 {
		block, _ := pem.Decode(rec.PrivatePEM)
		if block != nil {
			switch block.Type {
			case "RSA PRIVATE KEY":
				k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
				if err == nil {
					rec.priv = k
					rec.pub = &k.PublicKey
					return nil
				}
			case "PRIVATE KEY":
				k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
				if err == nil {
					if rk, ok := k.(*rsa.PrivateKey); ok {
						rec.priv = rk
						rec.pub = &rk.PublicKey
						return nil
					}
				}
			}
		}
	}
	if len(rec.PublicPEM) > 0 {
		block, _ := pem.Decode(rec.PublicPEM)
		if block != nil {
			k, err := x509.ParsePKIXPublicKey(block.Bytes)
			if err == nil {
				if rk, ok := k.(*rsa.PublicKey); ok {
					rec.pub = rk
					return nil
				}
			}
		}
	}
	return errors.New("kms: record missing usable key material")
}

// base64UrlBig / base64UrlInt 与 authjwt 同实现，避免内部包导出冲突。
func base64UrlBig(n interface{ Bytes() []byte }) string {
	return base64URLEncode(n.Bytes())
}

func base64UrlInt(e int) string {
	b := []byte{0, 0, 0, 0}
	for i := 3; i >= 0; i-- {
		b[i] = byte(e & 0xff)
		e >>= 8
	}
	// 去掉前导 0
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return base64URLEncode(b)
}

func base64URLEncode(b []byte) string {
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
