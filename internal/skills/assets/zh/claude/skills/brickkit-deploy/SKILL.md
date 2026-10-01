---
name: brickkit-deploy
description: 把 BrickKit 项目部署到 Docker、Podman 或 Kubernetes，编辑 deploy.yaml / deploy.local.yaml（target、mode、端口、暴露、副本、k8s 块、vars），开关本地模式（brickkit local on / off / refresh），本地调试（mode debug / local），给 config/ 填值与密钥（$var:、${VAR}、file://、existingSecret），构建镜像（brickkit build），在部署文件里安排外壳与成员、skipWaitFor，以及配多个环境（-f）时使用。当用户问「怎么上线 / 怎么本地调试 / 密码放哪 / 怎么暴露服务」时，这个技能适用。
---

# 部署

## 什么时候用这个技能

- 要把项目跑到 Docker / Podman / Kubernetes 上
- 要在本机调试某个组件，或者让它不走容器直接跑
- 要给组件填连接串、密码、证书这类值
- 要把某个组件暴露到外面，或者调副本、配额、标签
- 要把几个组件合进一个外壳进程里跑
- 要配多个环境（开发 / 生产）

## 你会猜错的地方

**1. 部署怎么跑写在 `deploy.yaml`，不在 `brickkit.yaml`。**

`brickkit.yaml` 只管有什么组件、什么版本。`deploy.yaml` 顶层是 `target`（`docker` / `podman` /
`k8s`，必填；Podman 用 `podman compose` 跑同一份生成的 compose 文件）、`k8s:` 块（`context`、`namespace`、
`createNamespace`、`podSecurity`、`imagePullSecrets`、`ingressClass`、`ingressAnnotations`、`serviceAccount`、
`networkPolicy`，只在 `target: k8s` 时生效，别的 target 下写了会警告）、`vars:`，以及
`components:` 下每个组件版本一个条目：`mode`、`localPort`、`expose` / `exposePort` / `hostname` /
`tlsSecret`、`replicas`、`serviceAccountName`、`resources`、`labels`、`skipWaitFor`、外壳的 `members`。
其中 `hostname`、`tlsSecret`、`replicas`、`serviceAccountName` 只对 K8s 有意义，`exposePort`、`skipWaitFor`
只对 docker / podman 有意义，写在别的 target 下会警告。
不带版本的 `id` 指默认版本；每个兼容版本要有自己的 `id@版本` 条目（外壳下的成员也算）。条目数对不上
`brickkit.yaml` 是 `DEPLOY_INCONSISTENT`。这些条目由 `add` / `remove` / `upgrade` 维护——别靠手写条目来加组件。

**2. 个人的事写进 `deploy.local.yaml`，不是改 `deploy.yaml`。**

`brickkit local on` 第一次会把 `deploy.yaml` 复制成 `deploy.local.yaml`（内容一字不差，只有开头那几行
注释——团队文件头——换成个人文件的说明，用 CLI 当前的语言；已存在就沿用，从不覆盖），并打开本地模式。
**开着时，跑部署、查部署的命令（`up`、`down`、`status`、`sync`、`lint`、`build`）整份读
`deploy.local.yaml`，完全替代 `deploy.yaml`，不合并**——所以你看到的就是实际跑的，但你改 `deploy.yaml`
也不会生效。`graph` 和 `deps` 永远读 `deploy.yaml`，人人看到的都一样。这份文件不进 Git。
`local off` 关开关、文件留着；`local status` 看开关和文件是否还跟 `brickkit.yaml` 一致；
单次忽略用 `--no-local`。该写在这里的：`mode: debug`、本机空着的 `localPort`、指向你自己数据库
的 `vars`、换一个 target。

**3. 团队改了 `deploy.yaml`，你要 `brickkit local refresh`。**

CLI 从不替你合并。团队加了组件后，你的本地文件就对不上了，`up` 会拒绝并提示刷新。`refresh`
把旧文件存成 `deploy.local.yaml.bak`、重新复制一份，并逐条列出旧文件里的本地改动
（`- [erp/backend] mode: debug (now unset)`），由你手工搬回去。

**4. `mode: debug` 只能写在 `deploy.local.yaml`。**

它说的是「我此刻在本机调试这个组件」，不是团队决策，写进 `deploy.yaml` 会被拒绝。写了之后这个
组件**不生成容器**，其他容器通过 `extra_hosts` 把它的版本化服务名解析到宿主机；每个组件给一个
`localPort`，CLI 生成 `local-debug.<版本化服务名>.env` 给 IDE 加载，让进程监听那个端口。
**迁移不会自动跑**，第一次要手动跑一遍。

