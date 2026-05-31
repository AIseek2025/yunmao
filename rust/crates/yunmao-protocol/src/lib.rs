//! yunmao-protocol
//!
//! 跨服务共享的协议数据结构（不依赖具体传输层）：
//!
//! - [`signaling`]：浏览器/App ↔ realtime-gateway 的 JSON 协议
//! - [`device`]：设备 MQTT 协议（指令/回执/遥测）
//! - [`events`]：Kafka CloudEvents 中常见的 `data` 负载

pub mod device;
pub mod events;
pub mod signaling;

pub use device::{DeviceAck, DeviceCommand, DeviceTelemetry};
pub use events::{
    DeviceStateChanged, FeedCommandAcked, FeedCommandDispatched, FeedCommandRequested,
    FeedRequestCreated, StreamLifecycle,
};
pub use signaling::{ClientFrame, ServerFrame};
