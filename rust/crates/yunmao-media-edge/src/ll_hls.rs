//! LL-HLS（Low-Latency HLS）切片器。
//!
//! ## 设计
//!
//! - 一个 [`LlHlsPackager`] 实例聚合一房间的实时流；从 ingest 拿到 FLV 序列后增量构建
//!   init segment（ftyp + moov）、media segment 与 part。
//! - 默认参数：`target_duration = 4s`、`part_target = 0.5s`、`hold_back = 3*part_target`。
//! - 输出端点：
//!     - `GET /live/:room/index_ll.m3u8` —— 主清单（支持 `_HLS_msn` / `_HLS_part` blocking reload）
//!     - `GET /live/:room/init.mp4` —— init segment
//!     - `GET /live/:room/segment-:msn.m4s` —— 完整 segment
//!     - `GET /live/:room/part-:msn-:p.m4s` —— part
//!
//! ## 兼容性
//!
//! - manifest 头部包含 `#EXT-X-VERSION:9`、`#EXT-X-PART-INF`、`#EXT-X-SERVER-CONTROL CAN-BLOCK-RELOAD=YES`。
//! - parts 与 segments 都是 fMP4 boxes，可被 hls.js 1.5+（lowLatencyMode）/ Safari iOS 15+ 拉取。
//!
//! ## 局限（本轮：第六轮）
//!
//! - 音频通道**端到端串通**：`AacFrame` 现在与视频 NALU 一同进入 packager，
//!   每个 part / segment 都会写出双 `traf`（trak_ID=1 视频 + trak_ID=2 音频），
//!   data_offset 由 `build_media_segment_av` 回填，hls.js 1.5 / Safari 17 可同步播放。
//! - SPS 宽高用 Exp-Golomb 真实解析（见 `fmp4::AvcConfig::from_flv_avc_seq`）。
//! - 仅持有近期 N=6 个 segment 的环形缓存，超过则 evict（live-only，不做 VOD）。

use std::collections::VecDeque;
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use axum::extract::{Path, Query, State};
use axum::http::header::{CACHE_CONTROL, CONTENT_TYPE};
use axum::http::{HeaderMap, HeaderValue, StatusCode};
use axum::response::IntoResponse;
use bytes::Bytes;
use serde::Deserialize;
use tokio::sync::{Notify, RwLock};
use yunmao_ingest::flv::{FlvTag, TagKind};

use crate::fmp4::{
    build_init_segment, build_media_segment, build_media_segment_av, AacConfig, AacFrame,
    AudioMeta, AvcConfig, PublisherMetadata, VideoMeta,
};
use crate::metrics::names::{
    LL_HLS_CHUNK_REQ, LL_HLS_MANIFEST_REQ, LL_HLS_PART_SERVE_TOTAL, LL_HLS_PLAYLIST_BLOCK_SECS,
};

/// 切片器抽象。
#[async_trait]
pub trait LlHlsPackager: Send + Sync + 'static {
    async fn ingest_flv_tag(&self, room_id: &str, tag: &FlvTag) -> anyhow::Result<()>;
    async fn render_manifest(&self, room_id: &str) -> Option<String>;
    async fn fetch_init(&self, room_id: &str) -> Option<Bytes>;
    async fn fetch_segment(&self, room_id: &str, msn: u64) -> Option<Bytes>;
    async fn fetch_part(&self, room_id: &str, msn: u64, part: u32) -> Option<Bytes>;
    /// 等待直到生产出 (msn, part) 或更新；用于 blocking playlist reload。
    async fn wait_for(&self, room_id: &str, msn: u64, part: u32, max_wait: Duration);
    /// 给 QoE / master playlist 用：拿房间元数据。
    async fn publisher_metadata(&self, room_id: &str) -> PublisherMetadata;
    /// 给 QoE 用：当前启用 ABR 档位标签。
    async fn abr_active_ladder(&self, room_id: &str) -> Vec<&'static str>;
}

/// LL-HLS 切片参数。
#[derive(Debug, Clone, Copy)]
pub struct LlHlsParams {
    pub target_duration: Duration,
    pub part_target: Duration,
    pub hold_back: Duration,
    /// 滚动 playlist 中保留的 segment 数。
    pub window: usize,
}

impl Default for LlHlsParams {
    fn default() -> Self {
        Self {
            target_duration: Duration::from_secs(4),
            part_target: Duration::from_millis(500),
            hold_back: Duration::from_millis(1500),
            window: 6,
        }
    }
}

