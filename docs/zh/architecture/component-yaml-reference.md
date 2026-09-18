# component.yaml 字段完整参考

AGENTS.zh.md §6 是那份可以直接复制粘贴的骨架。这篇文档是骨架背后的东西：每个字段的类型、是不是必填、默认值是什么，以及骨架没法展示的那部分——写错了到底会撞上哪一条具体校验规则，逐行核对过 `internal/manifest/types.go` 与 `internal/manifest/validate.go`。Manifest 没有扩展字段机制（AGENTS.zh.md §6 结尾那条说明）：下面就是完整的字段集合，清单之外的 key 在解析阶段就会被拒绝，不是悄悄忽略。

这篇文档负责的是**字段本身**。一个字段配对了之后实际会发生什么——依赖怎么变成地址、资源配额怎么跨三层合并、健康检查怎么变成三种不同的 K8s 探针——分别是 [environment-variables.md](environment-variables.md)、[resource-binding.md](resource-binding.md)、[deployment-generation.md](deployment-generation.md) 的主题，这篇文档只做交叉引用，不重复讲一遍。

## `apiVersion` / `kind`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `apiVersion` | string | 是 | 必须精确等于 `brickkit/v1` |
| `kind` | string | 是 | 必须精确等于 `Component` |

## `metadata`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `metadata.id` | string | 是 | 格式 `scope/name`；全部小写；两段各自匹配 `[a-z0-9]([a-z0-9-]*[a-z0-9])?`（首尾必须是字母或数字，中间可以有 `-`）；总长度 ≤63 字符——由它派生出的版本化服务名要能塞进一个 DNS 标签 |
| `metadata.name` | string | 是 | 自由文本，没有格式限制 |
| `metadata.version` | string | 是 | 必须匹配 `major.minor.patch`（`^\d+\.\d+\.\d+$`）——不带 `v` 前缀，不接受预发布后缀，不接受 `^`/`~` 范围语法 |
| `metadata.description` | string | 是 | 自由文本 |
| `metadata.vendor` | string | 否 | 自由文本 |
| `metadata.license` | string | 否 | 自由文本 |
| `metadata.apiDocs` | string | 否 | 自由文本（按惯例是个 URL，但不会按 URL 格式校验） |

## `tags`

`[]string`，可选。自由文本，只用于市场搜索时过滤——CLI 自己从不读它。

## `artifacts[]`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `artifacts[].type` | string | 是 | 自由字符串（`api-contract`、`api-docs`……不是枚举，平台不解释它） |
| `artifacts[].format` | string | 否 | 自由字符串（`protobuf`、`openapi`……） |
| `artifacts[].description` | string | 否 | 自由文本 |
| `artifacts[].files` | `[]string` | 是 | 至少一条；每一条必须是**相对路径**（写绝对路径会被拒绝），也不能用 `..` 跳出组件仓库根目录 |

## `dependencies.components[]`

同一个底层结构 `ComponentDep`，YAML 上有两种写法：

```yaml
- department/tree@1.0.0                 # 强依赖——直接写一行 "id@version"
- id: infra/redis-event-bus@1.0.0       # 弱依赖——写成一个带 optional: true 的映射
  optional: true
```

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `dependencies.components[].id`（`@` 前面那部分） | string | 是 | 跟 `metadata.id` 同一条 `scope/name` 规则 |
| version（`@` 后面那部分，不是独立的 YAML 键） | string | 是 | 精确版本，跟 `metadata.version` 同一条规则——这里写范围（`^1.0.0`）也会被拒绝，不会被悄悄接受 |
| `dependencies.components[].optional` | bool | 否（默认 `false`） | 只在映射写法下才有意义 |

校验器还会拦下三件从写法本身看不出来的事：

