# ADR-0005 Protobuf 工具链

- 状态：Proposed（架构负责人建议）
- 日期：2026-05-24
- 关联：`08-多端工程组织与协作.md`、`proto/README.md`

## 决策

推荐在下一阶段引入 **Buf CLI**，至少启用 `buf lint`、`buf breaking` 和本地/CI codegen 验证；Buf Schema Registry 暂缓，等多仓协作和 SDK 发布边界明确后再上。

## 理由

- 当前 `scripts/gen-proto.sh` 只校验 JSON/YAML 与 `.proto` 文件存在，不能阻止破坏性字段变更。
- Buf CLI 能在本地和 CI 先建立最低契约纪律，收益大、引入成本低。
- BSR 会带来账号、权限、远端依赖和发布流程治理，早期单仓阶段不必强行绑定。

## 替代方案

- 继续手写结构体：短期最快，但 Go/Rust/前端字段漂移风险会快速累积。
- 直接上 BSR：治理最完整，但流程成本偏高。
- 使用 protoc + 自维护脚本：可行，但 breaking check 和多语言体验不如 Buf 统一。

## 重新评估条件

- `proto/` 拆成独立 contracts 仓，或开始向 Web/Flutter/Go/Rust 发布生成 SDK。
- 多个团队并行修改契约，需要远端可审计版本与依赖管理。
- 外部合作方需要稳定读取 schema 版本。