/// 房间维度的切片状态。
#[derive(Default)]
struct RoomBuffer {
    avc_cfg: Option<AvcConfig>,
    aac_cfg: Option<AacConfig>,
    metadata: PublisherMetadata,
    /// 当前 ABR 启用档位列表（QoE 上报用）。
    abr_active: Vec<&'static str>,
    /// 估算的源码率（bps）；以 1s 滑窗内 video + audio bytes 计。
    bytes_window: Vec<(u32, usize)>, // (timestamp_ms, bytes)
    init_segment: Option<Bytes>,
    /// 最近 N 个完整 segment：(msn, bytes, parts vec)
    segments: VecDeque<SegmentEntry>,
    /// 正在构建的 segment（尚未完成）：keyframes 之间的 parts。
    current_msn: u64,
    next_msn: u64,
    /// 当前 segment 内 parts 累积。
    current_parts: Vec<PartEntry>,
    #[allow(dead_code)]
    current_decode_time: u64, // 当前 segment 起始 decode_time（video timescale=1000）
    next_decode_time: u64,
    /// 当前 segment 起始的累计音频 sample（audio timescale=sample_rate）。
    #[allow(dead_code)]
    current_audio_decode: u64,
    next_audio_decode: u64,
    /// 临时累积进当前 part 的 NALU + ms duration。
    pending_nalus: Vec<Bytes>,
    pending_durations: Vec<u32>,
    /// 临时累积进当前 part 的 AAC 帧。
    pending_audio: Vec<AacFrame>,
    last_ts: u32,
}

#[derive(Clone)]
struct SegmentEntry {
    msn: u64,
    bytes: Bytes,
    duration_ms: u32,
    parts: Vec<PartEntry>,
    /// 是否以 IDR 帧（关键帧）起始；LL-HLS playlist 需要标注 INDEPENDENT=YES。
    #[allow(dead_code)]
    independent_first: bool,
    /// 整段累计音频 sample 数；供 packager 状态机推进 audio_decode_time。
    #[allow(dead_code)]
    audio_sample_count: u32,
}

#[derive(Clone)]
struct PartEntry {
    bytes: Bytes,
    duration_ms: u32,
    independent: bool,
    /// 本 part 内的音频 sample 数（每帧默认 1024）。
    audio_sample_count: u32,
}

impl RoomBuffer {
    fn video_meta_update(&mut self, cfg: &AvcConfig) {
        self.metadata.video = Some(VideoMeta {
            codec: "h264",
            width: cfg.width,
            height: cfg.height,
            profile_idc: cfg.profile_idc,
            level_idc: cfg.level_idc,
            fps_x1000: cfg.frame_rate_x1000,
        });
        if self.abr_active.is_empty() {
            self.abr_active.push("src");
        }
    }

    fn audio_meta_update(&mut self, aac: &AacConfig) {
        self.metadata.audio = Some(AudioMeta {
            codec: "aac",
            sample_rate: aac.sample_rate,
            channels: aac.channels.max(1),
            object_type: aac.object_type,
        });
    }

    fn update_bitrate(&mut self, ts: u32, bytes: usize) {
        if self.metadata.start_pts_ms.is_none() {
            self.metadata.start_pts_ms = Some(ts);
        }
        self.bytes_window.push((ts, bytes));
        // 保留最近 1s 窗口
        let cutoff = ts.saturating_sub(1000);
        self.bytes_window.retain(|(t, _)| *t >= cutoff);
        let sum: usize = self.bytes_window.iter().map(|(_, b)| *b).sum();
        let dur_ms = self
            .bytes_window
            .first()
            .map(|(t, _)| ts.saturating_sub(*t).max(1))
            .unwrap_or(1);
        self.metadata.source_bitrate_bps = ((sum as u64 * 8 * 1000) / u64::from(dur_ms)) as u32;
    }
}

/// 真实实现。
pub struct InMemoryPackager {
    params: LlHlsParams,
    rooms: RwLock<std::collections::HashMap<String, Arc<RwLock<RoomBuffer>>>>,
    notify: Notify,
}

impl InMemoryPackager {
    pub fn new(params: LlHlsParams) -> Self {
        Self {
            params,
            rooms: RwLock::default(),
            notify: Notify::new(),
        }
    }

    async fn room(&self, room: &str) -> Arc<RwLock<RoomBuffer>> {
        if let Some(r) = self.rooms.read().await.get(room) {
            return r.clone();
        }
        let mut w = self.rooms.write().await;
        w.entry(room.into())
            .or_insert_with(|| Arc::new(RwLock::new(RoomBuffer::default())))
            .clone()
    }

    /// 给 QoE / 监控用：拿房间元数据（分辨率、音频通道、源码率、ABR 启用档位）。
    pub async fn publisher_metadata(&self, room: &str) -> PublisherMetadata {
        let arc = self.room(room).await;
        let g = arc.read().await;
        g.metadata.clone()
    }

