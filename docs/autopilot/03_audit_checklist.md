# yunmao Audit Checklist

## Universal Checks

- `work_report` 必须只陈述本轮真实写回与真实验证，不得把未执行的 sandbox/prod 联调写成完成。
- 任何关于支付、审核、TURN、移动端真机、Android 构建的结论，都必须注明实际运行环境与限制。
- 规划文档与代码现实冲突时，以仓库当前实现和最新 deliverable 为准，并把差异写回报告。

## Phase A: Contract And CI Hardening

审计重点：

1. 是否形成明确的共享契约输出，而不是继续手工维护多份 DTO。
2. 是否至少让一个客户端或一条 CI 流真正消费该契约。
3. 是否补齐了一条此前缺失的自动化门禁：
   - Android 构建
   - webrtc-rs / TURN 真实验证
   - 客户端共享 schema 校验
   - 若仓库当前未建立 GitHub 集成，但已存在仓库内可复现、与目标 CI 等价的本地自动化门禁，则不强制要求远端 GitHub Actions 实跑作为 Phase A 放行前提
4. 是否避免把外部凭据缺失的问题伪装成“已完成”。

阻断条件：

- 只更新文档，没有任何代码、脚本、CI 或生成链路落地。
- 生成了 schema，但没有任何消费方或验证步骤。
- 仍然把多端 DTO 漂移留在手工同步状态，且无缓解措施。

## Phase B: Admin Productization

审计重点：

1. Admin 登录是否真正接入角色鉴权。
2. `/admin/rooms` 与 `/admin/wallet` 是否摆脱占位文案。
3. 页面、API、权限与错误态是否一致。

阻断条件：

- 仍然只有静态页面或 mock 数据。
- 没有角色 gate 或权限绕过。

## Phase C: Cross-Client E2E And Media Validation

审计重点：

1. Web/iOS/Android 是否至少有一条真实关键路径自动化。
2. 媒体/WHEP 联调是否有可复现证据。
3. 真机、模拟器、CI 和 mock backend 的边界是否写清楚。

阻断条件：

- 没有任何新的 E2E 或联调证据。
- 报告无法说明测试环境与限制。

## Phase D: External Credential Cutover Readiness

审计重点：

1. 是否形成凭据注入与切换流程。
2. 是否明确灰度、回滚、runbook 和观测点。
3. 是否对未到位的外部材料做了显式 blocker 标注。

阻断条件：

- 依赖外部凭据却没有任何准备文档。
- 宣称可生产切换但无 runbook / rollback / checklist。

## Phase 5: Production Deployment Assets And Env Templates

审计重点：

1. 是否真正补齐了 staging/prod 级部署资产、环境模板或等价装配入口，而不是只修改描述性文档。
2. 是否修复了 `deploy/README.md`、`deploy/docker-compose.app.yml`、`Makefile`、前端部署说明之间的关键不一致。
3. 是否显式处理了 secrets 注入与轮换边界，而不是继续保留开发默认 secret 作为上线答案。
4. 是否为 Web/Admin 的正式部署模式给出了真实资产、脚本或 runbook。

阻断条件：

- 只写分析结论，没有任何部署资产、模板、脚本或说明文件落地。
- 仍保留开发默认 secret 且未说明正式注入来源。
- 仍把本地开发 compose 直接表述为生产部署方案。

## Phase 6: Staging Parity Smoke And External Integration

审计重点：

1. 是否从 handler 级或 httptest 级验证推进到了进程级 / 环境级 smoke。
2. 是否为 `/internal/diagnose/credentials`、healthz / readyz、关键 API、Web/Admin 访问、TURN 或支付联调形成真实证据。
3. 是否明确 staging / sandbox / mock / 本地 compose 的边界，而不是把任意一种验证夸大成“可上线”。
4. 是否把失败回滚动作或 blocker 诚实写回 work report。

阻断条件：

- 只有单测/httptest 级证据，没有任何进程级或环境级 smoke。
- 把未执行的 TURN / 支付 / Apple IAP 联调写成已完成。
- 缺少最小验证日志、curl 样例、启动命令或观测证据。

## Phase 7: Release Gates And Rollback Hardening

审计重点：

1. 是否真正形成了 build / push / deploy / smoke / rollback 的最小 release gate。
2. 是否为数据库迁移提供了 down migration 或前向兼容回退策略。
3. 是否把 QoE、压测、安全扫描、密钥扫描、演练记录纳入阶段性证据包。
4. 是否把 Prometheus / Grafana / alerts 与发布门禁指标显式绑定。

阻断条件：

- 只有“建议未来补齐”的说明，没有任何 release workflow、脚本、模板或 runbook 落地。
- `migrate-down` 仍完全停留在手工口径，且本轮未给出可执行改进。
- 没有任何与发布证据包相关的产物、模板或归档入口。

## Phase 8: Phase 1 MVP Recovery

审计重点：

1. 是否真正推进了 Web/App MVP 的关键链路，而不是回到基础设施泛读。
2. 是否形成了跨端一致性、通知/深链、房间/投喂/个人中心等最小可复现证据。
3. 是否对移动端真机、模拟器、mock backend、wiremock 的边界做了明确说明。

阻断条件：

- 没有任何新的 MVP 代码或验证证据。
- 只更新规划或 TODO，没有真实客户端/服务端落地。
- 把未跑的移动端链路表述为已验收。

## Phase 9: Phase 2-3 Productization Recovery

审计重点：

1. 是否推进了增长/运营/支付/权益/设备健康中的至少一条真实业务链路。
2. 是否为支付回调、对账、权益一致性或运营能力提供了真实验证证据。
3. 是否对仍缺失的机构入驻、财务导出、风控策略做了显式 blocker 标注。

阻断条件：

- 只有概念性规划，没有新增业务闭环或最小验证。
- 把“凭据准备度”误写成“商业化功能已完成”。
- 未说明真实外部联调与 mock/sandbox 的差异。

## Phase 10: Phase 4 Multi-Platform And Scale Recovery

审计重点：

1. 是否推进了小程序、App 深度能力、Rust 数据面收益验证或规模化材料中的至少一条真实主线。
2. 是否形成了 QoE、容量、故障演练、跨 region 或收益验证中的最小证据。
3. 是否避免把方向性基础或已有底座误判为“Phase 4 已完成”。

阻断条件：

- 没有任何新的多端增强、Rust 收益或规模化材料。
- 只有概念文档，没有代码、脚本、runbook 或验证证据落地。
- 把未验证的容量或演练目标写成已达成。
