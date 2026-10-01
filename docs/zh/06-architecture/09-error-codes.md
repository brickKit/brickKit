# 错误码参考

每一个让命令失败的错误，都带一个稳定的**错误码**。它是给脚本和 CI 用的：该重试，还是该报警、该找人。

## 在哪里看到错误码

错误先以人读的形式打在 stderr 上，紧接着一行 JSON 日志，`error_code` 就在里面：

```text
❌ 错误：数据库迁移失败
   组件：demo/caller@1.0.0
   看日志：docker compose -p brickkit-my-shop logs demo-caller-1-0-0-migration
   建议：
   1. 迁移失败时主服务不会启动：它要等迁移成功结束
   2. 修好之后重新 brickkit up：迁移容器会再跑一次
```

```json
{"time":"2026-09-29T14:10:21.916099514+02:00","level":"ERROR","message":"命令执行失败","command":"brickkit up","elapsed_ms":6564,"error_code":"MIGRATION_FAILED","error":"MIGRATION_FAILED: 错误：数据库迁移失败; 组件=demo/caller@1.0.0; 看日志=docker compose -p brickkit-my-shop logs demo-caller-1-0-0-migration","exit_code":1}
```

脚本要判断失败的种类，读 `error_code`，不要去匹配人读的文字：人读的文字跟着 CLI 的语言变（`brickkit lang`），也会随版本改进措辞；错误码不会。
`--log-level off` 会连这一行也关掉——只在没有程序需要解析 `error_code` 的场合用。

这行 JSON 是给程序读的，所以人在终端里直接敲命令时看不到它：stderr 是终端、又没有显式选过级别（既没写 `--log-level`，
也没设 `BRICKKIT_LOG_LEVEL`）时，只打 `❌` 那一段——除了错误码，该说的它都说了。只要 stderr 被捕获——脚本、CI、`2> 文件`、
AI 助手的 shell——它就不是终端，这一行一定在。在伪终端里跑 `brickkit`（`docker run -t`、`expect`、`script`）又要读
`error_code` 的程序，显式选一个级别：`BRICKKIT_LOG_LEVEL=warn`。想在自己的终端里也看到这一行，同样这么设。

## 退出码

| 退出码 | 含义 |
| --- | --- |
| `0` | 成功（只有警告也是 0） |
| `1` | 失败：配置、依赖、引擎、网络……一切命令跑了但没做成的情况 |
| `2` | 用法不对：未知命令、未知参数、`--file` 指向的文件不存在 |

`lint --strict` 下有警告时退出码是 `1`。

## 哪些值得重试

**只有 `NETWORK_UNREACHABLE` 值得原样重试**：网络抖动、市场暂时不可达、镜像仓库连不上，过一会儿再试可能就好了。
其余的错误码，原样重试多少次结果都一样——配置不会自己变对，依赖不会自己出现。`ENGINE_FAILED` 介于两者之间：引擎偶发的失败可以重试一次，
反复失败就去看引擎自己的输出。

错误码只增不改：已有的码不会改名、不会删除，新的情形会归到已有的码下，或者新增一个码。

## 怎么读下面每一节

每一节是一个错误码。表格第一列是 CLI 打出的错误标题（`<…>` 处是具体的值），可以直接拿它在这一页里搜。
一个错误码背后往往有好几种具体情形，靠标题区分。

### INTERNAL

