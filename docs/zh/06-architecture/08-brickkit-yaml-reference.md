# brickkit.yaml 字段完整参考

AGENTS.zh.md §7 是那份骨架。这篇文档是骨架背后的字典——每个字段的类型、是不是必填、默认值，以及校验器真正会套用的那条约束，逐行核对过 `internal/config/config.go` 与 `internal/config/validate.go`。跟 `component.yaml` 不一样的是，`brickkit.yaml` 在有些地方真的是项目自己的自由状态（`config` 覆盖、资源凭证），并不是每个字段下面都有一份固定的合法值枚举——凡是这样的字段，这篇文档会明说，不会暗示一条根本不存在的约束。

这篇文档负责的是**字段本身**。一个字段配对了之后实际起什么作用——`mode` 怎么级联、一条资源绑定怎么变成环境变量、`servedBy` 怎么把成员的配置合并到外壳身上——分别是 [04-environment-variables.md](04-environment-variables.md)、[05-resource-binding.md](05-resource-binding.md)、[02-dependency-resolution.md](02-dependency-resolution.md) 和 AGENTS.zh.md §5 的主题，这篇文档只做交叉引用，不重复讲。

本页的字段还有一份对应的 JSON Schema，[`schemas/brickkit.schema.json`](../../../schemas/brickkit.schema.json)，由同一批 Go 结构体生成。编辑器可以拿它在你敲字的时候补全字段名、把不认识的键标红；怎么接上、它刻意不覆盖什么，见[给编辑器接上自动补全](../00-quick-start.md#给编辑器接上自动补全)。

## 顶层

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `project` | string | 是 | 匹配 `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`——小写字母、数字、`-`，首尾必须是字母或数字。直接用作 Docker 网络名的后缀，也是默认 K8s 命名空间的后缀 |
| `deploy` | 对象 | 是 | 见下 |
| `sources` | `[]Source` | 否 | 见下 |
| `components` | `[]Component` | 否 | 见下 |
| `resources` | `[]Resource` | 否 | 见下 |
| `installer` | 对象 | 否 | 见下 |

## `deploy`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `deploy.target` | string | 是 | `docker` 或 `k8s` |
| `deploy.context` | string | 否 | 自由字符串（一个 kubeconfig context 名）；**仅 K8s 有意义**——写在 `docker` 下不会报错，只是没人用它 |
| `deploy.namespace` | string | 否（默认 `brickkit-<project>`） | 跟 `project` 同一条字符规则；**仅 K8s** |
| `deploy.podSecurity` | string | 否 | 只有 `""`（不写）或 `"restricted"` 是合法值——没有 `baseline`/`privileged` 可以选，这是故意的（AGENTS.zh.md 的"默认安全"并不延伸到自动打开一个可能让本来跑得好好的组件起不来的开关） |
| `deploy.imagePullSecrets` | `[]string` | 否 | 自由字符串（Secret 名）；**仅 K8s** |
| `deploy.ingressClass` | string | 否 | 自由字符串，原样写进生成的 Ingress 的 `spec.ingressClassName`；**仅 K8s** |
| `deploy.ingressAnnotations` | `map[string]string` | 否 | 透传，不校验，跟组件那侧的 `deployment.labels` 是同一种姿态；**仅 K8s** |
| `deploy.createNamespace` | `*bool` | 否（默认 `true`） | **仅 K8s**——`false` 表示 CLI 既不生成也不 apply `Namespace` 对象，给那些只有命名空间级权限的项目用 |
| `deploy.networkPolicy` | 对象 | 否 | 见下；**仅 K8s** |
| `deploy.serviceAccount.enabled` | bool | 否（默认 `false`；`serviceAccount` 这一块整体可选） | **仅 K8s**——`true` 会给每个组件生成一个专属、不挂载令牌的 ServiceAccount；不写的话每个 Pod 都用命名空间的 `default` ServiceAccount，令牌照常自动挂载（AGENTS.zh.md §7 的警告） |

### `deploy.networkPolicy`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `deploy.networkPolicy.enabled` | bool | 是（要打开这一块就得写） | |
| `deploy.networkPolicy.ingressController.namespace` | string | 只要写了 `ingressController` 这一块就必填 | 留空会生成一条谁也匹配不上的 Kubernetes 标签选择器，策略照样 apply 成功，却把 ingress controller 自己也一起挡在外面——所以干脆直接拒绝，不让这种情况发生 |
| `deploy.networkPolicy.ingressController.podSelector` | `map[string]string` | 否 | 不写 = 放行上面那个命名空间的全部，不再收窄 |
| `deploy.networkPolicy.allowFrom[].name` | string | 是（每条都要有） | 自由文本，只用在报错信息和生成策略自己的注解里——这正是让半年后 `kubectl get networkpolicy -o yaml` 还能看懂自己在干什么的东西 |
| `deploy.networkPolicy.allowFrom[].namespace` | string | 是（每条都要有） | 跟 `ingressController.namespace` 同一个"留空=匹配不上=apply 成功但悄悄挡人"的理由 |
| `deploy.networkPolicy.allowFrom[].podSelector` | `map[string]string` | 否 | 不写 = 上面那整个命名空间 |
| `deploy.networkPolicy.allowFrom[].ports` | `[]int` | 否 | 每个都要在 `1`–`65535`；不写 = 目标组件自己声明的全部端口（`deployment.port` + `extraPorts`） |

**只有生成阶段才查得出来、这里查不出来的一条**：一个 `expose: true` 的组件需要 `ingressController` 真的被设置，但单看 `brickkit.yaml` 判断不了这件事——得知道最终有哪些组件会跑起来，这需要依赖图。那条检查在 K8s 渲染器那边，不在这个解析器里。

### `deploy.networkPolicy.egress`

⚠️ 打开这一块会把每个组件的默认姿态从"想连谁都行"整个翻转成"只能连明确写出来的那些"——AGENTS.zh.md §7 的警告框，在这里再强调一遍，因为这是本页唯一一个漏写一条就能自己把一套正常运行的部署搞挂的字段，而且这个失败在下一次 Pod 重启之前完全是沉默的。

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `deploy.networkPolicy.egress.enabled` | bool | 是（要打开这一块就得写） | |
| `deploy.networkPolicy.egress.allowTo[].name` | string | 是（每条都要有） | 跟 `allowFrom[].name` 同一个注解用途 |
| `deploy.networkPolicy.egress.allowTo[].resource` | string | 否 | 必须指向一个 `resources[].id`——跟直接写 `ports` 互斥（端口会从那个资源自己的 `port` 字段取，这样两处才不会各写一份、慢慢不一致） |
| `deploy.networkPolicy.egress.allowTo[].namespace` | string | `namespace`/`cidr` 二选一 | 集群内的目标 |
| `deploy.networkPolicy.egress.allowTo[].podSelector` | `map[string]string` | 否 | 进一步收窄 `namespace` |
| `deploy.networkPolicy.egress.allowTo[].cidr` | string | `namespace`/`cidr` 二选一 | 集群外的目标（托管数据库、第三方 API） |
| `deploy.networkPolicy.egress.allowTo[].ports` | `[]int` | 否，且不能跟 `resource` 一起写 | 每个都要在 `1`–`65535` |

`namespace` 和 `cidr` 一个都不写会被拒绝，两个都写也会被拒绝——每条目标位置字段只能写恰好一个。DNS 本身和组件之间每一条依赖图上的边都是自动处理的，完全不需要在这里写一条；只有资源和真正在依赖图之外的目的地才需要。

## `sources[]`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `sources[].id` | string | 是 | 在全部安装源里唯一 |
| `sources[].type` | string | 是 | `market`/`git`/`local` |
| `sources[].url` | string | `market` 与 `git` 必填，`local` 忽略 | |
| `sources[].path` | string | `local` 必填，`market`/`git` 忽略 | |
| `sources[].ref` | string | 否 | 仅 git；不写 = 仓库的默认分支 |
| `sources[].authToken` | string | 否 | 通常写 `${ENV_VAR}` 引用；已经 `brickkit login` 登录过的话优先用存下来的 `.brickkit/credentials` |
| `sources[].enabled` | `*bool` | 否（默认 `true`） | 禁用的源在解析时整个跳过 |

## `components[]`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `components[].id` | string | 是 | 跟 Manifest 的 `metadata.id` 同一条 `scope/name` 规则 |
| `components[].version` | string | 是 | 精确版本，跟 Manifest 的 `metadata.version` 同一条规则 |
| `components[].mode` | string | 否 | `enabled`/`disable`/`debug`/`local` 四选一，或不写——不写＝跟着上层走（AGENTS.zh.md §5.4），`enabled`＝钉住一定跑，`disable`＝钉住一定不跑，`debug`＝钉住一定跑、**并且**在你自己机器上跑成**你自己启动**的裸进程，`local`＝钉住一定跑、并且在你自己机器上跑成**brickkit 自己启动并监管**的裸进程（见下方互斥说明）——只有 `docker` 目标接受 `debug`/`local`，`k8s` 下两者都在解析阶段就拒绝 |
| `components[].localPort` | int | 否 | 只有在同时写了 `mode: debug` 或 `mode: local` 时才合法——单独写会被拒绝，不是悄悄忽略；`1`–`65535`；在项目里全部组件的 `localPort` 之间必须唯一。在 `mode: debug` 下它纯粹是路由信息（进程听哪个端口是你自己的进程自己决定的，这里只是告诉 brickkit 该往哪路由）；在 `mode: local` 下 brickkit 默认自动分配一个空闲端口，这个字段是"固定某个端口"的手动覆盖——这是 `mode: debug` 做不到的，因为 brickkit 根本不掌控那个进程的启动 |
| `components[].servedBy` | string（`id@version`） | 否 | 见下方互斥说明 |
| `components[].expose` | bool | 否（默认 `false`） | |
| `components[].hostname` | string | `expose: true` 且 `deploy.target: k8s` 时必填 | `docker` 下不需要——Compose 的暴露是一个宿主机端口，不是一个域名 |
| `components[].exposePort` | int | 只有 `expose: true` 时才合法 | `1`–`65535`，在全部组件的 `exposePort` 之间必须唯一。只在 `deploy.target: docker` 下才有意义（会变成宿主机端口映射）。**校验器不会因为 `deploy.target: k8s` 就拒绝它**——K8s 那条路走的是 `hostname` + 生成的 Ingress——但它也不是被静默忽略：`brickkit up`（含 `--dry-run`）会警告这个字段在当前部署目标下不生效。这是有意为之：只有某一种部署目标才用的字段，只警告、不拒绝，这样同一份 `brickkit.yaml` 只改 `deploy.target` 就能在 `docker` 与 `k8s` 之间切换。 |
| `components[].tlsSecret` | string | 只有 `expose: true` 时才合法 | **仅 K8s**，指向一个已经存在的、装着 Ingress TLS 证书的 Secret 名 |
| `components[].serviceAccountName` | string | 否 | **仅 K8s**；引用一个运维已经建好的 SA——写了它，平台只引用、绝不为这个组件生成一个新的 |
| `components[].config` | `map[string]any` | 否 | key 会拿去跟组件自己的 `configSchema.properties` 核对（对不上只警告、不阻断，见 [04-environment-variables.md](04-environment-variables.md) 第五节第二条）；值从不做类型校验 |
| `components[].resources.requests.cpu` / `.memory` | string | 否 | 跟 Manifest 的 `deployment.resources.requests` 同一个结构（自由字符串，Kubernetes 数量语法）。怎么跟 Manifest 自己的推荐值、CLI 默认值三层合并见 [05-resource-binding.md](05-resource-binding.md) |
| `components[].resources.limits.cpu` / `.memory` | string | 否 | 同上，对应 Manifest 的 `deployment.resources.limits` |
| `components[].replicas` | `*int` | 否（默认 `1`，**仅 K8s**） | 写了就必须 `≥1`——`0` 会被拒绝，不会被当成"关掉"处理；真要停掉一个组件请用 `mode: disable`，它会走级联计算、提醒依赖方，而 `replicas: 0` 会让依赖方照常启动、照常拿到地址，然后连到一个根本不存在的后端 |
| `components[].labels` | `map[string]string` | 否 | 跟 Manifest 的 `deployment.labels` 同一条保留键规则（不能以 `brickkit.io/` 或 `com.docker.compose.` 开头，不能精确等于 `app`）；逐键合并覆盖 Manifest 自己的 `deployment.labels`，冲突时这一侧赢 |

### `components[].mode: debug` / `components[].mode: local` / `.servedBy` / `.replicas` 之间的互斥

这几个字段描述的是几种不同、互不相容的"这个组件的进程到底跑在哪"的设想，校验器把每一对组合都拦了下来：

- **`mode: debug`/`local` + `servedBy`**——两种 mode 都直接拒绝：`debug`/`local` 的意思都是"这个组件跑在你自己机器上，脱离任何容器"；`servedBy` 的意思是"这个组件的代码已经编进了另一个组件的镜像里"。一个组件不可能同时"没有被容器化"又"被合并进了别人的容器"。
- **`servedBy` 链式嵌套**——一个组件的 `servedBy` 不能指向一个自己也声明了 `servedBy` 的目标（"外壳不能被另一个外壳收编"），一个组件也不能一边是别的组件 `servedBy` 的目标、一边自己又是 `mode: debug` 或 `mode: local`——外壳必须能从容器/集群网络里被访问到，而一个跑在开发者自己机器上的进程在结构上做不到这一点，不管这个进程是谁启动的。
- **`servedBy` 自引用**——`components[].servedBy` 不能等于这一条自己的 `id@version`。
- **`replicas` + `mode: debug`/`local`**——两种 mode 都拒绝：都意味着这个组件在你自己机器上是单个进程；副本数描述的是多个 Pod，对一个根本不是 Pod 的进程毫无意义。

`localPort` 不带 `mode: debug` 或 `mode: local`、`exposePort`/`tlsSecret` 不带 `expose: true`，跟组件那侧 `healthCheck.startPeriodSeconds` 在 `type: none` 下的处理是同一种哲学（[07-component-yaml-reference.md](07-component-yaml-reference.md)）——在解析阶段就拒绝，不是悄悄什么都不做，因为"写了配置却被悄悄忽略"正是这整个平台最想避免的那类失败。

## `resources[]`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `resources[].kind` | string | 是 | `database`/`cache`/`mq`/`storage`/`search`/`smtp` 之一 |
| `resources[].engine` | string | 是 | 非空、自由字符串——跟 Manifest 侧 `dependencies.resources` 的 `engine` 字段同样是"纯描述、不跟任何东西交叉核对" |
| `resources[].id` | string | 是 | 在项目全部资源里唯一 |
| `resources[].host` | string | 是 | 自由字符串 |
| `resources[].port` | int | 是 | `1`–`65535` |
| `resources[].username` | string | 否 | |
| `resources[].password` | string | 否 | 解析器不强制要求这里必须写 `${ENV_VAR}` 引用，但写一个明文密码会在 `up` 时触发一条明文密码警告（AGENTS.zh.md §7 那条"必须用 ${ENV_VAR} 引用"是靠警告落地的，不是解析阶段的硬错误） |
| `resources[].existingSecret` | string | 否 | 仅 K8s。引用外部系统（Vault Secrets Operator、External Secrets Operator、Sealed Secrets……）已经放进集群的 Secret，而不是让平台从 `password` 生成一份。与 `password` 互斥——两个都写会报错。平台从不读写这个值，只是把 `secretKeyRef` 指向这个名字，用的 key 与平台自己生成时会用的一样（`password` 或 `secret-key`）。`docker` 下被忽略（有警告）。声明了 `secret: true` 的 `configSchema` 属性也能用同一个想法：把标量值换成 `{ existingSecret: <名>, key: <Secret 里的 key> }`。这条路与"把值放进进程环境"那条路怎么取舍，见[密钥](../07-patterns/10-secrets.md)。 |
| `resources[].bindings[]` | `[]Binding` | 否 | 见下 |

### `resources[].bindings[]`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `resources[].bindings[].componentId` | string | 是 | **在校验阶段完全不会拿去跟 `components[]` 交叉核对**——一条绑定指向一个哪儿都没声明过的组件，在 `up` 时只是一条**警告**（"悬空绑定"，天然无害：反正没人会去读它），从来不是解析阶段的错误。这是故意的：它让项目可以先把资源和绑定声明好，再去 `brickkit add` 这个组件本身；也让 `brickkit remove` 留下的一条残留绑定不会拖垮之后的每一条命令。 |
| `resources[].bindings[].database` / `.vhost` / `.bucket` / `.index` | string | 否 | **彼此互斥**——一条绑定最多只能写其中一个，写哪个（如果要写的话）取决于 `kind`：`database`→`database`，`mq`→`vhost`，`storage`→`bucket`，`search`→`index`；`cache` 和 `smtp` 这四个一个都不接受。完整细节，包括真实生成的校验错误，见 [05-resource-binding.md](05-resource-binding.md)"绑定的槽位名写错了"那一节。 |
| `resources[].bindings[].envPrefix` | string | 否 | 必须匹配 `^[A-Z][A-Z0-9_]*$`——大写字母开头，后面跟大写字母/数字/下划线，因为这个字符串会被直接拼进环境变量名（`{envPrefix}_DATABASE_HOST`） |

**校验器还会拦下另一种、性质完全不同的撞车**：把同一个组件绑到两个同 `kind` 的资源上、两边都没写 `envPrefix`（或者写了同一个 `envPrefix`），会生成完全相同的一组环境变量名两次，后一条绑定的值在注入时悄悄覆盖前一条。这是硬错误，不是警告——跟悬空绑定不一样，这里的失败方式是"组件连到了错误的数据库，而且没有任何迹象表明哪里不对"。真实生成的报错文案和修复方法见 [05-resource-binding.md](05-resource-binding.md)"一个组件绑两个同类资源"那一节。

## `installer`

| 字段 | 类型 | 是否必填 | 约束 |
| --- | --- | --- | --- |
| `installer.requireSignature` | `*bool` | 否（默认 `true`） | |
| `installer.publicKeys` | `map[string]string` | 否 | `publicKeyRef`（组件签名里用的那个名字）→ 存着这把公钥的本地文件路径 |

⚠️ **`publicKeys` 才是真正让签名校验生效的那个字段**——一把公钥都没配的话，不管 `requireSignature` 写没写、写的是什么，验签整个都是关掉的，而且 CLI 只会警告一次，不是每次运行都提醒。为什么公钥必须留在项目这一侧、不能跟着签名从市场一起取，完整理由见 AGENTS.zh.md §7 和 [06-signing-and-trust.md](06-signing-and-trust.md)。

## 延伸阅读

- AGENTS.zh.md §7——这篇文档展开的那份可以直接复制粘贴的骨架
- AGENTS.zh.md §5.4——`mode` 完整的级联规则，这篇文档只说了这个字段的形状
- [04-environment-variables.md](04-environment-variables.md)——`config`、资源绑定、`servedBy` 到了容器的运行环境里具体变成了什么
- [05-resource-binding.md](05-resource-binding.md)——绑定槽位错误与同类资源撞车的完整报错，以及配额合并链
- [02-dependency-resolution.md](02-dependency-resolution.md)——级联与解析两个阶段拿 `mode`、强/弱依赖、菱形依赖去重到底做了什么
- [07-component-yaml-reference.md](07-component-yaml-reference.md)——组件那一侧的对应文档，本文件的 `components[]`/`resources[]` 条目正是绑定到那里描述的组件上
- [06-signing-and-trust.md](06-signing-and-trust.md)——`installer.publicKeys` 到底把住了什么、为什么
