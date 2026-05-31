# ADR-0020 webrtc-rs 集成架构与自研 RTP packetizer 共存策略

- 状态：Accepted (第七轮)
- 上下文：第六轮 yunmao-webrtc 落地了自研 RTP packetizer（H.264 STAP-A/FU-A、AAC MPEG4-GENERIC、
  Opus）+ `RtpRoomHub` SFU + WHIP/WHEP 路由 + LocalSignaling stub，但 SDP answer 仍
  没有 DTLS/SRTP/ICE 协商；不能在生产中直接对接真实浏览器/移动客户端。本轮要求
  引入 [`webrtc-rs`](https://github.com/webrtc-rs/webrtc) 完整栈作为生产路径。

## 决策

### 1. 双轨保留：自研 packetizer 与 webrtc-rs 共存

- **自研 RTP packetizer（`crate::rtp`）**保留为 **fallback / 测试通道**：
  - LL-HLS 路径仍用它做 NALU → Annex-B → mp4 fragment 的快速转换，
    避免与 webrtc-rs 的 H.264 编解码器抽象耦合。
  - 单测在不开启 `webrtc-rs` feature 时仍可跑通（CI 默认配置）。
- **webrtc-rs（`webrtc` crate v0.11+）**：默认 production 路径。
  - 完整 ICE / DTLS / SRTP 协议栈；
  - `MediaEngine` 注册 H.264(PT=102, profile-level-id=42e01f) + Opus(PT=111)；
  - `PeerConnection.on_track` 收到 RTP 包后直接交给 `RtpRoomHub`（SFU），
    不再走 packetizer。

### 2. Feature flag 隔离

`rust/crates/yunmao-webrtc/Cargo.toml`：

```toml
[features]
webrtc-rs    = ["dep:webrtc"]     # 编译时启用真实栈
webrtc-rs-it = ["webrtc-rs"]      # 启用集成测试（需要系统依赖）
```

理由：
- `webrtc` crate 间接依赖 `cmake / clang / openssl`，CI 镜像默认不具备，
  feature off 时只是 `cargo build` 通过即可。
- 集成测试需要真实证书生成（rcgen）+ DTLS 握手 + SRTP 解密；
  CPU/内存开销大，不应在普通 PR 流水线跑，故再分一层 `-it`。

### 3. 模块布局

```
rust/crates/yunmao-webrtc/
├── src/
│   ├── lib.rs                # Signaling/Publisher/Subscriber trait + LocalSignaling stub
│   ├── rtp.rs                # 自研 packetizer（fallback / LL-HLS 路径）
│   ├── sfu.rs                # RtpRoomHub
│   ├── whip_whep.rs          # axum 路由
│   └── webrtc_rs.rs          # feature="webrtc-rs"；真实 PC 实现
```

`WebRtcRsSignaling` 实现 `Signaling` trait，与 `LocalSignaling` 二选一注入到
`whip_whep::router`，调用方无感知。

### 4. WHIP 数据流

```
client offer SDP
    ↓
WebRtcRsSignaling.create_publisher
    ↓ APIBuilder + MediaEngine(H.264+Opus)
RTCPeerConnection
    ↓ add_transceiver(recvonly H.264 / Opus)
    ↓ set_remote_description(offer)
    ↓ create_answer + set_local_description + ICE gather
on_track(callback)
    ↓ track.read() → RTP packet
RtpRoomHub.publish_rtp
    ↓
WhepSink (订阅者) + LL-HLS Packager（NALU 路径）
```

### 5. WHEP 数据流

```
client offer SDP
    ↓
WebRtcRsSignaling.create_subscriber
    ↓ add_transceiver(sendonly H.264 / Opus)
TrackLocalStaticRTP（sendonly）
    ↓ 从 RtpRoomHub.subscribe(...)
    ↓ track.write_rtp(...)
client（已 DTLS 握手 + SRTP 加密）
```

### 6. ICE servers

- `crate::IceServers` 字段映射到 `RTCIceServer`：
  - STUN：默认 `stun:stun.l.google.com:19302`；
  - TURN：URL + 短期 username/credential（room-svc `/v1/rooms/{id}/ice-servers`，
    见 ADR-0017）。

### 7. 集成测试（README 复现命令）

```bash
# Linux
sudo apt install -y cmake clang libclang-dev libssl-dev pkg-config

# macOS (Apple Silicon)
brew install cmake openssl@3
export OPENSSL_DIR=$(brew --prefix openssl@3)

# 真值跑：publisher ↔ subscriber 互联
cd rust
cargo test -p yunmao-webrtc --features webrtc-rs-it dtls_srtp_loopback -- --ignored --nocapture
```

集成测试断言：
1. 双向 ICE 候选交换完成；
2. DTLS 握手成功（Fingerprint 验证通过）；
3. SRTP 解密后至少 60 个 RTP packet 从 publisher → SFU → subscriber 回环；
4. RTP 序号连续、时间戳单调。

## 当前实现状态（本轮 PR）

- ✅ Cargo.toml 引入 webrtc 0.11 + feature flag；
- ✅ `webrtc_rs.rs` 模块；`WebRtcRsSignaling::create_publisher` 已落骨架（含
  MediaEngine 注册 + transceiver + offer/answer + ICE gather）；
- ✅ 默认 build 通过；不破坏现有测试；
- ⚠️ `create_subscriber` 留 TODO（OnTrack→SFU 路由 + Sub Track 写回需要
  spawn 长生命周期 task，与现有 `Publisher`/`Subscriber` trait 解耦后再补）；
- ⚠️ 集成测试脚本骨架在 README 中标注，**本机本轮未运行**，原因：
  - macOS Apple Silicon 上 openssl-sys 依赖 cmake；
  - 本机当前 sandbox 不允许装系统库；
  - 等价方案见 README 中的复现脚本，CI 由专门的 `webrtc-it.yml` workflow 在 Linux 大内存 runner 上跑。

## 风险

- webrtc-rs 0.11 是较新版本（最新 0.17），后续会跟 Apple Silicon openssl 兼容问题；
  评估升级到 0.17 时锁定单测矩阵。
- 自研 packetizer 与 webrtc-rs 同时存在会增加一些代码体量；
  通过 feature flag 切换避免运行时分叉。
- SRTP 密钥导出（用于内部观测）当前 webrtc-rs 暴露的 API 较少；
  生产监控用 ICE/DTLS 状态机的回调事件，而非直接观测密钥。

## 后续阶段（下一轮可继续）

- Subscriber 路径完整骨架（包括 NACK / PLI 处理）。
- RTCP feedback：sender/receiver report、PLI、FIR 转发给 publisher。
- Simulcast：多 layer transceiver + Bandwidth Estimation；与灰度策略
  ADR-0023 配合，按订阅端能力选 layer。
