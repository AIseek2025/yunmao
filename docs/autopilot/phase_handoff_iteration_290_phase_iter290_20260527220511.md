# Phase Handoff Iteration 290

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = phase_promoted`

## Read First Paths

1. `reports/work_report_iteration_289.md`
2. `reports/audit_report_iteration_289.md`
3. `reports/codemaster/project_owner_read_pack.md`
4. `docs/autopilot/02_phase_plan.md`
5. `docs/autopilot/03_audit_checklist.md`
6. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Promotion Basis

- `iteration_289` 已审计通过，可进入 `Phase 3`
- 审计来源：`reports/audit_report_iteration_289.md`

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 本轮必须优先参考负责人目标/阻塞/收口建议，避免再次只读空转

## Target Phase

- `phase_number = 3`
- `phase_title = Cross-Client E2E And Media Validation`

## Target Phase Definition

## Phase C: Cross-Client E2E And Media Validation

目标：

- 补齐 Web/iOS/Android 的关键冒烟与媒体联调证据。

建议范围：

1. Web 与 Admin 关键路径 E2E。
2. iOS/Android 通过 mock backend 或 wiremock 跑关键烟囱。
3. WHEP/媒体播放的真实链路联调报告。

退出条件：

- 至少形成一条跨端核心链路的可复现自动化或报告。

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
- 当前 app 骨架现状已经明确：已存在 `repo/apps/app/`；禁止再次读取 `clients/admin`, `clients/admin/src` 做状态确认，下一步只能直接补齐 `clients/admin/Cargo.toml`, `clients/admin/src/lib.rs`, `clients/admin/src/main.rs`、更新 `go/go.work` 纳入 `apps/app`，或写回 `reports/work_report_iteration_290.md`
- 若无法继续推进实现，必须直接写回 `reports/work_report_iteration_290.md` 记录阻塞，而不是继续 Todo/目录探测

## Rule

- 必须严格按目标 phase 范围推进，不允许回退到已关闭问题上重复工作
- 必须优先在 Rust 主链推进核心实现，Go 仅做配合层
- 路径基准必须固定为 workspace 根目录：源码位于 `repo/`，规则/报告/交接文档位于 workspace 根目录，禁止把 `rules/`、`reports/`、`docs/autopilot/` 误读成 `repo/...`
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须运行最小相关测试并写回新的 work report

## Required Write-Back

- `reports/work_report_iteration_290.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。
