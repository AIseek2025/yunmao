//! ABR（自适应码率）档位定义 + 转码 worker trait + passthrough 实现 + ffmpeg subprocess 占位。
//!
//! 与 `02-高清低延迟直播架构.md` 第 11 节一致：`src` / `hd1080` / `hd720` / `sd480` / `audio`。
//!
//! ## 设计
//!
//! - [`AbrProfile`]：档位枚举 + 推荐参数（分辨率 / 码率）。
//! - [`TranscodeWorker`] trait：异步消费 `TranscodeFrame`（含 FLV tag 字节 + 时间戳），
//!   产出 `Vec<TranscodeFrame>`（每个 enabled profile 一个）；passthrough worker 直接
//!   原样回写 `src`。
//! - 进程间协议：`spawn_subprocess_passthrough` 演示用 stdin/stdout 长度前缀帧
//!   （4-byte big-endian length + payload），方便未来替换为 ffmpeg pipeline，无需
//!   重写 media-edge 上层。
//! - 真正的 ffmpeg/GStreamer 转码留 GPU/CPU profile 评估后接入（见 ADR-0017 placeholder）。

use std::process::Stdio;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

use anyhow::{anyhow, Result};
use async_trait::async_trait;
use bytes::{Bytes, BytesMut};
use serde::{Deserialize, Serialize};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::process::{Child, ChildStdin, ChildStdout, Command};
use tokio::sync::{mpsc, Mutex};

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq, Hash)]
#[serde(rename_all = "snake_case")]
pub enum AbrProfile {
    Source,
    Hd1080,
    Hd720,
    Sd480,
    Audio,
}

impl AbrProfile {
    pub const ALL: &'static [Self] = &[
        Self::Source,
        Self::Hd1080,
        Self::Hd720,
        Self::Sd480,
        Self::Audio,
    ];

    pub fn label(self) -> &'static str {
        match self {
            Self::Source => "src",
            Self::Hd1080 => "hd1080",
            Self::Hd720 => "hd720",
            Self::Sd480 => "sd480",
            Self::Audio => "audio",
        }
    }

    /// 推荐目标码率（kbps）。
    pub fn target_bitrate_kbps(self) -> u32 {
        match self {
            Self::Source => 6_000,
            Self::Hd1080 => 3_500,
            Self::Hd720 => 1_800,
            Self::Sd480 => 800,
            Self::Audio => 96,
        }
    }

    /// 推荐输出分辨率（width, height）；`Source` / `Audio` 不缩放。
    pub fn target_resolution(self) -> Option<(u16, u16)> {
        match self {
            Self::Hd1080 => Some((1920, 1080)),
            Self::Hd720 => Some((1280, 720)),
            Self::Sd480 => Some((854, 480)),
            _ => None,
        }
    }
}

/// 转码 worker 的输入 / 输出帧。
#[derive(Debug, Clone)]
pub struct TranscodeFrame {
    pub profile: AbrProfile,
    pub timestamp_ms: u32,
    /// FLV tag 字节（含 header）。下游可直接喂回 ingest pipeline / LL-HLS。
    pub flv_tag_bytes: Bytes,
}

/// 转码 worker 抽象。
#[async_trait]
pub trait TranscodeWorker: Send + Sync {
    /// worker 启用的输出档位（不含 `src` 也不含 `audio`，只列需要*转码*的）。
    fn output_profiles(&self) -> &[AbrProfile];
    /// 处理一帧，返回 0..N 个输出帧。
    async fn process(&self, frame: TranscodeFrame) -> Result<Vec<TranscodeFrame>>;
    /// 平稳关闭：清理 subprocess / GPU 上下文。
    async fn shutdown(&self) -> Result<()> {
        Ok(())
    }
}

/// 直通 worker：把所有输入帧 echo 为 `src`，不做实际转码。
/// 用于 MVP 链路联调，CI 默认。
pub struct PassthroughWorker;

#[async_trait]
impl TranscodeWorker for PassthroughWorker {
    fn output_profiles(&self) -> &[AbrProfile] {
        &[AbrProfile::Source]
    }

    async fn process(&self, frame: TranscodeFrame) -> Result<Vec<TranscodeFrame>> {
        Ok(vec![TranscodeFrame {
            profile: AbrProfile::Source,
            ..frame
        }])
    }
}

