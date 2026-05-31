# Dispatch Prompt: TURN And RTC Closeout

请作为 `yunmao` RTC / 房间链路 + Infra 联合负责人，执行一轮 TURN 与 RTC closeout。

## 任务目标

- 把当前 STUN-only 状态推进到可复核的真实 TURN credentials evidence
- 关闭 TURN shared secret 与托管 infra 相关 blocker

## 必读输入

- `docs/execution/20260529-yunmao-phase6-planning/01-CURRENT-STATUS-SUMMARY.md`
- `docs/execution/20260529-yunmao-phase6-planning/04-NEXT-PHASE-PLAN.md`
- `reports/work_report_iteration_296.md`
- `docs/dev/runbooks/staging-smoke.md`
- `reports/iteration_296_evidence/process-level-smoke.log`
- `reports/iteration_296_evidence/e2e-smoke-final.log`

## 已知事实

- 当前 `/v1/rooms/{id}/ice-servers` 已返回可观测响应
- 当前结果仍是 STUN-only，说明 TURN 入口可达但真实 TURN credentials 尚未闭环
- 这属于外部配置与联调缺口，不应被误写成代码主线失败

## 明确禁止

- 不把 STUN-only 结果写成 TURN ready
- 不跳过新的 ICE/TURN evidence
- 不用口头承诺替代新的接口返回与日志

## 必做事项

1. 配置 `YUNMAO_TURN_SHARED_SECRET` 与相关 TURN infra。
2. 复跑 `/v1/rooms/{id}/ice-servers`，确认返回的不是仅 STUN。
3. 形成最小 RTC / TURN 运行证据：
   - 接口返回
   - 过期时间
   - 凭据模式
   - 失败回退说明
4. 明确仍存在的 infra 或网络层阻塞。

## 输出要求

- `summary.md`
- `turn_runtime_matrix.md`
- `ice_servers_after.json`
- `turn_credential_evidence.md`
- `remaining_infra_blockers.md`

## 完成判定

- TURN 不再只是 STUN-only
- 至少一轮接口结果能证明 time-limited TURN credentials 已形成
- 仍缺失的 infra 条件被明确写回 blocker 列表