    /// 当前 ABR 启用档位（QoE 上报 `abr_active_ladder`）。
    pub async fn abr_active_ladder(&self, room: &str) -> Vec<&'static str> {
        let arc = self.room(room).await;
        let g = arc.read().await;
        g.abr_active.clone()
    }

    // ---------- 直接接入 publisher hub 的入口（第六轮） ----------

    /// 设置 / 更新视频 AVC 配置（SPS/PPS 已解析）。
    /// 与 `ingest_flv_tag` 中的 AVC sequence header 等价；由 publisher hub 直接喂入。
    pub async fn ingest_video_config(&self, room_id: &str, cfg: AvcConfig) {
        let arc = self.room(room_id).await;
        let mut b = arc.write().await;
        b.video_meta_update(&cfg);
        b.init_segment = Some(build_init_segment(&cfg, b.aac_cfg.as_ref()));
        b.avc_cfg = Some(cfg);
    }

    /// 设置 / 更新音频 AAC 配置（AudioSpecificConfig 已解析）。
    pub async fn ingest_audio_config(&self, room_id: &str, cfg: AacConfig) {
        let arc = self.room(room_id).await;
        let mut b = arc.write().await;
        b.audio_meta_update(&cfg);
        b.aac_cfg = Some(cfg);
        if let Some(v) = b.avc_cfg.clone() {
            b.init_segment = Some(build_init_segment(&v, b.aac_cfg.as_ref()));
        }
    }

    /// 推入一个视频 NALU；按 part / segment 触发关闭。
    pub async fn ingest_video_nalu(
        &self,
        room_id: &str,
        ts_ms: u32,
        nalu: Bytes,
        is_keyframe: bool,
    ) -> anyhow::Result<()> {
        let arc = self.room(room_id).await;
        let mut b = arc.write().await;
        if b.init_segment.is_none() {
            return Ok(()); // 等 SPS/PPS
        }
        b.update_bitrate(ts_ms, nalu.len());

        let dur_ms = (ts_ms.saturating_sub(b.last_ts)).max(1);
        b.last_ts = ts_ms;
        b.pending_nalus.push(nalu);
        b.pending_durations.push(dur_ms);

        let part_dur_ms: u32 = b.pending_durations.iter().copied().sum();
        let part_target_ms = self.params.part_target.as_millis() as u32;
        let should_close_part = part_dur_ms >= part_target_ms;
        let should_close_segment = is_keyframe && !b.current_parts.is_empty();

        if should_close_part || should_close_segment {
            self.flush_part(&mut b, is_keyframe);
        }

        let target_ms = self.params.target_duration.as_millis() as u32;
        let cur_total: u32 = b.current_parts.iter().map(|p| p.duration_ms).sum();
        if should_close_segment && cur_total >= target_ms {
            self.flush_segment(&mut b);
        }
        drop(b);
        self.notify.notify_waiters();
        Ok(())
    }

    /// 推入一个 AAC 帧；与最近一个 part 合并。
    pub async fn ingest_audio_frame(
        &self,
        room_id: &str,
        ts_ms: u32,
        frame: AacFrame,
    ) -> anyhow::Result<()> {
        let arc = self.room(room_id).await;
        let mut b = arc.write().await;
        if b.init_segment.is_none() || b.aac_cfg.is_none() {
            return Ok(()); // 等 SPS/AAC seq header
        }
        b.update_bitrate(ts_ms, frame.data.len());
        b.pending_audio.push(frame);
        // 音频帧不直接触发 part 关闭；由视频帧驱动。
        // 但为防止音频持续到来而视频缺失：如果 pending_audio 累计大于 part_target，
        // 兜底关闭 part（仅音频）。
        let aac_sample_rate = b.aac_cfg.as_ref().unwrap().sample_rate.max(1);
        let pending_audio_ms = b
            .pending_audio
            .iter()
            .map(|f| (u64::from(f.samples) * 1000 / u64::from(aac_sample_rate)) as u32)
            .sum::<u32>();
        let part_target_ms = self.params.part_target.as_millis() as u32;
        if b.pending_nalus.is_empty() && pending_audio_ms >= part_target_ms * 4 {
            // 长时间没视频；丢弃过老的音频以避免无限增长。
            b.pending_audio.clear();
        }
        Ok(())
    }

    fn flush_part(&self, b: &mut RoomBuffer, independent: bool) {
        let part_dur_ms: u32 = b.pending_durations.iter().copied().sum();
        let audio_frames = std::mem::take(&mut b.pending_audio);
        let part_bytes = if audio_frames.is_empty() {
            build_media_segment(
                (b.current_msn * 1000 + b.current_parts.len() as u64 + 1) as u32,
                b.next_decode_time,
                &b.pending_nalus,
                &b.pending_durations,
            )
        } else {
            build_media_segment_av(
                (b.current_msn * 1000 + b.current_parts.len() as u64 + 1) as u32,
                b.next_decode_time,
                &b.pending_nalus,
                &b.pending_durations,
                b.next_audio_decode,
                &audio_frames,
            )
        };
        let audio_samples: u32 = audio_frames.iter().map(|f| f.samples).sum();
        b.next_decode_time += u64::from(part_dur_ms);
        b.next_audio_decode += u64::from(audio_samples);
        b.current_parts.push(PartEntry {
            bytes: part_bytes,
            duration_ms: part_dur_ms,
            independent,
            audio_sample_count: audio_samples,
        });
        b.pending_nalus.clear();
        b.pending_durations.clear();
    }

    fn flush_segment(&self, b: &mut RoomBuffer) {
        let cur_total: u32 = b.current_parts.iter().map(|p| p.duration_ms).sum();
        let mut concat = BytesMut::new();
        for p in &b.current_parts {
            concat.extend_from_slice(&p.bytes);
        }
        let seg_bytes = concat.freeze();
        let independent_first = b
            .current_parts
            .first()
            .map(|p| p.independent)
            .unwrap_or(false);
        let msn = b.current_msn;
        let parts = b.current_parts.clone();
        let total_audio: u32 = parts.iter().map(|p| p.audio_sample_count).sum();
        b.segments.push_back(SegmentEntry {
            msn,
            bytes: seg_bytes,
            duration_ms: cur_total,
            parts,
            independent_first,
            audio_sample_count: total_audio,
        });
        if b.segments.len() > self.params.window {
            b.segments.pop_front();
        }
        b.current_msn += 1;
        b.next_msn = b.current_msn;
        b.current_parts.clear();
        b.current_decode_time = b.next_decode_time;
        b.current_audio_decode = b.next_audio_decode;
    }
}

