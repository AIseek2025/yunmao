//! `MediaSink` trait + 房间级 [`PublisherHub`]，让 LL-HLS 切片器与 WHEP packetizer
//! 共享同一份解码前的视频 NALU + 音频 AAC frame 流。
//!
//! 设计动机（第六轮）：
//!
//! - 上轮 LL-HLS 直接持有 `InMemoryPackager`，WebRTC publisher 也单独维护一份 `Vec<frame>`，
//!   两条出口数据不一致，且 RTMP→WHEP 桥接拿不到帧。
//! - 本轮把上游（RTMP / WHIP）抽成 publisher，下游（LL-HLS / WHEP / HLS / FLV）抽成
//!   [`MediaSink`]，由 [`PublisherHub`] 在房间维度做 fan-out。
//!
//! ## 时间戳约定
//!
//! - 视频 timescale = 1000（ms），`ts_ms` 来自 RTMP 推流的 FLV tag timestamp。
//! - 音频 timescale = 该房间 AAC sample_rate；publisher 上报 `ts_ms` 一致，但 sink 自己
//!   按 sample 数推进 PTS。
//! - 推送顺序按 PTS 单调递增；hub 不重排，sink 需要容忍 1–2 帧抖动。
//!
//! ## 错误处理
//!
//! - sink 抛出错误时，hub 仅记录日志、不 retry；publisher 不感知 sink 失败。
//! - sink 被 drop 时，订阅 token 自动失效（基于 Weak 引用）。

use std::collections::HashMap;
use std::sync::{Arc, Weak};

use async_trait::async_trait;
use bytes::Bytes;
use tokio::sync::RwLock;

use crate::fmp4::{AacConfig, AacFrame, AvcConfig};

/// 一帧已编码视频（H.264 NALU，AVCC 长度前缀格式）。
#[derive(Debug, Clone)]
pub struct VideoNalu {
    /// AVCC 格式（4-byte length-prefix + NALU bytes），与 FLV body[5..] 一致。
    pub data: Bytes,
    /// FLV/RTMP timestamp（ms），timescale=1000。
    pub ts_ms: u32,
    /// 是否关键帧（IDR）。
    pub is_keyframe: bool,
}

/// 房间出向数据 sink；LL-HLS Packager / WHEP packetizer / HLS / DVR 都是 Sink。
#[async_trait]
pub trait MediaSink: Send + Sync {
    /// 视频配置（SPS/PPS 解析后）；每次配置变化都会回调一次。
    async fn on_video_config(&self, room_id: &str, cfg: &AvcConfig);
    /// 音频配置（AAC AudioSpecificConfig 解析后）。
    async fn on_audio_config(&self, room_id: &str, cfg: &AacConfig);
    /// 推一帧视频 NALU。
    async fn on_video_nalu(&self, room_id: &str, nalu: VideoNalu);
    /// 推一帧 AAC frame。
    async fn on_audio_frame(&self, room_id: &str, ts_ms: u32, frame: AacFrame);
    /// 标签：debug / metrics 用。
    fn name(&self) -> &'static str {
        "sink"
    }
}

/// 房间级 publisher hub：聚合多 sink，按房间维度 fan-out。
#[derive(Default)]
pub struct PublisherHub {
    inner: RwLock<HubInner>,
}

#[derive(Default)]
struct HubInner {
    /// 按 room_id 维护 sink 列表（弱引用，sink Drop 后自动失效）。
    rooms: HashMap<String, Vec<Weak<dyn MediaSink>>>,
    /// 房间最近的 video / audio config，新订阅的 sink 可立即拿到。
    video_cfg: HashMap<String, AvcConfig>,
    audio_cfg: HashMap<String, AacConfig>,
}

impl PublisherHub {
    /// 新建一个空的 hub。
    pub fn new() -> Self {
        Self::default()
    }

