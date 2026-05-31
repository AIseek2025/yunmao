//! WebSocket 网关基准压测工具（第五轮加强版）。
//!
//! 新增能力：
//!   - 连接握手速率限速（YUNMAO_BENCH_RATE_PER_SEC）
//!   - 订阅模式：hot（连完立即订阅） / cold（按 cold_delay_secs 延迟订阅，模拟真实用户）
//!   - 消息接收延迟直方图（接收端记录 receive_ts - server_ts，单位 ms）
//!   - JSON 输出：摘要写入 YUNMAO_BENCH_OUT_JSON 文件 + 同步打印
//!
//! ENV：
//!
//! - YUNMAO_BENCH_URL                 单 URL（与 YUNMAO_BENCH_URL_LIST 二选一）
//! - YUNMAO_BENCH_URL_LIST            逗号分隔，按连接 round-robin
//! - YUNMAO_BENCH_CONNS               目标连接数
//! - YUNMAO_BENCH_ROOMS               订阅房间数（每连接订阅 1 房间）
//! - YUNMAO_BENCH_DURATION_SECS       持续时间（默认 60s）
//! - YUNMAO_BENCH_RAMP_SECS           爬坡时间（默认 30s）
//! - YUNMAO_BENCH_RATE_PER_SEC        每秒握手上限（默认 conns/ramp）
//! - YUNMAO_BENCH_SUB_MODE            "hot"|"cold" 默认 hot
//! - YUNMAO_BENCH_COLD_DELAY_SECS     cold 模式订阅延迟（默认 10s）
//! - YUNMAO_BENCH_OUT_JSON            输出 JSON 路径（可选）
//! - YUNMAO_BENCH_TOKEN               预先拿好的 login JWT（可选）

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use futures::{SinkExt, StreamExt};
use tokio::sync::Mutex;
use tokio::time::sleep;
use tokio_tungstenite::tungstenite::Message;

const LATENCY_BUCKETS_MS: &[u64] = &[1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000];

#[derive(Default)]
struct Counters {
    connected: AtomicU64,
    failed: AtomicU64,
    msgs_in: AtomicU64,
    subscribe_acks: AtomicU64,
    errors: AtomicU64,
    latency_count: AtomicU64,
    latency_sum_us: AtomicU64,
    latency_buckets: Vec<AtomicU64>, // 长度 = LATENCY_BUCKETS_MS.len()+1
    /// 用于估算 P50/P95/P99：rolling reservoir
    samples: Mutex<Vec<u64>>,
}

impl Counters {
    fn new() -> Self {
        let mut buckets = Vec::with_capacity(LATENCY_BUCKETS_MS.len() + 1);
        for _ in 0..=LATENCY_BUCKETS_MS.len() {
            buckets.push(AtomicU64::new(0));
        }
        Self {
            latency_buckets: buckets,
            ..Default::default()
        }
    }

    fn observe_latency_us(&self, us: u64) {
        self.latency_count.fetch_add(1, Ordering::Relaxed);
        self.latency_sum_us.fetch_add(us, Ordering::Relaxed);
        let ms = us / 1000;
        let mut idx = LATENCY_BUCKETS_MS.len();
        for (i, b) in LATENCY_BUCKETS_MS.iter().enumerate() {
            if ms <= *b {
                idx = i;
                break;
            }
        }
        self.latency_buckets[idx].fetch_add(1, Ordering::Relaxed);
    }
}

