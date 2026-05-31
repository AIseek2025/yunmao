# realtime-gateway WebSocket signaling

> 实际数据结构定义在 `rust/crates/yunmao-protocol/src/signaling.rs`。本文档是“契约”视角的中文摘要。

## 连接

- URL：`ws://gateway-host:8090/ws`（生产 wss://）
- 鉴权（PoC）：暂未要求 token；Phase 1 起客户端必须以 `Authorization: Bearer <jwt>` 形式连接，由 gateway 调用 user-svc.VerifyToken 校验。

## 客户端 → 服务器

```json
{ "op": "subscribe", "rooms": ["room_demo"] }
{ "op": "unsubscribe", "rooms": ["room_demo"] }
{ "op": "chat", "room_id": "room_demo", "body": "你好", "client_msg_id": "01HX..." }
{ "op": "ping", "ts": 1700000000000 }
{ "op": "pong", "ts": 1700000000000 }
```

## 服务器 → 客户端

```json
{ "op": "hello", "connection_id": "c_01HZX...", "server_time": 1700000000 }
{ "op": "subscribed", "rooms": ["room_demo"] }
{ "op": "ping", "ts": 1700000000000 }
{ "op": "pong", "ts": 1700000000000 }
{ "op": "event", "event_type": "feed.command.acked", "room_id": "room_demo", "data": {...}, "ts": 1700000000000 }
{ "op": "error", "code": "AUTH.TOKEN_EXPIRED", "message": "..." }
```

## 心跳

服务器每 30s 发 `ping`；客户端 60s 内必须以 WebSocket Pong 帧或 `{"op":"pong"}` 回复，否则会被踢。
