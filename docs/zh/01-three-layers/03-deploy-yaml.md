# deploy.yaml 详解

部署文件回答"**怎么跑**"：部署到哪、每个组件跑不跑、端口怎么开、外壳承载谁。团队共享的那份叫 `deploy.yaml`；
个人的那份是 `deploy.local.yaml`（结构完全一样，见 [下一篇](04-deploy-local-yaml.md)）；别的环境可以各有一份，
比如 `deploy.prod.yaml`，用 `-f` 指定。

## 完整示例

```yaml
target: k8s                       # docker | podman | k8s

k8s:                              # 只在 target: k8s 时生效的项目级设置
  namespace: shop-prod
  ingressClass: nginx
  networkPolicy:
    enabled: true

vars:                             # 覆盖 config/vars.yaml 里的同名公共变量
  PG_HOST: pg.prod.internal

components:
  - id: erp/shell                 # 外壳：成员嵌在它下面
    members:
      - id: erp/api
      - id: people/basic
  - id: people/basic@0.9.0        # 同一组件的另一个版本：只覆盖这一个版本，独立部署
  - id: portal/web
    expose: true
    hostname: shop.example.com    # K8s 下 expose 生成 Ingress，要写域名
    replicas: 3
  - id: report/batch
    mode: disable                 # 这个环境不跑它
```

## 顶层字段

| 字段 | 含义 |
| --- | --- |
| `target` | 必填。`docker`、`podman` 或 `k8s`：决定生成 `compose.yaml` 还是 K8s 清单、调用哪个引擎 |
| `k8s` | K8s 专属的项目级设置：`context`、`namespace`、`createNamespace`、`podSecurity`、`imagePullSecrets`、`ingressClass`、`ingressAnnotations`、`networkPolicy`、`serviceAccount`。`target` 不是 `k8s` 时写了只警告 |
| `vars` | 覆盖 `config/vars.yaml` 的同名公共变量，只影响 `$var:` 引用（见 [公共变量](06-vars-and-var-ref.md)） |
| `components` | 组件的部署条目 |

## 组件部署条目

每个条目的 `id` 写裸 ID（`erp/api`，指这个组件的**默认版本**）或 `id@版本`（只指那一个版本）。
`brickkit.yaml` 里的每个组件版本，在部署文件里恰好对应一个条目——`lint` 和 `up` 都会核对。

| 字段 | 含义 |
| --- | --- |
| `mode` | 跑不跑、怎么跑，见下表；不写就是"跟着上层走" |
| `localPort` | `mode: local` / `mode: debug` 时，这个进程在你机器上的端口；只能和这两个 `mode` 一起写，或者写在 `deploy.local.yaml` 里 `focus` 组件的条目上（它按 `mode: local` 跑，见 [焦点运行](../02-project-guide/04-focus-run.md)） |
| `expose` | 对外开放：Docker 下映射到宿主机端口，K8s 下生成 Ingress。不写就不可达 |
| `exposePort` | Docker 下映射到宿主机的哪个端口（缺省与组件端口相同）；K8s 下不用 |
| `hostname`、`tlsSecret` | K8s 下 Ingress 的域名与 TLS 证书 Secret |
| `replicas` | K8s 下的副本数（缺省 1）；大于 1 时自动生成 PodDisruptionBudget |
| `resources` | 资源配额 `requests` / `limits`，覆盖组件建议的值 |
| `serviceAccountName` | K8s 下用一个运维已经建好的 ServiceAccount |
| `labels` | 原样透传：Docker 下是容器标签，K8s 下是 Pod 注解（给 Traefik、Prometheus 这类工具读） |
| `stopGracePeriodSeconds` | 停机宽限期（秒），覆盖组件在 `component.yaml` 里推荐的值：收到停止信号后等这么久再强杀 |
| `skipWaitFor` | 启动时不等这几个强依赖就绪（只去掉等待，照样连得到它们）。只对 Docker / Podman 有效：K8s 没有启动顺序。见 [外壳合并造成的启动环](../04-shell/04-members-management.md) |
| `members` | 只有外壳条目有：它承载的成员，每个成员也是一个完整的条目 |

只对 K8s 有意义的字段（`hostname`、`tlsSecret`、`replicas`、`serviceAccountName`）在别的 `target` 下写了只警告，
命令照常执行——同一份条目可以在两种目标之间切换。反过来也一样：只对 Docker / Podman 有意义的 `exposePort`、
`skipWaitFor` 在 `target: k8s` 下写了也只警告。

