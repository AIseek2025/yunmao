//! HTTP-FLV 出口。
//!
//! - 路由：`GET /live/{room}.flv`
//! - 行为：写出标准 FLV header + `PrevTagSize0=0` + 一系列 tag。
//! - 订阅：通过 [`yunmao_ingest::PublishRouter`] 订阅指定房间，先发 metadata + sequence header + 最近一个 GOP，
//!   再实时转发；与 `pipe::MediaSubscriber` 的语义一致，保证“快速首帧”。
//!
//! 浏览器侧：mpegts.js / flv.js 都可消费此响应，参考 `02-高清低延迟直播架构.md` 第 4 节。

use std::time::Instant;

use axum::body::Body;
use axum::extract::{Path, State};
use axum::http::{header, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::Json;
use bytes::Bytes;
use futures::stream::{self, Stream};
use tracing::{debug, info, warn};

use yunmao_ingest::flv::{FlvHeader, FlvTag};
use yunmao_ingest::pipe::MediaSubscriber;
use yunmao_ingest::router::{PublishRouter, RoomKey};

use crate::metrics::names;
use crate::qoe::QoeSession;

#[derive(Clone)]
pub struct AppState {
    pub router: PublishRouter,
    pub edge_node: String,
}

pub async fn flv_handler(State(state): State<AppState>, Path(filename): Path<String>) -> Response {
    let room = match strip_flv_suffix(&filename) {
        Some(r) => RoomKey::new(r),
        None => return (StatusCode::BAD_REQUEST, "expected room.flv").into_response(),
    };

    let pipe = match state.router.subscribe(&room).await {
        Some(p) => p,
        None => {
            return (
                StatusCode::NOT_FOUND,
                Json(yunmao_common::error::ErrorEnvelope::new(
                    yunmao_common::ErrorCode::MEDIA_STREAM_OFFLINE,
                    "stream offline",
                )),
            )
                .into_response();
        }
    };

    let subscriber = pipe.subscribe().await;
    metrics::gauge!(names::FLV_SUBSCRIBERS).increment(1.0);
    info!(room = %room.0, edge = %state.edge_node, "flv subscriber attached");

    let stream = build_flv_stream(subscriber, room.clone());
    let body = Body::from_stream(stream);
    Response::builder()
        .status(StatusCode::OK)
        .header(header::CONTENT_TYPE, "video/x-flv")
        .header(header::CACHE_CONTROL, "no-cache, no-store")
        .header("X-Edge-Node", state.edge_node)
        .body(body)
        .unwrap()
}

pub async fn qoe_handler(Json(body): Json<QoeSession>) -> Response {
    debug!(session = %body.session_id, room = %body.room_id, "qoe report received");
    metrics::counter!(names::QOE_REPORTS, "protocol" => body.protocol.clone()).increment(1);
    StatusCode::ACCEPTED.into_response()
}

fn strip_flv_suffix(s: &str) -> Option<&str> {
    s.strip_suffix(".flv")
}

struct FlvStreamState {
    subscriber: MediaSubscriber,
    room: RoomKey,
    header_sent: bool,
    started: Instant,
}

impl Drop for FlvStreamState {
    fn drop(&mut self) {
        metrics::gauge!(names::FLV_SUBSCRIBERS).decrement(1.0);
        info!(
            room = %self.room.0,
            duration_ms = self.started.elapsed().as_millis() as u64,
            "flv subscriber detached"
        );
    }
}

fn build_flv_stream(
    subscriber: MediaSubscriber,
    room: RoomKey,
) -> impl Stream<Item = Result<Bytes, std::io::Error>> + Send + 'static {
    let state = FlvStreamState {
        subscriber,
        room: room.clone(),
        header_sent: false,
        started: Instant::now(),
    };
    stream::unfold(state, |mut state| async move {
        if !state.header_sent {
            state.header_sent = true;
            let bytes = Bytes::copy_from_slice(
                &FlvHeader {
                    has_audio: true,
                    has_video: true,
                }
                .encode(),
            );
            return Some((Ok::<Bytes, std::io::Error>(bytes), state));
        }
        match state.subscriber.next_tag().await {
            Some(tag) => {
                let bytes = encode_tag_with_metrics(&tag);
                Some((Ok(bytes), state))
            }
            None => {
                warn!(room = %state.room.0, "publisher gone, closing flv stream");
                None
            }
        }
    })
}

fn encode_tag_with_metrics(tag: &FlvTag) -> Bytes {
    let bytes = tag.encode();
    metrics::counter!(names::FLV_TAGS_OUT).increment(1);
    metrics::counter!(names::FLV_BYTES_OUT).increment(bytes.len() as u64);
    bytes
}

#[cfg(test)]
mod tests {
    use bytes::Bytes;

    use super::*;
    use yunmao_ingest::flv::{FlvTag, TagKind};
    use yunmao_ingest::pipe::MediaPipe;

    fn keyframe() -> FlvTag {
        FlvTag {
            kind: TagKind::Video,
            timestamp_ms: 0,
            data: Bytes::from_static(&[0x17, 0x01, 0, 0, 0, 0xab, 0xcd]),
        }
    }

    #[tokio::test]
    async fn flv_stream_emits_header_first() {
        let pipe = MediaPipe::new();
        pipe.publish(keyframe()).await;
        let sub = pipe.subscribe().await;
        let stream = build_flv_stream(sub, RoomKey::new("room_demo"));

        use futures::StreamExt;
        let mut stream = Box::pin(stream);
        let first = stream.next().await.unwrap().unwrap();
        assert_eq!(&first[0..3], b"FLV");
        let second = stream.next().await.unwrap().unwrap();
        assert_eq!(second[0], 9); // video tag type
    }

    #[test]
    fn strip_suffix_works() {
        assert_eq!(strip_flv_suffix("room_a.flv"), Some("room_a"));
        assert_eq!(strip_flv_suffix("room_a"), None);
    }
}