- **组件不能依赖自己**——`id == metadata.id` 是硬错误。
- **同一个组件 ID 不能在 `dependencies.components` 里出现两次**，不管是强依赖还是弱依赖、版本相不相同——一条依赖最终解析出来的**变量名**（`{EnvPrefix}_ENDPOINT`）根本没有版本这个槽位，第二条只可能在注入那一刻悄悄覆盖第一条的值，不会真的多出什么。完整的"为什么"在 AGENTS.zh.md §5.1，而且校验器自己的报错文案已经把两条出路都写清楚了：只依赖其中一个版本（多版本共存是项目级能力，AGENTS.zh.md §5.1），或者把第二个改走 `configSchema` 里的一个配置项（AGENTS.zh.md §9.23，design/003 §4.9）。
- 第二种情况的真实报错文案，原样摘录：

  ```
  同一个组件声明了两个版本
       demo/hello@1.0.0（dependencies.components[0]）与 demo/hello@2.0.0
       两者都注入 DEMO_HELLO_ENDPOINT —— 依赖地址的环境变量名基于组件 ID、不带版本号
       （001 §8.3），后者覆盖前者，而组件不会察觉自己只连上了其中一个
       出路 1：只依赖其中一个版本。多版本共存是**项目级**的——
               brickkit.yaml 里可以同时跑两个版本，供不同调用方各用各的（002 §3.6）
       出路 2：确实要同时调两个，把第二个声明成 configSchema 里的一个配置项，
               由项目填地址（003 §4.9）
  ```

## `dependencies.resources[]`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `dependencies.resources[].kind` | string | 是 | `database`/`cache`/`mq`/`storage`/`search`/`smtp` 之一——不认识的值会被拒绝，不会被放过去 |
| `dependencies.resources[].engine` | string | 是 | 非空，但**从不会拿去跟一份"认识的引擎"清单核对**——`postgresql`、`mongodb`、随便编一个字符串，解析器一视同仁全部接受。这一格纯粹是给读 Manifest 的人看的自我说明，对实际会注入哪些变量没有任何影响（那完全由 `kind` 决定，见 [environment-variables.md](environment-variables.md) 第三节），也从不会跟项目 `brickkit.yaml` 侧将来给同一个资源声明的 `engine` 做交叉核对。 |

这一整块只是可选的自我说明，不是功能性的必填项——一个组件哪怕压根没声明 `dependencies.resources`，项目照样能把资源绑定给它（真正把资源接到组件身上的是 `brickkit.yaml` 自己的 `resources[].bindings[].componentId`，见 [brickkit-yaml-reference.md](brickkit-yaml-reference.md)）。

## `configSchema`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `configSchema.type` | string | 否 | 写了就必须是 `object`——没有别的合法值，因为 `configSchema` 描述的永远是一份扁平的具名属性映射 |
| `configSchema.properties.<key>.type` | string | 是（每个属性都要有） | `string`/`integer`/`number`/`boolean`/`array`/`object` 之一 |
| `configSchema.properties.<key>.default` | any | 否 | 不会拿去跟声明的 `type` 做类型核对（AGENTS.zh.md §9.12）——每种类型具体怎么渲染成环境变量，包括 array/object 的那个坑（渲染成 Go 的 `fmt.Sprint` 形式，不是 JSON），见 [environment-variables.md](environment-variables.md) 第四节 |
| `configSchema.properties.<key>.description` | string | 否 | 自由文本 |
| `configSchema.properties.<key>.enum` | `[]any` | 否 | **只是被解析、存下来，代码库里再没有任何地方读过它。** CLI 不读（没有任何一个值会拿去跟它核对），市场不读，任何渲染器都不读。今天它就是纯文档，给人（或者给 AI）读 schema 时看的，没有任何东西在真正执行它。 |
| `configSchema.properties.<key>.items.type` | string | 否（`items` 这一块整体可选，里面只有 `type` 一个键） | **同样只是被解析、存下来，代码库里再没有任何地方读过它**——全仓库搜一遍，唯一命中的 `.Items` 是一个毫不相关的 Kubernetes 列表类型。按惯例只在 `type: array` 的属性上才有意义，但今天写不写它效果完全一样。 |
| `configSchema.properties.<key>.minimum` | number | 否 | **同样只是被解析、存下来，没有任何东西去执行它。** 按惯例只在 `type: integer`/`number` 的属性上才有意义，但写不写它效果完全一样。必须是数字——写成字符串会在解析阶段被拒绝，这查的是说明书自己的结构，不是使用者填的值：默认值越界、`minimum` 大于 `maximum`，都照样通过，不会有任何警告。 |
| `configSchema.properties.<key>.maximum` | number | 否 | 同 `minimum`。 |
| `configSchema.properties.<key>.pattern` | string | 否 | **同样只是被解析、存下来。** 按惯例只在 `type: string` 的属性上才有意义。它不会被当成正则编译，所以写一个非法的正则也照样通过——没有任何东西会去跑它。 |
| `configSchema.required` | `[]string` | 否 | 每一条都必须是 `properties` 里真的声明过的 key——写了一个 `properties` 里没有的名字，在 Manifest 校验阶段就会被拒绝，项目根本没机会为它提供值 |

