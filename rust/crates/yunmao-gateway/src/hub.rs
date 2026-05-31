//! 房间订阅与事件扇出 Hub。
//!
//! 一个 Hub 实例 = 一个网关进程内的全局协调器。
//!
//! 设计要点：
//!
//! - 连接维度：每个连接拥有一个 `mpsc::Sender<Bytes>`，writer task 独占消费。
//! - 房间维度：`HashMap<RoomId, HashSet<ConnId>>`；订阅 / 退订时改这个表。
//! - 广播：`broadcast(room, msg)` 用 `room → conns` 反查发到每个连接的 mpsc。
//! - 投递失败（mpsc 满或断）会立刻清理订阅，避免“死订阅占住房间”。

use std::collections::{HashMap, HashSet};
use std::sync::Arc;

use bytes::Bytes;
use tokio::sync::{mpsc, RwLock};
use tracing::warn;

pub type ConnId = String;
pub type RoomId = String;

/// 一条扇出消息：已经序列化过的 JSON 字节，避免每个连接 re-serialize。
#[derive(Clone)]
pub struct RoomBroadcastMessage {
    pub room_id: RoomId,
    pub payload: Bytes,
}

#[derive(Debug, Default, Clone)]
pub struct HubMetrics {
    pub total_connections: u64,
    pub total_subscriptions: u64,
}

#[derive(Default)]
struct HubInner {
    /// connection id -> outbound channel sender
    conns: HashMap<ConnId, mpsc::Sender<Bytes>>,
    /// room id -> set of connection ids
    rooms: HashMap<RoomId, HashSet<ConnId>>,
    /// connection id -> subscribed rooms
    conn_rooms: HashMap<ConnId, HashSet<RoomId>>,
}

#[derive(Clone, Default)]
pub struct Hub {
    inner: Arc<RwLock<HubInner>>,
}

impl Hub {
    pub fn new() -> Self {
        Self::default()
    }

    pub async fn register(&self, conn_id: ConnId, sender: mpsc::Sender<Bytes>) {
        let mut g = self.inner.write().await;
        g.conns.insert(conn_id.clone(), sender);
        g.conn_rooms.entry(conn_id).or_default();
    }

    pub async fn deregister(&self, conn_id: &ConnId) {
        let mut g = self.inner.write().await;
        g.conns.remove(conn_id);
        if let Some(rooms) = g.conn_rooms.remove(conn_id) {
            for room in rooms {
                if let Some(set) = g.rooms.get_mut(&room) {
                    set.remove(conn_id);
                    if set.is_empty() {
                        g.rooms.remove(&room);
                    }
                }
            }
        }
    }

    pub async fn subscribe(&self, conn_id: &ConnId, rooms: Vec<RoomId>) {
        let mut g = self.inner.write().await;
        // 不存在的 conn 直接忽略（可能已经被 deregister）
        if !g.conns.contains_key(conn_id) {
            return;
        }
        let conn_rooms = g.conn_rooms.entry(conn_id.clone()).or_default();
        let mut to_add = Vec::new();
        for r in rooms {
            if conn_rooms.insert(r.clone()) {
                to_add.push(r);
            }
        }
        for r in to_add {
            g.rooms.entry(r).or_default().insert(conn_id.clone());
        }
    }

    pub async fn unsubscribe(&self, conn_id: &ConnId, rooms: Vec<RoomId>) {
        let mut g = self.inner.write().await;
        if let Some(conn_rooms) = g.conn_rooms.get_mut(conn_id) {
            for r in &rooms {
                conn_rooms.remove(r);
            }
        }
        for r in rooms {
            if let Some(set) = g.rooms.get_mut(&r) {
                set.remove(conn_id);
                if set.is_empty() {
                    g.rooms.remove(&r);
                }
            }
        }
    }

