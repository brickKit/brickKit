# department/tree

Go 写的部门树查询组件，HTTP 与 gRPC 共用 8080 端口，只依赖一个 PostgreSQL。它也是 BrickKit 仓库自带的平台验证夹具：单端口双协议、gRPC 反射、迁移、契约产物都靠它验证。怎么用、边界和契约见 `BRICKKIT.md`；依赖、配置、部署见 `component.yaml`。

## 代码地图

| 路径 | 管什么 |
| --- | --- |
| `main.go` | 入口：参数解析（启动服务 / `migrate [down [n] \| reset]`）、读配置、连库、起服务 |
| `config.go` | 只从环境变量读配置、拼 DSN、JSON 日志与敏感字段打码 |
| `server.go` | 同一端口上 h2c + 按 Content-Type 把 gRPC 与 REST 分流，注册 gRPC 反射 |
| `service.go` | 业务逻辑，同时是 gRPC 服务实现与 HTTP 路由；健康检查与运行时发出 OpenAPI 文档的端点也在这里 |
| `store.go` | `Store` 接口、`ErrNotFound`、内存实现 |
| `postgres.go` | `Store` 的 PostgreSQL 实现（子树用递归 CTE） |
| `migrate.go` | 内嵌迁移脚本的执行、回退与 `schema_migrations` 记录 |
| `migrations/` | 有版本的 SQL：`0001_init` 建表，`0002_seed_departments` 初始组织架构 |
| `proto/department/v1/department.proto` | gRPC 契约（发布为 api-contract 产物） |
| `gen/department/v1/` | 由 proto 生成的 Go 代码，不手改 |
| `openapi.json` | HTTP 文档（发布为 api-docs 产物，也被 `go:embed` 进二进制在运行时发出） |
| `Dockerfile` | 两阶段构建，运行期只有一个静态二进制，以 UID 10001 运行 |
| `component.yaml` | 组件契约 |
| `args_test.go` | 参数解析：不认识的参数必须报错 |
| `component_test.go` | 配置、日志、`component.yaml` 声明的产物存在、Dockerfile 非 root |
| `server_test.go` | 单端口双协议、反射、未知路径回 JSON 404 |
| `service_test.go` | 两种协议的查询结果一致、存储故障与不存在的映射 |
| `store_test.go` | 存储契约，内存实现始终跑，PostgreSQL 实现按环境变量跑 |
| `migrate_test.go` | 迁移的幂等、原子、有序与回退（需要真库） |
| `openapi_endpoint_test.go` | 运行时发出的文档就是仓库里那份 |

| 功能 | 从这里开始 | 然后 |
| --- | --- | --- |
| 加一个查询接口 | `proto/department/v1/department.proto` | 重新生成 `gen/department/v1/`，在 `service.go` 写一份逻辑、两个出口，再更新 `openapi.json` |
| 改存储或 SQL | `store.go` | `postgres.go`，`store_test.go` 的契约要两种实现都过 |
| 改表结构或初始数据 | `migrations/` | 新增一对 `.up.sql` / `.down.sql`，`migrate_test.go` |
| 改配置项 | `config.go` | `component.yaml` 的 `configSchema`，`BRICKKIT.md` 的配置指南 |
| 改端口或协议分流 | `server.go` | `config.go` 的 `listenAddr` 与 `component.yaml` 的 `deployment.port` 一起改 |

## 构建与测试

```bash
go vet ./... && go test ./...      # 单元测试：内存实现，不需要数据库
```

全部 `ok` 即通过；需要真库的用例会显示被跳过（"未设置 DEPARTMENT_TEST_DATABASE_URL"）。

含 PostgreSQL 的存储契约与迁移集成测试：

```bash
docker exec -i my-postgres psql -U postgres -c "CREATE DATABASE brickkit_department"
DEPARTMENT_TEST_DATABASE_URL="postgres://postgres:PASSWORD@localhost:5432/brickkit_department?sslmode=disable" \
  go test ./...
```

或在仓库根目录跑 `make test-components-integration`（读根目录 `.env`，库不存在时会代建）。

其他（都在仓库根目录执行）：

```bash
make proto-department                                              # 改了 .proto 之后重新生成 gen/
docker build -t brickkit-demo/department-tree:1.0.0 tests/components/department-tree   # 镜像，tag 与 component.yaml 一致
make check-components                                              # 每个测试组件都过 brickkit lint --strict
```

手工跑迁移（`brickkit up` 会在启动前自动跑 `migrate`；`.env` 里放 `DATABASE_*`）：

```bash
docker run --rm --env-file .env brickkit-demo/department-tree:1.0.0 migrate          # 向上，幂等
docker run --rm --env-file .env brickkit-demo/department-tree:1.0.0 migrate down     # 回退最近 1 个
docker run --rm --env-file .env brickkit-demo/department-tree:1.0.0 migrate down 3   # 回退最近 3 个
docker run --rm --env-file .env brickkit-demo/department-tree:1.0.0 migrate reset    # 全部回退
```

`down` / `reset` 是给开发与测试用的，让库能反复搭起来、拆掉；生产环境的结构问题用一个新的 up 迁移去修（先兼容后迁移、不做破坏性操作）。

## 设计取舍

