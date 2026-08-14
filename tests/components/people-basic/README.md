# people/basic

人员组件。**连接组件**：有自己的数据（人员），强依赖 `department/tree` 补全部门名，
弱依赖 `infra/redis-event-bus` 发事件。

HTTP 在 8080，gRPC 在 9090（Python 的 grpcio 无法与 HTTP 共用端口，
因此在 `component.yaml` 里用 `extraPorts` 声明）。

---

## 使用前：创建数据库（执行一次）

平台**不会**替你创建数据库（006 §9.1）。库里的**表**由本组件的 migrations 建，
但**库本身**需要你先建好。

本组件预设的数据库名是 **`brickkit_people`**。执行一次即可：

```bash
psql -U postgres -c "CREATE DATABASE brickkit_people"

# 或者数据库跑在容器里
docker exec -i my-postgres psql -U postgres -c "CREATE DATABASE brickkit_people"
```

然后在 `brickkit.yaml` 里绑定：

```yaml
resources:
  - kind: database
    engine: postgresql
    id: postgres-main
    host: postgres
    port: 5432
    username: postgres
    password: ${POSTGRES_PASSWORD}
    bindings:
      - componentId: people/basic
        database: brickkit_people          # ← 上面建好的库
      - componentId: department/tree
        database: brickkit_department      # ← 强依赖也需要它自己的库
```

> **每个组件用自己的库。** 共用一个库意味着一个组件能读到另一个组件的表，
> 违反 002 §2.2 的数据自治。迁移记录虽然按 `(component_id, version)` 隔离了
> （共用时不会互相顶掉），但组件启动时会打一条警告提醒你分开。

---

## 数据库迁移

```
migrations/
├── 0001_init.up.sql            建表与索引
├── 0001_init.down.sql          回退：删表
├── 0002_seed_people.up.sql     初始人员
└── 0002_seed_people.down.sql   回退：删初始数据
```

脚本随镜像一起打包（002 §8.4），执行记录写在 `schema_migrations` 表里。

### 命令

平台会在启动组件前自动执行 `migrate`（`component.yaml` 的 `migration.command`）。
手工执行时：

```bash
docker run --rm --env-file .env brickkit-demo/people-basic:1.0.0 migrate          # 向上迁移（幂等）
docker run --rm --env-file .env brickkit-demo/people-basic:1.0.0 migrate down     # 回退最近 1 个
docker run --rm --env-file .env brickkit-demo/people-basic:1.0.0 migrate down 3   # 回退最近 3 个
docker run --rm --env-file .env brickkit-demo/people-basic:1.0.0 migrate reset    # 全部回退
```

> `down` / `reset` 是**给开发与测试用的**。生产环境的结构问题请用一个新的
> up 迁移去修（002 §8.9：先兼容后迁移、不做破坏性操作）。

### 加一个新迁移

新增一对文件即可，不要改已有的：

```
migrations/0003_add_email.up.sql     ALTER TABLE people ADD COLUMN email TEXT
migrations/0003_add_email.down.sql   ALTER TABLE people DROP COLUMN email
```

---

## 环境变量

| 变量 | 必须 | 说明 |
| --- | --- | --- |
| `DATABASE_HOST` / `DATABASE_NAME` / `DATABASE_USER` | ✅ | 缺失时**启动即失败**并列出缺哪几个 |
| `DEPARTMENT_TREE_ENDPOINT` | ✅ | **强依赖**：平台按依赖关系注入。缺失时启动即失败——没有它本组件无法履行契约 |
| `INFRA_REDIS_EVENT_BUS_ENDPOINT` | ❌ | **弱依赖**：该组件没启动时平台完全不注入，本组件自行降级（跳过事件发布） |
| `DATABASE_PORT` / `DATABASE_PASSWORD` | ❌ | 默认端口 5432 |
| `COMPONENT_ID` / `COMPONENT_VERSION` / `LOG_LEVEL` | ❌ | 平台注入 |

### 强依赖与弱依赖的区别（真容器验证过）

| 情形 | 表现 |
| --- | --- |
| 弱依赖没注入 / 调用出错 | 接口 **200**，静默降级，只记一条警告 |
| **强依赖 department/tree 挂了** | 业务接口 **503**「部门信息暂时不可用」，`/healthz` 仍 200，依赖恢复后自动好 |

`/healthz` **只检查本进程存活**，不查数据库也不调依赖（002 §9.4）——
健康检查一旦连库，数据库抖一下就会让所有组件被判死重启。

---

## 接口

| 协议 | 地址 | 说明 |
| --- | --- | --- |
| HTTP | `GET /api/v1/people[?departmentId=]` | 人员列表，含部门名 |
| HTTP | `GET /api/v1/people/{id}` | 单个人员 |
| HTTP | `GET /openapi.json` | FastAPI 自动生成的接口文档 |
| HTTP | `GET /healthz` | 健康检查 |
| gRPC | `people.v1.PeopleService`（:9090） | 支持反射 |

```bash
grpcurl -plaintext localhost:9090 list
grpcurl -plaintext -d '{"departmentId":"d-hr"}' localhost:9090 \
  people.v1.PeopleService/ListPeople
```

**部门名不在本组件存副本**（002 §2.2 数据自治）：每次去问 `department/tree`，
因此部门改名后这里立刻跟着变。一次列表请求内按部门去重，不会 N+1。

---

## 开发

宿主机通常没有可用的 Python 环境，**测试跑在容器里**（版本固定、可复现）：

```bash
docker build --target test -t people-basic-test . && docker run --rm people-basic-test

# 含迁移的集成测试（仓库根目录执行）
make test-components-integration

# 改了接口之后重新导出 openapi.json
make openapi-people
```
