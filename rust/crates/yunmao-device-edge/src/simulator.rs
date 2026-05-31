//! 模拟单设备的执行行为。

use std::time::Duration;

use serde::{Deserialize, Serialize};
use yunmao_protocol::events::{AckedStatus, FeedCommandAcked, FeedCommandRequested};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DeviceProfile {
    pub device_id: String,
    pub room_id: String,
    /// 仓粮量（克）
    pub food_remaining_grams: u32,
    /// 失败概率（0..=100），用于注入失败注入；测试时常设 0。
    pub failure_rate_percent: u8,
    /// 模拟出粮时长上下界
    pub min_dispense_ms: u32,
    pub max_dispense_ms: u32,
}

impl Default for DeviceProfile {
    fn default() -> Self {
        Self {
            device_id: "dev_demo".into(),
            room_id: "room_demo".into(),
            food_remaining_grams: 1000,
            failure_rate_percent: 0,
            min_dispense_ms: 200,
            max_dispense_ms: 800,
        }
    }
}

/// 模拟设备：拿到一个 `FeedCommandRequested`，决定怎么 ack。
pub struct SimulatedDevice {
    profile: DeviceProfile,
}

impl SimulatedDevice {
    pub fn new(profile: DeviceProfile) -> Self {
        Self { profile }
    }

    pub fn profile(&self) -> &DeviceProfile {
        &self.profile
    }

    /// 异步执行：模拟的耗时由 [`DeviceProfile::min_dispense_ms`/`max_dispense_ms`] 决定。
    pub async fn execute(&mut self, req: &FeedCommandRequested) -> FeedCommandAcked {
        let dispatch = self.decide(req);
        if let Some(delay) = dispatch.delay_ms {
            tokio::time::sleep(Duration::from_millis(delay)).await;
        }
        let now = time::OffsetDateTime::now_utc();
        FeedCommandAcked {
            feed_request_id: req.feed_request_id.clone(),
            device_command_id: req.device_command_id.clone(),
            device_id: req.device_id.clone(),
            room_id: req.room_id.clone(),
            status: dispatch.status,
            actual_amount_grams: dispatch.actual_amount,
            remaining_food_grams: self.profile.food_remaining_grams,
            executed_at: now
                .format(&time::format_description::well_known::Rfc3339)
                .unwrap_or_default(),
            errors: dispatch.errors,
        }
    }

    fn decide(&mut self, req: &FeedCommandRequested) -> Dispatch {
        if self.profile.food_remaining_grams < req.amount_grams {
            return Dispatch {
                delay_ms: Some(50),
                status: AckedStatus::InsufficientFood,
                actual_amount: 0,
                errors: vec!["INSUFFICIENT_FOOD".into()],
            };
        }
        let roll = pseudo_random_percent(req.device_command_id.as_bytes());
        if roll < self.profile.failure_rate_percent {
            return Dispatch {
                delay_ms: Some(self.profile.min_dispense_ms as u64),
                status: AckedStatus::Failed,
                actual_amount: 0,
                errors: vec!["MOTOR_TIMEOUT".into()],
            };
        }
        // 成功路径
        let delay_ms = mix_delay(
            self.profile.min_dispense_ms,
            self.profile.max_dispense_ms,
            req.device_command_id.as_bytes(),
        );
        self.profile.food_remaining_grams = self
            .profile
            .food_remaining_grams
            .saturating_sub(req.amount_grams);
        Dispatch {
            delay_ms: Some(delay_ms as u64),
            status: AckedStatus::Succeeded,
            actual_amount: req.amount_grams,
            errors: Vec::new(),
        }
    }
}

#[derive(Debug)]
struct Dispatch {
    delay_ms: Option<u64>,
    status: AckedStatus,
    actual_amount: u32,
    errors: Vec<String>,
}

fn pseudo_random_percent(seed: &[u8]) -> u8 {
    // 简单 FNV-1a，无外部依赖
    let mut h: u64 = 0xcbf29ce484222325;
    for b in seed {
        h ^= *b as u64;
        h = h.wrapping_mul(0x100000001b3);
    }
    (h as u8) % 100
}

fn mix_delay(min_ms: u32, max_ms: u32, seed: &[u8]) -> u32 {
    if max_ms <= min_ms {
        return min_ms;
    }
    let span = max_ms - min_ms;
    let mut h: u64 = 0xcbf29ce484222325;
    for b in seed {
        h ^= *b as u64;
        h = h.wrapping_mul(0x100000001b3);
    }
    min_ms + (h as u32 % span)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn req(id: &str, amount: u32) -> FeedCommandRequested {
        FeedCommandRequested {
            feed_request_id: format!("feed_{id}"),
            device_command_id: format!("cmd_{id}"),
            device_id: "dev_x".into(),
            room_id: "room_x".into(),
            amount_grams: amount,
            motor_duration_ms: 1200,
            expires_at: "2099-01-01T00:00:00Z".into(),
        }
    }

    #[tokio::test]
    async fn happy_path_decreases_food() {
        let mut dev = SimulatedDevice::new(DeviceProfile {
            failure_rate_percent: 0,
            min_dispense_ms: 0,
            max_dispense_ms: 1,
            food_remaining_grams: 100,
            ..Default::default()
        });
        let ack = dev.execute(&req("a", 5)).await;
        assert!(matches!(ack.status, AckedStatus::Succeeded));
        assert_eq!(ack.actual_amount_grams, 5);
        assert_eq!(dev.profile.food_remaining_grams, 95);
    }

    #[tokio::test]
    async fn insufficient_food_returns_correct_status() {
        let mut dev = SimulatedDevice::new(DeviceProfile {
            failure_rate_percent: 0,
            food_remaining_grams: 3,
            min_dispense_ms: 0,
            max_dispense_ms: 1,
            ..Default::default()
        });
        let ack = dev.execute(&req("a", 5)).await;
        assert!(matches!(ack.status, AckedStatus::InsufficientFood));
        assert_eq!(ack.actual_amount_grams, 0);
        assert_eq!(dev.profile.food_remaining_grams, 3);
    }

    #[tokio::test]
    async fn failure_rate_100_always_fails() {
        let mut dev = SimulatedDevice::new(DeviceProfile {
            failure_rate_percent: 100,
            food_remaining_grams: 100,
            min_dispense_ms: 0,
            max_dispense_ms: 1,
            ..Default::default()
        });
        let ack = dev.execute(&req("a", 5)).await;
        assert!(matches!(ack.status, AckedStatus::Failed));
    }
}
