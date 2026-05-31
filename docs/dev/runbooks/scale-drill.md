# Scale Drill Runbook

> 故障演练与规模化门禁 — Phase 10 规模化材料归档入口。
>
> 与 `scripts/perf/ws-baseline-run.sh`、`scripts/perf/ws-baseline-report.md`、
> `deploy/observability/grafana/dashboards/yunmao-overview.json` 配套使用。

## 1. 演练总表

| 编号 | 演练 | 目标 | 执行频率 | 前置条件 |
|------|------|------|----------|----------|
| D-001 | Gateway 单实例宕机 | 验证 Redis fanout 降级 + 客户端自动重连 | 每月 | 3 实例 gateway + Redis |
| D-002 | Redis 断链 | 验证 gateway 降级到 LocalFanout | 每月 | Redis pub/sub 已启用 |
| D-003 | 5 万并发 WS 连接 | 验证单实例容量天花板 | 每季度 | `ws-baseline-run.sh` |
| D-004 | 10 万并发（双 bench） | 验证多实例水平扩展 | 每季度 | 5 实例 + 双 bench 机 |
| D-005 | 房间订阅洪峰 | 验证高写比房间的 fanout 吞吐 | 每季度 | 50+ 房间, 10k+ 连接 |
| D-006 | RTMP 推流中断恢复 | 验证 ingest 重连与 LL-HLS 重同步 | 每月 | RTMP source + media-edge |
| D-007 | 支付 webhook 风暴 | 验证 billing-svc 在瞬时回调洪峰下的幂等 | 每季度 | staging 环境 |

## 2. D-001: Gateway 单实例宕机

### 2.1 场景
3 个 gateway 实例通过 Redis fanout 互联。杀掉其中 1 个实例，验证：
- 客户端感知（WebSocket 断开 → 自动重连到其他实例）
- Redis pub/sub 状态（channel 清理）
- 存活实例的 `connections_open` 上升（接管约 1/3 连接）

### 2.2 执行步骤

```bash
# Step 1: 启动 3 实例（参见 ws-baseline-up.sh）
bash scripts/perf/ws-baseline-up.sh

# Step 2: 启动 bench 产生持续连接
YUNMAO_BENCH_CONNS=3000 YUNMAO_BENCH_ROOMS=20 bash scripts/perf/ws-baseline-run.sh &
BENCH_PID=$!

# Step 3: 等待 15s 让连接稳定
sleep 15

# Step 4: 抓取各实例连接数基线
for p in 18091 18092 18093; do
  echo "gw-$p before: $(curl -fsS http://127.0.0.1:$p/metrics | grep gateway_connections_open)"
done

# Step 5: kill 第三个实例 (18093)
# 找到对应 PID 并 kill
ps aux | grep yunmao-gateway | grep 18093 | awk '{print $2}' | xargs kill

# Step 6: 观察存活实例变化
sleep 10
for p in 18091 18092; do
  echo "gw-$p after: $(curl -fsS http://127.0.0.1:$p/metrics | grep gateway_connections_open)"
done

# Step 7: 重启 18093
# (手动或重启脚本)
```

### 2.3 预期结果

| 指标 | 预期 |
|------|------|
| 存活实例 connections_open | 增长约 50%（接管已断连） |
| 客户端 P95 publish latency | < 200ms（含重连开销） |
| 客户端重连率 | ≥ 80%（iOS/Android 启用 WSClient auto-reconnect） |
| Redis pub/sub pending messages | 恢复后归零 |

### 2.4 验收标准

- [ ] 存活实例 `connections_open` 在 10s 内上升
- [ ] bench 报告的 `failed` 连接 < 5%
- [ ] 重启实例后，连接逐步回流
- [ ] Grafana `yunmao-overview` 看板无异常尖峰持续 > 30s

---

## 3. D-002: Redis 断链

### 3.1 场景
Redis pub/sub 已启用时，Redis 服务断开。验证 gateway 降级到 `LocalFanout`。

### 3.2 执行步骤

```bash
# 确保 gateway 配置了 redis_url
# 断开 Redis
docker stop yunmao-redis

# 观察 gateway 日志
# 预期出现: WARN fanout degraded to local

# 在 Redis 恢复后验证 fanout 恢复
docker start yunmao-redis
```

### 3.3 预期结果

| 指标 | 预期 |
|------|------|
| 单实例内广播 | 正常（LocalFanout 工作） |
| 跨实例广播 | 停止（降级后不跨实例） |
| 客户端无感知 | ✅ 单 room 单实例场景无影响 |
| 恢复时间 | Redis 重启后 fanout 自动重连 < 5s |

### 3.4 验收标准

- [ ] gateway 日志出现 `fanout degraded to local`
- [ ] `gateway_connections_open` 保持稳定
- [ ] Redis 恢复后跨实例广播恢复

---

## 4. D-005: 房间订阅洪峰

### 4.1 场景
10,000 连接分布在 50 个房间，对每个房间连续发送 20 条消息，验证 fanout 吞吐。

### 4.2 执行方式

**代码级验证**（已在 `rust/crates/yunmao-gateway/src/hub.rs` 的
`capacity_10k_register_and_fanout` 测试中固化）：

```bash
cd rust && cargo test -p yunmao-gateway capacity_10k -- --nocapture
```

**脚本验证**（在真实进程上跑）：

```bash
# 启动 gateway
bash scripts/perf/ws-baseline-up.sh

# 执行基线压测
YUNMAO_BENCH_CONNS=10000 YUNMAO_BENCH_ROOMS=50 \
  YUNMAO_BENCH_DURATION_SECS=30 bash scripts/perf/ws-baseline-run.sh
```

