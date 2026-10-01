# BrickKit（中文版）

> 如果你是 AI 助手，这是你理解整个平台的唯一入口。读完这一份文件，你就知道该去哪里找什么、
> 不该读什么、以及这个平台的设计边界在哪里。
>
> 用户用英文（或其它语言）提问时，改读 [`AGENTS.md`](AGENTS.md)——内容对等的独立英文版，不是译本。
>
> 这份文件里的路径都相对仓库根目录；在网页上读，前面加 https://raw.githubusercontent.com/brickKit/brickKit/main/ 。
> 想用最少的抓取读完全部文档，从 [`llms/zh/00-core.md`](llms/zh/00-core.md) 开始，顺着"下一份"往下读。

## §1 平台定位

BrickKit 是一个声明式的组件组装平台：你声明要哪些组件、它们依赖谁，CLI 派生其余的一切——
启动顺序、服务地址、环境变量、部署文件——然后交给 Docker（或 Podman）、Kubernetes 执行，自己退出。

项目由三层文件描述，每层只管一件事：

| 层 | 文件 | 管什么 |
| --- | --- | --- |
| 声明 | `brickkit.yaml` | 有哪些组件、什么精确版本、谁是外壳；它就是锁文件 |
| 部署 | `deploy.yaml`（团队）/ `deploy.local.yaml`（个人，不进 Git） | 怎么跑：目标、端口、模式、外壳成员、K8s 设置 |
| 配置 | `config/<组件>.yaml`、`config/vars.yaml` | 每个组件拿到哪些环境变量 |

没有注册中心、没有配置中心、没有网关、没有常驻进程：CLI 跑完就退出，服务发现就是 Docker / K8s 的 DNS。

## §2 核心设计原则（十条）

1. **大声失败**：配置冲突、必填缺失、重复 key、推送失败，都在操作的那一刻报出来，不留到运行时才暴露。
2. **平台极简**：平台只做机械操作，不猜语义——不猜重命名、不做类型转换、不替组件决定降级。
3. **所见即所得**：没有隐式覆盖，你打开的那份文件就是实际生效的配置。
4. **组件自治**：组件需要什么配置、怎么降级、日志长什么样，由组件自己决定；平台只要 Manifest、健康检查和环境变量。
5. **职责单一**：三层文件各管一件事，同一个事实只写在一个地方。
6. **原子性操作**：`add`、`remove`、`upgrade`、`release` 要么完全成功，要么回到原样，不留半成品。
7. **显式优于隐式**：精确版本、`$var:` 显式引用、显式暴露端口；拒绝"自动猜"。
8. **概念一致性**：一个组件的项目和五十个组件的项目用同一套三层文件，没有"单文件模式"。
9. **构建与部署分离**：构建镜像是你的显式动作（`brickkit build`），`brickkit up` 绝不自动构建。
10. **按需检索优于全量合并**：不提供"把三层合成一份"的视图命令；要知道什么，就去读负责它的那一层。

每一条的论证（是什么、换来什么、代价是什么、拒绝了什么）见
[`docs/zh/06-architecture/05-design-principles.md`](docs/zh/06-architecture/05-design-principles.md)。

## §3 三层文件架构

### 3.1 文件检索地图

| 想知道什么 | 读哪个文件 |
| --- | --- |
| 项目有哪些组件、什么版本 | `brickkit.yaml` |
| 组件怎么部署（目标、端口、模式、外壳成员） | 本地模式开着时读 `deploy.local.yaml`，否则读 `deploy.yaml`（`-f` 指定的文件优先） |
| 组件拿到的环境变量（连接串、密钥、开关） | `config/<组件 ID 把 / 换成 ->.yaml`，带版本号的 `config/<…>@<版本>.yaml` 优先 |
| 公共变量 | `config/vars.yaml`（部署文件的 `vars:` 可以覆盖同名项） |
| 组件的依赖、能力声明、`configSchema` | `.brickkit/manifests/<scope>/<name>/<版本>/component.yaml`，本地源组件读源码里的 `component.yaml` |
| 依赖组件怎么用、配置项什么意思 | `.brickkit/manifests/<scope>/<name>/<版本>/BRICKKIT.md` |
| 项目里有哪些组件、文档在哪 | 项目根目录的 `BRICKKIT.md` |

### 3.2 关键规则

