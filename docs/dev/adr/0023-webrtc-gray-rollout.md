# ADR-0023: WebRTC 房间灰度策略与回滚条件

> 时间：2026-05-25  
> 状态：Accepted（第七轮上线灰度，初始 5%）  
> 关联：ADR-0016 / ADR-0018 / ADR-0020

## 背景

LL-HLS 是默认协议（延迟 1.5–3 秒）；WebRTC 真链路通过 webrtc-rs（ADR-0020）落地，目标延迟 ≤ 500ms。
由于：

- WebRTC 真链路 DTLS/SRTP 复杂度高，少量 CDN/NAT/防火墙场景仍可能失败；
- TURN 部署 / 带宽预算还在校准（ADR-0017 KMS / coturn 单实例形态）；
- 客户端兼容差异（webview / iOS / Android）需要小流量真实暴露。

为此设计单一开关 + 房间分片的灰度策略。

## 决策

### 开关

- 房间级开关：`room.webrtc.enabled`（bool）：
  - **disabled** → 所有房间走 LL-HLS（无视 gray_percent）；
  - **enabled** → 按 gray_percent 分桶。
- 分桶比例：`room.webrtc.enabled.value.gray_percent`（int 0–100）：
  - 上线阶梯：**5 → 20 → 50 → 100**（每档观察 ≥ 24 小时）；
  - 命中条件：`FNV1a64(room_id) % 100 < gray_percent`。

### 主播 override

- 房间元数据可显式写 `protocol_pref="webrtc"`，强制走 WebRTC；
- override 优先级最高（即使 enabled=false 仍走 WebRTC，便于灰度回滚后单房间复现问题）。

### 客户端选择逻辑

`room-svc GetRoom` 返回 `protocol_pref` + `gray_hit_webrtc` + `gray_hit_webrtc_percent`：

```jsonc
{
  "id": "room_demo",
  "protocol_pref": "webrtc",      // 客户端按此选 WHEP 或 LL-HLS
  "gray_hit_webrtc": true,
  "gray_hit_webrtc_percent": 20    // 当前灰度池
}
```

### 灰度池可视化

- admin 后台 `GET /v1/admin/webrtc/gray-sim?room_count=10000` 用 1 万个虚拟 `room_id` 模拟命中分布；
  返回 `{enabled, samples, hit_webrtc, hit_pct, configured_gray_pct}`。
- 与 `prometheus` 指标 `yunmao_webrtc_session_started_total{protocol="webrtc"}` 比对，
  线下灰度命中率 vs 线上实际接入率应 ±2%。

### 回滚条件（自动 + 人工）

| 触发 | 动作 |
|---|---|
| ICE 失败率 > 5%（5min 滑窗） | 灰度回退一档（100→50→20→5→0），打 PagerDuty |
| 平均时延 P95 > 1500ms 持续 10min | 同上 |
| WHEP 5xx 突增（10x 基线 5min） | 同上 |
| 业务侧主动 → admin `PUT /v1/admin/feature-flags/room.webrtc.enabled` 设 enabled=false | 立即关停 |

## 实现

### 代码改动

- `pkg/yunmao/featureflags`：新增 `IsRoomInGrayPercent` + `Hash100`（FNV-1a 64 → mod 100）。
- `services/room-svc/internal/service`：`ResolveProtocolPref` + `SimulateGrayDistribution`；
  `Get` 返回 protocol_pref / gray_hit_webrtc / gray_hit_webrtc_percent。
- `services/admin-svc`：默认 flag `room.webrtc.enabled` enabled=false / gray_percent=5；
  暴露 `GET /v1/admin/webrtc/gray-sim?room_count=N` 模拟命中。
- web-demo 后续读取 `protocol_pref` 自动切 WHEP / LL-HLS（已在 web-demo demo.js 里 placeholder）。

### 上线步骤

1. 部署所有服务（HS256 已删 + TURN credential 就绪）；
2. flag enabled=false, gray_percent=5 落地；observability 完成；
3. flag enabled=true → 灰度 5% 24h 观察；
4. 满足回滚 SLO → 20% / 50% / 100%；任何指标击穿立刻回退。

## 风险

1. FNV-1a 64 % 100 在小样本上分布偏差可见（5% 池在 100 room 上可能命中 2–8 个）；正式上线池规模 ≥ 1k；
2. 主播 override 与灰度池可能冲突（主播 override 优先生效，灰度池统计需要从指标里剔除 override 命中）；
3. 房间 ID 哈希加盐：目前直接哈希 `room_id`，无盐 → 任何人可以离线算出哪些房间会被灰度。
   后续可以加 `salt = flag.value.salt`，并随阶梯切换刷新盐。

## 回滚演练

```bash
# 全量回退到 LL-HLS
curl -X PUT http://admin-svc:8104/v1/admin/feature-flags/room.webrtc.enabled \
  -d '{"name":"room.webrtc.enabled","enabled":false,"scope":"global","value":{"gray_percent":0}}'
```

回退后 `GetRoom.protocol_pref` 立即变回 `ll-hls`（Manager 缓存 10s 内收敛）；现网 WHEP 会话因
`OnConnectionStateChange=disconnected` 而由客户端切回 LL-HLS。
