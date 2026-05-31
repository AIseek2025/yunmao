//! 跨实例扇出抽象。
//!
//! 第二轮的 Hub 只在单实例内做 fanout；本轮把它抽象成 [`Fanout`] trait，提供两种实现：
//!
//! - [`LocalFanout`]：单进程内直接广播；默认行为，零依赖。
//! - [`RedisFanout`]：通过 Redis Pub/Sub 在多个 gateway 实例间互相中继；同时维护
//!   `yunmao:room:{room_id}:online` SET，作为跨实例在线集合。
//!
//! 与 ADR-0013 对齐。
//!
//! ## Redis 协议
//!
//! - 频道：`yunmao:fanout:{room_id}`，payload = `ServerFrame::Event` 的 JSON 字节。
//! - 在线集合：`yunmao:room:{room_id}:online`（SADD on subscribe，SREM on unsubscribe / drop）。
//! - 房间统计：`yunmao:rooms:active`（每 `tick` 把 conn 数写到 HLL，便于运营看每秒峰值）。
//!
//! ## 失败模式
//!
//! - 单实例订阅 Redis 失败 → fanout 降级为 local-only，记录 `gateway_fanout_degraded_total`。
//! - publish 失败不 retry，依赖 outbox / 客户端重试机制。
//!
//! ## 测试
//!
//! - `LocalFanout` 用 Hub 单测；
//! - `RedisFanout` 端到端集成测试由 `make dev-up` 起 Redis + 多 gateway 后跑（脚本：scripts/bench-ws.sh）。

use std::sync::Arc;

use async_trait::async_trait;
use bytes::Bytes;
use tracing::{info, warn};

use crate::hub::{Hub, RoomBroadcastMessage};

/// 跨实例扇出抽象。
#[async_trait]
pub trait Fanout: Send + Sync + 'static {
    /// 本实例收到的事件 → 广播到所有订阅者（同实例 + 跨实例）。
    async fn publish(&self, msg: RoomBroadcastMessage);

    /// 加入房间（本实例订阅）。
    async fn join(&self, room_id: &str, conn_id: &str);

    /// 退出房间。
    async fn leave(&self, room_id: &str, conn_id: &str);

    /// 当前 backend 名称（"local" / "redis"），用于日志 / metrics。
    fn backend(&self) -> &'static str;
}

/// 本地（单实例）扇出：直接通过 Hub.broadcast。
#[derive(Clone)]
pub struct LocalFanout {
    hub: Hub,
}

impl LocalFanout {
    pub fn new(hub: Hub) -> Self {
        Self { hub }
    }
}

#[async_trait]
impl Fanout for LocalFanout {
    async fn publish(&self, msg: RoomBroadcastMessage) {
        let n = self.hub.broadcast(msg).await;
        metrics::counter!("gateway_fanout_local_delivered_total").increment(n as u64);
    }
    async fn join(&self, _room_id: &str, _conn_id: &str) {}
    async fn leave(&self, _room_id: &str, _conn_id: &str) {}
    fn backend(&self) -> &'static str {
        "local"
    }
}

/// Redis 扇出：多实例间互相中继。
///
/// 设计：
///
/// - 后台 task 维护一个连接到 redis 的 SUBSCRIBE，监听 `yunmao:fanout:*`；
/// - 每次本地 publish 同时 PUBLISH 到 redis；
/// - 每次 join/leave 同时 SADD / SREM 到 `yunmao:room:{id}:online`。
///
/// 实例自己产生的消息也会通过 redis 收到一遍，这里通过 `instance_id` 头去重。
pub struct RedisFanout {
    hub: Hub,
    redis_url: String,
    instance_id: String,
}

impl RedisFanout {
    pub fn new(hub: Hub, redis_url: impl Into<String>, instance_id: impl Into<String>) -> Self {
        Self {
            hub,
            redis_url: redis_url.into(),
            instance_id: instance_id.into(),
        }
    }

