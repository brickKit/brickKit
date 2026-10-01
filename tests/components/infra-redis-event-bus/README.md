# infra/redis-event-bus

基于 Redis Streams 的事件总线：组件把事件发到这里，消费方按需读取。

## 在项目里使用

```bash
brickkit add infra/redis-event-bus@1.0.0
brickkit up
```

它需要一个现成的 Redis，上线前要准备什么见 [BRICKKIT.md](BRICKKIT.md) 的"部署前准备"。

## 文档

| 想知道 | 读 |
| --- | --- |
| 负责什么、怎么配、要准备什么 | [BRICKKIT.md](BRICKKIT.md) |
| 接口与事件 | [openapi.json](openapi.json) |
| 依赖什么（说明在 BRICKKIT.md） | [component.yaml](component.yaml) |
| 怎么开发 | [AGENTS.md](AGENTS.md) |

## 开发

测试在容器里跑：

```bash
docker build --target test -t event-bus-test . && docker run --rm event-bus-test
```

真 Redis 契约测试、代码地图、设计取舍与易错点见 [AGENTS.md](AGENTS.md)。
