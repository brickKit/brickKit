# demo/caller

BrickKit 平台自测用的调用方组件，单个 Go 文件、只用标准库；同一个二进制既是 HTTP 服务，也是迁移命令。怎么用、边界和契约见 `BRICKKIT.md`；依赖、配置、部署见 `component.yaml`。

## 代码地图

| 路径 | 管什么 |
| --- | --- |
| `main.go` | 全部代码：读环境变量、四条路由、调用 demo/hello、迁移入口 `migrate`、JSON 日志、优雅退出 |
| `main_test.go` | 单元测试：`httptest` 打路由、假上游、迁移的三种结果 |
| `openapi.json` | 对外契约：四条接口的 OpenAPI 描述 |
| `component.yaml` | 组件清单：强依赖 demo/hello、可选依赖 demo/bus、`DATABASE_*` 与 `MIGRATION_SHOULD_FAIL` 配置项、`migration.command`、端口 8080 |
| `Dockerfile` | 两阶段构建：`golang:1.22-alpine` 编译出 /app/caller，`alpine:3.20` 里以 uid 10001 运行 |
| `.dockerignore` | 不把 `*_test.go` 带进构建上下文 |
| `go.mod` | 独立的 Go module（只用标准库），不参与 BrickKit 主 module 的构建 |

| 功能 | 从这里开始 | 然后 |
| --- | --- | --- |
| 强依赖调用 | `main.go` 的 `handleCall`、`fetchHello` | `TestCallUsesInjectedEndpoint`、`TestCallWithoutEndpointFails` |
| 可选依赖降级 | `main.go` 的 `newServerFromEnv`（`busEndpoint`）与 `handleStatus` | `TestOptionalDependencyDegradesGracefully`、`TestOptionalDependencyReportedWhenPresent` |
| 环境变量回显 | `main.go` 的 `platformEnvKeys` 与 `handleEnv` | `TestEnvEndpointEchoesInjectedVariables` |
| 迁移 | `main.go` 的 `runMode`（参数分派）与 `migrate` | `component.yaml` 的 `migration.command`，`TestRunModeRejectsUnknownArguments`、`TestMigrate*` 三条测试 |
| 健康检查 | `main.go` 的 `handleHealthz` | `TestHealthzDoesNotDependOnUpstream` |

## 构建与测试

```bash
go vet ./... && go test ./...          # 成功：ok  github.com/brickkit/demo-caller
go run .                                # 本地起在 :8080；没设 DEMO_HELLO_ENDPOINT 时 /api/v1/call 返回 424，/api/v1/status 报 degraded
go run . migrate                        # 不配 DATABASE_HOST 时打印 "migration finished" 并以 0 退出
docker build -t brickkit-demo/caller:1.0.0 .
brickkit lint --strict                  # 组件清单与这几份文档；成功时 0 warnings
```

`demo/hello` 的端口也固定在 8080，两个不能同时在宿主机上直接跑；要连着真上游验证，在一个声明了两者的项目里 `brickkit up`。测试不需要网络、数据库或 Docker：上游用 `httptest.NewServer` 模拟，"数据库不可达"用 `127.0.0.1:1`。在 BrickKit 仓库根目录，`make test-components` 连同其他自测组件一起跑 vet 与测试，`make demo-images` 构建镜像。

## 设计取舍

- **依赖 `demo/hello` 而且是强依赖**：要验证的就是平台把强依赖的地址注入为 `DEMO_HELLO_ENDPOINT`、靠 Docker / K8s 的 DNS 找到对方，不需要注册中心。组件之间是同步 HTTP 调用。
- **`demo/bus` 是可选依赖、而且从不真正调用**：要验证的是"可选依赖缺席时平台完全不注入变量、组件自行降级"，所以只看地址在不在、在 `/api/v1/status` 里报 `available` / `degraded`，用不着真的发事件。
- **迁移不建表**：平台要验证的是"迁移在主服务之前执行、失败能阻断"，而不是某张业务表长什么样。所以迁移只做两件可控的事：配了 `DATABASE_HOST` 就做一次 TCP 连通检查（证明配置注入是真的），`MIGRATION_SHOULD_FAIL=1` 时故意失败。
- **没配数据库时迁移直接成功**：迁移不该成为不用数据库的部署的阻塞项。
- **强依赖地址为空时返回 424 而不是假装正常**：错误信息点名缺失的变量，让注入出错时一眼看得出是哪一步。
- **只声明 `resources.requests`，不声明 `limits`**：理由写在 `component.yaml` 的注释里——"允许涨到多少"是部署方的判断，逐字段合并下组件写了 `limits.cpu` 项目就删不掉了。

