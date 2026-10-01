# people/basic

查询人员；部门名来自 department/tree，不在这里另存一份。

## 在项目里使用

```bash
brickkit add people/basic@1.0.0
brickkit up
```

先做准备：见 BRICKKIT.md 的"部署前准备"（要先建好它和 department/tree 各自的 PostgreSQL 数据库）。

## 文档

| 想知道 | 读 |
| --- | --- |
| 负责什么、不负责什么，怎么配置，要准备什么 | [BRICKKIT.md](BRICKKIT.md) |
| 接口与事件 | [proto/people/v1/people.proto](proto/people/v1/people.proto)、[openapi.json](openapi.json)；事件见 [BRICKKIT.md](BRICKKIT.md) 的契约索引 |
| 依赖什么（BRICKKIT.md 里有说明） | [component.yaml](component.yaml) |
| 怎么开发 | [AGENTS.md](AGENTS.md) |

## 开发

测试跑在容器里，在本目录：

```bash
docker build --target test -t people-basic-test . && docker run --rm people-basic-test
```

迁移集成测试、重新导出 `openapi.json`、构建镜像，见 [AGENTS.md](AGENTS.md)。