`required` 是这整块里唯一真正有牙齿的地方：一个列进 `required`、既没有 `default` 也没有被 `brickkit.yaml` 覆盖的属性，会让整个项目的 `brickkit up` 直接拒绝启动，不只是这一个组件——完整机制和真实报错文案见 [environment-variables.md](environment-variables.md) 第五节第三条。

## `deployment`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `deployment.type` | string | 是 | 必须精确等于 `container`——唯一合法值，这是故意的（AGENTS.zh.md §9.10：没有"静态"类型，前端也是容器） |
| `deployment.image` | string | 是 | 非空；不做其它校验（Manifest 校验阶段不会去检查这个镜像地址是否真的可达） |
| `deployment.port` | int | 是 | `1`–`65535` |
| `deployment.extraPorts[].name` | string | 是（每条都要有） | ≤15 字符，匹配 `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`——这正是 Kubernetes Service 端口名规则（`IANA_SVC_NAME`）搬到 Manifest 层面来用，因为一个额外端口的 `name` 在生成阶段会原样变成一个真实的 K8s Service 端口名；在一个组件自己的 `extraPorts` 里必须唯一 |
| `deployment.extraPorts[].port` | int | 是（每条都要有） | `1`–`65535`；不能与 `deployment.port` 相同 |
| `deployment.resources.requests.cpu` / `.memory` | string | 否 | 自由字符串（Kubernetes 数量语法，如 `"100m"`、`"128Mi"`）；CLI 自己不校验它能不能被解析 |
| `deployment.resources.limits.cpu` / `.memory` | string | 否 | 同上 |
| `deployment.labels` | `map[string]string` | 否 | 见下方保留键规则 |

**`deployment.resources` 只要出现，就必须至少声明 `requests`/`limits` 其中一个；这两者里出现的那个，也必须至少声明 `cpu`/`memory` 其中一个**——写一个空的 `resources: {}` 或空的 `requests: {}` 会被拒绝，不会悄悄什么都不做。这个字段怎么跟 `brickkit.yaml` 自己的覆盖、CLI 内置默认值三层合并，是 [resource-binding.md](resource-binding.md) 的主题，这里不重复——包括为什么只有 `requests` 有平台默认值、`limits` 永远没有。

**`deployment.labels` 保留键规则**：键不能以 `brickkit.io/` 开头（平台自己的命名空间——组件 ID、版本、项目名都记在这底下）、不能以 `com.docker.compose.` 开头（Compose 自己的记账标签）、也不能精确等于 `app`（Kubernetes 自己 Deployment 找 Pod 用的选择器键，也是 `NetworkPolicy` 的匹配依据）。撞上任何一条都是解析阶段的硬错误，不会悄悄丢弃——而且只查键，值一个字都不看。这个字段存在的全部意义见 AGENTS.zh.md §9.22：它就是一个透传口，专门让平台可以继续不理解任何标签的含义，所以标签的**值**从来不会被校验，只有**键**会不会撞上平台自己要用的那几个才会被拦。