/// ffmpeg subprocess wrapper（真正的转码接入点）。
///
/// 当前只 spawn `cat`（无 ffmpeg 二进制时退化为 echo passthrough）；
/// 真正 ladder 接入 = 替换 `args` 为 `ffmpeg -i pipe:0 -filter_complex split=3[v1][v2][v3];
///   [v1]scale=854:480[s480]; ... -map [s480] -c:v libx264 -b:v 800k -f flv pipe:1 ...`。
///
/// 协议（subprocess 标准输入/输出）：4-byte BE length + 该长度的 payload。
pub struct FfmpegSubprocessWorker {
    profiles: Vec<AbrProfile>,
    child: Mutex<Option<Child>>,
}

impl FfmpegSubprocessWorker {
    pub fn passthrough_via_cat() -> Result<Self> {
        let child = Command::new("cat")
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()?;
        Ok(Self {
            profiles: vec![AbrProfile::Source],
            child: Mutex::new(Some(child)),
        })
    }

    /// 兼容旧 API（仅检测 ffmpeg 二进制可用性，真正运行用 [`FfmpegLadderWorker`]）。
    pub fn ffmpeg_ladder(_ladder: &[AbrProfile]) -> Result<Self> {
        Err(anyhow!(
            "FfmpegSubprocessWorker::ffmpeg_ladder 已迁移到 FfmpegLadderWorker (第五轮)"
        ))
    }
}

#[async_trait]
impl TranscodeWorker for FfmpegSubprocessWorker {
    fn output_profiles(&self) -> &[AbrProfile] {
        &self.profiles
    }

    async fn process(&self, frame: TranscodeFrame) -> Result<Vec<TranscodeFrame>> {
        let mut g = self.child.lock().await;
        let Some(child) = g.as_mut() else {
            return Err(anyhow!("subprocess not alive"));
        };
        let stdin = child
            .stdin
            .as_mut()
            .ok_or_else(|| anyhow!("subprocess stdin missing"))?;
        let payload = &frame.flv_tag_bytes;
        let len = u32::try_from(payload.len()).map_err(|_| anyhow!("payload too large"))?;
        stdin.write_all(&len.to_be_bytes()).await?;
        stdin.write_all(payload).await?;
        stdin.flush().await?;

        // 读回（passthrough 模式必有同等输出）。
        let stdout = child
            .stdout
            .as_mut()
            .ok_or_else(|| anyhow!("subprocess stdout missing"))?;
        let mut lb = [0u8; 4];
        stdout.read_exact(&mut lb).await?;
        let l = u32::from_be_bytes(lb) as usize;
        let mut buf = vec![0u8; l];
        stdout.read_exact(&mut buf).await?;
        Ok(vec![TranscodeFrame {
            profile: AbrProfile::Source,
            timestamp_ms: frame.timestamp_ms,
            flv_tag_bytes: Bytes::from(buf),
        }])
    }

    async fn shutdown(&self) -> Result<()> {
        let mut g = self.child.lock().await;
        if let Some(mut child) = g.take() {
            let _ = child.kill().await;
        }
        Ok(())
    }
}

/// 一次性 spawn 一个 passthrough subprocess（仅用于 protocol 单测）。
pub fn spawn_subprocess_passthrough() -> Result<Arc<dyn TranscodeWorker>> {
    Ok(Arc::new(FfmpegSubprocessWorker::passthrough_via_cat()?))
}

// ---------------- ffmpeg ladder（真转码）----------------

/// 单个 ffmpeg 子进程的运行时句柄。
struct FfmpegLadderLeg {
    profile: AbrProfile,
    stdin: Mutex<Option<ChildStdin>>,
    output_rx: Mutex<mpsc::Receiver<Bytes>>,
    _child: Mutex<Option<Child>>,
    bytes_in: Arc<AtomicU64>,
    bytes_out: Arc<AtomicU64>,
    chunks_out: Arc<AtomicU64>,
    restarts: Arc<AtomicU64>,
}

/// FfmpegLadderWorker：每个 enabled profile 起一个 ffmpeg 子进程，串接到 LL-HLS 切片器。
///
/// 输入：FLV 字节流（与 RTMP ingest 协议同源）。
/// 输出：每个 leg 的 FLV 字节块（按 ffmpeg pipe:1 自然分块返回）。
///
/// QoE 字段：
/// - `abr_publisher_count` = enabled ladders（含 source 直通）；
/// - `transcode_worker_busy` = active legs 比例；
/// - `transcode_restarts_total{profile=}` = leg 重启次数；
/// - `abr_ladder_bitrate_bps{profile=}` histogram = (bytes_out * 8 / window)。
pub struct FfmpegLadderWorker {
    profiles: Vec<AbrProfile>,
    legs: Vec<Arc<FfmpegLadderLeg>>,
    #[allow(dead_code)]
    ffmpeg_bin: String,
}

