# department/tree

查询组织架构（部门树）。HTTP 与 gRPC 共用同一个端口。

## 组件定位

给其他组件一个查部门的地方：按 ID 查单个部门、列出某个部门的直接下级、取一个部门连同它全部下级的子树。它是叶子组件，不依赖任何其他组件，只需要一个 PostgreSQL 数据库。

**负责**

- 部门的 ID、名称、上级、层级，以及按这些关系查列表与子树
- 自己库里的表结构与初始组织架构（迁移 `0001_init`、`0002_seed_departments`）
- 同一个端口上的 HTTP REST 与 gRPC（支持 gRPC 反射）

**不负责（归谁）**

- 建数据库本身：使用者（见下一节）
- 人员属于哪个部门：`people/basic`，它来这里查部门名
- 给部门授予角色与权限：`authorization/rbac`
- 部门的新增、改名、删除：本组件没有写接口，数据目前只来自迁移里的样例；要改只能由管库的人直接改 `departments` 表

## 部署前准备

平台不会替你创建数据库：库里的**表**由本组件的迁移建，**库本身**要使用者先建好，执行一次即可。约定的库名是 `brickkit_department`（想用别的名字，改 `DATABASE_NAME` 即可，组件不认死名字）：

```bash
# 直接连 PostgreSQL
psql -U postgres -c "CREATE DATABASE brickkit_department"

# 或者数据库跑在容器里
docker exec -i my-postgres psql -U postgres -c "CREATE DATABASE brickkit_department"
```

确认建好了：`psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname='brickkit_department'"` 输出 `1`。之后 `brickkit up` 会先跑本组件的迁移（建表、写入初始部门），再启动它。

**每个组件用自己的库。** 共用一个库意味着一个组件能读到另一个组件的表，组件的数据就不再是它自己的了。迁移记录按 `(component_id, version)` 隔离，共用时不会互相顶掉，但迁移发现库里有别的组件的记录时会打一条警告。

## 依赖说明

不依赖任何组件。唯一的外部系统是 PostgreSQL，它不是组件依赖，而是通过下面的 `DATABASE_*` 配置项接上的。

## 配置指南

`DATABASE_HOST`、`DATABASE_NAME`、`DATABASE_USER` 三个必填，缺任何一个组件启动即失败并一次列出缺了哪几个，**不会**退化到 localhost——悄悄连到 localhost 会让人以为配好了，实际连的根本不是那个库。

- `DATABASE_HOST`：容器网络里 PostgreSQL 的服务名（例如 `postgres`），不是宿主机上看到的 `localhost`。
- `DATABASE_NAME`：「部署前准备」里建好的库。
- `DATABASE_USER` / `DATABASE_PASSWORD`：口令写成 `${VAR}` 或 `file://` 引用，不要明文写进 `config/`。口令里的 `@ : /` 没有问题，组件按 URL 规则转义。

`brickkit add` 会生成 `config/department-tree.yaml` 的骨架，填好后大致是：

```yaml
DATABASE_HOST: postgres              # 容器网络内的服务名
DATABASE_NAME: brickkit_department   # 「部署前准备」里建好的库
DATABASE_USER: postgres
DATABASE_PASSWORD: ${POSTGRES_PASSWORD}
# DATABASE_PORT 不写就是 5432
```

几个组件连同一台 PostgreSQL 时，把公共部分写进 `config/vars.yaml`，各组件用 `$var:` 引用，例如 `DATABASE_HOST: $var:PG_HOST`。组件不知道也不关心数据库跑在哪，换库只改项目的 `config/`。

`LOG_LEVEL` 调到 `debug` 只多出排障信息；口令、DSN 一类字段无论哪一级都会打码。

## 契约索引

- `proto/department/v1/department.proto`：gRPC 服务 `department.v1.DepartmentService`，三个方法——`ListDepartments`（`parent_id` 非空时只返回它的直接下级，否则返回全部，扁平列表，靠 `parent_id` 表达层级）、`GetDepartment`（按 ID，找不到是 `NOT_FOUND`）、`GetSubtree`（`root_id` 自己加全部下级，根不存在是 `NOT_FOUND`）。调用方据此生成 gRPC 客户端；`brickkit add` 之后它在 `.brickkit/artifacts/department-tree-1-0-0/api-contract/` 下。
- `openapi.json`：HTTP 接口——`GET /api/v1/departments[?parentId=]`（列表）、`GET /api/v1/departments/{id}`（单个，不存在是 404）、`GET /api/v1/departments/{id}/subtree`（子树，根不存在是 404）。运行中的组件也在 `GET /openapi.json` 发出同一份。

两个协议在同一个主端口上（平台注入给调用方的是 `DEPARTMENT_TREE_ENDPOINT`），gRPC 支持反射，不带 `.proto` 也能调：

```bash
grpcurl -plaintext localhost:8080 list
grpcurl -plaintext -d '{"parentId":"d-root"}' localhost:8080 \
  department.v1.DepartmentService/ListDepartments
```

数据库不可用时 HTTP 回 503「部门数据暂时不可用」、gRPC 回 `UNAVAILABLE`，不带底层原因。`GET /healthz` 只检查本进程存活，不查数据库。

初始组织架构（`0002_seed_departments`，`people/basic` 的样例人员挂在这些部门上）：`d-root` 总公司 → `d-tech` 技术中心、`d-hr` 人力资源部；`d-tech` → `d-backend` 后端组。

不发布事件，也不消费事件。

## 外壳声明

不是外壳。
