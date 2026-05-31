//! WebSocket JWT 鉴权（与 Go pkg/yunmao/authjwt 对齐）。
//!
//! - 登录 JWT：kind=login，scope 至少包含 "user"。
//! - 房间订阅 token：kind=room_sub，scope 含 "room:<id>"。
//!
//! 网关启动时通过 `YUNMAO_JWT_SECRET` 注入 HS256 secret；生产路径建议接 JWKS。

use jsonwebtoken::{decode, decode_header, Algorithm, DecodingKey, Validation};
use serde::Deserialize;
use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::sync::RwLock;

/// 与 Go pkg/yunmao/authjwt 完全对齐的 claims（自定义字段）。
///
/// 注：jsonwebtoken v9 默认会校验 exp 与可选的 iss / aud；
/// scope 为单字符串（"user"|"guest"|"admin"），房间订阅 token 还会带 `room` 字段。
#[derive(Debug, Deserialize)]
pub struct Claims {
    pub sub: String,
    pub exp: usize,
    #[serde(default)]
    pub iat: usize,
    #[serde(default)]
    pub iss: String,
    #[serde(default)]
    pub kind: String,
    #[serde(default)]
    pub scope: String,
    #[serde(default)]
    pub room: String,
}

#[derive(Clone)]
enum Backend {
    Hs(DecodingKey),
    Jwks(JwksCache),
}

#[derive(Clone)]
pub struct Verifier {
    backend: Backend,
    expected_issuer: Option<String>,
}

impl Verifier {
    pub fn from_hs256(secret: &str, expected_issuer: Option<String>) -> Self {
        Self {
            backend: Backend::Hs(DecodingKey::from_secret(secret.as_bytes())),
            expected_issuer,
        }
    }

    /// 用一个或多个 JWKS endpoint 构造校验器（RS256 主路径）。
    ///
    /// 网关收到 token 时通过 `kid` header 选取对应公钥；找不到则触发刷新。
    pub fn from_jwks(endpoints: Vec<String>, expected_issuer: Option<String>) -> Self {
        Self {
            backend: Backend::Jwks(JwksCache::new(endpoints, Duration::from_secs(300))),
            expected_issuer,
        }
    }

    pub fn verify(&self, token: &str) -> Result<Claims, String> {
        match &self.backend {
            Backend::Hs(key) => {
                let mut v = Validation::new(Algorithm::HS256);
                v.validate_exp = true;
                v.validate_aud = false;
                if let Some(iss) = &self.expected_issuer {
                    v.set_issuer(&[iss]);
                }
                let data = decode::<Claims>(token, key, &v).map_err(|e| e.to_string())?;
                Ok(data.claims)
            }
            Backend::Jwks(cache) => {
                let header = decode_header(token).map_err(|e| e.to_string())?;
                let kid = header.kid.clone().unwrap_or_default();
                let key = cache
                    .get_blocking(&kid)
                    .ok_or_else(|| format!("unknown kid {kid}"))?;
                let mut v = Validation::new(Algorithm::RS256);
                v.validate_exp = true;
                v.validate_aud = false;
                if let Some(iss) = &self.expected_issuer {
                    v.set_issuer(&[iss]);
                }
                let data = decode::<Claims>(token, &key, &v).map_err(|e| e.to_string())?;
                Ok(data.claims)
            }
        }
    }
}

/// 简易的 in-process JWKS 缓存：5min TTL，按 kid 查找。
#[derive(Clone)]
pub struct JwksCache {
    inner: Arc<RwLock<JwksInner>>,
    endpoints: Vec<String>,
    ttl: Duration,
}

struct JwksInner {
    keys: HashMap<String, DecodingKey>,
    fetched_at: Option<Instant>,
}

impl JwksCache {
    pub fn new(endpoints: Vec<String>, ttl: Duration) -> Self {
        Self {
            inner: Arc::new(RwLock::new(JwksInner {
                keys: HashMap::new(),
                fetched_at: None,
            })),
            endpoints,
            ttl,
        }
    }

    fn get_blocking(&self, kid: &str) -> Option<DecodingKey> {
        let handle = tokio::runtime::Handle::try_current().ok();
        if let Some(h) = handle {
            h.block_on(self.get(kid))
        } else {
            // 没有 tokio runtime（极少见，比如纯单测）；返回 None 等待初始化
            None
        }
    }

