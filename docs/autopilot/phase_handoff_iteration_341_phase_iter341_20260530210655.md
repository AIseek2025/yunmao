# Phase Handoff Iteration 341

## Contract

- `contract_version = relay_contract_v1`
- `next_owner = opencode + GLM-5.1`
- `required_report_kind = work_report`
- `current_status = phase_promoted`

## Read First Paths

1. `reports/work_report_iteration_340.md`
2. `reports/audit_report_iteration_340.md`
3. `reports/codemaster/project_owner_read_pack.md`
4. `docs/autopilot/02_phase_plan.md`
5. `docs/autopilot/03_audit_checklist.md`
6. `docs/autopilot/07_rust_core_go_assist_architecture.md`

## Promotion Basis

- `iteration_340` 已审计通过，可进入 `Phase 10`
- 审计来源：`reports/audit_report_iteration_340.md`

## Owner Context

- 发车前已刷新负责人读包：`reports/codemaster/project_owner_read_pack.md`
- 本轮必须优先参考负责人目标/阻塞/收口建议，避免再次只读空转


负责人接管协议：
- `owner_action = request_planning`
- `takeover_mode = owner_replan`
- `owner_judgment = 负责人判断当前阻塞已转成规划问题，先请求 GLM-4.5 负责人更新规划，再继续编码。 当前规划缺口: 当前已定义 phases 已全部完成；继续推进前需先做归档或下一 phase 规划。`
- `restart_strategy = pause_relaunch_until_replan_is_materialized`
- 必做下一步: 先按负责人判断执行 `request_planning`，推荐入口 `chat`。
- 必做下一步: 先读 `project_owner_read_pack` 与 handoff，再进入写入、验证或恢复动作。
- 必做下一步: 优先解决 `当前已定义 phases 已全部完成；继续推进前需先做归档或下一 phase 规划。` 并把结论写回新的报告工件。
- 禁止动作: 禁止跳过负责人必读包后直接盲目编码或重复全量读仓库。
- 禁止动作: 禁止在同一问题已进入 relaunch storm / repeated repair 后继续沿用旧 prompt 空转。
- 禁止动作: 禁止在 owner_replan / phase_alignment / request_audit 场景下继续把问题伪装成“还能直接开发”。
- 退出条件: 新的 planning / phase alignment / audit 结论已落盘，并且自动流下一步入口与负责人判断一致。
- 退出条件: supervisor / loop / pending session 不再处于 stale、storm 或 provider blocked 的异常回路。
- 退出条件: 负责人判断、read pack、handoff 与 recovery state 口径一致，不再互相冲突。

## Target Phase

- `phase_number = 10`
- `phase_title = Phase 4 Multi-Platform And Scale Recovery`

## Target Phase Definition

## Phase 10: Phase 4 Multi-Platform And Scale Recovery

目标：

- 补 Phase 4 路线中的多端增强、Rust 数据面收益验证与规模化材料，不再把方向性基础误判为完成。

建议范围：

1. 推进小程序预研入口、App 深度能力、缓存/推送/后台策略或设备管理中的最小一条真实链路。
2. 为 Rust 媒体/实时网关补齐收益验证、容量演练、QoE 或跨 region 材料。
3. 为故障演练、灰度门禁、多区域部署和容量规划建立可持续归档路径。

退出条件：

- 至少一条多端增强能力有真实落地。
- 至少一条 Rust 数据面收益或容量验证证据形成闭环。
- 至少一项故障演练或规模化材料被纳入阶段性证据包。

## Phase Completion History

以下 phases 已在后续迭代中依次完成并审计放行：

| Phase | Title | Completed | Evidence |
|-------|-------|-----------|----------|
| 5 | Production Deployment Assets And Env Templates | ✅ | iterations 320-325 |
| 6 | Staging Parity Smoke And External Integration | ✅ | iterations 326-330 |
| 7 | Closeout Fresh Evidence And Release Readiness | ✅ | iterations 331-336 |
| 8 | Phase 1 MVP Recovery | ✅ | iterations 337-338 |
| 9 | Phase 2-3 Productization Recovery | ✅ | iterations 339-340 |