    /// 房间扇出。返回成功投递条数。
    pub async fn broadcast(&self, msg: RoomBroadcastMessage) -> usize {
        // 第一阶段：拷贝目标 conn id 列表
        let targets: Vec<ConnId> = {
            let g = self.inner.read().await;
            match g.rooms.get(&msg.room_id) {
                Some(set) => set.iter().cloned().collect(),
                None => return 0,
            }
        };

        // 第二阶段：尝试投递；失败的连接事后清理
        let mut delivered = 0usize;
        let mut dead = Vec::new();
        {
            let g = self.inner.read().await;
            for c in &targets {
                if let Some(sender) = g.conns.get(c) {
                    match sender.try_send(msg.payload.clone()) {
                        Ok(()) => delivered += 1,
                        Err(mpsc::error::TrySendError::Full(_)) => {
                            warn!(conn = %c, "outbound channel full; dropping conn");
                            dead.push(c.clone());
                        }
                        Err(mpsc::error::TrySendError::Closed(_)) => dead.push(c.clone()),
                    }
                }
            }
        }
        if !dead.is_empty() {
            for c in dead {
                self.deregister(&c).await;
            }
        }
        delivered
    }

    /// 直接给一个连接发消息（如握手响应、Pong）。
    pub async fn send_direct(&self, conn_id: &ConnId, payload: Bytes) -> bool {
        let sender = {
            let g = self.inner.read().await;
            g.conns.get(conn_id).cloned()
        };
        match sender {
            Some(s) => match s.try_send(payload) {
                Ok(()) => true,
                Err(_) => {
                    self.deregister(conn_id).await;
                    false
                }
            },
            None => false,
        }
    }

    pub async fn metrics(&self) -> HubMetrics {
        let g = self.inner.read().await;
        let total_subs: usize = g.conn_rooms.values().map(|s| s.len()).sum();
        HubMetrics {
            total_connections: g.conns.len() as u64,
            total_subscriptions: total_subs as u64,
        }
    }