    pub async fn get(&self, kid: &str) -> Option<DecodingKey> {
        {
            let g = self.inner.read().await;
            if let Some(k) = g.keys.get(kid).cloned() {
                if let Some(t) = g.fetched_at {
                    if t.elapsed() < self.ttl {
                        return Some(k);
                    }
                }
            }
        }
        let _ = self.refresh().await;
        let g = self.inner.read().await;
        g.keys.get(kid).cloned()
    }

    pub async fn refresh(&self) -> Result<(), String> {
        if self.endpoints.is_empty() {
            return Err("no JWKS endpoints".into());
        }
        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(3))
            .build()
            .map_err(|e| e.to_string())?;
        let mut merged: HashMap<String, DecodingKey> = HashMap::new();
        for ep in &self.endpoints {
            let resp = match client.get(ep).send().await {
                Ok(r) => r,
                Err(e) => {
                    tracing::warn!(error = %e, endpoint = %ep, "jwks fetch failed");
                    continue;
                }
            };
            if !resp.status().is_success() {
                continue;
            }
            let v: serde_json::Value = match resp.json().await {
                Ok(v) => v,
                Err(_) => continue,
            };
            if let Some(keys) = v.get("keys").and_then(|v| v.as_array()) {
                for k in keys {
                    let kty = k.get("kty").and_then(|v| v.as_str()).unwrap_or("");
                    if kty != "RSA" {
                        continue;
                    }
                    let kid = match k.get("kid").and_then(|v| v.as_str()) {
                        Some(s) => s.to_string(),
                        None => continue,
                    };
                    let n = k.get("n").and_then(|v| v.as_str()).unwrap_or("");
                    let e = k.get("e").and_then(|v| v.as_str()).unwrap_or("");
                    if n.is_empty() || e.is_empty() {
                        continue;
                    }
                    if let Ok(dk) = DecodingKey::from_rsa_components(n, e) {
                        merged.insert(kid, dk);
                    }
                }
            }
        }
        if merged.is_empty() {
            return Err("empty JWKS".into());
        }
        let mut g = self.inner.write().await;
        g.keys = merged;
        g.fetched_at = Some(Instant::now());
        Ok(())
    }
}

/// 房间订阅 token 是否覆盖目标房间。
pub fn scope_allows_room(claims: &Claims, room_id: &str) -> bool {
    claims.kind == "room_subscription" && claims.room == room_id
}

/// 判定是否为登录态用户（kind=login 且非空 sub）。
pub fn is_logged_in(claims: &Claims) -> bool {
    claims.kind == "login" && !claims.sub.is_empty() && claims.scope != "guest"
}

#[cfg(test)]
mod tests {
    use super::*;
    use jsonwebtoken::{encode, EncodingKey, Header};
    use serde::Serialize;

    #[derive(Serialize)]
    struct C<'a> {
        sub: &'a str,
        exp: usize,
        iat: usize,
        iss: &'a str,
        kind: &'a str,
        scope: &'a str,
        #[serde(skip_serializing_if = "str::is_empty")]
        room: &'a str,
    }

    fn now() -> usize {
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs() as usize
    }

    fn sign(secret: &str, c: &C<'_>) -> String {
        encode(
            &Header::new(Algorithm::HS256),
            c,
            &EncodingKey::from_secret(secret.as_bytes()),
        )
        .unwrap()
    }

    fn secret() -> String {
        "0123456789abcdefghij".into() // >=16 bytes
    }

    #[test]
    fn verifies_valid_login_token() {
        let c = C {
            sub: "u_1",
            exp: now() + 60,
            iat: now(),
            iss: "yunmao.user-svc",
            kind: "login",
            scope: "user",
            room: "",
        };
        let token = sign(&secret(), &c);
        let v = Verifier::from_hs256(&secret(), Some("yunmao.user-svc".into()));
        let claims = v.verify(&token).unwrap();
        assert!(is_logged_in(&claims));
    }

    #[test]
    fn rejects_wrong_secret() {
        let token = sign(
            "rightsecret-1234567890",
            &C {
                sub: "u_1",
                exp: now() + 60,
                iat: now(),
                iss: "yunmao.user-svc",
                kind: "login",
                scope: "user",
                room: "",
            },
        );
        let v = Verifier::from_hs256("wrongsecret-1234567890", None);
        assert!(v.verify(&token).is_err());
    }

    #[test]
    fn room_scope_check() {
        let c = Claims {
            sub: "u_1".into(),
            exp: 0,
            iat: 0,
            iss: String::new(),
            kind: "room_subscription".into(),
            scope: "user".into(),
            room: "room_demo".into(),
        };
        assert!(scope_allows_room(&c, "room_demo"));
        assert!(!scope_allows_room(&c, "room_other"));
    }
}
