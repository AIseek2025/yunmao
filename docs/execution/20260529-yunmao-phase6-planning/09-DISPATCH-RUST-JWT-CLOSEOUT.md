# Dispatch Prompt: Rust Data-Plane And JWT Closeout

请作为 `yunmao` Platform + Data Plane + Auth/Gateway 联合负责人，执行一轮 Rust data-plane 与 JWT/JWKS closeout。

## 任务目标

- 关闭 `gateway/device-edge/media-edge` 未重建未启动的 blocker
- 关闭 `user-svc` HS256 与 `room-svc` RS256 / JWKS 之间的跨服务联调缺口

## 必读输入

- `docs/execution/20260529-yunmao-phase6-planning/01-CURRENT-STATUS-SUMMARY.md`
- `docs/execution/20260529-yunmao-phase6-planning/04-NEXT-PHASE-PLAN.md`
- `deploy/docker-compose.staging.yml`
- `reports/work_report_iteration_296.md`
- `reports/iteration_296_evidence/e2e-smoke-final.log`
- `reports/codemaster/project_owner_brief.md`

## 已知事实

- Phase 6 已经验证了仓库内 process-level smoke，但 Rust data-plane 本轮未全部参与
- 当前 JWT 迁移问题已被诚实记录，不应在未修复前宣称全链路联调通过
- 本任务的重点是运行态闭环，而不是继续扩新功能

## 明确禁止

- 不只做静态代码修改而不产出新的 staging 启动证据
- 不在 JWT/JWKS 缺口仍存在时宣称跨服务链路通过
- 不把本轮任务扩展成无关重构

## 必做事项

1. 重建并验证 Rust data-plane 相关组件：
   - `gateway`
   - `device-edge`
   - `media-edge`
2. 复核 image / binary 与 staging 启动路径是否一致。
3. 收口 JWT/JWKS 迁移：
   - 明确谁发 token
   - 明确谁校验 token
   - 明确 JWKS 发布与消费路径
4. 复跑最小跨服务 smoke，并记录新的运行结论。

## 输出要求

- `summary.md`
- `rust_dataplane_runtime_matrix.md`
- `jwt_jwks_alignment.md`
- `cross_service_smoke_after.md`
- `remaining_runtime_blockers.md`

## 完成判定

- Rust data-plane 已形成新的 staging 启动证据
- JWT/JWKS 路径已可被说明和复核
- 最少一轮跨服务 smoke 不再被已知迁移缺口阻断
