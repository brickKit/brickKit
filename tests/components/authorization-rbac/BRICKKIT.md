# authorization/rbac

回答"这个人能不能做这件事"；权限合并"直接授予这个人"与"授予他所在部门"两条路径，结果可缓存在 Redis 里。HTTP 与 gRPC 共用一个端口。

## 组件定位

基于角色的授权查询：给一个 personId，算出他的全部权限，或判断他有没有某一个权限。

```
权限 = 直接授予这个人的角色  ∪  授予这个人所在部门的角色
                                        ↑
                               部门取自 people/basic（强依赖）
```

**负责**

- 角色、角色包含的权限、把角色授予人或部门（三张表 `roles`、`role_permissions`、`role_grants`）
- 合并两条路径算出权限集合（排序、去重），以及单个权限的判断
- 权限缓存：配了 Redis 用 Redis，没配用进程内缓存

**不负责（归谁）**

- 建数据库、准备 Redis：使用者（见下一节）
- 查到"没有权限"之后要不要放行：调用方。本组件只回答有没有
- 权限名的含义（如 `erp.order.approve`）：使用它的业务组件。这里的权限是自由字符串
- 这个人是谁、在哪个部门：`people/basic`
- 部门的上下级：`department/tree`。本组件不读部门树，授予某部门的角色**不会**传给它的下级部门
- 登录与令牌：`auth/password-login`
- 增删角色与授权：本组件没有写接口，数据目前只来自迁移里的样例；要改只能由管库的人直接改表（生效时机见配置指南的 `CACHE_TTL_SECONDS`）

## 部署前准备

**1. 建数据库。** 平台不会替你创建数据库：表由本组件的迁移建，库本身要使用者先建好，执行一次即可。约定的库名是 `brickkit_rbac`（想用别的名字，改 `DATABASE_NAME` 即可）：

```bash
psql -U postgres -c "CREATE DATABASE brickkit_rbac"

# 或者数据库跑在容器里
docker exec -i my-postgres psql -U postgres -c "CREATE DATABASE brickkit_rbac"
```

确认建好了：`psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname='brickkit_rbac'"` 输出 `1`。每个组件用自己的库：共用一个库意味着一个组件能读到另一个组件的表；迁移发现库里有别的组件的记录时会打一条警告。

**2. Redis（可选）。** 要跨副本共享缓存，就准备一个 Redis，在配置里填上 `REDIS_HOST`。不配也完全能跑：启动时打一条"没有配置 Redis，改用进程内缓存"的警告，这是预期行为。

## 依赖说明

- `people/basic`（强依赖）：用 `GET /api/v1/people/{personId}` 取这个人所在的部门，地址由平台注入为 `PEOPLE_BASIC_ENDPOINT`，缺失时组件启动即失败。对方回 404：这个人不存在，本组件回 404（gRPC `NOT_FOUND`）。对方不可用、超时（5 秒）或回其他非 200：缓存命中时照常返回；缓存未命中时回 **503**（gRPC `UNAVAILABLE`），**不做部分降级**——那时只知道直接授予的角色、不知道部门角色，返回一个残缺的权限集比返回错误更危险：调用方会把它当成完整的用，一个本该有权限的人被拒绝，或者更糟，一段判断"是否为管理员"的逻辑因为缺了部门角色走进了别的分支。

PostgreSQL 与 Redis 不是组件依赖，而是通过下面的配置项接上的。三个外部系统挂掉时的表现各不相同：

| 挂了的 | 角色 | 表现 |
| --- | --- | --- |
| PostgreSQL | 数据源 | 缓存未命中的请求回 503 |
| `people/basic` | 数据源（部门在那边） | 缓存未命中 → 503；缓存命中 → 照常返回 |
| Redis | 加速器 | 照常回源，只是慢一点 |

## 配置指南

`DATABASE_HOST`、`DATABASE_NAME`、`DATABASE_USER` 三个必填，缺任何一个组件启动即失败并一次列出缺了哪几个，不会退化到 localhost。