- `brickkit.yaml` 是锁文件：用到的每个组件版本都写在这里。没声明的强依赖是错误（报错时给出 `add` 的写法），没声明的弱依赖就是不存在。
- `deploy.local.yaml` 是**完整替换**，不是属性覆盖：本地模式开着时，运行或检查部署的命令（`up`、`down`、`status`、`sync`、`lint`、`build`）只读它、不读 `deploy.yaml`；`graph` 与 `deps` 始终读 `deploy.yaml`，输出对谁都一样。它必须与 `brickkit.yaml` 一一对应，团队加了组件就要 `brickkit local refresh`。
- `mode: debug` 只写在 `deploy.local.yaml`：那是"我正在自己机器上调它"这个个人事实，不进 Git。
- `focus: <id>` 只写在 `deploy.local.yaml`——在组件目录里 `brickkit up` 或 `up --focus <id>` 会写上它，`up --all` 去掉它。写了它，就只启动这个组件（从源码跑）和它需要的组件；`sync` 不看它，`target: k8s` 下用不了。
- 项目命令在任何子目录里都能用：往上找到最近的 `brickkit.yaml`（像 `git` 一样，不停在 `.git`），找到上面的就说一句 `📁 项目：…`；打印的路径都相对你所在的目录。`release`、`publish`、`init`、`skills` 作用于当前目录。
- `$var:NAME` 从 `config/vars.yaml`（或部署文件的 `vars:`）取值；`${NAME}` 从进程环境与 `.env` 取值；`file://路径` 读文件。没有隐式的环境变量覆盖。
- `brickkit up` 绝不构建镜像：本地该有的镜像没有时，它报错并告诉你跑 `brickkit build`。
- `brickkit init <名字>` 新建目录；不带名字的 `brickkit init` 在当前目录补全缺的文件，已有的一个字节都不动，`.gitignore` 缺必需条目时大声警告。

### 3.3 分形架构（套娃机制）

- **开发态**：组件仓库自己就可以是一个完整的 BrickKit 项目（有自己的三层文件，用来本地联调）。
- **消费态**：使用它的项目只读它的 `component.yaml`（契约）和 `BRICKKIT.md`（文档）。
- **物理不套娃，规范套娃**：子组件的三层文件不会被带进父项目，只有契约和文档会。
- `brickkit add` 时把组件的 `BRICKKIT.md` 永久缓存到 `.brickkit/manifests/` 下。
- **项目里的组件用焦点运行，独立的组件用工作台。** 焦点运行不需要任何自己的文件；工作台是组件仓库自己的 `brickkit.yaml`，最近的 `brickkit.yaml` 永远说了算。
- **只有一个 `components/`**：组件源码只放在项目的 `components/` 里。嵌在另一个组件目录里的组件，`up`、`lint`、`sync` 都拒绝，也从不替你挪；`add --repo` 总是克隆到项目的 `components/`。git submodule 从不拉取。

### 3.4 AI 的三层路由

1. 读本文件，理解规范和检索路径。
2. 读项目根目录的 `BRICKKIT.md`，知道项目里有哪些组件、各自的文档在哪。
3. 按需读某个组件的 `BRICKKIT.md`（`.brickkit/manifests/` 下），理解那一个组件。

不要一次把所有组件文档读进来：问题只涉及哪个组件，就只读哪个。

## §4 外壳机制速查

- 外壳是一个普通组件，把多个成员组件编进**一个进程**里跑（1:N），用来省内存与 CPU。
- 成员写在部署文件里外壳条目的 `members` 下面（唯一来源）；`brickkit.yaml` 里外壳那一行带 `kind: shell`，由 CLI 维护。
- 外壳的 `component.yaml` 在 `shell.members` 里写明它编进了哪些成员的**精确版本**；项目托管的版本与之不符时报错，并给出三条出路。
- 成员的配置经 `BRICKKIT_SERVED_MEMBERS_CONFIG`（CLI 提前求好值的 JSON）注入外壳；多行密钥（PEM）、引号、`$` 照样能 JSON 编码，不是合法 UTF-8 的值（二进制）大声失败。
- 调用方的 `*_ENDPOINT` 由平台自动指到外壳的地址；外壳作者只负责进程内把请求分给对应成员。
- 成员的迁移照常单独跑，用成员自己的镜像与配置，所以每个成员都必须有自己的镜像（`image` 或 `build`）。
- 外壳本身也是组件：有自己的 `configSchema` 和 `config/` 文件。

## §5 命令集（21 个 + version + lang + completion）

