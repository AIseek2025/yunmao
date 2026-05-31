# Runbook: /internal/readyz 失败处置

> 适用：所有 Go svc（user-svc / room-svc / device-svc / feeding-svc / billing-svc / chat-svc / admin-svc）

## 触发条件

K8s readinessProbe 检测到 `/internal/readyz` 返回 503，或人工 curl 看到：

```bash
curl -fsS http://<svc>:<port>/internal/readyz | jq .
# {"ready":false,"deps":{"pg":"err: ...","kafka":"ok",...}}
```

## 处置流程

1. **定位失败依赖**：解析返回 JSON 中 `deps.<name>` 字段以 `err:` 开头者。
2. **分依赖处置**：

   | 依赖 | 探针 | 失败常见原因 | 操作 |
   | --- | --- | --- | --- |
   | `pg` | `SELECT 1` (200ms 超时) | 连接池耗尽 / PG 主从切换 / 网络分区 | `kubectl exec` 进 pod 执行 `psql $YUNMAO_PG_DSN -c 'select 1'` 验证；检查 PG 主机 `pg_stat_activity`、`max_connections` |
   | `redis` | `PING` (200ms) | Redis 节点宕机 / Sentinel 切主 | `redis-cli -h $YUNMAO_REDIS_HOST PING`；检查 sentinel 状态 |
   | `kafka` | TCP Dial 任一 broker (200ms) | broker 全部离线 / 安全组阻断 | `nc -zv $broker 9092`；查看 broker controller 状态 |
   | `mqtt` | `IsConnected()` | broker 离线 / 凭证过期 | 检查 mqttx 客户端日志；如凭证过期重新签发 |
   | `keys` | `KeyProvider.Active().Alg` | KMS 调用失败 / HS256 误注入 | 走 `key-rotation.md` 或本文档同目录 `turn-credentials-rotation.md` |

3. **判断是否需要从 LB 剔除**：

   - K8s readinessProbe 自动会把 Pod 从 Service Endpoint 摘掉，不会向其打流。
   - 公网入口 (gateway) 通过 ingress/SLB readiness 也已剔除。
   - **不要手动 down**，让 readiness 自动恢复。

4. **业务影响评估**：

   - `pg` 失败 → 写入路径受影响（user-svc 登录、room-svc 创房、feeding-svc 创单、billing-svc 钱包）。
   - `kafka` 失败 → 异步事件堆积；outbox relay 会重试，短时间不影响 saga 完成。
   - `redis` 失败 → 频控/幂等键失效；chat-svc 默认 fail-open（允许通过）。
   - `mqtt` 失败 → 设备指令下发降级；device-svc 走 HTTP 回环。
   - `keys` 失败 → 鉴权全断；属高危故障。

5. **恢复后验证**：

   ```bash
   curl -fsS http://<svc>/internal/readyz | jq .
   # 期望 ready=true，deps 全 ok。
   ```

## 演练命令

```bash
# 模拟 pg 失败：在 pod 内 iptables drop 5432
kubectl exec -it <pod> -- iptables -A OUTPUT -p tcp --dport 5432 -j DROP
curl -fsS http://<svc>:<port>/internal/readyz | jq .ready  # false
kubectl exec -it <pod> -- iptables -D OUTPUT -p tcp --dport 5432 -j DROP
```

## 报警阈值

- `up{kubernetes_pod_name=~".*-svc-.*"} == 0`（pod 被 readiness 摘掉时 Prometheus scrape 仍会成功，但 K8s endpoint 剔除可由 `kube_pod_status_ready` 监控）。
- `probe_success{job="blackbox_readyz"} == 0` 持续 2 分钟 → 立即 Page。
