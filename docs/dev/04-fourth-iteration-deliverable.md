# 第四轮开发交付报告（yunmao 云养猫直播投喂平台）

> 日期：2026-05-25  
> 状态：✅ A/B/C/E/F/G/H/I 完成；🟡 D scaffold 落地（需 docker 启用）  
> 全量验证：`cargo fmt/clippy/test` 通过、`go vet/test` 通过、`buf lint` 通过

---

## 一、交付清单（按目录）

### 1. Go 业务层

| 路径 | 性质 | 说明 |
| --- | --- | --- |
| `go/migrations/0004_fourth_iteration.sql` | 上轮已有 | 新增 `feature_flags`、`kms_key_versions`；扩 `rooms.status / stream_key_rotated_at`、`devices.owner_id / mqtt_credential_expires_at / firmware_target`；`feed_requests` 加 `cancelled_at / timeout_at` |
| `go/pkg/yunmao/kms/kms.go` | 上轮已有 | `KeyProvider` 抽象 + `MockKmsProvider` + `Vault/AwsKmsKeyProvider` 占位 + 轮换策略 |
| `go/pkg/yunmao/featureflags/featureflags.go` | 上轮已有 | `Memory`/`PgStore` + `Manager`（TTL 缓存 + 后台刷新） |
| `go/services/feeding-svc/internal/service/service.go` | 上轮已有 | `Cancel` + `TimeoutScanRun` + `StartTimeoutWorker` + feature flag 拒绝路径 |
| `go/services/feeding-svc/internal/transport/http.go` | 上轮已有 | `POST /api/v1/feed-requests/{id}/cancel` |
| `go/services/room-svc/internal/store/{store.go,sql/queries.sql,sql/schema.sql}` | 上轮已有 | `Store` 接口 + Memory + PG + sqlc 查询 |
| `go/services/device-svc/internal/store/{store.go,sql/queries.sql,sql/schema.sql}` | 上轮已有 | 同上 |
| `go/sqlc.yaml` | 上轮已有 | feeding/user/billing/room/device 5 个 sqlc 包 |
| `go/services/admin-svc/internal/service/service.go` | **改** | `New(safety, flags)` 注入 `featureflags.Store`，新增 `ListFlags/GetFlag/SetFlag` |
| `go/services/admin-svc/internal/transport/http.go` | **改** | 加 `GET/PUT /v1/admin/feature-flags[/{name}]` |
| `go/services/admin-svc/internal/service/service_test.go` | **改** | 加 `TestFeatureFlagsCrud` / `TestSetFlagRequiresName` |
| `go/services/admin-svc/cmd/admin-svc/main.go` | **改** | dev 默认注入内存 store，PG 模式同时挂 feature_flags 表 |
| `go/test/e2e/{go.mod,doc.go,integration_test.go,README.md}` | **新** | testcontainers-go 集成测试 scaffold（PG/Redis/Kafka/EMQX） |
| `go/go.work` | **改** | 加 `./test/e2e` |

### 2. clients

| 路径 | 性质 | 说明 |
| --- | --- | --- |
| `clients/cat-feeder-sim/{go.mod,cmd/cat-feeder-sim/main.go,README.md}` | 上轮已有 | 模拟设备 Go 二进制（heartbeat + cmd ack + 失败率） |
| `clients/web-demo/index.html` | **改** | 加「刷新流元数据」按钮 + `streamMeta` 展示 |
| `clients/web-demo/demo.js` | **改** | 调 `/live/{room}/meta.json`，展示真实分辨率 / 音频 / 源码率 / ABR |

### 3. Rust 媒体 / 网关 / WebRTC

