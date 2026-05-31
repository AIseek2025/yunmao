//! 极简 SFU：把上游帧（H.264 NALU + AAC frame）packetize 成 RTP，按房间扇出给 WHEP 订阅者。
//!
//! 本模块**不直接依赖** `yunmao-media-edge`（避免环形依赖）；调用方（media-edge / WHIP
//! handler）把帧通过 [`RtpRoomHub::push_video_avcc`] / [`RtpRoomHub::push_audio_aac`]
//! 喂入。订阅者（WHEP HTTP handler）通过 [`RtpRoomHub::subscribe`] 拿到 mpsc::Receiver。
//!
//! 时间戳：
//! - 视频 90kHz；按 RTMP ts_ms 转换：`ts_90k = ts_ms * 90`。
//! - 音频按 sample_rate；调用方需提供 `cumulative_samples`。

use std::collections::HashMap;
use std::sync::Arc;

use bytes::Bytes;
use tokio::sync::{mpsc, RwLock};

use crate::rtp::{AacPacketizer, H264Packetizer, RtpPacket};

/// 一个 WHEP 订阅 channel 的输出。
pub struct RtpSubscription {
    /// 订阅 ID（也用于 `unsubscribe`）。
    pub id: String,
    /// RTP packet 接收端（每包都是已序列化的 wire-format 字节）。
    pub rx: mpsc::Receiver<RtpPacket>,
    /// 视频 SSRC。
    pub video_ssrc: u32,
    /// 视频 payload type（96）。
    pub video_pt: u8,
    /// 音频 SSRC。
    pub audio_ssrc: u32,
    /// 音频 payload type（97）。
    pub audio_pt: u8,
    /// 音频时钟域（= AAC sample_rate）。
    pub audio_clock_rate: u32,
}

/// 单房间的状态。
struct RoomState {
    /// 视频 packetizer（共享 SSRC，所有订阅者拿同一序列号流）。
    vp: H264Packetizer,
    ap: AacPacketizer,
    /// 订阅者：id → (tx, room_lifetime keep flag)
    subscribers: Vec<(String, mpsc::Sender<RtpPacket>)>,
    /// 累计 RTP 包计数（监控）。
    packets_video: u64,
    packets_audio: u64,
}

/// 房间级 RTP fan-out hub。
#[derive(Clone)]
pub struct RtpRoomHub {
    inner: Arc<RwLock<HashMap<String, RoomState>>>,
    /// 每个订阅者的输出 channel 缓冲。
    pub subscriber_buffer: usize,
}

impl Default for RtpRoomHub {
    fn default() -> Self {
        Self::new()
    }
}

impl RtpRoomHub {
    /// 构造（默认 buffer=256 包）。
    pub fn new() -> Self {
        Self {
            inner: Arc::new(RwLock::new(HashMap::new())),
            subscriber_buffer: 256,
        }
    }

    /// 房间不存在则按 [`crate::rtp`] 默认参数创建。
    async fn ensure_room(&self, room_id: &str, audio_clock_rate: u32) {
        let mut g = self.inner.write().await;
        g.entry(room_id.to_string()).or_insert_with(|| {
            let video_ssrc = (rand::random::<u32>() | 1).max(1);
            let audio_ssrc = (rand::random::<u32>() | 1).max(1).wrapping_add(1);
            RoomState {
                vp: H264Packetizer::new(video_ssrc),
                ap: AacPacketizer::new(audio_ssrc, audio_clock_rate),
                subscribers: Vec::new(),
                packets_video: 0,
                packets_audio: 0,
            }
        });
    }

    /// WHIP 收到一帧 H.264 access unit（AVCC 长度前缀）→ 转 RTP → 扇出。
    pub async fn push_video_avcc(&self, room_id: &str, avcc_nalus: &Bytes, ts_ms: u32) {
        self.ensure_room(room_id, 48_000).await;
        let mut g = self.inner.write().await;
        if let Some(rs) = g.get_mut(room_id) {
            let ts_90k = ts_ms.wrapping_mul(90);
            let pkts = rs.vp.packetize_avcc(avcc_nalus, ts_90k);
            rs.packets_video += pkts.len() as u64;
            for p in pkts {
                rs.subscribers
                    .retain(|(_id, tx)| tx.try_send(p.clone()).is_ok() || !tx.is_closed());
            }
        }
    }

    /// 直接喂 NALU 列表（已拆好；FLV 路径用 `push_video_avcc`）。
    pub async fn push_video_nalus(&self, room_id: &str, nalus: &[Vec<u8>], ts_ms: u32) {
        self.ensure_room(room_id, 48_000).await;
        let mut g = self.inner.write().await;
        if let Some(rs) = g.get_mut(room_id) {
            let ts_90k = ts_ms.wrapping_mul(90);
            let refs: Vec<&[u8]> = nalus.iter().map(|v| v.as_slice()).collect();
            let pkts = rs.vp.packetize_nalus(&refs, ts_90k);
            rs.packets_video += pkts.len() as u64;
            for p in pkts {
                rs.subscribers
                    .retain(|(_id, tx)| tx.try_send(p.clone()).is_ok() || !tx.is_closed());
            }
        }
    }

