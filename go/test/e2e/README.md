# yunmao 集成测试（testcontainers-go）

这个模块用 [testcontainers-go](https://golang.testcontainers.org/) 起 PG / Redis /
Kafka / EMQX，在容器里跑投喂全链路 smoke：

```
login (user-svc HTTP) → 房间订阅 token (room-svc HTTP) → 创建房间 →
  绑定设备 → POST /feed-requests → outbox-relay → Kafka feed.command.dispatched
  → device-svc MQTT bridge → EMQX cmd → cat-feeder-sim 模拟 ack →
  device-svc → Kafka feed.command.acked → feeding-svc HandleAck →
  状态机变到 completed → gateway WS 推送
```

## 运行

需要本机 / CI 容器内可以 docker run（在 macOS / Linux CI Runner 上 OK，
**isolated_autoruns 容器内默认无 docker-in-docker，所以这个 job 不能在沙箱里跑**）：

```bash
cd go/test/e2e
go test -tags=integration ./... -v -timeout=15m
```

或者从仓库根：

```bash
make integration
```

## 跳过策略

- 没有 docker socket / 没有 `INTEGRATION=1` env：会通过 `t.Skip` 自动跳过；
- testcontainers-go 5min 拉镜像超时：失败标记 flaky，不影响其他单测；
- in-process 启动 user-svc / room-svc / feeding-svc / device-svc 失败：失败信息带容器日志。

## 包含的容器

| 容器 | 镜像 | 用途 |
| --- | --- | --- |
| postgres | `postgres:15` | PG，跑 0001-0004 迁移 |
| redis | `redis:7-alpine` | feeding-svc 幂等缓存 |
| kafka | `confluentinc/cp-kafka:7.6.1` (KRaft) | 事件总线 |
| emqx | `emqx/emqx:5.5.1` | 设备 MQTT |

## TODO

- 在 GitHub Actions `go.yml` 添加单独 job：`runs-on: ubuntu-latest` + docker service。
- 把 sim ack 改成多设备并发，断言 P95 时延。
- 把流媒体 ingest 集成（lavfi+ffmpeg push RTMP）放下一轮。
