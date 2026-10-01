# component.yaml 字段参考

组件的 Manifest 的每一个字段：类型、是否必填、缺省值、校验器真正套用的规则。怎么写好每一块，见 [component.yaml 字段指南](../03-component-guide/02-component-yaml-reference.md)。

文件里只能写这里列出的字段：写了不认识的字段，当场报错并提示是不是拼错了。没有扩展字段的机制。

## 顶层

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `apiVersion` | 字符串 | ✅ | 只能是 `brickkit/v1` |
| `kind` | 字符串 | ✅ | 只能是 `Component` |
| `tags` | 字符串列表 | | 检索用的标签，平台不解释 |

## metadata

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `metadata.id` | 字符串 | ✅ | `<scope>/<name>`；每段是小写字母、数字、中划线，以字母或数字开头结尾（`^[a-z0-9]([a-z0-9-]*[a-z0-9])?/[a-z0-9]([a-z0-9-]*[a-z0-9])?$`）；最长 63 个字符（版本化服务名要符合 DNS 标签规则） |
| `metadata.name` | 字符串 | ✅ | 给人看的名字 |
| `metadata.version` | 字符串 | ✅ | 精确版本 `主.次.修订`（`^\d+\.\d+\.\d+$`），不接受 `v` 前缀、范围与 `latest` |
| `metadata.description` | 字符串 | ✅ | 一句话描述 |
| `metadata.vendor` | 字符串 | | 发布者 |
| `metadata.license` | 字符串 | | 许可证 |
| `metadata.apiDocs` | 字符串 | | API 文档地址 |
| `metadata.repository` | 字符串 | | 组件仓库或主页的地址；项目 `AGENTS.md` 的组件表链到这里 |

## artifacts

契约与其它随组件发布的文件。每一项：

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `artifacts[].type` | 字符串 | ✅ | 自由字符串，平台不解释（如 `api-contract`） |
| `artifacts[].files` | 字符串列表 | ✅ | 至少一个；相对仓库根的路径，不能是绝对路径、不能用 `..` 走出仓库 |
| `artifacts[].format` | 字符串 | | 自由字符串（如 `openapi`、`protobuf`） |
| `artifacts[].description` | 字符串 | | 给人看的说明 |

## dependencies

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `dependencies.components[].id` | 字符串 | ✅ | `<组件ID>@<精确版本>`。也可以整项直接写成这个字符串（强依赖的简写） |
| `dependencies.components[].optional` | 布尔 | | `true` 为弱依赖；缺省为强依赖 |

两种写法：

```yaml
dependencies:
  components:
    - demo/hello@1.1.0
    - id: demo/bus@1.0.0
      optional: true
```

规则：版本必须是精确版本；不能依赖自己；同一个组件 ID 只能出现一次（地址变量名不带版本，两个版本会撞在同一个变量上）。

## configSchema

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `configSchema.type` | 字符串 | | 写的话是 `object` |
| `configSchema.required` | 字符串列表 | | 每一项必须在 `properties` 里声明过 |
| `configSchema.properties.<key>.type` | 字符串 | ✅ | `string` / `integer` / `number` / `boolean` / `array` / `object` |
| `configSchema.properties.<key>.default` | 任意 | | 缺省值；标量按写的原样注入（`1.10` 还是 `1.10`），列表与映射注入时编码成一行 JSON |
| `configSchema.properties.<key>.description` | 字符串 | | 出现在使用方配置骨架的行尾 |
| `configSchema.properties.<key>.secret` | 布尔 | | `true` 时这一项按密钥处理（Docker 进 0600 的 env 文件，K8s 进 Secret） |
| `configSchema.properties.<key>.enum` | 列表 | | 允许的取值——**只是说明，平台不检查值** |
| `configSchema.properties.<key>.minimum` | 数字 | | 同上 |
| `configSchema.properties.<key>.maximum` | 数字 | | 同上 |
| `configSchema.properties.<key>.pattern` | 字符串 | | 同上 |
| `configSchema.properties.<key>.items.type` | 字符串 | | `array` 类型的元素类型；同上 |

`<key>` 就是注入的环境变量名：必须是合法的环境变量名（字母、数字、下划线，不以数字开头），原样注入，不做大小写转换。
撞上平台保留的名字（`COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`、`*_ENDPOINT`）时：
`up` / `lint` 警告并忽略这一项，市场拒收。平台只检查键名、不检查值，详见 [configSchema 规格](04-config-schema-spec.md)。

