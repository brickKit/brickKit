# people/basic

查询人员；部门名来自 `department/tree`，不在这里另存一份。HTTP 在主端口 8080，gRPC 在额外端口 9090。

## 组件定位

回答"这个人是谁"：姓名、所在部门、职务。返回人员时现问 `department/tree` 补上部门名，查看单个人员时（事件总线在的话）发一条事件。

**负责**

- 人员的 ID、姓名、部门 ID、职务，按部门筛选的人员列表
- 返回结果里的部门名（每次现取，不存副本）
- 自己库里的表结构与样例人员（迁移 `0001_init`、`0002_seed_people`）
- 查看单个人员时发布 `people.person.viewed` 事件

**不负责（归谁）**

- 建数据库本身：使用者（见下一节）
- 部门本身（名称、上下级）：`department/tree`
- 登录与口令：`auth/password-login`
- 权限与角色：`authorization/rbac`
- 事件的投递与订阅：`infra/redis-event-bus`
- 人员的新增、修改、删除：本组件没有写接口，数据目前只来自迁移里的样例；要改只能由管库的人直接改 `people` 表

## 部署前准备

平台不会替你创建数据库：库里的**表**由本组件的迁移建，**库本身**要使用者先建好，执行一次即可。约定的库名是 `brickkit_people`（想用别的名字，改 `DATABASE_NAME` 即可）：

```bash
psql -U postgres -c "CREATE DATABASE brickkit_people"

# 或者数据库跑在容器里
docker exec -i my-postgres psql -U postgres -c "CREATE DATABASE brickkit_people"
```

确认建好了：`psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname='brickkit_people'"` 输出 `1`。

强依赖 `department/tree` 也要它**自己的**库（`brickkit_department`），按它的 `BRICKKIT.md` 准备。之后 `brickkit up` 会先跑迁移（建表、写入样例人员），再启动组件。

**每个组件用自己的库。** 共用一个库意味着一个组件能读到另一个组件的表，组件的数据就不再是它自己的了。迁移记录按 `(component_id, version)` 隔离，共用时不会互相顶掉，但迁移发现库里有别的组件的记录时会打一条警告。

## 依赖说明

- `department/tree`（强依赖）：用 gRPC 按部门 ID 查部门名，地址由平台注入为 `DEPARTMENT_TREE_ENDPOINT`。地址缺失时组件启动即失败——没有它本组件无法履行契约。运行中它不可用时，业务接口回 **503**「部门信息暂时不可用」（gRPC 回 `UNAVAILABLE`），`/healthz` 仍是 200，依赖恢复后自动好；不会返回一个没有部门名的人员，那会让调用方以为"这个人真的没有部门"。部门被删（对方回 `NOT_FOUND`）不算故障：人员照常返回，部门名为空。
- `infra/redis-event-bus`（可选依赖）：用来发布事件。项目里没有它时平台完全不注入 `INFRA_REDIS_EVENT_BUS_ENDPOINT`，本组件跳过事件发布；它在但调用出错（单次 1 秒超时）时只记一条警告。两种情况下接口都照常 **200**。

## 配置指南

`DATABASE_HOST`、`DATABASE_NAME`、`DATABASE_USER` 三个必填，缺任何一个组件启动即失败并一次列出缺了哪几个，不会退化到 localhost。

- `DATABASE_HOST`：容器网络里 PostgreSQL 的服务名，不是宿主机上的 `localhost`。
- `DATABASE_NAME`：「部署前准备」里建好的库。
- `DATABASE_USER` / `DATABASE_PASSWORD`：口令写成 `${VAR}` 或 `file://` 引用，不要明文写进 `config/`；口令里的 `@ : /` 没有问题，组件会转义。

本组件和 `department/tree` 通常连同一台 PostgreSQL、各用各的库，公共部分放进 `config/vars.yaml`：

```yaml
# config/vars.yaml
PG_HOST: postgres
PG_USER: postgres
PG_PASSWORD: ${POSTGRES_PASSWORD}

# config/people-basic.yaml
DATABASE_HOST: $var:PG_HOST
DATABASE_NAME: brickkit_people          # 「部署前准备」里建好的库
DATABASE_USER: $var:PG_USER
DATABASE_PASSWORD: $var:PG_PASSWORD

# config/department-tree.yaml（强依赖也需要它自己的库）
DATABASE_HOST: $var:PG_HOST
DATABASE_NAME: brickkit_department
DATABASE_USER: $var:PG_USER
DATABASE_PASSWORD: $var:PG_PASSWORD
```

`DEPARTMENT_TREE_ENDPOINT`、`INFRA_REDIS_EVENT_BUS_ENDPOINT` 由平台按依赖注入，不要写进 `config/`。`LOG_LEVEL` 只影响日志详细程度；口令一类字段无论哪一级都会打码。

## 契约索引

- `proto/people/v1/people.proto`：gRPC 服务 `people.v1.PeopleService`，在额外端口 9090（平台给调用方注入 `PEOPLE_BASIC_GRPC_ENDPOINT`）。`ListPeople`（`department_id` 非空时只返回该部门的人）、`GetPerson`（不存在是 `NOT_FOUND`）；`Person` 带 `department_name`。
- `openapi.json`：HTTP 接口，在主端口 8080（调用方拿到的是 `PEOPLE_BASIC_ENDPOINT`）——`GET /api/v1/people[?departmentId=]` 返回 `{people, total}`，`GET /api/v1/people/{id}` 返回 `{id, name, departmentId, departmentName, title}`，不存在是 404。它由 FastAPI 生成，运行中的组件也在 `GET /openapi.json` 发出。

两个协议给出同样的结果；gRPC 支持反射：

```bash
grpcurl -plaintext localhost:9090 list
grpcurl -plaintext -d '{"departmentId":"d-hr"}' localhost:9090 \
  people.v1.PeopleService/ListPeople
```

数据库不可用时回 503「人员数据暂时不可用」（gRPC `UNAVAILABLE`），不带底层原因。`GET /healthz` 只检查本进程存活，不查数据库、不调依赖。

样例人员（`0002_seed_people`，部门对应 `department/tree` 的样例组织架构）：`p-001` 张三、`p-002` 李四（`d-tech`），`p-003` 王五（`d-hr`），`p-004` 赵六（`d-backend`）。

发布的事件：`people.person.viewed`，载荷 `{"personId": "<ID>"}`，在单个人员查询成功时发出（HTTP 与 gRPC 都发），经 `infra/redis-event-bus` 的 `POST /api/v1/events` 投递；尽力而为，发不出去不重试。

不消费事件。

## 外壳声明

不是外壳。
