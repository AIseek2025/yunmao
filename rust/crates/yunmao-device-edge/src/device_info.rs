use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Instant;

use serde::Serialize;

#[derive(Debug, Clone, Serialize, PartialEq, Eq, Default)]
pub enum DeviceClass {
    #[default]
    HardwareFeeder,
    WebSimulator,
    MobileApp,
    MiniProgram,
}

impl DeviceClass {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::HardwareFeeder => "hardware_feeder",
            Self::WebSimulator => "web_simulator",
            Self::MobileApp => "mobile_app",
            Self::MiniProgram => "mini_program",
        }
    }
}

impl std::fmt::Display for DeviceClass {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Serialize)]
pub struct DeviceCapability {
    pub device_id: String,
    pub device_class: String,
    pub firmware_version: String,
    pub supports_push: bool,
    pub supports_background_sync: bool,
    pub max_concurrent_commands: u32,
    pub platform: String,
}

impl Default for DeviceCapability {
    fn default() -> Self {
        Self {
            device_id: String::new(),
            device_class: DeviceClass::HardwareFeeder.to_string(),
            firmware_version: "1.0.0".into(),
            supports_push: false,
            supports_background_sync: false,
            max_concurrent_commands: 1,
            platform: "unknown".into(),
        }
    }
}

pub struct DeviceHealthTracker {
    started_at: Instant,
    pub commands_processed: AtomicU64,
    pub commands_failed: AtomicU64,
}

impl DeviceHealthTracker {
    pub fn new() -> Self {
        Self {
            started_at: Instant::now(),
            commands_processed: AtomicU64::new(0),
            commands_failed: AtomicU64::new(0),
        }
    }

    pub fn mark_processed(&self) {
        self.commands_processed.fetch_add(1, Ordering::Relaxed);
    }

    pub fn mark_failed(&self) {
        self.commands_failed.fetch_add(1, Ordering::Relaxed);
    }

    pub fn uptime_secs(&self) -> u64 {
        self.started_at.elapsed().as_secs()
    }

    pub fn snapshot(&self) -> DeviceHealthSnapshot {
        DeviceHealthSnapshot {
            uptime_secs: self.uptime_secs(),
            commands_processed: self.commands_processed.load(Ordering::Relaxed),
            commands_failed: self.commands_failed.load(Ordering::Relaxed),
        }
    }
}

impl Default for DeviceHealthTracker {
    fn default() -> Self {
        Self::new()
    }
}

#[derive(Debug, Serialize)]
pub struct DeviceHealthSnapshot {
    pub uptime_secs: u64,
    pub commands_processed: u64,
    pub commands_failed: u64,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn device_class_round_trip() {
        let classes = [
            DeviceClass::HardwareFeeder,
            DeviceClass::WebSimulator,
            DeviceClass::MobileApp,
            DeviceClass::MiniProgram,
        ];
        for c in &classes {
            let s = serde_json::to_string(c.as_str()).unwrap();
            assert!(!s.is_empty());
        }
        assert_eq!(DeviceClass::HardwareFeeder.as_str(), "hardware_feeder");
        assert_eq!(DeviceClass::MiniProgram.as_str(), "mini_program");
    }

    #[test]
    fn capability_default_is_valid() {
        let c = DeviceCapability::default();
        assert_eq!(c.device_class, "hardware_feeder");
        assert_eq!(c.firmware_version, "1.0.0");
        assert_eq!(c.max_concurrent_commands, 1);
        assert!(!c.supports_push);
        assert!(!c.supports_background_sync);
    }

    #[test]
    fn capability_json_roundtrip() {
        let cap = DeviceCapability {
            device_id: "dev_001".into(),
            device_class: DeviceClass::MobileApp.to_string(),
            firmware_version: "2.1.0".into(),
            supports_push: true,
            supports_background_sync: true,
            max_concurrent_commands: 4,
            platform: "ios".into(),
        };
        let json = serde_json::to_string(&cap).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed["device_id"], "dev_001");
        assert_eq!(parsed["device_class"], "mobile_app");
        assert_eq!(parsed["max_concurrent_commands"], 4);
        assert_eq!(parsed["supports_push"], true);
    }

    #[test]
    fn health_tracker_atomic_counters() {
        let h = DeviceHealthTracker::new();
        assert_eq!(h.snapshot().commands_processed, 0);
        assert_eq!(h.snapshot().commands_failed, 0);
        h.mark_processed();
        h.mark_processed();
        h.mark_failed();
        let snap = h.snapshot();
        assert_eq!(snap.commands_processed, 2);
        assert_eq!(snap.commands_failed, 1);
        assert_eq!(h.uptime_secs(), snap.uptime_secs);
    }
}
