//! Kafka 模式下的 device-edge 接入：
//!
//! - 订阅 `feed.command.requested`，反序列化 CloudEvents 信封 → 投喂指令；
//! - 提交到 [`DeviceScheduler`]；
//! - ack 时通过 Kafka 发布 `feed.command.acked`。
//!
//! 与 HTTP 模式同源使用同一个 [`DeviceScheduler`]，靠注入不同 `AckSink` 切换。

use std::sync::Arc;

use async_trait::async_trait;
use tracing::{info, warn};
use yunmao_common::cloudevents::CloudEvent;
use yunmao_eventbus::{topic::names, Bus, Envelope, Topic};
use yunmao_protocol::events::{FeedCommandAcked, FeedCommandRequested};

use crate::scheduler::{AckSink, DeviceScheduler};

const SOURCE: &str = "device-edge";

/// 通过 Kafka 上报 ack。
pub struct KafkaAckSink {
    pub bus: Arc<dyn Bus>,
}

#[async_trait]
impl AckSink for KafkaAckSink {
    async fn deliver(&self, ack: FeedCommandAcked) {
        let evt = CloudEvent::new(
            names::FEED_COMMAND_ACKED.to_string(),
            SOURCE.to_string(),
            Some(ack.feed_request_id.clone()),
            ack.clone(),
        );
        let topic = Topic::new(names::FEED_COMMAND_ACKED);
        match Envelope::from_cloudevent(topic.clone(), ack.device_id.clone(), &evt) {
            Ok(env) => {
                if let Err(e) = self.bus.publish(env).await {
                    warn!(error = %e, "ack publish failed");
                } else {
                    info!(req = %ack.feed_request_id, "ack published to kafka");
                }
            }
            Err(e) => warn!(error = %e, "ack envelope build failed"),
        }
    }
}

/// 启动 Kafka 消费循环：
///
/// 把 `feed.command.requested` CloudEvent 解出 `FeedCommandRequested` 后提交到 scheduler。
pub async fn spawn_consumer(bus: Arc<dyn Bus>, scheduler: DeviceScheduler) -> anyhow::Result<()> {
    let topic = Topic::new(names::FEED_COMMAND_REQUESTED);
    let sched = scheduler.clone();
    let handler: yunmao_eventbus::Handler = Arc::new(move |env: Envelope| {
        let sched = sched.clone();
        Box::pin(async move {
            let evt: CloudEvent<FeedCommandRequested> = match serde_json::from_slice(&env.payload) {
                Ok(v) => v,
                Err(e) => {
                    warn!(error = %e, "decode feed.command.requested failed");
                    return Ok(());
                }
            };
            sched.submit(evt.data).await;
            Ok(())
        })
    });
    bus.subscribe("device-edge", vec![topic], handler).await?;
    Ok(())
}
