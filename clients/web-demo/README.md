# yunmao web-demo（DEPRECATED）

> 本目录已于第八轮被 `clients/web/`（Next.js 15 正式工程）取代。
> 仅保留作为最小回归 demo，不再接受新功能；如需迁移指引，见
> `docs/dev/clients/web-migration.md` 与 `clients/web/README.md`。

最小回归 Demo：把 yunmao 服务端（media-edge / gateway / user-svc / room-svc / feeding-svc）
所提供的能力串成一个浏览器页面，用于本地回归。

## 能做什么

- 调 `user-svc` 登录，拿 login JWT。
- 调 `room-svc` 签发短期房间订阅 token。
- 用 WebSocket 连 `gateway`：发送 `Auth` 帧 + `Subscribe` 帧。
- 用 `flv.js` 播放 `media-edge` 的 HTTP-FLV。
- 调 `feeding-svc` 投喂，观察 WS 事件流（`feed.command.requested` →
  `feed.command.acked` → `feed.command.completed`）。

## 启动方式

任选其一：

```bash
# 1. Python 内置
python3 -m http.server 5173 --directory clients/web-demo
# 2. 或顶层 Makefile
make web-demo
```

然后浏览器打开 <http://localhost:5173/>。

## 前置条件

需要 yunmao 后端跑起来（推荐 `make app-up`）：

| 服务 | 端口 |
| --- | --- |
| media-edge HTTP-FLV | 8080 |
| gateway WS | 8090 |
| user-svc | 8101 |
| room-svc | 8102 |
| feeding-svc | 8201 |

并且推流端通过 RTMP 推 `rtmp://localhost:1935/live/room_demo`。
没有真实摄像头时可用 `make poc-feed` 或 ffmpeg 推一个 test pattern。

## 注意

- 这是开发回归工具，不做安全加固。生产环境的鉴权页面请走 user-svc / room-svc
  的正式 SDK。
- flv.js 走 CDN；离线开发可把 `flv.min.js` 放到 `vendor/` 自取。
