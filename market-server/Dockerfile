# BrickKit Market API 镜像。
#
# 多阶段构建：编译期用 golang，运行期只留一个静态二进制。
# 构建上下文是 market-server/ 目录本身：
#
#   docker build -t brickkit/market-server:1.0.0 market-server/
#
# 版本号通过 --build-arg VERSION=... 注入，健康检查会回显它，
# 用来确认线上跑的到底是哪个镜像（运维指南 §7.1 升级流程依赖这一点）。

FROM golang:1.22-alpine AS build
WORKDIR /src

# 先只拷依赖清单，让依赖层能被缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/market-server ./cmd/server

FROM alpine:3.20
# wget 供 compose 的 healthcheck 用；ca-certificates 供访问 https 对象存储用
RUN apk add --no-cache ca-certificates wget && adduser -D -u 10001 brickkit

COPY --from=build /out/market-server /app/market-server

# 008 §4：容器不得以 root 运行
USER 10001
EXPOSE 8080

# 市场自身不写本地磁盘：数据在 PostgreSQL，产物在对象存储。
# 因此这个镜像可以随便重建、随便横向扩容。
ENTRYPOINT ["/app/market-server"]
