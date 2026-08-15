# erp/backend 组件镜像。
#
# 多阶段构建：编译期用 golang，运行期只留一个静态二进制。
# 没有 migrate 子命令——本组件不绑定资源，没有自己的表。
FROM golang:1.22-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/erp-backend .

FROM alpine:3.20
RUN adduser -D -u 10001 brickkit
COPY --from=build /out/erp-backend /app/erp-backend

# 002 §1.4：容器不得以 root 运行
USER 10001
EXPOSE 8080
ENTRYPOINT ["/app/erp-backend"]