| 命令 | 核心行为 |
| --- | --- |
| `init` | 带名字：新建目录并生成三层骨架；不带名字：在当前目录补全缺的文件 |
| `skills` | 查看、刷新装进项目的 AI 助手技能（`status` / `update`） |
| `graph` | 依赖拓扑，输出 Mermaid |
| `lint` | 离线、只读地检查三层文件与 `component.yaml` |
| `new` | 组件骨架（`--shell` 生成外壳） |
| `add` | 拉取组件与依赖，写入三层文件 |
| `remove` | 移除组件，配置移进 `config/.archive/` |
| `fetch` | 只下载组件的产物（契约），不装进项目 |
| `upgrade` | 升级版本，按新旧 `configSchema` 迁移配置，冲突处写重复 key 大声失败 |
| `up` | 生成部署文件 → 跑迁移 → 起容器（绝不自动构建）；在组件目录里或带 `--focus` 时，只起这个组件和它需要的 |
| `down` | 停止容器（不删 volume） |
| `status` | 运行状态表 |
| `sync` | 整理组件源码工作区：这次用不上的收进 `.archived/`，要用的放回来 |
| `local` | 本地模式：`on` / `off` / `status` / `refresh` |
| `restore` | 把 `mode` 与源码结构还原到最后一次提交 |
| `deps` | 依赖树 |
| `build` | 显式构建需要在本机构建的镜像（`--force`） |
| `release` | 校验 → 打 Git tag → 推送，推送失败就删掉 tag；`--local` 批量发布 |
| `publish` | 发布到组件市场（可选的基础设施） |
| `login` | 登录组件市场 |
| `logout` | 退出组件市场（吊销令牌、删本地凭据） |

`version` 打印版本；`lang` 查看或设置 CLI 的界面语言（`lang set zh|en`）；`completion` 打印某种 shell 的 TAB 补全脚本（`install.sh` 会给 bash、zsh、fish 装好）。

### 参数

- 唯一的全局参数是 `--log-level`（stderr 上 JSON 日志的级别，默认 `warn`）。
- `-f, --file <路径>`：读部署文件的命令（`up`、`down`、`status`、`sync`、`lint`、`graph`）用它指定一份部署文件，同时完全忽略 `deploy.local.yaml` 与本地模式开关。
- `--no-local`：`up`、`down`、`status`、`sync`、`lint` 本次忽略 `deploy.local.yaml`，不改变本地模式开关。
- `--dry-run`：`up` 只生成部署文件不执行；`upgrade` 只演算不写盘。
- `--focus <id>` / `--all`：`up` 在 `deploy.local.yaml` 里设上或去掉焦点；两者都不能和 `-f`、`--no-local` 一起用。
- 在组件目录里不带参数：`build` 和 `deps` 指的就是这个组件。

每条命令的全部参数见 [`docs/zh/07-cli-reference/README.md`](docs/zh/07-cli-reference/README.md)。

### 已经删掉的命令与参数

| 删掉的 | 现在怎么做 |
| --- | --- |
| `override` 命令与 `override.yaml` | `deploy.local.yaml` + `brickkit local` |
| `brickkit.yaml` 里的 `resources`、`servedBy`、`deploy:` | 连接信息进 `config/`，外壳成员进部署文件的 `members`，部署设置进 `deploy.yaml` |
| `--config` | 每个环境一份部署文件，`-f` 指定 |
| `up --context` | 在部署文件的 `k8s.context` 里写，不同集群用不同部署文件 |

## §6 "不做"清单

| 不做 | 理由 |
| --- | --- |
| 服务注册 / 发现 | Docker / K8s 的 DNS 就是服务发现 |
| 常驻守护进程 | CLI 跑完就退出，状态在文件和底层引擎里 |
| 健康检查轮询 | K8s 探针与 Compose healthcheck 原生就做 |
| API 网关、服务网格 | 组件之间直接按 DNS 调用；外部网关用 `labels` 透传接入 |
| 配置中心 / 热更新 | 改 `config/` 或部署文件，再 `up` |
| 熔断、限流、重试、降级 | 组件自己的业务逻辑 |
| 版本范围（`^1.0.0`） | 精确版本才是契约 |
| 多环境覆盖继承 | 每个环境一份完整的部署文件 |
| 自动构建镜像 | 构建是你的显式动作 |
| 合并视图命令 | 按需检索优于全量合并 |
| 单文件模式 | 概念一致，永远是三层 |
| 隐式配置覆盖 | 所见即所得 |
| Git 鉴权 | 用宿主机自己的 Git 配置，出错原样透传 |
| 替你校验配置值的类型与范围 | `configSchema` 是说明书，只检查键名，不检查值 |

