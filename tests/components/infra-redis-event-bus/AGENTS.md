# infra/redis-event-bus

基于 Redis Streams 的事件总线（Python，FastAPI + uvicorn）。它是 BrickKit 平台自测组件之一，常被别的组件声明为弱依赖；与 `authorization/rbac` 形成刻意对照：那里 Redis 是可有可无的加速器，这里 Redis 是唯一的数据源。

## 代码地图

| 路径 | 管什么 |
| --- | --- |
| `app/main.py` | 入口：拒绝任何命令行参数、读配置、建 Redis 客户端（3 秒连接与读写超时）、启动 uvicorn；启动时不 ping Redis |
| `app/config.py` | 环境变量 → `Config`（`REDIS_HOST` 必填、数字项校验）、JSON 日志与敏感字段打码 |
| `app/http_api.py` | HTTP 接口：健康检查、发布（202）、读取（倒序、`limit` 上限 500）、统一的 503 |
| `app/store.py` | `EventStore`：`XADD` 带 `maxlen` 近似裁剪、`XREVRANGE` 读取与按类型过滤、字段编解码 |
| `tests/test_component.py` | 配置、日志、参数、`component.yaml` 与实现一致、契约文件、Dockerfile（非 root、带 curl）、无硬编码地址 |
| `tests/test_service.py` | HTTP 行为：202、倒序、过滤、补时间、额外字段、健康检查不碰 Redis、Redis 挂了报 503 |
| `tests/test_store_contract.py` | 存储层行为契约：fakeredis 与真 Redis 跑同一份用例 |
| `openapi.json` | HTTP 契约（随镜像一起拷入） |
| `requirements.txt` | 运行期依赖，版本钉死 |
| `requirements-dev.txt` | 测试依赖（pytest、httpx、PyYAML、fakeredis），只进测试层 |
| `Dockerfile` | `base` → `test`（跑 pytest）→ `runtime`（装 curl、UID 10001） |
| `component.yaml` | 组件契约：配置项（`REDIS_HOST` 必填）、部署与健康检查 |

| 功能 | 从这里开始 | 然后 |
| --- | --- | --- |
| 改发布或读取接口 | `app/http_api.py` | `openapi.json`，`tests/test_service.py`，`tests/test_component.py` 的契约覆盖用例 |
| 改 Redis 读写或裁剪 | `app/store.py` | `tests/test_store_contract.py`（用真 Redis 跑一遍） |
| 加一个配置项 | `component.yaml` | `app/config.py` 的 `config_from_env`，`tests/test_component.py` |
| 改 Redis 连接参数 | `app/main.py` | `app/config.py` |
| 改镜像 | `Dockerfile` | `tests/test_component.py` 的 Dockerfile 用例 |

## 构建与测试

宿主机上没有可用的 Python 包管理环境，测试在容器里跑（`--target test` 那一层带上 pytest）：

```bash
# 只用 fakeredis，真 Redis 那组会跳过
docker build --target test -t event-bus-test . && docker run --rm event-bus-test

# 连同真 Redis 契约测试（有口令时再加 -e EVENT_BUS_TEST_REDIS_PASSWORD=...）
docker run --rm -e EVENT_BUS_TEST_REDIS_ADDR="<redis-ip>:6379" event-bus-test
```

成功的样子：只用 fakeredis 时最后一行是 `41 passed, 9 skipped`（跳过的 9 个是真 Redis 那组），设了 `EVENT_BUS_TEST_REDIS_ADDR` 时没有 skipped。仓库根目录的 `make test-components` 会跑前一种，`make test-components-integration` 会对名为 `my-redis` 的容器跑后一种。

构建与单独运行（Redis 要在同一个容器网络里）：

```bash
docker build -t brickkit-demo/infra-redis-event-bus:1.0.0 .   # 镜像名与 component.yaml 的 deployment.image 一致
docker run --rm -p 8080:8080 --network <Redis 所在的网络> -e REDIS_HOST=<Redis 容器名> \
  brickkit-demo/infra-redis-event-bus:1.0.0
curl -s localhost:8080/healthz   # → {"status":"ok"}
```

成功时 stdout 有一条 JSON 日志 `组件已就绪`，带着 Redis 地址、流名与 `maxlen`（不带口令）。

## 设计取舍

- **Redis 是唯一的数据源，缺了就启动失败**：一个连不上存储的事件总线，起来了也只会把每一条事件都丢掉。与 `authorization/rbac` 正好相反——那里 Redis 是加速器，不配也能跑、挂了照常回源查库；同一种外部系统，两种截然不同的故障处理，这是本组件要验证的东西之一。
- **收不下就 503，绝不假装成功**：假装收下等于把事件悄悄丢掉，而发布方会以为它已经安全落地、再也不会重发。
- **Stream 而不是 Pub/Sub**：Pub/Sub 发出去就没了，没有消费者在线时消息直接丢弃，也无法回看；Stream 把事件留在流里，消费方可以晚一点来读，排障时还能翻最近发生了什么。
- **`maxlen` + approximate 裁剪**：事件流会一直长，不裁剪迟早把 Redis 撑爆；approximate（Redis 的 `~`）按整节点裁剪，快得多，代价只是实际长度略多于 `maxlen`。
- **契约由调用方先定**：`erp/backend` 早就在往 `POST /api/v1/events` 发事件了，这里照着实现，没有反过来要求已经上线的调用方改。
- **202 而不是 200**：事件已收下，但消费是异步的；200 会让发布方以为已经被处理了。
- **额外字段原样存下**：事件总线不理解事件内容（平台不解析业务语义），丢掉不认识的字段等于逼所有发布方都来改这个组件。
- **发布方没带时间就补上**：宁可用收到的时间，也不要一条没有时间的事件——排障时时间往往是唯一能把几个组件的日志对起来的东西。
- **发布方填的 `id` 被忽略**：否则两条事件可能撞 ID，按 ID 去找会找出错的那条。
- **健康检查与启动都不 ping Redis**：Redis 一抖就把所有副本杀掉重启，而重启并不会让 Redis 变好，只会让恢复之后还要多等一轮拉起；Redis 还没起来时本组件也应该能先启动，等真正收到事件时再报 503。
- **测试在容器里跑**：宿主机没有可用的 Python 包管理环境；版本固定、可复现，不依赖开发机装了什么。
- **存储层是行为契约测试**：只测 fakeredis 的话，`XADD` 的 `maxlen` 写错、`xrevrange` 的顺序理解反了，单测照样全绿，而事件总线出错的表现往往是"事件偶尔丢了"，最难查。

