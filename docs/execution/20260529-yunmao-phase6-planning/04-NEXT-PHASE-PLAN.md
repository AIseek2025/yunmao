# yunmao Next Phase Plan

## 目标

本规划单只服务于 `phase6_iter296` 之后的最小 closeout / planning，目标不是继续扩功能，而是把剩余外部集成与发布前依赖收敛为可执行、可验证、可审计的最小任务集。

## 规划边界

### 纳入范围

- WeChat Pay / Alipay / Apple IAP 沙箱资料补齐
- TURN shared secret 与真实 TURN infra 补齐
- Rust data-plane 与 staging image / binary 启动闭环
- JWT/JWKS 跨服务联调闭环
- 新一轮 staging / closeout evidence 与治理刷新

### 不纳入范围

- 新功能开发
- 与 blocker 无关的 UI / 体验优化
- 与外部联调无关的泛技术债清理
- 重新打开已审计通过的 Phase 6 process-level smoke 主线

## 当前起点

- 项目状态: `planning`
- 当前周期: `phase6_iter296`
- 最新治理刷新: `yunmao_owner_planning_refresh_20260529`
- 当前正式结论: 既定 phases 已完成，但仍需外部输入驱动的 closeout
- 当前直接 blocker: 支付 / Apple IAP / TURN / Rust data-plane / JWT-JWKS 联调

## 任务分组

### Track A: 支付与商店外部输入补齐

Owner:

- 支付后端
- 业务 owner
- 外部渠道对接人

输入：

- [credential-cutover.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/dev/runbooks/credential-cutover.md)
- [credential-diagnostics.json](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/iteration_296_evidence/credential-diagnostics.json)

完成定义：

- WeChat Pay 沙箱凭据补齐
- Alipay 沙箱凭据补齐
- Apple IAP 测试资料补齐
- 新一轮 diagnostics 结果不再是 `missing/partial` 主导

### Track B: TURN 与 RTC 联调补齐

Owner:

- RTC / 房间链路
- Infra

输入：

- [work_report_iteration_296.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/work_report_iteration_296.md)
- [staging-smoke.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/dev/runbooks/staging-smoke.md)

完成定义：

- `YUNMAO_TURN_SHARED_SECRET` 已配置
- 真实 TURN infra 可用
- `/v1/rooms/{id}/ice-servers` 可返回 time-limited TURN credentials evidence

### Track C: Rust data-plane 与 JWT/JWKS 收口

Owner:

- Platform
- Data plane owner
- Auth / Gateway owner

输入：

- [docker-compose.staging.yml](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/deploy/docker-compose.staging.yml)
- [e2e-smoke-final.log](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/reports/iteration_296_evidence/e2e-smoke-final.log)

完成定义：

- `gateway/device-edge/media-edge` 形成新的 staging 启动证据
- JWT HS256 / RS256 / JWKS 路径闭环
- 形成一轮新的端到端 smoke 结果

### Track D: Owner closeout 复核

Owner:

- Owner
- QA

输入：

- 新的外部输入证据
- 新的 staging smoke 证据
- 新的治理刷新产物

完成定义：

- 决定是开启新 phase，还是以 closeout 方式归档当前主线
- 输出新的 owner 决策与治理摘要

## 建议执行顺序

1. 先做 Track A，因为支付与商店资料不到位，真实联调结论不成立
2. Track A 与 Track B 可并行，但 Track C 需要至少拿到部分真实外部输入后再统一复核
3. 最后做 Track D，把新 evidence 收敛成新的 owner 决策

## 需要产出的新文档或证据

1. 一份新的外部输入补齐清单
2. 一份新的 staging / integration evidence 汇总
3. 一份新的 owner closeout / next phase 决策
4. 一次新的治理刷新结果

## 成功判定

只有在以下条件都满足时，这一轮 planning / closeout 才算完成：

- 支付 / Apple IAP / TURN 外部输入已形成真实测试证据
- Rust data-plane 与 JWT/JWKS 联调闭环
- 新治理刷新不再只是 `waiting_for_fix`，而是基于新 evidence 给出更明确的 closeout 结论
- 下一步是“开启新 phase”还是“阶段归档”已有明确负责人决策
