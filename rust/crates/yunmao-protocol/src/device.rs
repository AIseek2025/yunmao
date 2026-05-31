//! 设备 MQTT 协议（参考 04-设备接入数据模型与API边界.md 第 7 节）。

use serde::{Deserialize, Serialize};

/// 平台 → 设备：投喂等指令。
///
/// MQTT topic: `devices/{device_id}/commands`
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct DeviceCommand {
    pub version: String,
    pub device_command_id: String,
    pub feed_request_id: String,
    pub command: DeviceCommandKind,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub params: Option<DispenseParams>,
    /// RFC3339 UTC，指令过期后设备必须丢弃
    pub expires_at: String,
    pub nonce: String,
    pub signature: String,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum DeviceCommandKind {
    Dispense,
    Heartbeat,
    /// 立即重启
    Reboot,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct DispenseParams {
    pub amount_grams: u32,
    /// 给电机/螺杆的实际驱动时长，由控制面计算
    pub motor_duration_ms: u32,
}

/// 设备 → 平台：指令回执。
///
/// MQTT topic: `devices/{device_id}/acks`
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct DeviceAck {
    pub version: String,
    pub device_command_id: String,
    pub status: AckStatus,
    #[serde(default)]
    pub actual_amount_grams: u32,
    #[serde(default)]
    pub remaining_food_grams: u32,
    pub executed_at: String,
    #[serde(default)]
    pub errors: Vec<String>,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum AckStatus {
    Succeeded,
    Failed,
    Jammed,
    InsufficientFood,
    Timeout,
}

/// 设备 → 平台：周期遥测。
///
/// MQTT topic: `devices/{device_id}/telemetry`
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct DeviceTelemetry {
    pub version: String,
    pub device_id: String,
    pub firmware_version: String,
    pub remaining_food_grams: u32,
    pub battery_percent: u8,
    pub bowl_weight_grams: u32,
    pub uptime_seconds: u64,
    pub reported_at: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn dispense_command_roundtrip() {
        let cmd = DeviceCommand {
            version: "1.0".into(),
            device_command_id: "cmd_01".into(),
            feed_request_id: "feed_01".into(),
            command: DeviceCommandKind::Dispense,
            params: Some(DispenseParams {
                amount_grams: 5,
                motor_duration_ms: 1200,
            }),
            expires_at: "2026-05-25T04:00:30Z".into(),
            nonce: "rnd".into(),
            signature: "sig".into(),
        };
        let json = serde_json::to_string(&cmd).unwrap();
        let back: DeviceCommand = serde_json::from_str(&json).unwrap();
        assert_eq!(cmd, back);
    }

    #[test]
    fn ack_status_lowercase() {
        let ack = DeviceAck {
            version: "1.0".into(),
            device_command_id: "cmd_01".into(),
            status: AckStatus::Succeeded,
            actual_amount_grams: 5,
            remaining_food_grams: 800,
            executed_at: "2026-05-25T04:00:12Z".into(),
            errors: vec![],
        };
        let json = serde_json::to_string(&ack).unwrap();
        assert!(json.contains("\"status\":\"succeeded\""));
    }
}
