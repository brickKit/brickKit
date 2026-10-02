# people/basic

Python（FastAPI + grpcio）写的人员查询组件：HTTP 在 8080，gRPC 在 9090，强依赖 `department/tree` 补部门名，可选依赖 `infra/redis-event-bus` 发事件。它也是 BrickKit 仓库自带的平台验证夹具：`extraPorts` 双端口、跨语言调用（Python 客户端调 Go 服务端）、强弱依赖在故障时的不同表现都靠它验证。怎么用、边界和契约见 `BRICKKIT.md`；依赖、配置、部署见 `component.yaml`。

## 代码地图

| 路径 | 管什么 |
| --- | --- |
| `app/main.py` | 入口：参数解析（启动服务 / `migrate [down [n] \| reset]`）、读配置、连库、同时起 HTTP 与 gRPC |
| `app/config.py` | 只从环境变量读配置、两个端口常量、JSON 日志与敏感字段打码 |
| `app/service.py` | 业务逻辑：查人、补部门名（单次请求内去重）、发事件；两个协议共用 |
| `app/http_api.py` | FastAPI 路由与错误到状态码的映射，含健康检查 |
| `app/grpc_api.py` | gRPC 服务实现（只做协议转换）与反射 |
| `app/department.py` | department/tree 的 gRPC 客户端（强依赖），不做跨请求缓存 |
| `app/events.py` | infra/redis-event-bus 的 HTTP 客户端与它缺席时的空实现（可选依赖） |
| `app/store.py` | `Store` 接口、内存实现与 PostgreSQL 实现 |
| `app/migrate.py` | 迁移的执行、回退与 `schema_migrations` 记录 |
| `migrations/` | 有版本的 SQL：`0001_init` 建表，`0002_seed_people` 样例人员 |
| `proto/people/v1/people.proto` | 本组件的 gRPC 契约（发布为 api-contract 产物） |
| `proto/vendor/department/v1/department.proto` | 从 department/tree 的 api-contract 产物拷来的契约，用来生成客户端 |
| `openapi.json` | 由 FastAPI 导出的 HTTP 文档（发布为 api-docs 产物），不手改 |
| `dump_openapi.py` | 导出 `openapi.json` 的脚本 |
| `Dockerfile` | 多阶段：codegen 生成 gRPC 代码 → base → test（跑 pytest）/ runtime（装 curl、UID 10001） |
| `requirements.txt` | 运行期依赖，版本钉死 |
| `requirements-dev.txt` | 测试与代码生成依赖，不进运行期镜像 |
| `tests/` | pytest：`test_args.py`、`test_component.py`、`test_service.py`、`test_events.py`、`test_migrate.py` |
| `component.yaml` | 组件契约 |

| 功能 | 从这里开始 | 然后 |
| --- | --- | --- |
| 加或改一个查询 | `app/service.py` | `app/http_api.py` 与 `app/grpc_api.py` 两个出口，`proto/people/v1/people.proto`，`make openapi-people` |
| 改部门名的获取 | `app/department.py` | `proto/vendor/department/v1/department.proto`，`tests/test_service.py` 的强依赖用例 |
| 改事件发布 | `app/events.py` | `app/service.py` 的 `_publish_safely`，`tests/test_events.py`（线上的请求体），`BRICKKIT.md` 的契约索引 |
| 改表结构或样例数据 | `migrations/` | 新增一对 `.up.sql` / `.down.sql`，`tests/test_migrate.py` |
| 改配置项 | `app/config.py` | `component.yaml` 的 `configSchema`，`BRICKKIT.md` 的配置指南 |

## 构建与测试

宿主机通常没有可用的 Python 环境，测试跑在容器里（版本固定、可复现）。在本目录：

```bash
docker build --target test -t people-basic-test . && docker run --rm people-basic-test
```

pytest 以 `-q` 运行，最后一行是 `N passed, M skipped` 且没有 `failed` 即通过；跳过的是需要真库的迁移用例（"未设置 PEOPLE_TEST_DATABASE_URL"）。

含 PostgreSQL 的迁移集成测试：

```bash
docker run --rm -e PEOPLE_TEST_DATABASE_URL="postgresql://postgres:PASSWORD@<PostgreSQL 的 IP>:5432/brickkit_people" \
  people-basic-test
```

或在仓库根目录跑 `make test-components-integration`（读根目录 `.env`，库不存在时会代建，自动取 `my-postgres` 容器的 IP）。

其他：

```bash
docker build -t brickkit-demo/people-basic:1.0.0 .   # 运行期镜像，tag 与 component.yaml 一致
make openapi-people                                   # 仓库根目录：改了 HTTP 接口后重新导出 openapi.json
make check-components                                 # 仓库根目录：每个测试组件都过 brickkit lint --strict
```

手工跑迁移（`brickkit up` 会在启动前自动跑 `migrate`；`.env` 里放 `DATABASE_*` 与 `DEPARTMENT_TREE_ENDPOINT`，后者在读配置时就是必填）：

```bash
docker run --rm --env-file .env brickkit-demo/people-basic:1.0.0 migrate          # 向上，幂等
docker run --rm --env-file .env brickkit-demo/people-basic:1.0.0 migrate down     # 回退最近 1 个
docker run --rm --env-file .env brickkit-demo/people-basic:1.0.0 migrate down 3   # 回退最近 3 个
docker run --rm --env-file .env brickkit-demo/people-basic:1.0.0 migrate reset    # 全部回退
```

