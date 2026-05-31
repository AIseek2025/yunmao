use yunmao_common::telemetry;
use yunmao_eventbus::{Backend, Config as BusConfig};
use yunmao_gateway::{kafka_runtime, server, Hub};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    telemetry::init("gateway");

    let cfg = server::GatewayConfig {
        listen_addr: std::env::var("YUNMAO_GATEWAY_LISTEN")
            .unwrap_or_else(|_| "0.0.0.0:8090".into()),
        allow_guest: std::env::var("YUNMAO_GATEWAY_ALLOW_GUEST")
            .map(|v| v != "false")
            .unwrap_or(true),
        guest_rooms: std::env::var("YUNMAO_GATEWAY_GUEST_ROOMS")
            .unwrap_or_else(|_| "room_demo".into())
            .split(',')
            .map(str::to_string)
            .collect(),
        jwt_secret: std::env::var("YUNMAO_JWT_SECRET").unwrap_or_default(),
        jwt_issuer: std::env::var("YUNMAO_JWT_ISSUER").ok(),
        jwks_endpoints: std::env::var("YUNMAO_JWKS_ENDPOINTS")
            .ok()
            .map(|s| {
                s.split(',')
                    .map(|p| p.trim().to_string())
                    .filter(|p| !p.is_empty())
                    .collect()
            })
            .unwrap_or_default(),
        redis_url: std::env::var("YUNMAO_GATEWAY_REDIS_URL")
            .ok()
            .filter(|s| !s.is_empty()),
        instance_id: std::env::var("YUNMAO_GATEWAY_INSTANCE_ID")
            .ok()
            .filter(|s| !s.is_empty()),
        ..Default::default()
    };

    let bus_mode = std::env::var("YUNMAO_EVENT_BUS").unwrap_or_else(|_| "memory".into());
    let hub = Hub::new();
    if bus_mode == "kafka" {
        let brokers: Vec<String> = std::env::var("YUNMAO_KAFKA_BROKERS")
            .unwrap_or_else(|_| "localhost:9092".into())
            .split(',')
            .map(str::to_string)
            .collect();
        let bus = yunmao_eventbus::open(BusConfig {
            backend: Backend::Kafka,
            brokers,
            client_id: "gateway".into(),
        })
        .await?;
        tracing::info!("gateway kafka fanout enabled");
        kafka_runtime::spawn_fanout(bus, hub.clone()).await?;
    }

    tracing::info!(listen = %cfg.listen_addr, allow_guest = cfg.allow_guest, "gateway starting");
    server::run_with_hub(cfg, hub).await
}
