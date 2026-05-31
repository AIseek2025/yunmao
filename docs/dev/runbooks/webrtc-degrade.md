# Runbook: WebRTC 降级 / 灰度回滚

> 适用：room-svc + webrtc-svc / media-edge + gateway WHEP 链路

## 关键指标

| 指标 | 阈值 | 说明 |
| --- | --- | --- |
| `yunmao_webrtc_whip_publish_failed_total / yunmao_webrtc_whip_publish_total` | > 2% (5min) | WHIP 接流失败率 |
| `yunmao_webrtc_whep_subscribe_failed_total / yunmao_webrtc_whep_subscribe_total` | > 5% (5min) | WHEP 订阅失败率 |
| `yunmao_webrtc_ice_failed_total` | rate(1m) > 5 | ICE 协商失败 |
| `yunmao_webrtc_dtls_handshake_failed_total` | rate(1m) > 5 | DTLS 握手失败（需 webrtc-rs 集成完成后才有） |
| `yunmao_webrtc_rtp_drop_total` | rate(1m) > 1000 | SFU 丢包 |

## 触发条件

任意一个达到阈值 → 进入降级流程；若主播侧弹「无法推流」客诉 → 直接执行步骤 1 全局回滚。

## 处置流程

### 1. 全局回滚到 LL-HLS（最快 30s 内生效）

```bash
# admin-svc 默认监听 :8401；feature flag 全局关闭。
curl -X PUT 'http://admin-svc:8401/v1/admin/feature-flags/room.webrtc.enabled' \
  -H 'Content-Type: application/json' \
  -d '{"enabled": false, "scope": "global", "value": {"gray_percent": 0}}'
```

- room-svc 读取 flag 后，`GetRoom` 返回 `protocol_pref="ll-hls"`。
- 客户端 web-demo / iOS / Android 自动重连 LL-HLS。

### 2. 部分灰度回滚（按房间池缩量）

```bash
# 5% → 1% 缩量（仅当个别房间出问题，不影响整体）。
curl -X PUT 'http://admin-svc:8401/v1/admin/feature-flags/room.webrtc.enabled' \
  -H 'Content-Type: application/json' \
  -d '{"enabled": true, "scope": "global", "value": {"gray_percent": 1}}'
```

### 3. 验证灰度分布

```bash
# admin-svc 灰度模拟工具：从 N 个虚拟 room_id 看分布。
curl 'http://admin-svc:8401/v1/admin/webrtc/gray-sim?room_count=1000' | jq .
# {"flag":"room.webrtc.enabled","hit_count":50,"miss_count":950,"hit_percent":5.0,...}
```

### 4. 检查 TURN 健康

如失败率集中在 NAT 难穿透场景（candidate=relay 占比 > 80% 且失败率高）：

```bash
# coturn 暴露 Prometheus exporter（默认 :9641）
curl -fsS http://coturn-exporter:9641/metrics | grep coturn_total_sessions
```

### 5. 单房间紧急切回 LL-HLS

```bash
# 通过 admin 工具把主播 protocol_pref override 为 ll-hls。
# 当前实现：主播在客户端选「LL-HLS 备份」即可，无需后端 override。
```

## 回滚后清理

- 灰度回到 0% 后保留 24h；观察 webrtc 失败率指标恢复后再爬坡到下个阶梯。
- 若回滚是因 webrtc-rs SRTP bug，记录在 `docs/dev/06-deliverable.md` 续记录，并提 ADR-0024（如需）。

## 阶梯回滚预案表

| 当前 | 触发条件 | 目标 |
| --- | --- | --- |
| 100% | WHIP 失败率 > 2% / 1h | 50% |
| 50% | WHIP 失败率 > 3% / 30min | 20% |
| 20% | WHIP 失败率 > 5% / 15min | 5% |
| 5% | WHIP 失败率 > 10% / 10min | 0%（全关） |