#[tokio::main(flavor = "multi_thread", worker_threads = 8)]
async fn main() -> anyhow::Result<()> {
    let urls = env_url_list();
    let conns: usize = env_usize("YUNMAO_BENCH_CONNS", 5_000);
    let rooms: usize = env_usize("YUNMAO_BENCH_ROOMS", 20);
    let duration_secs: u64 = env_u64("YUNMAO_BENCH_DURATION_SECS", 60);
    let ramp_secs: u64 = env_u64("YUNMAO_BENCH_RAMP_SECS", 30);
    let rate_per_sec: u64 = env_u64("YUNMAO_BENCH_RATE_PER_SEC", conns as u64 / ramp_secs.max(1));
    let sub_mode = std::env::var("YUNMAO_BENCH_SUB_MODE").unwrap_or_else(|_| "hot".into());
    let cold_delay_secs: u64 = env_u64("YUNMAO_BENCH_COLD_DELAY_SECS", 10);
    let token = std::env::var("YUNMAO_BENCH_TOKEN").ok();
    let out_json = std::env::var("YUNMAO_BENCH_OUT_JSON").ok();

    println!(
        "[bench] urls={urls:?} conns={conns} rooms={rooms} duration={duration_secs}s ramp={ramp_secs}s rate={rate_per_sec}/s mode={sub_mode}"
    );

    let counters = Arc::new(Counters::new());
    let started = Instant::now();

    let mut handles = Vec::with_capacity(conns);
    let mut launched_in_window: u64 = 0;
    let mut window_start = Instant::now();
    for i in 0..conns {
        let url = urls[i % urls.len()].clone();
        let counters = counters.clone();
        let room = format!("room_{}", i % rooms);
        let mode_owned = sub_mode.clone();
        let tok = token.clone();
        handles.push(tokio::spawn(connect_one(
            url,
            room,
            duration_secs,
            mode_owned,
            cold_delay_secs,
            tok,
            counters,
        )));
        launched_in_window += 1;
        if launched_in_window >= rate_per_sec {
            let elapsed = window_start.elapsed();
            if elapsed < Duration::from_secs(1) {
                sleep(Duration::from_secs(1) - elapsed).await;
            }
            window_start = Instant::now();
            launched_in_window = 0;
        }
    }

    let counters_p = counters.clone();
    let progress = tokio::spawn(async move {
        let p_started = Instant::now();
        loop {
            sleep(Duration::from_secs(5)).await;
            let elapsed = p_started.elapsed().as_secs_f64();
            println!(
                "[bench] t={:>5.1}s connected={} failed={} msgs_in={} subscribed={} errors={} lat_samples={}",
                elapsed,
                counters_p.connected.load(Ordering::Relaxed),
                counters_p.failed.load(Ordering::Relaxed),
                counters_p.msgs_in.load(Ordering::Relaxed),
                counters_p.subscribe_acks.load(Ordering::Relaxed),
                counters_p.errors.load(Ordering::Relaxed),
                counters_p.latency_count.load(Ordering::Relaxed),
            );
        }
    });

    for h in handles {
        let _ = h.await;
    }
    progress.abort();

    let total = started.elapsed();
    let summary = build_summary(&counters, total).await;
    println!(
        "[bench] DONE {}",
        serde_json::to_string_pretty(&summary).unwrap()
    );
    if let Some(path) = out_json {
        if let Ok(s) = serde_json::to_string_pretty(&summary) {
            let _ = std::fs::write(&path, s);
            println!("[bench] summary written to {}", path);
        }
    }
    Ok(())
}

fn env_usize(key: &str, def: usize) -> usize {
    std::env::var(key)
        .ok()
        .and_then(|s| s.parse().ok())
        .unwrap_or(def)
}
fn env_u64(key: &str, def: u64) -> u64 {
    std::env::var(key)
        .ok()
        .and_then(|s| s.parse().ok())
        .unwrap_or(def)
}
fn env_url_list() -> Vec<String> {
    if let Ok(v) = std::env::var("YUNMAO_BENCH_URL_LIST") {
        let list: Vec<String> = v
            .split(',')
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect();
        if !list.is_empty() {
            return list;
        }
    }
    vec![std::env::var("YUNMAO_BENCH_URL").unwrap_or_else(|_| "ws://localhost:8090/ws".into())]
}

