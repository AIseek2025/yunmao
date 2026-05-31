use std::sync::Arc;
use yunmao_common::telemetry;
use yunmao_device_edge::{server::run, DeviceEdgeConfig};
use yunmao_eventbus::{Backend, Config as BusConfig};

/// 入口：根据 `YUNMAO_EVENT_BUS` 选择模式：
///
/// - `http`（默认 / PoC）：纯 HTTP，Go 端 POST 指令进来，HTTP POST ack 回去；
/// - `kafka`：Kafka consumer 订阅 `feed.command.requested`，Kafka producer 推送 `feed.command.acked`，
///   HTTP server 仍然启动便于人工 PoC。
#[tokio::main]
async fn main() -> anyhow::Result<()> {
    telemetry::init("device-edge");

    let mode = std::env::var("YUNMAO_EVENT_BUS").unwrap_or_else(|_| "http".into());
    let cfg = DeviceEdgeConfig {
        listen_addr: std::env::var("YUNMAO_DEVICE_EDGE_LISTEN")
            .unwrap_or_else(|_| "0.0.0.0:8091".into()),
        ack_endpoint: std::env::var("YUNMAO_FEED_ACK_URL").unwrap_or_default(),
    };

    if mode == "kafka" {
        let brokers = std::env::var("YUNMAO_KAFKA_BROKERS")
            .unwrap_or_else(|_| "localhost:9092".into())
            .split(',')
            .map(str::to_string)
            .collect();
        let bus = yunmao_eventbus::open(BusConfig {
            backend: Backend::Kafka,
            brokers,
            client_id: "device-edge".into(),
        })
        .await?;
        tracing::info!("device-edge running in kafka mode");
        let sink = Arc::new(yunmao_device_edge::kafka_runtime::KafkaAckSink { bus: bus.clone() });
        let scheduler = yunmao_device_edge::DeviceScheduler::new(sink);
        yunmao_device_edge::kafka_runtime::spawn_consumer(bus, scheduler.clone()).await?;
        // 仍然启动 HTTP 便于 health/PoC 用
        yunmao_device_edge::server::run_with_scheduler(cfg, scheduler).await
    } else {
        tracing::info!(?cfg, "device-edge running in http mode");
        run(cfg).await
    }
}
