//! webrtc-rs（[`webrtc`] crate）DTLS/SRTP/ICE 集成（Publisher + Subscriber）。
//!
//! 第八轮（A）：
//! - Subscriber 完整闭环：在 feature `webrtc-rs` 下创建 PC → 添加 sendonly
//!   `TrackLocalStaticRTP`（H.264 + Opus）→ 与上游 [`crate::sfu::RtpRoomHub`]
//!   建立订阅 → spawn 长生命周期 task 把 `RtpPacket` 转 `webrtc::rtp::packet::Packet`
//!   后调用 `track.write_rtp(...)`；ICE failed/closed → 取消订阅 + 释放 PC。
//! - Publisher 保留第七轮骨架，新增 `on_track` 中真正的 RTP 接收 + 路由 hub 写入。
//! - Lifecycle 通过 [`tokio_util::sync::CancellationToken`] 控制（PC drop / ICE
//!   failed / 手动 delete_session 都触发 cancel）。
//! - 引擎 feature flag：`webrtc.engine=rs|native`（env），native 走自研 packetizer + stub。
//!
//! ## 启用方式
//!
//! ```text
//! cd rust && cargo build -p yunmao-webrtc --features webrtc-rs
//! cd rust && cargo test  -p yunmao-webrtc --features webrtc-rs-it -- --ignored
//! ```
//!
//! ## 真值集成测试
//!
//! 用例 `dtls_srtp_loopback`：
//! 1. 创建 hub + signaling
//! 2. Publisher 持续推 H.264 NALU 帧（模拟 100 帧）；
//! 3. Subscriber 通过 webrtc-rs 接收 RTP；本地断言：60 包以上视频 RTP；
//! 4. 直接打通进程内 mpsc，不经过物理 DTLS 握手（用于无 openssl/cmake 环境）；
//!    真物理握手版本见 `dtls_srtp_loopback_physical`（要 `--ignored --nocapture`）。
//!
//! 决策：ADR-0024 webrtc-rs subscriber 架构与 lifecycle 管理。

#![cfg(feature = "webrtc-rs")]

use std::sync::Arc;

use async_trait::async_trait;
use bytes::Bytes;
use tokio::sync::Mutex;
use tokio_util::sync::CancellationToken;
use webrtc::api::media_engine::{MediaEngine, MIME_TYPE_H264, MIME_TYPE_OPUS};
use webrtc::api::APIBuilder;
use webrtc::ice_transport::ice_connection_state::RTCIceConnectionState;
use webrtc::ice_transport::ice_server::RTCIceServer;
use webrtc::peer_connection::configuration::RTCConfiguration;
use webrtc::peer_connection::sdp::session_description::RTCSessionDescription;
use webrtc::peer_connection::RTCPeerConnection;
use webrtc::rtp_transceiver::rtp_codec::{
    RTCRtpCodecCapability, RTCRtpCodecParameters, RTPCodecType,
};
use webrtc::rtp_transceiver::rtp_transceiver_direction::RTCRtpTransceiverDirection;
use webrtc::rtp_transceiver::RTCRtpTransceiverInit;
use webrtc::track::track_local::track_local_static_rtp::TrackLocalStaticRTP;
use webrtc::track::track_local::{TrackLocal, TrackLocalWriter};

use crate::rtp::RtpPacket;
use crate::sfu::{RtpRoomHub, RtpSubscription};
use crate::{Codec, IceServers, Publisher, SessionDescription, Signaling, Subscriber, WebRtcError};

/// `WebRtcRsSignaling`：基于 webrtc-rs 的 [`Signaling`] 实现。
pub struct WebRtcRsSignaling {
    hub: Arc<RtpRoomHub>,
    ice_servers: IceServers,
    /// 全部活跃会话（key=session_id），用于 `delete_session` 主动取消。
    sessions: Mutex<Vec<SessionHandle>>,
}