## §7 配置迁移与冲突处理

- 升级就是对比新旧版本的 `configSchema`，按键集合机械地迁移你的配置。
- 你改过、开发者也改了默认值的键，写成重复 key 并加注释，`up` 在你解决之前拒绝启动（大声失败）。
- `remove` 时配置归档进 `config/.archive/`，重新 `add` 时走同一套迁移算法恢复。
- 不猜重命名、不做类型转换、不做语义分析。

## §8 无市场分发

- 组件就是一个 Git 仓库，版本就是 Git tag（monorepo 子目录里的组件用 `<scope>-<name>/<版本>`）。
- 拉取走用户级的 bare repo 缓存（`<用户缓存目录>/brickkit/repos`，多个项目共享），按需增量 fetch。
- 读 Manifest 的顺序：项目的永久缓存 `.brickkit/manifests/` → 本机 bare repo 里直接读 → 增量 fetch → 首次 clone。
- CLI 不处理鉴权，`git` 的报错原样透传。
- `brickkit release`：校验 → 打 tag → 推送，推送失败删掉 tag；`--local` 批量发布本地源里的组件，遇到第一个失败就停。
- 组件市场（`publish` / `login` / `logout`，`sources[].type: market`）是可选的基础设施，与 Git 源并存。

## §9 文档地图

要知道什么 → 读哪一页。逐页、每页一行说明的完整列表在 [`llms.zh.txt`](llms.zh.txt)。

| 要知道什么 | 读 |
| --- | --- |
| 一次读懂整个平台 | [`llms/zh/00-core.md`](llms/zh/00-core.md)：这份文件加上几篇核心页 |
| 先跑起来 | [`docs/zh/00-intro/02-quick-start.md`](docs/zh/00-intro/02-quick-start.md) |
| 术语与概念 | [`docs/zh/00-intro/04-core-concepts.md`](docs/zh/00-intro/04-core-concepts.md) |
| 三份文件、每个字段 | [`docs/zh/01-three-layers/README.md`](docs/zh/01-three-layers/README.md)、[`docs/zh/01-three-layers/09-field-reference.md`](docs/zh/01-three-layers/09-field-reference.md)、[`docs/zh/11-reference/README.md`](docs/zh/11-reference/README.md) |
| 一个配置值从哪来 | [`docs/zh/01-three-layers/08-resolution-priority.md`](docs/zh/01-three-layers/08-resolution-priority.md) |
| 运行项目：init、add、local、up、upgrade…… | [`docs/zh/02-project-guide/README.md`](docs/zh/02-project-guide/README.md) |
| 在项目里只改一个组件 | [`docs/zh/02-project-guide/04-focus-run.md`](docs/zh/02-project-guide/04-focus-run.md) |
| 编写组件 | [`docs/zh/03-component-guide/README.md`](docs/zh/03-component-guide/README.md) |
| 外壳 | [`docs/zh/04-shell/README.md`](docs/zh/04-shell/README.md) |
| 数据库迁移 | [`docs/zh/05-migration/README.md`](docs/zh/05-migration/README.md) |
| 架构、设计原则、环境变量契约、错误码 | [`docs/zh/06-architecture/README.md`](docs/zh/06-architecture/README.md)、[`docs/zh/06-architecture/05-design-principles.md`](docs/zh/06-architecture/05-design-principles.md)、[`docs/zh/06-architecture/03-env-injection-contract.md`](docs/zh/06-architecture/03-env-injection-contract.md)、[`docs/zh/06-architecture/09-error-codes.md`](docs/zh/06-architecture/09-error-codes.md) |
| 每条命令、每个参数 | [`docs/zh/07-cli-reference/README.md`](docs/zh/07-cli-reference/README.md) |
| AI 怎么配合 BrickKit 工作 | [`docs/zh/08-ai-guide/README.md`](docs/zh/08-ai-guide/README.md) |
| 推荐做法 | [`docs/zh/09-patterns/README.md`](docs/zh/09-patterns/README.md) |
| 出错了 | [`docs/zh/10-troubleshooting/README.md`](docs/zh/10-troubleshooting/README.md) |
| 贡献者的构建、测试与约定 | [`CONTRIBUTING.zh.md`](CONTRIBUTING.zh.md) |
| 某个功能的代码在哪 | 见下面 §10 |

