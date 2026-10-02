# authorization/rbac

Go 写的授权查询组件：权限 = 直接授予这个人的角色 ∪ 授予其部门的角色，部门取自 people/basic，结果缓存在可选的 Redis 或进程内。HTTP 与 gRPC 共用 8080。它也是 BrickKit 仓库自带的平台验证夹具：可选的外部系统（Redis）、单端口双协议、强依赖与缓存叠在一起时的降级行为都靠它验证。怎么用、边界和契约见 `BRICKKIT.md`；依赖、配置、部署见 `component.yaml`。

## 代码地图

| 路径 | 管什么 |
| --- | --- |
| `main.go` | 入口：参数解析（启动服务 / `migrate [down [n] \| reset]`）、读配置、连库、按配置选缓存实现、起服务 |
| `config.go` | 只从环境变量读配置（Redis 可选）、拼 DSN、JSON 日志与敏感字段打码 |
| `server.go` | 同一端口上 h2c + 按 Content-Type 把 gRPC 与 REST 分流，注册 gRPC 反射 |
| `service.go` | 业务逻辑：先查缓存 → 问 people/basic 要部门 → 查授权 → 写缓存；HTTP 与 gRPC 两个出口与错误映射 |
| `cache.go` | `Cache` 接口、内存实现、Redis 实现（键前缀、短超时、TTL、坏缓存当未命中） |
| `people.go` | people/basic 的 HTTP 客户端，区分"人不存在"与"依赖不可用" |
| `store.go` | `Store` 接口、`Role` / `Grant`、内存实现 |
| `postgres.go` | `Store` 的 PostgreSQL 实现（人与部门的授权一次查完） |
| `migrate.go` | 内嵌迁移脚本的执行、回退与 `schema_migrations` 记录 |
| `migrations/` | 有版本的 SQL：`0001_init` 三张表，`0002_seed_rbac` 样例角色与授权 |
| `proto/authorization/v1/authorization.proto` | gRPC 契约（发布为 api-contract 产物） |
| `gen/authorization/v1/` | 由 proto 生成的 Go 代码，不手改 |
| `openapi.json` | HTTP 文档（发布为 api-docs 产物，也被 `go:embed` 进二进制在运行时发出） |
| `Dockerfile` | 两阶段构建，运行期只有一个静态二进制，以 UID 10001 运行 |
| `component.yaml` | 组件契约 |
| `service_test.go` | 两条路径合并、排序去重、缓存命中与按人隔离、Redis 故障回源、people/basic 故障时的 503 与缓存兜底、两个协议一致、健康检查不越界 |
| `cache_test.go` | 缓存契约（内存实现始终跑，Redis 实现按环境变量跑） |
| `store_test.go` | 存储契约（内存实现始终跑，PostgreSQL 按环境变量跑）、迁移幂等、样例数据回退 |
| `component_test.go` | 配置、日志打码、参数解析、`component.yaml` 的声明与实现一致、Dockerfile 非 root、没有写死的地址 |
| `openapi_endpoint_test.go` | 运行时发出的文档就是仓库里那份 |

| 功能 | 从这里开始 | 然后 |
| --- | --- | --- |
| 改权限计算 | `service.go` 的 `resolve` / `buildPermissionSet` | `store.go`、`postgres.go`，`service_test.go` |
| 改缓存行为 | `cache.go` | `main.go` 的 `newCache`，`cache_test.go` |
| 加授权写接口 | `service.go` 的 `routes` | 写完调用 `invalidate` 让这个人的缓存失效，`openapi.json` |
| 改 gRPC 接口 | `proto/authorization/v1/authorization.proto` | 重新生成 `gen/authorization/v1/`，`service.go` 的 gRPC 出口 |
| 改表结构或样例数据 | `migrations/` | 新增一对 `.up.sql` / `.down.sql`，`store_test.go` |
| 改配置项 | `config.go` | `component.yaml` 的 `configSchema`，`BRICKKIT.md` 的配置指南 |

## 构建与测试

```bash
go vet ./... && go test ./...      # 单元测试：内存实现，不需要任何外部服务
```

全部 `ok` 即通过；需要真库或真 Redis 的用例会显示被跳过（"未设置 RBAC_TEST_DATABASE_URL" / "未设置 RBAC_TEST_REDIS_ADDR"）。

含 PostgreSQL 与 Redis 的契约测试：

