# 多阶段构建，统一为 yunmao Rust 二进制使用。
#
# 用法：
#   docker build -f rust/Dockerfile --build-arg BIN=yunmao-gateway -t yunmao-gateway:dev rust/
#
# 支持的 BIN：
#   - yunmao-media-edge
#   - yunmao-gateway
#   - yunmao-device-edge

ARG RUST_VERSION=1.94
FROM rust:${RUST_VERSION}-slim-bookworm AS builder
ARG BIN=yunmao-gateway
WORKDIR /app
# 先复制 manifest 缓存依赖
COPY Cargo.toml Cargo.lock ./
COPY crates ./crates
RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config libssl-dev ca-certificates && \
    rm -rf /var/lib/apt/lists/*
RUN cargo build --release --bin ${BIN}
RUN cp /app/target/release/${BIN} /app/yunmao-bin

FROM debian:bookworm-slim AS runtime
ARG BIN=yunmao-gateway
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    useradd -r -u 10001 yunmao
WORKDIR /app
COPY --from=builder /app/yunmao-bin /app/yunmao-bin
USER yunmao
ENV RUST_LOG=info
ENTRYPOINT ["/app/yunmao-bin"]
