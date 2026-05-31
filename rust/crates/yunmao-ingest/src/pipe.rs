//! `MediaPipe` 是 ingest → media-edge 的内存管道。
//!
//! 设计：
//!
//! - 一个 publisher（推流端）写入；多个 subscriber（拉流端 / 转码 worker）只读。
//! - 使用 `tokio::sync::broadcast` 提供高效扇出；当订阅者落后时，broadcast 会把对应订阅者的 channel 标记为 lagged。
//! - 缓存最新 metadata、AVC sequence header、AAC sequence header、最近一个 GOP 的关键帧用于"快速首帧"，与 02 第 11 节的 ABR 起播策略一致。

use std::sync::Arc;

use tokio::sync::{broadcast, RwLock};
use tracing::warn;

use crate::flv::FlvTag;

const BROADCAST_CAP: usize = 1024;

#[derive(Debug, Default)]
struct Cache {
    metadata: Option<FlvTag>,
    avc_seq: Option<FlvTag>,
    aac_seq: Option<FlvTag>,
    /// 最近一个 GOP 的 video 关键帧及其后的 P/B 帧
    last_gop: Vec<FlvTag>,
}

#[derive(Clone)]
pub struct MediaPipe {
    sender: broadcast::Sender<FlvTag>,
    cache: Arc<RwLock<Cache>>,
}

impl MediaPipe {
    pub fn new() -> Self {
        let (sender, _) = broadcast::channel(BROADCAST_CAP);
        Self {
            sender,
            cache: Arc::new(RwLock::new(Cache::default())),
        }
    }

    /// 推一个 tag；同步缓存与广播。
    pub async fn publish(&self, tag: FlvTag) {
        {
            let mut c = self.cache.write().await;
            // metadata
            if matches!(tag.kind, crate::flv::TagKind::ScriptData) {
                c.metadata = Some(tag.clone());
            }
            // 视频/音频 sequence header
            if tag.is_avc_sequence_header() {
                c.avc_seq = Some(tag.clone());
            }
            if matches!(tag.kind, crate::flv::TagKind::Audio)
                && tag.data.len() >= 2
                && tag.data[0] == 0xaf
                && tag.data[1] == 0x00
            {
                c.aac_seq = Some(tag.clone());
            }
            // GOP 缓存：碰到关键帧重置
            if tag.is_video_keyframe() {
                c.last_gop.clear();
            }
            if matches!(
                tag.kind,
                crate::flv::TagKind::Video | crate::flv::TagKind::Audio
            ) {
                c.last_gop.push(tag.clone());
                // 防御性截断
                if c.last_gop.len() > 4096 {
                    let drop_n = c.last_gop.len() - 4096;
                    c.last_gop.drain(0..drop_n);
                }
            }
        }

        // 没有订阅者也允许；broadcast 会丢弃
        if let Err(e) = self.sender.send(tag) {
            // 单订阅者退出场景属于正常
            tracing::trace!(error = %e, "broadcast send had no live receiver");
        }
    }

    pub async fn subscribe(&self) -> MediaSubscriber {
        // 先拿到当前缓存快照，再订阅；这样订阅者一开始就能拿到 metadata + sequence header + 最近一个 GOP
        let snapshot = {
            let c = self.cache.read().await;
            let mut tags = Vec::new();
            if let Some(t) = &c.metadata {
                tags.push(t.clone());
            }
            if let Some(t) = &c.avc_seq {
                tags.push(t.clone());
            }
            if let Some(t) = &c.aac_seq {
                tags.push(t.clone());
            }
            tags.extend(c.last_gop.iter().cloned());
            tags
        };

        let rx = self.sender.subscribe();
        MediaSubscriber {
            warmup: snapshot.into_iter(),
            rx,
        }
    }

    pub fn live_subscribers(&self) -> usize {
        self.sender.receiver_count()
    }
}

impl Default for MediaPipe {
    fn default() -> Self {
        Self::new()
    }
}

/// 订阅者：先发缓存，再发实时 broadcast。
pub struct MediaSubscriber {
    warmup: std::vec::IntoIter<FlvTag>,
    rx: broadcast::Receiver<FlvTag>,
}

impl MediaSubscriber {
    pub async fn next_tag(&mut self) -> Option<FlvTag> {
        if let Some(t) = self.warmup.next() {
            return Some(t);
        }
        match self.rx.recv().await {
            Ok(t) => Some(t),
            Err(broadcast::error::RecvError::Closed) => None,
            Err(broadcast::error::RecvError::Lagged(n)) => {
                warn!("media subscriber lagged by {n} tags, will resync from next tag");
                // 业务允许丢帧，继续等下一帧
                self.rx.recv().await.ok()
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use bytes::Bytes;

    use super::*;
    use crate::flv::TagKind;

    fn vid_kf() -> FlvTag {
        FlvTag {
            kind: TagKind::Video,
            timestamp_ms: 0,
            data: Bytes::from_static(&[0x17, 0x01, 0, 0, 0, 0xff]),
        }
    }
    fn vid_seq() -> FlvTag {
        FlvTag {
            kind: TagKind::Video,
            timestamp_ms: 0,
            data: Bytes::from_static(&[0x17, 0x00, 0, 0, 0]),
        }
    }

    #[tokio::test]
    async fn subscriber_gets_warmup_then_live() {
        let pipe = MediaPipe::new();
        pipe.publish(vid_seq()).await;
        pipe.publish(vid_kf()).await;

        let mut sub = pipe.subscribe().await;
        let first = sub.next_tag().await.unwrap();
        assert!(first.is_avc_sequence_header());
        let second = sub.next_tag().await.unwrap();
        assert!(second.is_video_keyframe());

        // 实时帧
        let new_tag = FlvTag {
            kind: TagKind::Video,
            timestamp_ms: 100,
            data: Bytes::from_static(&[0x27, 0x01, 0, 0, 0]),
        };
        let task_tag = new_tag.clone();
        let pipe_clone = pipe.clone();
        let handle = tokio::spawn(async move {
            tokio::time::sleep(std::time::Duration::from_millis(10)).await;
            pipe_clone.publish(task_tag).await;
        });
        let third = sub.next_tag().await.unwrap();
        assert_eq!(third.timestamp_ms, 100);
        handle.await.unwrap();
    }
}
