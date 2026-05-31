//! yunmao-common
//!
//! 平台通用基础设施：
//!
//! - [`id`]：`{prefix}_{ulid}` 格式的领域 ID（参见 docs/finalproductplanning/11 第 7 节）。
//! - [`error`]：跨服务的错误码 / 错误信封（与 `04` 第 11 节一致）。
//! - [`cloudevents`]：CloudEvents 1.0 信封（与 `04` 第 10 节一致）。
//! - [`telemetry`]：tracing-subscriber 初始化。

pub mod cloudevents;
pub mod error;
pub mod id;
pub mod telemetry;

pub use cloudevents::CloudEvent;
pub use error::{ErrorCode, ErrorEnvelope, YunmaoError};
pub use id::{DomainId, IdPrefix};
