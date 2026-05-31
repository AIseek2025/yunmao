//! Kafka 事件 `data` 负载（与 `04-设备接入数据模型与API边界.md` 10.2 一致）。

use serde::{Deserialize, Serialize};

/// `feed.request.created` 数据
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct FeedRequestCreated {
    pub feed_request_id: String,
    pub user_id: String,
    pub room_id: String,
    pub cat_id: String,
    pub device_id: String,
    pub amount_grams: u32,
    pub idempotency_key: String,
    pub created_at: String,
}

/// `feed.command.requested` 数据：由 feeding-svc 发，进入按 device_id 分区的指令队列
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct FeedCommandRequested {
    pub feed_request_id: String,
    pub device_command_id: String,
    pub device_id: String,
    pub room_id: String,
    pub amount_grams: u32,
    pub motor_duration_ms: u32,
    pub expires_at: String,
}

/// `feed.command.dispatched` 数据：由 device-svc 发，已经下发到 MQTT
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct FeedCommandDispatched {
    pub feed_request_id: String,
    pub device_command_id: String,
    pub device_id: String,
    pub room_id: String,
    pub dispatched_at: String,
}

/// `feed.command.acked` 数据：设备执行完成
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct FeedCommandAcked {
    pub feed_request_id: String,
    pub device_command_id: String,
    pub device_id: String,
    pub room_id: String,
    pub status: AckedStatus,
    pub actual_amount_grams: u32,
    pub remaining_food_grams: u32,
    pub executed_at: String,
    #[serde(default)]
    pub errors: Vec<String>,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum AckedStatus {
    Succeeded,
    Failed,
    Jammed,
    InsufficientFood,
    Timeout,
}

/// `device.state.changed` 数据
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct DeviceStateChanged {
    pub device_id: String,
    pub room_id: String,
    pub online: bool,
    pub remaining_food_grams: u32,
    pub last_seen_at: String,
}

/// `live.stream.online` / `live.stream.offline` 数据
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct StreamLifecycle {
    pub stream_id: String,
    pub room_id: String,
    pub online: bool,
    pub protocol: String,
    #[serde(default)]
    pub primary_profile: Option<String>,
    pub at: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn feed_command_requested_roundtrip() {
        let evt = FeedCommandRequested {
            feed_request_id: "feed_01".into(),
            device_command_id: "cmd_01".into(),
            device_id: "dev_demo".into(),
            room_id: "room_demo".into(),
            amount_grams: 5,
            motor_duration_ms: 1200,
            expires_at: "2026-05-25T04:00:30Z".into(),
        };
        let json = serde_json::to_string(&evt).unwrap();
        let back: FeedCommandRequested = serde_json::from_str(&json).unwrap();
        assert_eq!(evt, back);
    }
}
