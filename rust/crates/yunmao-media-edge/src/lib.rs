//! yunmao-media-edge：媒体边缘节点骨架。
//!
//! 主要职责（参见 `01-技术选型与系统架构.md` 第 11 节、`02-高清低延迟直播架构.md`）：
//!
//! - 通过 [`yunmao_ingest::PublishRouter`] 接收 RTMP 推流后的 FLV tag 流。
//! - 暴露 HTTP-FLV 出口给浏览器/移动端兜底拉流。
//! - 暴露 `/metrics` Prometheus 指标。
//! - 预留转码 worker hook（按 02 第 11 节 ABR 表）。
//!
//! 本 crate 同时提供二进制入口 `yunmao-media-edge`，启动 RTMP 监听 + HTTP 监听。

pub mod abr;
pub mod fmp4;
pub mod http_flv;
pub mod ll_hls;
pub mod mediasink;
pub mod metrics;
pub mod qoe;
pub mod server;

pub use abr::{Abr, AbrProfile};
pub use mediasink::{CountingSink, LlHlsSink, MediaSink, PublisherHub, VideoNalu, WhepSink};
pub use server::MediaEdgeConfig;
