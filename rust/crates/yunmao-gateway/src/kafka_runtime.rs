//! Kafka 消费侧：把投喂 / 设备 / 直播事件扇出到 WebSocket 房间。
//!
//! topic 与 partition key 与 Go 端 `pkg/yunmao/eventbus` 一致：
//!
//! - `feed.command.requested` / `feed.command.acked` / `feed.command.completed` / `feed.command.failed`
//! - `device.state.changed`
//! - `live.stream.online` / `live.stream.offline`
//!
//! payload 为 CloudEvents JSON 信封，`subject` 一般为 `feed_request_id` 或 `room_id`，
//! 我们从 envelope `key` 优先取 `room_id`，否则尝试从 `data.room_id` 解析。

use std::sync::Arc;

use bytes::Bytes;
use serde_json::Value;
use tracing::{debug, warn};
use yunmao_eventbus::{topic::names, Bus, Envelope, Topic};
use yunmao_protocol::signaling::ServerFrame;

use crate::hub::{Hub, RoomBroadcastMessage};

const TOPICS: &[&str] = &[
    names::FEED_COMMAND_REQUESTED,
    names::FEED_COMMAND_ACKED,
    names::FEED_COMMAND_COMPLETED,
    names::FEED_COMMAND_FAILED,
    names::DEVICE_STATE_CHANGED,
    names::LIVE_STREAM_ONLINE,
    names::LIVE_STREAM_OFFLINE,
    // 第六轮：chat-svc 发出的弹幕与审核事件，gateway 扇出给 WS 订阅者。
    names::ROOM_CHAT_MESSAGE,
    names::ROOM_CHAT_MODERATION,
];

/// 启动 Kafka 订阅，把事件扇出到对应房间。
///
/// 注意：当 gateway 启用 Redis fanout 时，本实例仍只对**本实例 hub** 直接 broadcast，
/// 不通过 redis 再次 publish —— Kafka 已经做了跨实例分发；再 publish 会引入 N² 重放。
pub async fn spawn_fanout(bus: Arc<dyn Bus>, hub: Hub) -> anyhow::Result<()> {
    let topics: Vec<Topic> = TOPICS.iter().map(|t| Topic::new(*t)).collect();
    let hub = hub.clone();
    let handler: yunmao_eventbus::Handler = Arc::new(move |env: Envelope| {
        let hub = hub.clone();
        Box::pin(async move {
            let event_type = env.topic.as_str().to_string();
            let envelope_val: Value = match serde_json::from_slice(&env.payload) {
                Ok(v) => v,
                Err(e) => {
                    warn!(error = %e, topic = %event_type, "decode envelope failed");
                    return Ok(());
                }
            };
            let data = envelope_val.get("data").cloned().unwrap_or(Value::Null);
            let room_id = data
                .get("room_id")
                .and_then(|v| v.as_str())
                .map(str::to_string)
                .unwrap_or_else(|| env.key.clone());
            if room_id.is_empty() {
                debug!(topic = %event_type, "no room_id; skip fanout");
                return Ok(());
            }
            let frame = ServerFrame::Event {
                event_type: event_type.clone(),
                room_id: room_id.clone(),
                data,
                ts: time::OffsetDateTime::now_utc().unix_timestamp_nanos() as i64 / 1_000_000,
            };
            if let Ok(payload) = serde_json::to_vec(&frame) {
                let n = hub
                    .broadcast(RoomBroadcastMessage {
                        room_id: room_id.clone(),
                        payload: Bytes::from(payload),
                    })
                    .await;
                metrics::counter!(
                    "gateway_kafka_fanout_total",
                    "event_type" => event_type.clone(),
                )
                .increment(1);
                metrics::counter!("gateway_kafka_fanout_delivered_total").increment(n as u64);
            }
            Ok(())
        })
    });
    bus.subscribe("gateway", topics, handler).await?;
    Ok(())
}
