# erp/backend

Order queries and approvals; authentication, authorisation and people data come from three upstream components, and approval events go to the event bus

## 在项目里使用

```bash
brickkit add erp/backend@1.0.0
brickkit up
```

上线前要准备什么，见 [BRICKKIT.md](BRICKKIT.md) 的"部署前准备"。

## 文档

| 想知道 | 读 |
| --- | --- |
| 负责什么、怎么配、要准备什么 | [BRICKKIT.md](BRICKKIT.md) |
| 接口与事件 | [openapi.json](openapi.json) |
| 依赖什么（说明在 BRICKKIT.md） | [component.yaml](component.yaml) |
| 怎么开发 | [AGENTS.md](AGENTS.md) |

## 开发

```bash
go vet ./... && go test ./...   # 不需要任何外部服务
docker build -t brickkit-demo/erp-backend:1.0.0 .
```

代码地图、设计取舍与易错点见 [AGENTS.md](AGENTS.md)。