impl FfmpegLadderWorker {
    /// 探测 ffmpeg 二进制是否在 PATH（dev/CI）。
    pub fn ffmpeg_available() -> bool {
        // 同步阻塞探测，仅在 spawn 前一次。
        std::process::Command::new(Self::default_bin())
            .arg("-version")
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .map(|s| s.success())
            .unwrap_or(false)
    }

    fn default_bin() -> &'static str {
        "ffmpeg"
    }

    /// 起一个 ladder：每个 profile 一个 ffmpeg；profile=Source 走 `cat` 不重新编码。
    pub async fn spawn(ladder: &[AbrProfile]) -> Result<Self> {
        Self::spawn_with_bin(Self::default_bin(), ladder).await
    }

    /// 自定义 ffmpeg 二进制路径（如 `/usr/local/bin/ffmpeg`）。
    pub async fn spawn_with_bin(bin: &str, ladder: &[AbrProfile]) -> Result<Self> {
        if ladder.is_empty() {
            return Err(anyhow!("ladder cannot be empty"));
        }
        let mut legs: Vec<Arc<FfmpegLadderLeg>> = Vec::with_capacity(ladder.len());
        for p in ladder {
            let leg = Self::spawn_leg(bin, *p).await?;
            legs.push(Arc::new(leg));
        }
        Ok(Self {
            profiles: ladder.to_vec(),
            legs,
            ffmpeg_bin: bin.to_string(),
        })
    }

    async fn spawn_leg(bin: &str, p: AbrProfile) -> Result<FfmpegLadderLeg> {
        let mut cmd = if matches!(p, AbrProfile::Source) {
            // Source 直通：用 cat 维持协议一致性。
            let mut c = Command::new("cat");
            c.stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::null());
            c
        } else {
            let mut c = Command::new(bin);
            let (w, h) = p.target_resolution().unwrap_or((1280, 720));
            let v_bitrate = format!("{}k", p.target_bitrate_kbps());
            let scale = format!("scale={}:{}", w, h);
            c.args([
                "-loglevel",
                "warning",
                "-fflags",
                "+genpts",
                "-i",
                "pipe:0",
                "-vf",
                &scale,
                "-c:v",
                "libx264",
                "-preset",
                "veryfast",
                "-tune",
                "zerolatency",
                "-b:v",
                &v_bitrate,
                "-maxrate",
                &v_bitrate,
                "-bufsize",
                &format!("{}k", p.target_bitrate_kbps() * 2),
                "-pix_fmt",
                "yuv420p",
                "-g",
                "60",
                "-c:a",
                "aac",
                "-b:a",
                "96k",
                "-ar",
                "44100",
                "-f",
                "flv",
                "pipe:1",
            ]);
            c.stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::null());
            c
        };
        let mut child = cmd
            .spawn()
            .map_err(|e| anyhow!("spawn ffmpeg leg {}: {}", p.label(), e))?;
        let stdin = child.stdin.take().ok_or_else(|| anyhow!("no stdin"))?;
        let stdout = child.stdout.take().ok_or_else(|| anyhow!("no stdout"))?;
        let bytes_out = Arc::new(AtomicU64::new(0));
        let chunks_out = Arc::new(AtomicU64::new(0));
        let (tx, rx) = mpsc::channel::<Bytes>(64);
        spawn_reader(stdout, tx, bytes_out.clone(), chunks_out.clone());
        Ok(FfmpegLadderLeg {
            profile: p,
            stdin: Mutex::new(Some(stdin)),
            output_rx: Mutex::new(rx),
            _child: Mutex::new(Some(child)),
            bytes_in: Arc::new(AtomicU64::new(0)),
            bytes_out,
            chunks_out,
            restarts: Arc::new(AtomicU64::new(0)),
        })
    }

    /// 当前 ladder labels（含 src），用于 master playlist 与 Prometheus label。
    pub fn labels(&self) -> Vec<&'static str> {
        self.profiles.iter().map(|p| p.label()).collect()
    }

    /// 输出每个 leg 的 QoE 指标快照。
    pub fn metrics_snapshot(&self) -> Vec<LegMetrics> {
        self.legs
            .iter()
            .map(|l| LegMetrics {
                profile: l.profile,
                bytes_in: l.bytes_in.load(Ordering::Relaxed),
                bytes_out: l.bytes_out.load(Ordering::Relaxed),
                chunks_out: l.chunks_out.load(Ordering::Relaxed),
                restarts: l.restarts.load(Ordering::Relaxed),
            })
            .collect()
    }
}