    /// 订阅房间。返回 Arc<sink> 的克隆，方便调用方自身 hold 一份。
    pub async fn subscribe(&self, room_id: &str, sink: Arc<dyn MediaSink>) {
        let weak = Arc::downgrade(&sink);
        let video = {
            let g = self.inner.read().await;
            g.video_cfg.get(room_id).cloned()
        };
        let audio = {
            let g = self.inner.read().await;
            g.audio_cfg.get(room_id).cloned()
        };
        {
            let mut g = self.inner.write().await;
            g.rooms.entry(room_id.to_string()).or_default().push(weak);
        }
        // catch-up：把已知配置回放一次给新 sink。
        if let Some(v) = video {
            sink.on_video_config(room_id, &v).await;
        }
        if let Some(a) = audio {
            sink.on_audio_config(room_id, &a).await;
        }
    }

    /// publisher 上报视频配置。
    pub async fn publish_video_config(&self, room_id: &str, cfg: AvcConfig) {
        {
            let mut g = self.inner.write().await;
            g.video_cfg.insert(room_id.to_string(), cfg.clone());
        }
        for s in self.live_sinks(room_id).await {
            s.on_video_config(room_id, &cfg).await;
        }
    }

    /// publisher 上报音频配置。
    pub async fn publish_audio_config(&self, room_id: &str, cfg: AacConfig) {
        {
            let mut g = self.inner.write().await;
            g.audio_cfg.insert(room_id.to_string(), cfg.clone());
        }
        for s in self.live_sinks(room_id).await {
            s.on_audio_config(room_id, &cfg).await;
        }
    }

    /// publisher 上报视频 NALU。
    pub async fn publish_video_nalu(&self, room_id: &str, nalu: VideoNalu) {
        for s in self.live_sinks(room_id).await {
            s.on_video_nalu(room_id, nalu.clone()).await;
        }
    }

    /// publisher 上报音频 frame。
    pub async fn publish_audio_frame(&self, room_id: &str, ts_ms: u32, frame: AacFrame) {
        for s in self.live_sinks(room_id).await {
            s.on_audio_frame(room_id, ts_ms, frame.clone()).await;
        }
    }

    /// 当前活跃 sink 数（已被 Drop 的 weak 不计）。
    pub async fn sink_count(&self, room_id: &str) -> usize {
        self.live_sinks(room_id).await.len()
    }

    async fn live_sinks(&self, room_id: &str) -> Vec<Arc<dyn MediaSink>> {
        let g = self.inner.read().await;
        let Some(list) = g.rooms.get(room_id) else {
            return Vec::new();
        };
        list.iter().filter_map(|w| w.upgrade()).collect()
    }
}

// ---------------- 内置 sink：WHEP RTP packetizer 适配器 ----------------

/// 把 `yunmao_webrtc::RtpRoomHub` 适配成 `MediaSink`。
///
/// LL-HLS 切片器与 WHEP RTP packetizer 共享同一个 [`PublisherHub`]：
/// 每帧 video NALU / audio AAC 同时进入 LL-HLS（HTTP fMP4）与 RTP（UDP/SRTP）。
pub struct WhepSink {
    hub: yunmao_webrtc::RtpRoomHub,
    /// 累计音频 sample（按房间 wrap）。
    cumulative_audio: std::sync::atomic::AtomicU64,
    /// 上次见到的 audio sample_rate（用于切换房间或重连后重置）。
    last_sample_rate: std::sync::atomic::AtomicU32,
}

impl WhepSink {
    /// 构造。
    pub fn new(hub: yunmao_webrtc::RtpRoomHub) -> Self {
        Self {
            hub,
            cumulative_audio: std::sync::atomic::AtomicU64::new(0),
            last_sample_rate: std::sync::atomic::AtomicU32::new(48_000),
        }
    }

    /// 共享底层 hub（供 WHEP HTTP handler 订阅）。
    pub fn hub(&self) -> yunmao_webrtc::RtpRoomHub {
        self.hub.clone()
    }
}