CLI 自己出了问题，或者读写本机文件失败（磁盘满、没有权限）。不是你的配置写错了。

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：写入部署文件失败` | 生成的部署文件写不进 `.brickkit/generated/` | 检查磁盘空间与目录权限 |
| `错误：创建生成目录失败` | 建不了 `.brickkit/generated/` | 同上 |
| `错误：写入本地调试环境变量文件失败` | `local-debug.*.env` 写不进去 | 同上 |
| `错误：AI 助手技能装入失败` | `init` 写 `.claude/skills/` 失败 | 同上；或加 `--no-skills` |
| `错误：写入 pre-commit hook 失败` | `.git/hooks/pre-commit` 写不进去 | 检查 `.git/hooks/` 的权限 |
| `启动本地进程失败` | `mode: local` 的进程起不来 | 看报错里的系统原因 |
| `内部错误：<…> 在这里不能被求值` | CLI 的内部不变量被打破 | 这是 bug，请带着完整输出报告 |

### INVALID_ARGUMENT

命令行写得不对。多数情况下退出码是 `2`。

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：未知命令 <…>` | 没有这条命令 | `brickkit --help` 看全部命令 |
| `错误：命令用法不正确` | 参数个数不对、不认识的参数 | `brickkit <命令> --help` |
| `错误：--file 指定的部署文件不存在` | `-f` 指向的文件不在 | 路径相对项目根；检查拼写 |
| `错误：组件 ID 不合法：<…>` | 组件 ID 不是 `<scope>/<name>` 的形式 | 小写字母、数字、中划线，中间一个 `/` |
| `错误：版本号不合法：<…>` | 不是精确版本 | 写成 `主.次.修订`，不接受 `^1.0.0`、`latest` |
| `错误：<…> 有好几个版本（<…>），要写明删哪一个` | `remove` 一个多版本组件时没写版本 | `brickkit remove <组件>@<版本>` |
| `错误：--init 只能与 --local 一起用` | `add --init` 单独用 | 写成 `add --local --init` |
| `错误：--focus 与 --all 互相矛盾` | 一条命令里同时写了 `up --focus` 和 `--all` | 二选一 |
| `错误：--focus 与 --all 改的是 deploy.local.yaml，而 -f 与 --no-local 都不读它` | `up --focus` 或 `--all` 和 `-f` / `--no-local` 一起用 | 去掉 `-f` / `--no-local`，或者自己改 `deploy.local.yaml` 里的 `focus:` 那一行 |
| `错误：<…> 是兼容版本（带 requiredBy），upgrade 只移动默认版本` | 对一个 `requiredBy` 版本执行 `upgrade` | 升级依赖它的组件，兼容版本会随之调整 |
| `错误：日志级别不合法` | `--log-level` 取值不对 | `debug`、`info`、`warn`、`error`、`off` |
| `错误：用户名不能为空` | `login` 没给用户名 | 交互输入或 `--username` |

### NOT_IMPLEMENTED

保留的错误码：目前没有任何命令会产生它。

### CONFIG_INVALID

