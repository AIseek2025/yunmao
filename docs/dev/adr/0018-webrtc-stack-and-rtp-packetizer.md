# ADR-0018: WebRTC 栈选型与 RTP packetizer 第一刀

> 时间：2026-05-25  
> 状态：Accepted（dev 路径已落地；生产 DTLS/SRTP 走 webrtc-rs，第七轮整合）  
> 关联：ADR-0011（LL-HLS）/ADR-0016（WebRTC 灰度）/ADR-0017（KMS 集成）

## 背景

第五轮已经把 WHIP/WHEP HTTP 信令路由 + `ProtocolPref` + room subscription token 鉴权钩子做完，
但 SDP answer 是 stub，没有真 RTP/SRTP/ICE/DTLS。本轮（第六轮 A 项）要把"投喂直播观众侧 WebRTC 可拉"
做到端到端 RTP packetize 可观测，并选定生产栈。

## 评估

| 方案 | 优势 | 风险 |
|---|---|---|
| `webrtc-rs`（`webrtc = 0.x`，meta-crate） | 全栈：ICE+DTLS+SRTP+RTP+SCTP+DataChannel；社区活跃 | 依赖 `openssl-sys`/`ring`，本地编译时间长；CI 镜像变大；`librustls` 与系统 OpenSSL 不冲突也要注意；API 仍 v0.x 频繁变更 |
| `str0m` | 纯 Rust（不依赖 OpenSSL）；事件驱动；codec-agnostic | 上手陡峭（用户需要自管 timer、network IO）；社区比 webrtc-rs 小 |
| 自写 RTP/RTCP + 外接 DTLS proxy | 编译最快；裁剪最小 | DTLS/SRTP 必须找现成实现（如 libnice + libsrtp 或 openssl）；维护成本 |

## 决策

1. **生产栈**：webrtc-rs（`webrtc = "0.x"`，与社区主流 SFU 选型一致）。第七轮替换 stub 路径，
   提供 `webrtc_backend::*` 模块，把 ICE/DTLS/SRTP 委托给 webrtc-rs，**RTP packetizer 沿用
   本轮自写实现**（webrtc-rs 的 Packetizer trait 兼容）。
2. **本轮（dev 路径）**：
   - 自实现 RTP packetizer（`rust/crates/yunmao-webrtc/src/rtp.rs`）：
     - H.264：Single NAL Unit / FU-A / STAP-A（RFC 3984）；
     - AAC：MPEG4-GENERIC AAC-hbr（RFC 3640）；
     - Opus：passthrough（RFC 7587）；
   - 房间级 RTP 扇出 hub（`sfu.rs::RtpRoomHub`）：上游 WHIP / RTMP 推帧 → packetize → 扇出给所有 WHEP
     订阅者；订阅者通过 `tokio::sync::mpsc::Receiver<RtpPacket>` 拉流；
   - WHIP/WHEP HTTP 路由（`whip_whep.rs`）：注入 hub 后，SDP answer 追加真实 m=video/audio + payload
     type + fmtp，能让客户端完成 codec 协商；真 DTLS/SRTP 用 stub（dev 模式下浏览器握手会因为指纹
     缺失而失败，但 packetize 与扇出已经在跑）。
3. **ICE 服务器**：默认 `stun:stun.l.google.com:19302`；TURN 通过 ADR-0017 的 KeyProvider 短期签发凭证。
4. **CI / 集成测试**：
   - `cargo test -p yunmao-webrtc` 跑 packetizer 单测 + RtpRoomHub fan-out 测试，验证收到 ≥ 30 video RTP
     packet、AAC packet、unsubscribe 清理；
   - 真 SRTP/E2E 联调（OBS WebRTC plugin → WHIP → WHEP → Chromium）在 ADR-0018 第七轮加测试矩阵，
     需要 docker compose 起 coturn + chrome headless。

## 后果

- 本轮 cargo test 可跑通 RTP 路径，不再依赖外部 webrtc-rs / openssl。
- 第七轮接入 webrtc-rs 时只需替换 `sfu.rs::RtpRoomHub` 内部的 packetizer 与 DTLS/SRTP 出口，
  trait 边界稳定。
- WHIP 上行：必须在 RTMP→WHIP 桥接打通后才能真正喂帧。本轮 hub 提供 `push_video_avcc` / `push_audio_aac`
  接口，media-edge `MediaSink` 适配器在第七轮新增（同时 LL-HLS 与 WHEP 共享一份解码前帧流）。
- TURN 凭证：dev 默认 None；生产由 user-svc 通过 ADR-0017 KeyProvider 短期 HMAC 凭证签发，
  避免长 TURN 密码泄漏。

## 复现命令（真 SRTP 联调，第七轮 CI 会做）

```bash
# 本机起 coturn（默认 3478）
docker run -p 3478:3478/udp -p 49152-49162:49152-49162/udp coturn/coturn:4 \
  -n --no-cli --lt-cred-mech --fingerprint --realm=yunmao

# 真 webrtc-rs 集成测试（需 ADR-0018 第七轮 webrtc_backend 模块）
cd rust && cargo test -p yunmao-webrtc --features=webrtc_full -- --ignored

# OBS WebRTC plugin → WHIP（手动）
# 1. obs-studio + obs-webrtc-plugin（v1.4+）
# 2. Settings → Stream → Server: http://127.0.0.1:8082/whip/room_demo
# 3. Bearer: <user_svc 签发的 room subscription token>
```
