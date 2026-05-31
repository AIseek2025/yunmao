# ADR-0026：弹幕审核真接（阿里云 Green）+ 词表热更新

- 状态：Accepted（2026-05-25，第八轮 C 落地）
- 关联：ADR-0022（弹幕审核 Provider + 撤回语义）

## 背景

第七轮我们引入了 Provider 抽象 + Local + AliyunGreenProvider mock + Manager fallback。
本轮目标：
1. 真接阿里云内容安全 Green API（text/scan）；
2. 词表通过 PG + 事件总线热更新（admin → chat.wordlist.updated → chat-svc local cache）；
3. 凭据 / 端点全部由 KeyProvider + env 注入，dev / CI mock 模式默认。

## 决策

### 1. 真接策略：stdlib 实现 POPv1 签名

不引入 `github.com/aliyun/alibaba-cloud-sdk-go/services/green`（重 SDK），而是用 stdlib
实现 RPC-style POPv1 签名：
- HMAC-SHA1 + Base64 over canonical query string；
- 字典序拼接 + 严格 RFC3986 percent-encode；
- 端点 `https://green.{region}.aliyuncs.com/green/text/scan`。

实现：`services/chat-svc/internal/moderation/aliyun_green.go`：
- `AliyunGreenRealClient.Inspect(ctx, text)` 发起请求；
- `signAliyunRPC(method, params, secret)` 签名；
- `parseAliyunGreenResponse(raw, original)` 把 `suggestion` 映射到 yunmao Action：
  - block → ActionBlock；
  - review → ActionWarn；
  - pass → ActionPass。

`HTTPDoer` 接口注入 mock server，测试覆盖签名稳定性、500 错误、suggestion 解析。

### 2. 凭据管理（与 ADR-0015 KMS 对齐）

- `AliyunGreenConfig.AccessKey` / `AccessSecret` 通过 KMS 注入；
- 缺失时强制 MockMode（不调真实 API）；
- `Manager.SetPrimary(AliyunGreenRealClient)` 触发热切；
- 超时硬上限 800ms（Manager 在 Provider 内部 ctx timeout 控制），超时 fallback Local。

### 3. 词表热更新

数据模型（migration 0009）：
```
chat_wordlists(id, region, language, word, action, version, created_at, updated_at)
UNIQUE (region, language, word)
```

`admin-svc.AdminService.ImportWordlist(entries, updatedBy)`：
- 批量 upsert（生产 PG，dev `InMemoryWordlistSink`）；
- 返回 version（自增整数）；
- 写 outbox：topic=`chat.wordlist.updated`，payload={region,language,version,count}。

`chat-svc`：
- 启动时拉 `GET admin-svc /v1/admin/chat/wordlist`；
- 订阅 Kafka topic `chat.wordlist.updated` → 收到事件后重拉；
- 兜底：5min 轮询 `/version`，与本地比较；
- 把 entries 灌进 `LocalProvider.SetWords(...)`。

### 4. 路由

`PUT /v1/admin/chat/wordlist`：
- Content-Type=application/json：`{entries: [{region,language,word,action}], updated_by}`；
- Content-Type=text/csv：逐行 `region,language,word,action`，X-Yunmao-Admin 头表示 updated_by；
- 返回 `{version, applied}`。

`GET /v1/admin/chat/wordlist?region=&language=`：列出。
`GET /v1/admin/chat/wordlist/version`：当前版本。

### 5. fallback 策略

- 真接路径失败 / 超时 → Manager 自动 fallback 到 LocalProvider；
- Provider 内部 metrics：`moderation_calls{provider, status}`、`moderation_latency_seconds{provider}`；
- /readyz 探针：Manager.Active() == "aliyun_green" 但最近 100 次有 > 20% fail → degraded。

## 影响

- chat-svc：本地词表实时反映 admin 改动（无需重启）；
- 监控：增加 `moderation_calls{provider="aliyun_green",status=...}`；
- 凭据滚动：双密钥窗口（与 TURN 同思路）：admin 改 KMS 后下次调用生效，旧失败重试自动切。

## 待业务确认

1. **凭据获取流程**：阿里云 sandbox + 生产 AK/SK；
2. **腾讯 TMS 双供应商**：是否在 Manager 增加 `TencentTmsProvider`，作为 aliyun 的姊妹（互为 fallback）？
3. **词表初始化**：上线前由产品提供初版（多语言敏感词、广告词、政治词）。

## 复现命令

```bash
cd go/services/chat-svc
go test ./internal/moderation/... -v
# 真接 sandbox：
YUNMAO_CHAT_ALIYUN_AK=xxx YUNMAO_CHAT_ALIYUN_SK=yyy \
  go test ./internal/moderation/... -run RealAliyun -v -tags realsdk
```
