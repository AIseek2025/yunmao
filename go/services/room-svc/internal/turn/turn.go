// Package turn 实现 TURN time-limited credentials（draft-uberti-rtcweb-turn-rest-00 /
// RFC 7635 expectations）：
//
//   - username = "<expiry_unix>:<user_id>"
//   - credential = base64(HMAC-SHA1(static_auth_secret, username))
//   - TTL：默认 5 分钟（在 IceServers 响应里返回 expires_at / ttl_seconds）。
//
// secret 由 [`Signer`] 持有，可在轮换期同时配置 PrimarySecret + LegacySecret，
// 校验时 PrimarySecret 与 LegacySecret 任一匹配即视为有效（双 secret 重叠窗口）。
package turn

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // SHA-1 是 coturn use-auth-secret 协议要求
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Signer 负责签发与校验 TURN time-limited credential。
type Signer struct {
	primary []byte
	legacy  []byte
	now     func() time.Time
}

// Config Signer 构造参数。
type Config struct {
	// PrimarySecret 必填；当前 active secret。
	PrimarySecret []byte
	// LegacySecret 可选；轮换期接受旧 secret 校验。
	LegacySecret []byte
}

// NewSigner 构造；secret 长度建议 >= 32 字节。
func NewSigner(cfg Config) (*Signer, error) {
	if len(cfg.PrimarySecret) < 16 {
		return nil, errors.New("turn: PrimarySecret too short (>=16 bytes)")
	}
	return &Signer{primary: cfg.PrimarySecret, legacy: cfg.LegacySecret, now: time.Now}, nil
}

// Credential 签发结果。
type Credential struct {
	Username   string    `json:"username"`
	Credential string    `json:"credential"`
	TTLSeconds int       `json:"ttl_seconds"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// Issue 签发短期 credential；userID 通常是 yunmao usr_xxx 或 device_xxx；ttl 必填且 > 0。
func (s *Signer) Issue(userID string, ttl time.Duration) (*Credential, error) {
	if userID == "" {
		return nil, errors.New("turn: userID required")
	}
	if ttl <= 0 {
		return nil, errors.New("turn: ttl must be > 0")
	}
	exp := s.now().Add(ttl).UTC()
	username := fmt.Sprintf("%d:%s", exp.Unix(), userID)
	cred := hmacSign(s.primary, username)
	return &Credential{
		Username:   username,
		Credential: cred,
		TTLSeconds: int(ttl.Seconds()),
		ExpiresAt:  exp,
	}, nil
}

// Verify 校验 username + credential：
//   - 解析 expiry；过期返回 ErrExpired；
//   - HMAC primary/legacy 任一匹配即认为有效。
func (s *Signer) Verify(username, credential string) error {
	parts := strings.SplitN(username, ":", 2)
	if len(parts) != 2 {
		return errors.New("turn: invalid username format")
	}
	expUnix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return fmt.Errorf("turn: invalid expiry: %w", err)
	}
	exp := time.Unix(expUnix, 0).UTC()
	if s.now().After(exp) {
		return ErrExpired
	}
	want := hmacSign(s.primary, username)
	if hmac.Equal([]byte(credential), []byte(want)) {
		return nil
	}
	if len(s.legacy) > 0 {
		want = hmacSign(s.legacy, username)
		if hmac.Equal([]byte(credential), []byte(want)) {
			return nil
		}
	}
	return ErrSignature
}

// 错误。
var (
	ErrExpired   = errors.New("turn: credential expired")
	ErrSignature = errors.New("turn: credential signature mismatch")
)

// hmacSign：HMAC-SHA1(secret, username) → base64(stdEncoding)，与 coturn use-auth-secret 协议一致。
func hmacSign(secret []byte, username string) string {
	mac := hmac.New(sha1.New, secret)
	mac.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// SetNow 注入时钟（测试用）。
func (s *Signer) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// IceServers 返回给客户端的 RTCConfiguration。
type IceServers struct {
	Urls       []string  `json:"urls"`
	Username   string    `json:"username"`
	Credential string    `json:"credential"`
	TTLSeconds int       `json:"ttl_seconds"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// ICEEndpoints 组装 turn:host:port?transport=...
func ICEEndpoints(hosts []string, ports []int) []string {
	if len(hosts) == 0 {
		return nil
	}
	if len(ports) == 0 {
		ports = []int{3478}
	}
	out := make([]string, 0, len(hosts)*len(ports)*2)
	for _, h := range hosts {
		for _, p := range ports {
			out = append(out,
				fmt.Sprintf("turn:%s:%d?transport=udp", h, p),
				fmt.Sprintf("turn:%s:%d?transport=tcp", h, p),
			)
		}
	}
	return out
}