三层文件、安装源、签名配置里有写错的地方。这是最常见的错误码，背后的情形很多，靠标题区分。

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：<…> 校验失败` | `brickkit.yaml` 或部署文件的结构不对：未知字段、类型不对、必填字段没写 | 报错点名了字段与行号，照着改 |
| `错误：<…> 校验失败`（focus 字段） | `focus:` 写在了 `deploy.yaml` 里（它是个人的事）、配了 `target: k8s`（集群够不着你机器上的进程），或者不是组件 ID；`up --focus` 不会写出这样的焦点 | `focus` 只写在 `deploy.local.yaml` 里、只用于 `docker` / `podman`；见[在项目里就地开发](../02-project-guide/04-focus-run.md) |
| `错误：<…> 不是合法的 YAML` | YAML 语法错误 | 按报错的行号检查缩进与冒号 |
| `错误：<…> 里有不止一个 YAML 文档（多写了一行 ---？）` | 文件里多了一行 `---`，后半段会被悄悄丢掉 | 删掉多余的 `---` |
| `错误：必填的组件配置没有值` | `configSchema.required` 里的项没有默认值，`config/` 里也没填 | 在 `config/<组件>.yaml` 里填上 |
| `错误：配置文件引用了哪里都没有定义的公共变量` | `$var:NAME` 找不到 | 在 `config/vars.yaml` 或部署文件的 `vars:` 里定义它 |
| `错误：config/ 或部署文件里引用的环境变量没有定义` | 生成部署文件时 `${VAR}` 在进程环境与 `.env` 里都取不到（K8s 要求值；Docker 下 compose 会把它换成空字符串） | 在 `.env` 或当前 shell 里设好，或写默认值 `${VAR:-x}` |
| `错误：<…> 指向的文件读不到` | `file://` 指向的文件不存在或读不了 | 路径相对项目根 |
| `错误：当前连着的不是配置里指定的集群` | `kubectl` 当前上下文与部署文件的 `k8s.context` 不同 | 切换上下文，或用写着那个集群的部署文件 |
| `错误：target: k8s 下 expose: true 的组件必须写 hostname` | K8s 上对外开放却没有域名 | 在部署条目上写 `hostname` |
| `错误：这次改动会让项目装载不了，三份文件已全部还原` | `add` / `remove` / `upgrade` 改完之后项目装载失败 | 看报错列出的原因；什么都没改 |
| `错误：<…> 还被这些组件强依赖：<…>` | `remove` 一个还被强依赖的版本 | 先移除或升级依赖它的组件 |
| `错误：源码目录删了就找不回来，没有删` | 要删的源码目录里有未提交或未推送的改动 | 先提交推送，或加 `--force` |
| `错误：这次外壳承载的成员版本，与外壳 component.yaml 里编进的版本对不上` | 外壳承载的成员版本不是它编进的版本 | 报错给出三条出路，见 [外壳升级](../04-shell/07-shell-upgrade.md) |
| `错误：<…> 写在了 <…> 的 members 里，但这个外壳的 component.yaml 没有编进它` | 部署文件把外壳没编进的组件放在它下面 | 把条目移到顶层，或换一个编进了它的外壳 |
| `错误：brickkit.yaml 把 <…> 标成了 kind: shell，它的 component.yaml 里却没有 shell 块` | `kind: shell` 与 Manifest 不一致 | 删掉 `kind: shell` |
| `错误：skipWaitFor 写的不是强依赖` | `skipWaitFor` 列了一个不是强依赖的组件 | 只能列这个组件版本真实的强依赖 |
| `错误：外壳成员 <…> 的配置项 <…> 不是合法的 UTF-8 文本` | 外壳成员的配置值装不进 JSON | 二进制内容先 base64 编码 |
| `错误：没有可用的安装源` | `brickkit.yaml` 没有启用的安装源 | 在 `sources` 里加一个 |
| `错误：本地安装源路径不存在` | 本地源的 `path` 指向不存在的目录 | 改 `path`，或建好目录 |
| `错误：项目名称不合法` | 项目名不符合规则 | 小写字母、数字、中划线，以字母或数字开头结尾 |
| `错误：找不到 cosign` | `publish --sign` 需要 cosign 但没装 | 安装 cosign（只有发布方需要） |
| `错误：可信公钥不可用` | `installer.publicKeys` 指向的公钥文件有问题 | 检查文件路径与内容 |
| `探测不出 <…> 该怎么启动` | `mode: local` 的组件认不出启动命令 | 在 `component.yaml` 的 `local` 里写 `language` 或 `runCommand` |
| `从本地仓库运行的代码与这次运行的版本对不上` | 本机进程运行的组件（`mode: local` 或焦点），本地仓库的版本不是 `brickkit.yaml` 里的版本——或者根本没有本地仓库 | `brickkit upgrade <组件>@<仓库版本>`，或把仓库切到对应 tag；没有仓库时 `brickkit add <组件> --repo` |
| `这个项目已经有一个本地会话在跑（PID <…>）——先停掉它，或者切到那个终端` | 另一个终端的 `up` 正看护着 `mode: local` 进程 | 在那个终端里 `Ctrl+C` |

