//! 平台错误码（参考 `04-设备接入数据模型与API边界.md` 第 11 节）。
//!
//! 命名 `{DOMAIN}.{REASON}`：
//!
//! - DOMAIN：`AUTH`、`USER`、`ROOM`、`FEED`、`DEVICE`、`MEDIA`、`PAY`、`RISK`、`SYSTEM`
//! - REASON：大写蛇形

use serde::{Deserialize, Serialize};
use thiserror::Error;

/// 错误码常量集合。新错误必须先加到这里，再在客户端文案表里同步。
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct ErrorCode(pub &'static str);

impl ErrorCode {
    pub const AUTH_LOGIN_REQUIRED: Self = Self("AUTH.LOGIN_REQUIRED");
    pub const AUTH_TOKEN_EXPIRED: Self = Self("AUTH.TOKEN_EXPIRED");
    pub const AUTH_FORBIDDEN: Self = Self("AUTH.FORBIDDEN");

    pub const USER_NOT_FOUND: Self = Self("USER.NOT_FOUND");

    pub const ROOM_NOT_FOUND: Self = Self("ROOM.NOT_FOUND");
    pub const ROOM_OFFLINE: Self = Self("ROOM.OFFLINE");

    pub const FEED_COOLDOWN_NOT_FINISHED: Self = Self("FEED.COOLDOWN_NOT_FINISHED");
    pub const FEED_HEALTH_LIMIT_HIT: Self = Self("FEED.HEALTH_LIMIT_HIT");
    pub const FEED_DEVICE_OFFLINE: Self = Self("FEED.DEVICE_OFFLINE");
    pub const FEED_NO_FEED_WINDOW: Self = Self("FEED.NO_FEED_WINDOW");
    pub const FEED_DUPLICATE_REQUEST: Self = Self("FEED.DUPLICATE_REQUEST");

    pub const DEVICE_UNBOUND: Self = Self("DEVICE.UNBOUND");
    pub const DEVICE_ERROR_JAMMED: Self = Self("DEVICE.ERROR_JAMMED");

    pub const MEDIA_STREAM_OFFLINE: Self = Self("MEDIA.STREAM_OFFLINE");
    pub const MEDIA_PROFILE_UNAVAILABLE: Self = Self("MEDIA.PROFILE_UNAVAILABLE");

    pub const PAY_ORDER_PAID: Self = Self("PAY.ORDER_PAID");
    pub const PAY_AMOUNT_MISMATCH: Self = Self("PAY.AMOUNT_MISMATCH");

    pub const RISK_ACTION_BLOCKED: Self = Self("RISK.ACTION_BLOCKED");

    pub const SYSTEM_RATE_LIMITED: Self = Self("SYSTEM.RATE_LIMITED");
    pub const SYSTEM_INTERNAL: Self = Self("SYSTEM.INTERNAL");
    pub const SYSTEM_DEPENDENCY_UNAVAILABLE: Self = Self("SYSTEM.DEPENDENCY_UNAVAILABLE");

    pub fn as_str(self) -> &'static str {
        self.0
    }
}

impl Serialize for ErrorCode {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.serialize_str(self.0)
    }
}

impl std::fmt::Display for ErrorCode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.0)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ErrorEnvelope {
    pub error: ErrorBody,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ErrorBody {
    pub code: String,
    pub message: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub remediation: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub trace_id: Option<String>,
}

impl ErrorEnvelope {
    pub fn new(code: ErrorCode, message: impl Into<String>) -> Self {
        Self {
            error: ErrorBody {
                code: code.0.to_string(),
                message: message.into(),
                remediation: None,
                trace_id: None,
            },
        }
    }

    pub fn with_trace(mut self, trace_id: impl Into<String>) -> Self {
        self.error.trace_id = Some(trace_id.into());
        self
    }
}

#[derive(Debug, Error)]
pub enum YunmaoError {
    #[error("{code} {message}")]
    App { code: ErrorCode, message: String },

    #[error(transparent)]
    Json(#[from] serde_json::Error),

    #[error(transparent)]
    Io(#[from] std::io::Error),
}

impl YunmaoError {
    pub fn app(code: ErrorCode, message: impl Into<String>) -> Self {
        Self::App {
            code,
            message: message.into(),
        }
    }

    pub fn to_envelope(&self) -> ErrorEnvelope {
        match self {
            Self::App { code, message } => ErrorEnvelope::new(*code, message.clone()),
            Self::Json(e) => ErrorEnvelope::new(ErrorCode::SYSTEM_INTERNAL, format!("json: {e}")),
            Self::Io(e) => ErrorEnvelope::new(ErrorCode::SYSTEM_INTERNAL, format!("io: {e}")),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn envelope_roundtrip() {
        let err = ErrorEnvelope::new(ErrorCode::FEED_COOLDOWN_NOT_FINISHED, "等 42s");
        let json = serde_json::to_string(&err).unwrap();
        let back: ErrorEnvelope = serde_json::from_str(&json).unwrap();
        assert_eq!(back.error.code, "FEED.COOLDOWN_NOT_FINISHED");
        assert_eq!(back.error.message, "等 42s");
    }

    #[test]
    fn yunmao_error_to_envelope() {
        let err = YunmaoError::app(ErrorCode::AUTH_TOKEN_EXPIRED, "token 过期");
        assert_eq!(err.to_envelope().error.code, "AUTH.TOKEN_EXPIRED");
    }
}
