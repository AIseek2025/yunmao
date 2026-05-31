//! QoE 上报数据结构（参考 `02-高清低延迟直播架构.md` 第 12 节）。
//!
//! 客户端通过 `POST /qoe` 把会话级 QoE 报告上来。media-edge 暂存后写到 ClickHouse / Kafka。
//! 当前 PoC 只在内存计数 + 写日志，方便联调。

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct QoeSession {
    pub session_id: String,
    pub user_id_hash: String,
    pub room_id: String,
    pub client_type: String,
    pub os: String,
    pub os_version: String,
    pub app_version: String,
    pub network_type: String,
    pub isp: Option<String>,
    pub region: Option<String>,
    pub protocol: String,
    pub profile: String,
    pub cdn_provider: Option<String>,
    pub edge_node: Option<String>,
    pub time_to_first_frame_ms: Option<u32>,
    #[serde(default)]
    pub stall_count: u32,
    #[serde(default)]
    pub stall_total_ms: u32,
    #[serde(default)]
    pub avg_bitrate_bps: u32,
    #[serde(default)]
    pub profile_switch_count: u32,
    #[serde(default)]
    pub protocol_fallback_count: u32,
    pub session_duration_ms: u32,
    pub exit_reason: Option<String>,
    pub bg_resume_success: Option<bool>,
    pub feed_overlay_delay_ms: Option<u32>,
    pub startup_failure_code: Option<String>,
    pub at: String,
    /// 第四轮新增：客户端观测到的真实分辨率（"1280x720"）。
    #[serde(default)]
    pub resolution_actual: Option<String>,
    /// 是否存在音频通道。
    #[serde(default)]
    pub audio_present: Option<bool>,
    /// 源码率（bps），由 media-edge `PublisherMetadata.source_bitrate_bps` 注入或客户端测速。
    #[serde(default)]
    pub source_bitrate_bps: Option<u32>,
    /// 启用的 ABR 档位（"src","hd720"…）。
    #[serde(default)]
    pub abr_active_ladder: Vec<String>,
    /// 第五轮新增：当前 publisher 数（含 source 直通 + 各 ladder leg）。
    #[serde(default)]
    pub abr_publisher_count: Option<u32>,
    /// 每 ladder 的实测平均码率（bps），按 label → bitrate。
    #[serde(default)]
    pub abr_ladder_bitrate_bps: Vec<LadderBitrateSample>,
    /// 转码 worker 当前是否在跑（true=至少一 leg 有 traffic）。
    #[serde(default)]
    pub transcode_worker_busy: Option<bool>,
    /// 转码 leg 重启次数（按 label）。
    #[serde(default)]
    pub transcode_restarts: Vec<LadderRestartSample>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LadderBitrateSample {
    pub label: String,
    pub bitrate_bps: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LadderRestartSample {
    pub label: String,
    pub restarts: u64,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn qoe_minimum_fields_roundtrip() {
        let raw = r#"{
            "session_id":"sess_01",
            "user_id_hash":"u_h",
            "room_id":"room_demo",
            "client_type":"web",
            "os":"macOS",
            "os_version":"26.5",
            "app_version":"web-0.0.1",
            "network_type":"wifi",
            "protocol":"http-flv",
            "profile":"src",
            "session_duration_ms":12345,
            "at":"2026-05-25T00:00:00Z"
        }"#;
        let s: QoeSession = serde_json::from_str(raw).unwrap();
        assert_eq!(s.protocol, "http-flv");
        assert_eq!(s.stall_count, 0);
    }
}
