//! 房间维度的发布者路由表：`{room_id} → MediaPipe`。
//!
//! 由 ingest 的 RTMP 接入端在 publish 阶段调用 `register_publisher`；
//! 由 media-edge 的 HTTP-FLV 出口调用 `subscribe`。

use std::collections::HashMap;
use std::sync::Arc;

use tokio::sync::RwLock;
use tracing::{info, warn};

use crate::pipe::MediaPipe;

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct RoomKey(pub String);

impl RoomKey {
    pub fn new(s: impl Into<String>) -> Self {
        Self(s.into())
    }
}

#[derive(Default, Clone)]
pub struct PublishRouter {
    inner: Arc<RwLock<HashMap<RoomKey, MediaPipe>>>,
}

impl PublishRouter {
    pub fn new() -> Self {
        Self::default()
    }

    /// 由 RTMP publish 阶段调用：拿到（或创建）一个 pipe。
    ///
    /// 如果已有同 room 的 pipe，**抢占**：旧推流被踢，新推流接入。这是标准直播平台的行为。
    pub async fn register_publisher(&self, room: RoomKey) -> MediaPipe {
        let mut g = self.inner.write().await;
        if let Some(existing) = g.remove(&room) {
            warn!(room = %room.0, "preempting existing publisher: {} subscribers", existing.live_subscribers());
        }
        let pipe = MediaPipe::new();
        g.insert(room.clone(), pipe.clone());
        info!(room = %room.0, "registered publisher");
        pipe
    }

    /// 拉流端订阅；如果该房间未在线则返回 None。
    pub async fn subscribe(&self, room: &RoomKey) -> Option<MediaPipe> {
        let g = self.inner.read().await;
        g.get(room).cloned()
    }

    /// 推流断开
    pub async fn unregister(&self, room: &RoomKey) {
        let mut g = self.inner.write().await;
        if g.remove(room).is_some() {
            info!(room = %room.0, "publisher unregistered");
        }
    }

    pub async fn snapshot(&self) -> Vec<RoomKey> {
        self.inner.read().await.keys().cloned().collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn register_and_subscribe() {
        let r = PublishRouter::new();
        let key = RoomKey::new("room_demo");
        assert!(r.subscribe(&key).await.is_none());

        let _pipe = r.register_publisher(key.clone()).await;
        assert!(r.subscribe(&key).await.is_some());

        r.unregister(&key).await;
        assert!(r.subscribe(&key).await.is_none());
    }

    #[tokio::test]
    async fn re_register_preempts() {
        let r = PublishRouter::new();
        let key = RoomKey::new("room_a");
        let p1 = r.register_publisher(key.clone()).await;
        let p2 = r.register_publisher(key.clone()).await;
        // p1 与 p2 是不同 pipe（不同 broadcast channel）
        assert_eq!(p1.live_subscribers(), 0);
        assert_eq!(p2.live_subscribers(), 0);
    }
}
