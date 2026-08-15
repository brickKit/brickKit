# authorization/rbac 组件镜像。
#
# 多阶段构建：编译期用 golang，运行期只留一个静态二进制。
# 迁移与服务共用同一个二进制（migration.command 指向它的 migrate 子命令），
# 这样迁移脚本与业务代码永远是同一个版本。
FROM golang:1.22-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/authorization-rbac .

FROM alpine:3.20
RUN adduser -D -u 10001 brickkit
COPY --from=build /out/authorization-rbac /app/authorization-rbac

# 002 §1.4：容器不得以 root 运行
USER 10001
EXPOSE 8080
ENTRYPOINT ["/app/authorization-rbac"]
