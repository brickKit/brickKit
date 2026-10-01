# portal/user-frontend

A static frontend reaching erp/backend through an nginx reverse proxy; the backend address is injected by the platform

## 在项目里使用

```bash
brickkit add portal/user-frontend@1.0.0
brickkit up
```

先做准备：见 [BRICKKIT.md](BRICKKIT.md) 的"部署前准备"（要从浏览器访问，部署文件里得开 `expose`）。

## 文档

| 想知道 | 读 |
|---|---|
| 负责什么、不负责什么，怎么配置，要准备什么 | [BRICKKIT.md](BRICKKIT.md) |
| 接口与事件（没有契约文件：浏览器能访问什么、`/api/` 转发到哪，写在契约索引里） | [BRICKKIT.md](BRICKKIT.md) |
| 依赖什么（BRICKKIT.md 里有说明） | [component.yaml](component.yaml) |
| 怎么开发 | [AGENTS.md](AGENTS.md) |

## 开发

```bash
go vet ./... && go test ./...    # 有 Docker 时会用真 nginx 跑一遍 nginx -t，没有时跳过这几条
docker build -t brickkit-demo/portal-user-frontend:1.0.0 .
```

改 nginx 配置、入口钩子或 Dockerfile 之前读 [AGENTS.md](AGENTS.md)：为什么配置要这样写、哪些改动会让容器起不来，都在那里。
