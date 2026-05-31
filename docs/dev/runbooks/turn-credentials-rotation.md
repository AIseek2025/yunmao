# Runbook: TURN 凭证轮换

> 适用：coturn TURN server + room-svc `/v1/rooms/{id}/ice-servers` 短期凭证签发

## 设计回顾

- TURN auth：`use-auth-secret` 模式 + RFC 7635 draft-uberti-rtcweb-turn-rest-00 时间限制凭证。
- room-svc 持有 `TurnPrimarySecret` + `TurnLegacySecret`（envvar 或 KMS 派生）：
  - 签发：用 primary secret 生成 `username:expiry / password=base64(HMAC-SHA1(secret, username))`。
  - 校验：先用 primary，失败再用 legacy。
- coturn 同时配置两个 `static-auth-secret`（多行）。
- 凭证 TTL：5min（短期）；轮换窗口：两个 secret 共存至少 1 个 TTL（保险 15min）。

## 触发条件

| 现象 | 阈值 | 操作 |
| --- | --- | --- |
| 静态密钥泄露（提交到 git / 日志） | 即时 | 立即轮换 + 撤销旧密钥 |
| 定期轮换（90d） | 时间到 | 走灰度切换 |
| coturn TURN 错误率激增 | `coturn_total_allocations_failed` rate > 5/s | 检查是否密钥不匹配 |

## 轮换步骤（双密钥滚动）

### Phase 1：注入新 secret（不影响在用）

```bash
# 1. 生成新 secret（32 字节随机）
NEW_SECRET=$(openssl rand -hex 32)

# 2. 写入 KMS（生产）或 k8s secret（dev）
kubectl -n yunmao patch secret yunmao-turn \
  --type=json \
  -p="[{\"op\":\"add\",\"path\":\"/data/primary_new\",\"value\":\"$(echo -n $NEW_SECRET | base64)\"}]"

# 3. coturn 配置 reload：把新 secret 作为「另一个」 static-auth-secret 注入
# coturn 通过 SIGHUP reload 配置文件。
docker compose -f deploy/turn/docker-compose.turn.yml exec coturn pkill -HUP turnserver
```

### Phase 2：room-svc 把新 secret 升级为 primary（旧 secret 降为 legacy）

```bash
# 设置 envvar 并滚动重启
kubectl -n yunmao set env deploy/room-svc \
  YUNMAO_TURN_SHARED_SECRET=$NEW_SECRET \
  YUNMAO_TURN_SHARED_SECRET_LEGACY=$OLD_SECRET

kubectl -n yunmao rollout restart deploy/room-svc
kubectl -n yunmao rollout status deploy/room-svc
```

### Phase 3：观察 + 清理旧 secret

```bash
# 观察 15 分钟（远大于 5min TTL），确认无 legacy 校验请求
grep "turn:verify legacy=true" /var/log/yunmao/room-svc.log | wc -l
# 期望 0

# 从 coturn 删除旧 secret
kubectl -n yunmao patch secret yunmao-turn \
  --type=json \
  -p="[{\"op\":\"remove\",\"path\":\"/data/primary_old\"}]"

# coturn reload
docker compose exec coturn pkill -HUP turnserver

# 从 room-svc 移除 legacy 配置
kubectl -n yunmao set env deploy/room-svc YUNMAO_TURN_SHARED_SECRET_LEGACY=
kubectl -n yunmao rollout restart deploy/room-svc
```

## 应急直接旋转（密钥泄露）

```bash
# 1. 立即生成新 secret + 写入 coturn + room-svc
# 2. 不走灰度，直接覆盖（接受 5min 内已签发的 TURN session 中断）
# 3. 通过 admin-svc 把 webrtc 灰度临时降到 0%（runbook: webrtc-degrade.md）
# 4. 完成后再恢复
```

## 验证命令

```bash
# 1. 取一个测试 TURN credential
curl -fsS 'http://room-svc:8201/v1/rooms/room123/ice-servers' \
  -H 'Authorization: Bearer <user-jwt>' | jq .

# 2. 验证 turncli 能拿到中继
turnutils_uclient -v -t -T -u <username> -w <credential> coturn.yunmao.local
```

## 报警阈值

- `coturn_total_allocations_failed` rate > 5/s 持续 2min → Page。
- `yunmao_room_svc_turn_legacy_verify_total` 在轮换 Phase 3 后仍 > 0 持续 10min → 警告（说明有客户端缓存了旧 username）。