#[async_trait]
impl MediaSink for WhepSink {
    async fn on_video_config(&self, _room_id: &str, _cfg: &AvcConfig) {
        // SPS/PPS 通过带外 SDP fmtp 配置；不通过 RTP 发。生产路径可改 SEI/STAP-A。
    }
    async fn on_audio_config(&self, _room_id: &str, cfg: &AacConfig) {
        self.last_sample_rate
            .store(cfg.sample_rate, std::sync::atomic::Ordering::Relaxed);
    }
    async fn on_video_nalu(&self, room_id: &str, nalu: VideoNalu) {
        // nalu.data 已是 AVCC 长度前缀（FLV body[5..]）；直接喂给 packetizer。
        self.hub
            .push_video_avcc(room_id, &nalu.data, nalu.ts_ms)
            .await;
    }
    async fn on_audio_frame(&self, room_id: &str, _ts_ms: u32, frame: AacFrame) {
        let sr = self
            .last_sample_rate
            .load(std::sync::atomic::Ordering::Relaxed)
            .max(8_000);
        let cum = self.cumulative_audio.fetch_add(
            u64::from(frame.samples),
            std::sync::atomic::Ordering::Relaxed,
        ) as u32;
        self.hub.push_audio_aac(room_id, &frame.data, cum, sr).await;
    }
    fn name(&self) -> &'static str {
        "whep"
    }
}

// ---------------- 内置 sink：LL-HLS packager 适配器 ----------------

/// 把 `ll_hls::InMemoryPackager` 适配成 `MediaSink`。
pub struct LlHlsSink {
    inner: Arc<crate::ll_hls::InMemoryPackager>,
}

impl LlHlsSink {
    /// 构造。
    pub fn new(inner: Arc<crate::ll_hls::InMemoryPackager>) -> Self {
        Self { inner }
    }
}

#[async_trait]
impl MediaSink for LlHlsSink {
    async fn on_video_config(&self, room_id: &str, cfg: &AvcConfig) {
        self.inner.ingest_video_config(room_id, cfg.clone()).await;
    }
    async fn on_audio_config(&self, room_id: &str, cfg: &AacConfig) {
        self.inner.ingest_audio_config(room_id, cfg.clone()).await;
    }
    async fn on_video_nalu(&self, room_id: &str, nalu: VideoNalu) {
        let _ = self
            .inner
            .ingest_video_nalu(room_id, nalu.ts_ms, nalu.data, nalu.is_keyframe)
            .await;
    }
    async fn on_audio_frame(&self, room_id: &str, ts_ms: u32, frame: AacFrame) {
        let _ = self.inner.ingest_audio_frame(room_id, ts_ms, frame).await;
    }
    fn name(&self) -> &'static str {
        "ll-hls"
    }
}

/// 一个简单的 Counting sink，用于单测 / debug。
#[derive(Default)]
pub struct CountingSink {
    pub videos: std::sync::atomic::AtomicU64,
    pub audios: std::sync::atomic::AtomicU64,
    pub video_cfgs: std::sync::atomic::AtomicU64,
    pub audio_cfgs: std::sync::atomic::AtomicU64,
}

#[async_trait]
impl MediaSink for CountingSink {
    async fn on_video_config(&self, _room_id: &str, _cfg: &AvcConfig) {
        self.video_cfgs
            .fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    }
    async fn on_audio_config(&self, _room_id: &str, _cfg: &AacConfig) {
        self.audio_cfgs
            .fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    }
    async fn on_video_nalu(&self, _room_id: &str, _nalu: VideoNalu) {
        self.videos
            .fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    }
    async fn on_audio_frame(&self, _room_id: &str, _ts_ms: u32, _frame: AacFrame) {
        self.audios
            .fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    }
    fn name(&self) -> &'static str {
        "count"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::Ordering;

