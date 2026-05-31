# yunmao Owner Planning Handoff

## 目的

本文件用于把 `yunmao` 当前状态交给下一位负责人，避免重新做一遍上下文恢复。

当前建议动作不是继续扩 phase，而是围绕“Phase 6 之后还缺哪些真实外部依赖与跨服务联调证据”做一轮窄范围 planning / closeout。

## 负责人先判断什么

### 已确认的事实

- 当前项目已切到 `planning`，不是继续编码态
- `project_completed_all_defined_phases` 已写入 autopilot state
- `phase6_iter296` 已审计通过，Phase 6 退出条件成立
- 最新 owner brief 已是 `request_planning`
- 最新治理面 `owner_consistency_status = ok`

### 不能误判的事实

- process-level smoke 成功不等于真实外部联调成功
- `waiting_for_fix` 不是回到开发态，而是仍有 follow-up / blocker 未闭环
- 当前 `dispatch_ready = false`，说明仍需先做规划，不应直接盲发新的开发任务

## 负责人必读输入

### 一级输入

- [01-CURRENT-STATUS-SUMMARY.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/01-CURRENT-STATUS-SUMMARY.md)
- [04-NEXT-PHASE-PLAN.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/04-NEXT-PHASE-PLAN.md)
- [project_owner_brief.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/codemaster/project_owner_brief.md)
- [run_summary_yunmao_owner_planning_refresh_20260529.json](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/codemaster/run_summary_yunmao_owner_planning_refresh_20260529.json)
- [owner_escalation_report.json](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/codemaster/owner_escalation_report.json)

### 二级输入

- [integration_readiness.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/codemaster/integration_readiness.md)
- [staging-smoke.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/dev/runbooks/staging-smoke.md)
- [credential-cutover.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/dev/runbooks/credential-cutover.md)
- [docker-compose.staging.yml](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/deploy/docker-compose.staging.yml)

### 底层证据

- [process-level-smoke.log](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/iteration_296_evidence/process-level-smoke.log)
- [credential-check-clean.log](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/iteration_296_evidence/credential-check-clean.log)
- [credential-diagnostics.json](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/iteration_296_evidence/credential-diagnostics.json)
- [e2e-smoke-final.log](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/iteration_296_evidence/e2e-smoke-final.log)

## 推荐规划边界

### 应纳入下一轮规划的事项

- WeChat Pay / Alipay / Apple IAP 沙箱或测试凭据补齐
- TURN shared secret 与真实 TURN infra 补齐
- Rust `gateway/device-edge/media-edge` 相关镜像或二进制重建与 staging 启动验证
- JWT HS256 -> RS256 / JWKS 迁移闭环
- 基于真实外部输入的新一轮 staging / closeout 证据

### 不应纳入下一轮规划的事项

- 与外部联调和发布闭环无关的新功能开发
- UI 或体验层扩面优化
- 与当前 Phase 6 blocker 无关的泛技术债清理
- 重新打开已审计通过的仓库内 process-level smoke 主线

## 建议发单顺序

1. 先发平台 / Infra: 补真实 staging 外部输入与 Rust data-plane 启动条件
2. 再发支付 / 后端: 补支付与 Apple IAP 沙箱资料并复跑 credential diagnostics
3. 再发 RTC / 房间链路: 补 TURN shared secret 与真实 TURN issuance evidence
4. 最后发 owner / QA: 基于新证据做一次 closeout 复核，决定是新 phase 还是归档收口

## 推荐完成定义

### 规划完成不等于真实联调完成

规划阶段的完成定义应是：

- 剩余 blocker 已收敛到最小集合
- 每个 blocker 都有 owner、输入、证据落点和出口标准
- `must_fix` 与 `external_wait` 已明确分界

### 能进入下一轮正式 closeout 判断的最低条件

只有在以下条件同时满足后，才可从 planning 进入下一轮放行判断：

- 支付与商店沙箱资料已到位并形成新的 credential diagnostics evidence
- TURN 不再只是 STUN-only，而是形成真实 time-limited credentials evidence
- Rust data-plane 与跨服务 JWT/JWKS 联调闭环
- 形成一轮新的、可追溯的 staging / closeout 治理刷新结果

## 建议输出物

下一位负责人如果继续推进，建议只输出以下 4 类产物：

1. 一份新的 phase / closeout 规划单
2. 一份真实外部输入补齐结果
3. 一份新的 staging smoke / integration evidence 汇总
4. 一次新的治理刷新与 owner 结论