## `migration`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `migration.command` | `[]string` | 只要写了 `migration` 这一块就必填 | 至少一个元素；每个元素去掉首尾空白后都不能是空字符串 |

`migration` 这一整块本身是可选的——没有迁移要做的组件可以整段不写。没有单独的 `migration.image`：迁移容器永远复用 `deployment.image`，只靠传的命令不同来区分——这正是 AGENTS.zh.md §6 那条关于入口点必须对不认识的参数快速失败的提醒（design/002 §8.5.1）存在的原因。

## `healthCheck`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `healthCheck.type` | string | 是 | `http`/`tcp`/`none` 之一 |
| `healthCheck.path` | string | 只有 `type: http` 时必填 | 必须以 `/` 开头 |
| `healthCheck.startPeriodSeconds` | int | 否（默认 `60`，即 `DefaultStartPeriodSeconds`） | 必须是正整数，`≤3600`（一小时）——**而且在 `type: none` 下写了这个字段本身就会被拒绝**，不是悄悄不生效：`type: none` 根本不生成任何探测，也就谈不上"宽限期"该套用在谁身上，校验器会点名说这件事，不会让这个字段就那么闲置在那 |

3600 秒这道上限不是嫌宽限期长了有害——一个真的要花十分钟预热的组件完全可以这么写。它存在的理由是这个字段最常见的写错方式是习惯性地按毫秒来写：`startPeriodSeconds: 60000` 读起来像"60 秒"，实际是 16 小时，而且——跟这篇文档里几乎所有别的错误都不一样——**这个错误不会产生任何报错**：组件就那么在 `starting` 里挂上大半天，表面上看不出哪里明显坏了。`startPeriodSeconds` 往下具体改变了什么——K8s `startupProbe` 的 `failureThreshold` 是按 `ceil(startPeriodSeconds ÷ 5)` 算出来的、不写它时你拿到的是固定 30 秒的预算——这些机制在 [deployment-generation.md](deployment-generation.md) 里讲，这里不重复。

**⚠️ AGENTS.zh.md §6 那条健康检查的禁令依然成立，而且 CLI 不会重新校验它**：`healthCheck.path` 只能检查这个进程自己是否存活。`internal/manifest/validate.go` 里没有任何代码能检测出"这个处理函数还顺便查了一下数据库"——那是代码评审阶段该管的规矩，不是一条能写成校验的约束，这正是为什么 AGENTS.zh.md 把它当作设计原则来陈述，而不是这篇文档把它列成一条字段约束。

## 故意不在这里的东西

曾经存在过两个字段——`observability`（一对 `metrics`/`tracing` 布尔值）和 `compatibility.minCliVersion`——它们不是被闲置在那，而是被彻底删掉了，而且没有扩展机制能不经平台明确加字段就悄悄补回一个等价物。AGENTS.zh.md §6 结尾那条说明给了两者的完整理由；简单说就是从来没有任何地方读过它们，而 `minCliVersion` 比单纯不存在还要糟——一个声明了 `minCliVersion: 2.0.0` 的组件，在一个连这个版本号都还不知道的老 CLI 上照装不误，长得像一道安全闸，实际什么都没拦住。

## 延伸阅读

- AGENTS.zh.md §6——这篇文档展开的那份可以直接复制粘贴的骨架
- [environment-variables.md](environment-variables.md)——每一个 `configSchema` 属性、每一条声明过的依赖，到了容器里到底变成了什么
- [resource-binding.md](resource-binding.md)——配额合并链，以及 `brickkit.yaml` 的绑定跟这份 Manifest 的预期对不上时会发生什么
- [deployment-generation.md](deployment-generation.md)——`deployment`/`healthCheck`/`migration` 怎么一步步变成真实的 Compose 服务或 K8s 资源，探针逐个讲
- [brickkit-yaml-reference.md](brickkit-yaml-reference.md)——项目那一侧的对应文档：项目要写什么才能真正跑起来、覆盖、暴露、把资源绑到这里描述的组件上
