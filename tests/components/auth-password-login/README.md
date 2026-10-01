# auth/password-login

校验用户名与口令并签发 JWT；身份来自 people/basic，不在这里另存一份。

## 在项目里使用

```bash
brickkit add auth/password-login@1.0.0
brickkit up
```

先做准备：见 BRICKKIT.md 的"部署前准备"（要先建好它的 PostgreSQL 数据库、准备签名密钥 `JWT_SECRET`，真实部署前删掉样例账号）。

## 文档

| 想知道 | 读 |
| --- | --- |
| 负责什么、不负责什么，怎么配置，要准备什么 | [BRICKKIT.md](BRICKKIT.md) |
| 接口与事件 | [openapi.json](openapi.json) |
| 依赖什么（BRICKKIT.md 里有说明） | [component.yaml](component.yaml) |
| 怎么开发 | [AGENTS.md](AGENTS.md) |

## 开发

```bash
go vet ./... && go test ./...    # 单元测试，不需要数据库
```

含 PostgreSQL 的契约与迁移测试、构建镜像，见 [AGENTS.md](AGENTS.md)。
