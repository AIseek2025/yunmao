# Phase Handoff Iteration 290

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = phase_promoted`

## Read First Paths

1. `.codemaster_orchestration/opencode/dispatch/opencode_session_response_phase_iter290_20260527220511.json`
2. `.codemaster_orchestration/artifacts/opencode_session_stderr_phase_iter290_20260527220511.log`
3. `reports/codemaster/project_owner_read_pack.md`
4. `reports/work_report_iteration_289.md`
5. `reports/audit_report_iteration_289.md`
6. `docs/autopilot/02_phase_plan.md`
7. `docs/autopilot/03_audit_checklist.md`
8. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Promotion Basis

- `iteration_289` 已审计通过，可进入 `Phase 3`
- 审计来源：`reports/audit_report_iteration_289.md`

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 本轮必须优先参考负责人目标/阻塞/收口建议，避免再次只读空转

## Auto Diagnosis
- `detected_reason = phase_completed_without_new_work_report`
- `previous_trace_id = phase_iter290_20260527220511`
- `previous_response_status = completed`
- `previous_returncode = `
- `previous_response_path = .codemaster_orchestration/opencode/dispatch/opencode_session_response_phase_iter290_20260527220511.json`
- `previous_stderr_path = .codemaster_orchestration/artifacts/opencode_session_stderr_phase_iter290_20260527220511.log`
- `diagnosis_summary = 会话已生成可解析输出。`
- `diagnosis_next_action = 进入正常写回、审计或下一阶段门禁流程。`
- `attempt_count = 1`
- `failed_attempt_count = 0`
- `sustained_relaunch_storm = False`
## Recovery Mandate
- 先核对上一次 phase response/stderr 与现有代码改动，判断为什么反复失败却没有写回 work_report
- 如果上一次实际上已经完成代码与测试，只是漏写 `work_report`，则先补齐报告，再继续当前 phase 收尾
- 所有路径都以 workspace 根目录为基准：源码在 `repo/`，但 `rules/`、`reports/`、`docs/autopilot/` 都在 workspace 根目录下，禁止误写成 `repo/rules/*`、`repo/reports/*`、`repo/docs/*`
- 当前实现入口固定为 `repo/apps/app/`；优先在该目录内继续实现，不要回到 `repo/` 顶部重新摸排
- 若继续实现 App，新增写入必须优先落在 `clients/admin/Cargo.toml`, `clients/admin/src/lib.rs`, `clients/admin/src/main.rs` 或其同目录子文件，禁止回到目录级 `repo`、`repo/apps`、`rules` 做泛读
- 若继续参考现有 workspace 配置，只允许按需读取 `go/go.work`；读完后必须直接进入实现或写回 `reports/work_report_iteration_290.md`
- 当前 `repo/apps/app/` 仍缺少 `clients/admin/Cargo.toml`, `clients/admin/src/lib.rs`, `clients/admin/src/main.rs`；下一步必须优先补齐这些骨架文件
- 当前 `repo/Cargo.toml` 仍未纳入 `apps/app` workspace member；下一步必须补齐该成员声明并再继续实现
- 当前 app 骨架现状已经明确：已存在 `repo/apps/app/`；禁止再次读取 `clients/admin`, `clients/admin/src` 做状态确认；下一步只能直接补齐 `clients/admin/Cargo.toml`, `clients/admin/src/lib.rs`, `clients/admin/src/main.rs`、更新 `go/go.work` 纳入 `apps/app`，或写回 `reports/work_report_iteration_290.md`
- 在重新阅读完关键证据后，下一步动作必须转入实现、测试或写回，不允许再次回到全量诊断循环
- 如果上一次没有形成有效交付，则必须先修复导致 phase 中断的根因，再继续当前 phase
- 若当前仍无法完成交付，也必须先写回 `work_report` 记录阻塞点与已验证证据
- 若无法继续推进实现，禁止只写 Todo 后退出；必须直接写回 `reports/work_report_iteration_290.md` 记录当前阻塞、缺失前提与已验证证据
- 若已出现累计 relaunch storm，不允许继续按同一空转路径重复尝试；必须显式调整排障策略并给出可审计的写回结果

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

- `target_admin_root = clients/admin/`
- `target_admin_root_exists = yes`
- `target_web_root = clients/web/`
- `target_web_root_exists = yes`
- `target_android_root = clients/android/`
- `target_android_root_exists = yes`
- `target_ios_root = clients/ios/`
- `target_ios_root_exists = yes`
- `backend_reference_root = go/services/room-svc/`
- `backend_reference_root_exists = yes`
- 当前目标是完成跨端真实验证，而不是继续伪造 Rust app 骨架：优先补齐 Admin/Web 的 E2E、Android/iOS 的最小 smoke 或媒体链路验证，以及必要的验证证据沉淀。
- `required_first_write_paths = `clients/admin/e2e/admin.spec.ts`, `clients/web/e2e/login.spec.ts`, `clients/android/app/src/test/java/live/yunmao/app/GrayHitTest.kt`, `clients/android/app/src/main/java/live/yunmao/app/webrtc/WhepClient.kt`, `clients/ios/YunmaoApp/Tests/YunmaoAppTests/YunmaoAppTests.swift``
- `allowed_reference_paths = clients/admin, clients/web, clients/android, clients/ios, go/services/room-svc, go/pkg/yunmao/openapi/v3.json`
- 若无法立即推进上述跨端验证路径，必须直接写回 `reports/work_report_iteration_290.md` 记录阻塞，不要继续虚构 `repo/apps/app`、`Cargo.toml` 或 `src/main.rs` 之类不存在的目标

## Rule

- 必须严格按目标 phase 范围推进，不允许回退到已关闭问题上重复工作
- 必须优先在 Rust 主链推进核心实现，Go 仅做配合层
- 路径基准必须固定为 workspace 根目录：源码位于 `clients/`、`go/`、`proto/` 等目录，规则/报告/交接文档位于 workspace 根目录，禁止虚构 `repo/...` 前缀。
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须运行最小相关测试并写回新的 work report

## Required Write-Back

- `reports/work_report_iteration_290.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。
