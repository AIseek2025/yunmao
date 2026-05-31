//! Web/App ↔ realtime-gateway WebSocket 协议（JSON）。
//!
//! - 客户端必须先 `Subscribe { rooms }` 之后才会收到该房间事件。
//! - `Ping/Pong` 用于心跳；服务端每 30s 发一次 `Ping`，客户端必须 5s 内回 `Pong`，否则断连。
//! - 服务端通过 `Event` 帧广播 CloudEvents 信封（`data` 任意 JSON）。

use serde::{Deserialize, Serialize};
use serde_json::Value;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "op", rename_all = "snake_case")]
pub enum ClientFrame {
    /// 上行携带 JWT；可携带 user 登录 token 或 room subscription token。
    /// 客户端应当在 `Subscribe`/`Chat` 之前发送。
    Auth { token: String },
    /// 在带 token 的握手之后，订阅一组房间
    Subscribe { rooms: Vec<String> },
    /// 取消订阅
    Unsubscribe { rooms: Vec<String> },
    /// 发送一条聊天消息（首期为 fan-out + 后端审核）
    Chat {
        room_id: String,
        body: String,
        client_msg_id: String,
    },
    /// 业务层心跳（与 WebSocket Ping 帧并存）
    Ping { ts: i64 },
    /// 心跳回包
    Pong { ts: i64 },
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "op", rename_all = "snake_case")]
pub enum ServerFrame {
    /// 握手成功的欢迎包
    Hello {
        connection_id: String,
        server_time: i64,
    },
    /// 订阅成功
    Subscribed { rooms: Vec<String> },
    /// 服务器主动 Ping
    Ping { ts: i64 },
    /// 客户端 Ping 的回包
    Pong { ts: i64 },
    /// 业务事件（直接转发 CloudEvents 信封的关键字段）
    Event {
        event_type: String,
        room_id: String,
        data: Value,
        ts: i64,
    },
    /// 错误（服务器主动断连前的最后一帧）
    Error { code: String, message: String },
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn client_subscribe_roundtrip() {
        let frame = ClientFrame::Subscribe {
            rooms: vec!["room_demo".into()],
        };
        let json = serde_json::to_string(&frame).unwrap();
        let back: ClientFrame = serde_json::from_str(&json).unwrap();
        assert_eq!(frame, back);
        assert!(json.contains("\"op\":\"subscribe\""));
    }

    #[test]
    fn server_event_roundtrip() {
        let frame = ServerFrame::Event {
            event_type: "feed.command.acked".into(),
            room_id: "room_demo".into(),
            data: serde_json::json!({"status":"succeeded"}),
            ts: 1700000000,
        };
        let json = serde_json::to_string(&frame).unwrap();
        let back: ServerFrame = serde_json::from_str(&json).unwrap();
        assert_eq!(frame, back);
    }
}