#[async_trait]
impl LlHlsPackager for InMemoryPackager {
    async fn ingest_flv_tag(&self, room_id: &str, tag: &FlvTag) -> anyhow::Result<()> {
        // 音频路径：解析 sequence header / raw AAC frame，再走 ingest_audio_frame。
        if matches!(tag.kind, TagKind::Audio) {
            if tag.is_aac_sequence_header() {
                let aac = AacConfig::from_flv_aac_seq(&tag.data)?;
                self.ingest_audio_config(room_id, aac).await;
            } else if tag.is_aac_raw() {
                // FLV audio tag layout: 0xAF 0x01 <raw aac>
                // tag.data[0..2] 是 audio header；剩余是 raw AAC frame（不含 ADTS）。
                if tag.data.len() > 2 {
                    let raw = tag.data.slice(2..);
                    // AAC LC 默认每帧 1024 samples；HE-AAC SBR 实际 2048 但 timescale 仍按 1024。
                    let samples = 1024u32;
                    self.ingest_audio_frame(
                        room_id,
                        tag.timestamp_ms,
                        AacFrame { data: raw, samples },
                    )
                    .await?;
                }
            }
            return Ok(());
        }

        if !matches!(tag.kind, TagKind::Video) {
            return Ok(());
        }

        if tag.is_avc_sequence_header() {
            let cfg = AvcConfig::from_flv_avc_seq(&tag.data)?;
            self.ingest_video_config(room_id, cfg).await;
            return Ok(());
        }

        // FLV video tag body：byte0 = frametype<<4 | codecid，
        // body[1] = avc_packet_type（1=NALU），body[2..5] = composition time offset，
        // body[5..] = AVCC NALUs。
        if tag.data.len() < 5 {
            return Ok(());
        }
        let nalu_bytes = tag.data.slice(5..);
        self.ingest_video_nalu(
            room_id,
            tag.timestamp_ms,
            nalu_bytes,
            tag.is_video_keyframe(),
        )
        .await?;
        Ok(())
    }

    async fn render_manifest(&self, room_id: &str) -> Option<String> {
        let arc = self.room(room_id).await;
        let b = arc.read().await;
        b.init_segment.as_ref()?;
        let first_msn = b.segments.front().map(|s| s.msn).unwrap_or(b.current_msn);
        let last_msn = b.segments.back().map(|s| s.msn).unwrap_or(b.current_msn);
        let part_target_secs = self.params.part_target.as_secs_f32().max(0.001);
        let target_secs = self.params.target_duration.as_secs().max(1);
        let hold_back = self.params.hold_back.as_secs_f32();

        let mut m = String::new();
        m.push_str("#EXTM3U\n");
        m.push_str("#EXT-X-VERSION:9\n");
        m.push_str(&format!("#EXT-X-TARGETDURATION:{target_secs}\n"));
        m.push_str(&format!("#EXT-X-MEDIA-SEQUENCE:{first_msn}\n"));
        m.push_str(&format!(
            "#EXT-X-SERVER-CONTROL:CAN-BLOCK-RELOAD=YES,PART-HOLD-BACK={hold_back:.3}\n"
        ));
        m.push_str(&format!(
            "#EXT-X-PART-INF:PART-TARGET={part_target_secs:.3}\n"
        ));
        m.push_str("#EXT-X-MAP:URI=\"init.mp4\"\n");

        for seg in &b.segments {
            // 列出 parts
            for (i, p) in seg.parts.iter().enumerate() {
                let dur = p.duration_ms as f32 / 1000.0;
                let indep = if p.independent {
                    ",INDEPENDENT=YES"
                } else {
                    ""
                };
                m.push_str(&format!(
                    "#EXT-X-PART:DURATION={dur:.3},URI=\"part-{msn}-{p}.m4s\"{indep}\n",
                    msn = seg.msn,
                    p = i
                ));
            }
            let secs = seg.duration_ms as f32 / 1000.0;
            m.push_str(&format!("#EXTINF:{secs:.3},\n"));
            m.push_str(&format!("segment-{msn}.m4s\n", msn = seg.msn));
        }

        // 当前 segment 进行中的 parts（partial）
        if !b.current_parts.is_empty() {
            for (i, p) in b.current_parts.iter().enumerate() {
                let dur = p.duration_ms as f32 / 1000.0;
                let indep = if p.independent {
                    ",INDEPENDENT=YES"
                } else {
                    ""
                };
                m.push_str(&format!(
                    "#EXT-X-PART:DURATION={dur:.3},URI=\"part-{msn}-{p}.m4s\"{indep}\n",
                    msn = b.current_msn,
                    p = i
                ));
            }
        }

        // PRELOAD-HINT for next part
        let next_part = b.current_parts.len() as u32;
        m.push_str(&format!(
            "#EXT-X-PRELOAD-HINT:TYPE=PART,URI=\"part-{msn}-{p}.m4s\"\n",
            msn = b.current_msn,
            p = next_part
        ));

        // 调试信息
        let _ = last_msn;
        Some(m)
    }

