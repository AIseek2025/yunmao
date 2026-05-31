//! yunmao-ingest
//!
//! RTMP/SRT/WHIP 推流接入。当前实现：
//!
//! - [`rtmp`]：基于 `rml_rtmp` 的 RTMP 服务器握手 / connect / publish + tag 解析与扇出。
//! - SRT、WHIP 暂以 TODO 形式列在文档中。
//!
//! 设计要点：
//!
//! - 每路推流视为一个"会话"，持有一个 [`MediaPipe`]，把 audio/video tag 投递到
//!   下游的 mpsc。下游（[`crate::router::PublishRouter`]）按 `room_id` 扇出。
//! - 不在 ingest 内部做转码，遵守 01 第 3 节"Rust 不直接写业务库"，仅把媒体 tag
//!   流标准化后向 media-edge 转发。
//!
//! 关键测试覆盖：
//! - FLV header 编码 / 解析
//! - tag 缓冲区与扇出
//! - publish 路由（`live/{room_id}` 提取）

pub mod flv;
pub mod pipe;
pub mod router;
pub mod rtmp;

pub use flv::{FlvHeader, FlvTag, TagKind};
pub use pipe::{MediaPipe, MediaSubscriber};
pub use router::{PublishRouter, RoomKey};

/// ingest crate 的版本字符串，用于日志 / metrics 标签。
pub const VERSION: &str = env!("CARGO_PKG_VERSION");
