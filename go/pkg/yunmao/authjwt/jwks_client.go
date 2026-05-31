package authjwt

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// JWKSClient 拉取并缓存远端 user-svc / room-svc 的 JWKS。
//
// 用法：
//
//	c := authjwt.NewJWKSClient([]string{"http://user-svc:8081/jwks.json"}, 5*time.Minute)
//	v := authjwt.NewVerifierFromProvider(c)
//	v.Parse(token)
//
// 实现策略：
//   - 任意一个 endpoint 命中即可（多写多备份）。
//   - cacheTTL 默认 5 分钟；过期后热刷新，刷新失败保留旧值并打 metric。
type JWKSClient struct {
	endpoints []string
	httpc     *http.Client
	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
	ttl       time.Duration
}

// NewJWKSClient 构造客户端。
func NewJWKSClient(endpoints []string, ttl time.Duration) *JWKSClient {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &JWKSClient{
		endpoints: endpoints,
		httpc:     &http.Client{Timeout: 5 * time.Second},
		keys:      map[string]*rsa.PublicKey{},
		ttl:       ttl,
	}
}

// Refresh 强制刷新 JWKS。
func (c *JWKSClient) Refresh(ctx context.Context) error {
	if len(c.endpoints) == 0 {
		return errors.New("authjwt: no JWKS endpoints configured")
	}
	merged := map[string]*rsa.PublicKey{}
	var lastErr error
	for _, ep := range c.endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := c.httpc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("authjwt: jwks %s status %d", ep, resp.StatusCode)
			continue
		}
		var doc struct {
			Keys []map[string]any `json:"keys"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			lastErr = err
			continue
		}
		for _, k := range doc.Keys {
			kty, _ := k["kty"].(string)
			if kty != "RSA" {
				continue
			}
			kid, _ := k["kid"].(string)
			nStr, _ := k["n"].(string)
			eStr, _ := k["e"].(string)
			if kid == "" || nStr == "" || eStr == "" {
				continue
			}
			nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
			if err != nil {
				continue
			}
			eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
			if err != nil {
				continue
			}
			merged[kid] = &rsa.PublicKey{
				N: new(big.Int).SetBytes(nBytes),
				E: int(new(big.Int).SetBytes(eBytes).Int64()),
			}
		}
	}
	if len(merged) == 0 {
		if lastErr == nil {
			lastErr = errors.New("authjwt: empty JWKS")
		}
		return lastErr
	}
	c.mu.Lock()
	c.keys = merged
	c.fetchedAt = time.Now()
	c.mu.Unlock()
	return nil
}

// ensureFresh 按 TTL 触发刷新。
func (c *JWKSClient) ensureFresh(ctx context.Context) {
	c.mu.RLock()
	stale := time.Since(c.fetchedAt) > c.ttl
	c.mu.RUnlock()
	if !stale {
		return
	}
	_ = c.Refresh(ctx)
}

// Active 不适用：JWKSClient 仅校验，不签发。
func (c *JWKSClient) Active() (*SigningKey, error) {
	return nil, errors.New("authjwt: JWKSClient is verify-only")
}

// PublicJWKS 暴露本地缓存的公钥集合（同样格式）。
func (c *JWKSClient) PublicJWKS() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := make([]map[string]any, 0, len(c.keys))
	for kid, p := range c.keys {
		keys = append(keys, map[string]any{
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"kid": kid,
			"n":   base64URLEncode(p.N.Bytes()),
			"e":   base64URLEncode(big.NewInt(int64(p.E)).Bytes()),
		})
	}
	return map[string]any{"keys": keys}
}

// VerifyingByKid 按 kid 查找公钥；找不到时同步触发一次刷新。
func (c *JWKSClient) VerifyingByKid(kid string) (*VerifyingKey, error) {
	c.ensureFresh(context.Background())
	c.mu.RLock()
	pub, ok := c.keys[kid]
	c.mu.RUnlock()
	if ok {
		return &VerifyingKey{Kid: kid, Alg: AlgRS256, Material: pub}, nil
	}
	if err := c.Refresh(context.Background()); err == nil {
		c.mu.RLock()
		pub, ok = c.keys[kid]
		c.mu.RUnlock()
		if ok {
			return &VerifyingKey{Kid: kid, Alg: AlgRS256, Material: pub}, nil
		}
	}
	return nil, fmt.Errorf("authjwt: unknown kid %q", kid)
}
