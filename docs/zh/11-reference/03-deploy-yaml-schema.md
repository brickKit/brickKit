# 部署文件字段参考

`deploy.yaml`、`deploy.local.yaml`、`deploy.<环境>.yaml` 是同一种结构。怎么用，见 [deploy.yaml](../01-three-layers/03-deploy-yaml.md) 与 [deploy.local.yaml](../01-three-layers/04-deploy-local-yaml.md)。

写了不认识的字段当场报错。只对另一种部署目标有用的字段不报错，会警告"在 target: … 下不起作用，已忽略"——换目标时它们生效。

## 顶层

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `target` | 字符串 | ✅ | `docker` / `podman` / `k8s` |
| `focus` | 字符串 | | 只能写在 `deploy.local.yaml` 里：一个组件 ID。只有这个组件和它需要的组件启动，它从源码跑（按 `mode: local`，写了 `mode: debug` 就按 debug）；`target: k8s` 下不能用。见 [deploy.local.yaml](../01-three-layers/04-deploy-local-yaml.md) |
| `vars` | 映射 | | 覆盖 `config/vars.yaml` 里的同名公共变量，只影响 `$var:` 的查找；键必须是合法的环境变量名 |

## k8s

项目级的 Kubernetes 设置。`target` 不是 `k8s` 时写了会被警告、忽略。

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `k8s.context` | 字符串 | | 部署到哪个 kubeconfig 上下文。写了之后，真正部署前 `up` 确认 `kubectl` 当前连着的就是它 |
| `k8s.namespace` | 字符串 | | 缺省 `brickkit-<项目名>`；规则同项目名 |
| `k8s.createNamespace` | 布尔 | | 缺省 `true`；只有命名空间级权限时写 `false` |
| `k8s.podSecurity` | 字符串 | | 只能是 `restricted`：给每个容器生成 restricted 级别的 `securityContext` |
| `k8s.imagePullSecrets` | 字符串列表 | | 拉镜像用的 Secret 名 |
| `k8s.ingressClass` | 字符串 | | Ingress 的 class |
| `k8s.ingressAnnotations` | 字符串映射 | | 原样写到每个 Ingress 上 |
| `k8s.serviceAccount.enabled` | 布尔 | | `true` 时每个组件一个 ServiceAccount，不挂载令牌 |
| `k8s.networkPolicy.enabled` | 布尔 | | `true` 时按依赖图为每个组件生成 NetworkPolicy |
| `k8s.networkPolicy.ingressController.namespace` | 字符串 | 见规则 | Ingress 控制器所在的命名空间；开了网络策略、又有 `expose: true` 的组件时必填 |
| `k8s.networkPolicy.ingressController.podSelector` | 字符串映射 | | Ingress 控制器 Pod 的标签 |
| `k8s.networkPolicy.allowFrom[].name` | 字符串 | ✅ | 这条放行给谁（写进注解，日后看得懂为什么开） |
| `k8s.networkPolicy.allowFrom[].namespace` | 字符串 | ✅ | 放行来自哪个命名空间 |
| `k8s.networkPolicy.allowFrom[].podSelector` | 字符串映射 | | 只放行那个命名空间里带这些标签的 Pod |
| `k8s.networkPolicy.allowFrom[].ports` | 整数列表 | | 只放行这些端口（1–65535） |
| `k8s.networkPolicy.egress.enabled` | 布尔 | | `true` 时也生成出站策略：依赖图里的依赖与 DNS 自动放行，其余一律拒绝 |
| `k8s.networkPolicy.egress.allowTo[].name` | 字符串 | ✅ | 这个出站目标是什么（如 `main-db`） |
| `k8s.networkPolicy.egress.allowTo[].namespace` | 字符串 | 二选一 | 集群内的目标：所在命名空间 |
| `k8s.networkPolicy.egress.allowTo[].cidr` | 字符串 | 二选一 | 集群外的目标：地址段；与 `namespace` 只能写一个 |
| `k8s.networkPolicy.egress.allowTo[].podSelector` | 字符串映射 | | 集群内目标 Pod 的标签 |
| `k8s.networkPolicy.egress.allowTo[].ports` | 整数列表 | | 只放行这些端口 |

开出站策略时，数据库这类外部服务要自己写进 `allowTo`：平台只知道依赖图里的组件，不认识 `config/` 里的一串地址。

## components