`mode: local` 是「不用容器，BrickKit 替我起」：从本地仓库探测启动命令、起裸进程、前台监管（`Ctrl+C`
停掉），`localPort` 可不写（自动挑空端口）。它不是个人的，可以写在 `deploy.yaml`。进程继承你终端的环境变量，
平台自己的名字除外（`COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、
`BRICKKIT_SERVED_MEMBERS_CONFIG`、所有 `*_ENDPOINT`、组件自己 `configSchema` 里的键）——这些只取 BrickKit
给的值，终端里残留的 `export` 冒充不了它们。两者都只支持
docker / podman，`target: k8s` 下拒绝——集群里的 Pod 到不了你的机器。从本地仓库跑的组件，
仓库里的 `metadata.version` 必须等于项目的默认版本，否则 `up` 拒绝（`brickkit upgrade <id>@<仓库版本>`
或检出对应 tag）；所以只有默认版本能这样跑，兼容版本写 `mode: local` / `debug` 会被拒。

**5. 值写在 `config/`，键就是环境变量名。**

`config/<scope>-<name>.yaml` 是默认版本的配置，兼容版本是 `config/<scope>-<name>@<版本>.yaml`。
值有五种写法：

| 写法 | 含义 |
| --- | --- |
| `DB_PORT: 5432` | 字面量 |
| `DB_HOST: $var:DB_HOST` | 公共变量：取 `config/vars.yaml`，部署文件的 `vars:` 同名时优先。未定义就报错，没有隐式兜底 |
| `DB_PASSWORD: ${DB_PASSWORD}` | 进程环境，其次项目根的 `.env`（不进 Git）；`${DB_PASSWORD:-dev}` 带默认值，`${VAR:-}` 表示可以为空 |
| `TLS_CERT: file://.secrets/cert.pem` | 文件内容，路径相对项目根（`.secrets/` 不进 Git） |
| `DB_PASSWORD: { existingSecret: db, key: password }` | 集群里已有的 Secret，仅 K8s、仅 `secret: true` 的键 |

`$var:NAME` 冒号后**没有空格**——写成 `$var: NAME` 是 YAML 映射，不是引用。

`add` 把必填键写成 `KEY: ""`：没填 `up` 就拒绝并点名那个键。可选键写成注释行（`# LOG_LEVEL: info`）：
保持注释就跟着组件的默认值走，升级带来的新默认值也会跟过来，别为了「显式」取消注释。

**环境差异放 `vars:`，不是复制 `config/`**：`config/` 各环境共用，`deploy.prod.yaml` 的
`vars: { DB_HOST: db.prod.internal }` 就把所有 `$var:DB_HOST` 换掉了。`config/vars.yaml` 里不能再
`$var:` 引用别的公共变量。

**6. 密钥不落明文。**

`${VAR}` 在生成部署文件时就必须有定义（进程环境或 `.env`），每种部署目标都查：没定义就停下，而不是
让 compose 悄悄换成空字符串。之后 Docker 下 CLI 把它原样留在 `compose.yaml`，由 compose 启动时求值；K8s 下 CLI 求值，
`secret: true` 的值进生成的 Secret（Deployment 里是 `secretKeyRef`）。`secret: true` 的键写了明文会警告：
`config/` 是要提交进 Git 的。已经由 Vault / ESO 放进集群的
Secret 用 `existingSecret` 引用，平台不读不写它的值。**平台不会替你去 Vault 取值**——能把值放进进程
环境的任何办法今天就能用。

**7. `up` 从不构建镜像。**

本地源的组件（和只有 `deployment.build` 的组件）先 `brickkit build`；镜像不在时 `up` 报
`IMAGE_MISSING`。镜像 tag 就是组件版本，同版本的镜像已存在时 `build` 跳过，所以改了代码没升版本要
`brickkit build <id> --force`。本机构建的外壳镜像里编进的成员版本与它的 `component.yaml` 不一致时是
`IMAGE_STALE`。有 `image:` 的 git / 市场组件是拉取的。

**数据库、缓存没有「资源绑定」。** 它们由运维部署，组件通过自己的配置项（`DB_HOST`、`DB_PASSWORD`……）
去连。数据库本身由你建一次；表由组件的迁移建。

**8. 外壳成员写在部署文件里，嵌在外壳条目下面。**

```yaml
components:
  - id: erp/shell
    members:
      - id: erp/api          # 默认版本的 erp/api 在外壳里跑
      - id: erp/auth@1.2.0   # 兼容版本也可以进外壳
```

成员条目是完整的部署条目，只是挪了位置。外壳在跑时，成员没有自己的容器，`*_ENDPOINT` 指向外壳；
成员自己的 expose / labels / 健康检查不生效。成员写 `mode: debug` / `mode: local` 就离开外壳、
单独以裸进程跑。**承载的成员版本必须等于外壳 `component.yaml` 编进的版本**，否则报错并给三条出路：
升级外壳；把成员条目挪出外壳独立跑；两个都要——给编进的版本加一行 `requiredBy: [<外壳>]`、把
`id@那个版本` 嵌进外壳，另一个版本留在顶层。`add` 一个外壳时这些都会自动写好。
想验证每个组件脱离外壳也能起：`brickkit up --ignore-shells --dry-run`。

