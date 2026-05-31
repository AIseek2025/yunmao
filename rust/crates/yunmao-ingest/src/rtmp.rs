//! 基于 [`rml_rtmp`] 的最小 RTMP 服务器实现。
//!
//! 当前能力：
//!
//! - 完成 RTMP 握手（[`rml_rtmp::handshake::Handshake`]）。
//! - 处理 connect / releaseStream / FCPublish / createStream / publish。
//! - 接收 metadata、AVC sequence header、视频/音频 tag，把它们以 [`crate::flv::FlvTag`] 投递到 [`crate::pipe::MediaPipe`]。
//! - 推流 URL 形如 `rtmp://host:port/live/{room_id}`：`live` 为 app name，`room_id` 为 stream key。
//!
//! 暂未做的：
//!
//! - 推流鉴权 token（接到 Go room-svc 后再补；目前任何 stream key 都接受）。
//! - SRT/WHIP（在 04 第 13 节中标 TODO）。

use std::sync::Arc;
use std::time::Duration;

use bytes::Bytes;
use rml_rtmp::handshake::{Handshake, HandshakeProcessResult, PeerType};
use rml_rtmp::sessions::{
    ServerSession, ServerSessionConfig, ServerSessionEvent, ServerSessionResult, StreamMetadata,
};
use rml_rtmp::time::RtmpTimestamp;
use thiserror::Error;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpListener, TcpStream};
use tokio::time::timeout;
use tracing::{debug, info, warn};

use crate::flv::{FlvTag, TagKind};
use crate::pipe::MediaPipe;
use crate::router::{PublishRouter, RoomKey};

#[derive(Debug, Error)]
pub enum RtmpError {
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
    #[error("handshake: {0}")]
    Handshake(String),
    #[error("rtmp session: {0}")]
    Session(String),
    #[error("publish rejected: {0}")]
    Rejected(String),
    #[error("connection closed by peer")]
    ConnectionClosed,
    #[error("operation timed out")]
    Timeout,
}

/// RTMP 接入服务器配置。
#[derive(Debug, Clone)]
pub struct RtmpServerConfig {
    /// 监听地址，默认 `0.0.0.0:1935`
    pub listen_addr: String,
    /// 单连接握手与首次 connect 的超时时间（防慢连接）
    pub initial_timeout: Duration,
    /// 接受的 app name（推流 URL 的第一段）。默认 "live"。
    pub allowed_app: String,
}

impl Default for RtmpServerConfig {
    fn default() -> Self {
        Self {
            listen_addr: "0.0.0.0:1935".into(),
            initial_timeout: Duration::from_secs(15),
            allowed_app: "live".into(),
        }
    }
}

#[derive(Clone)]
pub struct RtmpServer {
    config: Arc<RtmpServerConfig>,
    router: PublishRouter,
}

impl RtmpServer {
    pub fn new(config: RtmpServerConfig, router: PublishRouter) -> Self {
        Self {
            config: Arc::new(config),
            router,
        }
    }

    pub fn router(&self) -> PublishRouter {
        self.router.clone()
    }

    /// 启动监听并 accept 连接，每路连接 spawn 一个独立 task。
    pub async fn run(self) -> Result<(), RtmpError> {
        let listener = TcpListener::bind(&self.config.listen_addr).await?;
        info!(addr = %self.config.listen_addr, "RTMP server listening");
        loop {
            let (sock, peer) = listener.accept().await?;
            let server = self.clone();
            tokio::spawn(async move {
                if let Err(e) = server.handle_connection(sock).await {
                    warn!(?peer, error = %e, "RTMP connection ended with error");
                } else {
                    debug!(?peer, "RTMP connection closed cleanly");
                }
            });
        }
    }