## `mode`：跑不跑、怎么跑

| 值 | 意思 | 写在哪 |
| --- | --- | --- |
| 不写 | 跟着上层走：没人依赖的顶层组件默认跑，被依赖的组件只要还有一个上游在跑就跟着跑 | 哪都行 |
| `enabled` | 一定跑；它的强依赖被关掉时报错 | 团队文件或个人文件 |
| `disable` | 一定不跑；**强**依赖它的组件跟着不跑（钉住要跑的依赖方会报错），弱依赖它的组件照常跑，只是拿不到它的地址 | 团队文件或个人文件 |
| `local` | 一定跑，但作为你机器上的一个进程：BrickKit 探测启动命令、拉起它、盯着它 | 团队文件或个人文件 |
| `debug` | 一定跑，进程由你自己在 IDE 里启动，平台负责让别的组件找到它 | **只能**写在个人的 `deploy.local.yaml` |

`debug` 只能写在个人文件里，是因为它记录的是"我此刻在自己机器上调这个组件"——一个关于你、关于此刻的事实，
不该出现在团队评审的文件里，也不该被别人 `git pull` 到。写在 `deploy.yaml` 里会被拒绝：

```text
❌ 错误：deploy.yaml 校验失败
   文件：deploy.yaml
   components[0].mode：mode: debug 只能写在 deploy.local.yaml 里：它记录的是"我此刻在本机调试这个组件"，不是团队决策。先 brickkit local on，再到那里设置
   建议：完整字段说明：brickkit docs 11-reference/03-deploy-yaml-schema（网页版：https://github.com/brickKit/brickKit/blob/v1.1.0/docs/zh/11-reference/03-deploy-yaml-schema.md）
```

`local` 和 `debug` 的进程跑在你的机器上，所以只在 `docker` / `podman` 目标下有意义；集群里的 Pod 连不到你的笔记本。
怎么用它们调试见 [本地调试工作流](../02-project-guide/03-local-debug-workflow.md)。

## `members`：外壳承载谁

外壳是把多个组件编进一个进程里跑的组件。**谁被承载，只写在这里**：成员作为完整条目嵌在外壳条目的 `members` 下面，
只嵌一层。`brickkit.yaml` 里外壳那一行的 `kind: shell` 只是标记，由 CLI 维护。

- 外壳在跑：`members` 下的成员都在外壳进程里，不单独起容器；成员条目里的 `replicas`、`resources` 等字段这时不生效；`expose` 例外，由外壳替它开（见[成员管理](../04-shell/04-members-management.md)）。
- 外壳不跑（`mode: disable`，或者没人需要它）：成员按自己条目里的字段独立部署。
- 成员条目写裸 ID 时，外壳承载的是默认版本；要承载另一个版本，把那个版本的 `id@版本` 条目移到外壳下面。

外壳怎么声明它能承载哪些成员版本、成员怎么加入和移出，见 [外壳机制](../04-shell/README.md)。

## `vars`：按环境覆盖公共变量

部署文件的 `vars:` 与 `config/vars.yaml` 的写法相同，同名时部署文件优先。典型用法是每个环境的部署文件写自己的
数据库地址，组件配置里统一写 `$var:PG_HOST`。`vars:` 里的值可以是 `${VAR}`。它只影响 `$var:` 的查找，
不会覆盖组件配置文件里直接写的值——见 [解析优先级](08-resolution-priority.md)。

## `add` / `remove` / `upgrade` 会自动改它

| 操作 | 部署文件的变化 |
| --- | --- |
| `brickkit add` | 为新组件加一个条目（裸 ID；为依赖加入的非默认版本写 `id@版本`）；外壳的成员自动嵌到外壳下面 |
| `brickkit remove` | 删掉这个组件的条目；删的是外壳时，成员挪回顶层独立运行（它们的字段原样保留） |
| `brickkit upgrade` | 默认版本变了时，条目跟着走；外壳升级时，成员条目随外壳编进的新版本调整 |

`deploy.yaml` 与存在的 `deploy.local.yaml` 会被一起更新：你的个人文件不会因为 `add` 而落后。

每次这样改完，`components` 都按组件 ID 的字典序排好，和 `brickkit.yaml` 是同一个顺序：裸 ID 的条目在前，同一个组件的
`id@版本` 条目按版本号跟在后面；外壳下面的 `members` 各自排。条目的字段和注释跟着条目一起挪，顺序不影响什么会运行、怎么运行。
