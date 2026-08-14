# department/tree

部门树组件。**叶子组件**：不依赖任何其他组件，只依赖一个 PostgreSQL 数据库。

HTTP 与 gRPC 共用同一个端口（8080）。

---

## 使用前：创建数据库（执行一次）

平台**不会**替你创建数据库（006 §9.1：CLI 负责声明、绑定、注入配置，
不负责创建数据库、修改数据库结构、迁移生产数据）。库里的**表**由本组件的
migrations 建，但**库本身**需要你先建好。

本组件预设的数据库名是 **`brickkit_department`**。执行一次即可：

```bash
# 直接连 PostgreSQL
psql -U postgres -c "CREATE DATABASE brickkit_department"

# 或者数据库跑在容器里
docker exec -i my-postgres psql -U postgres -c "CREATE DATABASE brickkit_department"
```

然后在 `brickkit.yaml` 里把它绑定给本组件：

```yaml
resources:
  - kind: database
    engine: postgresql
    id: postgres-main
    host: postgres              # Docker Network 内的服务名
    port: 5432
    username: postgres
    password: ${POSTGRES_PASSWORD}
    bindings:
      - componentId: department/tree
        database: brickkit_department      # ← 上面建好的库
```

平台会把它转成 `DATABASE_HOST` / `DATABASE_PORT` / `DATABASE_NAME` /
`DATABASE_USER` / `DATABASE_PASSWORD` 注入给容器（006 §5.2）。
**组件不知道也不关心数据库跑在哪**，换库只改 `brickkit.yaml`。

> 想用别的库名？改 `bindings[].database` 即可，组件不认死名字。
> 但**每个组件用自己的库**：共用一个库意味着一个组件能读到另一个组件的表，
> 违反 002 §2.2 的数据自治。

---

## 数据库迁移

表结构与初始数据都在 `migrations/` 下，是**有版本的 SQL 文件**：

```
migrations/
├── 0001_init.up.sql                建表与索引
├── 0001_init.down.sql              回退：删表
├── 0002_seed_departments.up.sql    初始组织架构
└── 0002_seed_departments.down.sql  回退：删初始数据
```

脚本通过 `go:embed` 打进二进制（002 §8.4：迁移脚本与业务代码同镜像同版本），
执行记录写在 `schema_migrations` 表里，按 `(component_id, version)` 隔离。

### 命令

平台会在启动组件前自动执行 `migrate`（`component.yaml` 的 `migration.command`）。
手工执行时：

```bash
# 向上迁移（执行尚未执行过的迁移）——幂等，重复执行什么都不做
docker run --rm --env-file .env brickkit-demo/department-tree:1.0.0 migrate

# 回退最近 1 个迁移
docker run --rm --env-file .env brickkit-demo/department-tree:1.0.0 migrate down

# 回退最近 3 个
docker run --rm --env-file .env brickkit-demo/department-tree:1.0.0 migrate down 3

# 全部回退（库回到干净状态，方便反复测试）
docker run --rm --env-file .env brickkit-demo/department-tree:1.0.0 migrate reset
```

> `down` / `reset` 是**给开发与测试用的**，让你能反复把库搭起来、拆掉。
> 生产环境的结构问题请用一个新的 up 迁移去修，而不是 down 回去
> （002 §8.9：先兼容后迁移、不做破坏性操作）。

### 加一个新迁移

新增一对文件即可，**不要改已有的**（改了也不会重跑，因为版本已记录）：

```
migrations/0003_add_code.up.sql      ALTER TABLE departments ADD COLUMN code TEXT ...
migrations/0003_add_code.down.sql    ALTER TABLE departments DROP COLUMN code
```

三条不变量由测试锁定（`migrate_test.go`）：

| 不变量 | 含义 |
| --- | --- |
| 幂等 | 容器每次重启都会再跑一遍，第二次必须什么都不做 |
| 原子 | 迁移与它的版本记录在同一个事务里，失败整条回滚 |
| 有序 | 向上按版本递增，回退按版本倒序；失败即中止且不记录版本 |

---

## 环境变量

| 变量 | 必须 | 说明 |
| --- | --- | --- |
| `DATABASE_HOST` / `DATABASE_NAME` / `DATABASE_USER` | ✅ | 缺失时**启动即失败**并列出缺哪几个，不会退化到 localhost |
| `DATABASE_PORT` | ❌ | 默认 5432 |
| `DATABASE_PASSWORD` | ❌ | 按你的库配置 |
| `COMPONENT_ID` / `COMPONENT_VERSION` | ❌ | 平台注入，用于日志与迁移记录 |
| `LOG_LEVEL` | ❌ | debug / info / warn / error，默认 info |

---

## 接口

| 协议 | 地址 | 说明 |
| --- | --- | --- |
| HTTP | `GET /api/v1/departments[?parentId=]` | 部门列表 |
| HTTP | `GET /api/v1/departments/{id}` | 单个部门 |
| HTTP | `GET /api/v1/departments/{id}/subtree` | 子树（自己 + 全部下级） |
| HTTP | `GET /healthz` | 健康检查，**只检查本进程存活**，不查数据库（002 §9.4） |
| gRPC | `department.v1.DepartmentService` | 同一个 8080 端口，支持反射 |

```bash
# gRPC 不需要 .proto，反射就够
grpcurl -plaintext localhost:8080 list
grpcurl -plaintext -d '{"parentId":"d-root"}' localhost:8080 \
  department.v1.DepartmentService/ListDepartments
```

产物（`brickkit add` 后落在 `.brickkit/artifacts/department-tree-1-0-0/`）：

| 类型 | 文件 | 用途 |
| --- | --- | --- |
| api-contract | `proto/department/v1/department.proto` | 调用方据此生成 gRPC 客户端 |
| api-docs | `openapi.json` | HTTP 接口文档 |

---

## 开发

```bash
go test ./...                       # 不需要数据库的部分
make test-components-integration    # 含迁移的集成测试（仓库根目录执行）
make proto-department               # 改了 .proto 之后重新生成 Go 代码
```
