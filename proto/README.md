# yunmao 共享契约

本目录是 monorepo 中的"contracts 仓"占位（参见 `08-多端工程组织与协作.md` 第 3 节与 `docs/dev/adr/0001-monorepo-bootstrap.md`）。

## 目录

| 目录 | 用途 |
| --- | --- |
| `cloudevents/` | CloudEvents 1.0 信封 + 业务事件 `data` 字段的 JSON Schema |
| `errors/` | 错误码字典（与 `04-设备接入数据模型与API边界.md` 11 节一致） |
| `feeding/` | 投喂相关内部 gRPC + REST 描述（`.proto`，未编译） |
| `device/` | 设备控制 gRPC（`.proto`） |
| `user/` `room/` | 用户、房间 gRPC（`.proto`） |
| `gateway/` | realtime-gateway 与客户端的 JSON 信令文档 |

## 生成

`scripts/gen-proto.sh` 是占位脚本：当前仓库不强依赖 `protoc`，Rust 与 Go 代码用手写的等价 `serde` / Go struct 实现，等团队接入 `buf` 后再切到自动生成（与 08 章 7.1 一致）。
