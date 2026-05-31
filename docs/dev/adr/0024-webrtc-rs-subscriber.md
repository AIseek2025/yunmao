# ADR-0024：webrtc-rs Subscriber 架构与 lifecycle 管理

- 状态：Accepted（2026-05-25，第八轮 A 落地）
- 关联：ADR-0016（WebRTC 灰度策略）、ADR-0018（WebRTC 栈与 RTP packetizer）、ADR-0020（webrtc-rs 集成）

## 背景

第七轮我们引入了 webrtc-rs 0.11，完成 Publisher 骨架（MediaEngine + recvonly transceivers
+ ICE gather）。但 Subscriber（WHEP）的 sendonly track 路径 + 与 `RtpRoomHub` 的桥接、
PC lifecycle（ICE failed/closed 时的清理）都没有真正打通。本轮的 A 目标是补完闭环并
固化 lifecycle 协议，让 webrtc-rs 引擎可作为 yunmao 生产 SFU 入口的候选实现。

## 决策

### 1. Subscriber 数据通路

```
RTMP/WHIP → media-edge → RtpRoomHub.push_video_*/push_audio_aac
                                ↓
              hub.subscribe(room) → mpsc::Receiver<RtpPacket>
                                ↓
              spawn_forward_task(cancel, sub, video_track, audio_track)
                                ↓
        TrackLocalStaticRTP::write_rtp(webrtc::rtp::packet::Packet)
                                ↓
              webrtc-rs PC（DTLS/SRTP 加密 + ICE relay）
                                ↓
                            浏览器/iOS/Android WHEP 客户端
```

- `RtpPacket → webrtc::rtp::packet::Packet`：版本/PT/marker/seq/ts/ssrc 一一映射；
  payload 走 `Bytes` 零拷贝。Convert 函数 [`convert_to_webrtc`] 在 `webrtc_rs.rs`。
- Subscriber 端 `add_track` 注册 sendonly H.264(PT=102) + Opus(PT=111) 后，PC 完成
  ICE/DTLS/SRTP 协商；之后 forward task 通过 `track.write_rtp()` 把每个包 push 给浏览器。

### 2. Lifecycle 管理

- 每个会话持 `CancellationToken`；以下三处任意一处触发 cancel 都会停止 forward task：
  1. `delete_session(sid)` 主动调用（调用方关闭 PC）；
  2. `on_ice_connection_state_change` 进入 `Failed | Disconnected | Closed`；
  3. forward task 内部 `track.write_rtp(...)` 错误且包含 `closed/ErrClosedPipe`。
- `WebRtcRsSignaling.sessions` 维护活跃句柄（`SessionHandle { id, role, cancel, pc }`），
  `session_count()` 暴露监控；admin /readyz 可暴露。
- PC drop：会话从 vec 移除 → cancel → spawn task 关闭 PC（异步避免阻塞调用方）。

### 3. Feature flag 与默认引擎

- Crate feature：
  - `webrtc-rs`：依赖 `webrtc = "0.11"`（生产开启）；默认 off 保证无 cmake/openssl
    工具链时 workspace 仍可编译；
  - `webrtc-rs-it`：在 `webrtc-rs` 上叠加集成测试模块（`it_tests`），方便在 CI
    上专门跑 webrtc-rs 集成。
- 引擎选择（运行时 env / config）：
  - `webrtc.engine=rs`（推荐）→ 使用 `WebRtcRsSignaling`；
  - `webrtc.engine=native`（fallback）→ 使用第六轮自研 packetizer + stub。
  - 由 service main 注入 `Arc<dyn Signaling>`，wire 给 `whip_whep::router(sig, cfg)`。

### 4. 与自研 packetizer 共存

- 自研 [`crate::rtp`] 仍作为 `RtpRoomHub` 的内部 packetizer（从 RTMP/FLV 到 RTP），
  生产/dev 两条路径产出的 `RtpPacket` 结构一致；`convert_to_webrtc` 仅做封装映射。
- `WhepSink`（自研 RTP 直写 HTTP 路径）保留：用于不支持 WebRTC 的浏览器/调试，
  feature flag 可关；新订阅默认走 webrtc-rs。

### 5. 集成测试策略

- 进程内 loopback：`subscriber_forwards_60_video_rtp_packets_inproc`
  - 不依赖 openssl/cmake；
  - 直接 spawn forward task + 推帧 + 计数；断言 60+ 包；
  - 断言 cancel token 后任务退出。
- 物理 DTLS/SRTP 握手：`dtls_srtp_loopback_physical` 标 `#[ignore]`；CI 上专门 workflow
  `webrtc-it.yml`（Linux runner + apt install openssl/cmake）触发：
  ```bash
  cd rust && cargo test -p yunmao-webrtc --features webrtc-rs-it \
    dtls_srtp_loopback_physical -- --ignored --nocapture
  ```

## 影响

- 客户端：iOS/Android/Web 都可以从 `/whep/{room_id}` 走 webrtc-rs；
- 监控：`webrtc.engine="rs"` label；`forward_task_running` gauge；
- 退出策略：ICE 失败时 forward task 自动退出，hub subscription 的 channel
  在 100ms 内被 dropped（hub 下次 push 时清理）。

## 复现命令

```bash
cd rust
cargo check -p yunmao-webrtc --features webrtc-rs
cargo test  -p yunmao-webrtc --features webrtc-rs-it subscriber_forwards_60
# 物理 DTLS（需 openssl/cmake）：
cargo test  -p yunmao-webrtc --features webrtc-rs-it \
  dtls_srtp_loopback_physical -- --ignored --nocapture
```
