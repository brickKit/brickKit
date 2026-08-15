# authorization/rbac

授权组件：回答"某个人能不能做某件事"。BrickKit Phase 5 的第四个测试组件。

## 它验证平台的什么

| 验证点 | 怎么体现 |
| --- | --- |
| **cache 资源**（Redis） | 平台按 `kind: cache` 注入 `REDIS_HOST/PORT/PASSWORD`（006 §5.2） |
| 单端口双协议 | HTTP 与 gRPC 共用 `deployment.port`，h2c + Content-Type 分流 |
| gRPC Reflection | `grpcurl list` 不带 `.proto` 也能列出服务 |
| **三种依赖、三种故障表现** | 见下表，这是本组件最值得抄的地方 |
| 健康检查不越界 | `/healthz` 不碰 Redis、不查库、不调 people/basic |

## 三个依赖，三种不同的故障表现

| 依赖 | 角色 | 挂了会怎样 |
| --- | --- | --- |
| PostgreSQL | 数据源 | **503** |
| people/basic | 数据源（部门在那边） | 缓存未命中 → **503**；缓存命中 → 照常返回 |
| Redis | **加速器** | 照常回源，只是慢一点 |

**Redis 是加速器，不是数据源。** 它挂了若报错，等于让一个可选的基础设施变成单点——
整个系统的每一次权限检查都会失败。没绑定 `cache` 资源时组件也照样能起来，
只是改用进程内缓存。

**people/basic 挂了且缓存未命中时不做部分降级。** 那时只知道"直接授予的角色"、
不知道部门角色；返回一个残缺的权限集比返回错误更危险——调用方会把它当成完整的用，
于是一个本该有权限的人被拒绝，或者更糟：一个判断"是否为管理员"的逻辑因为缺了部门角色
而走进了别的分支。

## 权限从哪来

```
权限 = 直接授予这个人的角色  ∪  授予这个人所在部门的角色
                                        ↑
                                 部门取自 people/basic（强依赖）
```

样例数据特意安排了三种情形：

| 人 | 部门 | 权限来自 |
| --- | --- | --- |
| p-001 | d-tech | 直接（r-viewer）+ 部门（r-manager） |
| p-002 | d-tech | 只有部门（r-manager） |
| p-003 | d-hr | 只有部门（r-hr） |
| p-004 | d-backend | 什么都没有 —— 空权限也要能正确表达 |

## API

| 端点 | 说明 |
| --- | --- |
| `GET /api/v1/permissions?personId=` | 全部权限（已排序去重） |
| `POST /api/v1/check` | `{personId, permission}` → `{allowed, reason, cached}` |
| `GET /healthz` | 只检查本进程存活 |

gRPC 提供同样的两个方法，契约见 `proto/authorization/v1/authorization.proto`。
**两个协议必须给出同一个答案**——否则同一个人在不同调用路径上会有不同的权限，
`service_test.go` 里有专门的用例锁住这一条。

### check 没有权限时是 200，不是 403

这个端点回答的是"他有没有这个权限"，**调用方**才决定要不要放行。
用 403 表示"查到了但没有"，会让调用方分不清"我没权限查这个接口"
和"我查到了、答案是否"。

`reason` 字段（`direct` / `department` / `none`）只是排障线索，不参与任何判定。

## 缓存

键：`authorization-rbac:permissions:<personId>`，TTL 默认 5 分钟。

- **带组件前缀**（006 §7）：一个项目里多个组件可能绑定同一个 cache 资源，
  不带前缀的话两个组件用了同一个键名就会互相覆盖，症状是"权限偶尔不对"。
- **按人分开**：键漏了 personId 会让所有人共用同一份权限——这是权限系统里最严重的
  一类事故，而且功能测试全都会通过。
- **必须有 TTL**：授权变更时会主动失效缓存，TTL 只是兜底；没有它，万一漏了一条失效，
  一份错的权限会**永远**留在缓存里。
- 缓存内容坏了当作未命中去回源并顺手删掉——报错的话，一条坏缓存会把这个人永久挡在门外。

## 配置

| 环境变量 | 来源 | 必需 |
| --- | --- | --- |
| `DATABASE_*` | 平台按资源绑定注入（006 §5） | ✅ |
| `PEOPLE_BASIC_ENDPOINT` | 平台按强依赖注入（003 §4.5） | ✅ |
| `REDIS_HOST` / `REDIS_PORT` / `REDIS_PASSWORD` | 平台按 cache 资源注入 | ❌ 可选 |
| `LOG_LEVEL` | `configSchema.logLevel` | ❌ |
| `CACHE_TTL_SECONDS` | `configSchema.cacheTtlSeconds` | ❌ |

## 本地运行

```bash
docker exec my-postgres psql -U postgres -c "CREATE DATABASE brickkit_rbac"

# 单元测试（内存实现，不需要任何外部服务）
go test ./...

# 含 PostgreSQL 与 Redis 契约测试
RBAC_TEST_DATABASE_URL="postgres://postgres:PASSWORD@localhost:5432/brickkit_rbac?sslmode=disable" \
RBAC_TEST_REDIS_ADDR="localhost:6379" \
  go test ./...
```

改了 `.proto` 之后重新生成：`make proto-authorization`（在仓库根目录）。

## 设计依据

002 组件规范（§1.4、§7 契约即产物、§8 迁移、§9.4 健康检查、§11 日志）、
003 §4.5（依赖地址注入）、006（基础资源规范，§5.2 环境变量、§7 键前缀）、
009（Go 组件单端口双协议）。
