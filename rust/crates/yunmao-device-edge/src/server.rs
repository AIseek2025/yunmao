//! HTTP 入口：让 Go 端把 `feed.command.requested` POST 进来，然后 device-edge 把 ack 再 POST 出去。
//!
//! 这是 PoC 替代 Kafka 的最小集成方式；上线后由 Kafka consumer/producer 替换。

use std::sync::Arc;

use async_trait::async_trait;
use axum::extract::State;
use axum::routing::{get, post};
use axum::{Json, Router};
use serde::Deserialize;
use serde::Serialize;
use tokio::net::TcpListener;
use tracing::{info, warn};
use yunmao_protocol::events::{FeedCommandAcked, FeedCommandRequested};

use crate::device_info::{DeviceCapability, DeviceHealthTracker};
use crate::scheduler::{AckSink, DeviceScheduler};

#[derive(Debug, Clone)]
pub struct DeviceEdgeConfig {
    pub listen_addr: String,
    /// Go feeding-svc 接收 ack 的 URL；空字符串表示只打日志（PoC 默认）。
    pub ack_endpoint: String,
}

impl Default for DeviceEdgeConfig {
    fn default() -> Self {
        Self {
            listen_addr: "0.0.0.0:8091".into(),
            ack_endpoint: std::env::var("YUNMAO_FEED_ACK_URL").unwrap_or_default(),
        }
    }
}

struct HttpAckSink {
    endpoint: String,
}

#[async_trait]
impl AckSink for HttpAckSink {
    async fn deliver(&self, ack: FeedCommandAcked) {
        if self.endpoint.is_empty() {
            info!(
                ?ack,
                "ack delivered (logging only; YUNMAO_FEED_ACK_URL unset)"
            );
            return;
        }
        // 简单 axum::Client 替代：使用 hyper + serde_json，避免再引一个 reqwest
        let body = match serde_json::to_vec(&ack) {
            Ok(v) => v,
            Err(e) => {
                warn!(error = %e, "ack serialize failed");
                return;
            }
        };
        if let Err(e) = post_raw(&self.endpoint, body).await {
            warn!(error = %e, "ack delivery failed");
        }
    }
}

async fn post_raw(url: &str, body: Vec<u8>) -> std::io::Result<()> {
    use std::io::{Error, ErrorKind};
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpStream;

    // 极简 HTTP/1.1 客户端，仅用于 PoC 内网调用；URL 形如 http://127.0.0.1:8201/internal/feed-acks
    let parsed = url
        .strip_prefix("http://")
        .ok_or_else(|| Error::new(ErrorKind::InvalidInput, "only http://"))?;
    let (host_port, path) = match parsed.split_once('/') {
        Some((h, p)) => (h.to_string(), format!("/{p}")),
        None => (parsed.to_string(), "/".to_string()),
    };
    let mut sock = TcpStream::connect(&host_port).await?;
    let mut req = Vec::with_capacity(body.len() + 256);
    req.extend_from_slice(format!("POST {path} HTTP/1.1\r\n").as_bytes());
    req.extend_from_slice(format!("Host: {host_port}\r\n").as_bytes());
    req.extend_from_slice(b"Content-Type: application/json\r\n");
    req.extend_from_slice(format!("Content-Length: {}\r\n", body.len()).as_bytes());
    req.extend_from_slice(b"Connection: close\r\n\r\n");
    req.extend_from_slice(&body);
    sock.write_all(&req).await?;
    let mut buf = Vec::new();
    let _ = sock.read_to_end(&mut buf).await; // 忽略响应，PoC
    Ok(())
}

#[derive(Clone)]
struct AppState {
    scheduler: DeviceScheduler,
    health: Arc<DeviceHealthTracker>,
    capability: DeviceCapability,
}

#[derive(Debug, Deserialize)]
struct CommandRequest {
    #[serde(flatten)]
    inner: FeedCommandRequested,
}

#[derive(Debug, Serialize)]
struct DeviceInfoResponse {
    capability: DeviceCapability,
    uptime_secs: u64,
    live_devices: usize,
}

async fn readyz() -> &'static str {
    "ready"
}

async fn device_info(State(state): State<AppState>) -> Json<DeviceInfoResponse> {
    Json(DeviceInfoResponse {
        capability: state.capability.clone(),
        uptime_secs: state.health.uptime_secs(),
        live_devices: state.scheduler.live_devices().await,
    })
}

pub async fn run(cfg: DeviceEdgeConfig) -> anyhow::Result<()> {
    let sink = Arc::new(HttpAckSink {
        endpoint: cfg.ack_endpoint.clone(),
    });
    let scheduler = DeviceScheduler::new(sink);
    run_with_scheduler(cfg, scheduler).await
}

/// 用已存在的 scheduler 启动 HTTP server（Kafka 模式下复用）。
pub async fn run_with_scheduler(
    cfg: DeviceEdgeConfig,
    scheduler: DeviceScheduler,
) -> anyhow::Result<()> {
    let state = AppState {
        scheduler,
        health: Arc::new(DeviceHealthTracker::new()),
        capability: DeviceCapability {
            device_id: std::env::var("YUNMAO_DEVICE_ID").unwrap_or_else(|_| "device-edge-0".into()),
            device_class: crate::device_info::DeviceClass::WebSimulator.to_string(),
            firmware_version: env!("CARGO_PKG_VERSION").into(),
            supports_push: true,
            supports_background_sync: false,
            max_concurrent_commands: 1,
            platform: "rust".into(),
        },
    };

    let app = Router::new()
        .route("/healthz", get(|| async { "ok" }))
        .route("/readyz", get(readyz))
        .route("/device/info", get(device_info))
        .route(
            "/commands",
            post(
                |State(state): State<AppState>, Json(req): Json<CommandRequest>| async move {
                    state.scheduler.submit(req.inner).await;
                    axum::http::StatusCode::ACCEPTED
                },
            ),
        )
        .with_state(state);

    let listener = TcpListener::bind(&cfg.listen_addr).await?;
    info!(http = %cfg.listen_addr, ack_endpoint = %cfg.ack_endpoint, "device-edge listening");
    axum::serve(listener, app).await?;
    Ok(())
}