    /// WHIP/RTMP 上来的一帧 AAC raw（不含 ADTS）→ 转 RTP → 扇出。
    pub async fn push_audio_aac(
        &self,
        room_id: &str,
        raw_aac: &Bytes,
        cumulative_samples: u32,
        sample_rate: u32,
    ) {
        self.ensure_room(room_id, sample_rate).await;
        let mut g = self.inner.write().await;
        if let Some(rs) = g.get_mut(room_id) {
            let pkt = rs.ap.packetize(raw_aac, cumulative_samples);
            rs.packets_audio += 1;
            rs.subscribers
                .retain(|(_id, tx)| tx.try_send(pkt.clone()).is_ok() || !tx.is_closed());
        }
    }

    /// WHEP 订阅；返回 RtpSubscription 持有的 Receiver。
    pub async fn subscribe(&self, room_id: &str, audio_clock_rate: u32) -> RtpSubscription {
        self.ensure_room(room_id, audio_clock_rate).await;
        let mut g = self.inner.write().await;
        let rs = g.get_mut(room_id).expect("room exists after ensure");
        let (tx, rx) = mpsc::channel::<RtpPacket>(self.subscriber_buffer);
        let id = format!(
            "sub_{}",
            (rand::random::<u64>() ^ (rs.subscribers.len() as u64))
        );
        let video_ssrc = rs.vp.ssrc;
        let audio_ssrc = rs.ap.ssrc;
        rs.subscribers.push((id.clone(), tx));
        RtpSubscription {
            id,
            rx,
            video_ssrc,
            video_pt: rs.vp.payload_type,
            audio_ssrc,
            audio_pt: rs.ap.payload_type,
            audio_clock_rate: rs.ap.clock_rate,
        }
    }

    /// 取消订阅。
    pub async fn unsubscribe(&self, room_id: &str, sub_id: &str) {
        let mut g = self.inner.write().await;
        if let Some(rs) = g.get_mut(room_id) {
            rs.subscribers.retain(|(id, _)| id != sub_id);
        }
    }

    /// 监控：(video_packets, audio_packets, subscriber_count)
    pub async fn stats(&self, room_id: &str) -> (u64, u64, usize) {
        let g = self.inner.read().await;
        g.get(room_id)
            .map(|rs| (rs.packets_video, rs.packets_audio, rs.subscribers.len()))
            .unwrap_or((0, 0, 0))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn avcc(nalus: &[&[u8]]) -> Bytes {
        let mut v = Vec::new();
        for n in nalus {
            v.extend_from_slice(&(n.len() as u32).to_be_bytes());
            v.extend_from_slice(n);
        }
        Bytes::from(v)
    }

    #[tokio::test]
    async fn whip_then_whep_receives_at_least_30_rtp_video_packets() {
        let hub = RtpRoomHub::new();
        let mut sub = hub.subscribe("room_demo", 48_000).await;

        // 模拟 30 帧 H.264，每帧 1 NALU，500 字节
        for i in 0..30u32 {
            let nalu: Vec<u8> = std::iter::once(0x41u8)
                .chain(std::iter::repeat_n((i % 256) as u8, 499))
                .collect();
            hub.push_video_avcc("room_demo", &avcc(&[&nalu]), i * 33)
                .await;
        }

        // 至少收到 30 个 video RTP packet
        let mut got = 0usize;
        let mut last_ts = None;
        while let Ok(Some(p)) =
            tokio::time::timeout(std::time::Duration::from_millis(50), sub.rx.recv()).await
        {
            got += 1;
            if let Some(prev) = last_ts {
                assert!(p.timestamp >= prev, "ts should be monotonic");
            }
            last_ts = Some(p.timestamp);
            if got >= 30 {
                break;
            }
        }
        assert!(
            got >= 30,
            "expected at least 30 RTP video packets, got {got}"
        );

        let (v_pkts, _, subs) = hub.stats("room_demo").await;
        assert_eq!(subs, 1);
        assert!(v_pkts >= 30);
    }

    #[tokio::test]
    async fn whip_audio_aac_emits_rtp_packets() {
        let hub = RtpRoomHub::new();
        let mut sub = hub.subscribe("room_audio", 48_000).await;

        for i in 0..10u32 {
            let aac = vec![0xAB; 240];
            hub.push_audio_aac("room_audio", &Bytes::from(aac), i * 1024, 48_000)
                .await;
        }
        let mut got = 0usize;
        while let Ok(Some(p)) =
            tokio::time::timeout(std::time::Duration::from_millis(50), sub.rx.recv()).await
        {
            got += 1;
            assert_eq!(p.payload_type, 97);
            if got >= 10 {
                break;
            }
        }
        assert_eq!(got, 10);
    }

    #[tokio::test]
    async fn unsubscribe_removes_receiver() {
        let hub = RtpRoomHub::new();
        let sub = hub.subscribe("r1", 48_000).await;
        let id = sub.id.clone();
        drop(sub);
        // 推一帧，已 drop 的 channel 应被清理
        let nalu: Vec<u8> = vec![0x65; 50];
        hub.push_video_avcc("r1", &avcc(&[&nalu]), 0).await;
        // 显式 unsubscribe 也应是幂等
        hub.unsubscribe("r1", &id).await;
        let (_, _, subs) = hub.stats("r1").await;
        assert_eq!(subs, 0);
    }
}