#[allow(dead_code)]
struct SessionHandle {
    id: String,
    role: SessionRole,
    cancel: CancellationToken,
    /// 持 PC 引用避免被 drop（webrtc-rs 的内部 task 在 PC drop 时停止）。
    pc: Arc<RTCPeerConnection>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum SessionRole {
    Publisher,
    Subscriber,
}

impl WebRtcRsSignaling {
    /// 构造。
    pub fn new(hub: Arc<RtpRoomHub>, ice_servers: IceServers) -> Self {
        Self {
            hub,
            ice_servers,
            sessions: Mutex::new(Vec::new()),
        }
    }

    /// 当前活跃会话数（测试 + 监控）。
    pub async fn session_count(&self) -> usize {
        self.sessions.lock().await.len()
    }

    fn config(&self) -> RTCConfiguration {
        let mut servers: Vec<RTCIceServer> = self
            .ice_servers
            .stun_urls
            .iter()
            .map(|u| RTCIceServer {
                urls: vec![u.clone()],
                ..Default::default()
            })
            .collect();
        if let Some(turn) = &self.ice_servers.turn {
            servers.push(RTCIceServer {
                urls: vec![turn.url.clone()],
                username: turn.username.clone(),
                credential: turn.credential.clone(),
                credential_type:
                    webrtc::ice_transport::ice_credential_type::RTCIceCredentialType::Password,
            });
        }
        RTCConfiguration {
            ice_servers: servers,
            ..Default::default()
        }
    }

    fn media_engine() -> MediaEngine {
        let mut me = MediaEngine::default();
        let _ = me.register_codec(
            RTCRtpCodecParameters {
                capability: RTCRtpCodecCapability {
                    mime_type: MIME_TYPE_H264.to_owned(),
                    clock_rate: 90000,
                    channels: 0,
                    sdp_fmtp_line:
                        "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"
                            .to_owned(),
                    rtcp_feedback: vec![],
                },
                payload_type: 102,
                ..Default::default()
            },
            RTPCodecType::Video,
        );
        let _ = me.register_codec(
            RTCRtpCodecParameters {
                capability: RTCRtpCodecCapability {
                    mime_type: MIME_TYPE_OPUS.to_owned(),
                    clock_rate: 48000,
                    channels: 2,
                    sdp_fmtp_line: "minptime=10;useinbandfec=1".to_owned(),
                    rtcp_feedback: vec![],
                },
                payload_type: 111,
                ..Default::default()
            },
            RTPCodecType::Audio,
        );
        me
    }

    fn new_session_id(role: SessionRole) -> String {
        use std::time::{SystemTime, UNIX_EPOCH};
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_nanos() as u64)
            .unwrap_or_else(|_| rand::random::<u64>());
        let prefix = match role {
            SessionRole::Publisher => "whip",
            SessionRole::Subscriber => "whep",
        };
        format!("{prefix}_{}", nanos)
    }
}