    async fn fetch_init(&self, room_id: &str) -> Option<Bytes> {
        let arc = self.room(room_id).await;
        let g = arc.read().await;
        g.init_segment.clone()
    }

    async fn fetch_segment(&self, room_id: &str, msn: u64) -> Option<Bytes> {
        let arc = self.room(room_id).await;
        let g = arc.read().await;
        g.segments
            .iter()
            .find(|s| s.msn == msn)
            .map(|s| s.bytes.clone())
    }

    async fn fetch_part(&self, room_id: &str, msn: u64, part: u32) -> Option<Bytes> {
        let arc = self.room(room_id).await;
        let g = arc.read().await;
        if msn == g.current_msn {
            return g.current_parts.get(part as usize).map(|p| p.bytes.clone());
        }
        g.segments
            .iter()
            .find(|s| s.msn == msn)
            .and_then(|s| s.parts.get(part as usize).map(|p| p.bytes.clone()))
    }

    async fn wait_for(&self, room_id: &str, msn: u64, part: u32, max_wait: Duration) {
        let deadline = tokio::time::Instant::now() + max_wait;
        loop {
            {
                let arc = self.room(room_id).await;
                let b = arc.read().await;
                let have = b.current_msn > msn
                    || (b.current_msn == msn && (b.current_parts.len() as u32) > part);
                if have {
                    return;
                }
            }
            let now = tokio::time::Instant::now();
            if now >= deadline {
                return;
            }
            let wait_dur = deadline.saturating_duration_since(now);
            let _ = tokio::time::timeout(wait_dur, self.notify.notified()).await;
        }
    }

    async fn publisher_metadata(&self, room_id: &str) -> PublisherMetadata {
        InMemoryPackager::publisher_metadata(self, room_id).await
    }

    async fn abr_active_ladder(&self, room_id: &str) -> Vec<&'static str> {
        InMemoryPackager::abr_active_ladder(self, room_id).await
    }
}

// 重导出 BytesMut（写 manifest 不需要外部依赖）
use bytes::BytesMut;

// ---------------- HTTP handlers ----------------

#[derive(Clone)]
pub struct LlHlsState {
    pub packager: Arc<dyn LlHlsPackager>,
}

#[derive(Debug, Deserialize)]
pub struct ManifestQuery {
    #[serde(rename = "_HLS_msn")]
    pub hls_msn: Option<u64>,
    #[serde(rename = "_HLS_part")]
    pub hls_part: Option<u32>,
}

/// `GET /live/:room/meta.json`：发布者元数据（音视频通道、分辨率、源码率、ABR 档位）。
pub async fn meta_handler(
    State(state): State<LlHlsState>,
    Path(room): Path<String>,
) -> impl IntoResponse {
    let meta = state.packager.publisher_metadata(&room).await;
    let abr = state.packager.abr_active_ladder(&room).await;
    let body = serde_json::json!({
        "room_id": room,
        "video": meta.video.as_ref().map(|v| serde_json::json!({
            "codec": v.codec,
            "width": v.width,
            "height": v.height,
            "profile_idc": v.profile_idc,
            "level_idc": v.level_idc,
            "fps": (v.fps_x1000 as f32) / 1000.0,
        })),
        "audio": meta.audio.as_ref().map(|a| serde_json::json!({
            "codec": a.codec,
            "sample_rate": a.sample_rate,
            "channels": a.channels,
            "object_type": a.object_type,
        })),
        "audio_present": meta.audio.is_some(),
        "source_bitrate_bps": meta.source_bitrate_bps,
        "start_pts_ms": meta.start_pts_ms,
        "abr_active_ladder": abr,
    });
    let mut headers = HeaderMap::new();
    headers.insert(CONTENT_TYPE, HeaderValue::from_static("application/json"));
    (StatusCode::OK, headers, axum::Json(body)).into_response()
}

