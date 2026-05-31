# 多阶段构建，用于 yunmao Go services。
#
# 用法：
#   docker build -f go/Dockerfile --build-arg SVC=user-svc -t yunmao-user-svc:dev go/
#
# 支持的 SVC：user-svc / room-svc / feeding-svc / device-svc / billing-svc / admin-svc

ARG GO_VERSION=1.23
FROM golang:${GO_VERSION}-bookworm AS builder
ARG SVC=user-svc
WORKDIR /workspace/go
COPY go/go.work ./go.work
COPY go/pkg ./pkg
COPY go/proto ./proto
COPY go/services ./services
COPY go/migrations ./migrations
COPY go/test ./test
COPY clients/cat-feeder-sim /workspace/clients/cat-feeder-sim
RUN cd services/${SVC} && go mod download
RUN cd services/${SVC} && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' \
      -o /out/${SVC} ./cmd/${SVC}

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
ARG SVC=user-svc
WORKDIR /app
COPY --from=builder /workspace/go/migrations /app/migrations
COPY --from=builder /out/${SVC} /app/svc
ENV YUNMAO_MIGRATIONS_DIR=/app/migrations
USER 1000:1000
ENTRYPOINT ["/app/svc"]