同一个码下也有警告（不让命令失败）：

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `警告：.gitignore 缺少必需条目——个人部署文件和密钥可能被提交` | 已有的 `.gitignore` 缺条目，`init` 不替你改 | 把列出的每一行加进去 |
| `<…>：<…> 不在组件的 configSchema 里，不会生效` | 配置键写错了 | 按"是不是想写"的建议改 |
| `config/ 下的文件可能写了明文密钥` | 密钥写成了明文，而 `config/` 要进 Git | 改成 `${VAR}` 或 `file://` |
| `existingSecret 只在 K8s 生效，当前是 docker 目标` | Docker 下用了 `existingSecret` | 这一项不注入；Docker 上用 `${VAR}` 或 `file://` |
| `<…> 在 target: <…> 下不起作用，已忽略` | 写了只对另一种部署目标有用的字段 | 可以留着，换目标时生效 |

### CONFIG_CONFLICT

两件事说法冲突，平台不替你选。

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：检测到未解决的配置冲突` | 配置文件里有重复的键：升级留下的冲突块，或手写重复了 | 用纯文本编辑器保留一行、删掉另一行 |
| `提交被拦下：组件源码提交在归档目录里，但 <…> 说它该启动` | 提交前检查：`mode` 与目录结构不一致 | `brickkit restore`，或把目录移动一起提交 |
| `提交被拦下：同一个组件的源码在提交里出现了两处` | 活跃目录与归档目录里都有它 | 删掉其中一份 |
| `错误：pre-commit hook 已存在，不是 brickkit 写的` | 已有别的 pre-commit 钩子 | 自己把 `brickkit restore --check` 加进那个钩子 |
| `错误：<…>@<…> 已经发布过了` | 往市场重复发布同一个版本 | 提高版本号 |
| `错误：组件源码嵌在另一个组件的目录里` | 一个组件的源码放在了另一个组件自己的 `components/` 里（项目里的工作台）——同一个组件有了两份 | 自己挪走或删掉嵌套的那份；报错会说它在别处有没有副本。BrickKit 什么都不挪 |
| `错误：这个工作台在项目 <…> 里面；--repo 会在这里再克隆一份` | 在一个本身是外层项目组件的工作台里 `add --repo` | 到那个项目里 `add --repo`，或者在那里用焦点运行开发这个组件 |

警告：`配置冲突：组件 <…> 的配置项已被忽略`——配置项撞上了平台保留的变量名，平台的值优先。见 [环境变量注入契约](03-env-injection-contract.md#保留的名字)。

### DEPLOY_INCONSISTENT

部署文件与 `brickkit.yaml` 的组件对不上：少了组件版本的条目，或者多出了 `brickkit.yaml` 里没有的条目。

- 部署文件对不上时，错误标题点名那份文件（如"deploy.yaml 与 brickkit.yaml 的组件对不上"）。团队改了 `brickkit.yaml` 而部署文件没跟上：补上或删掉列出的条目。
- 本地模式下，标题是"deploy.local.yaml 已过期，与 brickkit.yaml 的组件对不上"：`brickkit local refresh`、手动补条目、或 `brickkit local off`。

### PROJECT_EXISTS

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：目录 <…> 已存在且不为空` | `init <名字>` 的目标目录已有东西 | 换个名字，或进去执行不带名字的 `init`（补全式） |
| `错误：<…> 已存在，而且不是目录` | 同名的是一个文件 | 换个名字 |

### PROJECT_MISSING

当前目录不是一个 BrickKit 项目，或少了必需的文件。

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：项目配置文件不存在` | 没有 `brickkit.yaml` | 进入项目目录，或 `brickkit init` |
| `错误：找不到 deploy.yaml` | 有 `brickkit.yaml`，没有部署文件 | 在项目里执行 `brickkit init` 补全 |
| `错误：本地模式已开启，但 deploy.local.yaml 不存在` | 开着本地模式，文件却被删了 | `brickkit local off`，或 `brickkit local on` 重新生成 |
| `错误：没有可刷新的 <…>` | 还没有 `deploy.local.yaml` 就 `local refresh` | 先 `brickkit local on` |
| `错误：当前目录既不是 BrickKit 项目，也不是组件仓库` | `skills` 在别的目录里执行 | 进入项目或组件仓库 |

### MANIFEST_INVALID

组件的 `component.yaml` 有问题——多半是组件作者的事。

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：<…> 校验失败` | 未知字段、必填字段缺失、版本写成范围 | 报错点名了字段；使用方可以找组件作者 |
| `错误：<…> 不存在` | 组件目录里没有 `component.yaml` | 检查路径 |
| `错误：<…> 不是合法的 YAML` | YAML 语法错误 | 按行号修 |
| `错误：<…> 的 component.yaml 用不了` | 从安装源取到的 Manifest 解析不了 | 联系组件作者 |
| `错误：<…> 里的组件 ID 与目录名对不上` | 本地源里 `<scope>/<name>/` 与 `metadata.id` 不一致 | 改目录名或 ID |
| `错误：artifacts 声明的文件不存在` | 发布时 `artifacts.files` 指向的文件不在 | 补上文件或改声明 |

