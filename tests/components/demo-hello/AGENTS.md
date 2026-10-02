# demo/hello

BrickKit 平台自测用的最小 HTTP 组件，单个 Go 文件、只用标准库。怎么用、边界和契约见 `BRICKKIT.md`；依赖、配置、部署见 `component.yaml`。

## 代码地图

| 路径 | 管什么 |
| --- | --- |
| `main.go` | 全部代码：读环境变量、三条路由、JSON 日志、收到 SIGTERM 时优雅退出 |
| `main_test.go` | 用 `httptest` 直接打路由的单元测试，不起端口、不依赖外部 |
| `openapi.json` | 对外契约：三条接口的 OpenAPI 描述 |
| `component.yaml` | 组件清单：`GREETING` 配置项、镜像、端口 8080、健康检查路径 |
| `Dockerfile` | 两阶段构建：`golang:1.22-alpine` 编译静态二进制，`alpine:3.20` 里以 uid 10001 运行 |
| `.dockerignore` | 不把 `*_test.go` 带进构建上下文 |
| `go.mod` | 独立的 Go module（只用标准库），不参与 BrickKit 主 module 的构建 |

| 功能 | 从这里开始 | 然后 |
| --- | --- | --- |
| 健康检查 | `main.go` 的 `handleHealthz` | `main_test.go` 的 `TestHealthz` |
| 问候语接口 | `main.go` 的 `handleHello` | `main_test.go` 的 `TestHelloUsesInjectedConfig`，`openapi.json` |
| 环境变量回显 | `main.go` 的 `platformEnvKeys` 与 `handleEnv` | `main_test.go` 的 `TestEnvEndpointEchoesPlatformVariables` |
| 配置读取与缺省值 | `main.go` 的 `newServerFromEnv`、`envOr` | `component.yaml` 的 `configSchema`，`TestNewServerFromEnvDefaults` |
| 启动与退出 | `main.go` 的 `main` | `Dockerfile` |

## 构建与测试

```bash
go vet ./... && go test ./...          # 成功：ok  github.com/brickkit/demo-hello
go run .                                # 本地起在 :8080；另开终端 curl localhost:8080/api/v1/hello
docker build -t brickkit-demo/hello:1.0.0 .
brickkit lint --strict                  # 组件清单与这几份文档；成功时 0 warnings
```

在 BrickKit 仓库根目录，`make test-components` 会连同其他自测组件一起跑 vet 与测试，`make demo-images` 构建它的 `1.0.0` 和 `2.0.0` 两个镜像标签。

## 设计取舍

- **不依赖任何组件**：它是平台自测依赖图里的叶子，`demo/caller` 把它当强依赖，用来验证平台注入的 `DEMO_HELLO_ENDPOINT`。它要验证的只是平台能力（注入、部署文件生成、健康检查、多版本共存），不需要任何上游。
- **一份源码打两个版本标签**：多版本共存要两个镜像，BrickKit 仓库的 `make demo-images` 用同一份源码打 `1.0.0` 和 `2.0.0` 两个标签；接口里报出的版本号来自平台注入的 `COMPONENT_VERSION`，没注入时才回退到 `1.0.0`。
- **只声明 `resources.requests`，不声明 `limits`**：组件知道自己稳态占多少，"允许涨到多少"是部署方的判断；而且部署设置逐字段合并，这里写了 `limits.cpu`，用它的项目就删不掉了（见 `component.yaml` 里的注释）。
- **缺省值与 `configSchema` 的 `default` 一致**：`GREETING` 没注入时回退到 `Hello`，平台文档的教程里展示的就是这个值。

## 易错点

| 不许 | 症状 | 原因 |
| --- | --- | --- |
| 在 `/healthz` 里检查依赖、数据库或任何外部系统 | 外部一抖，编排系统就把这个本身正常的容器杀掉重启 | 健康检查只回答"本进程还活着吗"，平台的健康检查语义就是如此 |
| 改端口只改 `main.go` 的 `addr` | 容器起来了但连不上，健康检查一直失败 | `component.yaml` 的 `deployment.port`、`Dockerfile` 的 `EXPOSE` 与 `addr` 必须是同一个 8080 |
| 把配置值写死在代码里，不走环境变量 | 平台改了 `config/` 再 `up`，接口返回的还是旧值，注入验证失效 | 这个组件存在的意义就是让平台验证环境变量注入 |
| 改 `newServerFromEnv` 里 `GREETING` 的回退值而不改 `configSchema` 的 `default` | `TestNewServerFromEnvDefaults` 失败；文档教程里展示的 `Hello` 与实际行为对不上 | 代码缺省值与清单里的 `default` 必须是同一个值 |
| 去掉 `Dockerfile` 里的 `USER 10001` | 部署文件写了 `k8s.podSecurity: restricted` 时 Pod 带 `runAsNonRoot: true`，以 root 运行的镜像在 K8s 里起不来 | 容器不得以 root 运行，这也是平台 restricted 安全级别的要求 |

## 改代码前自查

1. 新增或改名的接口是否同步写进了 `openapi.json` 和 `BRICKKIT.md` 的契约索引？
2. 新增的配置项是否同时写进了 `component.yaml` 的 `configSchema`，代码里的缺省值是否与 `default` 一致？
3. 端口是否仍在 `main.go`、`component.yaml`、`Dockerfile` 三处一致？
4. `/healthz` 是否仍然只返回常量、不碰任何外部？
5. 是否仍然只用标准库？`Dockerfile` 只拷 `go.mod` 和 `*.go`，引入第三方依赖要同时补 `go.sum` 与拷贝步骤。
6. `go vet ./... && go test ./...` 是否通过，`brickkit lint --strict` 是否 0 warnings？

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
