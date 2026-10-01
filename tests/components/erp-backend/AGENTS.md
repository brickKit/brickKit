# erp/backend

ERP 订单查询与审批的连接组件（Go）：自己几乎没有数据，把 `auth/password-login`、`authorization/rbac`、`people/basic` 三个强依赖与弱依赖 `infra/redis-event-bus` 串起来。它也是 BrickKit 的平台自测组件，专门验证弱依赖降级、`extraPorts` 注入、"只依赖组件、不需要外部系统"这三条路径。

## 代码地图

| 路径 | 管什么 |
| --- | --- |
| `main.go` | 入口：读配置、建三个下游客户端与事件总线出口、起 HTTP 服务；拒绝任何命令行参数 |
| `config.go` | 环境变量 → `config`（强依赖地址缺失时一次全报）、JSON 日志与敏感字段打码 |
| `service.go` | HTTP 路由与业务：登录转交、订单列表与补全、审批、401/403/503 的划分、内嵌并提供 `openapi.json` |
| `clients.go` | 三个下游客户端：auth/password-login（HTTP）、authorization/rbac 与 people/basic（gRPC），`grpcTarget()` 与超时 |
| `eventbus.go` | 弱依赖出口：`disabledEventBus`（没注入地址时）与 `httpEventBus`，事件类型 `erp.order.approved` |
| `orders.go` | 内存里的样例订单与 `Orders` 接口 |
| `openapi.json` | HTTP 契约，同时在编译时用 go:embed 打进二进制 |
| `proto/` | 从上游复制来的 `.proto` 契约：`proto/people/v1/`、`proto/authorization/v1/` |
| `gen/` | 由 `proto/` 生成的 gRPC 客户端代码，不手改 |
| `component_test.go` | 配置、日志、`grpcTarget()`、`component.yaml` 与实现一致、Dockerfile 非 root、无硬编码地址 |
| `service_test.go` | 业务路径：四个依赖的替身、状态码、弱依赖降级、健康检查 |
| `openapi_endpoint_test.go` | `/openapi.json` 运行时端点，以及内嵌的那份与文件一致 |
| `Dockerfile` | 两阶段构建：golang 编译静态二进制 → alpine 运行期，UID 10001 |
| `component.yaml` | 组件契约：依赖、配置项、部署与健康检查 |

| 功能 | 从这里开始 | 然后 |
| --- | --- | --- |
| 加一个配置项 | `component.yaml` | `config.go` 的 `configFromEnv`，再在 `component_test.go` 补用例 |
| 改订单接口 | `service.go` | `openapi.json`（`service.go` 内嵌它），`service_test.go` |
| 改下游调用或超时 | `clients.go` | `service_test.go` 里对应的替身 |
| 改事件内容或弱依赖行为 | `eventbus.go` | `service.go` 的 `publishApproved`，`service_test.go` 的事件用例 |
| 上游改了 `.proto` | `proto/people/v1/people.proto`、`proto/authorization/v1/authorization.proto` | 仓库根目录 `make proto-erp` 重新生成 `gen/` |
| 改订单数据 | `orders.go` | `service_test.go` |

## 构建与测试

```bash
go vet ./... && go test ./...   # 不需要任何外部服务：四个依赖都有替身
```

成功的样子：`ok  github.com/brickkit/components/erp-backend`，`gen/...` 两个包显示 `[no test files]`。

```bash
docker build -t brickkit-demo/erp-backend:1.0.0 .   # 镜像名与 component.yaml 的 deployment.image 一致
```

单独跑起来（不需要上游已经在线，gRPC 连接到真正调用时才建立）：

```bash
AUTH_PASSWORD_LOGIN_ENDPOINT=http://<auth 地址>:8080 \
AUTHORIZATION_RBAC_ENDPOINT=http://<rbac 地址>:8080 \
PEOPLE_BASIC_GRPC_ENDPOINT=http://<people 地址>:9090 \
go run .
curl -s localhost:8080/healthz   # → {"status":"ok"}
```

成功时 stdout 有一条 JSON 日志 `组件已就绪`；没设 `INFRA_REDIS_EVENT_BUS_ENDPOINT` 时先有一条 `事件总线未启用` 的警告，这是正常的。

上游改了 `.proto` 之后，在仓库根目录跑 `make proto-erp` 重新生成 `gen/`。仓库根目录的 `make test-components` 会连同其他自测组件一起跑本组件的 `go vet` 与 `go test`。

## 设计取舍