#[derive(Debug, Clone, Copy)]
pub struct LegMetrics {
    pub profile: AbrProfile,
    pub bytes_in: u64,
    pub bytes_out: u64,
    pub chunks_out: u64,
    pub restarts: u64,
}

fn spawn_reader(
    mut stdout: ChildStdout,
    tx: mpsc::Sender<Bytes>,
    bytes_out: Arc<AtomicU64>,
    chunks_out: Arc<AtomicU64>,
) {
    tokio::spawn(async move {
        let mut buf = [0u8; 32 * 1024];
        loop {
            match stdout.read(&mut buf).await {
                Ok(0) => break,
                Ok(n) => {
                    bytes_out.fetch_add(n as u64, Ordering::Relaxed);
                    chunks_out.fetch_add(1, Ordering::Relaxed);
                    let mut b = BytesMut::with_capacity(n);
                    b.extend_from_slice(&buf[..n]);
                    if tx.send(b.freeze()).await.is_err() {
                        break;
                    }
                }
                Err(_) => break,
            }
        }
    });
}

#[async_trait]
impl TranscodeWorker for FfmpegLadderWorker {
    fn output_profiles(&self) -> &[AbrProfile] {
        &self.profiles
    }

    /// 把 frame 写入每个 ffmpeg leg；只 drain 当前已就绪的输出（非阻塞）。
    async fn process(&self, frame: TranscodeFrame) -> Result<Vec<TranscodeFrame>> {
        let payload = &frame.flv_tag_bytes;
        for leg in &self.legs {
            let mut g = leg.stdin.lock().await;
            if let Some(stdin) = g.as_mut() {
                if stdin.write_all(payload).await.is_err() {
                    leg.restarts.fetch_add(1, Ordering::Relaxed);
                } else {
                    let _ = stdin.flush().await;
                    leg.bytes_in
                        .fetch_add(payload.len() as u64, Ordering::Relaxed);
                }
            }
        }
        let mut out = Vec::new();
        for leg in &self.legs {
            let mut rx = leg.output_rx.lock().await;
            // 非阻塞 try_recv：取所有已就绪 chunk
            loop {
                match rx.try_recv() {
                    Ok(b) => out.push(TranscodeFrame {
                        profile: leg.profile,
                        timestamp_ms: frame.timestamp_ms,
                        flv_tag_bytes: b,
                    }),
                    Err(mpsc::error::TryRecvError::Empty) => break,
                    Err(mpsc::error::TryRecvError::Disconnected) => {
                        leg.restarts.fetch_add(1, Ordering::Relaxed);
                        break;
                    }
                }
            }
        }
        Ok(out)
    }

    async fn shutdown(&self) -> Result<()> {
        for leg in &self.legs {
            let mut g = leg._child.lock().await;
            if let Some(mut child) = g.take() {
                let _ = child.kill().await;
            }
        }
        Ok(())
    }
}

/// Fake ladder worker：测试用，输入直接回写为多个 profile 的 echo，方便不依赖 ffmpeg 跑集成测试。
pub struct FakeLadderWorker {
    profiles: Vec<AbrProfile>,
    metrics: Mutex<Vec<LegMetrics>>,
}

impl FakeLadderWorker {
    pub fn new(profiles: Vec<AbrProfile>) -> Self {
        let metrics = profiles
            .iter()
            .map(|p| LegMetrics {
                profile: *p,
                bytes_in: 0,
                bytes_out: 0,
                chunks_out: 0,
                restarts: 0,
            })
            .collect();
        Self {
            profiles,
            metrics: Mutex::new(metrics),
        }
    }

    pub fn labels(&self) -> Vec<&'static str> {
        self.profiles.iter().map(|p| p.label()).collect()
    }

    pub async fn metrics_snapshot(&self) -> Vec<LegMetrics> {
        self.metrics.lock().await.clone()
    }
}

#[async_trait]
impl TranscodeWorker for FakeLadderWorker {
    fn output_profiles(&self) -> &[AbrProfile] {
        &self.profiles
    }

