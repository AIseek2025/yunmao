# Phase Handoff Iteration 292

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = phase_promoted`

## Read First Paths

1. `reports/work_report_iteration_291.md`
2. `reports/audit_report_iteration_291.md`
3. `reports/codemaster/project_owner_read_pack.md`
4. `docs/autopilot/02_phase_plan.md`
5. `docs/autopilot/03_audit_checklist.md`
6. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Promotion Basis

- `iteration_291` 已审计通过，可进入 `Phase 4`
- 审计来源：`reports/audit_report_iteration_291.md`

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 本轮必须优先参考负责人目标/阻塞/收口建议，避免再次只读空转

## Target Phase

- `phase_number = 4`
- `phase_title = External Credential Cutover Readiness`

## Target Phase Definition

## Phase D: External Credential Cutover Readiness

目标：

- 为支付、审核、TURN 的真实环境切换做仓库内准备，但不在没有凭据时伪造完成。

建议范围：

1. 明确生产/沙箱配置装配方式。
2. 补齐缺少的 runbook、凭据注入说明、回滚路径。
3. 为真实联调预置 smoke checklist 与观测点。

退出条件：

- 外部依赖切换所需的仓库内改造与文档准备完成。

## Kickoff Target

首轮 kickoff 目标锁定为：

**Phase A: Contract And CI Hardening**

原因：

- 它完全可以在仓库内部推进，不依赖外部业务凭据。
- 它能同时为 Web/Admin/iOS/Android 后续开发降风险。
- 它比直接做支付/审核/TURN 真值联调更适合无人值守自动收口。

## Kickoff Strategy

- 第一轮只做最小但真实的推进，不要一次覆盖 Phase A 的全部子项。
- 优先从以下切口二选一：
  1. 共享契约主链：`pkg/yunmao/openapi/v3.json` 或等价共享 schema 输出 + 至少一个客户端消费链路。
  2. Android / 媒体 CI 门禁：把当前“文档说明未跑”的一条链路接成仓库自动化。
- 每轮必须把新增事实、未解决 blocker、验证命令和下一轮建议写回 `reports/work_report_iteration_<n>.md`。

## Implementation Entry

- `target_app_root = clients/admin/`
- `target_app_root_exists = yes`
- `web_reference_root = clients/web/`
- `web_reference_root_exists = yes`
- 优先在 `repo/apps/app/` 内推进实现；只有遇到契约或样式复用问题时，才按需参考 `repo/apps/web/`
- `preferred_write_paths = `clients/admin/Cargo.toml`, `clients/admin/src/lib.rs`, `clients/admin/src/main.rs``
- `allowed_reference_path = go/go.work`
- `missing_skeleton_paths = `clients/admin/Cargo.toml`, `clients/admin/src/lib.rs`, `clients/admin/src/main.rs``
- `workspace_member_apps_app = missing`
- `forbidden_repeat_reads = `clients/admin`, `clients/admin/src``
- 当前 app 骨架现状已经明确：已存在 `repo/apps/app/`；禁止再次读取 `clients/admin`, `clients/admin/src` 做状态确认，下一步只能直接补齐 `clients/admin/Cargo.toml`, `clients/admin/src/lib.rs`, `clients/admin/src/main.rs`、更新 `go/go.work` 纳入 `apps/app`，或写回 `reports/work_report_iteration_292.md`
- 若无法继续推进实现，必须直接写回 `reports/work_report_iteration_292.md` 记录阻塞，而不是继续 Todo/目录探测

## Rule

- 必须严格按目标 phase 范围推进，不允许回退到已关闭问题上重复工作
- 必须优先在 Rust 主链推进核心实现，Go 仅做配合层
- 路径基准必须固定为 workspace 根目录：源码位于 `repo/`，规则/报告/交接文档位于 workspace 根目录，禁止把 `rules/`、`reports/`、`docs/autopilot/` 误读成 `repo/...`
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须运行最小相关测试并写回新的 work report

## Required Write-Back

- `reports/work_report_iteration_292.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。