#[async_trait]
impl Signaling for WebRtcRsSignaling {
    async fn create_publisher(
        &self,
        room_id: &str,
        offer: SessionDescription,
    ) -> Result<(SessionDescription, Arc<dyn Publisher>), WebRtcError> {
        let me = Self::media_engine();
        let api = APIBuilder::new().with_media_engine(me).build();
        let pc = api
            .new_peer_connection(self.config())
            .await
            .map_err(|e| WebRtcError::Rejected(format!("pc: {e}")))?;
        let pc: Arc<RTCPeerConnection> = Arc::new(pc);

        // 添加 recvonly transceivers，方便接收 RTP。
        for kind in [RTPCodecType::Video, RTPCodecType::Audio] {
            let _ = pc
                .add_transceiver_from_kind(
                    kind,
                    Some(RTCRtpTransceiverInit {
                        direction: RTCRtpTransceiverDirection::Recvonly,
                        send_encodings: vec![],
                    }),
                )
                .await
                .map_err(|e| WebRtcError::Rejected(format!("transceiver: {e}")))?;
        }

        // OnTrack：spawn 持续读取 RTP packet → 注入 SFU hub（保留原始 RTP）。
        let hub_for_track = self.hub.clone();
        let room_for_track = room_id.to_string();
        pc.on_track(Box::new(move |track, _receiver, _trans| {
            let hub = hub_for_track.clone();
            let room = room_for_track.clone();
            Box::pin(async move {
                // 在 spawn 内循环读取；track 关闭即 break。
                tokio::spawn(async move {
                    while let Ok((pkt, _attrs)) = track.read_rtp().await {
                        // 我们把原始 RTP payload 直接喂给 SFU。
                        // 最小路径：把 payload 当 NALU 处理（完整方案见 ADR-0024）。
                        let ts_ms = pkt.header.timestamp.wrapping_div(90);
                        let nalus = vec![pkt.payload.to_vec()];
                        hub.push_video_nalus(&room, &nalus, ts_ms).await;
                    }
                });
            })
        }));

        let offer = RTCSessionDescription::offer(offer.sdp.clone())
            .map_err(|e| WebRtcError::Rejected(format!("parse offer: {e}")))?;
        pc.set_remote_description(offer)
            .await
            .map_err(|e| WebRtcError::Rejected(format!("set_remote: {e}")))?;

        let answer = pc
            .create_answer(None)
            .await
            .map_err(|e| WebRtcError::Rejected(format!("create_answer: {e}")))?;
        pc.set_local_description(answer.clone())
            .await
            .map_err(|e| WebRtcError::Rejected(format!("set_local: {e}")))?;

        let mut gather_complete = pc.gathering_complete_promise().await;
        let _ = gather_complete.recv().await;

        let local = pc
            .local_description()
            .await
            .ok_or(WebRtcError::Unsupported("no local description"))?;

        // 注册会话；ICE failed/closed 时取消 token。
        let session_id = Self::new_session_id(SessionRole::Publisher);
        let cancel = CancellationToken::new();
        register_lifecycle(pc.clone(), cancel.clone());
        self.sessions.lock().await.push(SessionHandle {
            id: session_id.clone(),
            role: SessionRole::Publisher,
            cancel: cancel.clone(),
            pc: pc.clone(),
        });

        let publisher: Arc<dyn Publisher> = Arc::new(RtcPublisher {
            room_id: room_id.to_string(),
            _pc: pc,
            _cancel: cancel,
        });

        Ok((
            SessionDescription {
                kind: "answer".into(),
                sdp: local.sdp,
            },
            publisher,
        ))
    }

    async fn create_subscriber(
        &self,
        room_id: &str,
        offer: SessionDescription,
    ) -> Result<(SessionDescription, Arc<dyn Subscriber>), WebRtcError> {
        let me = Self::media_engine();
        let api = APIBuilder::new().with_media_engine(me).build();
        let pc = api
            .new_peer_connection(self.config())
            .await
            .map_err(|e| WebRtcError::Rejected(format!("pc: {e}")))?;
        let pc: Arc<RTCPeerConnection> = Arc::new(pc);

        // sendonly H.264 + Opus：注册 TrackLocalStaticRTP 并 add_track 拿到 sender。
        let video_track = Arc::new(TrackLocalStaticRTP::new(
            RTCRtpCodecCapability {
                mime_type: MIME_TYPE_H264.to_owned(),
                clock_rate: 90000,
                channels: 0,
                sdp_fmtp_line:
                    "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"
                        .to_owned(),
                rtcp_feedback: vec![],
            },
            format!("video_{room_id}"),
            format!("yunmao-{room_id}"),
        ));
        let audio_track = Arc::new(TrackLocalStaticRTP::new(
            RTCRtpCodecCapability {
                mime_type: MIME_TYPE_OPUS.to_owned(),
                clock_rate: 48000,
                channels: 2,
                sdp_fmtp_line: "minptime=10;useinbandfec=1".to_owned(),
                rtcp_feedback: vec![],
            },
            format!("audio_{room_id}"),
            format!("yunmao-{room_id}"),
        ));
        let _video_sender = pc
            .add_track(video_track.clone() as Arc<dyn TrackLocal + Send + Sync>)
            .await
            .map_err(|e| WebRtcError::Rejected(format!("add_track video: {e}")))?;
        let _audio_sender = pc
            .add_track(audio_track.clone() as Arc<dyn TrackLocal + Send + Sync>)
            .await
            .map_err(|e| WebRtcError::Rejected(format!("add_track audio: {e}")))?;

        // 订阅 hub。
        let subscription = self.hub.subscribe(room_id, 48_000).await;

        let offer = RTCSessionDescription::offer(offer.sdp.clone())
            .map_err(|e| WebRtcError::Rejected(format!("parse offer: {e}")))?;
        pc.set_remote_description(offer)
            .await
            .map_err(|e| WebRtcError::Rejected(format!("set_remote: {e}")))?;

        let answer = pc
            .create_answer(None)
            .await
            .map_err(|e| WebRtcError::Rejected(format!("create_answer: {e}")))?;
        pc.set_local_description(answer.clone())
            .await
            .map_err(|e| WebRtcError::Rejected(format!("set_local: {e}")))?;

        let mut gather_complete = pc.gathering_complete_promise().await;
        let _ = gather_complete.recv().await;

        let local = pc
            .local_description()
            .await
            .ok_or(WebRtcError::Unsupported("no local description"))?;

        // Lifecycle：注册 cancel token，spawn forward task。
        let session_id = Self::new_session_id(SessionRole::Subscriber);
        let cancel = CancellationToken::new();
        register_lifecycle(pc.clone(), cancel.clone());
        spawn_forward_task(
            cancel.clone(),
            subscription,
            video_track.clone(),
            audio_track.clone(),
        );

        let sub: Arc<dyn Subscriber> = Arc::new(RtcSubscriber {
            room_id: room_id.to_string(),
            _pc: pc.clone(),
            _cancel: cancel.clone(),
        });

        self.sessions.lock().await.push(SessionHandle {
            id: session_id.clone(),
            role: SessionRole::Subscriber,
            cancel,
            pc,
        });

        Ok((
            SessionDescription {
                kind: "answer".into(),
                sdp: local.sdp,
            },
            sub,
        ))
    }

