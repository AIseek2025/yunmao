//! yunmao-device-edge：模拟设备边缘代理 / 投喂机执行端。
//!
//! 职责（参见 `04-设备接入数据模型与API边界.md`）：
//!
//! - 接收 [`yunmao_protocol::events::FeedCommandRequested`] 事件（HTTP PoC，未来从 Kafka 消费）。
//! - **按 device_id 串行**执行：每个设备一个独立 worker（FIFO），保证不会并发指令撞车。
//! - 模拟随机执行延迟、随机失败概率、卡粮 / 粮量不足等异常。
//! - 写回 [`yunmao_protocol::events::FeedCommandAcked`] 到 HTTP-向（默认推到 Go feeding-svc 的 `/internal/feed-acks`）。
//!
//! 关键不变式：同一 `device_command_id` 多次到达只触发一次出粮（用 LRU 缓存 + 内存幂等）。

pub mod device_info;
pub mod kafka_runtime;
pub mod scheduler;
pub mod server;
pub mod simulator;

pub use scheduler::DeviceScheduler;
pub use server::DeviceEdgeConfig;
pub use simulator::SimulatedDevice;