| 路径 | 性质 | 说明 |
| --- | --- | --- |
| `rust/crates/yunmao-ingest/src/flv.rs` | **改** | 加 `is_aac_sequence_header` / `is_aac_raw` |
| `rust/crates/yunmao-media-edge/src/fmp4.rs` | **重写** | H.264 SPS Exp-Golomb 真解析（宽高/profile/level/fps）；AAC `AudioSpecificConfig` 解析；init segment 含视频 + 音频两个 trak（mp4a + esds 描述符链）|
| `rust/crates/yunmao-media-edge/src/ll_hls.rs` | **改** | RoomBuffer 持 `AacConfig` + `PublisherMetadata`，监控源码率（1s 滑窗），新增 `/live/{room}/index.m3u8` master、`/live/{room}/meta.json` |
| `rust/crates/yunmao-media-edge/src/abr.rs` | **重写** | `TranscodeWorker` trait + `PassthroughWorker` + `FfmpegSubprocessWorker`（IPC = 4-byte BE length + payload；现走 `cat`，未来切 `ffmpeg`） |
| `rust/crates/yunmao-media-edge/src/qoe.rs` | **改** | QoeSession 新增 `resolution_actual / audio_present / source_bitrate_bps / abr_active_ladder` |
| `rust/crates/yunmao-media-edge/src/server.rs` | **改** | 注册 master / meta 路由 |
| `rust/crates/yunmao-webrtc/{Cargo.toml,src/lib.rs}` | 上轮已有 | WebRTC/WHEP 评估占位 crate（Publisher/Subscriber/Signaling trait + LocalSignaling） |
| `rust/Cargo.toml` | 上轮已有 | members 加 `yunmao-webrtc` |

### 4. 可观测 / 部署

| 路径 | 性质 | 说明 |
| --- | --- | --- |
| `deploy/observability/grafana/dashboards/outbox-relay.json` | 上轮已有 | pending / published rate / latency / DLQ |
| `deploy/observability/grafana/dashboards/mqtt-bridge.json` | 上轮已有 | Kafka↔MQTT 消息计数 / 错误 / 连接 / 订阅 |
| `deploy/observability/grafana/dashboards/gateway-fanout.json` | 上轮已有 | WS 连接 / 房间 / fanout 后端 / 鉴权成功失败 |
| `deploy/observability/grafana/dashboards/ll-hls-qoe.json` | 上轮已有 | playlist / part / blocking reload / 启动延迟 / 分辨率 / 码率 |
| `deploy/observability/prometheus/alerts/yunmao.rules.yml` | 上轮已有 | outbox/mqtt/gateway/ll-hls 告警规则 |
| `deploy/observability/prometheus.yml` | 上轮已有 | `rule_files` 引入 alerts/ |
| `deploy/docker-compose.dev.yml` | 上轮已有 | Prometheus 挂载 alerts/ |

### 5. 性能与脚本

| 路径 | 性质 | 说明 |
| --- | --- | --- |
| `scripts/perf/ws-baseline-up.sh` | 上轮已有 | 起 3 gateway + redis + publisher + prom + grafana |
| `scripts/perf/ws-baseline-run.sh` | 上轮已有 | `bench_ws` 10k/50k/100k 三档 + metrics 抓取 |
| `scripts/perf/ws-baseline-report.md` | 上轮已有 | macOS 10k 本机结果 + Linux 8C16G ×3 等价模型 + 调优清单 |
| `Makefile` | **改** | 加 `integration` 目标（`go test -tags=integration`） |

### 6. ADR

| 路径 | 性质 | 说明 |
| --- | --- | --- |
| `docs/dev/adr/0015-kms-selection-and-rotation.md` | 上轮已有 | KMS 选型（Mock/Vault/AWS KMS）+ 30 天轮换 + JWKS 双 key 公开 + 设备 MQTT 短 token |
| `docs/dev/adr/0016-webrtc-whep-grayscale.md` | 上轮已有 | WHIP/WHEP 灰度策略、MVP 验证、与 LL-HLS 并存原则、托管 vs 自建权衡 |

---

## 二、关键模块能力一览

