# ADR-0027: OpenAPI 共享契约输出与客户端类型生成

- 状态：Implemented（2026-05-26 第九轮 / Phase 1: Contract And CI Hardening）
- 决策者：yunmao 团队
- 范围：`go/pkg/yunmao/openapi/` + `clients/web` + `clients/admin` + `.github/workflows/openapi-contract.yml`

## 上下文

第八轮交付后，Web/Admin/iOS/Android 四端的 DTO 类型均为手工同源维护。手工维护方式容易在多端并行开发时出现 DTO 漂移，且没有仓库级别的自动化机制来检测"客户端类型 vs 服务端响应"的不一致。

## 决策

1. **在仓库内建立唯一共享契约源**：`go/pkg/yunmao/openapi/v3.json` 作为 OpenAPI 3.0 规范的唯一真相。
2. **客户端类型由契约自动生成**：Web/Admin 客户端使用 `openapi-typescript` 从 `v3.json` 生成 TypeScript 类型文件（`generated-api.ts`），客户端代码不再手工维护 DTO 接口定义，只允许手工维护 client-only 辅助类型。
3. **CI 门禁验证契约一致性**：添加 `.github/workflows/openapi-contract.yml`，在 push / PR 时：
   - `spec-lint`：跑 Go 测试，校验 `v3.json` 结构完整
   - `gen-typescript`：跑 `openapi-gen` 生成类型文件，再编译客户端，确保生成产物能编译
   - `contract-consistency`：在 CI 中重新生成并检查是否与已提交文件一致，防止"改了 v3.json 但没重新生成"

## 后果

### 正面
- 四端 DTO 类型从同源契约生成，消除手工同步风险
- CI 自动阻断契约为空或漂移的提交
- 新增 / 修改 API 时只需更新 `v3.json` + 重新生成 + 更新消费代码，流程可预测

### 负面 / 权衡
- iOS / Android 暂不在本轮接入，仍需手工同步（在后续 phase 中加入 `openapi-generator` / `swift-openapi-generator`）
- `v3.json` 当前手工维护，若后续服务代码改了但未改契约会产生假阳性（CI 只验证契约本身有效，不验证服务端运行时响应匹配 schema）
- 客户端编译时多一步生成（`make openapi-gen`），已加入 package.json scripts

## 验证

| 命令 | 预期 |
| --- | --- |
| `make openapi-lint` | `go/pkg/yunmao/openapi/openapi_test.go` 全部通过 |
| `make openapi-gen` | `clients/web/src/lib/generated-api.ts` 生成成功 |
| `cd clients/web && pnpm test:run && pnpm build` | 9 个测试通过；构建成功，无类型错误 |

## 后续扩展

- 接入 iOS / Android（`swift-openapi-generator` 或 `openapi-generator-cli`）
- 服务端运行时契约校验（在 Go 集成测试中对响应做 schema 校验）
- `admin` 客户端同样接入 `openapi-gen`
