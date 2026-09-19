# 错误码

每一条让 `brickkit` 命令终止的错误，都带一个**稳定的错误码**。这一篇是字典：每个码是什么意思、哪些情形会产生它、接下来该怎么做。

CLI 的报错文案本身就是中文，下面原样引用，所以你可以直接拿终端里打印的那句话来搜。

## 读懂一次失败

命令失败时会打印一段给人看的错误块，紧接着是一行 JSON 日志——两者都在 stderr 上：

```
❌ 错误：项目配置文件不存在
   路径：brickkit.yaml
   建议：
   1. 在项目目录中执行 brickkit init <项目名称> 初始化项目
   2. 或用 --config 指定正确的配置文件路径
{"time":"2026-09-19T00:56:05+02:00","level":"ERROR","message":"命令执行失败","command":"brickkit up","elapsed_ms":0,"error_code":"PROJECT_MISSING","error":"PROJECT_MISSING: 错误：项目配置文件不存在; 路径=brickkit.yaml","exit_code":1}
```

那行日志里的 `error_code`，就是这一篇的索引。错误块的第一行——标题——说明你落在这个码底下的哪一种具体情形。这行日志在默认日志级别（`info`）下就会打印；`--log-level off`，或 `BRICKKIT_LOG_LEVEL=off`，可以关掉它。

## 错误码承诺什么

