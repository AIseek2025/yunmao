//! yunmao-gateway：高密度 WebSocket 网关。
//!
//! 协议：JSON（[`yunmao_protocol::signaling`]）。
//!
//! 主要能力：
//!
//! - 客户端连接握手 + 心跳。
//! - 房间订阅（每个连接订阅 0..N 房间，由网关维护订阅表）。
//! - 通过 [`Hub`] 把进程内的事件扇出到对应房间订阅者。
//! - HTTP /publish 接口：让 Go 端控制面把事件推过来（PoC 版替代 Kafka 消费者）。
//! - `/metrics` Prometheus 指标。
//!
//! 性能：
//!
//! - 单连接两个独立 task：reader / writer，互不阻塞。
//! - 每条出向消息使用 `Bytes` 共享，避免每个订阅者拷贝一份大消息。

pub mod auth;
pub mod fanout;
pub mod hub;
pub mod kafka_runtime;
pub mod server;

pub use auth::Verifier;
pub use fanout::{Fanout, LocalFanout, RedisFanout};
pub use hub::{Hub, RoomBroadcastMessage};
pub use server::GatewayConfig;
