# erp/backend

**连接组件**：自己几乎没有数据，价值全在"把别的组件正确地串起来"。
BrickKit Phase 5 的第五个测试组件，也是整条依赖链的汇合点。

## 一次请求要走多少路

```
GET /api/v1/orders
  ├─ auth/password-login   （HTTP） 这个令牌是谁的
  ├─ authorization/rbac    （gRPC） 这个人能不能看订单
  └─ people/basic          （gRPC，extraPorts 9090）补全姓名与部门

POST /api/v1/orders/{id}/approve
  ├─ 上面三步
  └─ infra/redis-event-bus （弱依赖）发一条 erp.order.approved
```

## 它独有的三个验证点

| 验证点 | 为什么前面的组件覆盖不了 |
| --- | --- |
| **弱依赖降级** | 前四个组件都没有弱依赖 |
| **extraPorts 注入** | people/basic 的 gRPC 在 9090，平台额外注入 `PEOPLE_BASIC_GRPC_ENDPOINT` |
| **没有资源依赖** | 前四个都绑定了 database；只依赖组件的路径没走过 |

### 弱依赖降级是什么意思

事件总线**缺席**（平台完全不注入 `INFRA_REDIS_EVENT_BUS_ENDPOINT`）或**调用失败**时：

- 审批照常返回 200
- **订单状态确实变成 approved**（不是只返回 200 就完事）
- 响应里 `eventPublished: false`，如实说明事件没发出去

若因为发不出事件就让审批失败、或把状态回滚，弱依赖就成了事实上的强依赖 ——
而 003 §4.3 对弱依赖的定义是"有就用、没有就降级"。

### 为什么没有数据库

订单是内置的样例数据，放在内存里。这不是偷懒：**连接组件不掌握主数据**。
声明一个用不上的 `database` 资源会让使用者白建一个库、还得为它填口令。

同理，订单上只存 `ownerId`，姓名与部门每次向 people/basic 现取 ——
否则人员改了名，这里就是一份永远对不上的旧数据。

## 状态码

| 码 | 含义 |
| --- | --- |
| 401 | 我不知道你是谁（没令牌 / 令牌无效） |
| 403 | 我知道你是谁，但你不能做这个 |
| 503 | 某个强依赖暂时不可用 |

401 与 403 混在一起会让调用方去重新登录，而登录一百次也不会有权限。

## 配置

| 环境变量 | 来源 | 必需 |
| --- | --- | --- |
| `AUTH_PASSWORD_LOGIN_ENDPOINT` | 平台按强依赖注入 | ✅ |
| `AUTHORIZATION_RBAC_ENDPOINT` | 平台按强依赖注入 | ✅ |
| `PEOPLE_BASIC_GRPC_ENDPOINT` | 平台按 people/basic 的 **extraPorts** 注入 | ✅ |
| `INFRA_REDIS_EVENT_BUS_ENDPOINT` | 弱依赖，缺席时**完全不注入** | ❌ |
| `SESSION_TTL_SECONDS` | `configSchema.sessionTtlSeconds`，可在 brickkit.yaml 覆盖 | ❌ |
| `LOG_LEVEL` | `configSchema.logLevel` | ❌ |

弱依赖那一项**绝不能**出现在"缺少必需配置"的校验里 —— 那会让一个从没装过
事件总线的项目永远启动不了这个组件。

## 一个很容易漏的转换

平台注入的 `*_ENDPOINT` 是带 scheme 的 URL，而 gRPC 要的是 `host:port`。
漏了这一步，报出来的是 `dns resolver: missing address` 之类跟业务毫无关系的话。
`grpcTarget()` 负责这件事，有专门的测试。

另外用的是 `grpc.NewClient` 而不是 `grpc.Dial`：它不在启动时阻塞等连接，
下游还没起来时本组件照样能先启动，等真正调用时再连。

## 本地运行

```bash
go test ./...          # 不需要任何外部服务：四个依赖都有替身
```

改了上游 `.proto` 之后重新生成客户端：`make proto-erp`（在仓库根目录）。

> `proto/` 下的两份 `.proto` 是**从上游组件复制来的产物**（002 §7 契约即产物）。
> `people.proto` 的 `go_package` 是本组件加的 —— people/basic 是 Python 组件，
> 它的契约里没有这一行，而生成选项本来就属于调用方。

## 设计依据

002 组件规范（§1.4、§6 依赖、§7 契约即产物、§9.4 健康检查、§11 日志）、
003（§4.3 强弱依赖、§4.5 地址注入、§5.4 config 覆盖）、009（gRPC 客户端）。
