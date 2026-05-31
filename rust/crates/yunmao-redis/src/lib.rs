//! yunmao-redis：Rust 端 Redis 客户端薄封装。
//!
//! 提供：
//!
//! - `IdempotentStore`：用 `SETNX + EX` 实现的幂等 set，
//!   替代 device-edge 现有的内存 LRU。
//! - `Cache`：通用 KV，少量原语（Get/Set/SetNx/Incr）。
//!
//! 与 Go `pkg/yunmao/cache` 设计同源：所有 key 都加 `yunmao:` 前缀。

use async_trait::async_trait;
use std::time::Duration;
use thiserror::Error;

const KEY_PREFIX: &str = "yunmao:";

#[derive(Debug, Error)]
pub enum RedisError {
    #[error("redis: {0}")]
    Backend(String),
}

impl From<redis::RedisError> for RedisError {
    fn from(value: redis::RedisError) -> Self {
        Self::Backend(value.to_string())
    }
}

/// Cache 抽象，与 Go pkg/yunmao/cache.Store 同语义。
#[async_trait]
pub trait Cache: Send + Sync + 'static {
    async fn set_nx(&self, key: &str, value: &str, ttl: Duration) -> Result<bool, RedisError>;
    async fn set(&self, key: &str, value: &str, ttl: Duration) -> Result<(), RedisError>;
    async fn get(&self, key: &str) -> Result<Option<String>, RedisError>;
    async fn del(&self, key: &str) -> Result<(), RedisError>;
}

/// 基于 `redis` crate 的真实实现。
pub struct RedisCache {
    client: redis::Client,
}

impl RedisCache {
    /// connect URL 形如 `redis://host:6379/0`。
    pub async fn connect(url: &str) -> Result<Self, RedisError> {
        let client = redis::Client::open(url)?;
        // ping
        let mut conn = client.get_multiplexed_async_connection().await?;
        let _: String = redis::cmd("PING").query_async(&mut conn).await?;
        Ok(Self { client })
    }
}

#[async_trait]
impl Cache for RedisCache {
    async fn set_nx(&self, key: &str, value: &str, ttl: Duration) -> Result<bool, RedisError> {
        let mut conn = self.client.get_multiplexed_async_connection().await?;
        // SET k v NX PX ms 返回 OK (success) 或 nil (skipped)。
        let res: redis::Value = redis::cmd("SET")
            .arg(format!("{KEY_PREFIX}{key}"))
            .arg(value)
            .arg("NX")
            .arg("PX")
            .arg(ttl.as_millis() as u64)
            .query_async(&mut conn)
            .await?;
        Ok(matches!(
            res,
            redis::Value::Okay | redis::Value::SimpleString(_)
        ))
    }

    async fn set(&self, key: &str, value: &str, ttl: Duration) -> Result<(), RedisError> {
        let mut conn = self.client.get_multiplexed_async_connection().await?;
        let _: () = redis::cmd("SET")
            .arg(format!("{KEY_PREFIX}{key}"))
            .arg(value)
            .arg("PX")
            .arg(ttl.as_millis() as u64)
            .query_async(&mut conn)
            .await?;
        Ok(())
    }

    async fn get(&self, key: &str) -> Result<Option<String>, RedisError> {
        let mut conn = self.client.get_multiplexed_async_connection().await?;
        let v: Option<String> = redis::cmd("GET")
            .arg(format!("{KEY_PREFIX}{key}"))
            .query_async(&mut conn)
            .await?;
        Ok(v)
    }

    async fn del(&self, key: &str) -> Result<(), RedisError> {
        let mut conn = self.client.get_multiplexed_async_connection().await?;
        let _: i64 = redis::cmd("DEL")
            .arg(format!("{KEY_PREFIX}{key}"))
            .query_async(&mut conn)
            .await?;
        Ok(())
    }
}

/// 内存版（PoC / 单测 / Redis 不可用时降级）。
pub struct MemoryCache {
    inner: tokio::sync::Mutex<std::collections::HashMap<String, (String, std::time::Instant)>>,
}

impl Default for MemoryCache {
    fn default() -> Self {
        Self::new()
    }
}

impl MemoryCache {
    pub fn new() -> Self {
        Self {
            inner: tokio::sync::Mutex::new(std::collections::HashMap::new()),
        }
    }
}

#[async_trait]
impl Cache for MemoryCache {
    async fn set_nx(&self, key: &str, value: &str, ttl: Duration) -> Result<bool, RedisError> {
        let mut g = self.inner.lock().await;
        let now = std::time::Instant::now();
        if let Some((_, exp)) = g.get(key) {
            if now < *exp {
                return Ok(false);
            }
        }
        g.insert(key.to_string(), (value.to_string(), now + ttl));
        Ok(true)
    }

    async fn set(&self, key: &str, value: &str, ttl: Duration) -> Result<(), RedisError> {
        let mut g = self.inner.lock().await;
        g.insert(
            key.to_string(),
            (value.to_string(), std::time::Instant::now() + ttl),
        );
        Ok(())
    }

    async fn get(&self, key: &str) -> Result<Option<String>, RedisError> {
        let g = self.inner.lock().await;
        Ok(g.get(key).map(|v| v.0.clone()))
    }

    async fn del(&self, key: &str) -> Result<(), RedisError> {
        let mut g = self.inner.lock().await;
        g.remove(key);
        Ok(())
    }
}

/// IdempotentStore 基于 set_nx 实现的幂等键。
pub struct IdempotentStore {
    cache: std::sync::Arc<dyn Cache>,
    ttl: Duration,
}

impl IdempotentStore {
    pub fn new(cache: std::sync::Arc<dyn Cache>, ttl: Duration) -> Self {
        Self { cache, ttl }
    }

    /// 返回 true 表示首次见到；false 表示已存在。
    pub async fn insert(&self, ns: &str, key: &str) -> Result<bool, RedisError> {
        let k = format!("idem:{ns}:{key}");
        self.cache.set_nx(&k, "1", self.ttl).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;

    #[tokio::test]
    async fn memory_set_nx_only_once() {
        let c: Arc<dyn Cache> = Arc::new(MemoryCache::new());
        let first = c.set_nx("k", "v", Duration::from_secs(1)).await.unwrap();
        let second = c.set_nx("k", "v", Duration::from_secs(1)).await.unwrap();
        assert!(first);
        assert!(!second);
    }

    #[tokio::test]
    async fn idempotent_store_deduplicates() {
        let c: Arc<dyn Cache> = Arc::new(MemoryCache::new());
        let s = IdempotentStore::new(c, Duration::from_secs(1));
        assert!(s.insert("feed", "abc").await.unwrap());
        assert!(!s.insert("feed", "abc").await.unwrap());
    }
}