    async fn handle_connection(self, mut sock: TcpStream) -> Result<(), RtmpError> {
        // 1. 握手
        let mut hs = Handshake::new(PeerType::Server);
        let mut buf = vec![0u8; 4096];
        let remaining_after_hs = loop {
            let n = timeout(self.config.initial_timeout, sock.read(&mut buf))
                .await
                .map_err(|_| RtmpError::Timeout)??;
            if n == 0 {
                return Err(RtmpError::ConnectionClosed);
            }
            match hs.process_bytes(&buf[..n]) {
                Ok(HandshakeProcessResult::InProgress { response_bytes }) => {
                    if !response_bytes.is_empty() {
                        sock.write_all(&response_bytes).await?;
                    }
                }
                Ok(HandshakeProcessResult::Completed {
                    response_bytes,
                    remaining_bytes,
                }) => {
                    if !response_bytes.is_empty() {
                        sock.write_all(&response_bytes).await?;
                    }
                    break remaining_bytes;
                }
                Err(e) => return Err(RtmpError::Handshake(format!("{e:?}"))),
            }
        };

        // 2. 创建 ServerSession
        let config = ServerSessionConfig::new();
        let (mut session, initial_results) =
            ServerSession::new(config).map_err(|e| RtmpError::Session(format!("{e:?}")))?;
        self.process_results(
            &mut sock,
            &mut session,
            initial_results,
            &mut SessionState::default(),
        )
        .await?;

        let mut state = SessionState::default();
        if !remaining_after_hs.is_empty() {
            let results = session
                .handle_input(&remaining_after_hs)
                .map_err(|e| RtmpError::Session(format!("{e:?}")))?;
            self.process_results(&mut sock, &mut session, results, &mut state)
                .await?;
        }

        // 3. 主循环
        loop {
            let n = sock.read(&mut buf).await?;
            if n == 0 {
                debug!("publisher disconnected");
                break;
            }
            let results = session
                .handle_input(&buf[..n])
                .map_err(|e| RtmpError::Session(format!("{e:?}")))?;
            self.process_results(&mut sock, &mut session, results, &mut state)
                .await?;
        }

        // 4. 清理：从 router 中摘除推流者
        if let Some(room) = state.publishing_room.clone() {
            self.router.unregister(&room).await;
        }
        Ok(())
    }

    async fn process_results(
        &self,
        sock: &mut TcpStream,
        session: &mut ServerSession,
        results: Vec<ServerSessionResult>,
        state: &mut SessionState,
    ) -> Result<(), RtmpError> {
        // 把 results 平铺成两层任务列表，避免 process_results 与 handle_event 互相 await 形成的递归 future。
        let mut queue: Vec<ServerSessionResult> = results;
        while let Some(r) = queue.pop() {
            match r {
                ServerSessionResult::OutboundResponse(packet) => {
                    sock.write_all(&packet.bytes).await?;
                }
                ServerSessionResult::RaisedEvent(evt) => {
                    let mut more = self.handle_event(sock, session, evt, state).await?;
                    queue.append(&mut more);
                }
                ServerSessionResult::UnhandleableMessageReceived(_) => {
                    // 忽略未识别的消息（如 fcSubscribe 等）
                }
            }
        }
        Ok(())
    }

    async fn handle_event(
        &self,
        sock: &mut TcpStream,
        session: &mut ServerSession,
        evt: ServerSessionEvent,
        state: &mut SessionState,
    ) -> Result<Vec<ServerSessionResult>, RtmpError> {
        let _ = sock; // sock 仅用于 process_results；这里只产生新的 result vec
        match evt {
            ServerSessionEvent::ConnectionRequested {
                request_id,
                app_name,
            } => {
                if app_name != self.config.allowed_app {
                    warn!(app_name, "rejecting connection: unknown app");
                    let pkts = session
                        .reject_request(request_id, "NetConnection.Connect.Rejected", "unknown app")
                        .map_err(|e| RtmpError::Session(format!("{e:?}")))?;
                    Ok(pkts)
                } else {
                    info!(app = %app_name, "RTMP connect accepted");
                    let pkts = session
                        .accept_request(request_id)
                        .map_err(|e| RtmpError::Session(format!("{e:?}")))?;
                    Ok(pkts)
                }
            }
            ServerSessionEvent::ReleaseStreamRequested { request_id, .. } => {
                let pkts = session
                    .accept_request(request_id)
                    .map_err(|e| RtmpError::Session(format!("{e:?}")))?;
                Ok(pkts)
            }
            ServerSessionEvent::PublishStreamRequested {
                request_id,
                app_name: _,
                stream_key,
                mode: _,
            } => {
                let room = RoomKey::new(stream_key.clone());
                let pipe = self.router.register_publisher(room.clone()).await;
                state.publishing_room = Some(room);
                state.pipe = Some(pipe);
                info!(stream_key = %stream_key, "RTMP publish accepted");
                let pkts = session
                    .accept_request(request_id)
                    .map_err(|e| RtmpError::Session(format!("{e:?}")))?;
                Ok(pkts)
            }
            ServerSessionEvent::PublishStreamFinished { stream_key, .. } => {
                info!(stream_key = %stream_key, "publish finished");
                if let Some(room) = state.publishing_room.take() {
                    self.router.unregister(&room).await;
                }
                Ok(Vec::new())
            }
            ServerSessionEvent::StreamMetadataChanged { metadata, .. } => {
                if let Some(pipe) = &state.pipe {
                    let bytes = encode_metadata_to_amf(&metadata);
                    let tag = FlvTag {
                        kind: TagKind::ScriptData,
                        timestamp_ms: 0,
                        data: Bytes::from(bytes),
                    };
                    pipe.publish(tag).await;
                }
                Ok(Vec::new())
            }
            ServerSessionEvent::AudioDataReceived {
                data, timestamp, ..
            } => {
                if let Some(pipe) = &state.pipe {
                    pipe.publish(FlvTag {
                        kind: TagKind::Audio,
                        timestamp_ms: timestamp.value,
                        data,
                    })
                    .await;
                }
                Ok(Vec::new())
            }
            ServerSessionEvent::VideoDataReceived {
                data, timestamp, ..
            } => {
                if let Some(pipe) = &state.pipe {
                    pipe.publish(FlvTag {
                        kind: TagKind::Video,
                        timestamp_ms: timestamp.value,
                        data,
                    })
                    .await;
                }
                Ok(Vec::new())
            }
            ServerSessionEvent::PingResponseReceived {
                timestamp: RtmpTimestamp { value },
            } => {
                debug!(rtt_ms = value, "rtmp ping response");
                Ok(Vec::new())
            }
            other => {
                debug!(?other, "ignored RTMP event");
                Ok(Vec::new())
            }
        }
    }
}

