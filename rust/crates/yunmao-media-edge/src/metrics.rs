//! Prometheus metrics handle。
//!
//! 在 [`crate::server`] 启动时初始化，作为 `/metrics` 端点的数据源。

use metrics_exporter_prometheus::{PrometheusBuilder, PrometheusHandle};

pub fn install() -> PrometheusHandle {
    PrometheusBuilder::new()
        .install_recorder()
        .expect("install prometheus recorder")
}

/// 业务指标常量名（避免拼写漂移）。
pub mod names {
    pub const FLV_SUBSCRIBERS: &str = "media_edge_flv_subscribers";
    pub const FLV_BYTES_OUT: &str = "media_edge_flv_bytes_out_total";
    pub const FLV_TAGS_OUT: &str = "media_edge_flv_tags_out_total";
    pub const QOE_REPORTS: &str = "media_edge_qoe_reports_total";
    pub const PUBLISH_ROOMS: &str = "media_edge_publish_rooms";
    pub const LL_HLS_MANIFEST_REQ: &str = "media_edge_ll_hls_manifest_requests_total";
    pub const LL_HLS_CHUNK_REQ: &str = "media_edge_ll_hls_chunk_requests_total";
    pub const LL_HLS_PART_SERVE_TOTAL: &str = "media_edge_ll_hls_part_serve_total";
    pub const LL_HLS_PLAYLIST_BLOCK_SECS: &str = "media_edge_ll_hls_playlist_block_seconds";
    pub const STREAM_START_LATENCY: &str = "media_edge_stream_start_latency_seconds";
    pub const FLV_PUBLISHERS: &str = "media_edge_flv_publishers";
    pub const FLV_BITRATE_BPS: &str = "media_edge_flv_publisher_bitrate_bps";
}