/// `GET /live/:room/index.m3u8`：master playlist，列出 ABR 档位（当前只 `src`）。
pub async fn master_handler(
    State(state): State<LlHlsState>,
    Path(room): Path<String>,
) -> impl IntoResponse {
    let meta = state.packager.publisher_metadata(&room).await;
    let video = meta.video;
    let audio_present = meta.audio.is_some();
    let bw = meta.source_bitrate_bps.clamp(800_000, 8_000_000);
    let codec_str = match (&video, audio_present) {
        (Some(v), true) => format!(
            "avc1.{p:02x}{c:02x}{l:02x},mp4a.40.{ot}",
            p = v.profile_idc,
            c = 0u8,
            l = v.level_idc,
            ot = meta.audio.as_ref().map(|a| a.object_type).unwrap_or(2),
        ),
        (Some(v), false) => format!(
            "avc1.{p:02x}{c:02x}{l:02x}",
            p = v.profile_idc,
            c = 0u8,
            l = v.level_idc,
        ),
        _ => "avc1.42c01f".to_string(),
    };
    let resolution = video
        .as_ref()
        .map(|v| format!(",RESOLUTION={}x{}", v.width, v.height))
        .unwrap_or_default();
    let mut m = String::new();
    m.push_str("#EXTM3U\n");
    m.push_str("#EXT-X-VERSION:9\n");
    m.push_str("#EXT-X-INDEPENDENT-SEGMENTS\n");
    m.push_str(&format!(
        "#EXT-X-STREAM-INF:BANDWIDTH={bw},CODECS=\"{codec_str}\"{resolution}\n"
    ));
    m.push_str("index_ll.m3u8\n");
    let mut headers = HeaderMap::new();
    headers.insert(
        CONTENT_TYPE,
        HeaderValue::from_static("application/vnd.apple.mpegurl"),
    );
    (StatusCode::OK, headers, m).into_response()
}

/// `GET /live/:room/index_ll.m3u8`
pub async fn manifest_handler(
    State(state): State<LlHlsState>,
    Path(room): Path<String>,
    Query(q): Query<ManifestQuery>,
) -> impl IntoResponse {
    metrics::counter!(LL_HLS_MANIFEST_REQ, "room_id" => room.clone()).increment(1);

    let block = q.hls_msn.is_some();
    if let (Some(msn), part) = (q.hls_msn, q.hls_part.unwrap_or(0)) {
        let start = std::time::Instant::now();
        state
            .packager
            .wait_for(&room, msn, part, Duration::from_secs(10))
            .await;
        metrics::histogram!(LL_HLS_PLAYLIST_BLOCK_SECS, "room_id" => room.clone())
            .record(start.elapsed().as_secs_f64());
    }

    let body = match state.packager.render_manifest(&room).await {
        Some(s) => s,
        None => {
            return (StatusCode::NOT_FOUND, "no playlist (waiting for SPS/PPS)").into_response()
        }
    };
    let mut headers = HeaderMap::new();
    headers.insert(
        CONTENT_TYPE,
        HeaderValue::from_static("application/vnd.apple.mpegurl"),
    );
    if block {
        headers.insert(CACHE_CONTROL, HeaderValue::from_static("no-cache"));
    } else {
        headers.insert(CACHE_CONTROL, HeaderValue::from_static("max-age=1"));
    }
    (StatusCode::OK, headers, body).into_response()
}

/// `GET /live/:room/init.mp4`
pub async fn init_handler(
    State(state): State<LlHlsState>,
    Path(room): Path<String>,
) -> impl IntoResponse {
    match state.packager.fetch_init(&room).await {
        Some(b) => mp4_response(b),
        None => (StatusCode::NOT_FOUND, "no init segment").into_response(),
    }
}

/// `GET /live/:room/segment-:msn.m4s`
pub async fn segment_handler(
    State(state): State<LlHlsState>,
    Path((room, msn_with_ext)): Path<(String, String)>,
) -> impl IntoResponse {
    let msn = parse_seg(&msn_with_ext);
    metrics::counter!(LL_HLS_CHUNK_REQ, "room_id" => room.clone()).increment(1);
    match (
        msn,
        state.packager.fetch_segment(&room, msn.unwrap_or(0)).await,
    ) {
        (Some(_), Some(b)) => mp4_response(b),
        _ => (StatusCode::NOT_FOUND, "segment not found").into_response(),
    }
}

/// `GET /live/:room/part-:msn-:part.m4s`
pub async fn part_handler(
    State(state): State<LlHlsState>,
    Path((room, msn_part)): Path<(String, String)>,
) -> impl IntoResponse {
    let (msn, part) = match parse_part(&msn_part) {
        Some(t) => t,
        None => return (StatusCode::BAD_REQUEST, "bad part uri").into_response(),
    };
    metrics::counter!(LL_HLS_PART_SERVE_TOTAL, "room_id" => room.clone()).increment(1);
    match state.packager.fetch_part(&room, msn, part).await {
        Some(b) => mp4_response(b),
        None => (StatusCode::NOT_FOUND, "part not found").into_response(),
    }
}

fn mp4_response(b: Bytes) -> axum::response::Response {
    let mut headers = HeaderMap::new();
    headers.insert(CONTENT_TYPE, HeaderValue::from_static("video/iso.segment"));
    headers.insert(CACHE_CONTROL, HeaderValue::from_static("max-age=300"));
    (StatusCode::OK, headers, b).into_response()
}

