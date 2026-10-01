# demo/hello

BrickKit 自测用的最小 HTTP 组件：回一句问候语，并回显自己拿到的环境变量。

## 在项目里使用

```bash
brickkit add demo/hello@1.0.0
brickkit up
```

先做准备：见 [BRICKKIT.md](BRICKKIT.md) 的"部署前准备"。

## 文档

| 想知道 | 读 |
|---|---|
| 负责什么、不负责什么，怎么配置，要准备什么 | [BRICKKIT.md](BRICKKIT.md) |
| 接口与事件 | [openapi.json](openapi.json) |
| 依赖什么（BRICKKIT.md 里有说明） | [component.yaml](component.yaml) |
| 怎么开发 | [AGENTS.md](AGENTS.md) |

## 开发

```bash
go vet ./... && go test ./...
docker build -t brickkit-demo/hello:1.0.0 .
```

改代码之前读 [AGENTS.md](AGENTS.md)：代码地图、易错点和改代码前的自查都在那里。
