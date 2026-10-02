# infra/api-docs

把各组件的 API 文档聚合到一个页面的展示组件（Go + 一个静态页面）：OpenAPI 走 `/openapi.json`，gRPC 走 Reflection。它是 BrickKit 平台自测组件里唯一一个**全部依赖都是弱依赖**的，验证的正是"文档入口不该因为某个业务组件没装就打不开"。怎么用、边界和契约见 `BRICKKIT.md`；依赖、配置、部署见 `component.yaml`。

## 代码地图

| 路径 | 管什么 |
| --- | --- |
| `main.go` | 入口：读配置、建 `Discoverer`、起 HTTP 服务；拒绝任何命令行参数；`WEB_ROOT` 可改静态页面目录 |
| `config.go` | `aggregated` 聚合清单（组件 ID → 主端口的环境变量名）、`grpcEnvVar()`（名为 grpc 的额外端口的变量名）、`cacheTTL`、JSON 日志与敏感字段打码 |
| `discovery.go` | 并发探测：OpenAPI 文档抓取、gRPC Reflection 列服务与方法（`Target.grpcAddress()` 选端口）、四种状态的判定、`grpcTarget()` |
| `descriptor.go` | 从 Reflection 返回的 FileDescriptorProto 里解析出某个服务的方法名 |
| `service.go` | HTTP 路由：健康检查、聚合状态、OpenAPI 代理、静态页面；探测结果缓存 |
| `web/index.html` | 首页：聚合状态表 + Swagger UI（只把有 OpenAPI 的组件喂给它） |
| `web/` | 静态页面目录；镜像里额外多一个 swagger-ui 子目录，由 Dockerfile 从官方镜像拷入，仓库里没有 |
| `service_test.go` | 全部测试：httptest 替身组件、四种状态、代理、地址不外泄、健康检查、缓存与过期、无必需配置、额外端口上的 gRPC |
| `Dockerfile` | 三段：swagger-ui 镜像取静态资源 → golang 编译 → alpine 运行期，UID 10001 |
| `component.yaml` | 组件契约：六个弱依赖、配置项、部署与健康检查 |

| 功能 | 从这里开始 | 然后 |
| --- | --- | --- |
| 多聚合一个组件 | `component.yaml`（加一条 `optional: true` 的依赖） | `config.go` 的 `aggregated` 加同一个组件，`service_test.go` |
| 改状态判定或探测超时 | `discovery.go` 的 `probe` | `service_test.go` 的状态用例 |
| 改 gRPC 方法的解析 | `discovery.go` 的 `listMethods` | `descriptor.go` |
| 改缓存时长 | `config.go` 的 `cacheTTL` | `service.go` 的 `sources`，`service_test.go` 的缓存用例 |
| 改页面 | `web/index.html` | `service.go` 的 `handleSources`（页面读的就是它） |
| 升级 Swagger UI | `Dockerfile` 的 `FROM swaggerapi/swagger-ui:...` | `web/index.html` 引用的三个文件名 |

## 构建与测试

```bash
go vet ./... && go test ./...   # 不需要任何外部服务：目标组件都有 httptest 替身
```

成功的样子：`ok  github.com/brickkit/components/infra-api-docs`。

```bash
docker build -t brickkit-demo/infra-api-docs:1.0.0 .   # 镜像名与 component.yaml 的 deployment.image 一致
```

单独跑起来（一个依赖地址都不设也能启动）：

```bash
WEB_ROOT=./web go run .
curl -s localhost:8080/healthz          # → {"status":"ok"}
curl -s localhost:8080/api/v1/sources   # 六个组件全是 absent
```

成功时 stdout 有一条 JSON 日志 `组件已就绪`，其中写着已安装的聚合目标数。本地跑时 `web/swagger-ui/` 不存在，Swagger UI 渲染不出来，状态表与 API 不受影响；完整页面要用镜像跑。仓库根目录的 `make test-components` 会连同其他自测组件一起跑本组件的 `go vet` 与 `go test`。

## 设计取舍