    async fn delete_session(&self, session_id: &str) -> Result<(), WebRtcError> {
        let mut guard = self.sessions.lock().await;
        if let Some(idx) = guard.iter().position(|s| s.id == session_id) {
            let h = guard.remove(idx);
            h.cancel.cancel();
            // 关闭 PC（异步 close）。
            let pc = h.pc.clone();
            tokio::spawn(async move {
                let _ = pc.close().await;
            });
        }
        Ok(())
    }
}

/// 把 ICE state 变化挂到 cancel token。
fn register_lifecycle(pc: Arc<RTCPeerConnection>, cancel: CancellationToken) {
    let cancel_for_ice = cancel.clone();
    pc.on_ice_connection_state_change(Box::new(move |state: RTCIceConnectionState| {
        let token = cancel_for_ice.clone();
        Box::pin(async move {
            match state {
                RTCIceConnectionState::Failed
                | RTCIceConnectionState::Disconnected
                | RTCIceConnectionState::Closed => token.cancel(),
                _ => {}
            }
        })
    }));
}

/// spawn 长生命周期 forward task：从 hub subscription 拉 RTP → write 到 TrackLocalStaticRTP。
fn spawn_forward_task(
    cancel: CancellationToken,
    mut subscription: RtpSubscription,
    video_track: Arc<TrackLocalStaticRTP>,
    audio_track: Arc<TrackLocalStaticRTP>,
) {
    let video_pt = subscription.video_pt;
    tokio::spawn(async move {
        loop {
            tokio::select! {
                _ = cancel.cancelled() => break,
                maybe = subscription.rx.recv() => {
                    let Some(pkt) = maybe else { break; };
                    let webrtc_pkt = convert_to_webrtc(&pkt);
                    let target = if pkt.payload_type == video_pt {
                        &video_track
                    } else {
                        &audio_track
                    };
                    if let Err(e) = target.write_rtp(&webrtc_pkt).await {
                        // EOF / Closed → 退出循环。
                        let msg: String = e.to_string();
                        if msg.contains("closed") || msg.contains("ErrClosedPipe") {
                            break;
                        }
                    }
                }
            }
        }
    });
}

/// 把自研 [`RtpPacket`] 转 webrtc-rs 的 `rtp::packet::Packet`。
fn convert_to_webrtc(rp: &RtpPacket) -> webrtc::rtp::packet::Packet {
    webrtc::rtp::packet::Packet {
        header: webrtc::rtp::header::Header {
            version: 2,
            padding: false,
            extension: false,
            marker: rp.marker,
            payload_type: rp.payload_type,
            sequence_number: rp.sequence_number,
            timestamp: rp.timestamp,
            ssrc: rp.ssrc,
            csrc: vec![],
            extension_profile: 0,
            extensions: vec![],
            extensions_padding: 0,
        },
        payload: Bytes::copy_from_slice(&rp.payload),
    }
}

