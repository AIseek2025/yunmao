// Package service 实现 user-svc 的核心：
//
//   - 手机号验证码登录（PoC：dev 直接返回 SMS code）
//   - dev mock 直接登录：`POST /v1/auth/login` 接受 phone 或 user_id，签发真实 JWT
//   - 用户元数据
//
// 真正生产应：把 challenges 走 Redis SETEX、把用户落 PG、把短信通道接入运营短信 API。
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"yunmao.live/pkg/yunmao/authjwt"
	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/ids"
	"yunmao.live/services/user-svc/internal/store"
)

type User struct {
	ID        string    `json:"id"`
	Nickname  string    `json:"nickname"`
	PhoneHash string    `json:"phone_hash"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// SmsChallenge 验证码挑战。MVP 用内存 map；生产用 Redis SETEX。
type SmsChallenge struct {
	ID        string
	PhoneHash string
	Code      string
	ExpiresAt time.Time
}

type UserService struct {
	mu         sync.Mutex
	challenges map[string]*SmsChallenge
	store      store.Store
	now        func() time.Time
	signer     *authjwt.Signer
	tokenTTL   time.Duration
	audience   string
}

// Config user-svc 配置。
type Config struct {
	Signer   *authjwt.Signer
	TokenTTL time.Duration
	Audience string
	Store    store.Store // 可选；默认使用 in-memory。
}

func New(cfg Config) *UserService {
	if cfg.TokenTTL == 0 {
		cfg.TokenTTL = 24 * time.Hour
	}
	if cfg.Audience == "" {
		cfg.Audience = "yunmao.gateway"
	}
	if cfg.Store == nil {
		cfg.Store = store.NewMemoryStore()
	}
	return &UserService{
		challenges: make(map[string]*SmsChallenge),
		store:      cfg.Store,
		now:        time.Now,
		signer:     cfg.Signer,
		tokenTTL:   cfg.TokenTTL,
		audience:   cfg.Audience,
	}
}

// StartSmsLogin 生成挑战 + 验证码。
func (s *UserService) StartSmsLogin(_ context.Context, phoneE164 string) (challengeID, smsCode string, expiresIn time.Duration, err error) {
	if len(phoneE164) < 5 {
		return "", "", 0, yerr.New(yerr.SystemInternal, "invalid phone")
	}
	code, err := genCode()
	if err != nil {
		return "", "", 0, err
	}
	id := "ch_" + ids.New(ids.PrefixSession)
	s.mu.Lock()
	s.challenges[id] = &SmsChallenge{
		ID:        id,
		PhoneHash: hashPhone(phoneE164),
		Code:      code,
		ExpiresAt: s.now().Add(5 * time.Minute),
	}
	s.mu.Unlock()
	return id, code, 5 * time.Minute, nil
}

// CompleteSmsLogin 校验 + 创建/获取用户 + 签发真实 JWT。
func (s *UserService) CompleteSmsLogin(ctx context.Context, challengeID, code string) (*User, string, error) {
	s.mu.Lock()
	ch, ok := s.challenges[challengeID]
	if !ok {
		s.mu.Unlock()
		return nil, "", yerr.New(yerr.AuthLoginRequired, "challenge not found")
	}
	if s.now().After(ch.ExpiresAt) {
		delete(s.challenges, challengeID)
		s.mu.Unlock()
		return nil, "", yerr.New(yerr.AuthTokenExpired, "challenge expired")
	}
	if ch.Code != code {
		s.mu.Unlock()
		return nil, "", yerr.New(yerr.AuthLoginRequired, "code mismatch")
	}
	delete(s.challenges, challengeID)
	s.mu.Unlock()

	su, err := s.store.GetByPhoneHash(ctx, ch.PhoneHash)
	var user *User
	if err != nil && !isNotFound(err) {
		return nil, "", yerr.New(yerr.SystemInternal, "load user: "+err.Error())
	}
	if su == nil {
		nick := "yunmao_" + ch.PhoneHash[:6]
		dto := &store.User{
			ID:        ids.New(ids.PrefixUser),
			Nickname:  nick,
			PhoneHash: ch.PhoneHash,
			Role:      "user",
			CreatedAt: s.now().UTC(),
		}
		if err := s.store.Upsert(ctx, dto); err != nil {
			return nil, "", yerr.New(yerr.SystemInternal, "store user: "+err.Error())
		}
		user = fromStore(dto)
	} else {
		user = fromStore(su)
	}

	tok, err := s.signer.SignLogin(user.ID, authjwt.ScopeUser, s.audience, s.tokenTTL)
	if err != nil {
		return nil, "", yerr.New(yerr.SystemInternal, "sign jwt: "+err.Error())
	}
	_ = s.store.AppendLogin(ctx, store.LoginHistoryEntry{
		UserID:  user.ID,
		Channel: "sms",
		JwtKid:  s.signerKid(),
		At:      s.now().UTC(),
	})
	return user, tok, nil
}

// LoginInput dev mock 的简化登录入参。
type LoginInput struct {
	UserID    string `json:"user_id"`    // 二者择一
	PhoneE164 string `json:"phone_e164"` // 自动转 sha256
}

// DevLogin 直接签发 token：开发联调使用，prod 必须关闭。
func (s *UserService) DevLogin(ctx context.Context, in LoginInput) (*User, string, error) {
	if in.UserID == "" && in.PhoneE164 == "" {
		return nil, "", yerr.New(yerr.SystemInternal, "user_id or phone_e164 required")
	}
	phHash := ""
	if in.PhoneE164 != "" {
		phHash = hashPhone(in.PhoneE164)
	}

	var existing *store.User
	var err error
	if in.UserID != "" {
		existing, err = s.store.GetByID(ctx, in.UserID)
		if err != nil && !isNotFound(err) {
			return nil, "", yerr.New(yerr.SystemInternal, "load user: "+err.Error())
		}
	}
	if existing == nil && phHash != "" {
		existing, err = s.store.GetByPhoneHash(ctx, phHash)
		if err != nil && !isNotFound(err) {
			return nil, "", yerr.New(yerr.SystemInternal, "load user: "+err.Error())
		}
	}
	if existing == nil {
		uid := in.UserID
		if uid == "" {
			uid = ids.New(ids.PrefixUser)
		}
		nickHash := phHash
		if nickHash == "" {
			nickHash = uid
		}
		if len(nickHash) > 6 {
			nickHash = nickHash[:6]
		}
		existing = &store.User{
			ID:        uid,
			Nickname:  "yunmao_" + nickHash,
			PhoneHash: phHash,
			Role:      "user",
			CreatedAt: s.now().UTC(),
		}
		if err := s.store.Upsert(ctx, existing); err != nil {
			return nil, "", yerr.New(yerr.SystemInternal, "store user: "+err.Error())
		}
	}

	tok, err := s.signer.SignLogin(existing.ID, authjwt.ScopeUser, s.audience, s.tokenTTL)
	if err != nil {
		return nil, "", yerr.New(yerr.SystemInternal, "sign jwt: "+err.Error())
	}
	_ = s.store.AppendLogin(ctx, store.LoginHistoryEntry{
		UserID:  existing.ID,
		Channel: "dev",
		JwtKid:  s.signerKid(),
		At:      s.now().UTC(),
	})
	return fromStore(existing), tok, nil
}

// signerKid 取出当前签名密钥的 kid，best-effort（HS256 / RS256 都有 kid）。
func (s *UserService) signerKid() string {
	if s.signer == nil {
		return ""
	}
	kp := s.signer.KeyProvider()
	if kp == nil {
		return ""
	}
	sk, err := kp.Active()
	if err != nil || sk == nil {
		return ""
	}
	return sk.Kid
}

// Get 查询用户。
func (s *UserService) Get(ctx context.Context, id string) (*User, error) {
	u, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return fromStore(u), nil
}

func fromStore(u *store.User) *User {
	if u == nil {
		return nil
	}
	return &User{
		ID: u.ID, Nickname: u.Nickname, PhoneHash: u.PhoneHash,
		Role: u.Role, CreatedAt: u.CreatedAt,
	}
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var ye *yerr.AppError
	if errors.As(err, &ye) {
		return ye.Code == yerr.UserNotFound
	}
	return false
}

// JWKS 暴露公开材料；HS256 占位（仅声明 KID）。
func (s *UserService) JWKS() map[string]any {
	return s.signer.JWKS()
}

// KeysHealth 输出 /internal/keys/health 内容；优先调用 KeyProvider.Health()，否则从 JWKS 衍生。
func (s *UserService) KeysHealth() map[string]any {
	out := map[string]any{
		"service": "user-svc",
		"now":     time.Now().UTC().Format(time.RFC3339),
	}
	kp := s.signer.KeyProvider()
	if h, ok := kp.(interface{ Health() map[string]any }); ok {
		for k, v := range h.Health() {
			out[k] = v
		}
		return out
	}
	jwks := kp.PublicJWKS()
	out["active"] = s.signerKid()
	if keys, ok := jwks["keys"].([]map[string]any); ok {
		kids := make([]string, 0, len(keys))
		for _, m := range keys {
			if kid, ok := m["kid"].(string); ok {
				kids = append(kids, kid)
			}
		}
		out["kids"] = kids
	}
	return out
}

func genCode() (string, error) {
	var buf [3]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	n := uint32(buf[0])<<16 | uint32(buf[1])<<8 | uint32(buf[2])
	return fmt.Sprintf("%06d", n%1_000_000), nil
}

func hashPhone(phone string) string {
	sum := sha256.Sum256([]byte(phone))
	return hex.EncodeToString(sum[:])
}
