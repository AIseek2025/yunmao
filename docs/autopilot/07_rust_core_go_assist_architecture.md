# yunmao Rust Core / Go Assist Architecture

## Boundary Summary

- `rust/` 负责媒体与实时数据面：
  - ingest / media-edge / gateway / device-edge
  - WebRTC / WHIP / WHEP / RTP / 低延迟播放相关能力
- `go/` 负责控制面与业务域：
  - user / room / feeding / device / billing / admin / chat
  - 支付、审核、词表、设备状态、房间治理、后台 API
- `clients/` 负责多端消费层：
  - `web`
  - `admin`
  - `ios`
  - `android`

## Current Development Bias

- 第七至第八轮已经把“底座能力 + 多端骨架”基本落齐。
- 当前最大风险不在是否继续新增服务，而在：
  - 多端与服务端契约漂移
  - CI / E2E / 媒体联调门禁不足
  - 后台页面仍有占位页
  - 外部凭据未到位时容易出现“仓库内准备不足”

## Safe First Moves

- 优先推进不会打破边界、但能降低后续并行成本的事项：
  - 共享 schema / OpenAPI 输出
  - CI workflow 与验证脚本
  - Admin 鉴权与真实页面
- 若涉及媒体联调：
  - 优先改 `rust/`、`.github/workflows/`、`scripts/turn/`、`reports/turn/`
  - 不要在没有环境支撑时伪造本机真值结论
- 若涉及客户端契约统一：
  - 优先改共享 schema 输出与生成脚本
  - 再改 `clients/web`、`clients/admin`、`clients/ios`、`clients/android` 的消费代码

## Do Not Do In Kickoff

- 不在首轮同时横跨 Rust 媒体、Go 业务、四端 UI 和外部支付凭据切换。
- 不因为早期规划写过 Flutter 或其他技术栈，就回退当前已经落地的 Swift/Kotlin/Next.js 现实。
- 不把外部依赖未到位的问题错误归因成仓库架构必须重做。
