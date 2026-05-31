# yunmao Result Collection Checklist

## 用法

本清单用于在 `07-10` 四路 dispatch 执行后，统一核对结果是否已经达到可被 owner 复核和治理面承接的最小标准。

建议搭配 [16-OWNER-BATCH-RUNBOOK.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/16-OWNER-BATCH-RUNBOOK.md) 一起使用。

## 收集前置条件

- 当前仍以 [06-DISPATCH-INDEX.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/06-DISPATCH-INDEX.md) 为唯一发单真相源
- 各执行路均已明确本轮 trace / session 标识
- 各执行路都知道自己对应哪一份 write-back 模板
- 所有新增证据都已落盘，而不是只存在于聊天记录

## 单路结果核对表

| Track | Dispatch | Template | 必收文件 | 最低结论要求 |
|------|----------|----------|----------|--------------|
| Payments / IAP | [07](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/07-DISPATCH-PAYMENTS-IAP-CLOSEOUT.md) | [12](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/12-TEMPLATE-PAYMENTS-IAP-WRITEBACK.md) | `summary.md`, `payments_iap_input_matrix.md`, `credential_diagnostics_after.json`, `credential_diagnostics_delta.md`, `external_waiting_items.md` | 必须能判定 `pending | partial | complete | external_wait` |
| TURN / RTC | [08](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/08-DISPATCH-TURN-RTC-CLOSEOUT.md) | [13](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/13-TEMPLATE-TURN-RTC-WRITEBACK.md) | `summary.md`, `turn_runtime_matrix.md`, `ice_servers_after.json`, `turn_credential_evidence.md`, `remaining_infra_blockers.md` | 必须能判定 `pending | partial | complete | infra_blocked` |
| Rust / JWT | [09](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/09-DISPATCH-RUST-JWT-CLOSEOUT.md) | [14](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/14-TEMPLATE-RUST-JWT-WRITEBACK.md) | `summary.md`, `rust_dataplane_runtime_matrix.md`, `jwt_jwks_alignment.md`, `cross_service_smoke_after.md`, `remaining_runtime_blockers.md` | 必须能判定 `pending | partial | complete | runtime_blocked` |
| Owner closeout | [10](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/10-DISPATCH-OWNER-CLOSEOUT.md) | [15](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/15-TEMPLATE-OWNER-CLOSEOUT-WRITEBACK.md) | `owner_closeout_decision.md`, `owner_closeout_decision.json`, `blocker_closure_matrix.md`, `next_action_recommendation.md` | 必须能判定 `continue_closeout | open_new_phase | archive_current_phase` |

## 逐项检查

1. Payments / IAP
   - 已存在一份基于 `12` 的 write-back 实例
   - WeChat、Alipay、Apple IAP 三项在矩阵中都有 `Before / After / Status / Evidence`
   - `credential_diagnostics_after.json` 与 `credential_diagnostics_delta.md` 都可读取
   - 若仍缺外部输入，结论明确写成 `external_wait`
2. TURN / RTC
   - 已存在一份基于 `13` 的 write-back 实例
   - `ice_servers_after.json` 可证明当前仍是 STUN-only 或已进入 TURN credentials
   - `turn_credential_evidence.md` 明确写清凭据模式、过期时间或失败原因
   - 剩余 infra 问题没有被混写成代码缺陷
3. Rust / JWT
   - 已存在一份基于 `14` 的 write-back 实例
   - `rust_dataplane_runtime_matrix.md` 覆盖 `gateway / device-edge / media-edge`
   - `jwt_jwks_alignment.md` 能说明 issuer、verifier、JWKS publish/consume 关系
   - `cross_service_smoke_after.md` 是本轮新结果，不是旧日志复用
4. Owner closeout
   - 只有在前三路都齐备后才创建基于 `15` 的 write-back 实例
   - `blocker_closure_matrix.md` 中 4 类 blocker 都有 `Status / Owner / Evidence / Next Action`
   - `next_action_recommendation.md` 明确说明为什么不是另外两条路径

## 汇总顺序

1. 先收 `12`、`13`、`14` 三份 write-back 与各自证据文件。
2. 再由 owner 读取三份结果，生成 `15` 对应的最终 write-back 与 4 个 owner 输出文件。
3. 若 owner 判断与当前治理产物不再一致，再执行治理刷新。
4. 刷新后至少复核：
   - `project_owner_brief.md`
   - `run_summary`
   - `owner_escalation_report.json`
   - `dashboard_summary.json`

治理刷新时，优先按 [19-GOVERNANCE-REFRESH-CLOSEOUT-CHECKLIST.md](file:///Users/brando/Documents/trae_projects/CodeMaster/isolated_autoruns/yunmao/docs/execution/20260529-yunmao-phase6-planning/19-GOVERNANCE-REFRESH-CLOSEOUT-CHECKLIST.md) 执行。

## 完成判定

- 三路技术/集成结果都已模板化落盘
- 每个 blocker 都能指向最新 evidence path
- owner 已给出唯一后续路径，而不是同时保留多个口头选项
- 若仍未收口，剩余 blocker、owner、下一步动作都已写清

## 不通过信号

- 只有结论，没有证据路径
- 只有证据文件，没有模板化 write-back
- owner closeout 在三路结果缺失时提前执行
- 把 `partial`、`infra_blocked`、`runtime_blocked` 写成 ready / done
