# infra/redis-event-bus

基于 Redis Streams 的事件总线。BrickKit Phase 5 的第七个测试组件。

## 它在平台里的角色很特殊

它是**很多组件的弱依赖** —— `people/basic` 与 `erp/backend` 都对它声明了
`optional: true`。这意味着它缺席时那些组件应当照常工作、只是不发事件（003 §4.3）。
弱依赖降级的另一半就在这里。

但它自己**不能假装成功**：收不下的事件必须如实报 503。假装收下等于把事件悄悄丢掉，
而发布方会以为它已经安全落地，再也不会重发。

## 与 authorization/rbac 的刻意对照

两个组件都绑 `cache` 资源，但 Redis 的角色完全相反：

| | authorization/rbac | 本组件 |
| --- | --- | --- |
| Redis 是什么 | **加速器** | **唯一的数据源** |
| 没绑定 cache 资源 | 照常启动，用进程内缓存 | **启动失败** |
| Redis 挂了 | 照常回源查库 | **503** |

同一种资源，两种截然不同的故障处理 —— 这正是本组件想验证的东西之一。

## 为什么是 Stream 而不是 Pub/Sub

Pub/Sub 是"发出去就没了"：没有消费者在线时消息直接丢弃，也无法回看。
Stream 会把事件留在流里，消费方可以晚一点来读、可以从某个 ID 之后接着读，
排障时还能直接翻最近发生了什么。

流用 `maxlen` + approximate 裁剪：事件流会一直长，不裁剪迟早把 Redis 撑爆。
approximate（Redis 的 `~`）让它按整节点裁剪，快得多，代价只是实际长度略多于 `maxlen`。

## API

| 端点 | 说明 |
| --- | --- |
| `POST /api/v1/events` | 发布事件 → **202**（已收下，但消费是异步的） |
| `GET /api/v1/events?limit=&type=` | 读最近的事件，**最新的在前** |
| `GET /healthz` | 只检查本进程存活 |

**契约由调用方先定下：** `erp/backend` 早在 Step 25 就在往 `POST /api/v1/events`
发事件了（见它的 `eventbus.go`）。这里照着实现，没有反过来要求已经上线的调用方改。

### 几个刻意的选择

- **202 而不是 200** —— 事件已收下，但消费是异步的。用 200 会让发布方以为"已经被处理了"。
- **额外字段原样存下** —— 事件总线不理解事件的内容（平台不解析业务语义）。
  丢掉不认识的字段等于逼所有发布方都来改这个组件。
- **发布方没带时间戳时由本组件补上** —— 宁可用"收到的时间"，也不要一条没有时间的事件：
  排障时时间往往是唯一能把几个组件的日志对起来的东西。
- **发布方自己填的 `id` 会被忽略** —— 否则两条事件可能撞 ID，按 ID 去找会找出错的那条。

## 健康检查不 ping Redis

Redis 在这里是唯一的数据源，比别处更容易让人想去探一探。但健康检查只回答
"本进程还活着吗"（002 §9.4）：去探的话，Redis 一抖，编排系统就把这些本身完全正常的
容器全杀掉重启 —— 而**重启并不会让 Redis 变好**，只会让恢复之后还要多等一轮拉起。

同理，启动时也**不 ping Redis**：Redis 还没起来时本组件照样应该能启动，
等真正收到事件时再报 503。

## 配置

| 环境变量 | 来源 | 必需 |
| --- | --- | --- |
| `REDIS_HOST` / `REDIS_PORT` / `REDIS_PASSWORD` | 平台按 cache 资源注入（006 §5.2） | ✅ |
| `STREAM_NAME` | `configSchema.streamName`，默认 `brickkit:events` | ❌ |
| `STREAM_MAXLEN` | `configSchema.streamMaxlen`，默认 10000 | ❌ |
| `LOG_LEVEL` | `configSchema.logLevel` | ❌ |

## 本地运行

宿主机没有可用的 Python 包管理环境，所以**测试也在容器里跑**：

```bash
# 只用 fakeredis（真 Redis 那组会跳过）
docker build --target test -t event-bus-test . && docker run --rm event-bus-test

# 含真 Redis 契约测试
docker run --rm -e EVENT_BUS_TEST_REDIS_ADDR="<redis-ip>:6379" event-bus-test
```

或用仓库根目录的 `make test-components` / `make test-components-integration`。

存储层是**行为契约测试**：fakeredis 与真 Redis 跑同一份用例。只测 fakeredis 的话，
`XADD` 的 maxlen 写错、`xrevrange` 的顺序理解反了，单测照样全绿 ——
而事件总线出错的表现往往是"事件偶尔丢了"，最难查。

## 设计依据

002 组件规范（§1.4 组件约束、§9.4 健康检查、§9.6 健康检查工具、§11 日志）、
003 §4.3（弱依赖）、006 §5.2（cache 资源的环境变量）。
