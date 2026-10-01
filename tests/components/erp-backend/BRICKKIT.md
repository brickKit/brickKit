# erp/backend

## 组件定位

ERP 的订单查询与审批接口。它是一个连接组件：自己几乎没有数据，价值全在把认证、授权、人员三个上游组件正确地串起来，审批后再把事件交给事件总线。

**负责**

- 订单列表（附上订单所有者的姓名与部门）与订单审批
- 把登录请求转交给认证组件，并带回本组件的会话时长
- 把"没登录 / 没权限 / 下游故障"分成 401 / 403 / 503 三种答复
- 审批成功后发一条 `erp.order.approved` 事件（事件总线在时）

**不负责（归谁）**

- 口令与令牌：`auth/password-login`，本组件不碰口令、没有凭据表
- 谁有哪些权限：`authorization/rbac`，本组件只引用权限名
- 人员姓名与部门：`people/basic`，本组件每次现取、不存副本
- 事件的存储与消费：`infra/redis-event-bus` 与读事件的那些组件
- 订单的持久化：本版本的订单是内置在内存里的样例数据（`o-1`、`o-2`、`o-3`），重启后审批状态回到 `pending`；要真实订单数据的话这件事归使用者另行建设

## 部署前准备

除下面的配置外无需准备：本组件不需要数据库、缓存或任何外部系统，三个上游组件的地址都由平台注入。

有一件事要在上游组件里就绪：调用者要在 `authorization/rbac` 里拿到 `erp.order.read`（看订单）与 `erp.order.approve`（审批）这两个权限，否则所有订单请求都是 403。`authorization/rbac` 自带的样例授权里，角色 `r-viewer` 有 `erp.order.read`，角色 `r-manager` 两个都有。检查方法：用 `authorization/rbac` 的 `POST /api/v1/check` 查某个人的这两个权限，`allowed` 为 `true` 即就绪。

## 依赖说明

一次订单查询要走三个强依赖，审批再加一个弱依赖：

```text
GET /api/v1/orders
  ├─ auth/password-login   （HTTP）这个令牌是谁的
  ├─ authorization/rbac    （gRPC）这个人能不能看订单
  └─ people/basic          （gRPC，额外端口 9090）补全姓名与部门

POST /api/v1/orders/{id}/approve
  ├─ 上面三步（权限换成 erp.order.approve）
  └─ infra/redis-event-bus （弱依赖）发一条 erp.order.approved
```

- `auth/password-login`（强依赖）：校验 `Authorization: Bearer <token>` 的令牌是谁的，并承接 `POST /api/v1/login`。它返回 401 时本组件报 401；它不可达时报 503。
- `authorization/rbac`（强依赖）：用 gRPC 问"这个人有没有某个权限"。它说没有这个人时按"没权限"处理（403），不可达时报 503。
- `people/basic`（强依赖）：用 gRPC 取订单所有者的姓名与部门。走的是它声明的额外端口（9090），所以本组件读的是 `PEOPLE_BASIC_GRPC_ENDPOINT`，而不是 `PEOPLE_BASIC_ENDPOINT`。某个所有者在人员系统里查不到时，那张订单照常返回、姓名留空；`people/basic` 不可达时整个列表报 503。
- `infra/redis-event-bus`（弱依赖，`optional: true`）：审批成功后发事件。没装它时平台不注入 `INFRA_REDIS_EVENT_BUS_ENDPOINT`，本组件启动时记一条警告"事件总线未启用"；装了但调用失败时也一样处理。两种情况下审批都照常返回 200、订单状态确实变成 `approved`，只是响应里 `eventPublished` 为 `false`，如实说明事件没发出去。

三个强依赖的地址缺任何一个，本组件启动时一次列出全部缺项并退出。

## 配置指南

三个强依赖的地址（`AUTH_PASSWORD_LOGIN_ENDPOINT`、`AUTHORIZATION_RBAC_ENDPOINT`、`PEOPLE_BASIC_GRPC_ENDPOINT`）和弱依赖的 `INFRA_REDIS_EVENT_BUS_ENDPOINT` 都由平台按依赖声明注入，不要在 `config/` 里手写。

- `SESSION_TTL_SECONDS`：登录接口返回给前端的 `sessionTtlSeconds`，前端据此决定会话多久后要求重新登录。它是本组件的会话策略，与 `auth/password-login` 自己签发的令牌有效期无关；把它设得比令牌有效期长，会话还没到期请求就会开始收到 401。必须是正整数，否则启动失败。
- `LOG_LEVEL`：排查上下游调用时调到 `debug`；不认识的值按 `info` 处理。日志里口令、令牌、密钥一类字段一律打码。

## 契约索引

- `openapi.json`：HTTP REST 接口，运行时也在 `GET /openapi.json` 上提供同一份（升级后以运行时那份为准）。
  - `POST /api/v1/login`：`{username, password}` → `{token, personId, username, sessionTtlSeconds}`
  - `GET /api/v1/orders`：需要 `erp.order.read`，返回 `{orders, total, requestBy}`，每张订单带 `ownerName` 与 `ownerDepartment`
  - `POST /api/v1/orders/{id}/approve`：需要 `erp.order.approve`，返回 `{orderId, status, approvedBy, eventPublished}`
  - `GET /healthz`：只检查本进程存活，不探测任何依赖
- 状态码：401 = 不知道你是谁（没令牌或令牌无效，去重新登录）；403 = 知道你是谁但没这个权限（重新登录没用，去授权）；503 = 某个强依赖暂时不可用（稍后重试），错误信息不带下游的底层原因。
- 调用示例：

  ```bash
  curl -s -X POST "$ERP_BACKEND_ENDPOINT/api/v1/login" \
    -H 'Content-Type: application/json' -d '{"username":"...","password":"..."}'
  curl -s "$ERP_BACKEND_ENDPOINT/api/v1/orders" -H "Authorization: Bearer $TOKEN"
  curl -s -X POST "$ERP_BACKEND_ENDPOINT/api/v1/orders/o-1/approve" -H "Authorization: Bearer $TOKEN"
  ```

- 发布的事件：`erp.order.approved`，经 `infra/redis-event-bus` 的 `POST /api/v1/events` 发出，内容 `{type, actor, subject, time}`——`actor` 是审批人的 personId，`subject` 是订单 ID，`time` 为 UTC。
- 消费的事件：无。

## 外壳声明

不是外壳。
