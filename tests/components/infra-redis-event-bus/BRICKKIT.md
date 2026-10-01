# infra/redis-event-bus

## 组件定位

基于 Redis Streams 的事件总线：组件把事件发到这里，消费方按需读回最近的事件。它常被别的组件声明为弱依赖（例如 `erp/backend`、`people/basic`），没装它时那些组件照常工作、只是不发事件。

**负责**

- 收下事件并追加到一条 Redis Stream 上（`202`），收不下时如实报 `503`，绝不假装收下
- 按时间倒序读出最近的事件，可按事件类型过滤
- 给事件分配 ID（Stream 条目 ID），发布方没带时间时补上收到的时间
- 按 `STREAM_MAXLEN` 裁剪事件流，不让它无限增长

**不负责（归谁）**

- Redis 本身的部署、持久化与备份：使用者（运维）
- 理解事件内容、校验业务字段：发布方与消费方；总线只要求有 `type`，其余字段原样存取
- 推送给订阅者、消费确认、重投：消费方自己轮询 `GET /api/v1/events`；本版本没有订阅或消费组接口
- 发布失败后的重试：发布方（收到 `503` 时由它决定重试还是放弃）

## 部署前准备

平台不会替你创建 Redis：要先有一个本组件的容器连得到的 Redis，并在 `config/` 里写上它的地址。Redis 在这里是**唯一的数据源**，不是加速器——没配 `REDIS_HOST` 本组件启动即失败。

Redis 跑在哪都行，只要本组件的容器连得到。用 Docker 起一个的话，再把它接进项目的容器网络 `brickkit-<项目名>-net`（`<项目名>` 是 `brickkit.yaml` 里的 `project`，这个网络在第一次 `brickkit up` 时创建），组件就能按容器名连到它：

```bash
docker run -d --name my-redis redis:7
docker network connect brickkit-<项目名>-net my-redis   # 第一次 brickkit up 之后执行一次
```

确认就绪：`docker exec my-redis redis-cli ping` 输出 `PONG`（有口令时加 `-a <口令>`）。本组件启动时**不**连 Redis，所以 Redis 晚一点起来也没关系，在它就绪之前收到的事件会得到 `503`。

## 依赖说明

不依赖任何组件。它只需要一个 Redis，见"部署前准备"。

## 配置指南

`REDIS_HOST` 必填，缺了组件启动即失败，**不会**退化到 localhost——悄悄连到 localhost 会让人以为配好了，实际连的根本不是那个 Redis。

- `REDIS_HOST`：容器网络里 Redis 的服务名（例如 `my-redis`），不是宿主机上看到的 `localhost`。
- `REDIS_PASSWORD`：Redis 没设口令就不写；有口令时写成 `${VAR}` 或 `file://` 引用，不要明文写进 `config/`。日志里口令一律打码。
- `STREAM_NAME`：事件流在 Redis 里的键名。几个项目共用一台 Redis 时各用各的名字，否则会读到别人的事件；改名之后旧流里的事件不再能从本组件读到（它们还留在 Redis 里）。
- `STREAM_MAXLEN`：流里大约保留多少条最近的事件，必须是正整数。裁剪是近似的（Redis 的 `~`），实际长度会略多于这个数；调大能回看更久，代价是 Redis 占用更多内存。
- `LOG_LEVEL`：不认识的值按 `info` 处理。

`brickkit add` 会生成 `config/infra-redis-event-bus.yaml` 的骨架，填好后大致是：

```yaml
REDIS_HOST: my-redis            # 容器网络内的服务名
REDIS_PASSWORD: ${REDIS_PASSWORD}
# REDIS_PORT 不写就是 6379
```

## 契约索引

- `openapi.json`：HTTP 接口。
  - `POST /api/v1/events`：发布事件。请求体必须有非空的 `type`，`actor`、`subject`、`time` 可选，其余字段原样存下；成功返回 **202** `{id, type}`——202 表示已收下，消费是异步的。发布方自己填的 `id` 会被忽略，由 Stream 生成；没带 `time` 时补上收到的时间（UTC，ISO 8601）。Redis 不可用时返回 503，错误信息不带底层原因。
  - `GET /api/v1/events?limit=&type=`：读最近的事件，**最新的在前**，`{events, total}`，没有事件时 `events` 是 `[]`。`limit` 取 1 到 500，默认 50。带 `type` 过滤时只在最近的 `min(limit×10, 1000)` 条里筛，所以匹配的事件比较稀疏时可能返回少于 `limit` 条。
  - `GET /healthz`：只检查本进程存活，不 ping Redis。
- 读回来的字段值都是字符串：发布时不是字符串的值（数字、对象、数组）以 JSON 文本存下，读回来仍是那段 JSON 文本，由消费方自己解析。
- 调用示例：

  ```bash
  curl -s -X POST "$INFRA_REDIS_EVENT_BUS_ENDPOINT/api/v1/events" \
    -H 'Content-Type: application/json' \
    -d '{"type":"erp.order.approved","actor":"p-001","subject":"o-1"}'
  curl -s "$INFRA_REDIS_EVENT_BUS_ENDPOINT/api/v1/events?type=erp.order.approved&limit=10"
  ```

- 发布的事件：无（本组件只转存别人的事件）。
- 消费的事件：任何类型都收；已知的发布方有 `erp/backend` 的 `erp.order.approved`（`subject` 是订单 ID）和 `people/basic` 的 `people.person.viewed`（`subject` 是人员 ID）。

## 外壳声明

不是外壳。
