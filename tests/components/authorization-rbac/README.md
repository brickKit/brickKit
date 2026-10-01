# authorization/rbac

Answers "may this person do this"; permissions merge a person path and a department path, and results are cached in Redis

## 在项目里使用

```bash
brickkit add authorization/rbac@1.0.0
brickkit up
```

先做准备：见 BRICKKIT.md 的"部署前准备"（要先建好它的 PostgreSQL 数据库；Redis 可选）。

## 文档

| 想知道 | 读 |
| --- | --- |
| 负责什么、不负责什么，怎么配置，要准备什么 | [BRICKKIT.md](BRICKKIT.md) |
| 接口与事件 | [proto/authorization/v1/authorization.proto](proto/authorization/v1/authorization.proto)、[openapi.json](openapi.json) |
| 依赖什么（BRICKKIT.md 里有说明） | [component.yaml](component.yaml) |
| 怎么开发 | [AGENTS.md](AGENTS.md) |

## 开发

```bash
go vet ./... && go test ./...    # 单元测试，不需要任何外部服务
```

含 PostgreSQL 与 Redis 的契约测试、改 proto 后重新生成代码、构建镜像，见 [AGENTS.md](AGENTS.md)。