```bash
docker exec -i my-postgres psql -U postgres -c "CREATE DATABASE brickkit_rbac"
RBAC_TEST_DATABASE_URL="postgres://postgres:PASSWORD@localhost:5432/brickkit_rbac?sslmode=disable" \
RBAC_TEST_REDIS_ADDR="localhost:6379" \
  go test ./...
```

或在仓库根目录跑 `make test-components-integration`（读根目录 `.env`，库不存在时会代建）。

其他（都在仓库根目录执行）：

```bash
make proto-authorization                                                                    # 改了 .proto 之后重新生成 gen/
docker build -t brickkit-demo/authorization-rbac:1.0.0 tests/components/authorization-rbac   # 镜像，tag 与 component.yaml 一致
make check-components                                                                       # 每个测试组件都过 brickkit lint --strict
```

手工跑迁移（`brickkit up` 会在启动前自动跑 `migrate`；`.env` 里放 `DATABASE_*` 与 `PEOPLE_BASIC_ENDPOINT`，读配置时它们都是必填）：

```bash
docker run --rm --env-file .env brickkit-demo/authorization-rbac:1.0.0 migrate          # 向上，幂等
docker run --rm --env-file .env brickkit-demo/authorization-rbac:1.0.0 migrate down     # 回退最近 1 个
docker run --rm --env-file .env brickkit-demo/authorization-rbac:1.0.0 migrate reset    # 全部回退
```

## 设计取舍

- **强依赖 people/basic，而不是 department/tree**：需要的是"这个人在哪个部门"，那是人员数据；部门树本身不参与计算，所以部门授权不沿部门树向下传。
- **Redis 是加速器，不是数据源，因此是配置项而不是组件依赖**：它挂了若报错，等于让一个可选的基础设施变成单点，整个系统的每一次权限检查都会失败。没配时改用进程内缓存，单副本完全够用。
- **people/basic 挂了且缓存未命中时不做部分降级**：只有直接授予的角色、缺部门角色的残缺权限集会被调用方当成完整的用，比返回错误更危险。缓存命中则照常返回，people/basic 的短暂抖动不至于立刻变成全系统的鉴权失败。
- **check 没有权限回 200 而不是 403**：调用方要能分清"我没权限调这个接口"和"查到了，答案是否"。
- **一份逻辑、两个协议出口**：两边各写一遍权限计算，迟早出现"HTTP 说有、gRPC 说没有"，`service_test.go` 里有用例锁住两者一致。
- **结果排序去重**：调用方常直接比对数组；顺序随 map 遍历变的话，同样的输入得到不同输出，缓存里也会存下两份语义相同的值。
- **人与部门的授权一次查完**：授权判断是最高频的调用，少一次往返就是每个请求都少一次。`reason` 只在确实有权限时才多查一次。
- **授权指向不存在的角色时跳过而不是报错**：角色被删、授权还在，是运维过程中正常的中间状态。
- **`subject_id` 不加外键**：personId 与 departmentId 是别的组件、别的库里的数据。
- **单端口双协议**：h2c 承载明文 HTTP/2，`application/grpc` 交给 gRPC、其余交给 REST；`component.yaml` 只声明一个 `deployment.port`，健康检查、`_ENDPOINT` 注入、K8s Service 都只认这一个端口。开 gRPC 反射，`grpcurl` 不带 `.proto` 也能排障。
- **迁移用 `go:embed` 打进二进制，`schema_migrations` 主键是 `(component_id, version)`**：迁移与服务永远同一个版本；版本号是每个组件各自的，共用一个库时只按版本记会让先跑的组件把后跑的顶掉。
- **运行时也发出 `/openapi.json`**：发布的产物是发布那一刻的快照，`infra/api-docs` 之类的工具要的是此刻跑着的服务长什么样。
- **`resources` 只写 requests 不写 limits**：允许它涨到多少是部署方的判断，而且逐字段合并意味着这里写了 limits，用它的项目就删不掉了。

## 易错点