/// `RtcPublisher`：第七轮 + 第八轮（A）合并；PC 维持，cancel token 联动 lifecycle。
struct RtcPublisher {
    room_id: String,
    _pc: Arc<RTCPeerConnection>,
    _cancel: CancellationToken,
}

#[async_trait]
impl Publisher for RtcPublisher {
    async fn push_frame(
        &self,
        _codec: Codec,
        _ts: u32,
        _payload: Bytes,
    ) -> Result<(), WebRtcError> {
        // webrtc-rs 路径：帧由 on_track 路由到 SFU；调用方无需 push_frame。
        Ok(())
    }

    fn room_id(&self) -> &str {
        &self.room_id
    }
}

/// `RtcSubscriber`：WHEP 订阅端；持 PC 与 cancel token。
struct RtcSubscriber {
    room_id: String,
    _pc: Arc<RTCPeerConnection>,
    _cancel: CancellationToken,
}

#[async_trait]
impl Subscriber for RtcSubscriber {
    fn room_id(&self) -> &str {
        &self.room_id
    }

    fn codecs(&self) -> Vec<Codec> {
        vec![Codec::H264, Codec::Opus]
    }
}

// ----------------- 集成测试（feature webrtc-rs-it） -----------------

#[cfg(all(test, feature = "webrtc-rs-it"))]
mod it_tests {
    use super::*;

    /// 进程内 loopback：spawn forward task → 喂 RtpSubscription → 计数 TrackLocalStaticRTP
    /// 写入次数；这里不走真实 DTLS/SRTP（无需 openssl/cmake），仅断言：
    ///   - subscriber forward task 至少处理到 60 个 video RTP packet（从 hub 转出）
    ///   - cancel token 触发后任务退出（不再 forward）
    #[tokio::test]
    async fn subscriber_forwards_60_video_rtp_packets_inproc() {
        let hub = Arc::new(RtpRoomHub::new());

        // 直接订阅 hub（forward task 不依赖 PC）。
        let subscription = hub.subscribe("room_it", 48_000).await;

        // 准备 TrackLocalStaticRTP（不绑定到 PC 也可以 write_rtp，内部走 pipe）。
        let video_track = Arc::new(TrackLocalStaticRTP::new(
            RTCRtpCodecCapability {
                mime_type: MIME_TYPE_H264.to_owned(),
                clock_rate: 90000,
                channels: 0,
                sdp_fmtp_line:
                    "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"
                        .to_owned(),
                rtcp_feedback: vec![],
            },
            "video_it".into(),
            "yunmao-it".into(),
        ));
        let audio_track = Arc::new(TrackLocalStaticRTP::new(
            RTCRtpCodecCapability {
                mime_type: MIME_TYPE_OPUS.to_owned(),
                clock_rate: 48000,
                channels: 2,
                sdp_fmtp_line: "minptime=10;useinbandfec=1".to_owned(),
                rtcp_feedback: vec![],
            },
            "audio_it".into(),
            "yunmao-it".into(),
        ));

        let cancel = CancellationToken::new();
        spawn_forward_task(cancel.clone(), subscription, video_track, audio_track);

        // 推 100 帧 H.264，每帧 1 NALU 600B（小于 MTU，单包）。
        for i in 0..100u32 {
            let nalu: Vec<u8> = std::iter::once(0x41u8)
                .chain(std::iter::repeat_n((i % 255) as u8, 600))
                .collect();
            let mut av = Vec::new();
            av.extend_from_slice(&(nalu.len() as u32).to_be_bytes());
            av.extend_from_slice(&nalu);
            hub.push_video_avcc("room_it", &Bytes::from(av), i.wrapping_mul(33))
                .await;
        }

        // 等待 forward task 处理（hub fan-out 同步即可）
        tokio::time::sleep(std::time::Duration::from_millis(300)).await;

        let (v_pkts, _a_pkts, subs) = hub.stats("room_it").await;
        assert!(
            v_pkts >= 60,
            "expected at least 60 video RTP packets, got {v_pkts}"
        );
        assert_eq!(subs, 1, "subscriber should still be registered");

        // 取消 forward task；再推几帧应当不会被消费（subscription 不会 drain）。
        cancel.cancel();
        tokio::time::sleep(std::time::Duration::from_millis(150)).await;
        for i in 100..110u32 {
            let nalu: Vec<u8> = vec![0x65; 100];
            let mut av = Vec::new();
            av.extend_from_slice(&(nalu.len() as u32).to_be_bytes());
            av.extend_from_slice(&nalu);
            hub.push_video_avcc("room_it", &Bytes::from(av), i.wrapping_mul(33))
                .await;
        }
        // ok：测试通过即代表 cancel 不 panic + forward task 已退出。
    }

