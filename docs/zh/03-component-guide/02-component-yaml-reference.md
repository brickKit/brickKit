# component.yaml 字段指南

`component.yaml` 是组件的自我描述。CLI 安装它、生成部署文件、给它注入环境变量，靠的都是这一份文件，从不读组件的源码。
这一篇按块讲每一块写什么、怎么写好；每个字段精确的类型与校验规则见 [component.yaml Schema](../11-reference/01-component-yaml-schema.md)。

`demo/quote` 的完整 `component.yaml`：

```yaml
apiVersion: brickkit/v1
kind: Component

metadata:
  id: demo/quote
  name: Quote
  version: 0.1.0
  description: 每次返回一句名言，带上 demo/hello 的问候语

artifacts:
  - type: api-contract
    format: openapi
    files:
      - api/openapi.yaml

dependencies:
  components:
    - demo/hello@1.1.0

configSchema:
  type: object
  properties:
    QUOTE_PREFIX:
      type: string
      default: "今日名言："
      description: 名言前面加的前缀

deployment:
  type: container
  build:
    context: .
    dockerfile: Dockerfile
  port: 8080

healthCheck:
  type: http
  path: /healthz
```

文件里只能写已知的字段：写错一个字段名（`dependancies`）会被当场拒绝，而不是悄悄忽略。

## metadata：我是谁

| 字段 | 说明 |
| --- | --- |
| `id` | `<scope>/<name>`，全局唯一。它决定仓库名（`demo-quote`）、服务名（`demo-quote-0-1-0`）和别人拿到的地址变量名（`DEMO_QUOTE_ENDPOINT`） |
| `version` | 精确版本 `主.次.修订`。**这是版本号唯一的事实来源**：`release` 打的 tag、`build` 的镜像 tag 都取它 |
| `name`、`description` | 给人看的名字与一句话描述。`description` 也是使用方项目 `AGENTS.md` 末尾组件表里"干什么"那一列，所以写它做什么，别写它叫什么 |
| `vendor`、`license`、`apiDocs` | 可选：发布者、许可证、API 文档地址 |
| `repository` | 可选：组件仓库或主页的地址。它就是同一张表的"主页"一列，在网页上读项目仓库的人（那里没有 `.brickkit/`）也能点到每个组件 |

`tags` 是可选的检索标签。

## dependencies：我依赖谁

```yaml
dependencies:
  components:
    - demo/hello@1.1.0             # 强依赖
    - id: demo/bus@1.0.0           # 弱依赖
      optional: true
```

- **只写精确版本。** `^1.0.0`、`~1.0`、`latest` 都会被拒绝：依赖的版本在你发布的那一刻就定死了，使用方装到的永远是你测过的那个。
- **强依赖**缺了，使用方的 `up` 直接报错、不启动。**弱依赖**缺了只警告，而且它的地址变量**完全不注入**——不是注入空字符串。
  所以代码里读弱依赖的地址必须能处理"没有这个变量"：

  ```go
  bus, ok := os.LookupEnv("DEMO_BUS_ENDPOINT")
  if !ok {
      // 降级：不发事件，照常服务
  }
  ```

  为什么不给空字符串：`http.Get(endpoint + "/healthz")` 拿到空串会变成请求 `/healthz`——打到自己身上、返回 200，
  你会以为依赖好好的。宁可启动时就大声失败，也不要运行时悄悄出错。
- **一个组件 ID 只能出现一次。** 地址变量名由组件 ID 推出、不带版本，同一个组件写两个版本会撞在同一个变量上。

每个依赖，平台注入一个地址变量：`<组件 ID 大写，/ 和 - 换成 _>_ENDPOINT`，值带着版本——
`DEMO_HELLO_ENDPOINT=http://demo-hello-1-1-0:8080`。依赖有额外端口时再加 `DEMO_HELLO_<端口名>_ENDPOINT`。

## configSchema：我要什么配置

```yaml
configSchema:
  type: object
  properties:
    QUOTE_PREFIX:
      type: string
      default: "今日名言："
      description: 名言前面加的前缀
  required: []
```

**键名就是环境变量名**，原样注入，所以写成 `DB_HOST` 这样的大写下划线形式。每一项可以写 `type`、`default`、`description`，
密钥写 `secret: true`，必填写进 `required`。`enum`、`minimum`、`maximum`、`pattern`、`items` 也能写，但它们只是说明，平台不按它们检查值。

怎么把配置设计好——拆多细、怎么命名、哪些该必填——见 [configSchema 设计准则](03-config-schema-design.md)。

## deployment：我怎么跑

| 字段 | 说明 |
| --- | --- |
| `type` | 固定是 `container` |
| `image` | 预构建镜像，使用方直接拉取 |
| `build` | 本机构建：`context`（构建上下文）与 `dockerfile`，都相对仓库根，缺省是 `.` 与 `Dockerfile` |
| `port` | 主端口：健康检查打它，别人的 `*_ENDPOINT` 指向它 |
| `extraPorts` | 额外端口（比如 gRPC）：`- name: grpc` / `port: 9090`，别人拿到 `…_GRPC_ENDPOINT` |
| `resources` | 建议的 CPU / 内存配额，使用方在部署文件里可以覆盖 |
| `stopGracePeriodSeconds` | 收到停止信号后要多久把手上的事做完（消费者确认在途消息、outbox 发完一批、HTTP 处理完在途请求）。不写用引擎的默认值：compose 10 秒、K8s 30 秒。你的代码里自己的关停超时要比它短 |
| `labels` | 原样透传的标签（Docker 的 service labels、K8s 的 Pod annotations），平台不解释 |