    /// 启动后台订阅 + 中继 task。
    pub async fn start(self: Arc<Self>) -> anyhow::Result<()> {
        let client = redis::Client::open(self.redis_url.as_str())?;
        let mut pubsub_conn = client.get_async_pubsub().await?;
        pubsub_conn.psubscribe("yunmao:fanout:*").await?;
        let this = self.clone();
        tokio::spawn(async move {
            use futures::StreamExt;
            let mut stream = pubsub_conn.on_message();
            while let Some(msg) = stream.next().await {
                let channel = msg.get_channel_name();
                // channel = yunmao:fanout:<room_id>
                let room_id = channel.strip_prefix("yunmao:fanout:").unwrap_or("");
                if room_id.is_empty() {
                    continue;
                }
                let raw: Vec<u8> = match msg.get_payload() {
                    Ok(v) => v,
                    Err(e) => {
                        warn!(error = %e, "redis fanout decode failed");
                        continue;
                    }
                };
                // 跳过本实例自己 publish 的消息（避免环回）
                // 简化协议：前 36 字节是 instance_id（UUID v4 短形式 + 分隔 \n）
                let payload = if let Some(idx) = raw.iter().position(|b| *b == b'\n') {
                    let from = std::str::from_utf8(&raw[..idx]).unwrap_or("");
                    if from == this.instance_id {
                        continue;
                    }
                    Bytes::copy_from_slice(&raw[idx + 1..])
                } else {
                    Bytes::copy_from_slice(&raw)
                };
                let n = this
                    .hub
                    .broadcast(RoomBroadcastMessage {
                        room_id: room_id.to_string(),
                        payload,
                    })
                    .await;
                metrics::counter!("gateway_fanout_redis_in_total").increment(1);
                metrics::counter!("gateway_fanout_redis_delivered_total").increment(n as u64);
            }
        });
        info!(backend = "redis", url = %self.redis_url, "fanout started");
        Ok(())
    }

    async fn pub_redis(&self, room_id: &str, payload: &[u8]) -> anyhow::Result<()> {
        let client = redis::Client::open(self.redis_url.as_str())?;
        let mut conn = client.get_multiplexed_async_connection().await?;
        let channel = format!("yunmao:fanout:{room_id}");
        let mut payload_with_id = Vec::with_capacity(self.instance_id.len() + 1 + payload.len());
        payload_with_id.extend_from_slice(self.instance_id.as_bytes());
        payload_with_id.push(b'\n');
        payload_with_id.extend_from_slice(payload);
        let _: () = redis::cmd("PUBLISH")
            .arg(channel)
            .arg(payload_with_id)
            .query_async(&mut conn)
            .await?;
        Ok(())
    }
}

#[async_trait]
impl Fanout for RedisFanout {
    async fn publish(&self, msg: RoomBroadcastMessage) {
        // 1) 本实例先扇出
        let n = self.hub.broadcast(msg.clone()).await;
        metrics::counter!("gateway_fanout_local_delivered_total").increment(n as u64);
        // 2) 跨实例
        if let Err(e) = self.pub_redis(&msg.room_id, &msg.payload).await {
            warn!(error = %e, "fanout redis publish failed; degraded to local");
            metrics::counter!("gateway_fanout_degraded_total", "reason" => "publish").increment(1);
        } else {
            metrics::counter!("gateway_fanout_redis_out_total").increment(1);
        }
    }

    async fn join(&self, room_id: &str, conn_id: &str) {
        let url = self.redis_url.clone();
        let room = room_id.to_string();
        let conn = conn_id.to_string();
        tokio::spawn(async move {
            if let Ok(client) = redis::Client::open(url) {
                if let Ok(mut c) = client.get_multiplexed_async_connection().await {
                    let _: redis::RedisResult<i64> = redis::cmd("SADD")
                        .arg(format!("yunmao:room:{room}:online"))
                        .arg(conn)
                        .query_async(&mut c)
                        .await;
                }
            }
        });
    }

    async fn leave(&self, room_id: &str, conn_id: &str) {
        let url = self.redis_url.clone();
        let room = room_id.to_string();
        let conn = conn_id.to_string();
        tokio::spawn(async move {
            if let Ok(client) = redis::Client::open(url) {
                if let Ok(mut c) = client.get_multiplexed_async_connection().await {
                    let _: redis::RedisResult<i64> = redis::cmd("SREM")
                        .arg(format!("yunmao:room:{room}:online"))
                        .arg(conn)
                        .query_async(&mut c)
                        .await;
                }
            }
        });
    }

    fn backend(&self) -> &'static str {
        "redis"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::sync::mpsc;

    #[tokio::test]
    async fn local_fanout_broadcasts_to_hub() {
        let hub = Hub::new();
        let (tx, mut rx) = mpsc::channel::<Bytes>(8);
        hub.register("a".into(), tx).await;
        hub.subscribe(&"a".into(), vec!["r".into()]).await;

        let f = LocalFanout::new(hub);
        f.publish(RoomBroadcastMessage {
            room_id: "r".into(),
            payload: Bytes::from_static(b"hello"),
        })
        .await;

        assert_eq!(rx.try_recv().unwrap(), Bytes::from_static(b"hello"));
        assert_eq!(f.backend(), "local");
    }
}