    pub async fn room_subscribers(&self, room: &str) -> usize {
        self.inner
            .read()
            .await
            .rooms
            .get(room)
            .map(|s| s.len())
            .unwrap_or(0)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    async fn make_pair() -> (mpsc::Sender<Bytes>, mpsc::Receiver<Bytes>) {
        mpsc::channel(64)
    }

    #[tokio::test]
    async fn subscribe_and_broadcast() {
        let hub = Hub::new();
        let (tx_a, mut rx_a) = make_pair().await;
        let (tx_b, mut rx_b) = make_pair().await;

        hub.register("a".into(), tx_a).await;
        hub.register("b".into(), tx_b).await;
        hub.subscribe(&"a".into(), vec!["room1".into()]).await;
        hub.subscribe(&"b".into(), vec!["room1".into(), "room2".into()])
            .await;

        let n = hub
            .broadcast(RoomBroadcastMessage {
                room_id: "room1".into(),
                payload: Bytes::from_static(b"hi"),
            })
            .await;
        assert_eq!(n, 2);
        assert_eq!(rx_a.try_recv().unwrap(), Bytes::from_static(b"hi"));
        assert_eq!(rx_b.try_recv().unwrap(), Bytes::from_static(b"hi"));

        let n2 = hub
            .broadcast(RoomBroadcastMessage {
                room_id: "room_unknown".into(),
                payload: Bytes::from_static(b"x"),
            })
            .await;
        assert_eq!(n2, 0);
    }

    #[tokio::test]
    async fn unsubscribe_removes_target() {
        let hub = Hub::new();
        let (tx_a, mut rx_a) = make_pair().await;
        hub.register("a".into(), tx_a).await;
        hub.subscribe(&"a".into(), vec!["r".into()]).await;
        assert_eq!(hub.room_subscribers("r").await, 1);
        hub.unsubscribe(&"a".into(), vec!["r".into()]).await;
        assert_eq!(hub.room_subscribers("r").await, 0);

        let n = hub
            .broadcast(RoomBroadcastMessage {
                room_id: "r".into(),
                payload: Bytes::from_static(b"x"),
            })
            .await;
        assert_eq!(n, 0);
        assert!(rx_a.try_recv().is_err());
    }

    #[tokio::test]
    async fn deregister_cleans_rooms() {
        let hub = Hub::new();
        let (tx_a, _rx_a) = make_pair().await;
        hub.register("a".into(), tx_a).await;
        hub.subscribe(&"a".into(), vec!["x".into(), "y".into()])
            .await;
        assert_eq!(hub.metrics().await.total_subscriptions, 2);
        hub.deregister(&"a".into()).await;
        assert_eq!(hub.metrics().await.total_subscriptions, 0);
        assert_eq!(hub.metrics().await.total_connections, 0);
    }

    #[tokio::test]
    async fn capacity_10k_register_and_fanout() {
        let hub = Hub::new();
        let total_conns = 10_000u32;
        let rooms = 50u32;
        let msgs_per_room = 20u32;
        let chan_size = 1024;

        let mut receivers: Vec<mpsc::Receiver<Bytes>> = Vec::with_capacity(total_conns as usize);
        let register_start = std::time::Instant::now();
        for i in 0..total_conns {
            let (tx, rx) = mpsc::channel(chan_size);
            let conn_id = format!("c_{}", i);
            hub.register(conn_id, tx).await;
            receivers.push(rx);
        }
        let register_elapsed = register_start.elapsed();

        let room_names: Vec<String> = (0..rooms).map(|r| format!("room_{}", r)).collect();

        let sub_start = std::time::Instant::now();
        for i in 0..total_conns {
            let conn_id = format!("c_{}", i);
            let r = room_names[(i as usize) % rooms as usize].clone();
            hub.subscribe(&conn_id, vec![r]).await;
        }
        let sub_elapsed = sub_start.elapsed();

        let m = hub.metrics().await;
        assert_eq!(m.total_connections, total_conns as u64);
        assert_eq!(m.total_subscriptions, total_conns as u64);

        let payload = Bytes::from_static(b"capacity-bench");
        let fanout_start = std::time::Instant::now();
        let mut total_delivered = 0u64;
        for _ in 0..msgs_per_room {
            for r in &room_names {
                let n = hub
                    .broadcast(RoomBroadcastMessage {
                        room_id: r.clone(),
                        payload: payload.clone(),
                    })
                    .await;
                total_delivered += n as u64;
            }
        }
        let fanout_elapsed = fanout_start.elapsed();

        let expected_per_room = total_conns / rooms;
        let expected_total = expected_per_room as u64 * rooms as u64 * msgs_per_room as u64;
        assert_eq!(total_delivered, expected_total);

        let throughput = total_delivered as f64 / fanout_elapsed.as_secs_f64();
        let conn_per_sec = total_conns as f64 / register_elapsed.as_secs_f64();

        println!("=== Hub Capacity Benchmark ===");
        println!("connections:       {}", total_conns);
        println!("rooms:             {}", rooms);
        println!("msgs_per_room:     {}", msgs_per_room);
        println!("total_delivered:   {}", total_delivered);
        println!("register_elapsed:  {:?}", register_elapsed);
        println!("register_throughput: {:.0} conn/s", conn_per_sec);
        println!("subscribe_elapsed: {:?}", sub_elapsed);
        println!("fanout_elapsed:    {:?}", fanout_elapsed);
        println!("fanout_throughput: {:.0} msg/s", throughput);
        println!("==============================");

        assert!(
            throughput > 50_000.0,
            "fanout throughput {throughput:.0} msg/s below 50k minimum"
        );

        assert!(
            conn_per_sec > 10_000.0,
            "register throughput {conn_per_sec:.0} conn/s below 10k minimum"
        );
    }

    #[tokio::test]
    async fn connection_churn_during_broadcast() {
        let hub = Hub::new();
        let total_conns = 2_000u32;
        let churn_fraction = total_conns / 2;
        let rooms = 20u32;
        let chan_size = 512;

        let mut receivers: Vec<mpsc::Receiver<Bytes>> = Vec::with_capacity(total_conns as usize);

        for i in 0..total_conns {
            let (tx, rx) = mpsc::channel(chan_size);
            hub.register(format!("c_{}", i), tx).await;
            let room = format!("room_{}", i % rooms);
            hub.subscribe(&format!("c_{}", i), vec![room]).await;
            receivers.push(rx);
        }

        let m = hub.metrics().await;
        assert_eq!(m.total_connections, total_conns as u64);

        for i in 0..churn_fraction {
            hub.deregister(&format!("c_{}", i)).await;
        }
        let m2 = hub.metrics().await;
        assert_eq!(m2.total_connections, (total_conns - churn_fraction) as u64);

        let payload = Bytes::from_static(b"churn-bench");
        let room_names: Vec<String> = (0..rooms).map(|r| format!("room_{}", r)).collect();
        let mut delivered_after_churn = 0u64;
        let start = std::time::Instant::now();
        for _ in 0..10 {
            for r in &room_names {
                let n = hub
                    .broadcast(RoomBroadcastMessage {
                        room_id: r.clone(),
                        payload: payload.clone(),
                    })
                    .await;
                delivered_after_churn += n as u64;
            }
        }
        let elapsed = start.elapsed();

        let live_conns = (total_conns - churn_fraction) as u64;
        let conns_per_room = live_conns / rooms as u64;
        let expected = conns_per_room * rooms as u64 * 10;
        assert_eq!(delivered_after_churn, expected);

        let mut rejoined_receivers: Vec<mpsc::Receiver<Bytes>> =
            Vec::with_capacity(churn_fraction as usize);
        for i in 0..churn_fraction {
            let (tx, rx) = mpsc::channel(chan_size);
            hub.register(format!("c_{}", i), tx).await;
            let room = format!("room_{}", i % rooms);
            hub.subscribe(&format!("c_{}", i), vec![room]).await;
            rejoined_receivers.push(rx);
        }
        let m3 = hub.metrics().await;
        assert_eq!(m3.total_connections, total_conns as u64);

        let mut delivered_after_rejoin = 0u64;
        let start2 = std::time::Instant::now();
        for _ in 0..10 {
            for r in &room_names {
                let n = hub
                    .broadcast(RoomBroadcastMessage {
                        room_id: r.clone(),
                        payload: payload.clone(),
                    })
                    .await;
                delivered_after_rejoin += n as u64;
            }
        }
        let elapsed2 = start2.elapsed();

        let total_expected = (total_conns / rooms) as u64 * rooms as u64 * 10;
        assert_eq!(delivered_after_rejoin, total_expected);

        let tp_churn = delivered_after_churn as f64 / elapsed.as_secs_f64();
        let tp_rejoin = delivered_after_rejoin as f64 / elapsed2.as_secs_f64();

        println!("=== Connection Churn Benchmark ===");
        println!("total_conns:         {}", total_conns);
        println!("churned:             {}", churn_fraction);
        println!("delivered_after_churn: {}", delivered_after_churn);
        println!("churn_fanout_elapsed:  {:?}", elapsed);
        println!("churn_throughput:      {:.0} msg/s", tp_churn);
        println!("delivered_after_rejoin: {}", delivered_after_rejoin);
        println!("rejoin_fanout_elapsed: {:?}", elapsed2);
        println!("rejoin_throughput:     {:.0} msg/s", tp_rejoin);
        println!("==================================");

        assert!(
            tp_churn > 50_000.0,
            "churn fanout throughput {tp_churn:.0} msg/s below 50k"
        );
        assert!(
            tp_rejoin > 50_000.0,
            "rejoin fanout throughput {tp_rejoin:.0} msg/s below 50k"
        );

        drop(rejoined_receivers);
    }
}