#[derive(Default)]
struct SessionState {
    publishing_room: Option<RoomKey>,
    pipe: Option<MediaPipe>,
}

/// 将 [`StreamMetadata`] 编码成 AMF0 onMetaData 帧（`onMetaData` 是 FLV ScriptData tag 的标准命名）。
fn encode_metadata_to_amf(meta: &StreamMetadata) -> Vec<u8> {
    use rml_amf0::Amf0Value;
    use std::collections::HashMap;

    let mut props: HashMap<String, Amf0Value> = HashMap::new();
    if let Some(w) = meta.video_width {
        props.insert("width".into(), Amf0Value::Number(w as f64));
    }
    if let Some(h) = meta.video_height {
        props.insert("height".into(), Amf0Value::Number(h as f64));
    }
    if let Some(fr) = meta.video_frame_rate {
        props.insert("framerate".into(), Amf0Value::Number(fr as f64));
    }
    if let Some(b) = meta.video_bitrate_kbps {
        props.insert("videodatarate".into(), Amf0Value::Number(b as f64));
    }
    if let Some(b) = meta.audio_bitrate_kbps {
        props.insert("audiodatarate".into(), Amf0Value::Number(b as f64));
    }
    if let Some(sr) = meta.audio_sample_rate {
        props.insert("audiosamplerate".into(), Amf0Value::Number(sr as f64));
    }
    if let Some(stereo) = meta.audio_is_stereo {
        props.insert("stereo".into(), Amf0Value::Boolean(stereo));
    }
    if let Some(enc) = &meta.encoder {
        props.insert("encoder".into(), Amf0Value::Utf8String(enc.clone()));
    }

    let values = vec![
        Amf0Value::Utf8String("onMetaData".into()),
        Amf0Value::Object(props),
    ];
    rml_amf0::serialize(&values).unwrap_or_default()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn metadata_encoding_carries_known_keys() {
        let mut meta = StreamMetadata::new();
        meta.video_width = Some(1280);
        meta.video_height = Some(720);
        meta.video_frame_rate = Some(30.0);
        meta.encoder = Some("obs-25".into());
        let bytes = encode_metadata_to_amf(&meta);
        // AMF0 字符串前缀是 0x02
        assert_eq!(bytes[0], 0x02);
        // 第一个字符串就是 "onMetaData"
        let len = u16::from_be_bytes([bytes[1], bytes[2]]) as usize;
        let name = std::str::from_utf8(&bytes[3..3 + len]).unwrap();
        assert_eq!(name, "onMetaData");
    }

    #[test]
    fn config_defaults_are_sensible() {
        let cfg = RtmpServerConfig::default();
        assert!(cfg.listen_addr.ends_with(":1935"));
        assert_eq!(cfg.allowed_app, "live");
    }

    #[tokio::test]
    async fn handshake_round_trip_self_test() {
        let mut client = Handshake::new(PeerType::Client);
        let mut server = Handshake::new(PeerType::Server);

        let c01 = client.generate_outbound_p0_and_p1().unwrap();
        let s_resp = match server.process_bytes(&c01).unwrap() {
            HandshakeProcessResult::InProgress { response_bytes } => response_bytes,
            other => panic!("server expected InProgress: {other:?}"),
        };
        let c2 = match client.process_bytes(&s_resp).unwrap() {
            HandshakeProcessResult::Completed { response_bytes, .. } => response_bytes,
            other => panic!("client expected Completed: {other:?}"),
        };
        let final_state = server.process_bytes(&c2).unwrap();
        assert!(matches!(
            final_state,
            HandshakeProcessResult::Completed { .. }
        ));
    }
}