## §10 代码地图

一个 Go 模块：`github.com/brickkit/brickkit`。CLI 的入口在 `cmd/brickkit/`；每条命令一个文件，`internal/cli/<命令>.go`。

### 包

| 包 | 管什么 |
| --- | --- |
| `internal/cli/` | 命令树：每条命令一个文件、参数、输出；报错里的路径按使用者所在的目录显示（`shown.go`）；TAB 补全的候选（`complete.go`） |
| `internal/project/` | 把一个项目（brickkit.yaml + 部署文件 + config/）装载成一个 `Project`；往上找项目根（`findroot.go`）；一致性检查；项目 `AGENTS.md` 末尾的组件表（`agents.go`） |
| `internal/project/projecttest/` | 测试辅助：用几行 YAML 搭一个三层项目并装载它 |
| `internal/projfile/` | `brickkit.yaml`：安装源、组件、版本、`kind: shell` |
| `internal/deployfile/` | 部署文件（`deploy.yaml`、`deploy.local.yaml`、`-f`）：字段、校验、`focus`、本地修改的差异 |
| `internal/configdir/` | `config/`：每个组件的环境变量文件、`vars.yaml`、取值、骨架、升级时的配置迁移 |
| `internal/manifest/` | `component.yaml`：解析、校验，以及 `brickkit new` 写出的骨架 |
| `internal/resolver/` | 依赖解析成图；启动顺序 |
| `internal/cascade/` | 这次到底跑哪些组件：`mode`、"跟着上层走"、焦点的可达性、外壳承载 |
| `internal/inject/` | 每个组件的环境变量：依赖地址、配置、资源配额 |
| `internal/shell/` | 外壳分组与成员的 JSON 配置 |
| `internal/compose/` | 给 docker / podman 渲染 `compose.yaml` |
| `internal/k8s/` | 渲染 Kubernetes 清单 |
| `internal/deploy/` | 两种部署目标共用的命名规则与文件头注释 |
| `internal/docspec/` | 组件与项目文档规范本身（写成数据）：文件、小节、各语言的标题、译本的文件名 |
| `internal/agentsmd/` | `AGENTS.md` 末尾由 CLI 维护的那一段（平台规则、组件表、记下的语言），以及 `AGENTS.md` / `CLAUDE.md` 的骨架 |
| `internal/engine/` | Docker、Podman、kubectl：探测与调用 |
| `internal/procsup/` | 在前台监管 `mode: local` 的本机进程 |
| `internal/runcmd/` | 从组件源码推出在本机怎么启动它 |
| `internal/sessionlock/` | 一个项目同一时刻只有一个前台本地会话 |
| `internal/install/` | `add` / `remove` / `upgrade` 要对三份文件做哪些改动 |
| `internal/source/` | 安装源（local / git / market）、Manifest 与产物缓存、bare 仓库缓存、不联网可知的版本 |
| `internal/source/gittest/` | 测试辅助：真实的 git "远端"——本地 bare 仓库，按版本打 tag，经 `file://` 访问 |
| `internal/gitrepo/` | 对 git 仓库的只读查询（状态、submodule） |
| `internal/workspace/` | `components/` 下的组件源码：归档、激活、删除风险 |
| `internal/release/` | `brickkit release`：检查、打 tag、推送、回滚 |
| `internal/market/` | 市场客户端：登录与发布 |
| `internal/security/` | 组件签名：签与验 |
| `internal/skills/` | 装进用户项目的 AI 助手技能（`assets/`） |
| `internal/clierr/` | 错误类型、错误码与报错的样子 |
| `internal/i18n/` | 消息目录（`locales/en.yaml`、`locales/zh.yaml`）与语言判定 |
| `internal/msgid/` | 消息的 key（`messages_gen.go` 是生成的） |
| `internal/msgid/msgidgen/` | 从英文目录生成 `messages_gen.go`（只在构建期用：`cmd/gen-msgid`、检查） |
| `internal/logging/` | stderr 上的 JSON 日志行 |
| `internal/suggest/` | "你是不是想写"的候选 |
| `internal/envref/` | `${VAR}` 引用 |
| `internal/yamlfile/` | 三份文件共用的读取 / 解析 / 解码流水线 |
| `internal/yamlcheck/` | YAML 里拼错或多余的字段 |
| `internal/yamlcomment/` | 生成的 YAML、`.env`、`.gitignore` 里的注释块 |
| `internal/schemagen/` | 从 Go 结构体生成 JSON Schema 到 `schemas/` |
| `internal/userconfig/` | 机器级的偏好（CLI 的显示语言） |
| `internal/version/` | 版本与能力常量 |
| `internal/llmsgen/` | `llms/` 下的文档合集与 `llms*.txt` 里的合集清单 |
| `internal/mdtext/` | 文档合集与 lint 文档检查共用的 Markdown 扫描：代码块围栏、链接、小节、表格单元格 |
| `cmd/brickkit/` | CLI 的 `main` |
| `cmd/gen-msgid/` | 生成 `internal/msgid/messages_gen.go` |
| `cmd/gen-schemas/` | 生成 `schemas/*.json` |
| `cmd/gen-llms/` | 生成 `llms/` |