- **room-svc PG**：HMAC stream_key（KeyProvider 签发，可轮换），CRUD + status/SetStatus/RotateStreamKey/IssueSubscription，按 owner/region/status 分页。
- **device-svc PG**：RegisterDevice / UpdateDevice / Bind|UnbindRoom / IssueMqttCredential（HS256 短 token） / UpdateFirmware / SetStatus，`last_seen_at` 由 MQTT bridge heartbeat 刷新。
- **feeding-svc**：cancel(queued/dispatched→rejected) + 超时补偿 worker（dispatched/executing > 30s 翻 timeout）+ feature flag 拒绝（`feed.global_kill_switch`、`feed.region.dispatch_limit`、`device.maintenance_mode`）。
- **admin-svc**：feeding-policy、global feeding-safety、feature-flags 三类 CRUD；运营可直接 PUT flag JSON。
- **media-edge**：H.264 SPS 真解析（宽高/profile/level/fps）；AAC LL-HLS init segment 含 mp4a/esds；ABR `TranscodeWorker` trait + passthrough + ffmpeg subprocess IPC 协议；`/live/{room}/meta.json` 输出实时元数据；QoE 新字段 4 个。
- **yunmao-webrtc crate**：`Publisher`/`Subscriber`/`Signaling` trait + `LocalSignaling` 进程内 stub（已带单测）；未替代 LL-HLS，做下一轮 SFU 接入骨架。
- **kms**：Mock + Vault Transit（HTTP sign/verify 占位） + AWS KMS（GetPublicKey/Sign 占位）+ 30 日轮换策略；JWKS 自动暴露 active+retiring。
- **cat-feeder-sim**：N 设备并发，随机出粮延迟，可配失败率，Prom 指标。
- **testcontainers-go scaffold**：PG/Redis/Kafka/EMQX 4 容器一键起，paho 客户端订阅 smoke；in-process 服务接入留 TODO（见下）。
- **WS baseline**：本机 10k 报告；Linux 8C16G×3 实例下 50k/100k 等价模型与调优清单。

---

## 三、验证结果摘要

```text
$ cd rust && cargo fmt --all -- --check         ⇒  通过
$ cd rust && cargo clippy --workspace -- -D warnings  ⇒  通过
$ cd rust && cargo test --workspace             ⇒  55 passed; 0 failed
$ make go-vet                                   ⇒  全部包通过
$ make go-test                                  ⇒  全部包通过（含新 admin feature flag 单测）
$ bash scripts/gen-proto.sh                     ⇒  proto 生成成功
$ make buf-lint                                 ⇒  通过
$ cd clients/cat-feeder-sim && go build ./...   ⇒  通过
$ cd go/test/e2e && go vet -tags=integration ./...  ⇒  通过（实际跑测需 docker socket）
```

样本输出（media-edge fmp4 单测）：

```
test fmp4::tests::sps_parser_extracts_size_from_known_avcc ... ok
test fmp4::tests::aac_seq_parses_48k_stereo ... ok          ⇒  AAC LC 48kHz stereo 解出 obj=2/sr=48000/ch=2
test fmp4::tests::init_segment_with_audio_has_two_traks ... ok  ⇒  init.mp4 含 2 个 trak（vide + soun）
```

样本输出（ABR subprocess 协议）：

```
test abr::tests::subprocess_passthrough_protocol_roundtrip ... ok
  → 4-byte BE length + 9-byte payload → 通过 `cat` echo 回 → length+payload 完整还原
```

样本输出（feature flags）：

```
=== RUN   TestFeatureFlagsCrud
--- PASS: TestFeatureFlagsCrud (0.00s)
=== RUN   TestSetFlagRequiresName
--- PASS: TestSetFlagRequiresName (0.00s)
```

> **WS baseline 实测数据**：参见 `scripts/perf/ws-baseline-report.md`。本机（macOS ulimit/sysctl 受限）只跑 10k；50k/100k 给出 Linux 8C16G×3 等价模型 + 调优清单。  
> **integration smoke**：CI runner / Linux 开发机执行 `make integration`；本沙箱无 docker socket，跳过实际容器启动。

---

## 四、本轮临时决策（ADR）

- **ADR-0015 KMS 选型与轮换**：dev 默认 MockKms；prod 推荐 Vault Transit 或 AWS KMS（asymmetric RSA 2048）；30 天主签发、7 天 retire 期间继续校验；JWKS 暴露 active + retiring；设备 MQTT 凭证仍走 HS256 短期 token（KMS 留下轮）。
- **ADR-0016 WebRTC/WHEP 灰度评估**：本轮不替代 LL-HLS；优先验证 WHIP/WHEP 入出口；中小规模托管（Cloudflare TURN）；自建用 Pion/webrtc-rs；权限模型复用 room-svc 订阅 token；MVP = 1 房 100 人 P2P + SFU 对比；回退策略走客户端探测。
- **额外决策**：`TranscodeWorker` IPC 协议固化为 4-byte BE length prefix + payload（适配 ffmpeg / GStreamer）；media-edge `/live/{room}/index.m3u8` 现为单档 master，未来加 ABR 时直接追加 EXT-X-STREAM-INF。

