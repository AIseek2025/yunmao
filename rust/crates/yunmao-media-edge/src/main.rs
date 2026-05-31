use yunmao_common::telemetry;
use yunmao_media_edge::{server::run, MediaEdgeConfig};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    telemetry::init("media-edge");

    let config = MediaEdgeConfig {
        rtmp_listen: std::env::var("YUNMAO_RTMP_LISTEN").unwrap_or_else(|_| "0.0.0.0:1935".into()),
        http_listen: std::env::var("YUNMAO_HTTP_LISTEN").unwrap_or_else(|_| "0.0.0.0:8080".into()),
        edge_node: std::env::var("YUNMAO_EDGE_NODE").unwrap_or_else(|_| "edge-local".into()),
        ll_hls: None,
    };

    tracing::info!(?config, "media-edge starting");
    run(config).await
}
