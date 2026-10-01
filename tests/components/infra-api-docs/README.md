# infra/api-docs

把每个组件的 API 文档收拢到一个入口；没装或没有文档的组件会被如实标出，页面照常打开。

## 在项目里使用

```bash
brickkit add infra/api-docs@1.0.0
brickkit up
```

上线前要准备什么，见 [BRICKKIT.md](BRICKKIT.md) 的"部署前准备"。

## 文档

| 想知道 | 读 |
| --- | --- |
| 负责什么、怎么配、要准备什么 | [BRICKKIT.md](BRICKKIT.md) |
| 接口与事件（本组件不发布契约文件，接口列在"契约索引"里） | [BRICKKIT.md](BRICKKIT.md) |
| 依赖什么（说明在 BRICKKIT.md） | [component.yaml](component.yaml) |
| 怎么开发 | [AGENTS.md](AGENTS.md) |

## 开发

```bash
go vet ./... && go test ./...   # 不需要任何外部服务
docker build -t brickkit-demo/infra-api-docs:1.0.0 .
```

代码地图、设计取舍与易错点见 [AGENTS.md](AGENTS.md)。
