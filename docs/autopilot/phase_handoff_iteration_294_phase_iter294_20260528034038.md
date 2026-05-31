# Phase Handoff Iteration 294

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = phase_promoted`

## Read First Paths

1. `reports/work_report_iteration_293.md`
2. `reports/audit_report_iteration_293.md`
3. `reports/codemaster/project_owner_read_pack.md`
4. `docs/autopilot/02_phase_plan.md`
5. `docs/autopilot/03_audit_checklist.md`
6. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Promotion Basis

- `iteration_293` 已审计通过，可进入 `Phase 5`
- 审计来源：`reports/audit_report_iteration_293.md`

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 本轮必须优先参考负责人目标/阻塞/收口建议，避免再次只读空转

## Target Phase

- `phase_number = 5`
- `phase_title = Production Deployment Assets And Env Templates`

## Target Phase Definition

## Phase 5: Production Deployment Assets And Env Templates

目标：

- 先补齐“能部署”的工程底座，让项目从“开发可跑”进入“预发/生产可装配”。

建议范围：

1. 为后端、Web、Admin 明确正式部署资产，而不再只依赖开发 compose。
2. 提供统一环境模板、变量说明与 secrets 注入边界。
3. 纠正部署文档、`Makefile` 与应用 compose 的不一致项。
4. 为前端正式部署模式补齐最小资产或明确 runbook。

退出条件：

- 至少形成一套面向 staging 的正式部署/装配资产。
- 至少形成一套统一环境模板或等价配置说明，覆盖根级、Web/Admin、关键后端与外部依赖。
- 部署文档与运行配置的关键端口、入口与命令保持一致。

## Implementation Entry

- `deploy_root = deploy/`
- `deploy_root_exists = yes`
- `workflow_root = .github/workflows/`
- `workflow_root_exists = yes`
- `target_admin_root = clients/admin/`
- `target_admin_root_exists = yes`
- `target_web_root = clients/web/`
- `target_web_root_exists = yes`
- 当前目标是补齐 staging/prod 级部署资产、环境模板与 secrets 装配边界：优先在 `deploy/`、`.github/workflows/`、`Makefile` 与 `*.env.example` 上推进，不要回退到产品功能泛读。
- `required_first_write_paths = `deploy/README.md`, `deploy/docker-compose.app.yml`, `Makefile`, `.env.example`, `clients/admin/.env.example`, `clients/web/.env.example`, `docs/dev/runbooks/staging-deploy.md`, `.github/workflows/release-staging.yml``
- `allowed_reference_paths = deploy, .github/workflows, clients/admin, clients/web, go/services, docs/finalproductplanning, reports/project_assessment_20260528`
- 若无法立即推进部署资产或环境模板，必须直接写回 `reports/work_report_iteration_294.md` 记录阻塞，不要继续虚构 `repo/apps/app` 或无关 Rust 骨架

## Rule

- 必须严格按目标 phase 范围推进，不允许回退到已关闭问题上重复工作
- 必须优先在当前 phase 指定的 `clients/`、`go/`、`rust/`、`deploy/`、`scripts/`、`.github/workflows/` 与 `docs/` 路径推进实现，不允许回退到不存在的 `repo/...` 模板。
- 路径基准必须固定为 workspace 根目录：源码位于 `clients/`、`go/`、`proto/` 等目录，规则/报告/交接文档位于 workspace 根目录，禁止虚构 `repo/...` 前缀。
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须运行最小相关测试并写回新的 work report

## Required Write-Back

- `reports/work_report_iteration_294.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。