    async fn process(&self, frame: TranscodeFrame) -> Result<Vec<TranscodeFrame>> {
        let mut out = Vec::with_capacity(self.profiles.len());
        let mut m = self.metrics.lock().await;
        for (i, p) in self.profiles.iter().enumerate() {
            out.push(TranscodeFrame {
                profile: *p,
                timestamp_ms: frame.timestamp_ms,
                flv_tag_bytes: frame.flv_tag_bytes.clone(),
            });
            m[i].bytes_in += frame.flv_tag_bytes.len() as u64;
            m[i].bytes_out += frame.flv_tag_bytes.len() as u64;
            m[i].chunks_out += 1;
        }
        Ok(out)
    }
}

/// 当前已启用的 ABR 档位（QoE 上报用 `abr_active_ladder`）。
#[derive(Default, Clone)]
pub struct Abr {
    enabled: Vec<AbrProfile>,
}

impl Abr {
    pub fn mvp_default() -> Self {
        Self {
            enabled: vec![AbrProfile::Source],
        }
    }

    pub fn enabled(&self) -> &[AbrProfile] {
        &self.enabled
    }

    pub fn labels(&self) -> Vec<&'static str> {
        self.enabled.iter().map(|p| p.label()).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn all_profiles_have_unique_labels() {
        let mut seen = std::collections::HashSet::new();
        for p in AbrProfile::ALL {
            assert!(seen.insert(p.label()));
            assert!(p.target_bitrate_kbps() > 0);
        }
    }

    #[tokio::test]
    async fn passthrough_worker_echoes_frame() {
        let w = PassthroughWorker;
        let f = TranscodeFrame {
            profile: AbrProfile::Source,
            timestamp_ms: 12,
            flv_tag_bytes: Bytes::from_static(&[1, 2, 3, 4, 5]),
        };
        let out = w.process(f.clone()).await.unwrap();
        assert_eq!(out.len(), 1);
        assert_eq!(out[0].profile, AbrProfile::Source);
        assert_eq!(out[0].timestamp_ms, 12);
        assert_eq!(&out[0].flv_tag_bytes[..], &[1, 2, 3, 4, 5][..]);
    }

    #[tokio::test]
    async fn subprocess_passthrough_protocol_roundtrip() {
        let w = match spawn_subprocess_passthrough() {
            Ok(w) => w,
            Err(_) => return, // 无 `cat` 时跳过（极少见）
        };
        let f = TranscodeFrame {
            profile: AbrProfile::Source,
            timestamp_ms: 99,
            flv_tag_bytes: Bytes::from_static(&[9, 8, 7, 6, 5, 4, 3, 2, 1]),
        };
        let out = w.process(f.clone()).await.unwrap();
        assert_eq!(out.len(), 1);
        assert_eq!(&out[0].flv_tag_bytes[..], &f.flv_tag_bytes[..]);
        w.shutdown().await.unwrap();
    }

    #[test]
    fn ffmpeg_ladder_legacy_api_returns_migrate_error() {
        let r = FfmpegSubprocessWorker::ffmpeg_ladder(&[AbrProfile::Hd720]);
        assert!(r.is_err());
    }

    #[tokio::test]
    async fn fake_ladder_emits_per_profile() {
        let w = FakeLadderWorker::new(vec![AbrProfile::Hd720, AbrProfile::Sd480]);
        let f = TranscodeFrame {
            profile: AbrProfile::Source,
            timestamp_ms: 1234,
            flv_tag_bytes: Bytes::from_static(&[1, 2, 3, 4, 5]),
        };
        let out = w.process(f).await.unwrap();
        assert_eq!(out.len(), 2);
        let labels: Vec<&str> = out.iter().map(|t| t.profile.label()).collect();
        assert!(labels.contains(&"hd720"));
        assert!(labels.contains(&"sd480"));
    }

    #[tokio::test]
    async fn ffmpeg_ladder_spawn_source_only_when_available() {
        // 探测：无 ffmpeg 即跳过；Source leg 总走 cat，与环境无关。
        let w = match FfmpegLadderWorker::spawn(&[AbrProfile::Source]).await {
            Ok(w) => w,
            Err(_) => return,
        };
        let f = TranscodeFrame {
            profile: AbrProfile::Source,
            timestamp_ms: 100,
            flv_tag_bytes: Bytes::from_static(&[7, 7, 7, 7]),
        };
        // 第一次 process 可能返回 0 帧（warm-up），允许；
        // 多调几次后应当能 drain。
        for _ in 0..16 {
            let _ = w.process(f.clone()).await.unwrap();
        }
        let m = w.metrics_snapshot();
        assert_eq!(m.len(), 1);
        assert!(m[0].bytes_in > 0);
        w.shutdown().await.unwrap();
    }
}