## 易错点

| 不许 | 症状 | 原因 |
| --- | --- | --- |
| 写入 Redis 失败时返回 2xx 或吞掉异常 | 事件悄悄丢失，发布方不再重发 | Redis 是唯一的数据源，收不下就必须如实报 503 |
| 给 `REDIS_HOST` 加 localhost 之类的默认值 | 看起来配好了，实际连的根本不是那个 Redis；`test_config_never_falls_back_to_localhost` 失败 | 缺了必须启动失败，让人当场看到 |
| 在 `/healthz` 或启动时 ping Redis | Redis 一抖，所有副本被反复杀掉重启；Redis 晚起时本组件起不来 | 重启不会让 Redis 变好；可用性问题由业务接口的 503 表达 |
| 从运行期镜像里去掉 curl | 组件明明正常，平台却判它 unhealthy，依赖方永远等不到它 | 健康检查跑在容器内部，`python:slim` 既没有 wget 也没有 curl |
| 去掉 `XADD` 的 `maxlen` 或改成精确裁剪 | 前者让流无限增长直到撑爆 Redis；后者每次写入都更慢 | 近似裁剪是有意的取舍：快得多，代价只是实际长度略多于 `maxlen` |
| 去掉 `limit` 的上限 500 | 一个 `limit=1000000` 的请求把内存吃干净 | 上限是防护，不是性能参数 |
| 让发布方的 `id` 覆盖 Stream 生成的 ID | 两条事件撞 ID，按 ID 找出错的那条 | `id` 在 `RESERVED_FIELDS` 里，由 Stream 生成 |
| 在 503 的响应里带 Redis 的底层错误 | 调用方看到 `connection refused` 一类信息，内部拓扑外泄 | 原因帮不上调用方，又把内网结构告诉了外面 |
| 只用 fakeredis 跑存储层的改动 | 单测全绿，上了真 Redis 才发现参数或顺序错了 | fakeredis 不保证与真 Redis 行为一致，改 `app/store.py` 要设 `EVENT_BUS_TEST_REDIS_ADDR` 跑一遍 |
| 默认以为读回来的非字符串字段还是原类型 | 消费方拿到的是 JSON 文本而不是数字或对象 | `_encode` 把非字符串值 JSON 序列化后存进 Stream，`_decode` 原样返回字符串 |

## 改代码前自查

1. Redis 不可用时，发布与读取是否仍然返回 503，且不带底层原因？
2. `/healthz` 与启动流程是否仍然不碰 Redis？
3. 改了 `app/store.py` 是否用真 Redis（`EVENT_BUS_TEST_REDIS_ADDR`）跑过契约测试？
4. 改了接口是否同步了 `openapi.json`，且 `erp/backend` 现有的 `POST /api/v1/events` 调用方式仍然可用？
5. 新的配置项是否同时写进了 `component.yaml` 的 `configSchema` 与 `app/config.py`？
6. 运行期镜像是否仍然以 UID 10001 运行、仍然带 curl？
7. 容器里的测试是否全部通过？

<!-- brickkit:managed:begin lang=zh -->
<!-- 由 brickkit 维护（init、add、remove、upgrade、skills update）：这对标记之间的改动会被覆盖 -->

## BrickKit

这是一个 BrickKit 组件：平台只读 `component.yaml`。它依赖的规则：

- `configSchema` 的键就是代码读的环境变量名。不能用保留名：`COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`，以及任何 `*_ENDPOINT`。
- 依赖写精确版本。依赖的地址以 `<ID>_ENDPOINT` 注入；缺席的可选依赖根本没有这个变量，读的时候要带兜底。
- `/healthz` 只查本进程，不查依赖。迁移命令用同一个镜像跑，遇到不认识的参数必须直接失败。
- `BRICKKIT.md` 会随版本进入每个使用它的项目，在那里是脱离仓库单独读的：跟代码一起改，不放相对链接。
- 发版：改 `metadata.version`，提交、推送，`brickkit release`。`brickkit lint` 会检查清单和这些文档。
- 完整规则在 `brickkit-component` 技能里（装了技能的项目或仓库根目录下的 `.claude/skills/brickkit-component/SKILL.md`；`brickkit skills update` 会装上）；参数问 `brickkit <命令> --help`。
<!-- brickkit:managed:end -->