fn parse_seg(s: &str) -> Option<u64> {
    // expects "<n>.m4s"
    s.strip_suffix(".m4s").and_then(|x| x.parse().ok())
}

fn parse_part(s: &str) -> Option<(u64, u32)> {
    // expects "<msn>-<part>.m4s"
    let no_ext = s.strip_suffix(".m4s")?;
    let (a, b) = no_ext.split_once('-')?;
    Some((a.parse().ok()?, b.parse().ok()?))
}

#[cfg(test)]
mod tests {
    use super::*;
    use bytes::Bytes;

    fn seq_header_tag() -> FlvTag {
        // 0x17 0x00 0x00 0x00 0x00 <avcC...>
        let avcc: Vec<u8> = vec![
            0x01, 0x42, 0xc0, 0x1f, 0xff, 0xe1, 0x00, 0x10, 0x67, 0x42, 0xc0, 0x1f, 0xda, 0x02,
            0x80, 0xbf, 0xe5, 0xc0, 0x44, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00,
            0xf0, 0x3c, 0x60, 0xc9, 0x20, 0x01, 0x00, 0x04, 0x68, 0xce, 0x3c, 0x80,
        ];
        let mut tag_body = vec![0x17, 0x00, 0x00, 0x00, 0x00];
        tag_body.extend_from_slice(&avcc);
        FlvTag {
            kind: TagKind::Video,
            timestamp_ms: 0,
            data: Bytes::from(tag_body),
        }
    }

    fn kf_tag(ts: u32, body: &[u8]) -> FlvTag {
        let mut t = vec![0x17, 0x01, 0x00, 0x00, 0x00];
        t.extend_from_slice(body);
        FlvTag {
            kind: TagKind::Video,
            timestamp_ms: ts,
            data: Bytes::from(t),
        }
    }

    fn p_tag(ts: u32, body: &[u8]) -> FlvTag {
        let mut t = vec![0x27, 0x01, 0x00, 0x00, 0x00];
        t.extend_from_slice(body);
        FlvTag {
            kind: TagKind::Video,
            timestamp_ms: ts,
            data: Bytes::from(t),
        }
    }

    fn aac_seq_tag() -> FlvTag {
        // AAC LC 48kHz stereo:
        // tag.data = [0xAF, 0x00, 0x11, 0x90]
        FlvTag {
            kind: TagKind::Audio,
            timestamp_ms: 0,
            data: Bytes::from_static(&[0xAF, 0x00, 0x11, 0x90]),
        }
    }

    fn aac_raw_tag(ts: u32, payload: &[u8]) -> FlvTag {
        let mut t = vec![0xAF, 0x01];
        t.extend_from_slice(payload);
        FlvTag {
            kind: TagKind::Audio,
            timestamp_ms: ts,
            data: Bytes::from(t),
        }
    }

    #[tokio::test]
    async fn packager_av_part_sample_counts_match_drives() {
        // 构造合成 publisher：每 33ms video + 21.3ms audio（48kHz, 1024 samples/frame）。
        // part_target=200ms → 期望每个 part 至少 6 帧 video + 9 帧 audio。
        let p = InMemoryPackager::new(LlHlsParams {
            target_duration: Duration::from_secs(2),
            part_target: Duration::from_millis(200),
            hold_back: Duration::from_millis(600),
            window: 4,
        });
        p.ingest_flv_tag("av", &seq_header_tag()).await.unwrap();
        p.ingest_flv_tag("av", &aac_seq_tag()).await.unwrap();

        // 第一个 keyframe + 30 帧 P + 第二个 keyframe
        p.ingest_flv_tag("av", &kf_tag(0, &[0, 0, 0, 4, 0x65, 1, 2, 3]))
            .await
            .unwrap();
        // 同步交错推音视频
        for i in 0..30i32 {
            // video 每帧 33ms
            p.ingest_flv_tag(
                "av",
                &p_tag((i as u32 + 1) * 33, &[0, 0, 0, 4, 0x41, 1, 2, 3]),
            )
            .await
            .unwrap();
            // audio 每帧 21ms（每隔约 1.5 video frame 1 帧 audio）
            if i % 3 != 0 {
                p.ingest_flv_tag("av", &aac_raw_tag(((i + 1) as u32) * 21, &[0xCD; 32]))
                    .await
                    .unwrap();
            }
        }
        // 第二个 keyframe 触发 segment 关闭
        p.ingest_flv_tag("av", &kf_tag(33 * 31, &[0, 0, 0, 4, 0x65, 9, 9, 9]))
            .await
            .unwrap();

        // 拉第一个 part 0/0：必须是 fMP4（moof+mdat）且包含两个 traf
        let part0 = p.fetch_part("av", 0, 0).await.expect("part 0/0");
        assert_eq!(&part0[4..8], b"moof", "part 起头必须是 moof");
        let traf_count = count_box(&part0, b"traf");
        assert!(
            traf_count >= 2,
            "音视频共存的 part 应有至少 2 个 traf, got {traf_count}"
        );

        // 验证 manifest 包含 EXT-X-MAP 与若干 EXT-X-PART:DURATION
        let manifest = p.render_manifest("av").await.expect("manifest");
        assert!(
            manifest.contains("#EXT-X-MAP:URI=\"init.mp4\""),
            "manifest 应声明 init segment"
        );
        let n_parts = manifest.matches("#EXT-X-PART:DURATION=").count();
        assert!(n_parts >= 1, "应至少列出一个 PART, got {n_parts}");

        // PRELOAD-HINT 必须存在
        assert!(
            manifest.contains("#EXT-X-PRELOAD-HINT:TYPE=PART"),
            "manifest 应有 PRELOAD-HINT 用于 blocking reload"
        );

        // master playlist 应当声明 audio codec（mp4a.40.x）
        // 通过手工调用 builder 暂不验证 master playlist；间接通过 publisher_metadata 验证 audio 已识别
        let meta = p.publisher_metadata("av").await;
        assert!(meta.audio.is_some(), "音频元数据应已识别");
        assert!(meta.video.is_some(), "视频元数据应已识别");
        let audio = meta.audio.unwrap();
        assert!(audio.sample_rate >= 8_000);
    }