- `DATABASE_HOST`：容器网络里 PostgreSQL 的服务名，不是宿主机上的 `localhost`。`DATABASE_NAME` 是「部署前准备」里建好的库。`DATABASE_USER` / `DATABASE_PASSWORD` 按库的账号填，口令写成 `${VAR}` 或 `file://` 引用；口令里的 `@ : /` 没有问题。
- `REDIS_HOST`：填了才启用 Redis，`REDIS_PORT` / `REDIS_PASSWORD` 只在它填了时才有意义。不填就是每个副本各用一份进程内缓存：单副本完全够用；多副本时各自缓存、各自按 TTL 过期，不会算错，只是命中率低一些。缓存键带组件前缀（`authorization-rbac:permissions:<personId>`），和别的组件共用一个 Redis 不会互相覆盖。单次 Redis 操作 500 毫秒超时，慢了就回源。
- `CACHE_TTL_SECONDS`：一份权限在缓存里最多留多久，必须是正整数。本组件没有写接口，直接改库里的角色或授权**不会**让缓存失效，所以改动最迟在一个 TTL 之后才生效；用 Redis 时可以删掉对应的键让它立刻生效，进程内缓存只能等 TTL 或重启。同样，缓存命中时不问 `people/basic`：一个人换了部门或被删掉，最多一个 TTL 之后才反映出来。调短，变更生效快、`people/basic` 与数据库的压力大；调长反之，`people/basic` 短暂抖动时能被缓存挡住的请求也越多。

`config/authorization-rbac.yaml` 填好后大致是：

```yaml
DATABASE_HOST: $var:PG_HOST
DATABASE_NAME: brickkit_rbac
DATABASE_USER: $var:PG_USER
DATABASE_PASSWORD: $var:PG_PASSWORD
# 可选：不写就用进程内缓存
REDIS_HOST: redis
REDIS_PASSWORD: ${REDIS_PASSWORD}
```

`PEOPLE_BASIC_ENDPOINT` 由平台按依赖注入，不要写进 `config/`。`LOG_LEVEL` 只影响日志详细程度；口令一类字段无论哪一级都会打码，缓存键会照常记录（排障用，不含秘密）。

## 契约索引

- `openapi.json`：HTTP 接口，调用方拿到的地址是 `AUTHORIZATION_RBAC_ENDPOINT`。
  - `GET /api/v1/permissions?personId=`：→ `{personId, departmentId, roles, permissions, cached}`，`roles` 与 `permissions` 已排序去重，没有时是 `[]` 而不是 `null`
  - `POST /api/v1/check`：`{personId, permission}` → `{personId, permission, allowed, reason, cached}`
  - `GET /healthz`：只检查本进程存活，不碰 Redis、不查库、不调 `people/basic`
  - 运行中的组件也在 `GET /openapi.json` 发出同一份文档
- `proto/authorization/v1/authorization.proto`：gRPC 服务 `authorization.v1.AuthorizationService`，`Check` 与 `ListPermissions` 两个方法，与 HTTP 在同一个端口，支持反射（`grpcurl -plaintext <地址> list` 不带 `.proto` 也能列出服务）。调用方据此生成客户端。

**两个协议给出同一个答案**，否则同一个人在不同调用路径上会有不同的权限。

**check 没有权限时是 200 + `allowed: false`，不是 403。** 这个端点回答的是"他有没有这个权限"，调用方才决定要不要放行；用 403 会让调用方分不清"我没权限调这个接口"和"我查到了，答案是否"。`reason`（`direct` / `department` / `none`）只是排障线索，不参与任何判定。`cached` 说明这次是不是缓存命中。

```bash
curl -s "$AUTHORIZATION_RBAC_ENDPOINT/api/v1/permissions?personId=p-001"

curl -s -X POST "$AUTHORIZATION_RBAC_ENDPOINT/api/v1/check" \
  -H 'Content-Type: application/json' \
  -d '{"personId":"p-002","permission":"erp.order.approve"}'
```

样例数据（`0002_seed_rbac`，对应 `people/basic` 的样例人员与 `department/tree` 的样例部门）：角色 `r-viewer`（`erp.order.read`）、`r-manager`（`erp.order.read`、`erp.order.approve`）、`r-hr`（`people.person.read`）。

| 人 | 部门 | 权限来自 |
| --- | --- | --- |
| p-001 | d-tech | 直接（r-viewer）+ 部门（r-manager） |
| p-002 | d-tech | 只有部门（r-manager） |
| p-003 | d-hr | 只有部门（r-hr） |
| p-004 | d-backend | 什么都没有——`d-backend` 是 `d-tech` 的下级，但部门授权不向下传 |

不发布事件，也不消费事件。

## 外壳声明

不是外壳。