### 4.3 capacity_10k 基准数据（debug profile, macOS M-series, iteration 341）

```
=== Hub Capacity Benchmark ===
connections:       10000
rooms:             50
msgs_per_room:     20
total_delivered:   200000
register_elapsed:  30.278ms
register_throughput: 330273 conn/s
subscribe_elapsed: 17.333ms
fanout_elapsed:    77.534ms
fanout_throughput: 2579489 msg/s
==============================
```

| 指标 | 基准值 | 生产阈值 |
|------|--------|----------|
| 注册吞吐 | 330k conn/s | ≥ 10k conn/s |
| 扇出吞吐 | 2.58M msg/s | ≥ 50k msg/s |
| 10k 注册总耗时 | ~30ms | < 1s |
| 扇出 200k 消息 | ~77ms | < 5s |

### 4.4 验收标准

- [ ] `capacity_10k_register_and_fanout` 测试 PASS
- [ ] 扇出吞吐 ≥ 50k msg/s（assert 在测试中固化）
- [ ] 注册吞吐 ≥ 10k conn/s（assert 在测试中固化）
- [ ] Grafana `yunmao-overview` 看板在压测期间可观测到连接数和 publish/sub 吞吐变化

---

## 5. D-003 / D-004: 5 万 / 10 万并发连接

参见 [`scripts/perf/ws-baseline-report.md`](../../../scripts/perf/ws-baseline-report.md) 中的
§3.2 / §3.3。关键前置条件：

| 项 | 5 万 | 10 万 |
|----|------|-------|
| Gateway 实例 | 3 × c6i.2xlarge | 5 × c6i.2xlarge |
| Redis | r6g.large | r6g.xlarge |
| Bench 机器 | 1 × c6i.4xlarge | 2 × c6i.4xlarge |
| 内核 | somaxconn=8192, tw_reuse=1 | 同左 |
| ulimit | 500000 | 500000 |

**阻塞**：当前 staging 环境未配备对应 AWS 实例；正式基线必须在 Linux 容器复跑。

---

## 6. D-006: RTMP 推流中断恢复

### 6.1 场景
模拟 RTMP 推流端断线 10s 后重连，验证：
- media-edge 的 ingest router 正确清理旧 publisher
- 客户端（LL-HLS / HTTP-FLV / WHEP）能自动重拉
- QoE session 报告 stall 计数 ≤ 2

### 6.2 执行步骤

```bash
# 使用 OBS 或 ffmpeg 推流
ffmpeg -re -i test.flv -c copy -f flv rtmp://localhost:1935/live/room_drill

# 推流 10s 后 kill ffmpeg
sleep 10 && kill %1

# 观察 metrics
curl -fsS http://localhost:8080/metrics | grep media_edge_flv_publishers
# 预期: 0

# 重新推流
ffmpeg -re -i test.flv -c copy -f flv rtmp://localhost:1935/live/room_drill

# 观察 metrics
curl -fsS http://localhost:8080/metrics | grep media_edge_flv_publishers
# 预期: 1
```

### 6.3 验收标准

- [ ] RTMP 断线后 `media_edge_flv_publishers` 归零 < 30s
- [ ] 重连后 FLV / LL-HLS 在 5s 内恢复
- [ ] QoE session 中 `stall_count` ≤ 2

---

## 7. D-007: 支付 webhook 风暴

### 7.1 场景
短时间内向 billing-svc 发送 1000 次相同 webhook 回调，验证：
- 幂等性：同一 `transaction_id` 不产生重复订单
- 对账 worker：reconcile 后 diff = 0

### 7.2 执行方式

参考 `billing-staging-checklist.md` § 7 中的 `TestMockChannelWebhookHappyAndReplay` 与
`TestFullPaymentChain_E2E`（已包含 replay 幂等验证）。

### 7.3 验收标准

- [ ] 1000 次重复 webhook 不产生重复 order
- [ ] ReconcileWorker 后 `total_diffs = 0`
- [ ] billing-svc `TestFullPaymentChain_E2E` 持续 PASS

---

## 8. 灰度门禁

| 阶段 | 灰度比例 | 检查门 | 通过条件 |
|------|----------|--------|----------|
| 0% | 内部测试 | D-001 ~ D-007 全绿 | 演练报告归档 |
| 10% | Canary 1 个 region | Grafana 看板无红 | P99 latency < 500ms |
| 50% | 半区放量 | 客户端 crash rate < 0.1% | reconnect 率 > 95% |
| 100% | 全量 | 7 天稳定性监控 | 无 P0 incident |

灰度命中通过 `GrayHit` (iOS/Android) 控制，与后端 featureflags 的 FNV-1a Hash100 保持一致。

---

## 9. 多区域归档结构

```
reports/iteration_<N>_evidence/
├── scale_drill_D001_<date>.log      # Gateway 宕机
├── scale_drill_D002_<date>.log      # Redis 断链
├── scale_drill_D005_<date>.log      # 洪峰（capacity_10k）
├── scale_drill_D006_<date>.log      # RTMP 中断
├── ws-baseline-<date>.log           # 5w/10w 基线
└── dashboard_screenshot_<date>.png  # Grafana 快照
```

所有演练日志归档到 `reports/` 对应迭代下，Dashboard 截图与演练结论一起作为审计证据。

---

## 10. 持续演练节奏

| 频率 | 演练 |
|------|------|
| 每次部署前 | D-001（自动） |
| 每月 | D-002, D-006, D-007 |
| 每季度 | D-003, D-004, D-005 |
| 每次大版本 | 全部 D-001 ~ D-007 |

演练记录由运维负责人归档到 `reports/` 并在 iteration work report 中引用。