- **稳定。** 码只会增加——不改名、不挪作他用、不删除。脚本可以放心按它分支。
- **一个码是一个类别，不是一种情形。** `CONFIG_INVALID` 背后有七十多种不同的报错。下面的表格列出你最可能遇到的那些，每一条都写着 CLI 打印的标题。
- **退出码。** `0`——成功，包括打印了警告的那次运行。`1`——命令失败。`2`——命令行本身写错了：缺参数或参数格式不对、未知的命令或参数、或者参数指名了一个不存在的东西（对不在 `brickkit.yaml` 里的组件执行 `brickkit remove`）。
- **写脚本时。** 只有 `NETWORK_UNREACHABLE` 值得原样重试：网络或市场可能恢复。`CONFIG_INVALID` 重试多少次结果都一样。其他任何码，都当作"得先改点什么"。
- **警告单独算。** ⚠️ 块永远不会让命令失败。CLI 在继续往下跑的过程中打印的那些警告根本不产生那行日志，所以要靠标题来认——见[警告](#警告)。

## 索引：按类别找错误码

点错误码跳到对应的一节；每一节里的表格按"你会看到的标题"列出具体情形。

**用法与内部错误**

| 错误码 | 一句话 |
| --- | --- |
| [`INVALID_ARGUMENT`](#invalid_argument) | 命令行写错了：缺参数、格式不对、未知的命令或参数 |
| [`NOT_IMPLEMENTED`](#not_implemented) | 保留码，如今没有任何命令会产生它 |
| [`INTERNAL`](#internal) | 原因不在你的输入（多半是往工作目录写文件出了问题），或 CLI 撞上一个没归类的错误 |

**配置**

| 错误码 | 一句话 |
| --- | --- |
| [`CONFIG_INVALID`](#config_invalid) | 范围最宽：`brickkit.yaml` 不合法，或项目当前的状态不满足某个前置条件 |
| [`CONFIG_CONFLICT`](#config_conflict) | 你要做的事，和已经存在的东西撞了 |
| [`PROJECT_EXISTS`](#project_exists) | 在已经是项目的目录里又执行了 `brickkit init` |
| [`PROJECT_MISSING`](#project_missing) | 命令需要一个 BrickKit 项目，而这里没有 |

**Manifest 与依赖**

| 错误码 | 一句话 |
| --- | --- |
| [`MANIFEST_INVALID`](#manifest_invalid) | 某份 `component.yaml` 用不了（不认识的键会被当场拒绝） |
| [`DEPENDENCY_MISSING`](#dependency_missing) | 有个强依赖在任何安装源里都找不到 |
| [`DEPENDENCY_CYCLE`](#dependency_cycle) | 强依赖连成了一个环 |
| [`VERSION_AMBIGUOUS`](#version_ambiguous) | 同时有多个版本，而命令没指明是哪一个 |
| [`COMPONENT_DISABLED`](#component_disabled) | 一个要运行的组件强依赖的那个组件被关掉了 |
| [`COMPONENT_NOT_FOUND`](#component_not_found) | 没有任何已启用的安装源有这个组件（或这个版本） |
| [`COMPONENT_BLOCKED`](#component_blocked) | 市场已经把那个版本下架（`blocked`） |

**资源与端口**

| 错误码 | 一句话 |
| --- | --- |
| [`RESOURCE_UNBOUND`](#resource_unbound) | 组件需要的资源，`brickkit.yaml` 里没有绑定 |
| [`PORT_CONFLICT`](#port_conflict) | 两个组件要用同一个宿主机端口（或同一个外壳里两个成员要用同一个端口） |

**迁移与引擎**

| 错误码 | 一句话 |
| --- | --- |
| [`MIGRATION_FAILED`](#migration_failed) | K8s 上的迁移 Job 失败，主服务被刻意拦住不启动 |
| [`MIGRATION_SKIPPED`](#migration_skipped) | 只会作为警告出现：`local: true` 或 `servedBy` 的组件不跑迁移 |
| [`ENGINE_FAILED`](#engine_failed) | Docker Compose 或 `kubectl` 跑了，但失败了 |
| [`ENGINE_MISSING`](#engine_missing) | 找不到容器引擎的可执行文件 |

**网络、认证与镜像**

| 错误码 | 一句话 |
| --- | --- |
| [`NETWORK_UNREACHABLE`](#network_unreachable) | 网络或市场连不上——唯一值得原样重试的码 |
| [`AUTH_REQUIRED`](#auth_required) | 这一步需要先登录 |
| [`AUTH_FAILED`](#auth_failed) | 登录失败：凭据不对，或没拿到令牌 |
| [`TOKEN_EXPIRED`](#token_expired) | 存着的令牌过期了 |
| [`IMAGE_UNAUTHORIZED`](#image_unauthorized) | 镜像仓库拒绝了这次拉取，或者镜像根本不存在 |

**签名与源码工作区**

| 错误码 | 一句话 |
| --- | --- |
| [`SIGNATURE_INVALID`](#signature_invalid) | 签名验证挡下了这次安装：没签名，或签名对不上 |
| [`CLONE_FAILED`](#clone_failed) | 克隆 Git 仓库失败 |
| [`SUBMODULE_GUARD`](#submodule_guard) | 组件源码是已登记的 git submodule，`remove` 和 `sync` 不会去碰它 |

警告不产生错误码，只能靠标题来认——见[警告](#警告)。

---

## 用法与内部错误

### INVALID_ARGUMENT

命令行写错了。这一类都靠改你敲的命令来解决；`brickkit <命令> --help` 会给出用法。

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `请指定要添加的组件` | `brickkit add` 没有给组件 | 给一个——`brickkit add people/basic@1.0.0`——或用 `--local` 把本地安装源里的全部加进来 |
| `请指定要移除的组件` | `brickkit remove` 没有给组件 | `brickkit remove people/basic` |
| `请指定项目名称：brickkit init <项目名称>` | `brickkit init` 没有给项目名 | `brickkit init my-shop` |
| `错误：组件 ID 不合法：<id>` | ID 不是 `<scope>/<name>` 的形式 | 用 `people/basic` 这样的 ID |
| `错误：版本号不合法：<version>` | 不是精确的 `major.minor.patch`——`^1.0.0` 这样的范围是被刻意拒绝的 | 写精确版本 |
| `错误：未知命令 <command>` | 命令名打错了 | `brickkit --help` 列出所有命令 |
| `错误：参数不合法` | 未知的或格式不对的参数 | `brickkit <命令> --help` |
| `错误：日志级别不合法` | `--log-level`（或 `BRICKKIT_LOG_LEVEL`）不是可接受的级别 | 用 `debug`、`info`、`warn`、`error`、`off` 之一 |

### NOT_IMPLEMENTED

保留码。它来自骨架阶段，那时一个命令可以只是占位；如今没有任何命令会产生它。它留在清单里，是因为错误码不会被删除。哪天真看到它，那是值得上报的 bug。

### INTERNAL

失败的原因不在你的输入——通常是往工作目录里写文件出了问题——或者 CLI 撞上了一个它没有归类的错误。凡是没有另外归类的错误，都以这个码报出，标题就是它自己的文字。

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：创建生成目录失败` | 放生成文件的目录建不出来 | 检查项目目录的写权限 |
| `错误：写入部署文件失败` | 生成的 Compose/Kubernetes 文件写不进去 | 同上——权限或磁盘空间 |
| `错误：写入本地调试环境变量文件失败` | `local-debug.env` 写不进去 | 同上 |
| `错误：写入 pre-commit hook 失败` | `.git/hooks/` 下的 hook 文件写不进去 | 检查 `.git/hooks/` 的权限 |
| `错误：AI 助手技能装入失败` | 项目已经建好，只是 AI 助手技能没装上 | 修好权限后执行 `brickkit skills update`；技能不影响任何其他功能 |
| `错误：缺少项目标签选择器，已中止删除` | 安全拦截：K8s 上的 `brickkit down` 发现没有项目标签来限定范围，拒绝删除 | 请上报——说明项目名没有传到选择器里 |

## 配置

### CONFIG_INVALID

范围最宽的码。要么 `brickkit.yaml`（或它指向的东西）不合法，要么项目当前的状态不满足某个前置条件。校验失败会一次把所有问题列在错误块里，每个字段一行，所以改一轮就能改完。

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：brickkit.yaml 校验失败` | 一个或多个字段不合法；标题下面逐条列出，并写明理由，例如 `components[1].exposePort：与 components[0].exposePort 冲突` | 逐个改掉列出的字段——[brickkit.yaml 字段完整参考](08-brickkit-yaml-reference.md)有每一条约束 |
| `错误：项目配置文件不是合法的 YAML` | YAML 语法错误 | 按报出的行号检查缩进 |
| `错误：项目配置文件内容为空` | 文件在，但是空的 | 写一份 `brickkit.yaml`，或在新目录里执行 `brickkit init` |
| `错误：项目名称不合法` | 名称必须是小写字母、数字、中划线，且以字母或数字开头结尾——它会变成 K8s 的 namespace 和 Docker 的网络名 | 换一个符合这个形状的名字 |
| `错误：必填的组件配置没有值` | 组件 `configSchema.required` 里的某个键没有 `default`，项目里也没给值——`up` 会被拦下，因为否则这个变量会悄悄地不存在 | 在 `brickkit.yaml` 里那个组件的 `config` 下补上 |
| `错误：brickkit.yaml 里引用的环境变量没有定义` | 某个 `${VAR}` 求不出值（K8s 清单没法把替换推迟到运行时，所以在生成时求值） | 在项目根的 `.env` 里定义、`export` 出来，或写默认值：`${VAR:-dev}` |
| `错误：local: true 只能在 deploy.target: docker 下使用` | 集群里的 Pod 连不到你笔记本上的进程 | 去掉 `local: true`，或把 target 改回 `docker` |
| `错误：deploy.target: k8s 下 expose: true 的组件必须写 hostname` | 没有 host 的 Ingress 会匹配所有域名 | 补上 `hostname:` |
| `错误：域名 <hostname> 被多个组件占用` | 两个组件写了同一个 `hostname` | 各写各的 |
| `错误：当前连着的不是配置里指定的集群` | 当前的 `kubectl` context 与 `deploy.context` 不一致 | `kubectl config use-context <名字>`，或 `brickkit up --context <名字>` |
| `错误：servedBy 指向的组件不存在` | `servedBy` 的值指向的组件不在项目里 | 检查外壳的 ID 和版本号有没有写错 |
| `错误：servedBy 指向的外壳当前没有在运行` | 外壳被关掉了（`enabled: false`） | 把它打开，或去掉 `servedBy` 让组件独立部署 |
| `错误：外壳 <shell> 下两个成员对同一个环境变量给出了不同的值` | 同一个外壳下的两个成员依赖了同一个组件的不同版本 | 让它们依赖同一个精确版本，或者不要放进同一个外壳 |
| `错误：没有可用的安装源` | `sources` 是空的，或者每个源都被关掉了 | 至少配一个安装源；本地开发可以配一个指向 `./components` 的 `type: local` |
| `错误：本地安装源路径不存在` | `local` 安装源的 `path` 写错了（它相对 `brickkit.yaml`） | 改对路径，或把这个源设为 `enabled: false` |
| `错误：安装源类型不合法：<type>` | `type` 不是 `market`、`git`、`local` 之一 | 用这三个之一 |
| `错误：可信公钥不可用` | `installer.publicKeys` 里有一条用不了：路径不对，或者不是 PEM 格式的公钥 | 指向 `cosign generate-key-pair` 产出的 `.pub`——绝不能指向私钥 `cosign.key` |
| `错误：找不到 cosign` | `brickkit publish --sign` 要用 cosign 签名（安装方从不需要） | 装上 cosign，并确保它在 `PATH` 里 |
| `错误：这里不是一个 git 仓库` | 在 Git 仓库之外执行了 `brickkit init --hooks` | 先 `git init` |
| `错误：这个仓库还没有任何提交` | `brickkit restore` 还原到最后一次提交，而一次都没有 | 先提交一次 |
| `错误：<path> 没有被 git 跟踪` | `brickkit restore` 指向了 Git 没有跟踪的东西 | 先 `git add` 并提交一次，它才有可还原的基准 |
| `错误：源码删掉就找不回来了` | `brickkit remove` 拒绝删掉带有"别处都没有的改动"的组件源码 | 先提交并推到远端，或把目录拷走；确认不要了就加 `--force`。只是暂时不用的话，写 `enabled: false` 再 `brickkit sync`，那会把源码归档而不是删掉 |

### CONFIG_CONFLICT

两件事不能同时成立：你要做的事，和已经存在的东西撞了。

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：<component>@<version> 已经发布过了` | 发布了一个市场上已有的版本——版本号一旦发布就不能重用 | 改 `component.yaml` 里的 `metadata.version` 再发 |
| `错误：<component>@<version> 上次建好了但没发完，而这次的 Manifest 与那份不一样` | 这个版本上次发布建好了、却没有走完，而这次的 Manifest 与那一份不同 | 换一个版本号——或者想把上次那份发完，就把 `component.yaml` 改回去 |
| `错误：pre-commit hook 已存在，不是 brickkit 写的` | `.git/hooks/pre-commit` 已经存在，而 BrickKit 绝不覆盖不是自己写的 hook（可能是 husky、lefthook，也可能是你自己的） | 把它打印出来的几行加进你自己的 hook，或删掉那个文件后重新执行 `brickkit init --hooks` |
| `错误：正在解决冲突，先把冲突处理完` | 在合并进行中执行了 `brickkit restore` | 先把合并处理完 |
| `错误：有组件的源码在两处都存在` | 同一个组件的源码既在活动目录、又在归档目录 | 留一份；错误块里写明了两个路径 |
| `提交被拦下：同一个组件的源码在提交里出现了两处` | pre-commit hook：这次提交会把同一个组件的源码放在两个位置 | 把其中一处从暂存区拿掉 |
| `提交被拦下：组件源码提交在归档目录里，但 <path> 说它该启动` | pre-commit hook：源码被归档了，而 `brickkit.yaml` 里的 `enabled` 没有跟着改 | 把对应的 `brickkit.yaml` 改动一起提交——或执行 `brickkit restore` |

市场也可能对一次发布回复版本冲突；它同样归在这个码下。

### PROJECT_EXISTS

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：项目已初始化，无需重复执行 init` | 在已经是项目的目录里又执行了 `brickkit init` | 无需处理——或换个目录 `init` |

### PROJECT_MISSING

命令需要一个 BrickKit 项目，而这里没有。

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：项目配置文件不存在` | 指定路径（默认是当前目录）下没有 `brickkit.yaml` | 在项目根执行、先执行 `brickkit init <项目名称>`，或用 `--config` 指向那个文件 |
| `错误：项目未初始化` | 同上，只是经由一个要改配置的命令走到这里 | 同上 |
| `错误：当前目录既不是 BrickKit 项目，也不是组件仓库` | `brickkit skills` 既没找到 `brickkit.yaml`，也没找到 `component.yaml` | 在项目根或组件仓库里执行 |
| `错误：这里没有 <file>，不是一个 BrickKit 项目` | 在没有 `brickkit.yaml` 的目录里执行了 `brickkit init --hooks` | 先 `brickkit init <项目名称>`——项目根就是仓库根时它会顺带装上 hook |

## Manifest 与依赖

### MANIFEST_INVALID

某份 `component.yaml` 用不了。Manifest 没有扩展字段机制：不认识的键会被当场拒绝，而不是被忽略。

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：component.yaml 校验失败` | 必填字段缺失或格式不对，或有不认识的键；标题下面逐条列出问题 | 逐个改掉列出的字段——[component.yaml 字段完整参考](07-component-yaml-reference.md)有每一条约束 |
| `错误：component.yaml 不是合法的 YAML` | YAML 语法错误 | 按报出的行号检查缩进 |
| `错误：component.yaml 不存在` | 目录里没有它 | 检查 `--path`，或安装源指向的目录 |
| `错误：component.yaml 内容为空` | 文件在，但是空的 | 把 Manifest 写出来 |
| `错误：组件目录中没有 component.yaml` | `brickkit publish` 指向的目录里没有它 | 用 `--path` 指向组件源码目录——归档目录（`components/.archived/…`）也可以 |
| `错误：artifacts 声明的文件不存在` | 某个 `artifacts[].files` 路径不存在——常常是契约文件还没生成 | 先生成（protobuf、OpenAPI），或改对路径 |
| `错误：component.yaml 里没有 deployment 段，无法钉住 digest` | 发布会钉住镜像 digest，而没有 `deployment` 段可钉 | 补上 `deployment` |
| `错误：<component>@<version> 的 component.yaml 用不了` | 取回来的 Manifest 解析或校验失败 | 错误块会说原因；如果来自市场，告诉发布者 |
| `错误：市场返回的 Manifest 为空` | 市场对这个版本什么都没返回 | 确认市场服务正常 |
| `错误：市场返回的 Manifest 无法解析` | 市场返回的东西不是 Manifest | 确认市场服务版本是否兼容 |

### DEPENDENCY_MISSING

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：强依赖缺失` | 有个强依赖在任何安装源里都找不到；错误块写明是哪个组件、缺哪个依赖 | 检查 `brickkit.yaml` 的 `sources`，确认那个精确版本已发布，再 `brickkit add` |
| `无法移除 <component>` | 对一个被其他组件强依赖的组件执行了 `brickkit remove` | 先移除依赖方 |

弱（可选）依赖缺失是警告，不是这个错误——见[警告](#警告)。

### DEPENDENCY_CYCLE

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：检测到循环依赖` | 强依赖连成了一个环；错误块会打印完整的环 | 把环上的一条边改成弱依赖（`optional: true`）。弱依赖不约束启动顺序，所以环上有一条弱边就不再是死结 |

### VERSION_AMBIGUOUS

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `<component> 存在多个版本（<versions>），请指定版本：` | `brickkit.yaml` 里同时有多个版本时执行了 `brickkit remove people/basic` | `brickkit remove people/basic@1.0.0` |

### COMPONENT_DISABLED

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：强依赖 <component> 被禁用` | 一个要运行的组件强依赖它，而它是 `enabled: false`——或者你给一个组件钉了 `enabled: true`，它的强依赖却被关掉了：两个互相冲突的意图 | 去掉依赖那一方的 `enabled: false`，或去掉依赖方的 `enabled: true`，让它跟着上层走 |

### COMPONENT_NOT_FOUND

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：组件未找到` | 没有任何已启用的安装源有这个 ID；错误块列出试过的安装源 | 检查 `sources`、ID，以及这个版本是否已发布 |
| `错误：安装源里有这个组件，但版本不是要的那个` | 安装源里有这个组件，只是没有那个版本 | 检查版本号 |
| `错误：<component> 不在 brickkit.yaml 中` | `brickkit remove` 了一个项目里没有的组件（退出码 `2`） | 检查 ID；`brickkit status` 会列出现有的 |
| `错误：<component>@<version> 不在 brickkit.yaml 中` | 同上，针对指定的版本 | 检查版本号——多个版本可以共存 |

市场回复"没有这个组件"时，同样归在这个码下。

### COMPONENT_BLOCKED

市场已经把那个版本下架（`blocked`）——这是信任模型所依赖的最后一道防线。它不能再被安装，去登录也改变不了这一点。换一个版本，或者向市场管理员了解原因。这条消息的文字来自市场，所以没有固定的标题可搜。

## 资源与端口

### RESOURCE_UNBOUND

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：资源依赖未满足` | 某个组件声明了资源依赖（`dependencies.resources`：一个 `kind` 加一个 `engine`），而 `brickkit.yaml` 没有满足它——没有对应的 `resources` 条目，或者没有给这个组件的 `bindings`。所有未满足的资源会一次列出。真正的 `up` 会被拦下；`up --dry-run` 只警告，所以你仍能看到会生成什么 | 补上这个资源，并给该组件加一条绑定——见[资源绑定的实际机制](05-resource-binding.md)——或者暂时不想跑它，就写 `enabled: false` |

### PORT_CONFLICT

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：宿主机端口 <port> 被多个组件占用` | 两个 `expose: true` 的组件落在同一个宿主机端口上，因为它们都没写 `exposePort`，于是都默认用自己的 `deployment.port`。（如果两个都**显式写了同一个** `exposePort`，则是 `CONFIG_INVALID`：一条点名两个字段的 `错误：brickkit.yaml 校验失败`。）两种情形都是在生成阶段查出来的，所以你不会看到 Docker 自己的 "port is already allocated" | 给其中一个写一个不同的、明确的 `exposePort`，或去掉 `expose: true`——组件之间在容器网络里互访本来就不需要暴露 |
| `错误：外壳 <shell> 上有两个组件都要用端口 <port>` | 同一个 `servedBy` 外壳的成员最终都在同一个容器或 Pod 里 | 让它们用不同的端口 |

## 迁移与引擎

### MIGRATION_FAILED

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：数据库迁移失败` | K8s 上的迁移 Job 失败；主服务被刻意拦住不启动，Job 也不重试（`backoffLimit: 0`） | `kubectl logs job/<迁移-job>`；修好脚本后重新 `brickkit up`——CLI 会先把旧 Job 删掉 |

在 Docker 上，迁移是一个一次性的 Compose 服务，它的失败以 `ENGINE_FAILED` 出现，错误块上方是 Compose 自己的输出。

### MIGRATION_SKIPPED

只会以警告出现，从不作为错误——见[警告](#警告)。它的意思是：平台不为 `local: true` 或 `servedBy` 的组件跑迁移，迁移得你自己来。

### ENGINE_FAILED

Docker Compose 或 `kubectl` 跑了，但失败了。引擎自己的原始输出会打印在错误块上方，通常已经说明了原因。

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：部分组件没有正常启动` | `up` 走完了，但有些容器不健康 | 先 `brickkit status`，再看容器日志。冷启动超过默认 60 秒宽限期的组件，要把 `healthCheck.startPeriodSeconds` 调大 |
| `错误：<command> 执行失败` | 引擎命令本身以非零状态退出 | 看错误块上方的原始输出 |
| `错误：kubectl 执行失败` | 某次 `kubectl` 调用失败 | 同上 |
| `错误：无法解析容器引擎的状态输出` | 装的 Docker Compose 比 V2 旧 | 升级 Compose——`brickkit version` 会打印检测到的引擎 |
| `错误：无法解析 kubectl 的输出` | `kubectl` 打印了 CLI 读不懂的东西 | 检查 `kubectl` 的版本 |
| `错误：没能从 registry 取到镜像 digest` | 发布会按 digest 钉住每个镜像，而 registry 没有响应 | 先把镜像推上去，并确认这台机器连得上 registry |
| `错误：无法确定镜像的 digest，发布已中止` | 同上，最终解析那一步 | `docker push <镜像>`；私有仓库先 `docker login`。`--no-pin-digest` 可以跳过钉 digest，但那样签名就只锁住 tag |

### ENGINE_MISSING

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：找不到容器引擎 <engine>` | 没装这个引擎的可执行文件 | 安装 Docker 20.10+ |
| `错误：没有找到可用的容器引擎` | 一个引擎都没找到 | 安装 Docker 20.10+——或用 `brickkit up --dry-run`，不需要引擎就能生成文件 |
| `错误：暂不支持 Podman，请使用 Docker` | 只装了 Podman。Podman 支持写过、后来撤回了：rootless Podman 上 `down` 会失败，而一个停不掉的项目比根本起不来的更糟 | 安装 Docker |
| `错误：找不到 kubectl` | `deploy.target: k8s` 需要 `kubectl` | 装上它，或本地开发时把 `deploy.target` 改成 `docker` |

## 网络、认证与镜像

### NETWORK_UNREACHABLE

唯一值得原样重试的码。

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：市场不可达` | 市场地址没有响应 | 检查网络和 `sources`（或 `--market`）里的市场地址，然后重试 |
| `错误：无法连接镜像仓库` | 镜像仓库没有响应 | 检查网络和仓库地址 |
| `错误：读取市场响应失败` | 响应到一半连接断了 | 重试 |
| `错误：市场返回的内容无法解析` | 这个 URL 有响应，但它不是 BrickKit 市场 | URL 应该以市场的 `/api/v1` 结尾 |
| `错误：市场返回的内容格式不符` | 市场用的协议版本不同 | 确认市场服务版本是否兼容 |
| `错误：市场返回的版本列表无法解析` | 同上，针对版本列表 | 同上 |
| `错误：市场返回的产物列表无法解析` | 同上，针对产物列表 | 同上 |
| `错误：<component> 的产物一个都没下载成功` | `brickkit fetch` 一个产物都没能下载 | 检查网络，以及这个组件是否声明了产物 |

### AUTH_REQUIRED

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：发布失败：未登录` | 没登录就执行了 `brickkit publish` | `brickkit login`，或配置 `sources.authToken` |
| `错误：无法确定要登录的市场地址` | `brickkit login` 不知道登录哪个市场 | `brickkit login --market https://market.example.com/api/v1` |
| `错误：brickkit.yaml 中没有可用的市场安装源` | 没有 `type: market` 的安装源可登录 | 在 `sources` 里加一个，或传 `--market` |
| `错误：配置了多个市场安装源，无法确定登录哪一个` | 有不止一个市场安装源 | 用 `--market` 选一个 |

### AUTH_FAILED

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：登录失败：用户名或密码错误` | 凭据不对 | 核对一下；忘记密码请联系市场管理员 |
| `错误：市场没有返回访问令牌` | 登录有响应，却没带令牌 | 确认市场服务版本是否兼容 |
| `错误：读取登录凭据失败` | `.brickkit/credentials` 读不了 | 重新 `brickkit login` |
| `错误：登录凭据格式不合法` | 那个文件损坏了 | 删掉它，再 `brickkit login` |
| `错误：写入登录凭据失败` | 写不进去 | 检查目录权限 |
| `错误：删除登录凭据失败` | `brickkit logout` 删不掉它 | 检查权限，或手工删除 |

私有组件拒绝你访问时也归在这个码下；那条消息来自市场自己，处理办法是确认当前账号是不是该组件的所有者。

### TOKEN_EXPIRED

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：Token 已过期` | 存着的令牌超过了有效期 | 重新 `brickkit login` |

### IMAGE_UNAUTHORIZED

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：镜像拉取未授权` | 镜像仓库拒绝了这次拉取 | `docker login <registry>`，并确认这个账号有拉取该镜像的权限 |
| `错误：镜像不存在` | 镜像不在仓库里，或者没在本地构建 | 检查 `deployment.image` 的拼写和 tag。本地测试的组件要先自己构建镜像——CLI 从不替你构建 |

## 签名

### SIGNATURE_INVALID

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：组件未签名，安装被阻断` | `installer.requireSignature` 开着（默认），而这个组件没有签名 | 让发布者用 `brickkit publish --sign` 重新发布；本地开发可以设 `requireSignature: false` |

签名与你 `installer.publicKeys` 里的公钥对不上，也归在这个码下，消息文字是运行时拼出来的。最常见的原因是公钥与发布者签名用的私钥不是一对——见[签名与信任模型](06-signing-and-trust.md)和[故障排除](../08-troubleshooting.md)。

## 源码工作区

### CLONE_FAILED

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：Git 仓库克隆失败` | 克隆失败 | 检查网络与仓库地址；私有仓库要配 Git 凭据；或把这个安装源设为 `enabled: false` |
| `错误：clone 失败` | `--repo` 没能克隆组件的仓库 | 同上 |
| `clone 失败：目录已存在` | 目标目录已经存在 | 如果是误操作，删掉或改名；如果源码已经在那儿，就无需再 clone |
| `clone 失败：源码已经在了，只是被归档着` | 源码在，只是被归档到了 `components/.archived/` | `brickkit sync` 会按启停判定把它带回来 |
| `clone 失败：该组件为闭源组件` | 闭源组件没有仓库可克隆 | 没有可克隆的东西 |
| `clone 失败：没有可用的 Git 仓库地址` | 提供这个组件的那个安装源没有 Git 地址 | 把一个 Git 安装源排到它前面，或去掉 `--repo` |
| `错误：无法创建源码目录` | 目标目录建不出来 | 检查权限 |

### SUBMODULE_GUARD

| 你会看到 | 原因 | 怎么办 |
| --- | --- | --- |
| `错误：无法删除组件源码——它是一个已登记的 git submodule` | `brickkit remove` 不会碰已登记的 submodule：直接删或改名不认识 `.gitmodules`，会悄悄让它的版本历史脱钩 | 手工执行错误块里列出的 `git submodule deinit` / `git rm`，再重跑 |
| `错误：无法移动组件源码——它是一个已登记的 git submodule` | `brickkit sync` 同样不会移动它 | 手工执行错误块里列出的等价 `git mv`，确认 `.gitmodules` 与 `git status` 都正常，再重跑 `brickkit sync` |

## 警告

⚠️ 块永远不会让命令失败（退出码 `0`）。CLI 继续往下跑时打印的那些不产生 `error_code` 日志行，所以这张表按标题索引；"码"一列是同一条消息在 CLI 源码里带的码，供好奇的人参考。

| 你会看到 | 码 | 含义与怎么办 |
| --- | --- | --- |
| `警告：弱依赖缺失：<component>` | `DEPENDENCY_MISSING` | 一个可选依赖不可用。它的 `*_ENDPOINT` 根本不会被注入——连空字符串都没有——所以组件必须防御式读取（`os.environ.get`，永远别写 `os.environ["X"]`） |
| `警告：<component> 被弱依赖` | `DEPENDENCY_MISSING` | 你在移除一个被别的组件弱依赖着的东西。允许这样做；它们会在没有它的情况下运行 |
| `警告：无法确认 <component> 的依赖关系` | `MANIFEST_INVALID` | `brickkit remove` 读不了另一个组件的 Manifest，所以没法核对它是否需要被移除的这个组件 |
| `brickkit.yaml 中存在明文密码` | `CONFIG_INVALID` | 密码被直接写在了文件里。改成 `password: ${DB_PASSWORD}`，真实值放进 `.env`，并让 `.env` 在 `.gitignore` 里 |
| `brickkit.yaml 的 config 里可能写了明文密钥` | `CONFIG_INVALID` | `config` 里一个名字像密钥的项写了字面值。判据只看名字，不看值；确实不是密钥的话可以忽略 |
| `config 里有配置项不会生效：组件 <component> 的 <key>` | `CONFIG_INVALID` | `config` 里的某个键不在该组件 `configSchema.properties` 里——通常是笔误，CLI 会猜你想写的是哪一个。没有这个检查的话，那个变量会压根不存在，组件悄悄走默认值 |
| `config 整块不会生效：组件 <component> 没有声明 configSchema` | `CONFIG_INVALID` | 你给一个没声明 `configSchema` 的组件写了 `config`，所以整块都不会生效 |
| `警告：configSchema 里有配置项声明的键不会生效` | `MANIFEST_INVALID` | `configSchema` 下某个属性有拼错的键（`defualt:`）。发布、或从本地安装源添加时显示 |
| `配置冲突：组件 <component> 的配置项已被忽略` | `CONFIG_CONFLICT` | 一个 `configSchema` 键转成大写后与保留变量（`*_ENDPOINT`、`DATABASE_*` ……）冲突；平台注入的值优先，这个键被跳过。把键改名——见[环境变量注入契约](04-environment-variables.md) |
| `基础资源的 host 看起来是个服务名，容器里可能解析不了` | `CONFIG_INVALID` | 资源的 `host` 像是一个 Compose 服务名，但资源并不属于本项目。资源在你本机时写 `host.docker.internal`，否则写它的真实地址 |
| `配置里有只对 <target> 生效的字段` | `CONFIG_INVALID` | 有个字段只对另一种 `deploy.target` 生效——比如 `k8s` 下的 `exposePort`——现在它什么也没做 |
| `local: true 的组件上，labels 本次不生效` | `CONFIG_INVALID` | `local: true` 的组件没有容器可以挂标签。想让平台管标签，就去掉 `local: true` |
| `提示：local 组件的数据库迁移不会自动执行` | `MIGRATION_SKIPPED` | `local: true` 的组件跑在你本机，CLI 不为它跑迁移。自己执行一次迁移命令，环境变量用它的 `local-debug.<service>.env` |
| `提示：servedBy 组件的数据库迁移不会自动执行` | `MIGRATION_SKIPPED` | `servedBy` 成员没有自己的容器，也就没有迁移容器。得由外壳来覆盖它 |
| `提示：servedBy 组件自己的健康检查不会独立生效` | `CONFIG_INVALID` | 算数的是外壳的健康检查 |
| `提示：servedBy 组件上，<field> 本次不生效` | `CONFIG_INVALID` | `expose`、`exposePort`、`hostname`、`replicas`、`resources`、`serviceAccountName`、`labels` 描述的是一个组件自己的容器怎么部署，而 `servedBy` 成员没有自己的容器。想单独部署这个组件，就去掉它的 `servedBy` |
| `警告：requireSignature 为 true，但项目没有声明任何可信公钥，签名校验实际未生效` | `SIGNATURE_INVALID` | 一个 `installer.publicKeys` 都没有时，验证整体关闭——光有 `requireSignature: true` 什么也验证不了。声明发布者的公钥，或显式设 `requireSignature: false` 让这条提醒消失 |
| `警告：签名来自未声明的发布者，未做校验` | `SIGNATURE_INVALID` | 签名指向一个你没声明公钥的发布者，所以没有校验 |
| `警告：产物下载失败，已跳过` | `NETWORK_UNREACHABLE` | 某个产物没能下载；安装在没有它的情况下继续了 |
| `跳过组件结构检查：<reason>` | `CONFIG_INVALID` | pre-commit hook 这次没能跑它的结构检查，提交照常进行。`brickkit restore --check` 可以手工跑一遍 |

## 延伸阅读

- [故障排除](../08-troubleshooting.md)——人们真正撞上的那些失败，按"你看到了什么"而不是按错误码来组织。
- [环境变量注入契约](04-environment-variables.md)——保留变量冲突的警告，附真实示例。
- [CLI 命令完整参考](09-cli-reference.md)——每一个命令与参数。