**9. 成员合进外壳可能在 compose 下形成启动环。**

外壳作为整体启动，继承了所有成员的依赖：外壳里的 A 依赖外面的 X，X 又依赖外壳里的 B，就成了永远
起不来的 `depends_on` 环。报错会列出那几条边，三条出路：把 X 也挪进外壳；把一个成员挪出来；或在条目
上写 `skipWaitFor: [X 的 ID]`——只去掉启动时的等待，连接不变，组件自己必须在依赖起来之前不断重试。
K8s 和裸进程下它不起作用（会警告）。

**10. 多环境 = 每个环境一份完整的部署文件。**

`brickkit up -f deploy.prod.yaml`。没有 overlay、没有继承、没有合并。`-f` 同时忽略
`deploy.local.yaml` 与本地模式开关，个人设置不会漏进生产。

**11. `limits` 没有默认值，平台不猜。**

只有 `requests` 有默认（`100m` / `128Mi`）。建议 CPU 只设 requests、不设上限（CPU limit 会限流成
p99 毛刺）；内存 requests = limits（拿 Guaranteed QoS）。写法：
`resources: { requests: { cpu: 200m, memory: 256Mi }, limits: { memory: 256Mi } }`。
部署条目上的配额逐字段覆盖组件的建议值。节点上只要求所有 `requests` 之和放得下，`limits` 超卖是常态。
真正吃内存的是空转的运行时（一个 JVM 的底线就是几百 MB）：用外壳把它们合进一个进程，或者少跑几个——
别为了省内存把两个组件的代码合成一个。

**12. 平台不做网关，`labels` 是给网关的透传口。**

给部署条目写 `labels`，平台原样搬进 Docker service labels / K8s annotations，键值不解释，逐键覆盖
组件的 `deployment.labels`。值必须加引号（`"true"`）；`app`、`brickkit.io/*`、`com.docker.compose.*`
是平台自己的键，会被拒。Docker 下网关要接进项目网络 `brickkit-<项目名>-net` 才连得到组件。别退回去手写满是
版本化服务名的 file-provider 配置——每次升级它都静默过期。

**13. `publicKeys` 是唯一让验签生效的字段**（`brickkit.yaml` 的 `installer:`，市场组件用）。
一个公钥都没配，`requireSignature: true` 也不起作用。

**14. `focus:` 是个人的「只跑这一个」开关。**

`focus: <id>` 只存在于 `deploy.local.yaml`（`deploy.yaml` 里会被拒，`target: k8s` 下也会被拒）。写了它，
`up` 只启动这个组件——从本地源码跑，相当于 `mode: local`（你写的 `mode: debug` 会保留）——和它需要的；
钉住的组件（`enabled` / `local` / `debug`）照样启动。在组件目录里 `brickkit up` 或 `up --focus <id>` 会写上它
（需要时顺手打开本地模式），`up --all` 去掉它；`-f` 与 `--no-local` 跳过它；`sync` 不看它；`local refresh`
把它列在你的本地修改里。会让文件通不过校验的焦点，在写任何东西之前就被拒绝。

## 机制是怎么运作的

**地址格式在所有 target 下一样**：`http://<版本化服务名>:<端口>`，组件代码零修改，多版本天然共存。

**暴露**：`expose: true`。K8s 下必须给 `hostname`（生成 Ingress），`tlsSecret` 可选；Docker 下映射到宿主
端口，`exposePort` 可改。不写就不暴露。`replicas > 1` 时 K8s 自动生成 PDB。

**迁移**：K8s 下是一个独立的 Job（不是 InitContainer，避免多副本并发迁移），Docker 下是一次性容器；
失败则主服务不启动。

**`up` 做了什么**：装载三层文件 → 判定谁启动 → 检查镜像（缺了给出 build 提示，git 镜像去拉）→ 生成部署文件
（`--dry-run` 停在这里）→ 跑迁移（失败则主服务不启动）→ 交给引擎启动 → 前台监管 `mode: local` 的进程。

**`status` / `down` / `graph`**：`status`、`down` 跟 `up` 一样读当前生效的部署文件（也接受 `-f`）；
`down` 不删 volume。`graph` 从不读本地模式，只写在 `deploy.local.yaml` 里的东西不会出现在图上。

## 去哪查更细的

- 参数：`brickkit up --help`、`brickkit local --help`、`brickkit build --help`、`brickkit lint --help`
  （`lint --strict` 还会查 `${VAR}` 和 `file://` 引用能否取到）
- 某个组件要配哪些值：`.brickkit/manifests/<scope>/<name>/<版本>/BRICKKIT.md` 的配置指南
- 完整规范：<https://github.com/brickKit/brickKit> 根目录 `AGENTS.zh.md`
