// Package authjwt 提供 yunmao 平台的 JWT 签发 / 校验 / JWKS 暴露。
//
// 现状（第三轮）：
//
//   - 引入 `KeyProvider` 抽象。HS256（dev 兼容）+ RS256（默认）+ KMS 占位。
//   - user-svc / room-svc 共享一个 KeyProvider；gateway 通过 `JWKSClient` 拉公开 JWKS。
//   - 网关用 `Verifier` 拿到 kid → 公钥并按 alg 校验签名。
//
// Claims 形状不变：
//
//	{
//	  "sub":   "usr_01H...",
//	  "scope": "user|guest|admin",
//	  "kind":  "login|room_subscription",
//	  "room":  "room_demo",      // kind=room_subscription 时填
//	  "iat":   1700000000,
//	  "exp":   1700003600,
//	  "iss":   "yunmao.user-svc",
//	  "aud":   "yunmao.gateway"
//	}
package authjwt

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Kind 区分 token 用途。
type Kind string

const (
	KindLogin            Kind = "login"
	KindRoomSubscription Kind = "room_subscription"
)

// Scope 用户权限范围（与 04 章对齐）。
type Scope string

const (
	ScopeUser  Scope = "user"
	ScopeGuest Scope = "guest"
	ScopeAdmin Scope = "admin"
)

// Claims 平台 JWT 自定义 claims。
type Claims struct {
	jwt.RegisteredClaims
	Scope Scope  `json:"scope,omitempty"`
	Kind  Kind   `json:"kind"`
	Room  string `json:"room,omitempty"`
}

// Signer 签发者（user-svc / room-svc 使用）。
type Signer struct {
	kp     KeyProvider
	issuer string
	now    func() time.Time
}

// NewSigner 历史入口；ADR-0019 第七轮起永远返回 [`ErrHS256Removed`]。
//
// 保留函数签名只为避免编译断裂；新代码请用 [`NewSignerFromProvider`]。
func NewSigner(_ []byte, _ string) (*Signer, error) {
	return nil, EnsureHS256Allowed()
}

// NewSignerFromProvider 使用 KeyProvider 构造签发者；推荐路径。
func NewSignerFromProvider(kp KeyProvider, issuer string) (*Signer, error) {
	if kp == nil {
		return nil, errors.New("authjwt: nil KeyProvider")
	}
	if issuer == "" {
		return nil, errors.New("authjwt: issuer required")
	}
	return &Signer{kp: kp, issuer: issuer, now: time.Now}, nil
}

// JWKS 暴露公开材料。HS256 模式只声明 kid + alg；RS256 模式返回真实 n/e。
func (s *Signer) JWKS() map[string]any { return s.kp.PublicJWKS() }

// KeyProvider 暴露内部 KeyProvider（供 device-svc 的 MQTT 凭证签发器复用）。
func (s *Signer) KeyProvider() KeyProvider { return s.kp }

func (s *Signer) signClaims(c Claims) (string, error) {
	sk, err := s.kp.Active()
	if err != nil {
		return "", err
	}
	var method jwt.SigningMethod
	switch sk.Alg {
	case AlgHS256:
		// ADR-0019：HS256 路径已下线；如果 KeyProvider 仍返回 HS256 SigningKey 视为配置错误。
		return "", ErrHS256Removed
	case AlgRS256:
		if _, isRemote := sk.Material.(RemoteSigner); isRemote {
			method = remoteRS256
		} else {
			method = jwt.SigningMethodRS256
		}
	default:
		return "", fmt.Errorf("authjwt: unsupported alg %s", sk.Alg)
	}
	tok := jwt.NewWithClaims(method, c)
	tok.Header["kid"] = sk.Kid
	return tok.SignedString(sk.Material)
}

// SignLogin 签发登录 token；audience 一般是 "yunmao.gateway"。
func (s *Signer) SignLogin(subject string, scope Scope, audience string, ttl time.Duration) (string, error) {
	now := s.now().UTC()
	c := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Audience:  jwt.ClaimStrings{audience},
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Scope: scope,
		Kind:  KindLogin,
	}
	return s.signClaims(c)
}

// SignRoomSubscription 签发短期房间订阅 token；TTL 建议 5–15 分钟。
func (s *Signer) SignRoomSubscription(subject string, scope Scope, room string, audience string, ttl time.Duration) (string, error) {
	if room == "" {
		return "", errors.New("authjwt: room required for room_subscription token")
	}
	now := s.now().UTC()
	c := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Audience:  jwt.ClaimStrings{audience},
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Scope: scope,
		Kind:  KindRoomSubscription,
		Room:  room,
	}
	return s.signClaims(c)
}

// Verifier 在网关 / 各服务校验 token。
type Verifier struct {
	kp  KeyProvider
	now func() time.Time
}

// NewVerifier 历史入口；ADR-0019 第七轮起永远返回 [`ErrHS256Removed`]。
//
// 保留函数签名只为避免编译断裂；新代码请用 [`NewVerifierFromProvider`]。
func NewVerifier(_ []byte) (*Verifier, error) {
	return nil, EnsureHS256Allowed()
}

// NewVerifierFromProvider 使用 KeyProvider 构造校验器；推荐路径。
//
// 注意：gateway 通常用 `JWKSClient` 拉公钥 + 缓存，本函数主要给服务自校验使用。
func NewVerifierFromProvider(kp KeyProvider) (*Verifier, error) {
	if kp == nil {
		return nil, errors.New("authjwt: nil KeyProvider")
	}
	return &Verifier{kp: kp, now: time.Now}, nil
}

// Parse 校验签名 + 过期；返回 Claims。
func (v *Verifier) Parse(tokenStr string) (*Claims, error) {
	c := &Claims{}
	tok, err := jwt.ParseWithClaims(tokenStr, c, func(t *jwt.Token) (any, error) {
		// ADR-0019：拒绝 HS256 token，避免下游服务接受历史窗口残留 token。
		if t.Method.Alg() == jwt.SigningMethodHS256.Alg() {
			return nil, ErrHS256Removed
		}
		kid, _ := t.Header["kid"].(string)
		vk, err := v.kp.VerifyingByKid(kid)
		if err != nil {
			return nil, err
		}
		switch vk.Alg {
		case AlgHS256:
			return nil, ErrHS256Removed
		case AlgRS256:
			if t.Method.Alg() != jwt.SigningMethodRS256.Alg() {
				return nil, fmt.Errorf("authjwt: unexpected alg %s, want RS256", t.Method.Alg())
			}
			pub, ok := vk.Material.(*rsa.PublicKey)
			if !ok {
				return nil, errors.New("authjwt: RS verifying key not *rsa.PublicKey")
			}
			return pub, nil
		default:
			return nil, fmt.Errorf("authjwt: unsupported alg %s", vk.Alg)
		}
	}, jwt.WithTimeFunc(v.now))
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("authjwt: invalid token")
	}
	return c, nil
}
