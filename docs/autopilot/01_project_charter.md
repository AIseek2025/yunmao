# yunmao Project Charter

## Project Purpose

- `yunmao` 是“真实猫咪直播 + 远程投喂 + 社交陪伴 + 公益救助”的多端平台。
- 当前仓库已经完成 Rust 数据面、Go 控制面、Web/Admin/iOS/Android 基础工程与多轮交付，但仍处在“准生产工程化完成、真实联调与发布门禁未收口”的阶段。
- 本无人值守开发流的目标不是重做已有底座，而是沿着现有交付继续收尾未完成的真实联调、发布硬化、客户端/API 契约统一和后台产品化工作。

## Truth Sources

按优先级使用以下真相源：

1. `README.md`
2. `docs/dev/08-eighth-iteration-deliverable.md`
3. `docs/dev/07-seventh-iteration-deliverable.md`
4. `docs/finalproductplanning/05-开发Phase里程碑与验收.md`
5. `docs/finalproductplanning/09-测试质量与发布工程.md`
6. `docs/finalproductplanning/07-决策记录与待确认问题.md`

说明：

- 用户消息中提到 `docs/finalproduct`，但工作区内实际存在的权威目录是 `docs/finalproductplanning/`。
- 用户消息中称最新进度报告为 `07-seventh-iteration-deliverable.md`，但磁盘上实际还有更晚的 `08-eighth-iteration-deliverable.md`；后续开发必须以磁盘上的更新状态为准，不能回退到旧结论。

## Current Delivery Snapshot

- 已完成：
  - Rust/Go 核心服务、投喂状态机、MQTT/Kafka/Redis/Postgres、可观测性、支付/审核/TURN/WebRTC 基础能力。
  - Web/Admin/iOS/Android 正式工程骨架。
  - 第八轮已完成 webrtc-rs subscriber、支付真路径、审核真路径、词表热更新、TURN 校验脚本、多端正式工程初始化。
- 未完成：
  - `webrtc-it.yml` 与 TURN e2e 真实跑绿。
  - Android `assembleDebug` CI 接通。
  - admin 登录鉴权与真实 `/admin/rooms`、`/admin/wallet` 页面。
  - OpenAPI/共享 schema 输出与客户端 DTO 自动生成。
  - iOS/Android 真机 WHEP 回放和移动端 e2e。
  - 真实支付/审核/TURN 生产凭据切换仍依赖外部信息。

## Current Execution Principle

- 第一轮启动必须选择“仓库内部可独立推进”的工作，不依赖外部商户证书、Apple/微信/支付宝正式凭据、真实 TURN 公网 IP。
- 优先处理能直接降低后续多端并行开发成本的事项：
  - API/DTO 契约统一
  - CI 与联调门禁
  - 后台真实页面与鉴权收口
- 不允许把未跑通的真实 sandbox/prod 联调写成已完成事实。

## Not In Scope For Kickoff

- 不在首轮就同时推进支付真商户联调、阿里云正式 AK/SK 切换、真实公网 TURN 部署、真机农场大规模 E2E。
- 不重写已有架构，不回退 Web/Admin/iOS/Android 现有骨架技术选型。
- 不把早期灵感文档 `docs/earlyproductplanning/` 当作当前执行依据。