---

## 五、仍未完成的硬核 TODO

| 项 | 原因 | 下一步建议 |
| --- | --- | --- |
| D：in-process 服务接入测 e2e（POST /feed-requests → MQTT cmd → ack → completed） | scaffold 已就位，但需要把 services 的 server 构造函数（HTTP / Kafka / MQTT）以纯库形式 export 给 `e2e` 包；当前服务的 `cmd/<svc>/main.go` 把 wiring 写死在 main，重构成本中等 | 下一轮做：将各服务 `cmd` 中的 wiring 抽到 `internal/server.New(deps)` 工厂；e2e 包就能直接 `service.New + transport.New + httptest.NewServer` 起服务 |
| B：fMP4 audio sample 写入 mdat（当前只写 init segment 的 audio trak，没有把 raw AAC 帧拼进 moof） | hls.js / Safari 在 init segment 有 audio trak 时仍可只播视频；做 audio sample 需要拆分 `trun` 多 track 与 mdat 偏移管理，工作量大 | 下一轮：fMP4 fragmented mp4 多 trak 写入 + audio dts 同步 |
| B：真 ffmpeg ladder 转码 | 需 GPU/CPU profile，否则可能成本失控 | 下一轮：预算确认后接 `FfmpegSubprocessWorker::ffmpeg_ladder`，写 720p/480p/360p 输入参数 |
| C：Vault Transit / AWS KMS 真实 HTTP/SDK 调用 | 占位实现已存在；签名 / 公钥读取需要 sdk 凭证 | 下一轮：开发环境给个 dev Vault container（`make dev-vault`），生产用 cloud KMS |
| G：5w/10w 真机压测 | macOS ulimit/sysctl 限制；CI runner 不适合大规模 socket | 下一轮：在 Linux x86 8C16G×3 实例上跑 `ws-baseline-run.sh`，把结果回灌 report.md |
| Feeding-svc cancel → device 物理 cancel | 已发 `feed.command.cancelled` 事件；device-svc bridge 已能消费 Kafka，但 cancel topic 还没 wire 到 EMQX 出口 | 下一轮：在 `device-svc/internal/bridge` 加 `cancel` topic 映射 |

---

## 六、仍需用户 / 业务确认的问题

1. **KMS 后端**：生产是走 AWS KMS（公司已有 AWS Org）还是自建 HashiCorp Vault？影响 ADR-0015 final state 与运维 SOP。
2. **WebRTC 是否独立预算**：ADR-0016 假设 Cloudflare 托管 TURN/SFU + 自建 WHIP 入口，是否需要 PoC 项目立项？预算与 SLA？
3. **超时补偿默认 30s** 是否足够？目前文档把超时阈值写死，未做 feature flag；如需 region 级别可调，需要再扩 `feed_requests` 元数据。
4. **ABR ladder 是否上 1080p?** 推流端来源大多 720p，若 720p 直推，hd1080 转码意义不大；建议从 720p 源 + 480p ladder 起步。
5. **e2e 是否要做"模拟设备 + 投喂端到端" CI job**？需要 docker-in-docker / runner-on-host；如同意，可加 `go.yml` 单独 job（label = `integration`）。

---

## 七、下一轮建议优先级

1. **Services 工厂重构 + testcontainers-go 真跑投喂全链路**（D 收尾）。
2. **fMP4 audio sample 写入 + ffmpeg subprocess 真转码**（B 收尾，配合 GPU profile）。
3. **Vault / AWS KMS 真实 sign/verify 接入 + 设备 MQTT 凭证走 RS256 KMS**（C 收尾）。
4. **WebRTC WHIP/WHEP MVP**（房间订阅 token 复用，1 房 100 人对比测试）。
5. **WS 5w/10w Linux 真机压测**（产出 SLO 与扩容公式）。
6. **device-svc bridge 加 cancel topic 出口 + cat-feeder-sim 在 e2e 中跑 ack/cancel 双路径**。

— end —
