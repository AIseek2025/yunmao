//! WebSocket 服务器（基于 `axum::extract::ws`）。

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use axum::extract::ws::{Message, WebSocket, WebSocketUpgrade};
use axum::extract::State;
use axum::http::StatusCode;
use axum::response::IntoResponse;
use axum::routing::{get, post};
use axum::{Json, Router};
use bytes::Bytes;
use futures::stream::SplitStream;
use futures::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use tokio::net::TcpListener;
use tokio::sync::mpsc;
use tracing::{debug, info, warn};
use ulid::Ulid;

use metrics_exporter_prometheus::{PrometheusBuilder, PrometheusHandle};
use yunmao_protocol::signaling::{ClientFrame, ServerFrame};

use crate::auth::{is_logged_in, Verifier};
use crate::hub::{ConnId, Hub, RoomBroadcastMessage};

#[derive(Debug, Clone)]
pub struct GatewayConfig {
    pub listen_addr: String,
    pub heartbeat_interval: Duration,
    pub heartbeat_timeout: Duration,
    pub outbound_buffer: usize,
    /// 是否允许游客订阅公开房间（只读）。
    pub allow_guest: bool,
    /// 公开房间白名单；游客可订阅这些 room_id 的 base 频道。
    pub guest_rooms: Vec<String>,
    /// JWT secret（HS256）；空表示不启用鉴权（PoC）。
    pub jwt_secret: String,
    pub jwt_issuer: Option<String>,
    /// RS256 JWKS endpoints；非空时优先使用 JWKS 校验。
    pub jwks_endpoints: Vec<String>,
    /// 跨实例 fanout 后端：None 走本地 hub；Some(url) 启用 Redis Pub/Sub。
    pub redis_url: Option<String>,
    /// 实例标识；不填则使用 ulid。
    pub instance_id: Option<String>,
}

impl Default for GatewayConfig {
    fn default() -> Self {
        Self {
            listen_addr: "0.0.0.0:8090".into(),
            heartbeat_interval: Duration::from_secs(30),
            heartbeat_timeout: Duration::from_secs(60),
            outbound_buffer: 1024,
            allow_guest: true,
            guest_rooms: vec!["room_demo".into()],
            jwt_secret: String::new(),
            jwt_issuer: None,
            jwks_endpoints: vec![],
            redis_url: None,
            instance_id: None,
        }
    }
}

#[derive(Clone)]
pub struct AppState {
    pub hub: Hub,
    pub cfg: Arc<GatewayConfig>,
    pub prom: Arc<PrometheusHandle>,
    pub verifier: Option<Verifier>,
    pub fanout: Arc<dyn crate::fanout::Fanout>,
}

