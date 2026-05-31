# Phase Transition: Phase 9 → Phase 5

## Transition Type

`complete_closeout_and_replan` — Phase 9 exit criteria met (per closeout matrix); kickoff target Phase 5 is now the active phase.

## Authority

- `docs/autopilot/02_phase_plan.md` lines 146-152 (Phase 7 exit condition): owner must be able to issue `continue_closeout | open_new_phase | archive_current_phase`
- `docs/autopilot/02_phase_plan.md` lines 206-215 (Kickoff Target): Phase 5 is the declared kickoff target
- `reports/iteration_304_evidence/phase_9_closeout_matrix.json`: owner decision = `archive_current_phase`

## Phase 9 Closeout Summary

| Exit criterion | Status | Evidence |
|---|---|---|
| 至少一条增长/运营能力和一条商业化/权益能力具备真实代码闭环 | **MET** | `TestFullPaymentChain_E2E` + `TestFullPaymentChain_WalletBalanceConsistency` |
| 至少一条支付或对账链路具备真实受控验证证据 | **MET** | Full-chain reconciliation trace (diffs=0 pre- and post-refund) |

## Phase 5 Scope (per `02_phase_plan.md` lines 96-114)

### Target

补齐"能部署"的工程底座，从"开发可跑"进入"预发/生产可装配"。

### Suggested Scope

1. 为后端、Web、Admin 明确正式部署资产（不止开发 compose）
2. 统一环境模板、变量说明与 secrets 注入边界
3. 纠正部署文档、`Makefile` 与应用 compose 的不一致项
4. 为前端正式部署模式补齐最小资产或明确 runbook

### Exit Conditions

- 至少形成一套面向 staging 的正式部署/装配资产
- 至少形成一套统一环境模板或等价配置说明
- 部署文档与运行配置的关键端口、入口与命令保持一致

### First-Move Priorities (per `02_phase_plan.md` lines 221-224)

1. `deploy/` + `Makefile`: staging/prod 级部署入口、端口对齐、前后端部署说明
2. 环境模板与 secrets: `.env.example` 统一模板
3. 最小 CD 入口: build/push/deploy/smoke/rollback 最小工作流或 runbook

## Blocker Carry-Over

From Phase 9 to Phase 5 (informational; not blockers for Phase 5):

- WeChat Pay / Alipay / Apple IAP sandbox vendor accounts (`external_wait`) — these affect Phase 6+ staging parity smoke, not Phase 5 deployment asset readiness.

## Verification Command for Next Iteration

```bash
# Phase 5 first iteration should produce at least one of:
ls deploy/*.yml deploy/.env.example Makefile
# plus a reconciliation of deploy/README.md vs actual compose entry points
```
