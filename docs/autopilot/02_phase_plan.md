# yunmao Autopilot Phase Plan

## Overall Goal

在保留既有交付与审计结论的前提下，把 `yunmao` 从“当前 autopilot A-D 主线已完成、但仍不具备生产上线条件”的状态，推进到“上线工程化补齐 + 预发验证闭环 + 05 路线图继续推进”的状态。

说明：

- `Phase A` 到 `Phase D` 已作为上一轮 autopilot 主线完成并收口。
- 本文档当前新增的 `Phase 5` 到 `Phase 10`，用于承接 `reports/project_assessment_20260528/` 中确认的上线阻塞项与 `docs/finalproductplanning/05-开发Phase里程碑与验收.md` 中未完成的产品路线图。
- 后续无人值守开发必须以 workspace 根目录为准，真实代码位于 `clients/`、`go/`、`rust/`、`deploy/`、`.github/workflows/` 与 `docs/`，禁止回退到不存在的 `repo/apps/app` 骨架模板。

## Phase A: Contract And CI Hardening

状态说明：

- 已完成，保留作为历史闭环 phase，不再作为当前 kickoff 目标。

目标：

- 收敛客户端与服务端的共享契约，降低 Web/Admin/iOS/Android 并行演进时的 DTO 漂移风险。
- 补齐当前最关键的 CI 与联调门禁，让后续多端工作有可复现的质量基线。

建议范围：

1. 在仓库中形成可生成/可校验的 OpenAPI 或共享 schema 输出。
2. 为 Web/Admin/移动端建立基于共享契约的类型/DTO 生成或同步机制。
3. 接通 Android CI 构建链路。
4. 让 `webrtc-it.yml`、TURN 脚本或相关验证更接近可重复绿态。

退出条件：

- 至少一条共享契约主链落盘并被客户端消费。
- 至少一项当前“只能手工说明”的 CI 缺口变成仓库内可跑的自动化门禁。

## Phase B: Admin Productization

状态说明：

- 已完成，保留作为历史闭环 phase，不再作为当前 kickoff 目标。

目标：

- 收口后台真实可用性，而不是继续停留在占位页面。

建议范围：

1. Admin 登录/鉴权与 `user-svc` 角色体系打通。
2. `/admin/rooms` 与 `/admin/wallet` 接入真实 API。
3. 后台页面与 feature flags、喂食规则、词表治理形成一致的权限与数据流。

退出条件：

- Admin 至少具备真实登录与两个真实业务页面。

## Phase C: Cross-Client E2E And Media Validation

状态说明：

- 已完成，保留作为历史闭环 phase，不再作为当前 kickoff 目标。

目标：

- 补齐 Web/iOS/Android 的关键冒烟与媒体联调证据。

建议范围：

1. Web 与 Admin 关键路径 E2E。
2. iOS/Android 通过 mock backend 或 wiremock 跑关键烟囱。
3. WHEP/媒体播放的真实链路联调报告。

退出条件：

- 至少形成一条跨端核心链路的可复现自动化或报告。

## Phase D: External Credential Cutover Readiness

状态说明：

- 已完成，保留作为历史闭环 phase，不再作为当前 kickoff 目标。

目标：

- 为支付、审核、TURN 的真实环境切换做仓库内准备，但不在没有凭据时伪造完成。

建议范围：

1. 明确生产/沙箱配置装配方式。
2. 补齐缺少的 runbook、凭据注入说明、回滚路径。
3. 为真实联调预置 smoke checklist 与观测点。

退出条件：

- 外部依赖切换所需的仓库内改造与文档准备完成。

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

## Phase 6: Staging Parity Smoke And External Integration

目标：

- 再补齐“部署后真的成立”的证据，把 handler 级/readiness 级证明推进到进程级与环境级验证。

建议范围：

1. 建立 staging 或 staging-like 同构启动路径。
2. 为 healthz / readyz / `/internal/diagnose/credentials` / 关键 API / Web/Admin 访问形成进程级 smoke。
3. 为 TURN、支付、Apple IAP 等外部依赖补齐受控联调 checklist、脚本与证据沉淀入口。
4. 把失败回滚动作纳入 smoke 或 runbook，而不是只停留在文档描述。

退出条件：

- 至少一轮同构 staging smoke 可在仓库内复现。
- 至少一条关键外部依赖闭环形成进程级验证或受控联调证据。
- 所有未完成的外部联调项都被显式标注为 blocker，而不是口头带过。

## Phase 7: Closeout Fresh Evidence And Release Readiness

目标：

- 不继续泛化扩功能，而是围绕 Phase 6 后仍未闭环的外部输入、TURN、Rust data-plane 与 JWT/JWKS 问题，形成一轮可验证、可审计、可决定去留的 fresh evidence closeout。

建议范围：

