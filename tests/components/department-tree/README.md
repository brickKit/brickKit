# department/tree

查询组织架构（部门树）；HTTP 与 gRPC 共用一个端口。

## 在项目里使用

```bash
brickkit add department/tree@1.0.0
brickkit up
```

先做准备：见 BRICKKIT.md 的"部署前准备"（要先建好它的 PostgreSQL 数据库）。

## 文档

| 想知道 | 读 |
| --- | --- |
| 负责什么、不负责什么，怎么配置，要准备什么 | [BRICKKIT.md](BRICKKIT.md) |
| 接口与事件 | [proto/department/v1/department.proto](proto/department/v1/department.proto)、[openapi.json](openapi.json) |
| 依赖什么（BRICKKIT.md 里有说明） | [component.yaml](component.yaml) |
| 怎么开发 | [AGENTS.md](AGENTS.md) |

## 开发

```bash
go vet ./... && go test ./...    # 单元测试，不需要数据库
```

含 PostgreSQL 的集成测试、改 proto 后重新生成代码、构建镜像，见 [AGENTS.md](AGENTS.md)。
