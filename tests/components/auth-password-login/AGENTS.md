# auth/password-login

Go 写的口令登录组件：校验用户名口令、签发并校验 HS256 令牌，强依赖 `people/basic` 确认主体仍在。它也是 BrickKit 仓库自带的平台验证夹具，验证的是"强依赖别人、自己却没有可展示数据"的组件形态——全部价值在于依赖正常与异常时能不能给出正确的判断。怎么用、边界和契约见 `BRICKKIT.md`；依赖、配置、部署见 `component.yaml`。

## 代码地图

| 路径 | 管什么 |
| --- | --- |
| `main.go` | 入口：参数解析（启动服务 / `migrate [down [n] \| reset]`）、读配置、连库、建签发器、起服务 |
| `config.go` | 只从环境变量读配置、拼 DSN、JSON 日志与敏感字段打码 |
| `service.go` | HTTP 路由与业务：登录（查凭据 → 验口令 → 问 people/basic）、校验令牌、错误到状态码的映射 |
| `password.go` | PBKDF2-HMAC-SHA256 口令哈希与定时安全比较 |
| `token.go` | HS256 令牌的签发与解析，密钥长度下限 |
| `people.go` | people/basic 的 HTTP 客户端，区分"人不在了"与"依赖不可用" |
| `store.go` | `Store` 接口、语义错误、内存实现 |
| `postgres.go` | `Store` 的 PostgreSQL 实现 |
| `migrate.go` | 内嵌迁移脚本的执行、回退与 `schema_migrations` 记录 |
| `migrations/` | 有版本的 SQL：`0001_init` 建 `credentials` 表，`0002_seed_credentials` 样例账号 |
| `openapi.json` | HTTP 文档（发布为 api-docs 产物，也被 `go:embed` 进二进制在运行时发出） |
| `Dockerfile` | 两阶段构建，运行期只有一个静态二进制，以 UID 10001 运行 |
| `component.yaml` | 组件契约 |
| `service_test.go` | 登录、校验、401/503 的区分、用户是否存在不可分辨、健康检查不越界，以及口令哈希（加盐、畸形哈希被拒） |
| `token_test.go` | 令牌：必需的 claims、不带秘密、只认 HS256、拒绝篡改与过期、弱密钥与非正 TTL 被拒 |
| `config_test.go` | 配置：一次报出全部缺失、不退化到默认值、DSN 特殊字符、日志打码、不认识的参数报错 |
| `component_test.go` | `component.yaml` 的声明与实现一致、Dockerfile 非 root、没有写死的地址 |
| `store_test.go` | 存储契约（内存实现始终跑，PostgreSQL 按环境变量跑）、迁移幂等、样例账号与回退 |
| `openapi_endpoint_test.go` | 运行时发出的文档就是仓库里那份 |

| 功能 | 从这里开始 | 然后 |
| --- | --- | --- |
| 改登录流程 | `service.go` 的 `authenticate` | `people.go`、`store.go`，`service_test.go` |
| 改状态码映射 | `service.go` 的 `writeAuthError` | `openapi.json`，`BRICKKIT.md` 的状态码表 |
| 改令牌内容或算法 | `token.go` | `token_test.go`，`BRICKKIT.md` 的契约索引 |
| 改口令哈希参数 | `password.go` | 新哈希带着参数前缀，旧哈希仍按自己的参数验 |
| 加接口（如改口令） | `service.go` 的 `routes` | `store.go` 的 `Upsert` 已经有了，`openapi.json` |
| 改表结构或样例数据 | `migrations/` | 新增一对 `.up.sql` / `.down.sql`，`store_test.go` |
| 改配置项 | `config.go` | `component.yaml` 的 `configSchema`，`BRICKKIT.md` 的配置指南 |

## 构建与测试

```bash
go vet ./... && go test ./...      # 单元测试：内存实现，不需要数据库
```

全部 `ok` 即通过；需要真库的用例会显示被跳过（"未设置 AUTH_TEST_DATABASE_URL"）。600000 轮的哈希让这一组测试要跑几秒。

含 PostgreSQL 的存储契约与迁移测试：

```bash
docker exec -i my-postgres psql -U postgres -c "CREATE DATABASE brickkit_auth"
AUTH_TEST_DATABASE_URL="postgres://postgres:PASSWORD@localhost:5432/brickkit_auth?sslmode=disable" \
  go test ./...
```

或在仓库根目录跑 `make test-components`（全部测试组件的单元测试）/ `make test-components-integration`（读根目录 `.env`，库不存在时会代建）。

其他（都在仓库根目录执行）：

```bash
docker build -t brickkit-demo/auth-password-login:1.0.0 tests/components/auth-password-login   # 镜像，tag 与 component.yaml 一致
make check-components                                                                          # 每个测试组件都过 brickkit lint --strict
```

手工跑迁移（`brickkit up` 会在启动前自动跑 `migrate`；`.env` 里放 `DATABASE_*`、`JWT_SECRET`、`PEOPLE_BASIC_ENDPOINT`，读配置时它们都是必填）：

```bash
docker run --rm --env-file .env brickkit-demo/auth-password-login:1.0.0 migrate          # 向上，幂等
docker run --rm --env-file .env brickkit-demo/auth-password-login:1.0.0 migrate down     # 回退最近 1 个
docker run --rm --env-file .env brickkit-demo/auth-password-login:1.0.0 migrate reset    # 全部回退
```

## 设计取舍