其他地方：`market-server/`（可选的组件市场，独立的 Go 模块）、`tools/i18n/`（多语言迁移时的一次性脚本）、`scripts/`（lint 检查、安装检查、发布）、`install.sh`、`.githooks/`（提交钩子）。

### 功能 → 代码

| 功能 | 从这里开始 | 接着看 |
| --- | --- | --- |
| `up` | `internal/cli/up.go`（`up_local.go`、`up_k8s.go`、`up_upgrade.go`） | `cascade`、`inject`、`compose`、`k8s`、`engine`、`procsup` |
| `down`、`status` | `internal/cli/down.go`、`internal/cli/status.go`、`internal/cli/lifecycle.go` | `engine`、`sessionlock` |
| `add`、`remove`、`upgrade` | `internal/cli/add.go`、`internal/cli/remove.go`、`internal/cli/upgrade.go`、`internal/cli/install_apply.go` | `install`、`configdir`、`source` |
| `fetch` | `internal/cli/fetch.go`、`internal/cli/artifacts.go` | `source` |
| `build` | `internal/cli/build.go` | `source`、`engine`、`gitrepo` |
| `lint` | `internal/cli/lint.go`、`internal/cli/lint_config.go` | `project`、`yamlcheck` |
| `graph`、`deps` | `internal/cli/graph.go`、`internal/cli/deps.go`、`internal/cli/topology.go` | `resolver`、`cascade` |
| `sync`、`restore` | `internal/cli/sync.go`、`internal/cli/restore.go`、`internal/cli/restore_check.go` | `workspace` |
| `local` | `internal/cli/local.go` | `deployfile` |
| `init`、`new` | `internal/cli/init.go`、`internal/cli/new.go`、`internal/cli/hooks.go` | `project`、`manifest`、`skills` |
| `release`、`publish`、`login`、`logout` | `internal/cli/release.go`、`internal/cli/publish*.go`、`internal/cli/login.go`、`internal/cli/logout.go` | `release`、`market`、`security` |
| `skills`、`lang`、`version` | `internal/cli/skills.go`、`internal/cli/lang.go`、`internal/cli/version.go` | `skills`、`i18n`、`userconfig` |
| 往上找项目根 | `internal/project/findroot.go`、`internal/cli/root.go` | |
| 焦点运行 | `internal/cli/focus.go` | `internal/cascade/cascade.go`、`internal/deployfile/focus.go` |
| 只有一个 `components/`（嵌套副本） | `internal/cli/nested.go` | `internal/project/nested.go` |
| TAB 补全 | `internal/cli/complete.go` | `internal/source/cached.go`、`install.sh` |
| "你是不是想写" | `internal/cli/didyoumean.go` | `suggest` |
| 报错长什么样 | `internal/clierr/clierr.go`、`internal/cli/shown.go` | |
| 加一条消息 | `internal/i18n/locales/en.yaml`、`internal/i18n/locales/zh.yaml`，再跑 `make generate-msgid` | `msgid` |
| 文档合集 | `cmd/gen-llms/`、`internal/llmsgen/` | `.githooks/pre-commit`、`make generate-llms` |

### 测试与检查

- 单元测试就在代码旁边（`*_test.go`）；CLI 的测试在进程内运行命令，夹具在 `internal/cli/testdata/`。
- `tests/components/`：真实的组件（Go、Python、nginx），当作夹具用；`tests/checklist/`：`make test-all` 跑的回归清单；`tests/docfields/`：保证文档与代码一致的守卫。
- `make lint`（全部静态检查，`scripts/check-*.py`）和 `make test-all`；每个克隆运行一次 `make hooks`。