`down` / `reset` 是给开发与测试用的；生产环境的结构问题用一个新的 up 迁移去修（先兼容后迁移、不做破坏性操作）。

## 设计取舍

- **gRPC 单独占 9090**：Python 的 grpcio 不能与 HTTP 共用端口，所以在 `component.yaml` 里用 `extraPorts` 声明，平台据此给调用方注入 `PEOPLE_BASIC_GRPC_ENDPOINT`。
- **部门名不存副本**（数据自治）：每次去问 `department/tree`，部门改名后这里立刻跟着变。
- **不做跨请求缓存**：最初缓存过部门名，真容器验证时把 `department/tree` 停掉，接口照样返回 200——缓存盖住了"强依赖已经挂了"，也让"部门改名立刻生效"变成空话。列表的 N+1 改由单次请求内按部门去重解决，既不重复调用也不会让数据变旧。
- **强依赖挂了如实报 503，不返回缺部门名的人员**：假装成功会让调用方以为"这个人真的没有部门"。部门被删（`NOT_FOUND`）则不是依赖故障，人员照常可读。
- **可选依赖缺席或出错都不影响主流程**：这正是"弱"的含义；变量缺席是正常情况，用取不到就降级的方式读。
- **契约跨语言复用**：`department/tree` 把 proto 作为 api-contract 发布，本组件 `brickkit add` 拿到后 vendored 到 `proto/vendor/`，构建时生成客户端；Go 写的服务端、Python 写的客户端靠同一份 proto 对话。
- **`openapi.json` 用生成而不是手写**：手写的文档迟早和代码对不上。
- **测试在容器里跑**：宿主机没有可用的 Python 包管理环境，`--target test` 那一层带上 pytest 与 grpcio-tools。
- **`resources` 只写 requests 不写 limits**：允许它涨到多少是部署方的判断，而且逐字段合并意味着这里写了 limits，用它的项目就删不掉了。

## 易错点

| 不许 | 症状 | 原因 |
| --- | --- | --- |
| 运行期镜像里去掉 curl | 组件明明跑得好好的，平台却判它 unhealthy，依赖方永远等不到它（真跑起来撞到过） | 健康检查在容器内部执行，`python:slim` 既没有 wget 也没有 curl |
| 给部门名加跨请求缓存 | `department/tree` 停了接口仍回 200；部门改名不重启就看不到 | 缓存掩盖了强依赖故障，`tests/test_service.py` 里有用例锁住"不跨请求缓存" |
| 用 `os.environ["INFRA_REDIS_EVENT_BUS_ENDPOINT"]` 读可选依赖 | 事件总线没装时组件启动就 `KeyError` 崩溃 | 可选依赖缺席时平台完全不注入这个变量，要用 `get()` |
| 把 `DEPARTMENT_TREE_ENDPOINT` 原样交给 grpc | 连不上 `department/tree` | 注入的值形如 `http://department-tree-1-0-0:8080`，gRPC 要 `host:port`，要先去掉 scheme |
| 去掉 Dockerfile 里改写生成代码导入的 `sed` | 启动时 `ModuleNotFoundError` | grpc 生成的 stub 用绝对导入（`people.v1`、`vendor.department.v1`），要改成 `gen.*` 才能作为包引用 |
| 手改 `openapi.json` | 下次 `make openapi-people` 覆盖掉手改的内容，或文档与代码对不上 | 它是 FastAPI 导出的 |
| 只改了 `department/tree` 的 proto 没同步 vendored 拷贝 | 本组件按旧契约调用 | `proto/vendor/department/v1/department.proto` 是拷贝，不会自动更新 |
| 让入口把不认识的参数当成"启动服务" | 迁移容器永不退出，项目卡在 Created，日志却写着"组件已就绪" | 迁移容器与主容器是同一个镜像；`parse_args` 是纯函数，先校验参数再读环境变量、连库 |
| 按自己的想法给事件总线拼请求体（比如 `{"topic": …, "payload": …}`） | 查询照常 200，日志里只有"事件发布失败，已跳过"，总线上一条 `people.person.viewed` 都没有 | 总线要求非空的 `type`，形状不对回 422；可选依赖的失败只记警告，没有别处会发现，`tests/test_events.py` 锁住了形状 |
| 改已经执行过的迁移文件 | 改动在已有的库上永远不生效 | 版本已记进 `schema_migrations`，不会重跑；只能新增一对文件 |

## 改代码前自查

- 动了查询：HTTP 与 gRPC 两个出口结果一致（`tests/test_component.py` 的 `test_grpc_and_http_agree`），改了 HTTP 接口就重新导出 `openapi.json`。
- 动了部门名获取：强依赖不可用仍回 503、部门被删仍返回人员、仍然不跨请求缓存（`tests/test_service.py`）。
- 动了事件：事件总线缺席或出错时请求仍是 200；请求体仍是总线收的形状（非空的 `type`，`tests/test_events.py`）；事件名或字段变了要同步 `BRICKKIT.md` 的契约索引。
- 动了 `Dockerfile`：运行期仍装 curl、仍以 UID 10001 运行、生成代码的导入改写仍在。
- 动了表结构或样例数据：新增了一对迁移文件，没改旧的；样例人员的 `department_id` 要对得上 `department/tree` 的样例部门，`auth/password-login` 与 `authorization/rbac` 的样例数据也引用 `p-001`～`p-004`。
- 动了配置项：`app/config.py`、`component.yaml` 的 `configSchema`、`BRICKKIT.md` 的配置指南三处一致。

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