## 易错点

| 不许 | 症状 | 原因 |
| --- | --- | --- |
| 让 `/healthz` 去探 `demo/hello` 或数据库 | 上游一抖，编排系统就把这个本身正常的容器杀掉重启 | 健康检查只查本进程；`TestHealthzDoesNotDependOnUpstream` 专门守这一条 |
| 假设 `DEMO_BUS_ENDPOINT` 一定存在、缺席时报错或崩溃 | 项目没装 `demo/bus` 时组件起不来，可选依赖的降级验证失效 | 平台对缺席的可选依赖完全不注入变量，必须用安全的方式读、自行降级 |
| 把 `DEMO_BUS_ENDPOINT` 从 `platformEnvKeys` 里删掉 | `TestEnvEndpointEchoesInjectedVariables` 失败；平台没法从回显里确认"没有注入" | 它的值为空正是"平台没注入"的证据 |
| 让迁移失败时仍以 0 退出（比如只打日志不 `os.Exit(1)`） | 迁移失败了主服务照样启动，"失败能阻断"的验证永远通过 | 平台只看迁移命令的退出码 |
| 没配 `DATABASE_HOST` 时让迁移失败 | 不用数据库的部署每次 `up` 都卡在迁移 | `TestMigrateSucceedsWithoutDatabase` 要求此时跳过 |
| 去掉 `upstreamTimeout`，用不带超时的请求调上游 | `demo/hello` 卡住时 `/api/v1/call` 一直挂着不返回 | 调上游的超时固定 3 秒，失败要尽快变成 502 |
| 让入口把不认识的参数当成"起服务" | `migration.command` 里 `migrate` 拼错一个字母，迁移容器就成了第二个永不退出的服务，部署一直卡着 | 迁移与主服务是同一个镜像；`runMode` 对不认识的参数以 2 退出，`TestRunModeRejectsUnknownArguments` 守着 |
| 改端口只改 `main.go` 的 `addr` | 容器起来了但连不上，健康检查一直失败 | `component.yaml` 的 `deployment.port`、`Dockerfile` 的 `EXPOSE` 与 `addr` 必须是同一个 8080 |

## 改代码前自查

1. 新增或改名的接口是否同步写进了 `openapi.json` 和 `BRICKKIT.md` 的契约索引？
2. 新增的配置项是否写进了 `component.yaml` 的 `configSchema`，需要回显的是否加进了 `platformEnvKeys`？
3. `/healthz` 是否仍然只返回常量，不碰 `demo/hello`、`demo/bus` 或数据库？
4. 迁移的三种结果（没配数据库成功、开关为 `1` 失败、数据库不可达失败）是否仍都以正确的退出码结束？
5. `component.yaml` 的 `migration.command` 是否仍与 `runMode` 认的参数（`migrate`）和镜像里的路径 `/app/caller` 对得上？新增入口参数时，`runMode` 是否仍对其余参数直接失败？
6. 是否仍然只用标准库？`Dockerfile` 只拷 `go.mod` 和 `*.go`，引入第三方依赖要同时补 `go.sum` 与拷贝步骤。
7. `go vet ./... && go test ./...` 是否通过，`brickkit lint --strict` 是否 0 warnings？

<!-- brickkit:managed:begin lang=zh -->
<!-- 由 brickkit 维护（init、add、remove、upgrade、skills update）：这对标记之间的改动会被覆盖 -->

## BrickKit

这是一个 BrickKit 组件：平台只读 `component.yaml`。它依赖的规则：

- `configSchema` 的键就是代码读的环境变量名。不能用保留名：`COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`，以及任何 `*_ENDPOINT`。
- 依赖写精确版本。依赖的地址以 `<ID>_ENDPOINT` 注入；缺席的可选依赖根本没有这个变量，读的时候要带兜底。
- `/healthz` 只查本进程，不查依赖。迁移命令用同一个镜像跑，遇到不认识的参数必须直接失败。
- `BRICKKIT.md` 会随版本进入每个使用它的项目，在那里是脱离仓库单独读的：跟代码一起改，不放相对链接。
- 发版：改 `metadata.version`，提交、推送，`brickkit release`。`brickkit lint` 会检查清单和这些文档。
- 完整规则在 `brickkit-component` 技能里（装了技能的项目或仓库根目录下的 `.claude/skills/brickkit-component/SKILL.md`；`brickkit skills update` 会装上）；参数问 `brickkit <命令> --help`。
<!-- brickkit:managed:end -->