- **不声明数据库**：连接组件不掌握主数据，订单是内置的样例数据。声明一个用不上的数据库会让使用者白建一个库、还得为它填口令。也因此没有 `migrate` 子命令、`component.yaml` 里没有 `migration`。
- **订单只存 `ownerId`**：姓名与部门每次向 `people/basic` 现取，否则人员改了名，这里就是一份永远对不上的旧数据；同一个人在一次列表里只查一次，免得 N 张订单变成 N 次 gRPC 调用。
- **`people/basic` 走 gRPC 额外端口**：它是 Python 组件，grpcio 不能与 HTTP 共用端口，所以它在 `extraPorts` 里声明了 9090，平台据此额外注入 `PEOPLE_BASIC_GRPC_ENDPOINT`。本组件只用这个变量，不用 `PEOPLE_BASIC_ENDPOINT`。
- **`grpc.NewClient` 而不是 `grpc.Dial`**：它不在启动时阻塞等连接，下游还没起来时本组件照样能先启动，等真正调用时再连。
- **事件总线是弱依赖，用空实现而不是 nil**：没注入地址时用 `disabledEventBus`，调用点不必到处判空。事件超时（2 秒）比强依赖超时（5 秒）短：事件是顺手发的，不该拖住一个已经成功的审批。
- **健康检查只看本进程**：依赖四个组件，逐个探的话任意一个抖动都会让这个本身正常的容器被杀掉重启；暂时干不了活由业务接口如实报 503。
- **运行时也提供 `/openapi.json`**：发布时的 `openapi.json` 是那一刻的快照，`infra/api-docs` 之类的工具要回答的是此刻跑着的服务长什么样。
- **`proto/` 是复制来的产物**：`people.proto` 的 `go_package` 是本组件加的——`people/basic` 是 Python 组件，它的契约里没有这一行，而生成选项本来就属于调用方。
- **只声明 `requests` 不声明 `limits`**：组件知道自己稳态占多少，允许它涨到多少是部署方的判断；这里写了 `limits`，使用方就删不掉了。

## 易错点

| 不许 | 症状 | 原因 |
| --- | --- | --- |
| 把 `INFRA_REDIS_EVENT_BUS_ENDPOINT` 加进 `configFromEnv` 的必需校验 | 没装事件总线的项目里本组件永远启动不了 | 它是弱依赖，缺席时平台完全不注入，缺席是正常状态 |
| 事件发不出去时让审批失败或回滚状态 | 事件总线一抖，审批就失败 | 那样弱依赖就成了事实上的强依赖；正确做法是照常 200、状态照改、`eventPublished: false` |
| 把平台注入的 `*_ENDPOINT` 直接交给 gRPC | 报 `dns resolver: missing address` 之类跟业务无关的错 | 注入的是带 scheme 的 URL，gRPC 要 `host:port`，必须经过 `grpcTarget()` |
| 用 `PEOPLE_BASIC_ENDPOINT` 连 `people/basic` 的 gRPC | gRPC 调用全部失败，列表 503 | 那是 HTTP 主端口 8080，gRPC 在额外端口 9090 |
| 把 401 和 403 混用 | 调用方被引导去反复重新登录，却始终没有权限 | 401 是"不知道你是谁"，403 是"知道你是谁但不能做"，处置完全不同 |
| 把 `authorization/rbac` 的 NotFound 当故障 | 一个不在 rbac 里的人拿到 503 而不是 403 | rbac 说没这个人就是没权限，是业务答案 |
| 在 503 的响应里带上下游的底层错误 | 调用方看到 `rpc error ... dial tcp`，内部拓扑外泄 | 原因帮不上调用方，又把内网结构告诉了外面 |
| 去掉下游调用的超时 | 一个下游卡住后连接被逐个耗尽，最后连 `/healthz` 都进不来 | 一个组件的慢会拖垮整条链 |
| 手改 `gen/` 下的代码 | 下次 `make proto-erp` 改动全部丢失 | 那是生成产物，源头在 `proto/` |
| 改了 `openapi.json` 却不重新构建镜像 | 容器里 `/openapi.json` 发出去的还是旧文档，文件本身早已改了 | 它是 `//go:embed` 在编译时读进二进制的，只有重新编译才更新 |
| 在 `main.go`、`service.go`、`clients.go`、`config.go`、`eventbus.go` 里写 `localhost:` 或 `http://people` 一类地址 | `TestNoHardcodedEndpoints` 失败；部署后连错地方 | 地址只能来自平台注入的环境变量 |

## 改代码前自查

1. 新的依赖调用是否区分了"业务答案"（令牌无效、没权限、查无此人）与"依赖故障"，分别落到 401/403/503？
2. 动到事件总线的代码后，事件总线缺席和调用失败两种情况下审批是否仍然 200、状态是否仍然变成 `approved`？
3. 新的 gRPC 地址是否经过 `grpcTarget()`？
4. 新的配置项是否同时写进了 `component.yaml` 的 `configSchema` 与 `configFromEnv`，且弱依赖相关的项没有进必需校验？
5. 改了接口是否同步了 `openapi.json`，并重新构建了镜像？
6. 健康检查是否仍然只回答本进程存活，不探测任何依赖？
7. `go vet ./... && go test ./...` 是否全绿？

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