async fn connect_one(
    url: String,
    room: String,
    hold_secs: u64,
    sub_mode: String,
    cold_delay_secs: u64,
    token: Option<String>,
    c: Arc<Counters>,
) {
    let conn_res = if let Some(_tok) = &token {
        // 这里仅示意：真正的 Auth 走 WS Frame，token 注入到 subscribe 帧
        tokio_tungstenite::connect_async(&url).await
    } else {
        tokio_tungstenite::connect_async(&url).await
    };
    let conn = match conn_res {
        Ok((s, _)) => s,
        Err(_) => {
            c.failed.fetch_add(1, Ordering::Relaxed);
            return;
        }
    };
    c.connected.fetch_add(1, Ordering::Relaxed);
    let (mut sink, mut stream) = conn.split();

    let subscribe_frame = || {
        serde_json::json!({
            "op": "subscribe",
            "rooms": [room],
            "token": token,
        })
    };

    if sub_mode == "hot" {
        let frame = subscribe_frame();
        let _ = sink.send(Message::Text(frame.to_string())).await;
    }

    let deadline = Instant::now() + Duration::from_secs(hold_secs);
    let mut cold_due = if sub_mode == "cold" {
        Some(Instant::now() + Duration::from_secs(cold_delay_secs))
    } else {
        None
    };

    while Instant::now() < deadline {
        if let Some(due) = cold_due {
            if Instant::now() >= due {
                let frame = subscribe_frame();
                let _ = sink.send(Message::Text(frame.to_string())).await;
                cold_due = None;
            }
        }
        let timeout = deadline
            .saturating_duration_since(Instant::now())
            .min(Duration::from_secs(1));
        let msg = tokio::time::timeout(timeout, stream.next()).await;
        match msg {
            Ok(Some(Ok(Message::Text(t)))) => {
                c.msgs_in.fetch_add(1, Ordering::Relaxed);
                if t.contains("\"subscribed\"") {
                    c.subscribe_acks.fetch_add(1, Ordering::Relaxed);
                }
                // 解析事件 ts（server_ts_ms 字段），计算 receive_latency
                if let Some(ts_ms) = parse_event_ts_ms(&t) {
                    let now_ms = SystemTime::now()
                        .duration_since(UNIX_EPOCH)
                        .map(|d| d.as_millis() as u64)
                        .unwrap_or(0);
                    if now_ms > ts_ms {
                        let lat_us = (now_ms - ts_ms) * 1000;
                        c.observe_latency_us(lat_us);
                        let mut s = c.samples.lock().await;
                        if s.len() < 100_000 {
                            s.push(lat_us);
                        }
                    }
                }
            }
            Ok(Some(Ok(_))) => {}
            Ok(None) | Err(_) => {
                continue;
            }
            Ok(Some(Err(_))) => {
                c.errors.fetch_add(1, Ordering::Relaxed);
                break;
            }
        }
    }
}

fn parse_event_ts_ms(s: &str) -> Option<u64> {
    // 简化：找 "ts":<num>
    let key = "\"ts\":";
    if let Some(i) = s.find(key) {
        let tail = &s[i + key.len()..];
        let end = tail
            .find(|c: char| !c.is_ascii_digit())
            .unwrap_or(tail.len());
        return tail[..end].parse::<u64>().ok();
    }
    None
}

async fn build_summary(c: &Counters, total: Duration) -> serde_json::Value {
    let samples = c.samples.lock().await.clone();
    let mut sorted = samples;
    sorted.sort_unstable();
    let percentile = |p: f64| -> u64 {
        if sorted.is_empty() {
            0
        } else {
            let idx = ((sorted.len() as f64) * p).floor() as usize;
            sorted[idx.min(sorted.len() - 1)]
        }
    };
    let avg_us = if c.latency_count.load(Ordering::Relaxed) > 0 {
        c.latency_sum_us.load(Ordering::Relaxed) / c.latency_count.load(Ordering::Relaxed)
    } else {
        0
    };
    let bucket_values: Vec<u64> = c
        .latency_buckets
        .iter()
        .map(|a| a.load(Ordering::Relaxed))
        .collect();
    serde_json::json!({
        "duration_secs": total.as_secs_f64(),
        "connected": c.connected.load(Ordering::Relaxed),
        "failed": c.failed.load(Ordering::Relaxed),
        "subscribed": c.subscribe_acks.load(Ordering::Relaxed),
        "msgs_in": c.msgs_in.load(Ordering::Relaxed),
        "errors": c.errors.load(Ordering::Relaxed),
        "latency_ms": {
            "avg":  avg_us as f64 / 1000.0,
            "p50":  percentile(0.50)  as f64 / 1000.0,
            "p95":  percentile(0.95)  as f64 / 1000.0,
            "p99":  percentile(0.99)  as f64 / 1000.0,
            "max":  percentile(1.0)   as f64 / 1000.0,
            "buckets_ms": LATENCY_BUCKETS_MS,
            "bucket_counts": bucket_values,
            "samples": c.latency_count.load(Ordering::Relaxed),
        }
    })
}