## Current Target

当前活跃目标锁定为：

**Phase 10: Phase 4 Multi-Platform And Scale Recovery**

原因：

- Phase 5-9 已依次完成，deployment assets、staging smoke、fresh evidence closeout、MVP recovery 与 productization recovery 均已闭环。
- Phase 10 聚焦多端增强、Rust 数据面收益验证与规模化材料：QoE/容量验证、故障演练、灰度门禁与多区域部署。
- 退出条件：至少一条多端增强能力有真实落地；至少一条 Rust 数据面收益或容量验证证据形成闭环；至少一项故障演练或规模化材料被纳入阶段性证据包。

## Execution Strategy

- 优先补齐 Rust 数据面容量验证：`rust/crates/yunmao-media-edge/` 容量基准测试、`rust/crates/yunmao-gateway/` WebSocket 基线复跑脚本与报告。
- 补齐规模化材料：`docs/dev/runbooks/scale-drill.md` 故障演练 runbook、`scripts/perf/` 基线执行脚本与 `deploy/observability/grafana/dashboards/` Prometheus 看板。
- 为小程序预研或多端增强能力补齐一条最小落地：`clients/` 下的具体增强功能。
- 每轮必须把新增事实、未解决 blocker、验证命令和下一轮建议写回 `reports/work_report_iteration_<n>.md`。
- 证据必须包含原始日志（test output + exit code），不允许仅凭"文件存在性+SHA256"声明功能闭环。

## Implementation Entry

- `rust_root = rust/`
- `rust_root_exists = yes`
- `perf_root = scripts/perf/`
- `observability_root = deploy/observability/`
- 当前目标是补齐多端增强与规模化材料：优先推进 Rust 数据面收益验证、QoE/容量材料、观测看板与演练 runbook，不要把已有底座误判为 Phase 4 已完成。
- `required_first_write_paths = `rust/crates/yunmao-gateway/src/server.rs`, `rust/crates/yunmao-media-edge/src/qoe.rs`, `rust/crates/yunmao-media-edge/src/metrics.rs`, `scripts/perf/ws-baseline-run.sh`, `scripts/perf/ws-baseline-report.md`, `deploy/observability/grafana/dashboards/ll-hls-qoe.json`, `deploy/observability/grafana/dashboards/yunmao-overview.json`, `docs/dev/runbooks/scale-drill.md``
- `allowed_reference_paths = rust, scripts/perf, deploy/observability, docs/finalproductplanning, reports/project_assessment_20260528, clients/web, clients/android, clients/ios`
- 若无法立即推进规模化或多端增强主链路，必须直接写回 `reports/work_report_iteration_341.md` 记录阻塞，不要继续虚构 `repo/apps/app`

## Rule

- 必须严格按目标 phase 范围推进，不允许回退到已关闭问题上重复工作
- 必须优先在当前 phase 指定的 `clients/`、`go/`、`rust/`、`deploy/`、`scripts/`、`.github/workflows/` 与 `docs/` 路径推进实现，不允许回退到不存在的 `repo/...` 模板。
- 路径基准必须固定为 workspace 根目录：源码位于 `clients/`、`go/`、`proto/` 等目录，规则/报告/交接文档位于 workspace 根目录，禁止虚构 `repo/...` 前缀。
- 必须在当前主会话内顺序完成分析、修改、测试与 write-back，不允许拆成多个并发会话
- 禁止使用 `task` 工具、禁止启动 Explore Agent 或任何子代理
- 禁止并行探索、并发调查或额外派生 session，避免触发 opencode 的 SQLite / snapshot 锁冲突
- 必须运行最小相关测试并写回新的 work report

## Required Write-Back

- `reports/work_report_iteration_341.md`
- 注意：Required Write-Back 中的目标 `work_report` 是本轮输出路径；如果该文件尚未落盘，禁止把它当作 Read First 输入读取。
