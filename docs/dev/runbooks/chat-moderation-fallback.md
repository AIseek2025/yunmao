# Runbook: 弹幕审核 provider 降级 / fallback

> 适用：chat-svc 外接审核（阿里云 Green / 腾讯 TMS / 网易易盾）+ 本地词表 fallback

## 配置回顾

- 环境变量：`YUNMAO_CHAT_MODERATION=local|aliyun_green|tencent_tms|easeshield`，默认 `local`。
- feature flag `chat.moderation_provider`：admin 可热切。
- ModerationProvider trait（Go interface）：

  ```go
  type Provider interface {
      Inspect(ctx context.Context, text string) (Decision, error)
  }
  type Decision struct {
      Action string  // pass | warn | hide | recall | block
      Score  float64
      Reason string
      Provider string
  }
  ```

- `local` provider 永远可用；其他 provider 调用失败时 chat-svc 自动 fallback 到 local（带 metrics 标签 `provider="<external>" outcome="fallback"`）。

## 触发条件

| 现象 | 阈值 | 来源 |
| --- | --- | --- |
| 外部 provider 调用失败率 | > 5% 持续 5min | `yunmao_chat_moderation_calls_total{outcome="error"}` |
| 外部 provider P99 延迟 | > 500ms 持续 5min | `yunmao_chat_moderation_latency_seconds_bucket` |
| 外部 provider 凭据过期 | 任意 1 次 401/403 | `yunmao_chat_moderation_auth_failed_total` |
| local 词表过期 | 超过 30 天未更新 | `yunmao_chat_wordlist_age_seconds` |

## 处置流程

### 1. 即时切回 local（运维 < 30s）

```bash
curl -X PUT 'http://admin-svc:8401/v1/admin/feature-flags/chat.moderation_provider' \
  -H 'Content-Type: application/json' \
  -d '{"enabled": true, "scope": "global", "value": {"provider":"local"}}'
```

- chat-svc 监听 feature flag 变化（5min 缓存），下次审核切到 local。
- 紧急情况：滚动重启 chat-svc 强制立即生效。

### 2. 评估 false-negative 风险

- local 词表覆盖率约 70%（基于历史数据）；外部审核覆盖率约 95%。
- 切回 local 期间：把高风险房间（主播信任分 < 60）的 chat-svc 频控阈值临时下调 50%（人工策略）。

### 3. 主因排查

```bash
# 阿里云 Green
curl -fsS 'https://green.cn-shanghai.aliyuncs.com' -o /dev/null -w '%{http_code}\n'
grep aliyun_green /var/log/yunmao/chat-svc.log | tail -50

# 腾讯 TMS
curl -fsS 'https://cms.tencentcloudapi.com' -o /dev/null -w '%{http_code}\n'

# 凭证轮换
# yunmao 把 access key 放在 KMS / k8s secret；
kubectl get secret yunmao-moderation -o yaml | yq '.data | keys'
```

### 4. 切回外部 provider

```bash
# 验证健康后再切回。
curl -X PUT 'http://admin-svc:8401/v1/admin/feature-flags/chat.moderation_provider' \
  -d '{"enabled": true, "scope": "global", "value": {"provider":"aliyun_green"}}'

# 灰度回切：先指定 5% 房间。
# 当前 v1 还未实现按 room 分片，留 TODO。
```

## 词表热更新

- 词表存 PG 表 `chat_wordlists`（v2 引入）或 admin feature flag `chat.wordlist`（v1 兼容）。
- chat-svc 每 5min 拉取一次；可手工触发：

  ```bash
  curl -X POST 'http://chat-svc:8300/internal/wordlist/reload'
  ```

## 报警阈值

- `yunmao_chat_moderation_calls_total{outcome="fallback"}` rate > 1/s 持续 10min → 警告。
- 持续 30min → Page。
