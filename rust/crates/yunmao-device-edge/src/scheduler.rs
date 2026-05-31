//! 按 device_id 串行的调度器：每台设备一个 mpsc + worker。
//!
//! 输入：[`yunmao_protocol::events::FeedCommandRequested`]
//! 输出（回调）：[`yunmao_protocol::events::FeedCommandAcked`]
//!
//! 设计理由（参见 `01-技术选型与系统架构.md` 第 5 节）：投喂在 device 维度严格串行，
//! 避免并发指令撞车 / 重复出粮。

use std::collections::{HashMap, HashSet};
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::{mpsc, Mutex, RwLock};
use tracing::{debug, info, warn};
use yunmao_protocol::events::{FeedCommandAcked, FeedCommandRequested};

use crate::simulator::{DeviceProfile, SimulatedDevice};

/// 上报回调（由 server / kafka producer 实现）。
#[async_trait]
pub trait AckSink: Send + Sync + 'static {
    async fn deliver(&self, ack: FeedCommandAcked);
}

#[derive(Default, Clone)]
pub struct LoggingSink;

#[async_trait]
impl AckSink for LoggingSink {
    async fn deliver(&self, ack: FeedCommandAcked) {
        info!(?ack, "ack delivered (logging sink)");
    }
}

/// 进程内的 device 调度器。
#[derive(Clone)]
pub struct DeviceScheduler {
    /// 按 device_id → mpsc::Sender 的映射；同一设备 push 进同一队列。
    queues: Arc<RwLock<HashMap<String, mpsc::Sender<FeedCommandRequested>>>>,
    /// 幂等缓存：已处理的 device_command_id（限定大小，避免 OOM）
    processed: Arc<Mutex<IdempotentSet>>,
    sink: Arc<dyn AckSink>,
}

impl DeviceScheduler {
    pub fn new(sink: Arc<dyn AckSink>) -> Self {
        Self {
            queues: Arc::new(RwLock::new(HashMap::new())),
            processed: Arc::new(Mutex::new(IdempotentSet::new(8192))),
            sink,
        }
    }

    /// 提交一条投喂请求；如果该设备的 worker 还没启动，会自动启动。
    pub async fn submit(&self, req: FeedCommandRequested) {
        // 幂等：已经处理过的 device_command_id 直接丢弃
        {
            let mut g = self.processed.lock().await;
            if !g.insert(req.device_command_id.clone()) {
                debug!(cmd = %req.device_command_id, "dropping duplicate command (idempotent)");
                return;
            }
        }

        let device_id = req.device_id.clone();
        let sender = {
            let r = self.queues.read().await;
            r.get(&device_id).cloned()
        };
        let sender = match sender {
            Some(s) => s,
            None => {
                let mut w = self.queues.write().await;
                if let Some(s) = w.get(&device_id) {
                    s.clone()
                } else {
                    let (tx, rx) = mpsc::channel::<FeedCommandRequested>(64);
                    let profile = DeviceProfile {
                        device_id: device_id.clone(),
                        room_id: req.room_id.clone(),
                        ..Default::default()
                    };
                    let sink = self.sink.clone();
                    tokio::spawn(run_worker(profile, rx, sink));
                    w.insert(device_id.clone(), tx.clone());
                    tx
                }
            }
        };
        if let Err(e) = sender.send(req).await {
            warn!(error = %e, device = %device_id, "queue send failed");
        }
    }

    pub async fn live_devices(&self) -> usize {
        self.queues.read().await.len()
    }
}

async fn run_worker(
    profile: DeviceProfile,
    mut rx: mpsc::Receiver<FeedCommandRequested>,
    sink: Arc<dyn AckSink>,
) {
    let mut device = SimulatedDevice::new(profile);
    while let Some(req) = rx.recv().await {
        let ack = device.execute(&req).await;
        sink.deliver(ack).await;
    }
}

/// 简化的 LRU/FIFO set；超出 capacity 时按插入顺序剔除最早元素。
struct IdempotentSet {
    set: HashSet<String>,
    order: std::collections::VecDeque<String>,
    capacity: usize,
}

impl IdempotentSet {
    fn new(capacity: usize) -> Self {
        Self {
            set: HashSet::with_capacity(capacity),
            order: Default::default(),
            capacity,
        }
    }

    fn insert(&mut self, key: String) -> bool {
        if !self.set.insert(key.clone()) {
            return false;
        }
        self.order.push_back(key);
        if self.order.len() > self.capacity {
            if let Some(old) = self.order.pop_front() {
                self.set.remove(&old);
            }
        }
        true
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::sync::Mutex as AsyncMutex;

    struct CapturingSink {
        out: Arc<AsyncMutex<Vec<FeedCommandAcked>>>,
    }

    #[async_trait]
    impl AckSink for CapturingSink {
        async fn deliver(&self, ack: FeedCommandAcked) {
            self.out.lock().await.push(ack);
        }
    }

    fn req(id: &str, device: &str, amount: u32) -> FeedCommandRequested {
        FeedCommandRequested {
            feed_request_id: format!("feed_{id}"),
            device_command_id: format!("cmd_{id}"),
            device_id: device.into(),
            room_id: "room_x".into(),
            amount_grams: amount,
            motor_duration_ms: 1200,
            expires_at: "2099-01-01T00:00:00Z".into(),
        }
    }

    #[tokio::test]
    async fn submit_processes_in_order_per_device() {
        let captured = Arc::new(AsyncMutex::new(Vec::new()));
        let sink = Arc::new(CapturingSink {
            out: captured.clone(),
        });
        let s = DeviceScheduler::new(sink);

        for i in 0..5 {
            s.submit(req(&format!("a{i}"), "dev_x", 5)).await;
        }
        // 等所有命令处理完；默认设备 profile 的最长延迟 800ms × 5 = 4s。
        for _ in 0..200 {
            if captured.lock().await.len() >= 5 {
                break;
            }
            tokio::time::sleep(std::time::Duration::from_millis(50)).await;
        }
        let g = captured.lock().await;
        assert_eq!(g.len(), 5);
        for (i, ack) in g.iter().enumerate() {
            assert_eq!(ack.device_command_id, format!("cmd_a{i}"));
        }
    }

    #[tokio::test]
    async fn idempotent_drops_duplicate_command_id() {
        let captured = Arc::new(AsyncMutex::new(Vec::new()));
        let sink = Arc::new(CapturingSink {
            out: captured.clone(),
        });
        let s = DeviceScheduler::new(sink);

        s.submit(req("dup", "dev_y", 5)).await;
        s.submit(req("dup", "dev_y", 5)).await;
        s.submit(req("dup", "dev_y", 5)).await;
        for _ in 0..40 {
            if !captured.lock().await.is_empty() {
                break;
            }
            tokio::time::sleep(std::time::Duration::from_millis(50)).await;
        }
        // 等一会儿确认没有更多 ack 进来
        tokio::time::sleep(std::time::Duration::from_millis(200)).await;
        let g = captured.lock().await;
        assert_eq!(g.len(), 1);
    }
}
