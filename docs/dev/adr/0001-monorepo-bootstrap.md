# ADR-0001 monorepo bootstrap

- 状态：Accepted（开发期）
- 日期：2026-05-24
- 关联：08-多端工程组织与协作.md 第 2 节，07 决策记录章节

## 背景

`08` 的目标态是“多仓 + contracts 单仓”。但当前阶段：仓库尚未公开发布、单人/极小团队推进；同时需要 Rust/Go/proto/部署脚本快速联动。

## 决策

MVP 阶段在仓库内采用 **单仓多语言子目录** 形态：

- `rust/` 是单一 Cargo workspace。
- `go/` 是单一 Go workspace（`go.work`），多个 module。
- `proto/` 充当 contracts 仓在 monorepo 中的占位，未来可零成本拆出去。
- 所有跨语言依赖通过 `proto/`（schema、CloudEvents、错误码 yaml）共享，禁止跨语言子目录直接 import。

## 替代方案

1. 直接按 08 第 2 节拆 9 个仓 — 当前阶段过重，跨仓 PR 频繁会拖慢底层联调。
2. 单 Cargo + 单 Go module — 失去 Go workspace 的隔离性，所有 service 共用一份依赖图，不利于独立部署。

## 影响

- 后续拆仓时只需把 `rust/`/`go/`/`proto/` 整目录搬走，因此 import path 都加 `yunmao` 命名空间。
- `go.work` 与 `rust/Cargo.toml` 的工作区文件成为唯一编译入口，CI 与 `Makefile` 只 invoke 这两个根。
