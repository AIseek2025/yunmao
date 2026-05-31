//! yunmao-eventbus：Rust 端事件总线抽象 + Kafka 实现。
//!
//! 设计与 Go `pkg/yunmao/eventbus` 完全对齐：
//!
//! - 使用 CloudEvents 1.0 JSON 信封作为 payload；
//! - `Topic` 命名 `<domain>.<entity>.<verb>`；
//! - DLQ 后缀 `.dlq`；
//! - partition key 由调用方决定（device_id / room_id 等）。
//!
//! 当前实现：
//!
//! - [`MemoryBus`]：内存版（PoC、单测）。
//! - [`KafkaBus`]：基于 `rskafka`（纯 Rust，无 librdkafka 依赖）。
//!
//! Rust 端的角色：
//!
//! - device-edge：消费 `feed.command.requested`、发布 `feed.command.acked`；
//! - gateway：消费 `feed.command.*` / `live.stream.*`，扇出到 WS。
//! - feeding-svc 由 Go 端发布，无需 Rust 端发布器（短期）。

pub mod memory;
pub mod topic;

#[cfg(feature = "kafka")]
pub mod kafka;

pub use memory::MemoryBus;
pub use topic::Topic;

use async_trait::async_trait;
use std::collections::HashMap;
use thiserror::Error;
use yunmao_common::cloudevents::CloudEvent;

/// 与 Go envelope 等价。
#[derive(Debug, Clone)]
pub struct Envelope {
    pub topic: Topic,
    pub key: String,
    pub headers: HashMap<String, String>,
    pub payload: Vec<u8>,
}

impl Envelope {
    /// 把 CloudEvent 包装成 Envelope。
    pub fn from_cloudevent<T: serde::Serialize>(
        topic: Topic,
        key: impl Into<String>,
        evt: &CloudEvent<T>,
    ) -> Result<Self, EventBusError> {
        let payload = serde_json::to_vec(evt)?;
        let mut headers = HashMap::new();
        headers.insert("content-type".into(), "application/cloudevents+json".into());
        headers.insert("ce-type".into(), topic.as_str().to_string());
        Ok(Self {
            topic,
            key: key.into(),
            headers,
            payload,
        })
    }
}

/// 事件总线统一抽象。
#[async_trait]
pub trait Bus: Send + Sync + 'static {
    async fn publish(&self, env: Envelope) -> Result<(), EventBusError>;

    /// 订阅一组 topic；handler 返回 Err 触发重试（实现内部）。
    async fn subscribe(
        &self,
        group: &str,
        topics: Vec<Topic>,
        handler: Handler,
    ) -> Result<(), EventBusError>;

    async fn close(&self) -> Result<(), EventBusError>;
}

/// 消费回调。
pub type Handler = std::sync::Arc<
    dyn Fn(Envelope) -> futures::future::BoxFuture<'static, Result<(), EventBusError>>
        + Send
        + Sync,
>;

#[derive(Debug, Error)]
pub enum EventBusError {
    #[error("eventbus closed")]
    Closed,
    #[error("bad config: {0}")]
    BadConfig(String),
    #[error("kafka: {0}")]
    Kafka(String),
    #[error(transparent)]
    Json(#[from] serde_json::Error),
    #[error("handler failed: {0}")]
    Handler(String),
}

/// 后端类型。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Backend {
    Memory,
    #[cfg(feature = "kafka")]
    Kafka,
}

/// 构造参数。
#[derive(Debug, Clone)]
pub struct Config {
    pub backend: Backend,
    pub brokers: Vec<String>,
    pub client_id: String,
}

/// 工厂入口。
pub async fn open(cfg: Config) -> Result<std::sync::Arc<dyn Bus>, EventBusError> {
    match cfg.backend {
        Backend::Memory => Ok(std::sync::Arc::new(MemoryBus::new())),
        #[cfg(feature = "kafka")]
        Backend::Kafka => {
            if cfg.brokers.is_empty() {
                return Err(EventBusError::BadConfig("brokers required".into()));
            }
            Ok(std::sync::Arc::new(
                kafka::KafkaBus::connect(cfg.brokers, cfg.client_id).await?,
            ))
        }
    }
}
