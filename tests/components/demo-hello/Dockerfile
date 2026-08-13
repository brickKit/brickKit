# 多阶段构建：编译期用 golang，运行期只留一个静态二进制
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hello .

FROM alpine:3.20
RUN adduser -D -u 10001 brickkit
COPY --from=build /out/hello /app/hello
# 002 §1.4 / 开发计划 21.10：容器不得以 root 运行
USER 10001
EXPOSE 8080
ENTRYPOINT ["/app/hello"]