每个组件版本恰好一个条目，与 `brickkit.yaml` 一一对应（外壳成员的条目嵌在外壳下面，同样算）。

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `components[].id` | 字符串 | ✅ | 裸 ID 指默认版本；`<组件ID>@<版本>` 指那个版本（兼容版本必须这样写） |
| `components[].mode` | 字符串 | | `enabled` / `disable` / `local` / `debug`。不写就跟着上层走。`debug` 只能写在 `deploy.local.yaml`；`local`、`debug` 在 `target: k8s` 下不允许 |
| `components[].localPort` | 整数 | | 本机进程监听的端口，1–65535；只在 `mode: local` / `mode: debug` 下能写；不能与别的条目冲突。`mode: local` 不写时自动选一个 |
| `components[].expose` | 布尔 | | 对外开放：Docker 映射宿主机端口，K8s 生成 Ingress |
| `components[].exposePort` | 整数 | | Docker 下映射到的宿主机端口，缺省等于组件端口；只在 `expose: true` 时能写；不能冲突；K8s 下忽略 |
| `components[].hostname` | 字符串 | 见规则 | Ingress 的域名；`target: k8s` 且 `expose: true` 时必填 |
| `components[].tlsSecret` | 字符串 | | Ingress 的 TLS 证书 Secret；只在 `expose: true` 时能写 |
| `components[].replicas` | 整数 | | K8s 副本数，缺省 1，必须 ≥ 1；大于 1 时自动生成 PodDisruptionBudget；不能与 `mode: local` / `debug` 同时写。要关掉组件用 `mode: disable` |
| `components[].serviceAccountName` | 字符串 | | K8s 下用一个已有的 ServiceAccount（平台只引用，不创建） |
| `components[].resources.requests.cpu` | 字符串 | | 覆盖组件建议的配额，逐字段 |
| `components[].resources.requests.memory` | 字符串 | | 同上 |
| `components[].resources.limits.cpu` | 字符串 | | 同上 |
| `components[].resources.limits.memory` | 字符串 | | 同上 |
| `components[].labels` | 字符串映射 | | 逐键覆盖组件 `deployment.labels`，原样透传 |
| `components[].skipWaitFor` | 字符串列表 | | 启动时不等这些强依赖（写不带版本的组件 ID）；必须是这个组件版本真实的强依赖；不能是自己、不能重复。Docker / Podman 容器专用，K8s 与本机进程下忽略 |

配额的优先级：部署文件的 `resources` > 组件 `deployment.resources` > 平台缺省（只有 `requests`：`100m` / `128Mi`）。

### 外壳的成员：`components[].members`

外壳条目下面可以嵌 `members`，每个成员是一个完整的部署条目，字段与上表相同，只嵌一层：

| 字段 | 类型 | 必填 | 规则 |
| --- | --- | --- | --- |
| `components[].members[].id` | 字符串 | ✅ | 裸 ID 或 `id@版本`；不能是外壳自己；一个外壳里一个组件只能有一个版本 |
| `components[].members[].mode` | 字符串 | | 同 `components[].mode`；`debug` / `local` 时这个成员这次以本机进程运行，外壳不承载它 |
| `components[].members[].localPort` | 整数 | | 同上 |
| `components[].members[].skipWaitFor` | 字符串列表 | | 同上 |
| `components[].members[].expose` | 布尔 | | 外壳承载时不生效，外壳不跑时生效 |
| `components[].members[].exposePort` | 整数 | | 同上 |
| `components[].members[].hostname` | 字符串 | | 同上 |
| `components[].members[].tlsSecret` | 字符串 | | 同上 |
| `components[].members[].replicas` | 整数 | | 同上 |
| `components[].members[].serviceAccountName` | 字符串 | | 同上 |
| `components[].members[].labels` | 字符串映射 | | 同上 |
| `components[].members[].resources.requests.cpu` | 字符串 | | 同上 |
| `components[].members[].resources.requests.memory` | 字符串 | | 同上 |
| `components[].members[].resources.limits.cpu` | 字符串 | | 同上 |
| `components[].members[].resources.limits.memory` | 字符串 | | 同上 |

只有外壳（`brickkit.yaml` 里 `kind: shell`）的条目能写 `members`，放在下面的成员必须是外壳 `component.yaml` 里编进了的组件。见 [成员管理](../04-shell/04-members-management.md)。

## 只对某种目标生效的字段

| 只在 | 字段 | 在另一种目标下 |
| --- | --- | --- |
| k8s | `k8s` 块、`replicas`、`hostname`、`tlsSecret`、`serviceAccountName` | 警告、忽略 |
| docker、podman | `exposePort`、`skipWaitFor` | 警告、忽略 |
| docker、podman | `mode: local`、`mode: debug` | `k8s` 下报错（集群里的 Pod 到不了你的机器） |
