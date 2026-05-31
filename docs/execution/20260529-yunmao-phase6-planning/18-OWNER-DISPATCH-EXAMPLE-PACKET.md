# yunmao Owner Dispatch Example Packet

## 用法

本示例包展示负责人如何把当前 Phase 6 planning packet 变成一轮可直接发出的最小 closeout 批次。

它不是新的规范来源，而是 [16-OWNER-BATCH-RUNBOOK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/16-OWNER-BATCH-RUNBOOK.md) 的实例化参考。

## 示例场景

- 日期: `2026-05-29`
- 批次标识: `20260529-phase6-closeout-round1`
- 目标: 以一轮最小 closeout 判断 Payments/IAP、TURN/RTC、Rust/JWT 三路 blocker 是否已经具备进入 owner closeout 的条件
- 负责人结论起点: 仍是 `planning / request_planning / dispatch_ready=false`

## 示例目录布局

```text
reports/closeout/
  20260529-payments-iap-round1/
    payments_iap_writeback.md
    summary.md
    payments_iap_input_matrix.md
    credential_diagnostics_after.json
    credential_diagnostics_delta.md
    external_waiting_items.md
  20260529-turn-rtc-round1/
    turn_rtc_writeback.md
    summary.md
    turn_runtime_matrix.md
    ice_servers_after.json
    turn_credential_evidence.md
    remaining_infra_blockers.md
  20260529-rust-jwt-round1/
    rust_jwt_writeback.md
    summary.md
    rust_dataplane_runtime_matrix.md
    jwt_jwks_alignment.md
    cross_service_smoke_after.md
    remaining_runtime_blockers.md
  20260529-owner-closeout-round1/
    owner_closeout_writeback.md
    owner_closeout_decision.md
    owner_closeout_decision.json
    blocker_closure_matrix.md
    next_action_recommendation.md
```

## 示例发单顺序

1. 先发 `payments-iap`
   - dispatch: [07-DISPATCH-PAYMENTS-IAP-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/07-DISPATCH-PAYMENTS-IAP-CLOSEOUT.md)
   - write-back: [12-TEMPLATE-PAYMENTS-IAP-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/12-TEMPLATE-PAYMENTS-IAP-WRITEBACK.md)
2. 再并行发 `turn-rtc` 与 `rust-jwt`
   - dispatch: [08-DISPATCH-TURN-RTC-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/08-DISPATCH-TURN-RTC-CLOSEOUT.md) / [09-DISPATCH-RUST-JWT-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/09-DISPATCH-RUST-JWT-CLOSEOUT.md)
   - write-back: [13-TEMPLATE-TURN-RTC-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/13-TEMPLATE-TURN-RTC-WRITEBACK.md) / [14-TEMPLATE-RUST-JWT-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/14-TEMPLATE-RUST-JWT-WRITEBACK.md)
3. 三路结果齐备后再发 `owner-closeout`
   - dispatch: [10-DISPATCH-OWNER-CLOSEOUT.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/10-DISPATCH-OWNER-CLOSEOUT.md)
   - write-back: [15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md)

## 示例发单正文骨架

### Batch 1: Payments / IAP

```text
任务名: yunmao 20260529 payments-iap closeout round1
必读:
- 01-CURRENT-STATUS-SUMMARY.md
- 04-NEXT-PHASE-PLAN.md
- 07-DISPATCH-PAYMENTS-IAP-CLOSEOUT.md
- project_owner_brief.md
输出目录:
- reports/closeout/20260529-payments-iap-round1/
必交文件:
- payments_iap_writeback.md
- summary.md
- payments_iap_input_matrix.md
- credential_diagnostics_after.json
- credential_diagnostics_delta.md
- external_waiting_items.md
禁止:
- 不得把 partial 写成 ready
- 不得缺少 diagnostics delta
```

### Batch 2A: TURN / RTC

```text
任务名: yunmao 20260529 turn-rtc closeout round1
必读:
- 01-CURRENT-STATUS-SUMMARY.md
- 04-NEXT-PHASE-PLAN.md
- 08-DISPATCH-TURN-RTC-CLOSEOUT.md
输出目录:
- reports/closeout/20260529-turn-rtc-round1/
必交文件:
- turn_rtc_writeback.md
- summary.md
- turn_runtime_matrix.md
- ice_servers_after.json
- turn_credential_evidence.md
- remaining_infra_blockers.md
禁止:
- 不得把 STUN-only 写成 TURN ready
```

### Batch 2B: Rust / JWT

```text
任务名: yunmao 20260529 rust-jwt closeout round1
必读:
- 01-CURRENT-STATUS-SUMMARY.md
- 04-NEXT-PHASE-PLAN.md
- 09-DISPATCH-RUST-JWT-CLOSEOUT.md
- project_owner_brief.md
输出目录:
- reports/closeout/20260529-rust-jwt-round1/
必交文件:
- rust_jwt_writeback.md
- summary.md
- rust_dataplane_runtime_matrix.md
- jwt_jwks_alignment.md
- cross_service_smoke_after.md
- remaining_runtime_blockers.md
禁止:
- 不得只交静态修改而没有新的运行证据
```

### Batch 3: Owner Closeout

```text
任务名: yunmao 20260529 owner-closeout round1
前置:
- 已收到 payments_iap_writeback.md
- 已收到 turn_rtc_writeback.md
- 已收到 rust_jwt_writeback.md
必读:
- 10-DISPATCH-OWNER-CLOSEOUT.md
- 15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md
- 三路 write-back 与证据目录
输出目录:
- reports/closeout/20260529-owner-closeout-round1/
必交文件:
- owner_closeout_writeback.md
- owner_closeout_decision.md
- owner_closeout_decision.json
- blocker_closure_matrix.md
- next_action_recommendation.md
禁止:
- 不得在前三路结果缺失时提前下结论
```

## 示例收口判断

- 若 Payments/IAP 仍依赖外部真实沙箱资料，可保留 `external_wait`
- 若 TURN 仍只有 STUN-only，可保留 `infra_blocked`
- 若 Rust/JWT 仍无新的 staging 运行证据，可保留 `runtime_blocked`
- 只有在 owner 能为 4 类 blocker 都给出最新 `Status / Owner / Evidence / Next Action` 时，才进入最终 owner 决策

## 下一步

1. 用 [17-RESULT-COLLECTION-CHECKLIST.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/17-RESULT-COLLECTION-CHECKLIST.md) 回收三路与 owner 结果。
2. 复制 [20-OWNER-CLOSEOUT-OUTPUT-EXAMPLE-INDEX.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/20-OWNER-CLOSEOUT-OUTPUT-EXAMPLE-INDEX.md) 中的 `21-24` 实例文件，生成真实 owner 输出。
3. 若 owner 决策改变了治理状态，用 [19-GOVERNANCE-REFRESH-CLOSEOUT-CHECKLIST.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/19-GOVERNANCE-REFRESH-CLOSEOUT-CHECKLIST.md) 刷新并复核治理产物。
