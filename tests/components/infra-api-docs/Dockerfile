# infra/api-docs 组件镜像。
#
# 三段：Swagger UI 静态资源 → 编译 Go → 运行期。
#
# Swagger UI 从官方镜像里拷，**不从 CDN 加载**：008 §5 的默认不暴露原则意味着
# 这个页面很可能跑在内网甚至气隙环境里，指向公网 CDN 的 <script> 会让页面
# 永远转圈——而症状看起来像"文档组件坏了"。版本钉死才可复现。
FROM swaggerapi/swagger-ui:v5.17.14 AS swagger

FROM golang:1.22-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/infra-api-docs .

FROM alpine:3.20
RUN adduser -D -u 10001 brickkit

COPY --from=build /out/infra-api-docs /app/infra-api-docs
COPY web/ /app/web/
COPY --from=swagger /usr/share/nginx/html/swagger-ui.css \
                    /usr/share/nginx/html/swagger-ui-bundle.js \
                    /usr/share/nginx/html/swagger-ui-standalone-preset.js \
                    /app/web/swagger-ui/

# 002 §1.4：容器不得以 root 运行
USER 10001
EXPOSE 8080
ENTRYPOINT ["/app/infra-api-docs"]