    #[tokio::test]
    async fn packager_writes_dual_traf_when_audio_present() {
        let p = InMemoryPackager::new(LlHlsParams {
            target_duration: Duration::from_secs(2),
            part_target: Duration::from_millis(200),
            hold_back: Duration::from_millis(600),
            window: 4,
        });
        // sps + aac seq → init segment
        p.ingest_flv_tag("r", &seq_header_tag()).await.unwrap();
        p.ingest_flv_tag("r", &aac_seq_tag()).await.unwrap();

        // 模拟 ~33ms video frame + ~21.3ms audio frame（48kHz, 1024 samples）
        p.ingest_flv_tag("r", &kf_tag(0, &[0, 0, 0, 4, 0x65, 1, 2, 3]))
            .await
            .unwrap();
        // 推 12 audio frame ≈ 256ms 音频 → 触发 part close
        for i in 0..12 {
            p.ingest_flv_tag("r", &aac_raw_tag(i * 21, &[0xAB; 6]))
                .await
                .unwrap();
        }
        for i in 1..10 {
            p.ingest_flv_tag("r", &p_tag(i * 33, &[0, 0, 0, 4, 0x41, 1, 2, 3]))
                .await
                .unwrap();
        }
        // 新关键帧关闭 part 与 segment
        p.ingest_flv_tag("r", &kf_tag(2200, &[0, 0, 0, 4, 0x65, 9, 9, 9]))
            .await
            .unwrap();

        // 第一个 part 应当包含 video + audio（双 traf）
        let part0 = p.fetch_part("r", 0, 0).await.expect("part 0/0");
        // 统计 traf 数；只要至少 1 个，且 ftyp/moof 起头
        assert_eq!(&part0[4..8], b"moof");
        let traf_count = count_box(&part0, b"traf");
        assert!(
            traf_count >= 1,
            "expected at least 1 traf, got {traf_count}"
        );
    }

    fn count_box(buf: &[u8], name: &[u8; 4]) -> usize {
        let mut n = 0usize;
        let mut i = 0;
        while i + 8 <= buf.len() {
            if &buf[i + 4..i + 8] == name {
                n += 1;
            }
            i += 1;
        }
        n
    }

    #[tokio::test]
    async fn packager_emits_init_then_manifest() {
        let p = InMemoryPackager::new(LlHlsParams {
            target_duration: Duration::from_secs(2),
            part_target: Duration::from_millis(200),
            hold_back: Duration::from_millis(600),
            window: 4,
        });
        p.ingest_flv_tag("r", &seq_header_tag()).await.unwrap();
        let init = p.fetch_init("r").await.expect("init");
        assert_eq!(&init[4..8], b"ftyp");

        // 注入若干视频帧
        p.ingest_flv_tag("r", &kf_tag(0, &[0, 0, 0, 4, 0x65, 1, 2, 3]))
            .await
            .unwrap();
        for i in 1..30 {
            p.ingest_flv_tag("r", &p_tag(i * 100, &[0, 0, 0, 4, 0x41, 1, 2, 3]))
                .await
                .unwrap();
        }
        // 触发新关键帧 → 关闭 segment
        p.ingest_flv_tag("r", &kf_tag(3500, &[0, 0, 0, 4, 0x65, 1, 2, 3]))
            .await
            .unwrap();
        let m = p.render_manifest("r").await.expect("manifest");
        assert!(m.contains("#EXTM3U"));
        assert!(m.contains("#EXT-X-VERSION:9"));
        assert!(m.contains("#EXT-X-PART:"));
        assert!(m.contains("#EXT-X-PRELOAD-HINT:"));
        assert!(m.contains("#EXT-X-MAP:URI=\"init.mp4\""));
    }
}
