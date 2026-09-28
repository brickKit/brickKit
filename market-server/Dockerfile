# BrickKit Market API 镜像。
#
# 多阶段构建：编译期用 golang，运行期只留一个静态二进制。
# 构建上下文是**仓库根目录**：市场用主模块的 internal/ 包校验 Manifest
# （market-server/go.mod 里 replace 指向 ../），所以两个模块都要进上下文：
#
#   docker build -f market-server/Dockerfile -t brickkit/market-server:1.0.0 .
#   （或 make docker-market）
#
# 上下文里只放构建用得到的东西，见 market-server/Dockerfile.dockerignore。
#
# 版本号通过 --build-arg VERSION=... 注入，健康检查与启动日志都会回显它，
# 用来确认线上跑的到底是哪个镜像。

FROM golang:1.22-alpine AS build
WORKDIR /src

# 先只拷两个模块的依赖清单，让依赖层能被缓存
COPY go.mod go.sum ./
COPY market-server/go.mod market-server/go.sum ./market-server/
RUN cd market-server && go mod download

COPY internal/ ./internal/
COPY market-server/ ./market-server/

ARG VERSION=dev
RUN cd market-server && CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/market-server ./cmd/server

FROM alpine:3.20
# wget 供 compose 的 healthcheck 用；ca-certificates 供访问 https 对象存储用
RUN apk add --no-cache ca-certificates wget && adduser -D -u 10001 brickkit

COPY --from=build /out/market-server /app/market-server

# 容器不以 root 运行
USER 10001
EXPOSE 8080

# 市场自身不写本地磁盘：数据在 PostgreSQL，产物在对象存储。
# 因此这个镜像可以随便重建、随便横向扩容。
ENTRYPOINT ["/app/market-server"]