- **全部依赖都是弱依赖、没有任何必需配置**：文档入口不该因为某个业务组件没装就打不开，而且业务组件全挂的时候正是最需要看文档的时候。把任何一个列成必需，就等于要求使用者把六个组件全装上才能看文档。
- **两条发现路径**：OpenAPI 取 `/openapi.json`（FastAPI 之类的框架自带，BrickKit 的 Go 自测组件也照这个路径提供）；gRPC 用 Reflection，不必预先存一堆 `.proto`，组件升级加了新方法这里自动跟上——`grpcurl` 用的是同一套机制。gRPC 的地址从平台的命名规则推出来（`<ID>_GRPC_ENDPOINT`，名为 `grpc` 的额外端口），不在 `aggregated` 里另写一列：新聚合一个把 gRPC 开在额外端口上的组件时不用多改一处。
- **四种状态分开**：`ok`、`absent`、`unreachable`、`no-docs` 对应四种不同的处置；混成一种，使用者只能对着空页面猜。`/api/v1/sources` 本身就是排障工具。
- **由本组件代理 OpenAPI，不让浏览器直连**：那些组件默认不暴露端口，浏览器连不上，连得上也会撞跨域。
- **响应里不带组件的内部地址**：那等于把内网结构告诉任何能打开这个页面的人（`Source.Endpoint` 标了 `json:"-"`）。
- **Swagger UI 从镜像拷，不从 CDN 加载**：页面很可能跑在内网甚至气隙环境里，指向公网 CDN 的 `<script>` 会让页面永远转圈，症状看起来像"文档组件坏了"。版本钉死才可复现。
- **探测结果缓存 30 秒、并发探测、单次探测 3 秒超时**：每次刷新都去探六个组件，一个卡住的上游会让页面很慢，而 API 文档几乎不会在几十秒内变；缓存必须会过期，否则新上线的组件永远看不到。每个目标各自捕获错误，一个出问题最坏只是它自己显示成不可用。
- **方法名用真正的描述符解析**：在字节里找字符串看起来能跑，但方法名恰好出现在别的字段里就会多列一个不存在的方法，比少列一个更误导人。
- **不需要数据库**：探测结果放在内存里，缓存 30 秒就够；因此也没有 `migration`。
- **只声明 `requests` 不声明 `limits`**：允许它涨到多少是部署方的判断；这里写了 `limits`，使用方就删不掉了。

## 易错点

| 不许 | 症状 | 原因 |
| --- | --- | --- |
| 只改 `config.go` 的 `aggregated`、不在 `component.yaml` 里声明对应的弱依赖（或反过来） | 那个组件在页面上永远显示"未安装" | 只有声明了依赖，平台才会注入它的 `*_ENDPOINT`；现有测试只核对清单与配置的条数，不读 `component.yaml`，挡不住这种漏 |
| 把任何一个依赖地址加进必需校验 | 没装齐六个组件的项目里本组件启动不了 | 弱依赖缺席是常态，`TestNoDependencyIsRequired` 会挡住 |
| 让 `/healthz` 去探那六个组件 | 业务组件一抖，文档页面跟着被杀掉重启 | 它们全是弱依赖，全挂了这个页面也该打得开 |
| 把"连不上"和"连上了但没有这份文档"合并 | `unreachable` 与 `no-docs` 分不清，使用者不知道该修组件还是该让它补文档 | 两种情况的处置完全不同，`errNoDocs` 就是为区分它们存在的 |
| 在 `/api/v1/sources` 的响应里带上组件地址 | 内网拓扑暴露给能打开页面的任何人 | `TestEndpointsAreNotLeaked` 会挡住 |
| 只在主端口上做 gRPC Reflection | `people/basic` 的 gRPC 服务永远不出现，它只显示 OpenAPI | 它的 gRPC 在额外端口 9090 上（`PEOPLE_BASIC_GRPC_ENDPOINT`），主端口只有 HTTP；`TestGRPCOnExtraPortIsListed` 会挡住 |
| 把平台注入的 `*_ENDPOINT` 直接交给 gRPC | 报 `dns resolver: missing address`，所有 gRPC 文档都拿不到 | 注入的是带 scheme 的 URL，gRPC 要 `host:port`，必须经过 `grpcTarget()` |
| 在 `web/index.html` 里引用 CDN 上的脚本或样式 | 内网或气隙环境里页面永远转圈 | 静态资源必须在镜像里，由 Dockerfile 从 swagger-ui 镜像拷入 |
| 去掉缓存的过期判断 | 新装上的组件永远不出现在页面上 | 缓存一旦不过期，第一次探测的结果就永远是答案 |

## 改代码前自查

1. 动了聚合目标时，`component.yaml` 的依赖与 `config.go` 的 `aggregated` 是否一一对应，且新依赖写了 `optional: true`？
2. 一个依赖地址都没有时，本组件是否仍能启动、`/api/v1/sources` 是否返回全部 `absent`？
3. 新的探测路径是否区分了"连不上"和"没有这份文档"，并且一个目标出错不影响其余目标？
4. 对外的响应里是否仍然没有任何组件的内部地址？
5. `/healthz` 是否仍然只回答本进程存活？
6. 改了页面后，用镜像跑一遍，Swagger UI 与状态表是否都能显示？
7. `go vet ./... && go test ./...` 是否全绿？

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