    fn avc() -> AvcConfig {
        AvcConfig {
            width: 1280,
            height: 720,
            profile_idc: 0x42,
            level_idc: 0x1f,
            frame_rate_x1000: 30_000,
            avcc_raw: Bytes::from_static(&[0x01, 0x42, 0xc0, 0x1f, 0xff, 0xe1, 0x00, 0x00]),
            nalu_len_size: 4,
        }
    }

    #[tokio::test]
    async fn hub_fanout_to_multiple_sinks() {
        let hub = PublisherHub::new();
        let s1: Arc<CountingSink> = Arc::new(CountingSink::default());
        let s2: Arc<CountingSink> = Arc::new(CountingSink::default());
        hub.subscribe("room", s1.clone()).await;
        hub.subscribe("room", s2.clone()).await;
        hub.publish_video_config("room", avc()).await;
        for i in 0..5 {
            hub.publish_video_nalu(
                "room",
                VideoNalu {
                    data: Bytes::from_static(&[0u8; 16]),
                    ts_ms: (i as u32) * 33,
                    is_keyframe: i == 0,
                },
            )
            .await;
        }
        assert_eq!(s1.videos.load(Ordering::Relaxed), 5);
        assert_eq!(s2.videos.load(Ordering::Relaxed), 5);
        assert_eq!(s1.video_cfgs.load(Ordering::Relaxed), 1);
    }

    #[tokio::test]
    async fn hub_catchup_replays_known_config() {
        let hub = PublisherHub::new();
        hub.publish_video_config("room", avc()).await;
        let late: Arc<CountingSink> = Arc::new(CountingSink::default());
        hub.subscribe("room", late.clone()).await;
        // 订阅时 catch-up 一次 video config
        assert_eq!(late.video_cfgs.load(Ordering::Relaxed), 1);
    }

    #[tokio::test]
    async fn whep_sink_emits_rtp_packets_when_hub_publishes() {
        let rtp_hub = yunmao_webrtc::RtpRoomHub::new();
        let whep: Arc<WhepSink> = Arc::new(WhepSink::new(rtp_hub.clone()));
        let hub = PublisherHub::new();
        hub.subscribe("rtp_room", whep.clone()).await;

        let mut sub = rtp_hub.subscribe("rtp_room", 48_000).await;

        // 喂 5 帧 video，每帧 1 NALU，长度 300（< MTU，应该一包一帧）
        for i in 0..5u32 {
            let avcc = {
                let mut v = Vec::with_capacity(4 + 300);
                v.extend_from_slice(&(300u32).to_be_bytes());
                v.push(0x41);
                v.extend_from_slice(&vec![i as u8; 299]);
                v
            };
            hub.publish_video_nalu(
                "rtp_room",
                VideoNalu {
                    data: Bytes::from(avcc),
                    ts_ms: i * 33,
                    is_keyframe: i == 0,
                },
            )
            .await;
        }
        let mut got = 0;
        while let Ok(Some(_p)) =
            tokio::time::timeout(std::time::Duration::from_millis(50), sub.rx.recv()).await
        {
            got += 1;
            if got >= 5 {
                break;
            }
        }
        assert!(got >= 5, "expected >=5 RTP packets via WhepSink, got {got}");
    }

    #[tokio::test]
    async fn hub_drops_dead_sinks_gracefully() {
        let hub = PublisherHub::new();
        {
            let s: Arc<CountingSink> = Arc::new(CountingSink::default());
            hub.subscribe("room", s.clone()).await;
            assert_eq!(hub.sink_count("room").await, 1);
        }
        // sink 已 Drop；live_sinks 应过滤掉
        hub.publish_video_nalu(
            "room",
            VideoNalu {
                data: Bytes::from_static(&[0u8]),
                ts_ms: 0,
                is_keyframe: true,
            },
        )
        .await;
        assert_eq!(hub.sink_count("room").await, 0);
    }
}
