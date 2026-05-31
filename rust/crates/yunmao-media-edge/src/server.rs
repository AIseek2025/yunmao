//! 把 ingest（RTMP 监听）+ HTTP-FLV 出口 + LL-HLS 出口 + `/metrics` 拼成一个进程。

use std::net::SocketAddr;
use std::sync::Arc;

use axum::routing::{get, post};
use axum::Router;
use tokio::net::TcpListener;
use tracing::info;

use yunmao_ingest::router::PublishRouter;
use yunmao_ingest::rtmp::{RtmpServer, RtmpServerConfig};

use crate::http_flv::{flv_handler, qoe_handler, AppState};
use crate::ll_hls::{
    init_handler, manifest_handler, master_handler, meta_handler, part_handler, segment_handler,
    InMemoryPackager, LlHlsPackager, LlHlsParams, LlHlsState,
};
use crate::metrics::install;

#[derive(Debug, Clone)]
pub struct MediaEdgeConfig {
    pub rtmp_listen: String,
    pub http_listen: String,
    pub edge_node: String,
    /// LL-HLS 切片器参数（target / part / hold-back / window）；None 使用默认。
    pub ll_hls: Option<LlHlsParams>,
}

impl Default for MediaEdgeConfig {
    fn default() -> Self {
        Self {
            rtmp_listen: "0.0.0.0:1935".into(),
            http_listen: "0.0.0.0:8080".into(),
            edge_node: "edge-local".into(),
            ll_hls: None,
        }
    }
}

pub async fn run(config: MediaEdgeConfig) -> anyhow::Result<()> {
    let prom = install();
    let router = PublishRouter::new();

    let rtmp_cfg = RtmpServerConfig {
        listen_addr: config.rtmp_listen.clone(),
        ..Default::default()
    };
    let rtmp_server = RtmpServer::new(rtmp_cfg, router.clone());
    tokio::spawn(async move {
        if let Err(e) = rtmp_server.run().await {
            tracing::error!(error = %e, "rtmp server exited");
        }
    });

    let prom_handle = Arc::new(prom);
    let prom_for_route = prom_handle.clone();

    let packager = Arc::new(InMemoryPackager::new(config.ll_hls.unwrap_or_default()));

    // 接 ingest router → 把每个新 publish 的 FLV tag 也推给 packager。
    spawn_llhls_ingest_loop(router.clone(), packager.clone());

    let app_state = AppState {
        router,
        edge_node: config.edge_node.clone(),
    };
    let ll_state = LlHlsState {
        packager: packager.clone(),
    };
    let app = Router::new()
        .route("/live/:filename", get(flv_handler))
        .route(
            "/live/:room/index.m3u8",
            get(master_handler).with_state(ll_state.clone()),
        )
        .route(
            "/live/:room/meta.json",
            get(meta_handler).with_state(ll_state.clone()),
        )
        .route(
            "/live/:room/index_ll.m3u8",
            get(manifest_handler).with_state(ll_state.clone()),
        )
        .route(
            "/live/:room/init.mp4",
            get(init_handler).with_state(ll_state.clone()),
        )
        .route(
            "/live/:room/segment-:msn_with_ext",
            get(segment_handler).with_state(ll_state.clone()),
        )
        .route(
            "/live/:room/part-:msn_part",
            get(part_handler).with_state(ll_state.clone()),
        )
        .route("/qoe", post(qoe_handler))
        .route(
            "/metrics",
            get(move || {
                let h = prom_for_route.clone();
                async move { h.render() }
            }),
        )
        .route("/healthz", get(|| async { "ok" }))
        .with_state(app_state);

    let addr: SocketAddr = config.http_listen.parse()?;
    let listener = TcpListener::bind(addr).await?;
    info!(http = %addr, edge = %config.edge_node, "media-edge http listening");
    axum::serve(listener, app).await?;
    Ok(())
}

/// 后台任务：每隔一段时间扫描已注册房间，将其 FLV tag 转入 LL-HLS 切片器。
///
/// 当前 ingest 不提供 hook，临时实现：为每个 snapshot 房间起一个 subscriber，专门
/// 把 tag 发给 packager。生产应让 `PublishRouter` 暴露 broadcast subscription，
/// 这里只跑一个 best-effort poller，覆盖率 = 1s 内新增的房间。
fn spawn_llhls_ingest_loop(router: PublishRouter, packager: Arc<InMemoryPackager>) {
    tokio::spawn(async move {
        use std::collections::HashSet;
        let mut wired: HashSet<String> = HashSet::new();
        loop {
            for k in router.snapshot().await {
                if wired.contains(&k.0) {
                    continue;
                }
                if let Some(pipe) = router.subscribe(&k).await {
                    wired.insert(k.0.clone());
                    let room = k.0.clone();
                    let packager = packager.clone();
                    tokio::spawn(async move {
                        let mut sub = pipe.subscribe().await;
                        while let Some(tag) = sub.next_tag().await {
                            if let Err(e) = packager.ingest_flv_tag(&room, &tag).await {
                                tracing::debug!(error = %e, room=%room, "ll-hls ingest tag failed");
                            }
                        }
                    });
                }
            }
            tokio::time::sleep(std::time::Duration::from_millis(500)).await;
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn config_default_addrs() {
        let c = MediaEdgeConfig::default();
        assert!(c.rtmp_listen.ends_with(":1935"));
        assert!(c.http_listen.ends_with(":8080"));
    }
}