| 不许 | 症状 | 原因 |
| --- | --- | --- |
| 把 Redis 错误往上抛 | Redis 一抖，所有权限检查都失败 | 缓存错误一律吞掉、回源；`TestCacheFailureDegradesToSource` 锁住这一条 |
| 缓存键去掉组件前缀 | 偶发"权限不对"，和别的组件共用 Redis 时才出现 | 两个组件用了同一个键名会互相覆盖 |
| 缓存键漏了 personId | 所有人共用同一份权限，而功能测试全都会通过 | 权限系统里最严重的一类事故；`TestCacheIsPerPerson` |
| Redis 写入不带 TTL | 漏一次失效，一份错的权限就永远留在缓存里 | TTL 是主动失效之外的最后兜底 |
| 缓存内容解析失败时报错 | 一条坏缓存把这个人永久挡在门外 | 坏缓存当作未命中回源，并顺手删掉 |
| Redis 超时设得比回源还长 | 缓存比不用缓存还慢 | 单次操作 500 毫秒，连不上就快速失败 |
| people/basic 故障时返回只含直接角色的权限 | 调用方拿残缺权限当完整的用，有权限的人被拒或走错分支 | 不做部分降级；`TestPeopleOutageFailsClosedOnCacheMiss` |
| 把"查到为空"和"存储不可用"混成一种 | 数据库一抖就变成"所有人都没有权限"，一次静默的全局降权 | `errStorageUnavailable` 映射到 503，空结果才是"没有权限" |
| 加了授权写接口却不调 `invalidate` | 被收回权限的人在 TTL 到期前仍然畅通无阻 | 这是安全事故，不是延迟问题 |
| 让 `/healthz` 碰 Redis、查库或调 people/basic | Redis 一抖，编排系统把所有本身正常的副本杀掉重启 | 健康检查只回答"本进程还活着吗" |
| 调用 people/basic 不设超时 | people/basic 卡住时本组件连接耗尽，连 `/healthz` 都挤不进来 | 一个组件的慢拖垮两个组件；超时 5 秒 |
| 让入口把不认识的参数当成"启动服务" | 迁移容器永不退出，项目卡在 Created，日志却写着"组件已就绪" | 迁移容器与主容器是同一个镜像，只靠参数区分 |

## 改代码前自查

- 动了权限计算：直接 + 部门两条路径仍然合并，结果排序去重，空权限是 `[]`（`service_test.go` 的前几个用例）。
- 动了缓存：键仍带前缀与 personId、Redis 写入仍带 TTL、Redis 故障仍回源（`cache_test.go`、`TestCacheIsPerPerson`、`TestCacheFailureDegradesToSource`）。
- 动了依赖调用：people/basic 故障时缓存未命中仍是 503、命中仍照常返回（`TestPeopleOutageFailsClosedOnCacheMiss`、`TestPeopleOutageServedFromCache`）。
- 动了任一出口：HTTP 与 gRPC 结果一致（`TestGRPCAndHTTPAgree`、`TestGRPCCheckMatchesHTTP`），check 没有权限仍是 200（`TestCheckDeniedIsNot403`）。
- 动了样例数据：仍对得上 people/basic 的 `p-001`～`p-004` 与 department/tree 的 `d-tech`、`d-hr`，`BRICKKIT.md` 的样例表跟着改。
- 动了 `.proto` 或 HTTP 接口：重新生成了 `gen/`，`openapi.json` 与 `BRICKKIT.md` 的契约索引一起改；`erp/backend` 用这份 proto 生成了客户端。

<!-- brickkit:managed:begin lang=zh -->
<!-- 由 brickkit 维护（init、add、remove、upgrade、skills update）：这对标记之间的改动会被覆盖 -->

## BrickKit

这是一个 BrickKit 组件：平台只读 `component.yaml`。它依赖的规则：

- `configSchema` 的键就是代码读的环境变量名。不能用保留名：`COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`，以及任何 `*_ENDPOINT`。
- 依赖写精确版本。依赖的地址以 `<ID>_ENDPOINT` 注入；缺席的可选依赖根本没有这个变量，读的时候要带兜底。
- `/healthz` 只查本进程，不查依赖。迁移命令用同一个镜像跑，遇到不认识的参数必须直接失败。
- `BRICKKIT.md` 会随版本进入每个使用它的项目，在那里是脱离仓库单独读的：跟代码一起改，不放相对链接。
- 发版：改 `metadata.version`，提交、推送，`brickkit release`。`brickkit lint` 会检查清单和这些文档——在项目里、在这个目录下跑，只查这个组件（`--all` 查整个项目）。
- 完整规则在 `brickkit-component` 技能里（装了技能的项目或仓库根目录下的 `.claude/skills/brickkit-component/SKILL.md`；`brickkit skills update` 会装上）；参数问 `brickkit <命令> --help`。
<!-- brickkit:managed:end -->
