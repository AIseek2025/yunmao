# ADR-0002 MVP 直播协议

- 状态：Proposed（架构负责人建议）
- 日期：2026-05-24
- 关联：`02-高清低延迟直播架构.md`、`docs/dev/01-bootstrap-deliverable.md`

## 决策

MVP 自研链路推荐仅承诺 **RTMP 输入 + HTTP-FLV 输出**，并在 media-edge 内预留 LL-HLS 切片器接口；WebRTC 暂缓到强互动低延迟场景被真实验证后再建设。

## 理由

- 当前代码已形成 RTMP publisher、房间 pipe、HTTP-FLV 拉流的最小闭环，继续加固这条链路最符合“先验证底座”的阶段目标。
- HTTP-FLV 能覆盖桌面 Web 与 Android 低延迟演示，LL-HLS 可作为后续兼容 iOS/H5/CDN 的主路径。
- WebRTC 会引入 SFU、NAT、移动端耗电、权限、计费和调度复杂度，过早投入会稀释投喂安全与业务闭环。

## 替代方案

- MVP 完全依赖云直播 HLS/LL-HLS：交付更稳，但不验证 Rust media-edge 能力。
- MVP 直接 WebRTC：延迟最低，但成本和运维复杂度过高。
- 同时支持 SRT/WHIP/RTMP：协议面过宽，不适合当前团队规模。

## 重新评估条件

- 投喂强反馈房间明确要求端到端 < 1 秒，HTTP-FLV/LL-HLS 达不到体验目标。
- 首批硬件只支持非 RTMP 推流，或云厂商低延迟 SDK 已成为确定路线。
- iOS/H5 播放兼容性验证表明 HTTP-FLV 无法作为演示路径。