- **身份不在本组件存第二份**：登录时向 `people/basic` 现取。主体的存废由人员系统说了算，也不会出现两份会漂移的身份数据，部门调整只改一处。代价是多一次网络调用，以及 `people/basic` 挂掉时登录不可用——这正是强依赖的含义，所以如实报 503，而不是假装成认证失败。
- **提供 `/api/v1/verify`**：令牌用 HS256 签，密钥只有本组件有；没有这个端点，签出去的令牌对下游（`erp/backend`、`authorization/rbac`）就是一串不可用的字符串。代价是令牌无法离线验证。
- **PBKDF2-HMAC-SHA256，600000 轮（OWASP 2023 建议值），16 字节随机盐**：口令哈希要慢，快的哈希意味着一张显卡一秒能试上百亿次。每行盐不同，所以样例的四个账号口令相同、哈希却各不一样，否则一眼就能看出"这几个人用了同一个密码"。存储格式 `pbkdf2-sha256$<迭代次数>$<盐>$<派生密钥>` 带上算法与参数，将来换算法时旧哈希还认得出该怎么验。
- **比较用 `subtle.ConstantTimeCompare`**：普通的 `==` 在第一个不同的字节处返回，攻击者据此可以逐字节把哈希试出来。
- **用户不存在时也算一遍哈希（`dummyHash`）**：否则"秒回"与"算了 600000 轮"的时间差本身就泄露了用户是否存在。
- **只接受 HS256，不看令牌自称的算法**：那正是 `alg=none` 与算法混淆攻击的入口（RFC 8725 §3.1）。
- **令牌里只放变动很慢的信息**：令牌签出去就改不了，放频繁变动的东西等于发出一堆很快就不准的快照。
- **弱密钥挡在 `newTokenIssuer` 里、启动即失败**：一个用弱密钥跑起来的认证组件表面上一切正常，只有被攻破时才会发现，宁可根本起不来。
- **日志在出口统一打码**：认证组件出错时最想打印的就是请求体，而那里面正好是明文口令；靠"记得别写"不可靠。
- **错误响应不带底层原因**：把 `pq: connection refused` 透出去既帮不上调用方，又把内部拓扑告诉了外面；verify 也不说是过期还是签名错，那等于告诉伪造者还差哪一步。
- **`resources` 只写 requests 不写 limits**：允许它涨到多少是部署方的判断，而且逐字段合并意味着这里写了 limits，用它的项目就删不掉了。

## 易错点

| 不许 | 症状 | 原因 |
| --- | --- | --- |
| 给 `JWT_SECRET` 加默认值 | 一切看起来正常，但所有部署共用同一把钥匙，谁都能签出管理员令牌 | 令牌的可信完全取决于密钥只有这一处部署知道 |
| 把 `people/basic` 不可用映射成 401 | 使用者在自己的密码上白折腾，真正坏的依赖没人去查 | 401 是"你是谁我不认"，503 是"我这边有问题"；`people.go` 把 404 与其余失败分成两个错误 |
| 让"用户不存在"与"口令错误"在状态码、文案、字段或耗时上有任何差别 | 用户名字典能被筛成"这些账号真实存在"，撞库的第一步 | `errInvalidCredentials` 是唯一的对外错误，不存在的用户也走一遍 `dummyHash` |
| 解析令牌时信任它头部的 `alg` | `alg=none` 或算法混淆的伪造令牌被接受 | `token.go` 定死了唯一接受的签名方法 |
| 把秘密放进令牌载荷 | 任何拿到令牌的人都能读到 | 载荷只是 base64，不是加密 |
| 用 `migrate down 1` 清样例账号 | 下次 `brickkit up` 后账号又回来了 | 平台每次启动前都跑 `migrate`，被回退的版本会重新执行；清账号要删行 |
| 让 `/healthz` 查库或调 `people/basic` | 依赖一抖，这个本身正常的容器被杀掉重启，故障扩散 | 健康检查只回答"本进程还活着吗" |
| 让入口把不认识的参数当成"启动服务" | 迁移容器永不退出，项目卡在 Created，日志却写着"组件已就绪" | 迁移容器与主容器是同一个镜像，只靠参数区分 |
| 改已经执行过的迁移文件 | 改动在已有的库上永远不生效 | 版本已记进 `schema_migrations`，不会重跑；只能新增一对文件 |

## 改代码前自查

- 动了登录：用户不存在、口令错、人已不在 `people/basic` 三种情况仍返回完全相同的 401（`TestLoginDoesNotRevealWhetherUserExists`、`TestLoginRejectsWhenPersonGone`）。
- 动了错误映射：数据库或 `people/basic` 故障仍是 503 而不是 401（`TestLoginReportsDependencyOutageAsUnavailable`、`TestStorageFailureIsNotAuthFailure`）。
- 动了令牌：仍只认 HS256、载荷里没有口令与哈希、弱密钥与非正 TTL 仍被拒（`token_test.go`）。
- 动了日志或响应：口令、哈希、令牌、密钥不出现在日志与响应里（`TestLoggerRedactsSecrets`、`TestLoginResponseNeverLeaksHash`）。
- 动了样例账号或口令：`BRICKKIT.md` 的样例账号表与口令 `demo-password` 跟着改，`store_test.go` 也验这个口令；`person_id` 要对得上 `people/basic` 的样例人员。
- 动了接口或状态码：`openapi.json` 与 `BRICKKIT.md` 的契约索引一起改。

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