1. 先补 Payments / IAP 的真实 round2 after 状态与 diagnostics evidence，明确 WeChat Pay、Alipay、Apple IAP 仍是 `external_wait` 还是已具备真实测试条件。
2. 补 TURN / RTC 的真实 `/v1/rooms/{id}/ice-servers` 结果、credential mode 与 failure mode，不能再只停留在 STUN-only 的旧证据复述。
3. 补 Rust data-plane 与 JWT/JWKS 的 round2 runtime evidence，形成新的 cross-service smoke、runtime matrix 与 alignment 结论。
4. 在三路技术轨道结果齐备后，收敛 owner closeout write-back、decision、blocker closure matrix 与 next action recommendation，并据此判断是否进入 governance refresh。

退出条件：

- Payments / IAP、TURN / RTC、Rust / JWT 三路都已形成 round2 主 write-back 与 leaf evidence，且不再保留模板占位符。
- owner closeout 已能基于 round2 evidence 给出 `continue_closeout | open_new_phase | archive_current_phase` 之一的唯一结论。
- governance refresh 只在 round2 fresh evidence 已改变 blocker 或 owner 结论时才执行；如果只会重复 `waiting_for_fix`，则本 phase 不应以 refresh 完成来冒充收口。

## Phase 8: Phase 1 MVP Recovery

目标：

- 回到 `05-开发Phase里程碑与验收.md` 的产品主线，优先补齐 Web + App MVP 内测缺口。

建议范围：

1. 收口 `clients/web`、`clients/android`、`clients/ios` 的 MVP 核心链路和通知/深链/状态一致性缺口。
2. 为 Web/App 的登录、房间、投喂、记录、个人中心形成统一验收路径。
3. 把真实房间连续运行、跨端一致性、弱网/前后台恢复等验证入口制度化。

退出条件：

- 至少一条 Web/App MVP 关键链路具备跨端一致性证据。
- 至少形成一套可重复执行的 MVP 验收入口或 runbook。

## Phase 9: Phase 2-3 Productization Recovery

目标：

- 继续补齐增长、运营、商业化与机构接入的核心缺口，而不是只停留在“支付准备度”。

建议范围：

1. 推进增长玩法、分享归因、排行榜、通知频控与运营后台闭环。
2. 推进支付回调、订单/退款/对账、权益一致性、设备健康与基础风控。
3. 为机构入驻、公益记录、财务导出等能力补齐最小可用路径或显式 blocker。

退出条件：

- 至少一条增长/运营能力和一条商业化/权益能力具备真实代码闭环。
- 至少一条支付或对账链路具备真实受控验证证据。

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

## Phase 11 Candidate: Phase 4 Platformization And Cross-Region Readiness

状态说明：

- 当前仅为 `draft / not materialized` 候选阶段。
- 该阶段只在负责人明确从 `continue_closeout` 改判为 `open_new_phase` 后，才允许升级成正式 `Phase 11`。
- 关联治理输入：
  - `docs/autopilot/08_phase10_completion_and_closeout_decision_2026-05-31.md`
  - `docs/autopilot/09_phase11_candidate_plan_2026-05-31.md`
  - `docs/autopilot/10_phase11_handoff_draft_2026-05-31.md`
  - `docs/autopilot/11_phase10_owner_continue_closeout_writeback_2026-05-31.md`

目标：

- 若继续沿 `Phase 4` 产品主线推进，把剩余高阶目标收敛成一个明确的新阶段，而不是重复 `Phase 10` 的最小闭环。

建议范围：

1. 小程序与多端统一能力：统一账号、房间、投喂、支付、通知与设备 API 的差距梳理与最小闭环。
2. App 深度能力：播放 SDK、推送、画中画/后台策略、离线草稿与本地缓存中的至少一条真实增强链路。
3. Rust 试点收益放大：`gateway / media-edge / device-edge` 至少一条收益形成 before/after 对比。
4. Cross-region / scale readiness：至少一项跨 region、故障演练或容量演练形成新的执行证据。

退出条件：

- 至少一条小程序或跨端统一能力具备真实实现或受控 blocker 结论。
- 至少一条 App 深度能力具备真实代码与验证证据。
- 至少一条 Rust 试点收益具备可比较的 before/after 证据。
- 至少一项跨区域或规模化 readiness 演练形成可审计证据。

禁止误用：

- 禁止把本候选节当成已经批准的新 live phase。
- 禁止在没有新的 owner 决策、正式 handoff 与 read-pack/brief 同步前，依据本节直接开始编码或 repair。

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
- `Phase 11 Candidate` 目前只作为候选治理输入存在，不改变当前 `Phase 10` 已完成后的 `continue_closeout` 默认语义。

## Execution Strategy

- 优先补齐 Rust 数据面容量验证：`rust/crates/yunmao-media-edge/` 容量基准测试、`rust/crates/yunmao-gateway/` WebSocket 基线复跑脚本与报告。
- 补齐规模化材料：`docs/dev/runbooks/scale-drill.md` 故障演练 runbook、`scripts/perf/` 基线执行脚本与 `deploy/observability/grafana/dashboards/` Prometheus 看板。
- 为小程序预研或多端增强能力补齐一条最小落地：`clients/` 下的具体增强功能。
- 每轮必须把新增事实、未解决 blocker、验证命令和下一轮建议写回 `reports/work_report_iteration_<n>.md`。
- 证据必须包含原始日志（test output + exit code），不允许仅凭"文件存在性+SHA256"声明功能闭环。
