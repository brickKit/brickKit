# CLI 命令参考

BrickKit CLI 共 21 个业务命令，外加 `version` 与 `lang` 两个关于 CLI 自身的命令。这一页把每条命令、每个子命令、
每个参数都写全了；内容与 `brickkit <命令> --help` 一致，`--help` 是权威。

没有长驻进程：每条命令跑完就退出。项目的"应该是什么样"写在三层文件里（`brickkit.yaml`、部署文件、`config/`，
见 [三层架构](../01-three-layers/README.md)），"实际是什么样"在底层引擎（Docker / Podman / K8s）里。

## 命令总览

| 分组 | 命令 | 一句话 |
| --- | --- | --- |
| 项目 | [`init`](#brickkit-init) | 创建项目，或把一个目录补全成项目 |
| 项目 | [`skills`](#brickkit-skills) | 管理装进项目的 AI 助手技能 |
| 组件 | [`new`](#brickkit-new) | 生成一个组件（或外壳）的骨架 |
| 组件 | [`add`](#brickkit-add) | 拉取组件及其依赖，写进三层文件 |
| 组件 | [`remove`](#brickkit-remove) | 移除组件，配置移进 `config/.archive/` |
| 组件 | [`upgrade`](#brickkit-upgrade) | 移动组件的默认版本，迁移配置 |
| 组件 | [`fetch`](#brickkit-fetch) | 只下载组件的产物（契约），不装进项目 |
| 查看 | [`lint`](#brickkit-lint) | 离线检查三层文件与 `component.yaml` |
| 查看 | [`deps`](#brickkit-deps) | 把依赖关系打印成树 |
| 查看 | [`graph`](#brickkit-graph) | 把依赖拓扑画成 Mermaid 图 |
| 运行 | [`build`](#brickkit-build) | 构建需要在本机构建的镜像 |
| 运行 | [`up`](#brickkit-up) | 生成部署文件、跑迁移、启动 |
| 运行 | [`down`](#brickkit-down) | 停止项目（不删数据卷） |
| 运行 | [`status`](#brickkit-status) | 查看运行状态 |
| 运行 | [`local`](#brickkit-local) | 管理本地模式（个人的 `deploy.local.yaml`） |
| 源码 | [`sync`](#brickkit-sync) | 把用不上的组件源码收进 `components/.archived/` |
| 源码 | [`restore`](#brickkit-restore) | 把 `mode` 与源码结构还原到最后一次提交 |
| 发布 | [`release`](#brickkit-release) | 打 Git tag 并推送，不经市场发布组件 |
| 发布 | [`publish`](#brickkit-publish) | 把组件发布到组件市场 |
| 发布 | [`login`](#brickkit-login) | 登录组件市场 |
| 发布 | [`logout`](#brickkit-logout) | 退出组件市场的登录 |
| CLI | [`version`](#brickkit-version) | 查看 CLI 版本、支持的 Manifest 版本与部署目标 |
| CLI | [`lang`](#brickkit-lang) | 查看或切换 CLI 的显示语言 |

## 全局参数

只有一个，每条命令都接受：

| 参数 | 说明 |
| --- | --- |
| `--log-level <级别>` | stderr 上 JSON 日志的级别：`debug` / `info` / `warn` / `error` / `off`，缺省 `warn` |

缺省的 `warn` 很安静：只有出错时，`❌` 那段说明后面会跟一行带 `error_code` 的 JSON 日志，给脚本判断用
（错误码见 [错误码参考](../06-architecture/09-error-codes.md)）。想看每条命令的起止等例行日志，用
`--log-level info`；想在整个 shell 里都这样，设一次环境变量 `BRICKKIT_LOG_LEVEL=info`。`--log-level off` 连出错时
那行 JSON 也不打，只适合没有程序解析 `error_code` 的场合（比如 pre-commit 钩子）。

## 读部署文件的命令共用的参数

`up`、`down`、`status`、`sync`、`lint` 读部署文件，都接受下面两个参数；`graph` 只接受 `-f`（它本来就从不读本地模式）。

| 参数 | 说明 |
| --- | --- |
| `-f, --file <文件>` | 只读这一份部署文件（如 `deploy.prod.yaml`，路径相对项目根），本地模式被忽略。文件不存在直接报错 |
| `--no-local` | 本次忽略 `deploy.local.yaml`，读 `deploy.yaml`；本地模式的开关不变 |

不带这两个参数时，读哪一份由本地模式决定：开着读 `deploy.local.yaml`，关着读 `deploy.yaml`
（见 [deploy.local.yaml](../01-three-layers/04-deploy-local-yaml.md)）。每个环境一份完整的部署文件，用 `-f` 选——
这就是多环境的全部做法：

```bash
brickkit up -f deploy.prod.yaml
```

```text
❌ 错误：--file 指定的部署文件不存在
   路径：deploy.prod.yaml
   建议：路径相对项目根目录；检查一下拼写，例如 -f deploy.prod.yaml
```

---

## `brickkit init`

创建 BrickKit 项目，或把一个目录补全成项目。两种模式共用同一个补全原语——对照完整项目的文件清单，缺的创建，有的不动：

| 写法 | 模式 | 做什么 |
| --- | --- | --- |
| `brickkit init <名字>` | 创建式 | 新建 `<名字>/` 目录，在里面生成整套骨架 |
| `brickkit init` | 补全式 | 当前目录缺什么补什么：已有项目接入，或组件仓库给自己建一个本地联调工作台 |

完整的项目有 `brickkit.yaml`、`deploy.yaml`、`config/vars.yaml`、`components/` 与 `shell/` 两个本地源、`BRICKKIT.md`
（项目地图）和 `.gitignore`。`deploy.local.yaml` 从不在这里生成，要用时由 `brickkit local on` 生成。

补全式的规矩：

- 已有的文件跳过，一个字节都不动。
- 已有的 `.gitignore` 只校验、不修改：缺的每一条必需条目都大声警告——缺了它们，个人部署文件和密钥会被提交。
- 目录非空时先打印计划，确认后才执行（`--yes` 跳过确认）。
- 项目名取 `--name`，否则沿用已有 `brickkit.yaml` 的 `project`，否则取目录名。
- 最后按 `lint` 与 `up` 的方式装载一遍项目；装载失败，命令就失败。

项目名只能用小写字母、数字和中划线，以字母或数字开头结尾——它会成为 K8s namespace 与 Docker 网络的名字。

`init` 同时装入 AI 助手技能（`.claude/skills/`、`AGENTS.md`），但不碰你的 `CLAUDE.md`。项目根就是 Git 仓库根时，
顺带装上提交前检查（pre-commit 钩子，调用 `brickkit restore --check`）；项目嵌在别人的仓库里时，用 `--hooks` 显式安装。

```text
brickkit init [<项目名>] [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--name <名字>` | 补全式的项目名 |
| `--yes` | 不询问、直接补全（CI 用） |
| `--no-skills` | 不装 AI 助手技能 |
| `--hooks` | 只安装提交前检查用的 pre-commit 钩子（在已有项目里补装） |

```bash
brickkit init my-shop                 # 新建 my-shop/，生成整套骨架
brickkit init                         # 补全当前目录（目录非空时先确认）
brickkit init --name my-shop --yes    # 不询问直接补全（CI 用）
brickkit init my-shop --no-skills     # 不装 AI 助手技能
brickkit init --hooks                 # 只装提交前检查
```

```text
✅ 项目已初始化：my-shop
   📄 brickkit.yaml        项目配置
   📄 deploy.yaml          怎么部署（团队文件，要提交）
   📁 config/              组件配置与公共变量
   📁 components/          组件源码（已配为本地安装源 local-dev）
   📁 shell/               外壳（kind: shell），项目自己的代码（本地安装源 local-shells）
   📁 .brickkit/           CLI 工作目录
   📄 BRICKKIT.md          项目地图：有哪些组件、文档在哪
```

## `brickkit skills`

管理装进本项目的 AI 助手技能（`.claude/skills/`、`AGENTS.md`）。这些文件由 `init` 装入、跟着项目提交、团队共享；
它们描述的是**当前这个 CLI 版本**的行为，所以 CLI 升级后要刷新一次。不带子命令等同 `brickkit skills status`。

**手改过的文件绝不覆盖**：`update` 把它们列出来并跳过。想放弃本地修改，删掉那个文件再执行一次 `update`——
刻意不提供 `--force`：删文件这个动作本身已经足够明确，多一个开关就多一条误伤路径。

在独立的组件仓库里（有 `component.yaml`、没有 `brickkit.yaml`）也能用：这时只管理 `brickkit-component` 一个技能，
组件仓库自己的 `AGENTS.md` 一个字都不碰。任何情况下都不碰你的 `CLAUDE.md`。

```text
brickkit skills [flags]
brickkit skills <命令> [参数]
```

### `brickkit skills status`

查看每个技能文件的状态（只读）。没有自己的参数。

```text
技能语言：zh（记在 skills.lock 里；brickkit skills update --lang 可以改）
   ┌───────────────────────────────────────────────┬──────┐
   │ 文件                                          │ 状态 │
   ├───────────────────────────────────────────────┼──────┤
   │ AGENTS.md                                     │ 缺失 │
   │ .claude/skills/brickkit-assemble/SKILL.md     │ 缺失 │
```

### `brickkit skills update`

缺的装上、旧的刷新；手改过的跳过。

| 参数 | 说明 |
| --- | --- |
| `--lang <en\|zh>` | 用这种语言重装，并从此记住这个项目该用哪种语言 |

```bash
brickkit skills update
brickkit skills update --lang zh
```

## `brickkit new`

生成一个组件的骨架：一份能通过 `brickkit up --dry-run` 校验的 `component.yaml`，和一份带标准章节（组件定位、依赖说明、
配置指南、契约索引、外壳声明）的 `BRICKKIT.md`，写给将来使用这个组件的人和 AI 助手。

默认写到 `components/<scope>/<name>/`——本地安装源本来就按这个布局扫描，写完就能 `brickkit add --local` 加进项目。
生成之后**不会**自动 `add`：写进 `brickkit.yaml` 是一次单独、可审阅的动作。

```text
brickkit new <scope>/<name> [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--contract <openapi\|proto>` | 顺带生成一份契约占位文件，登记在 `artifacts` 里；缺省不生成 |
| `--shell` | 生成外壳骨架（带一个要换掉的占位成员的 `shell.members`），默认写到 `shell/<scope>/<name>/` |
| `--path <目录>` | 写到别的目录，给独立成一个 Git 仓库的组件用；这时目录本身就是组件仓库根，不再套 `<scope>/<name>` |

```bash
brickkit new demo/widget                          # 写到 components/demo/widget/
brickkit new demo/widget --contract openapi       # 顺带生成一份 OpenAPI 契约占位文件
brickkit new erp/shell --shell                    # 外壳骨架，写到 shell/erp/shell/
brickkit new demo/widget --path ../widget-repo    # 写到别的目录（独立仓库场景）
```

## `brickkit add`

拉取组件及其依赖，一次写好三层文件：`brickkit.yaml` 的声明、部署文件的条目（`deploy.yaml`，存在时还有
`deploy.local.yaml`）、`config/` 的配置骨架。

- 不写版本时取安装源里最新的精确版本，钉进 `brickkit.yaml`。
- 依赖要的是项目里已有组件的另一个版本时，这个版本以 `requiredBy` 保留（多版本共存）。
- `add` 一个外壳时，它编进的成员按声明的版本一起加进来，嵌在外壳条目的 `members` 下面。
- 配置骨架里只有"必填且没有默认值"的键写成 `KEY: ""`，其余注释掉；有必填项要填时，`add` 点名是哪个文件、哪几个键。
- `config/vars.yaml` 里有同名公共变量时，会问你要不要直接写成 `$var:` 引用。
- `config/.archive/` 里有这个组件以前的配置（`remove` 留下的），按迁移规则恢复，而不是给一份空骨架。
- 改完之后项目装载不了，全部还原。

```text
brickkit add [组件ID[@精确版本]] [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--local` | 把本地安装源里的组件一次全部加进来 |
| `--init` | 与 `--local` 一起用：先给每个还没有 `brickkit.yaml` 的本地组件建好本地联调工作台 |
| `--repo` | 同时把这个组件的源码仓库克隆到 `components/`，检出这个版本 |
| `--repo-all` | 同时克隆这次加进来的每个开源组件的源码（默认版本） |
| `-y, --yes` | 非交互：每个问题都回答"是"（CI 用） |

```bash
brickkit add erp/backend                    # 最新 tag，钉成精确版本
brickkit add erp/backend@1.0.0              # 指定版本
brickkit add --local                        # 本地安装源里的组件一次全加
brickkit add --local --init                 # ……并先给每个组件建好本地联调工作台
brickkit add erp/backend@1.0.0 --repo       # 顺便克隆源码（检出这个版本）
brickkit add erp/backend@1.0.0 --yes        # 非交互（CI）
```

```text
➕ 加入 demo/greeter@0.1.0, demo/hello@0.1.0
   ✅ demo/hello@0.1.0
   ✅ demo/greeter@0.1.0
📝 已写：brickkit.yaml, deploy.yaml
📝 配置骨架：config/demo-greeter.yaml
```

## `brickkit remove`

先检查有没有别的组件强依赖它，然后移除：

- 配置不删，移进 `config/.archive/`——以后再 `add` 回来时按它恢复。
- 部署文件（`deploy.yaml`，存在时还有 `deploy.local.yaml`）里的条目一起删。
- 删外壳时，它承载的成员挪回顶层、独立运行。
- 只因它而保留的兼容版本（`requiredBy` 只剩它）一并移除。
- 删的是默认版本、而这个组件只剩一个版本时，剩下的那个转正成默认版本。
- 组件的最后一个版本走了，才删它的源码目录（连同已归档的那一份），而且先确认删了还找得回来：有未提交或未推送的改动就停下。

```text
brickkit remove <组件ID>[@版本] [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--force` | 源码目录里有未提交、未推送的改动也照样删 |

```bash
brickkit remove erp/backend                 # 只有一个版本时
brickkit remove erp/backend@1.0.0           # 有好几个版本时写明
brickkit remove erp/backend@1.0.0 --force   # 源码目录里有没推送的改动也删
```

## `brickkit upgrade`

移动组件的默认版本，三层文件跟着改：

- `brickkit.yaml` 的版本与 `requiredBy`；
- 部署条目；
- `config/` 的迁移：你写过的值抄过去，新增的键按默认值，新版本删掉的键留在归档里并报出来。

还有组件依赖旧版本时，旧版本留作兼容版本。升级外壳会换成新外壳编进的那一套成员版本。不写组件就升级所有有新版本的
组件——要么全做，要么一个都不改。

你改过、而组件作者也改了默认值的键是**冲突**：终端里逐条选择；`--yes` 或没有终端输入时，写成两行重复键，
`up` 在你删掉其中一行之前拒绝启动（见 [解析优先级](../01-three-layers/08-resolution-priority.md#同一级里重复写了同一个键)）。

```text
brickkit upgrade [组件ID[@版本]] [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--dry-run` | 只显示会发生什么，不改任何文件 |
| `-y, --yes` | 非交互：配置冲突一律写成两行重复键 |

```bash
brickkit upgrade                    # 所有有新版本的组件
brickkit upgrade erp/backend        # 升到最新
brickkit upgrade erp/backend@2.0.0  # 升到指定版本
brickkit upgrade --dry-run          # 只看会发生什么
```

## `brickkit fetch`

下载一个组件声明的产物文件（`.proto`、`openapi.json` 之类），但**不把它装进本项目**。用于跨项目调用：你要对方的契约来
生成客户端，而那个服务由别的项目部署——写进 `brickkit.yaml` 会让平台在你这边再部一份。

不写版本号时取安装源里的最新版本。产物落到 `.brickkit/artifacts/<版本化服务名>/<type>/...`，与 `add` 下来的是同一个位置。
`.brickkit/` 是本机缓存、不进 Git：队友在自己机器上执行同一条 `fetch`；要让契约随项目提交，把需要的文件复制到你自己的目录里。

这条命令只读：不修改 `brickkit.yaml`，不生成部署文件，不启动任何东西。没有自己的参数。

```text
brickkit fetch <组件ID>[@<版本>] [flags]
```

```bash
brickkit fetch infra/notifier@1.0.0   # 取指定版本的产物
brickkit fetch infra/notifier         # 取最新版本的产物
```

## `brickkit lint`

不联网、不需要 Docker / K8s，只读地把这个目录里的 YAML 检查一遍。按当前目录自动判断场景：

**项目（有 `brickkit.yaml`）：**

1. `brickkit.yaml`；
2. 部署文件：`deploy.yaml`，存在时还有 `deploy.local.yaml`（给了 `-f` 只查那一份）——两份都必须与 `brickkit.yaml` 一致，不管本地模式开没开；
3. 三层文件放在一起查：每个组件版本恰好一个部署条目、成员写在外壳下面、`$var:` 都有定义、`config/` 的文件对得上组件、没有升级遗留的重复键；
4. 按 `configSchema` 查每个组件的配置：必填项有值、写下的键在 schema 里、外壳成员的值能 JSON 编码进外壳；以及外壳声明：`kind: shell`
   与 `shell` 块一致、放在外壳下面的成员确实编进了外壳——只查盘上有 Manifest 的组件，其余的列为未检查；
5. 本地安装源目录下的每一份 `component.yaml`，不管有没有 `add` 过（`.archived/` 不查）。

`brickkit.yaml` 自己没通过时，后面的都不可信，跳过并说明。

**组件仓库（有 `component.yaml`、没有 `brickkit.yaml`）：** 只查这一份 `component.yaml`。

查的是结构规则：必填字段、类型、未知字段（拼写笔误）、版本号格式、端口范围。警告有两类：`configSchema` 里拼错的键
（比如 `defualt`）不会生效；配置项名字撞上平台保留变量。**不查**：依赖能不能解析、外壳这次承载的成员版本与它编进的版本对不对得上
（要解析出依赖图才知道，而 `lint` 故意不建这张图——那可能意味着联网），这些留给 `up --dry-run` 与 `graph`；
也不查配置的值合不合 `enum`、`minimum`（平台只检查键名、不检查值）。

有错误时退出码为 1；只有警告时为 0。

```text
brickkit lint [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--strict` | 还检查引用：进程环境与 `.env` 里都没有的 `${VAR}`、文件不存在的 `file://` 报成警告；并且警告也算失败（退出码 1），给 CI 门禁用 |
| `-f, --file <文件>` | 只查这一份部署文件，见 [共用参数](#读部署文件的命令共用的参数) |
| `--no-local` | 本次忽略 `deploy.local.yaml` |

```bash
brickkit lint
brickkit lint --strict           # 警告也算失败（CI 门禁）
brickkit lint -f deploy.prod.yaml
```

```text
✅ brickkit.yaml
✅ deploy.yaml
✅ 跨文件：brickkit.yaml ↔ deploy.yaml ↔ config/
✅ components/demo/greeter/component.yaml
✅ components/demo/hello/component.yaml

📋 检查了 5 个文件：0 个有错误，0 条警告
```

## `brickkit deps`

把项目的依赖关系打印成树。`brickkit.yaml` 只锁版本；谁依赖谁写在各组件自己的 `component.yaml` 里，这条命令从那里读出来——
与 `graph` 画的是同一张解析好的依赖图（没缓存的 Manifest 会从安装源获取）。

| 写法 | 输出 |
| --- | --- |
| `brickkit deps` | 每个顶层组件（项目里没有谁依赖它）一棵树 |
| `brickkit deps <id>` | 这个组件的树（项目里它的每个版本各一棵），以及谁依赖它 |
| `brickkit deps <id>@<版本>` | 只看这一个版本 |

一个组件版本在一次输出里只展开一次，之后再出现标"（见上）"；弱依赖标"（弱依赖）"，不在项目里的弱依赖标"（弱依赖，未安装）"。
没有自己的参数。

```text
brickkit deps [<id>[@<version>]] [flags]
```

```bash
brickkit deps
```

```text
demo/greeter@0.1.0
└── demo/hello@0.1.0
```

```bash
brickkit deps demo/hello
```

```text
demo/hello@0.1.0

被依赖：demo/greeter@0.1.0
```

## `brickkit graph`

把依赖拓扑画成 Mermaid 图，打印到 stdout：

| 图上 | 表示 |
| --- | --- |
| 节点 | 组件 `id@版本`；`mode: local` 的标上"托管本地"（写了 `localPort` 的连端口一起标） |
| 实线 / 虚线 | 强依赖 / 弱依赖（取不到的弱依赖画成"未安装"节点） |
| 置灰 | 这次不会启动的组件（`mode: disable` 关掉的，以及跟着上层一起不跑的） |
| 分组 | 这次被外壳承载的成员画在外壳的 subgraph 里 |

`graph` **从不读本地模式**：它读 `deploy.yaml`（或 `-f` 指定的那份），所以它的输出可以提交、分享，不会因为谁生成的而不同。
stdout 里只有 Mermaid，`brickkit graph > graph.mmd` 就得到一份 GitHub 能直接渲染的文件；依赖解析产生的警告写到 stderr。

```text
brickkit graph [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--ignore-shells` | 画之前在内存里忽略全部外壳的 `members`，看每个组件独立启动时的样子；不写回任何文件 |
| `-f, --file <文件>` | 画这一份部署文件，见 [共用参数](#读部署文件的命令共用的参数) |

```bash
brickkit graph
brickkit graph > graph.mmd
brickkit graph --ignore-shells
```

```text
graph TD
    demo_hello_0_1_0["demo/hello@0.1.0"]
    demo_greeter_0_1_0["demo/greeter@0.1.0"]
    demo_greeter_0_1_0 --> demo_hello_0_1_0
```

## `brickkit build`

构建需要在本机构建的镜像：没有 `deployment.image` 的组件，以及本地安装源里的组件（正在开发的代码，镜像必须从它构建）。
有 `image` 的 git / 市场组件是拉取的，不构建。

镜像 tag 与组件的 `metadata.version` 一致；外壳镜像记下编进去的成员版本，`up` 用它核对。镜像已存在时跳过。
源码来自本地仓库（正是这个版本时），否则从这个版本的 Git tag 导出。**`up` 从不构建**：镜像不在时它报错，提示运行 `build`。

```text
brickkit build [组件ID[@版本]] [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--force` | 镜像已存在也重新构建 |

```bash
brickkit build                      # 构建所有需要本机构建的组件
brickkit build erp/backend          # 只构建这个组件
brickkit build erp/backend --force  # 改了代码之后重新构建
```

## `brickkit up`

一键启动项目：

1. 装载三层文件与所有组件的 Manifest；
2. 启停判定（跟着上层走：顶层没写 `mode` 就跑，下层跟上层）；
3. 检查强依赖（缺失报错）与弱依赖（缺失警告，且完全不注入环境变量）；
4. 拓扑排序得出启动顺序；
5. 解析配置、注入环境变量、合并资源配额，生成部署文件：`docker` / `podman` 生成 `.brickkit/generated/compose.yaml`，`k8s` 生成 Kubernetes 清单；
6. 有 `mode: debug` 组件时生成 `local-debug.<版本化服务名>.env`，给你在 IDE 里启动它用；
7. 检查镜像：本机构建的镜像必须已经在（缺了就提示 `brickkit build`），拉取的镜像要取得到；
8. 调用底层引擎启动；数据库迁移先跑（Docker 一次性容器、K8s Job），失败则阻断主服务；
9. 有 `mode: local` 组件时，在前台启动并看护这些本机进程，`Ctrl+C` 停止。

组件版本与上一次 `up` 不同时会提示一句，`--dry-run` 还会输出变更摘要；移动版本本身是 `upgrade` 的事。
部署目标是 `k8s` 时，真正部署（不带 `--dry-run`）之前，`up` 先确认要部署到的集群（部署文件的 `k8s.context`），再生成任何东西。

```text
brickkit up [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--dry-run` | 只生成部署文件，不启动（有版本变化时额外输出变更摘要） |
| `--ignore-shells` | 在内存里忽略全部外壳的 `members` 再跑一次，验证每个组件能否独立启动；不写回任何文件 |
| `--crash-lines <N>` | `mode: local` 的组件崩溃时，最后一屏打印它最后几行输出；缺省 20，`0` 只打印崩溃信息 |
| `-f, --file <文件>` | 用这一份部署文件，见 [共用参数](#读部署文件的命令共用的参数) |
| `--no-local` | 本次忽略 `deploy.local.yaml` |

```bash
brickkit up
brickkit up --dry-run              # 只生成文件，不启动
brickkit up -f deploy.prod.yaml    # 使用另一份部署文件（每个环境一份完整文件）
brickkit up --no-local             # 本次忽略 deploy.local.yaml
brickkit up --ignore-shells --dry-run
```

```text
🚀 启动项目 my-shop（target: docker）
📋 组件状态计算：
   ✅ demo/hello@0.1.0    启动（demo/greeter 需要）
   ✅ demo/greeter@0.1.0  启动（顶层）

📋 启动顺序（拓扑排序）：
   1. demo-hello-0-1-0    无依赖
   2. demo-greeter-0-1-0  ← 依赖 1

可独立启动：demo-hello-0-1-0（无依赖）
最长依赖链（2 层）：demo-hello-0-1-0 → demo-greeter-0-1-0

依赖图：
   demo/greeter@0.1.0 → demo/hello@0.1.0
📄 已生成：.brickkit/generated/compose.yaml

💡 --dry-run 只生成文件，未启动任何组件
   查看：cat .brickkit/generated/compose.yaml
```

## `brickkit down`

停止项目。停止顺序与启动顺序相反（依赖方先停，被依赖方后停），交给引擎处理。

**`down` 不删除数据卷**，数据库数据始终保留；要彻底清理，自己执行 `docker volume rm`（或 `docker compose down -v`，Podman 同理）。
只想停其中几个：在部署文件里给它们的条目写 `mode: disable` 再 `up`——引擎会把对应的容器一并移除，而且配置里留下了痕迹，
下次 `up` 不会又把它们拉起来。`mode: local` 的进程属于运行 `up` 的那个终端，`down` 够不着它，只提示那个会话的进程号。

```text
brickkit down [flags]
```

| 参数 | 说明 |
| --- | --- |
| `-f, --file <文件>` | 按这一份部署文件的目标去停，见 [共用参数](#读部署文件的命令共用的参数) |
| `--no-local` | 本次忽略 `deploy.local.yaml` |

```bash
brickkit down
```

## `brickkit status`

查看当前项目所有组件的运行状态。CLI 本身不存储运行状态，查询时直接问部署文件里 `target` 对应的引擎：

| target | 查询方式 |
| --- | --- |
| `docker` | `docker compose ps -a --format json` |
| `podman` | `podman compose ps -a --format json` |
| `k8s` | `kubectl get deployments -o json`（在项目的命名空间里） |

输出包含：运行中的组件、未启动的组件及原因（原因来自 `deploy.local.yaml` 时会标出来）、本地调试组件（`mode: debug`）。
`mode: local` 的组件是另一个终端里 `up` 看护的进程，不在表里；有这样的会话在跑时，会提示是哪个进程。

```text
brickkit status [flags]
```

| 参数 | 说明 |
| --- | --- |
| `-f, --file <文件>` | 按这一份部署文件去查，见 [共用参数](#读部署文件的命令共用的参数) |
| `--no-local` | 本次忽略 `deploy.local.yaml` |

```bash
brickkit status
```

## `brickkit local`

管理本地模式：你个人的部署文件 `deploy.local.yaml`（从不提交）。本地模式开着时，所有命令读 `deploy.local.yaml` 而不是
`deploy.yaml`——读整份文件，不是两份合并，所以文件里写的就是实际运行的。个人的事实写在这里：正在调试的组件写
`mode: debug`、本机空着的 `localPort`、自己数据库的 `vars` 值，甚至换一个 `target`（见 [deploy.local.yaml](../01-three-layers/04-deploy-local-yaml.md)）。

文件里的组件必须与 `brickkit.yaml` 声明的完全一致：团队新加了组件，`up` 会拒绝运行，直到你 `refresh`（或者手改文件、
或者关掉本地模式）。单次运行可以用 `--no-local` 忽略它；`-f <文件>` 也会忽略它。

```text
brickkit local [flags]
brickkit local <命令> [参数]
```

四个子命令都没有自己的参数。

### `brickkit local on`

开启本地模式；第一次开启时从 `deploy.yaml` 复制出 `deploy.local.yaml`——内容一字不差，只有 `init` 写的团队文件头注释换成个人文件的说明。

```text
✅ 本地模式已开启：命令现在读取 deploy.local.yaml
   deploy.local.yaml 从 deploy.yaml 复制而来——随意修改，它不会被提交
```

### `brickkit local off`

关闭本地模式。文件留着，下次 `local on` 接着用。

```text
✅ 本地模式已关闭：命令现在读取 deploy.yaml
   deploy.local.yaml 保留着；brickkit local on 会接着用它
```

### `brickkit local status`

查看开关、文件，以及文件是否还与 `brickkit.yaml` 一致。

```text
本地模式：开启
deploy.local.yaml：存在，正在使用
✅ deploy.local.yaml 与 brickkit.yaml 一致
```

### `brickkit local refresh`

团队改了 `deploy.yaml` 之后重新复制。旧文件保存为 `deploy.local.yaml.bak`，其中每一处本地修改都列出来，由你合并回去。

```bash
brickkit local refresh
```

## `brickkit sync`

把当前用不上的组件源码从 `components/` 收进 `components/.archived/`。组件一多，那些当下根本不碰的源码仍然堆在 `components/` 下：
IDE 索引、全局搜索、`grep`、以及替你读代码的 AI 都得连它们一起扫。`sync` 把它们挪进一个固定的目录——不打开就不用关心，
要找时又一眼知道在哪。

**判据与 `up` 完全一致**：这次会启动的留在活跃目录，不启动的归档。想收窄范围就改部署文件里的 `mode`（顶层关掉，下面一串跟着走），
`sync` 跟着走。

- 双向：该归档的归档，该激活的移回来；
- 不影响运行中的容器，也不改变 `up` 会启动谁——它只动目录；
- 只操作 `brickkit.yaml` 里声明过、且已有源码的组件；
- 整个目录连 `.git` 一起搬，归档后 git 命令照常；
- 刻意不提供 `--dry-run`：搞错了再执行一次就回来了。

```text
brickkit sync [flags]
```

| 参数 | 说明 |
| --- | --- |
| `-f, --file <文件>` | 按这一份部署文件判定，见 [共用参数](#读部署文件的命令共用的参数) |
| `--no-local` | 本次忽略 `deploy.local.yaml` |

```text
📂 工作区整理：
   ✅ components/demo/greeter/             活跃
   ✅ components/demo/hello/               活跃
✅ 工作区整理完成（2 个活跃，0 个归档，0 个激活）
```

## `brickkit restore`

把 `deploy.yaml` 里各组件的 `mode` 还原成最后一次提交的值，再让源码结构跟着走。

给谁用：把 `components/` 从 `.gitignore` 去掉、让组件源码跟项目一起进版本库的项目。那种项目里 `sync` 移动目录会进项目的 diff，
而"关掉几个顶层、`sync` 归档、忘了还原就提交"总会发生。

它只动 `deploy.yaml`（团队文件）的 `mode` 字段，逐条目：

| 条目 | 处理 |
| --- | --- |
| 工作区与最后一次提交都有 | `mode` 回到提交里的值（提交里没写就删掉这个字段） |
| 工作区新增的（刚 `add` 的，或裸 id 改成了 `id@版本`） | 一个字不动 |
| 提交里有、工作区没有 | 绝不加回来（这不是 `git revert`） |

`deploy.local.yaml` 是个人文件、不进版本库，没有可还原的基准，保持原样。被覆盖的旧值会在动手前打印出来。

```text
brickkit restore [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--check` | 只检查、不改任何东西：即将提交的 `deploy.yaml` 与即将提交的目录结构是否自洽？不自洽就以非零退出——pre-commit 钩子调用的正是它 |

```bash
brickkit restore           # 还原 mode 与源码结构
brickkit restore --check   # 只检查这次提交自洽不自洽
```

## `brickkit release`

不经市场发布组件：发布就是往组件自己的仓库推一个 Git tag。版本号取 `component.yaml` 的 `metadata.version`——唯一的事实来源。
只读 `component.yaml`；同一目录里的 `brickkit.yaml`（作者的本地联调工作台）与发布无关。

写任何东西之前，这些检查必须全部通过：

- `component.yaml` 能解析、能通过校验；
- 组件目录里没有未提交的改动（monorepo 子目录里的组件只看它自己的目录）；
- 当前分支有上游、没有未推送的提交：打 tag 的提交必须已经在远端历史里；
- 这个 tag 还不存在（本地和远端都查）。

然后打 tag 并推送。推送失败时删掉本地 tag——发布要么完整做完，要么不留痕迹。组件目录是仓库根时 tag 是 `<版本>`，是子目录时是
`<scope>-<name>/<版本>`——正是 git 安装源读取的名字。

```text
brickkit release [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--path <目录>` | 组件目录（`component.yaml` 所在处），缺省当前目录 |
| `--local` | 发布项目本地安装源里的全部组件：先全部检查一遍，再逐个打 tag、推送；已经发布过的（tag 就在当前提交上）跳过；遇到第一个推送失败就停 |

```bash
brickkit release                                  # 当前目录的组件
brickkit release --path ./components/erp/backend
brickkit release --path svc/api                   # monorepo 子目录里的组件（tag 为 erp-api/<版本>）
brickkit release --local                          # 项目里的全部本地源组件
```

## `brickkit publish`

把组件发布到组件市场（发布到 Git 是另一条命令 `release`）。目前没有 BrickKit 官方运营的公共市场，市场地址指向你或你的组织
自己部署的实例。

1. 检查登录状态（`.brickkit/credentials` 或安装源的 `authToken`），未登录报错；
2. 读取组件目录中的 `component.yaml` 并校验；
3. 检查镜像引用是否有效，并确认 `artifacts` 声明的文件都在；
4. 建 draft 版本 → 上传产物 → 转 stable；
5. 设置可见性。

分三步是有意的：版本转 stable 时市场会校验"文件与 `artifacts` 声明一致"，先建 draft 才能保证不会出现"已 stable 但文件没传齐"的半成品。
`--path` 也可以指向归档目录，例如 `./components/.archived/erp/backend`。

```text
brickkit publish [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--path <目录>` | 组件源码目录（含 `component.yaml`），缺省当前目录 |
| `--market <地址>` | 市场地址，缺省取 `brickkit.yaml` 中的 market 安装源 |
| `--visibility <public\|private>` | 可见性，缺省沿用市场侧设置 |
| `--changelog <文字>` | 本次版本的更新说明 |
| `--source-type <git\|registry>` | 来源类型：`git`（开源）或 `registry`（闭源），缺省按组件目录的 git remote 推断 |
| `--git-url <地址>` | 开源组件的 Git 仓库地址，缺省取组件目录的 origin |
| `--sign` | 用 cosign 对组件签名后发布 |
| `--key <路径>` | cosign 私钥路径，缺省 `cosign.key` |
| `--public-key-ref <ref>` | 写进签名的公钥 ref，缺省按 `--key` 的 `.key` → `.pub` 推导 |
| `--signed-by <标识>` | 签名者标识，如 `release-bot@example.com` |
| `--no-pin-digest` | 不把镜像 tag 钉成 digest（缺省会钉；跳过后 registry 上换掉同名 tag 时签名照样有效） |

```bash
brickkit publish --path ./components/people/basic
brickkit publish --path ./components/people/basic --visibility private
brickkit publish --path ./components/people/basic --changelog "新增人员状态字段"
brickkit publish --path ./components/people/basic --sign --key cosign.key --signed-by release-bot@example.com
```

## `brickkit login`

登录组件市场：终端输入用户名与密码（隐藏输入），调用市场 API 验证，成功后把 Token 写入 `.brickkit/credentials`（权限 0600）。
市场地址取自 `brickkit.yaml` 中类型为 market 的安装源；配了多个时用 `--market` 指定。

Token 优先级：`.brickkit/credentials` 高于安装源的 `authToken`。每次使用 Token 前检查过期时间，过期则提示重新登录（不做自动刷新）。

```text
brickkit login [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--market <地址>` | 市场地址，缺省取 `brickkit.yaml` 中的 market 安装源 |
| `--username <用户名>` | 用户名，不指定则交互式输入 |
| `--password-stdin` | 从标准输入读密码（CI 用，不回显也不进历史） |

```bash
brickkit login
brickkit login --market https://market.example.com/api/v1
echo "$PASSWORD" | brickkit login --username ci-bot --password-stdin
```

## `brickkit logout`

退出组件市场的登录，做两件事：调市场的 `POST /auth/logout` 作废这个 Token（服务端那一侧），删掉 `.brickkit/credentials`（本地那一侧）。

**本地那一份一定会删**，即使市场连不上——否则一次网络抖动就让人以为自己已经退出了，而凭据还躺在盘上。市场不可达时只警告一句，
并说明那个 Token 仍然有效到过期为止。没登录时什么都不做，也不算失败。

```text
brickkit logout [flags]
```

| 参数 | 说明 |
| --- | --- |
| `--keep-remote` | 只删本地凭据，不调市场作废 Token（离线时用） |

```bash
brickkit logout
brickkit logout --keep-remote
```

## `brickkit version`

查看 CLI 版本、支持的 Manifest 版本（`brickkit/v1`）与部署目标（`docker`、`podman`、`k8s`）。

```text
brickkit version [flags]
```

| 参数 | 说明 |
| --- | --- |
| `-v, --verbose` | 额外输出 Git commit 与构建时间 |

## `brickkit lang`

查看 CLI 当前的显示语言，以及它从哪里来。**CLI 缺省说英语。** 语言按这个顺序决定：环境变量 `BRICKKIT_LANG`，
其次 `brickkit lang set` 存在用户配置里的值，最后是英语。刻意没有 `--lang` 参数：语言必须在命令树（包括 `--help`）
建立之前就确定。给人看的文字都跟着它走，包括 JSON 日志行的 `message` 和生成文件里的注释；`error_code`、命令名、参数名不变。

```text
brickkit lang [flags]
brickkit lang <命令> [参数]
```

```text
当前语言：zh（来源：BRICKKIT_LANG 环境变量）
```

### `brickkit lang set`

设置 CLI 的显示语言（`en` 或 `zh`），存进用户配置，此后每条命令都用它；`BRICKKIT_LANG` 仍然可以单次覆盖。没有自己的参数。

```text
brickkit lang set <en|zh> [flags]
```

```bash
brickkit lang set zh                 # 从此说中文
BRICKKIT_LANG=en brickkit status     # 只对这一条命令说英文
```

---

## 常用命令速查

```bash
# 项目生命周期
brickkit init my-shop
brickkit add erp/backend@1.0.0
brickkit build
brickkit up

# 多环境
brickkit up -f deploy.prod.yaml

# 本地调试
brickkit local on
brickkit up
brickkit local refresh      # 团队改了 deploy.yaml 之后
brickkit local off

# 升级
brickkit upgrade --dry-run
brickkit upgrade erp/backend@2.0.0

# 检查与诊断
brickkit lint --strict
brickkit deps erp/backend
brickkit graph > graph.mmd
brickkit up --dry-run

# 发布
brickkit release --path ./components/erp/backend

# 只取契约（跨项目）
brickkit fetch infra/notifier@1.0.0
```

## 与三层文件重构之前相比的变化

从前的项目只有一份 `brickkit.yaml`，外加一份个人的 `override.yaml`。三层文件取代了它们，命令也跟着变了：

| 变化 | 命令 / 参数 | 说明 |
| --- | --- | --- |
| 已删除 | `brickkit override` | 已删除：个人覆盖改成整份的 `deploy.local.yaml`，由 `brickkit local` 管理 |
| 已删除 | `--config` | 已删除：多环境改成每个环境一份部署文件，用 `-f` 选；`brickkit.yaml` 只有一份 |
| 已删除 | `up --context` / `down --context` | 已删除：要部到哪个集群写在部署文件的 `k8s.context` 里，换集群就换一份部署文件，所见即所得 |
| 改名 | `--ignore-served-by` → `--ignore-shells` | 旧参数已删除：`servedBy` 换成了外壳（`kind: shell`）与部署文件里的 `members` |
| 新增 | `local` | 子命令 `on` / `off` / `status` / `refresh` |
| 新增 | `upgrade` | 移动默认版本，承载配置迁移与冲突处理 |
| 新增 | `deps` | 依赖树 |
| 新增 | `build` | 显式构建本机镜像；`up` 从不构建 |
| 新增 | `release` | 打 Git tag 发布；发布到市场的 `publish` 保留 |
| 新增 | `-f, --file` / `--no-local` | 读部署文件的命令选文件、忽略本地模式 |
| 扩展 | `init` | 不带参数时是补全式；新增 `--name`、`--yes` |
| 扩展 | `add` | 新增 `--local --init`；一次写好三层文件 |