## deployment

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `deployment.type` | 字符串 | ✅ | 只能是 `container` |
| `deployment.image` | 字符串 | 二选一 | 预构建镜像；不带 tag 时补上 `metadata.version` |
| `deployment.build.context` | 字符串 | 二选一 | 构建上下文，相对仓库根，缺省 `.` |
| `deployment.build.dockerfile` | 字符串 | | Dockerfile 路径，相对仓库根，缺省 `Dockerfile` |
| `deployment.port` | 整数 | ✅ | 主端口，1–65535 |
| `deployment.extraPorts[].name` | 字符串 | ✅ | 小写字母、数字、中划线，最长 15 个字符（K8s Service 端口名规则）；不能重复。调用方拿到的地址变量是 `<ID>_<名字>_ENDPOINT`，`-` 换成 `_`（`demo/hello` 的 `admin-api` → `DEMO_HELLO_ADMIN_API_ENDPOINT`） |
| `deployment.extraPorts[].port` | 整数 | ✅ | 1–65535，不能与主端口相同 |
| `deployment.resources.requests.cpu` | 字符串 | | 建议的 CPU 请求，如 `"100m"` |
| `deployment.resources.requests.memory` | 字符串 | | 建议的内存请求，如 `"128Mi"` |
| `deployment.resources.limits.cpu` | 字符串 | | 建议的 CPU 上限（建议不写） |
| `deployment.resources.limits.memory` | 字符串 | | 建议的内存上限 |
| `deployment.labels` | 字符串映射 | | 原样透传：Docker 写成 service labels，K8s 写成 Deployment 与其 Pod 的 annotations；值必须是字符串 |

`image` 与 `build` 至少写一个；`build` 的路径必须在仓库里。`resources` 写了的话，`requests` 或 `limits` 里至少要有 `cpu` 或 `memory` 之一；
这里的值是作者的建议，使用方在部署文件的条目上逐字段覆盖。只有 `requests` 有平台缺省值（`100m` / `128Mi`），`limits` 没有缺省值。

## migration

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `migration.command` | 字符串列表 | ✅（写了 `migration` 时） | 迁移命令，数组形式；用同一个镜像执行 |

## healthCheck

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `healthCheck.type` | 字符串 | ✅ | `http` / `tcp` / `none` |
| `healthCheck.path` | 字符串 | `http` 时必填 | 以 `/` 开头 |
| `healthCheck.startPeriodSeconds` | 整数 | | 启动宽限期，单位秒，缺省 60，最大 3600；`type: none` 时不能写 |

检查间隔 10 秒、超时 3 秒、连续 3 次失败算不健康，由平台固定，不可配置。

每种类型实际跑什么：Docker 下检查在容器里经由 `/bin/sh` 执行——`http` 先用 `wget`、不行再用 `curl` 访问 `http://localhost:<端口><路径>`，
`tcp` 执行 `nc -z localhost <端口>`，所以镜像里要有 shell 和这些工具（`scratch`、distroless 这类没有 shell 的镜像两种都过不了）。
K8s 下它们变成 `httpGet` / `tcpSocket` 探针，从容器外面发起。`none` 不生成检查：依赖方只等容器启动。
见 [组件日志正常，平台却说它不健康](../10-troubleshooting/01-up-down-issues.md#组件日志正常平台却说它不健康)。

## shell

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `shell.members` | 字符串列表 | ✅（写了 `shell` 时） | 外壳编进的成员，每项 `<组件ID>@<精确版本>`；至少一项；不能是自己；同一个组件只能列一个版本 |

写了 `shell`，这个组件就是外壳；项目的 `brickkit.yaml` 里它的条目必须带 `kind: shell`。见 [外壳声明](../04-shell/03-shell-declaration.md)。

## local

组件以 `mode: local` 运行时，BrickKit 怎么启动它。多数情况下不用写：从源码目录里的标记文件自动认出。

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `local.language` | 字符串 | | `go` / `rust` / `dotnet` / `node` / `java` / `python` / `ruby`；同一目录里有好几种语言的标记文件时用它指明 |
| `local.runCommand` | 字符串列表 | | 直接给出启动命令；第一项含路径分隔符时相对组件目录 |