- **单端口双协议**：用 h2c 承载明文 HTTP/2，`application/grpc` 交给 gRPC，其余交给 REST。`component.yaml` 只声明一个 `deployment.port`，健康检查、`_ENDPOINT` 注入、K8s Service 都只认这一个端口；双端口（如 `people/basic`）要额外声明 `extraPorts`，能省则省。
- **一份逻辑、两个协议出口**：HTTP 与 gRPC 各写一遍查询，迟早出现"HTTP 说有、gRPC 说没有"，所以两边都只是 `service.go` 里业务方法的薄壳。
- **开 gRPC 反射**：`grpcurl` 不带 `.proto` 也能列出并调用服务，是排障最省事的入口。
- **迁移用 `go:embed` 打进二进制**：迁移与服务同一个二进制、同一个版本，不可能出现"镜像里漏了 SQL 文件"。
- **`schema_migrations` 主键是 `(component_id, version)`**：版本号是每个组件各自的，两个组件都有 `0001_init`；真容器测出过共用一个库时先跑的组件把后跑的顶掉、后者的表建不出来。
- **运行时也发出 `/openapi.json`**：发布的产物是发布那一刻的快照，`infra/api-docs` 之类的工具要的是此刻跑着的服务长什么样。
- **缺配置不退化到 localhost**：悄悄连到 localhost 会让人以为配好了，实际连的不是那个库。
- **`resources` 只写 requests 不写 limits**：允许它涨到多少是部署方的判断，而且逐字段合并意味着这里写了 limits，用它的项目就删不掉了。

## 易错点

| 不许 | 症状 | 原因 |
| --- | --- | --- |
| 让入口把不认识的参数当成"启动服务" | 迁移容器永不退出，项目卡在 Created，日志却写着"组件已就绪" | 迁移容器与主容器是同一个镜像，只靠参数区分；`parseArgs` 是纯函数，先校验参数再读环境变量、连库 |
| 改已经发布过的迁移文件 | 改动在已有的库上永远不生效 | 版本已记进 `schema_migrations`，不会重跑；只能新增 `0003_…` 一对文件 |
| 让 `/healthz` 查数据库 | 数据库抖一下，所有副本被判死重启，一次故障放大成雪崩 | 健康检查只回答"本进程还活着吗" |
| 只改 `.proto` 不重新生成 | 编译失败，或 gRPC 行为与契约对不上 | `gen/department/v1/` 是生成物，`make proto-department` 才会更新 |
| 改接口不改 `openapi.json` | 产物与运行时文档都描述着旧接口 | 文档是手写的、被 `go:embed` 进二进制，`openapi_endpoint_test.go` 只保证二者是同一份 |
| 改 `.proto` 时忘了 `people/basic` | `people/basic` 用旧契约调本组件 | `people/basic` 在 `proto/vendor/department/v1/` 里有一份拷贝，要同步更新 |
| 用字符串拼 DSN | 口令里有 `@ : /` 时连到错误的主机 | `DSN()` 用 `url.URL` 转义 |

## 改代码前自查

- 动了参数解析：`args_test.go` 仍然要求不认识的参数报错，且不读环境变量就能报。
- 动了查询：HTTP 与 gRPC 两个出口给出同样的结果（`service_test.go` 的一致性用例）。
- 动了表结构或初始数据：新增了一对迁移文件，没改旧的；`people/basic` 的样例人员挂在 `d-tech`、`d-hr`、`d-backend` 上，删改这些部门会让装配出来的样例对不上。
- 动了 `.proto`：重新生成了 `gen/`，同步了 `people/basic` 里的 vendored 拷贝，必要时更新 `openapi.json`。
- 动了配置项：`config.go`、`component.yaml` 的 `configSchema`、`BRICKKIT.md` 的配置指南三处一致。
- 改了行为却没改 `metadata.version`：已经构建过的同 tag 镜像不会被重建。

<!-- brickkit:managed:begin lang=zh -->
<!-- 由 brickkit 维护（init、add、remove、upgrade、skills update）：这对标记之间的改动会被覆盖 -->

## BrickKit

这是一个 BrickKit 组件：平台只读 `component.yaml`。它依赖的规则：

- `configSchema` 的键就是代码读的环境变量名。不能用保留名：`COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`，以及任何 `*_ENDPOINT`。
- 依赖写精确版本。依赖的地址以 `<ID>_ENDPOINT` 注入；缺席的可选依赖根本没有这个变量，读的时候要带兜底。
- `/healthz` 只查本进程，不查依赖。迁移命令用同一个镜像跑，遇到不认识的参数必须直接失败。
- `BRICKKIT.md` 会随版本进入每个使用它的项目，在那里是脱离仓库单独读的：跟代码一起改，不放相对链接。
- 发版：改 `metadata.version`，提交、推送，`brickkit release`。`brickkit lint` 会检查清单和这些文档——在项目里、在这个目录下跑，只查这个组件（`--all` 查整个项目）。
- 完整规则在 `brickkit-component` 技能里（装了技能的项目或仓库根目录下的 `.claude/skills/brickkit-component/SKILL.md`；`brickkit skills update` 会装上）；参数问 `brickkit <命令> --help`；BrickKit 自己的文档用 `brickkit docs` 看。
<!-- brickkit:managed:end -->