警告：`警告：configSchema 里有配置项声明的键不会生效`——`configSchema` 某一项里写了拼错的键（如 `defualt`）。

### DEPENDENCY_MISSING

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：强依赖缺失` | 一个强依赖在项目里找不到 | `brickkit add` 它；或在安装源里发布它 |
| `<…> 没有声明在 brickkit.yaml 里` | 依赖的那个版本不在 `brickkit.yaml` | `brickkit add <依赖>@<版本>` |

警告：`警告：弱依赖缺失：<…>`——弱依赖不在，照常启动，它的地址变量不注入。

### DEPENDENCY_CYCLE

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：检测到循环依赖` | 几个组件互相强依赖成环，排不出启动顺序 | 把环上的一条依赖改成弱依赖（`optional: true`），或重新划分组件 |
| `错误：把成员放进外壳 <…> 之后，外壳要等一个组件，而那个组件又在等外壳` | 组件本身没有环，合并进外壳后出现了等待环 | 报错给出三条出路，见 [成员管理](../04-shell/04-members-management.md#合并造成的启动环与-skipwaitfor) |

### VERSION_AMBIGUOUS

保留的错误码：目前没有任何命令会产生它。多个版本时要写明版本的情形，现在报 `INVALID_ARGUMENT`。

### COMPONENT_DISABLED

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：强依赖 <…> 被禁用` | 一个被钉住要跑的组件（`mode: enabled` / `debug` / `local`），它的强依赖却写了 `mode: disable` | 两个意图矛盾：去掉其中一个 |
| `错误：焦点 <…> 的条目写着 mode: disable` | 焦点组件的条目写了 `mode: disable` | 去掉 `mode: disable`，或换一个焦点 |

### COMPONENT_NOT_FOUND

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：组件未找到` | 所有安装源里都没有这个组件 | 检查组件 ID、安装源配置；Git 源上要有版本 tag |
| `错误：安装源里有这个组件，但版本不是要的那个` | 本地源里是另一个版本 | 写对版本，或换安装源 |
| `仓库里没有 <…> 这个版本` | Git 仓库里没有这个版本的 tag | 让组件作者 `brickkit release` 这个版本 |
| `错误：项目里没有 <…>` | `remove` 一个不在项目里的组件 | 检查组件 ID |
| `错误：焦点 <…> 不是这个项目的组件` | `focus:` 或 `--focus` 写的组件不在 `brickkit.yaml` 里（后来删了，或者拼错了） | 照"是不是想写"改；`brickkit up --all` 去掉焦点 |

### COMPONENT_BLOCKED

组件市场把这个组件版本标成了 `blocked`（确认有问题），不能再安装。换一个版本，或联系发布者。

### RESOURCE_UNBOUND

保留的错误码：目前没有任何命令会产生它。BrickKit 已经没有"基础资源"这一概念——数据库地址等都是组件的普通配置项。

### PORT_CONFLICT

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：外壳 <…> 上有两个组件都要用端口 <…>` | 外壳与它的成员、或两个成员用了同一个端口 | 端口必须互不相同 |
| `错误：宿主机端口 <…> 被多个组件占用` | 几个本机进程或对外端口撞在同一个宿主机端口上 | 改 `localPort` 或 `exposePort` |

### MIGRATION_FAILED

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：数据库迁移失败` | 组件的迁移以非零退出（Docker 下是迁移容器，K8s 下是 Job）；主服务没有启动 | 按报错给出的命令看迁移日志，修好之后重新 `up` |

### MIGRATION_SKIPPED

只作警告：`提示：mode: <…> 组件的数据库迁移不会自动执行`——本机进程运行的组件没有迁移容器，迁移要你手动跑一次。

### ENGINE_FAILED

底层引擎（`docker compose`、`kubectl`）执行失败。报错里带着引擎自己的输出，那通常已经说清了原因。

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：<…> 执行失败` | `docker compose` / `podman compose` 返回失败 | 看报错里的"输出" |
| `错误：kubectl 执行失败` | `kubectl` 返回失败 | 同上 |
| `错误：部分组件没有正常启动` | 引擎跑完了，但有组件没健康 | 按提示看那个组件的日志 |
| `<…> 个本地组件崩溃了` | `mode: local` 的进程意外退出 | 看崩溃摘要里的最后几行输出 |

### ENGINE_MISSING

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：没有找到可用的容器引擎` | 没装 Docker，也没装 Podman | 安装 Docker（含 Compose V2） |
| `错误：找不到容器引擎 <…>` | 部署文件的 `target` 对应的引擎没装 | 安装它，或换 `target` |
| `错误：找不到 kubectl` | `target: k8s` 但没有 `kubectl` | 安装 `kubectl` |
| `错误：检测到 Podman，但尚未启用` | 只装了 Podman，部署文件却是 `target: docker` | 把 `target` 改成 `podman` |

### NETWORK_UNREACHABLE

网络问题。**这是唯一值得原样重试的错误码。**

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `拉取组件失败：<…>` | 从 Git 仓库取组件失败，而且根本没连上远端：离线、主机名解析不了、连接被拒绝 | 检查网络与主机名，网络恢复后重试；离线时写明一个本机缓存里已有的版本 |
| `错误：市场不可达` | 连不上组件市场 | 检查网络与市场地址，稍后重试 |
| `错误：无法连接镜像仓库` | 检查镜像时连不上镜像仓库 | 检查网络；稍后重试 |
| `错误：<…> 的产物一个都没下载成功` | `fetch` 的产物全部下载失败 | 稍后重试 |
| `错误：没能向远端 <…> 查询已有的 tag` | `release` 查远端 tag 失败 | 检查网络与远端地址 |

警告：`警告：产物下载失败，已跳过`——个别产物没下载成功，命令照常完成。

### AUTH_REQUIRED

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：发布失败：未登录` | `publish` 前没有登录 | `brickkit login` |
| `错误：brickkit.yaml 中没有可用的市场安装源` | `login` 找不到市场地址 | 在 `sources` 里加一个 `type: market`，或 `--market` |
| `错误：配置了多个市场安装源，无法确定登录哪一个` | 多个市场 | `--market` 指定 |

### AUTH_FAILED

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：登录失败：用户名或密码错误` | 市场拒绝了凭据 | 检查用户名与密码 |
| `错误：登录凭据格式不合法` | `.brickkit/credentials` 损坏 | `brickkit logout` 再 `login` |
| `拉取组件失败：<…>` | 连上了 Git 远端，却被拒绝：凭据不被接受，或者仓库不存在（托管平台对两者常给同一句话） | 看报错里 git 的原话，按 [Git 鉴权问题](../10-troubleshooting/04-git-auth-issues.md) 的对照表分清是哪一类；原样重试不会有变化 |

### TOKEN_EXPIRED

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：Token 已过期` | 市场令牌过期（CLI 不自动刷新） | `brickkit login` |

### IMAGE_UNAUTHORIZED

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：镜像拉取未授权` | 镜像仓库拒绝了拉取 | `docker login` 那个仓库；或改成本机构建 |
| `错误：镜像不存在` | 仓库里没有这个镜像 | 检查镜像地址；或 `brickkit build` 本机构建 |

### IMAGE_MISSING

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：这些镜像要在本机构建，还没有构建` | 需要本机构建的镜像不在（`up` 从不构建） | `brickkit build` |

### IMAGE_STALE

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：外壳 <…> 的本机镜像编进的成员版本，与它的 component.yaml 不一致` | 改了外壳的 `shell.members` 却没重建镜像 | `brickkit build <外壳> --force` |

### IMAGE_UNVERIFIED

只作警告：外壳的本机镜像没有 `brickkit build` 记下的成员版本标签（手工构建、第三方镜像），无法核对编进了哪些成员版本。

### SIGNATURE_INVALID

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：组件未签名，安装被阻断` | `requireSignature: true`，市场组件没有签名 | 找发布者要签名版本；确认不需要签名再关掉 `requireSignature` |

警告：`警告：requireSignature 为 true，但项目没有声明任何可信公钥，签名校验实际未生效`、`警告：签名来自未声明的发布者，未做校验`。
见 [安全与签名](08-security-and-signing.md)。

### CLONE_FAILED

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：clone 失败` | `add --repo` 克隆源码失败 | 看报错里 git 的原话 |
| `clone 失败：目录已存在` | `components/` 下已有同名目录 | 移走它再试 |
| `clone 失败：源码已经在了，只是被归档着` | 源码在 `components/.archived/` 里 | `brickkit sync` 把它激活 |

### SUBMODULE_GUARD

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：无法移动组件源码——它是一个已登记的 git submodule` | `sync` 要归档的目录是项目仓库登记的 submodule | 先在项目仓库里取消这个 submodule |
| `错误：无法删除组件源码——它是一个已登记的 git submodule` | `remove` 要删的目录是 submodule | 同上 |

### SUBMODULES_SKIPPED

只作警告，来自 `build`：`警告：<…> 的源码里有 git submodule，这里它们是空目录`——BrickKit 从不拉取 git submodule
（见 [bare 仓库机制](06-bare-repo-mechanism.md#从不拉取-git-submodule)），所以在拿来构建的源码里它们是空的。让组件发布镜像
（`deployment.image`），或者让构建不依赖它们；实在需要，在 `components/` 下克隆的仓库里 `git submodule update --init`。

### RELEASE_BLOCKED

`brickkit release` 在打 tag 之前的检查没通过。什么都没有写。

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：<…> 有未提交的改动` | 组件目录里有未提交的改动 | 提交或丢弃 |
| `错误：发布 <…> 的这个分支没有上游` | 分支从没推送过 | 按提示 `git push -u origin <分支>` |
| `错误：当前分支有未推送到远端的提交（发布 <…>）` | 本地有提交还没推送 | `git push` |
| `错误：<…> 已经发布过（tag <…> 就在当前提交上）` | 这个版本已经发布 | 提高 `metadata.version` |
| `错误：tag <…> 已经存在，指向别的提交` | 同名 tag 指向另一个提交 | 提高版本号；已发布的版本不再改 |
| `错误：HEAD 处于游离状态——<…> 只能从分支上发布` | 不在任何分支上 | 切到分支 |
| `错误：组件目录不在 Git 仓库里` | 组件目录没有 Git | `git init` 并设好远端 |

### RELEASE_PUSH_FAILED

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：把 <…> 的 tag <…> 推送到 <…> 失败` | tag 打好了，推送失败（网络、权限） | 本地的 tag 已经删掉，什么都没留下；修好网络或权限后重新 `release` |

### LINT_FAILED

| 标题 | 什么情况 | 怎么办 |
| --- | --- | --- |
| `错误：结构检查未通过` | `lint` 发现了错误（`--strict` 下也包括警告） | 按上面逐条列出的位置修改 |

## 文档检查

`brickkit lint` 会机械地检查组件的文档和项目的 `AGENTS.md`（见[组件的文档](../03-component-guide/08-component-doc-spec.md)）。
下面每一条都只是警告：从不拦下 `up` 和 `release`，只有 `lint --strict` 才把它算成失败（`LINT_FAILED`）——给想让 CI 守住的团队用。
每条警告都点名文件，有行号的带上行号。加上 `--log-level info` 时，每条查出的问题还会输出一行 JSON 日志，带着它的 `error_code` 与 `file`，
给要区分它们的脚本用。

### DOC_FILE_MISSING

缺了必需的文档：组件里是 `BRICKKIT.md`、`AGENTS.md`、`CLAUDE.md` 或 `README.md`；项目里是 `AGENTS.md` 或 `CLAUDE.md`。
新组件用 `brickkit new` 会全部写好；已有的组件把缺的那份补上（`brickkit skills update` 会建出缺的 `AGENTS.md` 和 `CLAUDE.md`）。

### DOC_SECTION_MISSING

文档缺了一个固定小节——比如 `BRICKKIT.md` 的"部署前准备"、组件 `AGENTS.md` 的"代码地图"。中英文标题都认，前面带不带编号都行。
在对应标题下补上这一节。

### DOC_PATH_MISSING

组件 `AGENTS.md` 代码地图里的某条路径不存在（代码挪了，地图没跟上）。把地图里的路径改对；目录以 `/` 结尾。反引号里以 `/` 开头的会当成 HTTP 路由而不是路径，从不检查。

### DOC_LINK_BROKEN

`README.md`、`AGENTS.md` 或 `docs/` 里的相对链接指向的文件不存在。改链接，或者补上文件。

### DOC_LINK_NOT_PORTABLE

两种情况：`BRICKKIT.md` 里有相对链接——它在别的项目缓存里是单独读的，链接在那里是死的：用行内代码写文件名，或者用绝对地址；
或者组件文档的相对链接跑出了组件目录，指向使用这个组件的项目里根本没有的文件。

### DOC_OUT_OF_STEP

`component.yaml` 写着的事实，文档该提的地方没提："依赖说明"里少了一条依赖、"配置指南"里少了一个必填配置项、"契约索引"里少了
`artifacts` 的一个文件、"外壳声明"里少了一个成员。把它写上（依赖写 ID；版本留在 `component.yaml`）。

### DOC_PLACEHOLDER

文档正文里还留着 `TODO`、`TBD`、`FIXME`、`待补`、`后补` 或 `待填`（代码块和行内代码里的不算）。`brickkit new` 生成的骨架故意留下
`<!-- TODO: … -->` 注释，就是为了让这条列出还有哪些没填。

### DOC_TRANSLATION_DRIFT

译本（`README.zh.md`、`BRICKKIT.zh.md`、`docs/design.zh.md`，或 `docs/zh/` 下的一页）没有原文，或者二级小节数与原文不同
（`AGENTS.md` 末尾 brickkit 维护的那一段不算），或者某个语言版本没在开头链接其余每一份（`BRICKKIT*.md` 除外：它根本不放相对链接）；
用 `docs/<语言>/` 分树时，原文树里的某一页在另一棵树里缺了；或者一个文件看着像译本，后缀却不是小写的语言代码（`README.zh-CN.md`）。
把译本改回与原文一致——以原文为准。

### AGENTS_BLOCK_MISSING

`AGENTS.md` 里没有可用的、由 brickkit 维护的一段（没有，或者标记坏了），组件表和平台规则因此不会自动更新。
`brickkit skills update` 会追加一段；`init`、`add`、`remove`、`upgrade` 从不改你的文件。

### CLAUDE_IMPORT_MISSING

`CLAUDE.md` 在，但里面没有 `@AGENTS.md` 这一行，Claude Code 不会读 `AGENTS.md`。加上这一行，或者运行 `brickkit skills update`。

### PROJECT_MAP_OBSOLETE

项目根还留着旧版的项目地图 `BRICKKIT.md`（带 brickkit 标记的那一份）。组件表现在在 `AGENTS.md` 末尾：把你自己写的内容挪进
`AGENTS.md`，再删掉 `BRICKKIT.md`——brickkit 不会替你删。