`image` 与 `build` 至少写一个。只写 `build`：使用方用 `brickkit build` 从你的 tag 构建。两个都写：你的 Git / 市场使用方拉镜像，
把源码克隆下来开发的人从源码构建。

**配额只建议 `requests`。** 你知道自己稳态要多少；"最多能涨到多少"是部署方的判断，而且你写了 `limits.cpu`，使用方就去不掉它了。
平台不给 `limits` 编默认值：编一个 512Mi，一个本来需要 600Mi 的健康组件就会被 OOM 杀掉。

## migration：数据库迁移

```yaml
migration:
  command: ["/app/quote", "migrate"]
```

`up` 在主服务启动之前，用**同一个镜像**、这条命令跑一次迁移；迁移失败，主服务不启动。Kubernetes 上它是一个 Job——跨副本只跑一次。

**入口程序遇到不认识的参数必须立刻报错退出。** 迁移容器和主服务是同一个镜像，只靠参数区分；如果入口程序把拼错的参数当成"那就启动服务"，
迁移容器就会变成第二个一直在跑的服务，主服务永远等不到迁移结束，而日志里看起来一切正常。参数要在读环境变量、连数据库之前就校验。

## healthCheck：我还活着吗

```yaml
healthCheck:
  type: http          # http | tcp | none
  path: /healthz
  startPeriodSeconds: 90
```

- **只检查本进程还活着。** 不要在健康检查里连数据库、调依赖：一个下游抖一下，所有上游同时被判不健康、一起重启，就是一次级联故障。
- 检查节奏由平台固定（每 10 秒一次、3 秒超时、连续 3 次失败算不健康）。**启动宽限期**缺省 60 秒，冷启动更久的组件（重的 Spring Boot、
  预加载很多东西的 Django、.NET 首次 JIT）要写 `startPeriodSeconds`，否则 Kubernetes 上会永远 CrashLoopBackOff。宽限期只推迟"判死"，
  不推迟"判活"，写大一点没有代价。
- Docker 下 HTTP 检查在容器里执行 `wget` 或 `curl`：镜像里至少要有其中一个（Alpine 自带 `wget`；distroless 镜像两者都没有，改用 `type: tcp`）。

## readinessCheck：我能接流量了吗

```yaml
readinessCheck:       # 可选
  type: http          # http | tcp
  path: /readyz
```

活着不等于能接流量：缓存还在预热、第一次同步还没完成、权限数据还没拉到时，进程好好的，请求却只能答 503。两件事的后果不同——
存活检查失败，Kubernetes 杀掉重启；就绪检查失败，只是不把流量导给它。所以有这段时间的组件，单独声明一个就绪检查：

| 目标 | 用在哪 |
| --- | --- |
| Kubernetes | readinessProbe 探它（滚动更新时，还没就绪的新 Pod 不收流量）；startupProbe、livenessProbe 仍探 `healthCheck` |
| Docker / Podman | compose 的 healthcheck 探它：依赖方等它真的能接流量才启动，`up` 也等到那时才返回。compose 的 healthcheck 不会杀容器，所以这里问的本来就是"就绪了吗" |

不写时，就绪就看 `healthCheck`，和以前一样。规则和健康检查一样：**只看本进程准备好没有，不查下游**——下游暂时挂了就让所有副本一起
"没就绪"，等于把自己整个从服务里摘掉。启动宽限期、检查节奏与 `healthCheck` 共用。

## shell：我是外壳

```yaml
shell:
  members:
    - erp/api@1.2.0
    - erp/auth@2.0.0
```

写了 `shell.members`，这个组件就是**外壳**：它在构建时把列出的这些组件（精确版本）编进自己的进程里。只有外壳才写这一块。见 [外壳机制](../04-shell/README.md)。

## artifacts：我给调用方的契约

```yaml
artifacts:
  - type: api-contract
    format: openapi
    description: HTTP 接口
    files:
      - api/openapi.yaml
```

`type`、`format` 是自由的字符串，平台不解释，只负责把 `files` 送到使用方的 `.brickkit/artifacts/` 下。见 [契约与产物](06-artifacts-and-contracts.md)。

## local：本机进程怎么启动

使用方把组件设成 `mode: local` 时，BrickKit 从源码目录里认出语言、自己启动它（`go.mod` → `go run .`，`package.json` → 它的 `start` 脚本……）。
认不出、或者认出了不止一种时，写这一块：

```yaml
local:
  language: go                  # 同一目录里有好几种语言的标记文件时，说明是哪一种
  runCommand: ["go", "run", "./cmd/server"]   # 或者直接给出启动命令
```

`mode: local` 下平台还会注入 `PORT`（它为这个进程选定的本机端口，优先用 `deployment.port`）：代码读 `PORT`、读不到再用自己的缺省端口，就不会和别的进程抢端口。