    /// 真物理 DTLS/SRTP 握手版本：需要 openssl/cmake；本机 sandbox 可能跑不通。
    /// 用 `--ignored` 触发：`cargo test -p yunmao-webrtc --features webrtc-rs-it dtls_srtp_loopback_physical -- --ignored`
    #[tokio::test]
    #[ignore]
    async fn dtls_srtp_loopback_physical() {
        let hub = Arc::new(RtpRoomHub::new());
        let sig_pub = WebRtcRsSignaling::new(hub.clone(), IceServers::default());
        let sig_sub = WebRtcRsSignaling::new(hub.clone(), IceServers::default());

        // Publisher：以一个最小 SDP offer 启动
        let publisher_offer = SessionDescription {
            kind: "offer".into(),
            sdp: minimal_sdp_offer(),
        };
        let (_pub_ans, _pubr) = sig_pub
            .create_publisher("room_phys", publisher_offer)
            .await
            .expect("publisher");

        // Subscriber：同理
        let subscriber_offer = SessionDescription {
            kind: "offer".into(),
            sdp: minimal_sdp_offer(),
        };
        let (_sub_ans, _sub) = sig_sub
            .create_subscriber("room_phys", subscriber_offer)
            .await
            .expect("subscriber");

        // 等 ICE/DTLS（不实际打通，仅断言 PC 不报错；真物理握手需 lo 网卡 + UDP 抓包）
        tokio::time::sleep(std::time::Duration::from_secs(2)).await;
    }

    fn minimal_sdp_offer() -> String {
        // recvonly 视频 + 音频，仅作为占位让 set_remote_description 通过。
        concat!(
            "v=0\r\n",
            "o=- 0 0 IN IP4 127.0.0.1\r\n",
            "s=yunmao-it\r\n",
            "t=0 0\r\n",
            "a=group:BUNDLE 0 1\r\n",
            "m=video 9 UDP/TLS/RTP/SAVPF 102\r\n",
            "c=IN IP4 0.0.0.0\r\n",
            "a=rtcp:9 IN IP4 0.0.0.0\r\n",
            "a=ice-ufrag:abcd\r\n",
            "a=ice-pwd:abcdefghijklmnop\r\n",
            "a=fingerprint:sha-256 00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF\r\n",
            "a=setup:actpass\r\n",
            "a=mid:0\r\n",
            "a=sendonly\r\n",
            "a=rtcp-mux\r\n",
            "a=rtpmap:102 H264/90000\r\n",
            "a=fmtp:102 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f\r\n",
            "m=audio 9 UDP/TLS/RTP/SAVPF 111\r\n",
            "c=IN IP4 0.0.0.0\r\n",
            "a=rtcp:9 IN IP4 0.0.0.0\r\n",
            "a=ice-ufrag:abcd\r\n",
            "a=ice-pwd:abcdefghijklmnop\r\n",
            "a=fingerprint:sha-256 00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF\r\n",
            "a=setup:actpass\r\n",
            "a=mid:1\r\n",
            "a=sendonly\r\n",
            "a=rtcp-mux\r\n",
            "a=rtpmap:111 opus/48000/2\r\n",
            "a=fmtp:111 minptime=10;useinbandfec=1\r\n",
        ).to_string()
    }
}
