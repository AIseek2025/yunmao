# ADR-0011 LL-HLS 接口预留，本轮返回 501

- 状态：Accepted
- 日期：2026-05-24
- 关联：ADR-0002（MVP 直播协议 = RTMP+HTTP-FLV）

## 决策

`yunmao-media-edge` 新增模块 `mod ll_hls`，提供：

- `LlHlsPackager` trait 与 `LlHlsParams`（`target_duration=4s`、`part_target=1s`、
  `hold_back≈3*part_target`）；
- HTTP 路由 `/live/{room}/index_ll.m3u8`、`/live/{room}/segment-*.m4s` 返回
  `501 Not Implemented`，并埋点 `media_edge_ll_hls_manifest_requests_total` /
  `..._chunk_requests_total` 便于观察客户端意愿。

真实切片器（fMP4 + EXT-X-PART + Preload Hint）由下一轮接入。

## 理由

- 让前端 / SDK 团队可以提前对接 URL 结构、CDN 缓存策略与监控面板。
- 当真实切片器接入时，唯一变更是替换 trait 默认实现，对外路由不动。
- 不阻塞 MVP（HTTP-FLV 已能跑）。

## 替代方案

- **直接实现 LL-HLS**：fMP4 切片器 + 子段预热 + ABR 调度工程量大，与本轮其它
  硬核目标冲突。
- **跳过路由占位**：未来上线时需要全栈协作，会推迟实际价值。

## 重新评估条件

- iOS Safari / 微信浏览器 PoC 需要 LL-HLS 端到端验证。
- HTTP-FLV 在大规模带宽下成本不可控，需要 LL-HLS over CDN 替代。
