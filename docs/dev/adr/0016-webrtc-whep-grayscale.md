# ADR-0016：WebRTC / WHEP 灰度评估与设计

- 状态：评估（不在第四轮落地完整 SFU）
- 相关：ADR-0002（MVP 直播协议）、ADR-0011（LL-HLS）
- 影响范围：`yunmao-media-edge`、`yunmao-ingest`、`yunmao-webrtc`（新）

## 背景

LL-HLS（第三轮已真实切片）能稳定做 1.5–3s 延迟，覆盖了云养猫直播 95% 场景。
但下列场景需要更低延迟（< 500ms）或双向能力：

- **VIP 房间近实时**：付费用户看主播侧投喂示意；
- **强互动指令**：用户给猫送零食后，主播立即反馈；
- **猫舍管理员调试**：本地小范围调试摄像头时不想引入 1s 延迟。

调研了三条路径：

| 方案 | 延迟 | 自建复杂度 | 成本 | 适用 |
| --- | --- | --- | --- | --- |
| LL-HLS（现有） | 1.5–3 s | 低 | 低 | 主流直播观看 |
| WebRTC + 自建 SFU | < 300 ms | 高 | 中 | 强互动 / VIP |
| WebRTC + 托管 SFU（如 Cloudflare Calls / Agora） | < 300 ms | 极低 | 高 | 流量小 / 起步 |

## 决策

1. **不替换 LL-HLS**：所有公开房间默认 LL-HLS；WebRTC 是 **opt-in 灰度**。
2. **优先 WHIP/WHEP 协议**（IETF draft），与现有 RTMP 输入并存：
   - 输入侧：摄像头 / OBS 通过 WHIP（HTTP POST + SDP）推到 media-edge；
   - 输出侧：观众通过 WHEP（HTTP POST + SDP）拉流；
   - 信令走 HTTPS，无需独立 WebSocket 通道。
3. **第一版用 Pion**（Rust 生态用 `webrtc-rs`，Go 生态用 `pion/webrtc/v3`）自建轻量
   SFU，部署在 media-edge 节点；
   - MVP 验证 1 房间 100 观众；
   - 失败回退：自动 fallback 到 LL-HLS（前端在 `RTCPeerConnection.connectionState`
     失败时切换 video src）。
4. **TURN 集群**：第一版直接复用 Cloudflare / 阿里云 TURN 托管；自建留第六轮。
5. **房间订阅 token 复用**：room-svc 已签发 JWT，token 中加 `webrtc:true`
   scope；media-edge WHEP endpoint 校验。

## MVP 验证步骤（第六轮）

1. media-edge 增加 `/whep/:room` endpoint，返回 SDP answer；
2. media-edge 内 `yunmao-webrtc` crate 用 `webrtc-rs` 接 RTMP → WebRTC 转封装
   （H.264 → H.264 直通，不重新编码；AAC → Opus 需要 transcode）；
3. 灰度名单（feature_flags `media.webrtc_rooms`）控制；
4. 指标：`media_edge_webrtc_subscribers`、`media_edge_webrtc_rtt_ms`、
   `media_edge_webrtc_packet_loss_rate`；
5. 失败回退：前端检测到 `failed` 状态 → 重新拉 LL-HLS playlist；
6. 评估：100 观众下 GPU/CPU 占用、带宽放大系数。

## 关键决策点

- **自建 vs 托管**：第一阶段（DAU < 10k）建议直接用 Cloudflare Calls 验证业务
  需求，自建留 DAU > 100k 后；本 ADR 不锁死。
- **信令协议**：WHIP/WHEP（HTTP），不引入 SIP/XMPP/自定义 WS。
- **权限**：复用 room-svc 房间订阅 JWT，scope 增加 `webrtc:publisher` /
  `webrtc:subscriber` 区分。
- **ICE/TURN**：使用云托管 TURN，避免自建 BGP/Anycast 复杂度。
- **与 LL-HLS 关系**：并存；前端做协议协商，参数 `?proto=webrtc` 显式切换。

## 本轮已落地

- 新建 crate `rust/crates/yunmao-webrtc/`，定义 `Publisher` / `Subscriber` /
  `Signaling` trait + LocalSignaling stub，**无外部依赖**，让后续真实
  `webrtc-rs` 实现可平滑落地；
- ADR-0016（本文档）；
- web-demo 预留 `?proto=webrtc` 入口（占位 alert，等后续实现）。

## 不在本轮范围

- 真实 SDP / ICE / DTLS / SRTP 交互；
- SFU 多对多扇出实现；
- TURN 集群部署 / NAT 穿透；
- 转码（AAC → Opus）。