#[derive(Clone, Default)]
struct ConnAuth {
    /// 通过 ClientFrame::Subscribe 携带的 token 解析得到的 claims。
    user_id: Option<String>,
    allowed_rooms: std::collections::HashSet<String>,
    logged_in: bool,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct PublishRequest {
    pub room_id: String,
    pub event_type: String,
    pub data: serde_json::Value,
}

pub async fn run(cfg: GatewayConfig) -> anyhow::Result<()> {
    let hub = Hub::new();
    run_with_hub(cfg, hub).await
}

/// 用外部已创建的 Hub 启动（Kafka 模式下与 fanout 共享）。
pub async fn run_with_hub(cfg: GatewayConfig, hub: Hub) -> anyhow::Result<()> {
    let prom = PrometheusBuilder::new().install_recorder()?;
    let verifier = if !cfg.jwks_endpoints.is_empty() {
        info!(jwks = ?cfg.jwks_endpoints, "gateway verifier=jwks/RS256");
        Some(Verifier::from_jwks(
            cfg.jwks_endpoints.clone(),
            cfg.jwt_issuer.clone(),
        ))
    } else if !cfg.jwt_secret.is_empty() {
        info!("gateway verifier=HS256");
        Some(Verifier::from_hs256(
            &cfg.jwt_secret,
            cfg.jwt_issuer.clone(),
        ))
    } else {
        None
    };
    let fanout: Arc<dyn crate::fanout::Fanout> = match &cfg.redis_url {
        Some(url) if !url.is_empty() => {
            let instance_id = cfg
                .instance_id
                .clone()
                .unwrap_or_else(|| Ulid::new().to_string());
            let rf = Arc::new(crate::fanout::RedisFanout::new(
                hub.clone(),
                url.clone(),
                instance_id.clone(),
            ));
            if let Err(e) = rf.clone().start().await {
                tracing::warn!(error = %e, "fanout start failed; degraded to local");
                Arc::new(crate::fanout::LocalFanout::new(hub.clone()))
            } else {
                info!(instance_id = %instance_id, "gateway fanout=redis");
                rf
            }
        }
        _ => Arc::new(crate::fanout::LocalFanout::new(hub.clone())),
    };
    let state = AppState {
        hub: hub.clone(),
        cfg: Arc::new(cfg.clone()),
        prom: Arc::new(prom),
        verifier,
        fanout,
    };

    let prom_for_route = state.prom.clone();
    let app = Router::new()
        .route("/ws", get(ws_handler))
        .route("/publish", post(publish_handler))
        .route(
            "/metrics",
            get(move || {
                let h = prom_for_route.clone();
                async move { h.render() }
            }),
        )
        .route("/healthz", get(|| async { "ok" }))
        .with_state(state);

    let addr: SocketAddr = cfg.listen_addr.parse()?;
    let listener = TcpListener::bind(addr).await?;
    info!(http = %addr, "gateway listening");
    axum::serve(listener, app).await?;
    Ok(())
}

async fn publish_handler(
    State(state): State<AppState>,
    Json(req): Json<PublishRequest>,
) -> impl IntoResponse {
    // 与 yunmao_protocol::signaling::ServerFrame::Event 一一对应
    let frame = ServerFrame::Event {
        event_type: req.event_type.clone(),
        room_id: req.room_id.clone(),
        data: req.data,
        ts: time::OffsetDateTime::now_utc().unix_timestamp_nanos() as i64 / 1_000_000,
    };
    let json = match serde_json::to_vec(&frame) {
        Ok(b) => b,
        Err(e) => return (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    };
    let payload = Bytes::from(json);
    state
        .fanout
        .publish(RoomBroadcastMessage {
            room_id: req.room_id.clone(),
            payload,
        })
        .await;
    metrics::counter!(
        "gateway_publish_total",
        "event_type" => req.event_type
    )
    .increment(1);
    Json(serde_json::json!({"queued": true})).into_response()
}

async fn ws_handler(ws: WebSocketUpgrade, State(state): State<AppState>) -> impl IntoResponse {
    ws.on_upgrade(move |socket| handle_socket(socket, state))
}

async fn handle_socket(socket: WebSocket, state: AppState) {
    let conn_id: ConnId = format!("c_{}", Ulid::new());
    let (tx, mut rx) = mpsc::channel::<Bytes>(state.cfg.outbound_buffer);
    state.hub.register(conn_id.clone(), tx).await;
    metrics::counter!("gateway_connections_total").increment(1);
    metrics::gauge!("gateway_connections_open").increment(1.0);

    let (mut sink, stream) = socket.split();

    // hello 帧
    let hello = ServerFrame::Hello {
        connection_id: conn_id.clone(),
        server_time: time::OffsetDateTime::now_utc().unix_timestamp(),
    };
    if let Ok(text) = serde_json::to_string(&hello) {
        let _ = sink.send(Message::Text(text)).await;
    }

    // 启动 reader task
    let reader_state = state.clone();
    let reader_conn_id = conn_id.clone();
    let auth_state = Arc::new(tokio::sync::RwLock::new(ConnAuth::default()));
    let reader = tokio::spawn(reader_loop(
        stream,
        reader_state,
        reader_conn_id,
        auth_state.clone(),
    ));

    // writer loop
    let cfg_writer = state.cfg.clone();
    let writer_conn = conn_id.clone();
    let mut hb = tokio::time::interval(cfg_writer.heartbeat_interval);
    hb.tick().await;
    loop {
        tokio::select! {
            biased;
            maybe = rx.recv() => match maybe {
                Some(payload) => {
                    let text = match std::str::from_utf8(&payload) {
                        Ok(s) => s.to_string(),
                        Err(_) => continue,
                    };
                    if sink.send(Message::Text(text)).await.is_err() {
                        debug!(conn = %writer_conn, "writer send failed; closing");
                        break;
                    }
                }
                None => break,
            },
            _ = hb.tick() => {
                let ping = ServerFrame::Ping {
                    ts: time::OffsetDateTime::now_utc().unix_timestamp_nanos() as i64 / 1_000_000,
                };
                if let Ok(text) = serde_json::to_string(&ping) {
                    if sink.send(Message::Text(text)).await.is_err() {
                        break;
                    }
                }
            }
        }
    }

    // 收尾
    let _ = sink.close().await;
    state.hub.deregister(&conn_id).await;
    reader.abort();
    metrics::gauge!("gateway_connections_open").decrement(1.0);
}

async fn send_err(state: &AppState, conn_id: &ConnId, code: &str, message: &str) {
    let frame = ServerFrame::Error {
        code: code.into(),
        message: message.into(),
    };
    if let Ok(b) = serde_json::to_vec(&frame) {
        state.hub.send_direct(conn_id, Bytes::from(b)).await;
    }
}

async fn reader_loop(
    mut stream: SplitStream<WebSocket>,
    state: AppState,
    conn_id: ConnId,
    auth_state: Arc<tokio::sync::RwLock<ConnAuth>>,
) {
    while let Some(msg) = stream.next().await {
        match msg {
            Ok(Message::Text(text)) => match serde_json::from_str::<ClientFrame>(&text) {
                Ok(ClientFrame::Auth { token }) => {
                    let v = match &state.verifier {
                        Some(v) => v,
                        None => {
                            send_err(&state, &conn_id, "auth_disabled", "auth not configured")
                                .await;
                            continue;
                        }
                    };
                    match v.verify(&token) {
                        Ok(claims) => {
                            let mut g = auth_state.write().await;
                            if is_logged_in(&claims) {
                                g.logged_in = true;
                                g.user_id = Some(claims.sub.clone());
                            }
                            // 房间订阅 token 直接覆盖单个 room 字段
                            if !claims.room.is_empty() {
                                g.allowed_rooms.insert(claims.room.clone());
                            }
                            metrics::counter!("gateway_auth_ok_total").increment(1);
                        }
                        Err(e) => {
                            warn!(error = %e, conn = %conn_id, "auth verify failed");
                            send_err(&state, &conn_id, "auth_invalid", &e).await;
                            metrics::counter!("gateway_auth_fail_total").increment(1);
                        }
                    }
                }
                Ok(ClientFrame::Subscribe { rooms }) => {
                    let auth = auth_state.read().await.clone();
                    let mut allowed = Vec::new();
                    let mut denied = Vec::new();
                    for r in rooms {
                        let public_ok =
                            state.cfg.allow_guest && state.cfg.guest_rooms.iter().any(|x| x == &r);
                        let room_token_ok = auth.allowed_rooms.contains(&r);
                        if public_ok || room_token_ok || auth.logged_in {
                            allowed.push(r);
                        } else {
                            denied.push(r);
                        }
                    }
                    if !denied.is_empty() {
                        send_err(
                            &state,
                            &conn_id,
                            "subscribe_forbidden",
                            &format!("missing room token for: {}", denied.join(",")),
                        )
                        .await;
                    }
                    if !allowed.is_empty() {
                        state.hub.subscribe(&conn_id, allowed.clone()).await;
                        metrics::counter!("gateway_subscribe_total")
                            .increment(allowed.len() as u64);
                        for r in &allowed {
                            state.fanout.join(r, &conn_id).await;
                        }
                        let frame = ServerFrame::Subscribed { rooms: allowed };
                        if let Ok(b) = serde_json::to_vec(&frame) {
                            state.hub.send_direct(&conn_id, Bytes::from(b)).await;
                        }
                    }
                }
                Ok(ClientFrame::Unsubscribe { rooms }) => {
                    state.hub.unsubscribe(&conn_id, rooms.clone()).await;
                    for r in &rooms {
                        state.fanout.leave(r, &conn_id).await;
                    }
                }
                Ok(ClientFrame::Ping { ts }) => {
                    let frame = ServerFrame::Pong { ts };
                    if let Ok(b) = serde_json::to_vec(&frame) {
                        state.hub.send_direct(&conn_id, Bytes::from(b)).await;
                    }
                }
                Ok(ClientFrame::Pong { .. }) => {}
                Ok(ClientFrame::Chat {
                    room_id,
                    body,
                    client_msg_id,
                }) => {
                    let auth = auth_state.read().await.clone();
                    let allowed = auth.logged_in
                        && (auth.allowed_rooms.contains(&room_id)
                            || (state.cfg.allow_guest
                                && state.cfg.guest_rooms.iter().any(|x| x == &room_id)));
                    if !allowed {
                        send_err(
                            &state,
                            &conn_id,
                            "chat_forbidden",
                            "login + room token required",
                        )
                        .await;
                        continue;
                    }
                    metrics::counter!("gateway_chat_in_total").increment(1);
                    let frame = ServerFrame::Event {
                        event_type: "chat.message.posted".into(),
                        room_id: room_id.clone(),
                        data: serde_json::json!({
                            "body": body,
                            "client_msg_id": client_msg_id,
                            "user_id": auth.user_id,
                        }),
                        ts: time::OffsetDateTime::now_utc().unix_timestamp_nanos() as i64
                            / 1_000_000,
                    };
                    if let Ok(b) = serde_json::to_vec(&frame) {
                        state
                            .fanout
                            .publish(RoomBroadcastMessage {
                                room_id,
                                payload: Bytes::from(b),
                            })
                            .await;
                    }
                }
                Err(e) => {
                    warn!(error = %e, conn = %conn_id, "bad client frame");
                }
            },
            Ok(Message::Ping(_)) | Ok(Message::Pong(_)) => {}
            Ok(Message::Close(_)) => break,
            Ok(Message::Binary(_)) => {
                debug!(conn = %conn_id, "binary frame ignored");
            }
            Err(e) => {
                debug!(error = %e, conn = %conn_id, "ws read error");
                break;
            }
        }
    }
    state.hub.deregister(&conn_id).await;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn config_default_listen() {
        let c = GatewayConfig::default();
        assert!(c.listen_addr.ends_with(":8090"));
        assert_eq!(c.outbound_buffer, 1024);
    }
}
