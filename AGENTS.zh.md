# BrickKit · AI 导读

[English](AGENTS.md) | 中文

> **这份文件是写给 AI 助手的。**
>
> 如果有人把 `https://github.com/brickKit/brickKit` 丢给你、让你了解或讨论这个项目，
> 读完这一份就够了——它是全平台核心理念与做法的压缩件，覆盖 BrickKit 的**全部核心理念与做法**。
>
> 读完之后你应该能做到：用用户提问所用的语言和他讨论这个项目的任何设计、判断某个提议是否
> 符合它的哲学、在需要细节时知道该去抓哪一份文档（见文末第 11 节）。
>
> 讨论时请沿用本文的中文术语（组件 / 强依赖 / 版本化服务名 / 跟着上层走……）——这是全平台
> 通用的概念名字，不因为用户用英文提问就换一套说法；英文文档里这些术语对应的英文写法见
> `docs/en/` 各篇自己的术语表。

> **语言路由：按用户提问的语言选文档，不是按这份文件的语言选。** 这份 `AGENTS.zh.md` 与
> 英文版 `AGENTS.md` 内容对等，都是独立完整的一份文件，不是互为翻译附属——用户用中文提问，
> 读这一份；用户用英文（或其他语言）提问，去读 `AGENTS.md`。涉及具体细节、需要深挖某个机制
> 时——中文继续深挖去读 `docs/zh/` 下对应路径，英文深挖读 `docs/en/` 下对应路径。两棵树结构
> 完全镜像，路径公式是 `docs/{en,zh}/<同一相对路径>`（文件夹和文件名前面有表示阅读顺序的
> 编号，如 `06-architecture/00-overview.md`，两棵树完全一样）。第 11 节的表格默认给中文
> 路径，把 `zh` 换成 `en` 就是对应的英文版。

---

## 1. 一句话定位

**BrickKit 是一个声明式的组件管理与拼装平台：声明组件和它们的依赖，其余由它派生。像搭积木一样构建系统。**

每块积木（组件）独立制造、独立测试、独立部署、独立调用。BrickKit CLI 负责把积木拉来、
排好顺序、生成图纸、交给 Docker 或 K8s 装好。剩下的，全是组件自己的事。

它**不是**操作系统，不是 ERP，不是任何一个具体的业务软件。它是让你**渐进式**长出架构的工具：
先写一个小组件跑通，再写一个跑通，然后写一个连接组件把它们串起来——最终拼出任何系统。

### 1.1 类比（帮助快速定位）

| BrickKit | 大致相当于 |
| --- | --- |
| BrickKit CLI | `npm` + `helm` + `docker compose` + `git clone`，但面向**业务组件** |
| BrickKit Market（组件市场） | npmjs.com / Docker Hub / App Store |
| Component（组件） | npm package / Docker image |
| `component.yaml`（Manifest） | `package.json` |
| `brickkit.yaml`（项目配置） | `docker-compose.yaml` 的"声明式输入" |
| `brickkit add` | `npm install` |
| `brickkit up` | `docker compose up -d` / `kubectl apply` |

**但有一个根本区别：** npm 装的是代码库，BrickKit 装的是**能独立跑起来的业务服务**。
所以它同时要管依赖解析、部署文件生成、地址注入、数据库迁移、启动顺序。

### 1.2 五个核心价值

| 价值 | 说明 |
| --- | --- |
| 渐进式构建 | 不需要一次性设计完整系统，一块积木一块积木地加 |
| 语言无关 | 任何语言只要能构建 Docker 镜像，就能成为组件 |
| 环境一致 | 本地（Docker）与生产（K8s）使用**同一套地址格式**，组件代码零修改 |
| 极简平台 | 平台不越界：业务逻辑、通信治理、多租户全部交给组件 |
| 开源优先 | CLI 与市场均开源，闭源组件通过市场受控分发 |

---

## 2. 系统组成与关键架构事实

### 2.1 四个部分

| 部分 | 形态 | 是否常驻 | 职责 |
| --- | --- | --- | --- |
| **BrickKit CLI** | 本地单二进制 | ❌ 用完即走 | 拉取、解析、生成、调用、发布、源码工作区管理 |
| **BrickKit Market** | 独立 SaaS（可私有化） | ✅ | 组件发布与发现、版本/可见性/签名、产物存储 |
| **组件层** | Docker 容器 / K8s Pod | ✅ | 业务本体，组件间 DNS 直连 |
| **基础设施层** | PostgreSQL / Redis 等 | ✅ | **运维手动部署**，在 `brickkit.yaml` 中声明绑定 |

### 2.2 五条容易被误解的架构事实

这五条是理解 BrickKit 的关键。它们都是**刻意的取舍**，不是"还没做"：

1. **没有常驻的"主系统服务"。** CLI 执行完命令就退出。期望状态由 `brickkit.yaml` 持有，
   实际状态由底层引擎（Docker / K8s）持有。没有后台进程、不监听端口、无攻击面。
2. **没有自建注册中心。** 服务发现 = Docker Compose service DNS / K8s Service DNS。
3. **没有平台轮询健康检查。** 健康检查由 K8s Probe / Compose healthcheck + 重启策略承担。
4. **CLI 不挂载 Docker Socket。** 它只是调用 `docker compose` / `kubectl` 命令行，权限边界清晰。
5. **所有组件都是 container。** 包括前端组件——用 nginx 等 Web Server 容器 serve 静态资源。
   平台里**没有**"无容器 / static"类型的组件。

### 2.3 数据流（一次 `brickkit up` 发生了什么）

```
brickkit.yaml（声明）+ override.yaml（可选，本地专属——最先合并进来，§7.1）
   ↓ ① 启停判定：算出这次实际该启动哪些组件（`mode` + 依赖图，跟着上层走）
   ↓ ② 依赖解析：递归展开依赖树，强依赖缺失报错，拓扑排序得出启动顺序
   ↓ ③ 环境变量注入：依赖地址、资源连接、自身配置 → 环境变量
   ↓ ④ 生成部署文件：docker-compose.yaml 或 K8s Deployment/Service/Ingress
   ↓ ⑤ 执行迁移：K8s Job / Docker 一次性 service，失败则阻断主服务
   ↓ ⑥ 调用底层引擎：docker compose up -d / kubectl apply
运行中的容器
```

对来自市场或 Git 源的组件，CLI 的 Manifest 来自 `.brickkit/manifests/` 缓存，不需要
`components/` 下的任何东西。由**本地**安装源提供的组件则不同：`init` 默认的 `local-dev`
指向 `./components/`，所以你用 `--repo` 克隆下来的、或直接写在那里的组件都算。它的
`component.yaml` 每次运行都直接从那个目录重读，不走缓存——改了它，下一次 `up` 就生效，
被 `sync` 归档的那份也照样找得到。（请求的版本和目录里放的不一样时，仍从缓存取；
多版本共存靠的就是这个。）不管哪种，`up` 读的都只是 Manifest，从不碰组件的代码——
源码目录里的其余部分只服务于开发（IDE、调试）。

---

## 3. 术语表

| 术语 | 英文 | 定义 |
| --- | --- | --- |
| 主系统 | BrickKit CLI | 本地命令行工具，无常驻进程 |
| 组件市场 | BrickKit Market | 公开的组件发布和发现平台 |
| 组件 | Component | 最基本的安装和运行单元，**全部是 container** |
| Manifest | component.yaml | 组件的自我描述文件 |
| 项目配置 | brickkit.yaml | 项目级声明：组件列表、启停、暴露、配置覆盖、资源、部署目标。共享、要评审、进 Git |
| 本地覆盖配置 | override.yaml | 可选、进 `.gitignore`、按开发者各自本地一份的文件，覆盖 `brickkit.yaml` 的 `deploy.target`（只许降级）和某个组件的 `mode`/`localPort`。`mode: debug` **只能**写在这里——`brickkit.yaml` 自身直接拒绝它（§5.4、§5.6） |
| 强依赖 | Required Dependency | 缺失时 CLI **报错并阻断启动** |
| 弱依赖 | Optional Dependency | `optional: true`；缺失时警告但继续，且**完全不注入该环境变量** |
| 版本化服务名 | Versioned Service Name | 带精确版本号的服务名，如 `people-basic-1-0-0` |
| 本地调试模式 | Local Debug Mode | `mode: debug`，写在 `override.yaml` 里；组件跑在宿主机 IDE 中，用 `extra_hosts` 映射进容器网络 |
| 本地托管模式 | Local Managed Mode | `mode: local`，写在 `brickkit.yaml` 里；BrickKit 自己探测启动命令，把组件跑成一个裸进程并监管它——不生成容器，也不需要 IDE |
| 安装源 | Source | 组件来源：市场（http）/ Git 仓库 / 本地目录 |
| 基础资源 | Resource | 组件依赖的外部系统（数据库、Redis 等），运维部署，`brickkit.yaml` 绑定 |
| 环境变量注入 | Env Injection | CLI 生成部署文件时写入依赖地址、资源连接、自身配置 |
| 部署目标 | Deploy Target | `brickkit.yaml` 里是 `docker` 或 `k8s`，决定 CLI 生成哪种部署文件。`override.yaml` 可以在本地把它往下降（k8s → docker/podman；docker ↔ podman 不受限；绝不会升回 k8s）——`podman` 本身是 `override.yaml` 独有的取值，`brickkit.yaml` 自己的 `deploy.target` 从不接受它 |
| 数据库迁移 | Migration | 组件声明 `migration.command`，CLI 在部署前执行 |
| 配置覆盖 | Config Override | `brickkit.yaml` 的 `config` 覆盖 configSchema 默认值。**不校验值的类型，但校验键名存不存在** |
| 连接组件 | Connector Component | 协调多个单一组件的编排组件 |
| 单一组件 | Standalone Component | 独立完成一个功能、内部事务自洽的组件 |
| 精确版本 | Exact Version | `major.minor.patch`，依赖声明**不接受** `^` / `~` 范围约束 |
| 跟着上层走 | Top-down Inheritance | 顶层默认跑；下层只要还有一个上层在跑就跑。写了 `mode` 就按写的来（5.4） |

---

## 4. 十二条设计原则（理念内核）

讨论任何设计提议时，用这十二条去判断它是否属于 BrickKit：

| 原则 | 说明 |
| --- | --- |
| **平台极简** | CLI 能不做的事就不做；每多一个功能就多一份维护成本 |
| **组件自治** | 语言、框架、API、事务、日志格式由组件自定；平台只要求 Manifest + 健康检查 + 环境变量 |
| **渐进式** | 不要求一次性设计完整系统；随时可停，随时可继续 |
| **环境无关** | 组件代码不感知自己跑在 Docker 还是 K8s |
| **精确优于隐式** | 精确版本、显式暴露、显式启用；拒绝一切"自动猜测"带来的不可控 |
| **安全默认** | 不映射端口 = 外部不可访问；不声明 expose = 无 Ingress；private = 未授权不可见 |
| **开源优先** | CLI 与市场开源；闭源组件通过市场受控分发 |
| **平台提供工具，不替人做决定** | 多版本默认共存；降级逻辑归组件；破坏性变更是组织协调问题 |
| **安装即信任** | 平台不做前置安全审查，只做事后 `blocked` 下架 |
| **configSchema 是说明书，不是安检机** | CLI 不校验使用者填的 config **值**（类型 / 枚举 / 范围）。但**键名**要查：写了 configSchema 里没有的配置项会警告 |
| **一个组件一个仓库** | 不支持 monorepo 拆子目录；组件是独立的发布 / 移动 / 权限单元 |
| **brickkit.yaml 就是声明** | 配置即意图。写了就执行，CLI 不反问"你确定吗" |

> 每条原则背后的论证——换来了什么、付出了什么、拒绝了什么——在
> [设计原则与取舍](docs/zh/06-architecture/01-design-principles.md)（英文版把 `zh` 换 `en`）。
> 那里的十二个小节标题，去掉编号后，与本表第一列逐字一致；两边一旦分叉、或编号错位，`make lint` 会失败。

### 4.1 平台明确**不做**的事（拒绝清单）

这份清单是"平台极简"的具体落地。**向用户提议时，不要建议 BrickKit 去做下面任何一项**——
它们都被明确论证过并拒绝了（理由见第 9 节）：

| 不做 | 替代方案 |
| --- | --- |
| 常驻服务 / 控制面 | CLI 用完即走，状态外置到 `brickkit.yaml` + 底层引擎 |
| 注册中心 / 地址簿 | Docker DNS / K8s Service DNS |
| 健康检查轮询 | K8s Probe / Compose healthcheck + 重启策略 |
| API 网关 / 服务网格 / 负载均衡 | 组件 DNS 直连；K8s Service 原生负载均衡。**带外部署的网关靠 `labels` 透传接进来**——平台透传标签但不理解路由 |
| 配置中心 / 动态热更新 | 环境变量注入；改配置就 `brickkit up` 重启 |
| 通信治理（熔断 / 限流 / 重试） | 组件自己的代码 |
| 弱依赖降级逻辑 | 组件自己的业务逻辑 |
| 多租户 | 组件自己的事 |
| 版本范围解析（`^1.0.0`） | 只接受精确版本 |
| 多环境 overlay / 继承合并 | 每个环境一份完整自包含的 `brickkit.yaml` |
| config 值类型校验 | configSchema 只是说明书 |
| 第三方组件安全审查 | 安装即信任 + 事后 `blocked` |
| monorepo 子目录组件 | 一个组件一个 Git 仓库 |
| 合并部署 / 单体外壳——平台不会、也不打算自己提供外壳脚手架或进程管理器 | 但一小块**结构性支撑已经落地**：`servedBy` 让一个组件声明"我的工作负载由另一个组件提供"，平台在 Docker 和 K8s 下都会把 `*_ENDPOINT` 地址正确接到它身上——全程不需要理解外壳里面是什么。见下文 5.7 |
| 依赖别名（`dependencies.components[].as`） | 变量名基于组件 ID 是双向可推算的，别名只保住一半；"一个能力多个实现"该走 `kind` 资源或 `configSchema` 里的地址项 |
| 低代码 / BI / DevOps 流水线 | 不在范围内 |
| Podman 作为真正能跑起来的部署目标 | 支持写过、也跑通过——`up`、`status`、真实请求、幂等重跑全部正常——但 `down` 在 rootless Podman 上失败，报 `rootless netns: kill network process: permission denied`，纯 `podman rm -f` 都能复现，完全在 BrickKit 自己的代码之外。一个停不掉的项目比根本不支持更糟——容器会一直占着端口和卷，而 CLI 却报告成功——所以真正的 `engine.Engine` 实现选择整个撤回，不留一个跑到一半的支持。**现在实际有的**：`override.yaml` 的 `target` 字段已经合法接受 `podman` 作为降级取值（§7.1）——`up --dry-run` 能完整生成它的 compose 文件——但真的（非 dry-run）对 podman target 跑 `up` 会明确报错（`ENGINE_MISSING`，并指回 `--dry-run`），而不是悄悄跑歪或者真的能用，因为背后还没有 `engine.Engine` 实现。要恢复剩下这部分，需要先有一台 `podman compose down` 本身就能干净跑通的机器，在那台机器上验证完整生命周期，再加一条可重复的检查防止它悄悄再次坏掉 |
| 平台代为从外部密钥存储（Vault / AWS Secrets Manager 的 SDK）取值 | `${VAR}` 先查进程环境、再查 `.env`——任何能把值放进环境的工具今天就能接入，平台零代码。内置的话，每接一种存储就多一个 SDK，每次 `up`（含 `--dry-run`）都要带存储凭据并联网，还是被否决的"配置中心"的邻居。**已经支持的：** `resources[].existingSecret` 与 `secret: true` 配置项写成 `{ existingSecret, key }`，引用外部系统（Vault Secrets Operator、External Secrets Operator、Sealed Secrets……）已经放进集群的 Secret——两种写法平台都不读写值本身，仅 K8s（§5.2） |
| 引擎插件 / 第三方部署目标（`up` 上一个假想的 `--engine nomad` 风格参数） | 一个目标的 `Down` / `Status` / 孤儿清理保证，才让"一个能拆干净的项目"成立；插件要自己担保它们，而 CLI 会替它报"成功"——撤掉 Podman 的同一个理由。`deploy.target` 是 `brickkit.yaml` 里的声明，绝不变成命令行参数。新目标在仓库内实现，带全套测试守卫。（`engine.Engine` 本来就是接口；这里说的是谁来担保它的语义，不是代码怎么分层） |
| 增量生成缓存（`.brickkit/` 里存哈希状态） | 没有可加速的东西：50 个组件走完整条链路约 2 ms（`tests/perf`），使用者真正在等的是 `docker compose up` / `kubectl apply`，而它们本来就只动有变化的。缓存要维护状态，过期时静默产出错误的部署文件 |
| 按契约生成 mock（一个完整的 `mock` 命令）、自动替换缺失的强依赖（`up --with-mocks` 风格的参数） | 平台从不解析契约（`artifacts.format` 只是个字符串）；给缺失的强依赖换上替身，违反"强依赖缺失就阻断启动"，还可能被误部署；mock 起在另一个名字下接不到流量，因为注入的地址指向真实组件的版本化服务名。现在就能用的：`brickkit new <id> --contract openapi` + `override.yaml` 里的 `mode: debug` + 任意 mock 工具（`docs/zh/03-guide/08-consuming-artifacts.md`） |
| 给 `brickkit graph` 自己造渲染器——HTML / SVG 输出、内置查看器、替你写文件的参数 | Mermaid 文本本来就有人免费渲染：GitHub 直接渲染 `.mmd` / `.mermaid` 文件，以及 Markdown 里标了 `mermaid` 的代码块，什么都不用装。CLI 里再内置一个渲染器，是给一件今天不花钱的事添一份永久的维护成本（排版、多一种要保证正确的输出格式）。而 stdout 里只有 Mermaid，正是 shell 重定向 `brickkit graph > graph.mmd` 得到合法文件的前提——所以连"替你写文件"的参数也不需要 |
| 在 `brickkit lint` 里做依赖解析与跨文件引用检查（`servedBy` 指向的组件存不存在？那条依赖找不找得到？） | `lint` 是一个承诺——离线、只读、秒回、不需要 Docker / K8s——而且它不新增任何规则：只是把 `up` / `add` / `publish` 本来就对每个文件跑的解析加校验单独拿出来跑。解析依赖图要读每个组件的 Manifest，对市场 / Git 组件就意味着联网；只要联一次网，这个承诺就没了。**`brickkit up --dry-run` 本来就在做这件事**——它无论如何都要解析依赖图，`servedBy` 目标不存在、或者有解析不出来的强依赖时，会报错并点出是哪一个。想看声明出来的结构，用 `brickkit graph` |

---

## 5. 核心机制（做法）

### 5.1 版本化服务名与统一地址

**服务名 = 组件 ID 转换 + 精确版本号。** 转换规则：`/` → `-`，`.` → `-`，全部小写。

| 组件 ID | 版本 | 服务名 |
| --- | --- | --- |
| `people/basic` | 1.0.0 | `people-basic-1-0-0` |
| `erp/backend` | 2.1.3 | `erp-backend-2-1-3` |

地址格式在两个环境下**完全一样**：`http://<版本化服务名>:<端口>`
（本地 `http://people-basic-1-0-0:8080`，K8s 也是 `http://people-basic-1-0-0:8080`）。

这一条带来两个直接后果：**多版本天然共存**（`people-basic-1-0-0` 和 `people-basic-2-0-0`
是两个互不冲突的 DNS 名），以及**调用方永远明确知道自己调的是哪个版本**，不存在隐式升级。

⚠️ **多版本共存是「项目级」能力，不是「组件级」。** `brickkit.yaml` 里可以并列两个版本
（供不同调用方各用各的），但**同一份 `component.yaml` 的 `dependencies` 里，一个组件 ID
只能出现一次**——依赖地址的变量名基于组件 ID、不带版本号，写两个版本会撞同一个
`*_ENDPOINT`，后者静默覆盖前者。CLI 在解析 Manifest 时就报错。
菱形依赖（A 依赖 X@1、B 依赖 X@2）不受影响，各拿各的。

### 5.2 环境变量注入规范

**核心原则：环境变量名不带版本号（基于组件 ID），值带版本号（指向具体服务）。**

```bash
DEPARTMENT_TREE_ENDPOINT=http://department-tree-1-0-0:8080
```

| 类别 | 命名规则 | 示例 |
| --- | --- | --- |
| 依赖组件主端口 | `{组件ID前缀}_ENDPOINT`（`/` 和 `-` → `_`，全大写） | `people/basic` → `PEOPLE_BASIC_ENDPOINT` |
| 依赖组件额外端口 | `{组件ID前缀}_{NAME大写}_ENDPOINT` | `PEOPLE_BASIC_GRPC_ENDPOINT` |
| 平台通用变量 | 固定 | `COMPONENT_ID`、`COMPONENT_VERSION` |
| 资源连接 | 按资源类型（kind 名就是前缀） | `DATABASE_*`、`REDIS_*`、`MQ_*`、`STORAGE_*`、`SEARCH_*`、`SMTP_*` |
| 自身配置 | configSchema 驼峰项转大写下划线 | `defaultPageSize` → `DEFAULT_PAGE_SIZE` |

**保留变量保护（两层防御）：** `COMPONENT_ID`、`COMPONENT_VERSION`、
`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`（精确匹配），`*_ENDPOINT`（后缀匹配），
`DATABASE_*` / `REDIS_*` / `MQ_*` / `STORAGE_*` / `SEARCH_*` /
`SMTP_*` / `{envPrefix}_*`（前缀匹配）。
configSchema 里的配置项名转大写后不得与之冲突——**市场在发布时拒绝**，
**CLI 在注入时警告并跳过该配置项**（平台注入的值优先）。

**第三种、跟前两种都不一样的失败方式：`configSchema.required` 里声明了一项、又没给
`default`，会直接阻断 `up`**——这既不是保留变量冲突（警告、跳过），
也不是普通的未设置可选项（悄悄不注入，组件走自己的"未配置"分支）。必填又没默认值，
说的正是"这一项平台真的猜不出来，必须由项目告诉我"（典型场景是跨项目服务的地址，
那台服务归别的项目管，平台没法推导出它在哪）。缺了它既不会崩溃也不会
报警——那个变量根本不存在——如果照旧悄悄跳过，组件会照常跑起来、看着很健康，只是
其中一条调用路径永远走不通，而使用者以为自己配好了。所以 `brickkit up` 直接报错，
并点名到底缺了哪一项、是哪个组件声明它为必填的。

**密钥与普通值走不同的路。** 资源密码（`DATABASE_PASSWORD` 等）一律是密钥；组件自己的配置项只有在
`configSchema` 里写了 `secret: true` 才算（平台从不按名字猜）。`deploy.target: k8s` 下，密钥进平台生成的
`Secret`（文件权限 0600），Deployment 里只有 `secretKeyRef`；其余都是明文 `env`。Docker 下，`config` 与
`resources[].password` 里的 `${VAR}` 在 CLI 写 `docker-compose.yaml` 时**从不**求值——由 `docker compose`
启动时求值（先进程环境、后 `.env`）。`brickkit.yaml` 里永远只有引用。平台没有内置"去 Vault 取值"，
以后也不会有（§4.1）：任何能把值放进进程环境的工具都行——而对于外部系统（Vault Secrets Operator、
External Secrets Operator、Sealed Secrets……）已经在集群里建好的 Secret，`resources[].existingSecret`
与 `secret: true` 配置项的 `{ existingSecret, key }` 写法能直接引用它，仅 K8s，平台从不读写那个值。
详见[密钥](docs/zh/07-patterns/10-secrets.md)。

上表给的是命名的"形状"；完整字典——每种资源 `kind` 精确的变量名、每条警告和那唯一
一种阻断错误的真实生成样例、`servedBy` 怎么把成员的配置合并到外壳身上——见
[环境变量注入契约](docs/zh/06-architecture/04-environment-variables.md)。

### 5.3 强依赖与弱依赖

| | 声明 | 缺失时 CLI 的行为 |
| --- | --- | --- |
| 强依赖 | `- department/tree@1.0.0` | **报错阻断启动** |
| 弱依赖 | `- id: infra/redis@1.0.0` + `optional: true` | 警告但继续，**完全不注入该环境变量** |

⚠️ **"完全不注入"不是"注入空字符串"。** 这是 BrickKit 最容易被误解的设计之一。
组件代码必须用安全读取方式（Python 的 `os.environ.get()`、Java 的 `System.getenv()`）；
用 `os.environ["X"]` 会抛 `KeyError` 让组件立刻崩溃——**这是刻意的**，理由见 9.13。

降级逻辑（Redis 挂了是查数据库、返空列表还是写本地文件）是组件自己的业务代码，平台不管。

### 5.4 `mode`：跟着上层走

> **顶层组件默认跑；下层组件只要还有一个上层在跑，它就跑；写了 `mode` 就按写的来。**

| 写法 | 含义 | 行为 |
| --- | --- | --- |
| **不写** `mode` 字段 | **跟着上层走** | 顶层（没有任何组件依赖它）默认跑；下层看上层 |
| `mode: enabled` | **一定跑** | 不看上层。它的**强**依赖被关掉时**报错**（两个意图冲突） |
| `mode: disable` | **一定不跑** | 依赖它的组件跟着不跑（钉住的——`mode: enabled`、`mode: debug` 或 `mode: local`——则报错） |
| `mode: debug` | **一定跑，进程由你自己启动** | 跟 `mode: enabled` 一样钉住（不看上层；强依赖被关掉时报错），但不生成容器——你在宿主机上、IDE 里自己跑（5.6）。仅限 Docker |
| `mode: local` | **一定跑，进程由 BrickKit 自己启动并监管** | 跟 `mode: enabled` 一样钉住（不看上层；强依赖被关掉时报错），但不生成容器——BrickKit 从组件自己的源码里探测启动命令、拉起它、盯着它（5.6）。仅限 Docker |

⚠️ **`mode: debug` 只能写在 `override.yaml` 里——`brickkit.yaml` 自身在解析阶段就直接拒绝它。**
这张表里其余每一个取值（不写 / `enabled` / `disable` / `local`）都照旧直接写在 `brickkit.yaml`
里。原因在于 `mode: debug` **本身是什么**：一个"我现在正在自己机器上调试这个组件"的个人事实，
从来都不该是一个评审 `brickkit.yaml` 的同事需要看到、被它影响、或者从 `git pull` 里意外继承到的
东西——这正是 `override.yaml`（可选、进 `.gitignore`、按开发者各自一份；§5.6）存在的理由。
`mode: local` 留在 `brickkit.yaml` 里，是因为它不是个人的——谁跑 `up`，这个进程都照样跑起来、
照样能被同样的方式连到。

**强依赖和弱依赖一视同仁**：上层弱依赖它，它照样跟着跑。`optional: true` 只管两件事——
解析期取不到只警告不阻断、它没在跑时不注入 `*_ENDPOINT`。

几个要点：

- 被多个上层共用时，只要还有一个上层在跑，它就跑——共享的底层组件不会被误伤
- 两个组件互相依赖（只可能是弱依赖成环）时，环上没有更上层的东西，两个都是顶层，都跑
- 实现算的是"**谁不跑**"（最小不动点），环因此不需要任何特例
- CLI 输出里每一行都带着理由：`启动（顶层）` / `启动（mode: enabled）` / `启动（mode: debug）` / `启动（X 需要）`（`mode: local` 跟 `mode: debug` 共用一模一样的措辞——理由讲的是**为什么**被钉住，不区分是哪一种裸进程模式）

**收窄启动范围只有一条路：改 `mode`。** 没有 `--only` 之类的命令行参数——
把不搞的**顶层**写上 `mode: disable`，`up` 与 `sync` 跟着走；
恢复全量就 `git checkout brickkit.yaml`（如果那条 `mode: disable` 写在 `override.yaml` 里，
直接删掉那一行就行——`override.yaml` 不进 Git，没什么可 checkout 的）。

`brickkit add` 自动添加的组件**不写** `mode` 字段。

### 5.5 部署文件生成与数据库迁移

组件仓库里**绝不自带**任何环境相关的部署文件。CLI 读统一的 `component.yaml`，
按 `deploy.target` 动态生成 `docker-compose.yaml` 或 K8s `Deployment/Service/Ingress`。
切换环境只改一个字段：

```yaml
deploy:
  target: k8s        # 原本是 docker
```

**迁移：** 组件声明 `migration.command`，CLI 在主服务启动前执行——
K8s 生成独立的 **Job**（不是 InitContainer，理由见 9.6）并 `kubectl wait`，
Docker 用一次性 service。**迁移失败阻断主服务启动。**
K8s 下 CLI 会先 `kubectl delete job --ignore-not-found` 清理残留旧 Job，保证幂等。

**暴露：** 默认不暴露。`expose: true` 时，K8s 生成 Ingress（需填 `hostname`），
Docker 映射端口到宿主机（可用 `exposePort` 自定义，端口冲突时 CLI 报错）。

### 5.6 裸进程两种：本地调试（`mode: debug`）与本地托管（`mode: local`）

两个值说的是同一件事："这个组件跑成你自己机器上的一个普通操作系统进程，不在容器里，其他一切照样能正确连到它。" 区别在于**谁负责启动它**：`mode: debug` 是**你自己**启动——在 IDE 里，带着断点——BrickKit 只负责把地址指过去；`mode: local` 是你只想让它跑起来、不用你操心——BrickKit 自己探测启动命令、自己拉起进程、自己盯着它。同一个形状，启动这一步的责任方相反。

**两者也在"写在哪个文件里"这件事上分道扬镳。** `mode: local` 跟其他字段一样，直接写在
`brickkit.yaml` 里。`mode: debug` **只能**写在 `override.yaml`（§3）里——`brickkit.yaml`
在解析阶段就直接拒绝它，无条件，跟 `deploy.target` 是什么完全无关。原因在于这个取值本身
代表什么："我现在正在自己机器上调试这个组件"是一个**这个开发者、这一刻**的个人事实——
恰恰是一个评审 `brickkit.yaml` diff 的同事永远不该看到、被它挡住、或者从一次 `git pull`
里意外继承到的东西。`override.yaml` 可选、进 `.gitignore`、按开发者各自一份，正是为此存在的。
`brickkit override` 会根据 `brickkit.yaml` 当前的组件列表生成/刷新它——每个组件都会有一行
裸 `- id: <id>`，`mode: debug`（连同 `localPort`）由使用者自己手工加在上面。

**`mode: debug`：** 要在 IDE 里断点调试某个组件，同时它还要被 Docker 网络里的其他组件访问：

- `override.yaml` 中标记 `mode: debug` 的组件**不生成容器**
- 其他容器通过 `extra_hosts` 把该组件的版本化服务名解析到 `host-gateway`
- 多个组件可同时本地调试，用不同 `localPort`，CLI 自动注入对应端口
- CLI 生成 `local-debug.env` 供 IDE 加载
- **组件代码零修改**（照常读环境变量）
- 它跟 `mode: enabled` 一样是**钉住**的（5.4）：不管上层怎么样它都在跑，它的**强**依赖被关掉时是
  报错，不是悄悄让步
- **仅限 Docker**：`mode: debug` 与**生效**的 `deploy.target: k8s` 同时出现，会在拿 `override.yaml`
  跟 `brickkit.yaml` 做交叉校验时被拒绝（所以 `brickkit lint` 也拦得住）——集群里的 Pod 没有路径
  能连到你自己机器上的进程。由于 `override.yaml` 只能把 `deploy.target` 往下降（k8s → docker/
  podman，绝不会反过来——§3 术语表那一条），生效目标能是 k8s 的唯一情形，就是 `brickkit.yaml`
  自己本来就是 k8s、而 `override.yaml` 又完全没碰 `target`
- 写进这份文件的值，只要含有会被 shell 误解析的字符（空白、`|`、`$`、值内部的
  真实换行……）就会按 POSIX shell 规则加上引号——多行的 PEM 值或竖线分隔的列表
  经过 `set -a && source … && set +a` 之后完整保留，不会在第一个换行处截断，
  也不会报一串 `command not found`。不含这些字符的值原样写出，不加引号
- 同一个保证的另一半：`${VAR}` 这类 config 值是从项目根目录的 `.env` 文件里
  查出来的（`internal/cli/up_k8s.go` 的 `envLookup`——K8s 渲染与 local-debug 共用的查找函数；
  Docker 的 compose 文件本身由 `docker compose` 自己按同样顺序求值——先看进程环境，再看
  `.env`），解析这个文件时用的是
  真实 `docker compose` 自己对它的解释规则（双引号值支持 `\n`/`\r`/`\t`/`\"`/`\\`
  转义、可以跨多个物理行；单引号值原样保留、同样可以跨行），不是简单地逐行
  按 `KEY=value` 切。K8s 那条渲染路径查的是同一个函数，所以一个跨多行的
  `.env` 值同样会完整出现在生成的 Deployment 的 `env` 列表里，不只是
  `local-debug.env` 才对
- 它**不会**改写的东西：一个指向 brickKit 依赖图之外某个东西（比如一个带外容器
  的地址）的 config 字面量——这类值本来就是按"另一端也在容器网络里"写的
  （比如 `http://host.docker.internal:8000`）。brickKit 不解析 config 字符串的
  内容，所以把该组件改成 `mode: debug` 并不会把这个字面量换算成宿主机视角能
  访问到的地址，需要开发者自己手工改（通常改成 `localhost`）
- 依赖如果是一个 `servedBy` 成员（§5.7），它没有自己的 compose service——它的
  宿主机端口映射改开在它的**外壳**身上，用的是这个成员自己声明的端口（正是
  外壳本来就必须监听的那个端口：`internal/shell/shell.go` 的
  `checkPortConflicts` 在生成阶段就校验过这一点，K8s 渲染器把 `*_ENDPOINT`
  路由进外壳靠的也是同一个假设——这里没有对外壳内部结构引入任何新的理解）。
  这个依赖的主端口和额外端口两个 `*_ENDPOINT` 变量在 `local-debug.<服务名>.env`
  里都会解析成一个真正的 `http://localhost:<端口>`，跟普通依赖一样。这处修复
  之前，额外端口那个变量甚至不是"诚实地连不上"：它会静默猜出一个看起来完全
  合理的 `localhost:<端口>`，实际上背后没有任何进程监听（brickKit 反馈：
  local 组件依赖 servedBy 成员时本地调试地址错误）

**`mode: local`：** 同一个想法的"帮我跑起来就行"那半边——

- 标记 `mode: local` 的组件**不生成容器**，跟 `mode: debug` 一样
- BrickKit 读这个组件自己的源码目录（`go.mod`、`package.json`、`pom.xml`……）自己判断怎么
  启动它——`component.yaml` 里没有任何地方声明这条命令，每个语言生态各自有 BrickKit
  认得的标记文件
- BrickKit 自己拉起这个进程、自己盯着它，贯穿这次 `brickkit up` 的整个生命周期：`up` 会
  一直待在前台，把进程自己的输出实时打出来、每行带服务名前缀——跟不加 `-d` 的
  `docker compose up` 一样
- **`localPort` 是可选的**——默认 BrickKit 自动挑一个空闲端口（优先用组件自己声明的
  `deployment.port`）；只有想固定某个端口时才需要手动写 `localPort`
- `.brickkit/` 下一份小小的会话锁文件是唯一能让**第二个**终端知道本地会话存在的东西：
  `status` 把 `mode: local` 组件排除在容器表之外，但也不会把它报成"没跑"；`graph` 给它
  自己的标签和颜色，钉住、永远不会被置灰；`down` 没法伸进另一个终端的进程树，会打印
  同样的会话提示，而不是悄悄什么都不做
- 停掉它就是在跑 `up` 的那个终端按 `Ctrl+C`——体面停下不打印任何额外内容（一个照要求
  停下的组件不需要崩溃摘要）；意料之外的崩溃会打一份，带退出原因和进程自己最近的输出，
  用 `--crash-lines` 控制（默认 20 行，`0` = 只留退出原因）
- 进程树是故意只属于这一个终端会话的——它从不试图活得比启动它的那次 `up` 更久，不像
  容器，CLI 退出后容器照样接着跑
- **仅限 Docker**，跟 `mode: enabled` 一样钉住（§5.4）——规则跟 `mode: debug` 一样，同样
  的理由：集群里的 Pod 没有路径能连到你自己机器上的进程
- 带真实输出的上手教程：[让一个组件在本地跑起来，不用你操心](docs/zh/03-guide/04-local-execution.md)

### 5.7 合并部署（`servedBy`）

组件可以声明 `servedBy: <外壳组件ID>@<版本>`，意思是"我的工作负载由那个
组件提供"——被指向的外壳本身就是一个普通组件，有自己的镜像、端口、健康
检查。平台会做这些事：

- 不为这个 `servedBy` 组件生成工作负载（容器 / Deployment），也不生成
  迁移容器 / Job。
- 照常给依赖它的组件算出正确的 `*_ENDPOINT`——地址指向外壳的真实位置，
  端口用的是这个 `servedBy` 组件自己声明的端口。
- 把 `*_ENDPOINT` 一类变量不带前缀地合并进外壳共享的操作系统环境
  （组件 ID 本身已经让这些名字唯一），也把每个成员自己 `configSchema`
  生成的配置合并进来——但一律带组件 ID 前缀
  （`{EnvPrefix(组件ID)}_{名字}`，跟 `*_ENDPOINT` 用同一个前缀），两个
  各自独立开发的模块就算用了同一个配置项名字也不可能撞车。绝不会替
  成员合并 `COMPONENT_ID` / `COMPONENT_VERSION`、资源连接变量，（见下文）
  也不会合并 `labels`——这些始终只是外壳自己那一套平台/资源变量，不按
  成员各来一份。`BRICKKIT_SERVED_MEMBERS_CONFIG`（下面两条之后）是这些
  带前缀变量的一份索引，不是它们值的第二份拷贝。
- 一个 `servedBy` 成员自己 `component.yaml` 里 `dependencies.resources`
  声明的资源依赖，只要外壳自己的 componentId 已经绑了同一个
  `kind`+`engine` 的资源，就算满足——不需要成员自己在那份资源的
  `bindings` 里也重复写一份（写了也不会产生任何真实作用）。真正的连接
  环境变量只会落进外壳的容器，不管绑定写在哪个 componentId 名下都一样，
  逼成员重复绑一份，纯粹是为了骗过这条校验，不会多产出任何东西。
- 往外壳的环境里写入 `BRICKKIT_SERVED_MEMBERS`（一个保留变量）：当前
  部署里实际包含的版本化服务名，逗号分隔。合规的外壳可以读它来跳过
  初始化（包括跳过迁移和资源连接）任何编译进来、但不在这份名单上的
  模块——这是可选的，平台从不检查外壳有没有真的照做。
- 还会写入 `BRICKKIT_SERVED_MEMBERS_CONFIG`（另一个保留变量）：一个
  JSON 数组，跟上面那份名单里的每个成员一一对应，各自带上
  `componentId`/`version`/`httpPort`/`extraPorts`/`configEnvVars`
  （`configEnvVars` 把该成员原始的 configSchema key 映射到上面那条带
  前缀变量的名字——不是值本身）。这是外壳作者真正要装配每个模块所需的
  索引——没有它，唯一的办法是在每次部署前手算一份等价的 JSON、贴进某个
  字符串配置项，而这份手工数据只要成员的版本号、config 或 `servedBy`
  归属一变就会过期，平台不会提醒，只会在下一次真机启动时才表现成
  crash-loop。只携带变量名、从不携带值是故意的：成员的 config 值经常是
  指向密钥的 `${VAR}` 引用，brickKit 生成 Docker Compose 文件时刻意不
  自己展开这类引用（好让生成的文件能安全地打开看、进 git diff）——把这
  类占位符最终会展开成的值塞进一段 JSON 字符串，一旦这个值带引号、反
  斜杠或换行符，就会被 docker compose 自己那套无结构感知的变量替换撑
  坏这段 JSON。零个成员时是 `[]`，不是变量缺失。

`mode: debug` 不受影响——含义、代码路径都和以前一样；`servedBy`
是一套完全独立的机制，只是碰巧和它共享"在依赖图里、但不生成工作负载"
这个形状。

**一个 `servedBy` 组件自己的 `expose` / `exposePort` / `hostname` /
`replicas` / `resources` / `serviceAccountName` / `labels` 会怎样：**
什么都不会发生，平台只警告，不报错，也不会悄悄忽略。这些字段描述的是
"我自己的容器 / Pod 怎么部署"，而一个 `servedBy` 组件根本没有属于自己的
容器 / Pod。`labels` 原先是这里唯一的例外（跟每个成员合并，规则跟
`*_ENDPOINT` 变量一样），直到真实的多组件测试证明这条规则行不通：语义上
本该因组件而异的标签值（最常见的例子是 `prometheus.io/port`，值就是各自
的端口号）几乎在每一次真实合并里都会触发"同名不同值"冲突。给"哪些键
语义上必然因组件而异"单独开一份名单，等于让平台去解释具体某个 label 键
的含义，这跟 labels 存在的初衷（§9.22：平台刻意不理解任何 label 键的
语义）直接冲突——所以 `labels` 被挪进了这一类"什么都不会发生"的字段，
而不是去扩张那份名单。

字段级细节、校验规则、外壳实现本身必须做对的事：
[打造一个合格的外壳](docs/zh/07-patterns/07-shell-implementers-guide.md)。
要不要在自己项目里声明 `servedBy`、怎么声明：
[怎么声明 servedBy：部署方检查清单](docs/zh/07-patterns/06-servedby-deployment-checklist.md)。
再往上一层，整个项目该选哪种部署形态——拓扑（纯独立/纯外壳/混搭）×
`docker`/`k8s`、`mode: debug` 调试开关、手动裸跑组件放在哪个位置：
[怎么选部署形态](docs/zh/07-patterns/05-deployment-selection-guide.md)。

### 5.8 组件源码工作区

| 命令 | 行为 |
| --- | --- |
| `brickkit add <id>@<ver> --repo` | 额外 clone 该组件的完整 Git 仓库到 `components/`（仅开源组件） |
| `brickkit add <id>@<ver> --repo-all` | clone 所有递归依赖中开源组件的仓库（闭源跳过并提示） |
| `brickkit sync` | 按启停判定结果**双向**归档 / 激活：不启动的移到 `components/.archived/`，需要启动的移回 |
| `brickkit remove <id>` | 自动删除对应源码目录，**含已归档的那一份** |

`brickkit add` **默认不 clone 源码**（只拉 Manifest + artifacts）。
`brickkit sync` 是**独立命令**，刻意不集成进 `brickkit up`（理由见 9.17）。
CLI **不管 Git 权限**：fork、remote、push 全是用户自己的事。

**把 `components/` 从 `.gitignore` 去掉的项目**（组件源码要跟项目一起进版本库），
`sync` 的整目录移动会进项目的 diff——`brickkit restore` 与 `brickkit init --hooks`
装的 pre-commit hook 就是为了拦住「归档结构进了提交、`mode` 却没跟着提交」
这个反复出现的失误。

这一整块——克隆、改了推回去、归档、`remove` 的几道保护、`restore` 与钩子——带真实输出
一步一步走一遍，见 [管理组件源码](docs/zh/03-guide/09-component-source.md)。

### 5.9 市场、签名与信任模型

市场是独立的公共平台，**不是组件，不需要被安装**。它只回答两个问题：
**有什么可以装？谁有权装？** 它不安装组件、不运行组件、不管运行状态。

**目前没有 BrickKit 官方运营的公开市场实例。** `sources[].type: market`、`brickkit login`、
`add`、`publish`、`fetch` 都需要一个市场地址，因为 CLI 里没有内置任何默认地址——要么自己
或团队搭一套（见下文），要么用别人已经跑好给你用的那一套。

| 能力 | 说明 |
| --- | --- |
| 发布 / 发现 | 上传 Manifest + 镜像引用（+ 签名）；搜索、标签、命名空间筛选 |
| 版本管理 | 精确版本列表 + 状态：`draft` / `stable` / `deprecated` / `blocked` |
| 可见性 | `public` / `private` |
| 签名 | 发布方用 **cosign** 签名；**安装方用 Go 标准库验签**（不需要装 cosign）；生产强制 |
| 产物存储 | 开源：登记 Git 仓库地址；闭源：市场存 Manifest + 镜像引用 |

**信任模型：安装即信任。** 平台不做前置安全审查（不扫码、不沙箱、不静态分析），
只做最后的"城管"——发现确凿恶意组件时标记 `blocked`，阻止新安装。
一句话：**平台提供"集市"，不提供"保险箱"。**

认证：`brickkit login` 终端交互输入账密，Token 存 `.brickkit/credentials`。

跑市场本身（而不是用别人跑好的市场）是另一件独立的部署工作，见
[自己搭一套 BrickKit Market](docs/zh/07-patterns/09-deployment/self-hosted-market.md)。

---

## 6. `component.yaml`（Manifest）字段骨架

```yaml
apiVersion: brickkit/v1
kind: Component

metadata:
  id: <scope>/<name>             # 必须，组件唯一标识
  name: <显示名称>                # 必须
  version: <major.minor.patch>   # 必须，精确版本
  description: <组件描述>         # 必须
  vendor: <发布者>                # 可选
  license: <许可证>               # 可选
  apiDocs: <API 文档地址>         # 可选

tags: [<标签>]                    # 可选，市场搜索用

artifacts:                       # 可选，组件附带的产物（API 契约 / SDK / 文档）
  - type: api-contract           # 必须，自由字符串
    format: protobuf             # 可选，自由字符串
    description: <描述>           # 可选
    files: [<相对仓库根的路径>]     # 必须

dependencies:                    # 可选
  components:
    - department/tree@1.0.0                # 强依赖（精确版本）
    - id: infra/redis-event-bus@1.0.0      # 弱依赖
      optional: true
  resources:
    - kind: database             # database / cache / mq / storage / search / smtp
      engine: postgresql

configSchema:                    # 可选，自身配置项的"说明书"（不校验值类型，但键名要对得上）
  type: object                   # 配置项名不得与保留变量冲突（见 5.2）
  properties:
    defaultPageSize:
      type: integer              # string | integer | number | boolean | array | object
      default: 20
      description: <说明>
      enum: [...]                # 可选——enum、items、minimum、maximum、pattern 都只是
      minimum: 1                 #   说明书：被解析、存下来，从不被校验
      maximum: 100               #   （minimum/maximum 是数字，pattern 是字符串，
      pattern: <正则>            #   items 在数组类型的配置项上写成 `{ type: <类型> }`）
      secret: true               # 可选——声明这是凭据：K8s 下值走平台生成的 Secret（secretKeyRef），
                                 #   绝不明文进 env。不校验任何值（§5.2）
  required: [<必填项>]

deployment:                      # 必须
  type: container                # 固定为 container（前端组件也是）
  image: <镜像地址>               # 必须
  port: 8080                     # 必须，主端口（健康检查 + _ENDPOINT 变量）
  extraPorts:                    # 可选，如 gRPC
    - name: grpc
      port: 9090
  resources:                     # 可选，**推荐值**，CLI 透传不校验
    requests: { cpu: "100m", memory: "128Mi" }   # 建议只写 requests
  # limits 建议留给部署方：配额逐字段合并，组件写了 limits.cpu，项目就删不掉
  labels:                        # 可选，部署元数据透传，平台不解释键值
    prometheus.io/scrape: "true" # 值必须是字符串——引号别丢
    prometheus.io/port: "9090"   # Docker → service labels；K8s → Pod annotations

migration:                       # 可选
  command: ["<命令>", "<参数>"]   # 数组格式
  # ⚠️ 迁移容器跟主容器是同一个镜像——入口对识别不出的参数必须 fail fast（非零码退出），
  # 绝不能落到"那就启动服务吧"（见下面的说明）

healthCheck:                     # 必须
  type: http                     # http | tcp | none
  path: /healthz                 # http 必填
  startPeriodSeconds: 60         # 可选，启动宽限期（秒），默认 60
  # ⚠️ 只检查本进程存活，禁止检查数据库 / 依赖组件 / 任何外部系统
```

> **`startPeriodSeconds` 是 `healthCheck` 下唯一可覆盖的时间参数。**
> interval / timeout / failureThreshold 由平台固定（10s / 3s / 3），三者相乘只有
> 30 秒，所以平台默认给每个组件 **60 秒**的启动宽限期，这个字段就是用来改它的。
> 冷启动超过 60 秒的组件（很重的 Spring Boot / Django / .NET）要调大：不调的话，
> Docker 下 `up` 会失败、K8s 下会永久 CrashLoopBackOff，而容器日志一路正常。
> 宽限期只推迟"判死"，不推迟"判活"，所以写大一点没有代价。

> **组件入口必须对识别不出的参数 fail fast**——这是组件开发者的责任，
> 平台没法替你强制。迁移容器和主服务容器用的是**同一个镜像**，只靠平台传的命令行
> 参数区分。如果入口的分发逻辑碰到一个不认识的参数（比如 `migration.command` 写错
> 一个字）没有报错，而是落到"那就启动服务吧"，迁移容器就会悄悄变成第二个服务容器：
> 它永不退出，主服务永远等不到 `service_completed_successfully`，整个部署卡在
> `Created`——而这个容器自己的日志还在说组件已就绪，这正是它排查起来极具误导性的
> 原因。参数校验也要放在**读环境变量、连数据库之前**——否则一个写错的参数会表现成
> 一句误导人的"连接数据库失败"，而不是真正的问题。

> **这就是全部字段。** Manifest **没有扩展字段机制**，不认识的键会被当场拒绝
> ，不是静默忽略——所以上面这份骨架照抄下来必须能过。
> 曾经有过 `observability` 与 `compatibility.minCliVersion` 两个"预留"字段，
> 已删除：两者从未被任何一处读取，而后者更糟——它长得像一道安全闸，
> 写了 `minCliVersion: 2.0.0` 的组件在 0.1.0 的 CLI 上照装不误。

**资源配额优先级链：** `brickkit.yaml` 的 `resources` > `component.yaml` 的 `resources` > CLI 默认值。

⚠️ **只有 `requests` 有默认值（`100m` / `128Mi`），`limits` 没有。** 都没写就**不生成 `limits`**——
限额是业务判断，平台猜一个数字的后果是去 OOMKill 一个跑得好好的组件（它真需要 600Mi，
而 512Mi 是平台编的）。建议**相反**地设：CPU 设 requests、**不设上限**（CPU limit 走 CFS quota，
节点空闲时也会限流成 p99 毛刺）；内存 **requests = limits**（拿 Guaranteed QoS，缺内存时最后被驱逐）。

**能塞多少组件：** 硬约束只有"一个节点上所有 Pod 的 `requests` 之和 ≤ 节点 allocatable"，
`limits` 之和可以远超容量（超卖是正常用法）。20 个组件按默认 requests 合计仅 2 核 / 2.5G。
真正的成本是**每个进程的内存地板**，几乎完全由语言决定：Go 8–20MB、Python/Node 40–90MB、
JVM 200–450MB——20 个 Spring Boot 光空转就 4–9G。**别为省内存去合并组件**，
那是解错了题（该换运行时或用 `mode: disable` 少跑几个）。

**⚠️ 健康检查禁令：** `/healthz` 只检查本进程存活。在健康检查里查数据库或依赖组件
会导致生产环境雪崩——一个下游抖动会让所有上游同时被判不健康并重启。

**⚠️ 冷启动超过 60 秒的组件要调大 `startPeriodSeconds`。** `interval` / `timeout` /
`failureThreshold` 由平台固定（10s / 3s / 3），相乘只有 30 秒——对启动慢的组件太短，
所以平台**默认给每个组件 60 秒的启动宽限期**，绝大多数组件不用碰它。
超过 60 秒：Docker 下判 `unhealthy` 让 `up -d --wait` 失败、依赖方卡在 `service_healthy`；
K8s 下启动探针放弃、Pod 被 kill 重启、再走一遍同样的 60 秒 → **永久 CrashLoopBackOff**，
而容器日志一路正常。很重的 Spring Boot、预加载很多东西的 Django、.NET 首次 JIT 可能碰到这条线。
宽限期只推迟"判死"不推迟"判活"（两秒就绪的组件照样两秒转 healthy），所以写大一点没有代价。

以上是骨架——每个字段精确的类型、是否必填、默认值、校验约束（端口范围、正则、哪些字段
不配另一个字段就悄悄不生效）见 [07-component-yaml-reference.md](docs/zh/06-architecture/07-component-yaml-reference.md)。

---

## 7. `brickkit.yaml`（项目配置）字段骨架

```yaml
project: my-shop                 # 必须，用于 K8s namespace 与 Docker network 命名

deploy:
  target: docker                 # 必须：docker | k8s
  # ↓ 以下仅 K8s 生效
  context: <kubeconfig 上下文>     # 可选，钉住部到哪个集群
  namespace: <命名空间>            # 可选，默认 brickkit-<项目名>
  createNamespace: true          # 可选，只有命名空间级权限时置 false
  podSecurity: restricted        # 可选，目前只支持 restricted
  imagePullSecrets: [<secret>]   # 可选
  ingressClass: <class 名>
  ingressAnnotations: { <键>: <值> }
  serviceAccount: { enabled: true }        # 每组件一个不挂载令牌的 SA——这是 opt-in（见下文）
  networkPolicy:                           # 按依赖图生成网络策略
    enabled: true
    ingressController: { namespace: <ns>, podSelector: {<键>: <值>} }
    allowFrom: [ { name: <为谁开>, namespace: <ns>, podSelector: {...}, ports: [...] } ]
    egress:
      enabled: true
      allowTo: [ { name: <为谁开>, resource: "<resources[].id>" } ]

sources:                         # 安装源
  - id: <安装源ID>
    type: market                 # market | git | local
    url: <API 地址或 Git 地址>     # market/git 必填
    path: <本地路径>              # local 必填
    authToken: ${ENV_VAR}        # 可选；已 login 时优先用 .brickkit/credentials
    ref: <分支 / tag / commit>    # 可选，仅 git 源；不写用默认分支（生产建议钉 tag）
    enabled: true

components:
  - id: people/basic
    version: 1.0.0               # 必须，精确版本
    mode: local                  # 这份文件里可选：enabled | disable | local（5.4）。mode: debug
                                  #   在这份文件里不合法——只能写在 override.yaml（7.1）
    localPort: 8081              # mode: local 时的宿主机端口（可选——默认自动分配）。
                                  #   mode: debug 的 localPort 也写在 override.yaml 里，跟它的 mode 挨着
    servedBy: <id>@<版本>         # 可选，这个组件的工作负载由另一个组件提供
    expose: false                # 可选，默认 false
    hostname: <域名>              # expose + k8s 时必填
    exposePort: 8080             # 可选，仅 Docker 生效
    tlsSecret: <Secret 名>        # 可选，仅 K8s + expose，Ingress 的 TLS 证书
    replicas: 3                  # 可选，仅 K8s，默认 1；>1 时自动生成 PDB
    serviceAccountName: <SA 名>   # 可选，仅 K8s；用运维建好的 SA，平台只引用不生成
    config:                      # 可选，覆盖 configSchema 默认值（不校验值类型；键名写错会警告）
      defaultPageSize: 50
    resources:                   # 可选，覆盖组件推荐配额
      requests: { cpu: "200m", memory: "256Mi" }
      limits:   { cpu: "1", memory: "1Gi" }
    labels:                      # 可选，部署元数据透传，逐键覆盖组件的 deployment.labels
      traefik.enable: "true"     # 平台不解释键值；值必须是字符串

resources:                       # 基础资源声明与绑定（资源本身由运维部署）
  - kind: database
    engine: postgresql
    id: main-db
    host: <主机>
    port: 5432
    username: <用户名>
    password: ${DB_PASSWORD}     # 必须通过环境变量引用
    existingSecret: <K8s Secret 名>  # 可选，仅 K8s，与 password 互斥——
                                    #   引用运维/Vault Secrets Operator/ESO 已经建好的 Secret，而不是让平台生成
    bindings:
      - componentId: people/basic
        # ↓ 下面四个是**同一格**（这个组件在资源里占哪一块），按 kind 用对应的
        #   那个、只能写一个。用错名字会报错并点名该用哪个
        database: people         # kind: database → DATABASE_NAME（库由使用者建一次）
      # vhost: orders            # kind: mq      → MQ_VHOST
      # bucket: media-prod       # kind: storage → STORAGE_BUCKET
      # index: products          # kind: search  → SEARCH_INDEX
      #                            kind: cache / smtp 没有这一格，写了会报错
        envPrefix: <前缀>         # 可选，多同类资源时区分环境变量

installer:
  requireSignature: true         # 可选，默认 true
  publicKeys:                    # 项目信任的发布者公钥：publicKeyRef → 公钥文件路径
    keys/vendor.pub: keys/vendor.pub
```

> ⚠️ **`serviceAccount.enabled` 是 opt-in——不写它，每个 Pod 用的还是命名空间的 `default`
> ServiceAccount，那张令牌照常自动挂载**。这一点很容易跟 §4 的"默认安全"原则
> 搞混：那条原则管的是组件能碰到什么（没有依赖边、没有资源绑定、没有暴露——全都是**对方
> 那一侧**的 opt-in），不是这个 K8s 默认行为。平台不替你打开这个开关，理由跟不默认打开
> `podSecurity: restricted` 一样：两者都可能让一个本来跑得好好的组件起不来，而这是一项
> 真实的、因项目而异的代价，平台没资格替你权衡。如果一个组件确实完全不该有调用 K8s API
> 的理由，就该主动写上 `serviceAccount: { enabled: true }`。

> ⚠️ **`publicKeys` 是唯一让验签真正生效的字段。** 一个公钥都没配时，
> 签名校验**整体失效**，`requireSignature: true` 也一并不起作用——
> 没有信任锚点就没有可校验的对象。CLI 会为此警告一句，
> 但那时它已经什么都没验过了。
>
> 公钥必须配在这里、而不是跟着签名从市场取：否则就成了市场自己给自己发证，
> 市场被攻破时攻击者把组件和公钥一起换掉，验签照样通过。

**多环境：** 每个环境一份**完整自包含**的 `brickkit.yaml`（如 `brickkit.prod.yaml`），
用 `brickkit up --config brickkit.prod.yaml` 指定。**没有 overlay / 继承 / 合并机制**（理由见 9.9）。

以上是骨架——每个字段精确的类型、是否必填、默认值、校验约束（`mode`/`servedBy`/`replicas`
之间的每一种互斥、绑定槽位规则、哪些字段只在某一种部署目标下生效而写在另一种下 `up` 会警告）见
[08-brickkit-yaml-reference.md](docs/zh/06-architecture/08-brickkit-yaml-reference.md)。

### 7.1 `override.yaml` 字段骨架

一份可选、**进 `.gitignore`**、按开发者各自本地一份的文件，跟 `brickkit.yaml` 放在一起，
本地覆盖两件事：`deploy.target`（只许降级）和某个组件的 `mode`/`localPort`。从不跟
`brickkit.yaml` 按字段合并/叠加——一个话题要么这里完全不提（那就完全跟着 `brickkit.yaml`），
要么这里写了（那就整个替掉 `brickkit.yaml` 在这个话题上的值）。`brickkit override` 首次运行时
创建它，之后每次运行都是刷新——这同时也是它的重置/修复操作，没有单独的第二个命令。

```yaml
target: podman                 # 可选：docker | podman | k8s——必须是对 brickkit.yaml 自己
                                # deploy.target 的一次降级：k8s → docker/podman 允许，反过来
                                # 永远拒绝，docker ↔ podman 不受限。不写就跟着 brickkit.yaml
                                # 自己的 deploy.target 走
targetBaseline: docker         # 这份覆盖上一次被确认时，deploy.target 的取值——喂给下面的
                                # 漂移提示，从不被强制校验

components:                    # brickkit override 给 brickkit.yaml 里每一个组件 ID 写一行——
                                # 同一个 ID 共存的多个版本共用一行，因为这份文件里根本没有
                                # 版本号（一份覆盖对那个 ID 的所有版本一视同仁）
  - id: department/tree        # 独立组件，没有覆盖——裸的一行

  - id: erp/backend            # 一个外壳——它自己也是个普通组件，这里同样没有覆盖
    members:                   # servedBy 成员嵌在它们的外壳下面，只有一层
      - id: people/basic          # servedBy 成员，没有覆盖——裸的一行
      - id: auth/rbac
        mode: disable             # 这次外壳照常跑的同时，把它排除在 BRICKKIT_SERVED_MEMBERS
                                   # 之外——不保证一定生效（AGENTS.md §5.7）

  - id: infra/redis-event-bus
    mode: local
    localPort: 8082             # 这台机器上端口 8080（brickkit.yaml 建议的默认值）已经被占了——
                                 # 在这里覆盖成另一个

  - id: payment/gateway
    mode: debug                  # 唯一能写 mode: debug 的地方（§5.4、§5.6）
    localPort: 9091
    baseline: local               # 这份覆盖上一次被确认时，brickkit.yaml 自己给这个组件写的
                                    # mode——喂给漂移提示，从不被强制校验
```

- `mode: debug` 配上一个生效的 `deploy.target: k8s` 会被拒绝（唯一能达成这个组合的情形是
  `brickkit.yaml` 自己本来就是 k8s、而这里的 `target` 没碰）
- 只写了 `localPort` 却没写 `mode`，或者 `localPort` 超出合法范围，跟写在 `brickkit.yaml` 里
  一样会被拒绝
- 一条条目指向的组件 ID 在 `brickkit.yaml` 里根本不存在（悬空引用——组件被删了，或者 ID 打错了）
  会被拒绝
- 漂移检测是按内容比对，从不按时间戳——`baseline`/`targetBaseline` 每次都跟 `brickkit.yaml`
  **当前**的值比对，对不上就打印一条提示（从不阻断）：这份覆盖可能依然是使用者想要的，
  也可能已经过时、值得回头看看
- `brickkit up`（以及 `sync`/`status`/`down`）在它存在时会自动应用它，**但只对针对默认
  `brickkit.yaml` 的这次运行生效**——`--config brickkit.prod.yaml` 会忽略当前存在的
  `override.yaml` 并打印一句说明，这样一份个人本地覆盖就永远不会泄漏进某个具名环境的运行里
- `brickkit graph` 刻意从不读它——它的输出是一份要分享、要提交的产物
  （`brickkit graph > graph.mmd`），不能因为是谁在本地生成的就长得不一样
- `brickkit add`/`remove`/`lint`/`restore` 同样接入了它（§8 命令表里各自的确切行为）——
  跟 `up` 一样，`--config` 指到默认 `brickkit.yaml` 以外的文件时，这四个也完全不理会它

以上是骨架——完整的逐字段结构在 `schemas/override.schema.json`（生成、已提交，跟
`brickkit.yaml` 自己的 schema 用的是同一套机制）。

---

## 8. CLI 命令集（17 个命令 + `version` + `lang`）

| 命令 | 核心行为 |
| --- | --- |
| `brickkit init <name>` | 生成 `brickkit.yaml` 骨架和 `.brickkit/` 目录，并装入 AI 助手技能（`--no-skills` 跳过） |
| `brickkit skills` | 查看/刷新装进项目的 AI 助手技能（`status` / `update`）。在独立的组件仓库里（有 `component.yaml`、没有 `brickkit.yaml`）只管理 `brickkit-component` 这一个技能。手改过的绝不覆盖；不碰使用者的 `CLAUDE.md` |
| `brickkit graph` | 把项目的依赖拓扑画成 Mermaid 文本，打印到 stdout：实线是强依赖，虚线是弱依赖（取不到的弱依赖画成"未安装"节点），置灰的节点是这次不会启动的组件，`mode: local` 组件带着自己的"托管本地"标签与颜色，`servedBy` 收编的成员画在各自的外壳里。**stdout 里只有 Mermaid**，所以 `brickkit graph > graph.mmd` 存下来的文件 GitHub 能直接渲染。它读的是与 `up --dry-run` 同一份解析出来的依赖图（所以还没缓存的市场 / Git 组件的 Manifest 要联网取），不生成部署文件、不碰引擎。`--ignore-served-by` 把每个组件都画成独立部署 |
| `brickkit lint` | **离线、只读**地检查当前目录里 YAML 的结构——不联网，不需要 Docker / K8s。在项目里：先查 `brickkit.yaml`，再查 `override.yaml`（如果存在，§7.1——悬空条目报错，baseline 过期报警告，`--strict` 才让警告算失败；`--config` 指到别处时跳过并打印说明），再查 `local` 安装源目录下的每一份 `component.yaml`（不管有没有 add 过；`.archived/` 不查）。在独立的组件仓库里（有 `component.yaml`、没有 `brickkit.yaml`）：只查那一份。报告必填字段、类型、未知键（拼写笔误）、版本号格式、端口范围，另有两类警告——`configSchema` 属性声明里拼错的键（不会生效）、配置项名字撞上保留变量。几乎不新增任何规则（override.yaml 过期性检查是唯一的例外）；有错误时退出码 `1`（`LINT_FAILED`），只有警告时退出码 `0`，加 `--strict` 则警告也算失败（给 CI 门禁用）。**不做**依赖解析、也不查 `servedBy` 目标在不在——两者都要解析出依赖图才知道，而 `lint` 故意不建这张图（对市场或 Git 来源的组件来说这可能意味着联网）——那是 `up --dry-run` 的事 |
| `brickkit new <scope>/<name>` | 生成一个组件的最小骨架——一份已经能通过校验的 `component.yaml`，带 `--contract openapi\|proto` 时还生成一份契约占位文件并登记进 `artifacts`。默认写到 `components/<scope>/<name>/`（`local` 安装源本来就扫描这个布局）；`--path` 写到别的地方、不再套一层，给独立组件仓库用。不生成 Dockerfile，不生成源码——平台不替你选语言，也不会替你执行 `add` |
| `brickkit add <id>[@ver]` | 递归拉取依赖，下载 artifacts，写入配置（**不写 `mode` 字段**）。不写版本时取安装源上最新可安装版本，并以**精确版本**落盘。`override.yaml`（§7.1）如果存在、且只有裸 id 默认行，刷新它补上新组件；已经有真实覆盖时原样不动，只打印提醒。`--config` 指到别处时完全不理会 `override.yaml` |
| `brickkit remove <id>` | 检查强依赖方后移除，自动删除源码目录（含归档的那份）。多版本共存时必须指定版本。被删的是最后一个版本、且 `override.yaml`（§7.1）存在时，也删掉它在那份文件里的条目——被删外壳嵌套的成员会提升成顶层条目，不会被一并删掉。`--config` 指到别处时完全不理会 `override.yaml` |
| `brickkit fetch <id>[@版本]` | 只下载组件的产物到 `.brickkit/artifacts/<版本化服务名>/`，**不写入 brickkit.yaml、不部署**。跨项目调用别人的服务时用 |
| `brickkit up` | 先应用 `override.yaml`（如果存在，且只对针对默认 `brickkit.yaml` 的这次运行——§7.1，过程中打印漂移提示）→ 启停判定 → 生成部署文件 → 生成 `local-debug.env` → 检测镜像权限 → 执行迁移 → 调用引擎 → 在前台拉起并监管所有 `mode: local` 组件（§5.6） |
| `brickkit down` | 先应用 `override.yaml` 对 `deploy.target` 的降级（§7.1），再按生效目标停止所有容器。**不删除 volume，保留数据。** 够不到跑在另一个终端里的 `mode: local` 进程——会改打印一句提示，点名那个会话的 PID |
| `brickkit status` | 应用 `override.yaml`（§7.1），读底层引擎，展示运行表格（含多版本检测、不启动的组件也列出来）。因为 `override.yaml` 里的 `mode: disable` 而没跑的组件会标上 `(override.yaml)`，跟 `brickkit.yaml` 自己写的 disable 分开。`mode: local` 组件不进这张表（它们不是容器），但有一个在别处跑着时会打印提示 |
| `brickkit sync` | 应用 `override.yaml`（§7.1），再按启停判定结果双向归档 / 激活组件源码。无参数 |
| `brickkit override` | 首次运行创建 `override.yaml`，之后每次运行都是刷新（同时也是它自己的重置/修复操作——§7.1）。`brickkit.yaml` 里每个组件都会有一行；已有的自定义值（`mode`/`localPort`/`baseline`）原样保留，从 `brickkit.yaml` 移除的组件那一行也跟着消失，新组件补一条裸 `- id:`。对着非默认的 `--config` 会拒绝运行。写完之后打印漂移提示（§7.1） |
| `brickkit restore` | 把 `mode` 与组件源码结构还原到最后一次提交。`--check` 供 pre-commit hook 判断这次提交自洽不自洽。`override.yaml`（§7.1）存在、且真的有 `mode` 变动时，打印因此产生的漂移提示——跟 `up`/`lint` 同一套非阻断检查，不是一句笼统的"可能过期了" |
| `brickkit login` | 终端交互登录市场，Token 存 `.brickkit/credentials` |
| `brickkit logout` | 先调市场作废 Token，再删本地的 `.brickkit/credentials`。**本地那份一定会删**，即使市场连不上——否则一次网络抖动就让人以为自己已经退出、凭据却还躺在盘上。没登录时什么都不做，也不算失败 |
| `brickkit publish` | 上传 Manifest + 镜像引用 + 产物到市场（需先 login） |

另有两个命令不在这 17 个之内，因为它们管的是 CLI 自己、不是你的项目：`brickkit version`，以及 `brickkit lang`——查看或切换 CLI 说哪种语言。**CLI 默认说英文。** 语言取值：先看环境变量 `BRICKKIT_LANG`，其次是 `brickkit lang set en|zh` 存进用户级配置文件的值，最后才是英文；**刻意没有** `--lang` 参数（语言必须在命令树——包括 `--help`——搭起来之前就确定）。人读的一切都跟着语言走，包括 JSON 日志行里的 `message` 和生成文件里的注释；`error_code`、命令名与参数名永远不随语言变。

**常用参数：**

```bash
brickkit up --config brickkit.prod.yaml           # 多环境
brickkit up --dry-run                             # 只生成部署文件，供审查
brickkit up --context prod-cluster                # 本次运行覆盖 deploy.context（仅 k8s）
brickkit up --ignore-served-by --dry-run          # 验证：去掉 servedBy 之后每个组件还能不能独立起来
brickkit up --crash-lines 0                       # mode: local 崩溃摘要只打退出原因，不带输出行（默认 20 行）
brickkit graph > graph.mmd                        # 依赖拓扑存成 Mermaid 文本（GitHub 直接渲染 .mmd 文件）
brickkit graph --ignore-served-by                 # 把每个组件都画成独立部署，当作没声明过 servedBy
brickkit lint                                     # 离线检查 brickkit.yaml 与本地安装源里 component.yaml 的结构
brickkit lint --strict                            # 警告也算失败（退出码 1）——给 CI 门禁用
brickkit override                                 # 根据 brickkit.yaml 当前的组件列表创建/刷新 override.yaml
brickkit down --context prod-cluster              # 同样的覆盖，用来关停指定集群
brickkit add people/basic@1.1.0 --yes             # 非交互（CI/CD）
brickkit add --local                              # 把本地安装源里的组件一次全部添加
brickkit add erp/backend@1.0.0 --repo-all         # clone 所有开源依赖的源码
brickkit fetch infra/notifier@1.0.0               # 只取产物（跨项目调用，不装进项目）
brickkit remove people/basic@1.0.0                # 多版本共存时指定版本移除
brickkit remove people/basic@1.0.0 --force        # 有未提交/未推送的改动也照样删源码目录
brickkit login --market https://market.example.com/api/v1   # 配了多个市场安装源时才需要
brickkit logout                                   # 作废市场 Token，删本地凭据
brickkit logout --keep-remote                     # 只删本地凭据，不调市场（离线时用）
brickkit publish --path ./components/people/basic --market https://market.example.com/api/v1 --visibility private --changelog "新增 X"
brickkit publish --path ./components/people/basic --git-url https://github.com/org/people-basic --sign --key cosign.key --signed-by release-bot@example.com --public-key-ref keys/vendor.pub
brickkit version --verbose                        # 额外输出 Git commit 与构建时间
brickkit lang set zh                              # 从此 CLI 说中文（用 BRICKKIT_LANG=en 可对单条命令覆盖）
brickkit up --log-level info                      # 每条命令都有这个 flag：默认是 warn（安静），info 会把常规的单命令生命周期日志找回来
BRICKKIT_LOG_LEVEL=debug brickkit up              # 用环境变量给整个终端会话定一个更详细的默认级别，不用每条命令都加 flag
```

### 8.1 一分钟示例

```bash
brickkit init my-shop                 # 创建项目
brickkit add erp/backend@1.0.0        # 一条命令拉下整棵依赖树
brickkit up --dry-run                        # 看启动顺序（拓扑排序）
brickkit up                           # 生成部署文件 → 跑迁移 → 起容器
```

---

## 9. 二十三个"为什么"（架构辩护）

这一节是 BrickKit 理念最值钱的部分。每一条都是对一个**看起来反直觉**的设计的辩护。
用户问"为什么不……"时，答案基本都在这里。（完整论证见设计书 012。）

**9.1 为什么没有注册中心，也不做健康检查轮询？**
自建注册中心意味着平台必须是常驻高可用集群，运维成本剧增；而 Docker / K8s 原生的 DNS 和 Probe
已经足够好，且比"每 10 秒轮询一次 `/healthz`"响应更快。组件也因此**零侵入**——不需要任何注册 SDK。

**9.2 为什么强制精确版本，拒绝 `^1.0.0`？**
范围版本是"在我机器上能跑，在生产挂了"的罪魁祸首。精确版本还让版本号能直接拼进服务名，
使多版本共存成为零成本的天然能力。
⚠️ 注意区分：`brickkit add people/basic`（省略版本号）**是允许的**——CLI 在 add 那一刻问出
一个具体版本、把精确版本钉进配置，等同 npm 写 lockfile。被拒绝的是**配置里写范围约束**
（`brickkit add people/basic@^1.0.0` 仍然报错）。区别在**解析时机**：一次 vs 每次。

**9.3 为什么多版本默认共存，而不是报错？**
版本化服务名让共存不需要任何额外机制。而且 `brickkit.yaml` 里写了两个版本条目，
这就是用户的意图，CLI 不需要再问一次"你确定吗"。破坏性变更是**组织协同问题**，
多版本共存只是物理缓冲，数据层兼容由使用者保证。

**9.4 为什么组件不自带部署文件？**
一套 Manifest、两种环境。组件开发者不必同时维护 `docker-compose.yaml` 和 K8s 三件套。
组件只描述"我是什么、我需要什么"，不关心"我跑在哪里"。

**9.5 为什么 CLI 是用完即走的本地工具，不是常驻 Server？**
没有后台进程 = 没有单点故障、不占资源、不监听端口、无攻击面。
`brickkit.yaml` 是唯一的真相来源，可以完美接入 Git 做版本控制和 Code Review。
未来若要可视化 Console，它应该是一个普通的前端组件 + API 组件，而不是硬编码进平台核心。

**9.6 为什么 K8s 迁移用 Job 而不是 InitContainer？**
InitContainer 是 **Pod 级**的。`replicas: 3` 时会有 3 个 InitContainer **并发**跑同一份迁移脚本，
极易死锁或损坏表结构。Job 是**集群级**的，CLI 串行控制，保证整个集群只执行一次。

**9.7 为什么不做通信治理和弱依赖降级？**
熔断阈值、限流算法、重试退避策略因业务而异，平台做"一刀切"既做不好又限制灵活性。
降级更是纯业务逻辑：Redis 挂了，组件 A 想查数据库，组件 B 想返空列表，组件 C 想写本地文件稍后重发——
平台无法统一定义什么叫"降级"。

**9.8 为什么不做配置中心（动态热更新）？**
配置中心意味着长连接、配置推送、版本比对等一整套沉重机制。而 90% 的基础配置
（连接池大小、超时时间）重启本来就是唯一安全的生效方式。真需要毫秒级热更新的业务开关，
组件自己去连 Redis 轮询即可。

**9.9 为什么多环境不引入 overlay 继承？**
overlay 要求开发者理解"基础层 / 覆盖层 / 合并规则 / 数组合并策略"，
打开一个文件看不到全貌、必须脑补继承链。自包含的完整文件所见即所得，
Git diff 时一目了然，也不会因基础层变更**隐式**影响到生产。

**9.10 为什么前端和后端组件在平台眼中没有区别？**
前端组件同样要监听端口、提供健康检查、被 Ingress 暴露、通过环境变量拿后端地址。
如果发明一种"static"特殊类型，CLI 就会长满 `if type == static` 的分支，架构出现裂痕。

**9.11 为什么平台不做第三方组件安全审查？**
与 VSCode 插件市场、npm、GitHub 一致：你用它说明你信他。开源组件代码全透明，
闭源组件基于商业协议。前置审查要么误报率极高（阻断正常组件），要么漏报率极高
（放过精心伪装的恶意代码）。`blocked` 是成本极低且足够有效的最后安全网。

**9.12 为什么 CLI 不校验 config 值的类型？**
一旦开了校验的口子，就要追问：要不要校验 `enum`？`minimum`？`pattern`？`required`？
JSON Schema 能力极其丰富，CLI 会越来越臃肿。而且组件自治——组件自己决定怎么处理错误配置
（报错、降级、用默认值），不该由平台越俎代庖。**说明书已经给你了，你不看或看错，平台不兜底。**

⚠️ **但这条不能顺延到「键名」——那里 CLI 会警告。** 分界在**有没有运行时兜底**：
类型填错了，组件拿到 `"abc"` 去 `int()` 会崩，你一定会发现；而键名填错**没有任何
运行时失败**——变量根本不出现，组件走进 `os.environ.get(k, 默认值)` 的默认分支，
一切正常运行，只是不按你配的运行。所以 `brickkit up` 会说一句
"config 里有配置项不会生效"，并猜出你想写的那个（`greetting` → `greeting`？）。
组件**根本没声明 `configSchema`** 而项目写了 `config` 时，整块都不生效，也会警告。

这也不在上面那条滑坡上：`type` / `enum` / `minimum` 都是 JSON Schema 的**约束**，
开一个口子就得追下去；而"这个键在 `properties` 里有没有"只是一次存在性检查，没有下一步——
和 Manifest 拒绝未知字段是同一条推理。

**9.13 为什么弱依赖缺失时完全不注入环境变量，而不是注入空字符串？**
这是最能体现 BrickKit 哲学的一条。注入空字符串最致命的问题是**制造静默失败**：
开发者忘记判空，写出 `requests.get(f"{ENDPOINT}/healthz")`，空字符串会拼出 `/healthz`，
请求打到**容器自身**的 8080 端口，而自己的 `/healthz` 返回 200——
开发者于是误以为弱依赖是健康的。这种 bug 极难排查。
**宁可让组件在启动时"响亮地崩溃"，也不要让它在运行时"安静地出错"。**

**9.14 为什么 `mode` 是一个字段，规则又写成"跟着上层走"？**
`mode` 从前是两个开关——`enabled`（不写 / `true` / `false`）和 `local: true`——而且没有任何东西
拦着它们互相矛盾：`enabled: false` 旁边再写 `local: true`，等于同时说"绝不跑它"和"我自己在跑它"。
一个字段、用取值说明**这个组件这一次扮演什么角色**，这种组合就根本写不出来：不写就跟着上层走，
`enabled` 与 `debug` 把它钉在"要跑"上（一个在容器里，一个是你自己启动的进程），`disable` 把它钉在
"不跑"上。规则本身的表述，则从**实现视角**的倒推（"没有启用中的组件需要它就跳过"）改成了
**使用者视角**的继承。两者逐种情况一一对应，但只有前者读得懂：使用者要做的决定就是
"这个顶层我要不要"，下面那一串跟着走，不用他算。
这不是措辞洁癖——原来那句话真的把人读岔过，照着它认真读完会得出"这条规则错了"的结论。
配套改了一处实质规则：判定里的"依赖"从只算强依赖改成强弱一视同仁，
否则 `add` 写进配置的弱依赖默认不启动，装了组件却发现一半功能是哑的。

**9.15 为什么 50 个组件不会失控？**
① **局部性原则**：组件 A 只需要知道自己直接依赖的 B、C、D 的 API 契约，
B 后面连着什么与 A 无关——单个开发者的认知边界永远是"我的直接依赖"（通常 1~3 个）。
② **按需启用**：50 个组件的项目，本地开发时可能只有 4 个容器在跑，其余 46 个"不存在"。
③ **CLI 封装了传递依赖**：一条 `add` 命令，系统就完整了。
④ **`brickkit sync`** 让源码目录也只剩下你当前关注的那 2~3 个。
**这就是"搭积木"的本质——你不需要同时看着所有积木。**

**9.16 为什么不支持 monorepo？**
组件是独立的**发布单元**（各自版本号，monorepo 的 Git tag 怎么打？）、独立的**移动单元**
（`sync` 归档移动的是含 `.git` 的完整仓库目录）、独立的**权限单元**（各自的可见性和发布者）。
注意：同一业务的多部分内容（proto + 后端代码 + 迁移脚本）属于**同一个组件**，不必拆开。

**9.17 为什么 `brickkit sync` 不集成进 `brickkit up`？**
职责分离：`up` 管运行时，`sync` 管源码目录。而且 `up` 从不需要组件的代码，只要它的
Manifest（§2.3：市场/Git 组件读缓存，本地源提供的组件每次从源码目录重读——被 `sync`
归档的也找得到）。如果 `up` 自动移动文件，用户会困惑"我的文件怎么突然 moved 了"。

**9.18 为什么 `add --repo` 不自动 clone 所有源码？**
大多数用户只想**使用**组件，不想**修改**它。一条 `add` 递归下来可能有 5~10 个依赖，
全部 clone 又慢又占盘。**源码是"开发时"需求，不是"安装时"需求。**

**9.19 为什么 clone 后的 Git 权限不归 CLI 管？**
用户可能用 GitHub / GitLab / Gitee / 自建 Gitea，CLI 不可能适配所有平台的 fork API。
fork、remote、分支策略、PR 流程都是 Git 工作流的一部分，与 BrickKit 无关。

**9.20 为什么 `brickkit remove` 自动删除源码目录？**
`remove` 的语义就是"彻底移除"。留着源码会产生"僵尸目录"——不在 `brickkit.yaml` 里
但文件还在。而且归档目录以 `.` 开头（文件管理器默认隐藏），一旦组件被 remove，
`sync` 就再也不认识它了，比普通僵尸目录更难被发现。有未提交的修改？
那应该在 remove 前先 commit——平台提供工具，不替人做决定。

**9.21 既然已经有了 `servedBy`，为什么平台还是不提供完整的合并部署命令？**
需求是真的：JVM 组件的内存地板 200–450MB，20 个就是 4–9G，私有化交付时会真的装不下。
平台**已经免费把最难的一半做完了**——调用方只读 `*_ENDPOINT`，对端是 10 个容器、
1 个容器还是 1 个 JVM 里的 10 个模块，它无从知道。`servedBy`（5.7）补上了那个真正
属于平台自己的缺口——不用借用 `mode: debug`、也不用在 K8s 侧留一个完全没有对应
方案的空档，就能把地址正确路由到合并后的整体。再往后的活（合并进程内部端口别撞、
接手健康检查与迁移、把各模块的配置互相隔离开）还是全在外壳作者自己的代码里，
一行平台代码也用不上——平台依然不需要理解哪些东西*能*合、由什么进程管理器
统一调度、某个框架怎么起多个 listener。真做一条完整的 `--consolidated` 命令，
还是得像本平台一贯反对的那样开始理解「外壳」；`servedBy` 刻意只做到
"有帮助的最小结构性支撑"为止，不是通向那条被否掉的命令的第一步。

**9.22 平台不做网关，为什么反而要加 `labels` 透传？**
因为它让平台可以**继续不理解网关**。Traefik / Prometheus 这一整类工具的标准接入方式就是
读容器标签。平台不做路由是对的，但如果**同时**不给透传口，「不做路由」就变成「也不让你
自己做」——使用者只能退回去手写一份 file-provider 配置，而那份配置里必须写满**版本化
服务名**（`erp-sales-1-0-0`），组件每升一次版本就静默过期一次，**而平台一个字都不会提醒**。
透传口把这份易腐的映射搬回了升版本时本来就要改的地方。
平台在加它前后要理解的东西**完全一样**：零。它只是一个 `map[string]string`。
这与 `deployment.resources`「透传不校验」、`deploy.ingressAnnotations`「原样透传」是同一种姿态。
**该拒绝的是语义层的自定义字段，不是部署层的透传口**——前者要求平台跟着长出理解能力，
后者明说了平台不看。

**9.23 为什么不做依赖别名（`as:`）？**
「变量名基于组件 ID」现在是一条**双向**规则：看到 `people/basic` 知道变量叫
`PEOPLE_BASIC_ENDPOINT`，看到 `PEOPLE_BASIC_ENDPOINT` 也知道它指的是谁。`as: iam` 只保住
前一半——`IAM_ENDPOINT` 在任何一处都查不出它指向哪个组件，而排查"地址怎么指错了"恰恰
是从变量名开始的。它还要求保留变量保护（§5.2 两层防御）与依赖查重（按组件 ID）
各重新推导一遍，而那两套机制的现有形状全都建立在"名字从 ID 算出来"之上。
真要换实现的那几类东西平台已经给了更便宜的位置：event-bus / 对象存储 / 缓存 / 搜索走
`kind` 资源（改 `engine` 一个字段），需要地址但不需要依赖边的服务（如 IAM）走
`configSchema` 里一个非保留键。走到依赖边上的关系里，实现的名字出现在变量名里不是缺陷
——**它就是那条依赖的事实**。

### 9.24 一句话总结

> **平台只做"连接器"和"翻译官"，绝不越界去做"业务逻辑"和"基础设施"已经做好的事情。**

---

## 10. 和用户讨论时的注意事项

这一节是给你（AI）的提醒，都是实际踩过的坑：

| 场景 | 正确做法 |
| --- | --- |
| 用户问"能不能加个注册中心 / 配置中心 / 网关" | 先说明这是被**明确论证并拒绝**的（第 4.1 节 + 第 9 节），再讨论他真正想解决的问题 |
| 写依赖声明 | **只能写精确版本**。`^1.0.0` / `~1.0.0` / `latest` 全部报错 |
| 写健康检查 | `/healthz` 只查本进程。**不要**在里面 ping 数据库或依赖组件 |
| 读弱依赖的环境变量 | 必须 `os.environ.get()` / `System.getenv()`。**绝不能**用 `os.environ["X"]` |
| 组件镜像里没有 `wget` / `curl` | Compose healthcheck 会判它 unhealthy——组件日志写着"已就绪"平台却说不健康，多半是这个 |
| 组件冷启动超过一分钟（很重的 Spring Boot / Django / .NET） | 把 `healthCheck.startPeriodSeconds` 调到比实际冷启动更长。默认宽限期是 60 秒，超了 K8s 下会永久 CrashLoopBackOff；不到 60 秒不用管 |
| 用户想在一个组件里同时调 X 的两个版本 | 不行，`dependencies` 里一个组件 ID 只能出现一次（变量名不带版本会撞）。多版本共存是**项目级**的 |
| 改了 `config` 却"没生效" | 先看键名对不对——`brickkit up` 会警告"config 里有配置项不会生效"，并猜出你想写的那个 |
| 给 MQ / 对象存储 / 搜索写绑定 | 那一格分别叫 `vhost` / `bucket` / `index`，不是 `database`。用错会报错并点名该用哪个 |
| 用户说"前端不用打包成镜像吧" | 平台里没有 static 类型。前端 = nginx 容器，`port: 80` |
| 用户想在一个仓库里放多个组件 | 不支持。一个组件一个 Git 仓库 |
| 用户问 `database` 谁来建 | **数据库由使用者创建一次**（运维侧），表由组件的 migration 建 |
| 两个组件共用一个数据库 | 迁移状态表的主键**必须含组件标识**，否则迁移会互相顶掉 |
| 用 `docker compose logs` 看不到东西 | 漏了 `-p brickkit-<项目名>`，compose 找的是另一个项目 |
| 改了本地源的 `component.yaml` 但 `up` 没反应 | 本地源不吃缓存；确认组件确实来自本地源 |
| `mode: debug` 后调用方持续 503 | 进程实际监听的端口与 `localPort` 不一致 |
| `mode: debug` 的组件报 `relation does not exist` | debug 组件不生成迁移容器，迁移要自己手动跑一次 |
| `mode: debug` 组件自己的 config 里，某个带外依赖的地址还是 `host.docker.internal` | 那是使用者自己写的字面量，brickKit 不解析 config 值，改成 `mode: debug` 不会帮你换算。自己把这个字面量改掉（通常改成 `localhost`） |
| `mode: debug` 组件依赖了一个 `servedBy` 成员 | 能连上：它的 `*_ENDPOINT` 会解析成一个真正的 `localhost:<端口>`——这个成员没有自己的容器，CLI 把映射开在它的外壳身上（§5.6） |
| 用户问「该用 `mode: debug` 还是 `mode: local`」 | 要打断点调试 → `mode: debug`，只能写在 `override.yaml` 里（你自己在 IDE 里启动）。只是想让它跑起来、不生容器、也不想自己守着一个终端命令 → `mode: local`，写在 `brickkit.yaml` 里（BrickKit 自己探测启动命令、拉起、盯着）。两个都仅限 Docker，都跟 `mode: enabled` 一样钉住 |
| 用户问「为什么 `brickkit.yaml` 不让我写 `mode: debug`」 | 设计如此（§5.4、§5.6）——`mode: debug` 是一个"我现在正在自己机器上调试这个组件"的个人事实，从来都不该是一个评审 `brickkit.yaml` 的同事需要看到的东西。它只能写在 `override.yaml`（可选、进 `.gitignore`）里。跑一次 `brickkit override` 生成/刷新那份文件，再在对应组件条目下手动加 `mode: debug` + `localPort` |
| `brickkit override` 拒绝写入，报 target 升级被拒 | `override.yaml` 的 `target` 只能降级 `brickkit.yaml` 自己的 `deploy.target`（k8s → docker/podman；docker ↔ podman 随便切；绝不能升回 k8s）。改一下 `override.yaml` 里的 `target`，或者干脆删掉那一行、跟着 `brickkit.yaml` 走 |
| 用户改了 `override.yaml` 但什么都没变 | 先查 `--config`——`override.yaml` 只对针对**默认** `brickkit.yaml` 的这次运行生效；`--config brickkit.prod.yaml` 会忽略它，并打印一句说明 |
| 用户问「为什么 `brickkit down` 停不掉我的 `mode: local` 组件」 | 这是设计如此——`down` 只碰容器；`mode: local` 进程只属于跑 `up` 的那个终端，也只能从那里够到。`status`/`down`/`graph` 有一个在跑时都会打印一句点名那个会话 PID 的提示，但谁都伸不进去 |
| `mode: local` 组件崩了，摘要太吵（或者不够详细） | `brickkit up` 的 `--crash-lines N` 控制崩溃摘要留多少行进程自己最近的输出（默认 20 行，`0` = 只留退出原因） |
| 讨论签名 | 发布方需要装 **cosign**；**安装方不需要**（验签用 Go 标准库） |
| 用户想让平台帮忙做安全审查 | 安装即信任。平台只在事后 `blocked` |
| 用户问「能不能把多个组件合并成一个实例省内存」 | 先问是不是 JVM（Go/Rust 20 个才 0.4G，不值得）；再推 GraalVM native image 与按需启用。还要合并的话：**`servedBy`（5.7）是平台支持的路径**——它在 Docker 和 K8s 下都能正确处理地址路由；其余的事（模块隔离、配置、外壳内部的迁移顺序）还是他们自己的代码，参见外壳实现者指南。`mode: disable` 和这个无关——它照样不能拿来当「我自己接管」的开关 |
| 用户的 `brickkit` 输出不是他预期的那种语言（或者某个 grep 输出的脚本坏了） | 语言是每次运行时决定的：`BRICKKIT_LANG` 优先于 `brickkit lang set` 存下的值，后者优先于默认的英文——`brickkit lang` 会打印当前生效的是哪一个、为什么。脚本别去 grep 给人看的文字：认退出码和 stderr JSON 日志行里稳定的 `error_code`，或者把 `BRICKKIT_LANG=en` 钉死 |
| 用户说命令会冒出一堆 `{"time":...,"level":"INFO",...}` 这种 JSON，很吵，不想看到 | CLI 默认就是 `--log-level warn`——常规的单命令生命周期日志（`Command started`、`Command finished` 之类）出厂就是安静的，所以这个情况只会出在有什么东西覆盖了默认值的时候：命令上显式加了 `--log-level info`/`debug`，或者 shell/CI 环境里某处设了 `BRICKKIT_LOG_LEVEL` 为其中之一。先找到那个覆盖，去掉它（或者显式传 `--log-level warn`）就是解法。`--log-level off` 更进一步，连失败时那行 `error_code` 也一起关掉——只有在人只想看 ❌ 那句话、没有脚本要解析 `error_code` 的场景（比如 pre-commit hook）才用它 |
| 用户问「纯独立/纯外壳/混搭，docker 还是 k8s，到底该选哪个」 | 这正是 `docs/zh/07-patterns/05-deployment-selection-guide.md` 那份矩阵存在的目的——照着它的矩阵走，不要临场现编答案。里面唯一一条值得直接记住的硬规则：`mode: debug`（调试开关，写在 `override.yaml` 里）只在**生效**的 `deploy.target: docker` 下存在，`k8s` 下会被拒绝 |
| 用户的上游组件还没做好/没发布，问能不能给个 mock、或让 CLI 自动替换一个 | 不是平台功能——平台从不解析契约，也从不给缺失的强依赖换上替身（§4.1）。现成能走通的路：`brickkit new <id> --contract openapi` 立一个带约定契约的桩、`add --local`、给桩在 `override.yaml` 里加 `mode: debug` + `localPort`、主机上任意 mock 工具监听那个端口。带真实输出的演示：`docs/zh/03-guide/08-consuming-artifacts.md` |
| 用户问「完全不经过 Docker/K8s，怎么把整套东西跑在本地」 | 这是平台唯一完全不管理、不注入任何东西的一档——见 `deployment-selection-guide.md` 的"手动跑起来"那节。值得告诉他们的一个技巧：在 `override.yaml` 里给那个组件临时加一条 `mode: debug` 之后跑一次 `brickkit up --dry-run`，能拿到一份真实部署会注入的环境变量清单当参考——抄完就还原这次改动，不要真的照这个方式部署 |
| 用户贴了一段 `brickkit` 的报错，或问怎么在脚本里应对失败（重试还是报警） | 每条终止命令的错误，在 `❌` 块后面紧跟的那行 stderr JSON 日志里都带一个稳定的 `error_code`。去 `docs/zh/06-architecture/10-error-codes.md`（英文版把 `zh` 换 `en`）查——它按 CLI 打印的确切标题列出每个码底下的各种情形、原因与解法。只有 `NETWORK_UNREACHABLE` 值得原样重试；码稳定，只增不改 |

---

## 11. 仓库地图与深挖入口

### 11.1 代码结构

```
cmd/brickkit/          CLI 入口
cmd/gen-schemas/       重新生成 schemas/*.json（make generate-schemas）；开发用的小工具，不编进 CLI
tools/i18n/            CLI 多语言迁移工具集（AST 抽取改写用户可见文案、测试期望值批量改写）；开发用的小工具，不编进 CLI
internal/              CLI 实现
  ├── config/            brickkit.yaml 解析与校验
  ├── override/           override.yaml 解析、单文件校验、跟 brickkit.yaml 的交叉校验
  │                        （target 降级方向、k8s+debug/local、悬空条目）、漂移检测
  ├── manifest/          component.yaml 解析与校验
  ├── schemagen/         从 config / manifest / override 的 Go 结构体反射生成 JSON Schema
  ├── resolver/          依赖解析、拓扑排序
  ├── shell/             servedBy 分组/合并，compose 与 k8s 两边渲染器共用
  ├── cascade/           启停判定：算出这次实际启动谁（跟着上层走）
  ├── skills/           内嵌的 AI 助手技能资产 + 五态判定（brickkit skills）
  ├── inject/            环境变量注入与资源配额合并
  ├── compose/           docker-compose.yaml 生成
  ├── k8s/               Kubernetes 清单生成
  ├── engine/            docker compose / kubectl 驱动
  ├── source/            安装源：market / git / local
  ├── security/          cosign 签名与标准库验签
  ├── workspace/         组件源码工作区（--repo / sync）
  └── market/            市场客户端
market-server/         组件市场后端（独立 Go module）
schemas/               component.yaml、brickkit.yaml 与 override.yaml 的 JSON Schema——生成出来并签入仓库，编辑器用它做字段补全和拼写笔误检测
docs/zh/、docs/en/     现行文档（architecture / guide / patterns，双语镜像）
docs/archive/          历史记录，不属于现行文档
tests/components/      10 个真实组件，用来测试平台本身
tests/checklist/       验收清单 → 证明它们的测试
deploy/market/         市场的 compose / kustomize / Helm
```

单元测试**紧挨被测代码**（`internal/**/*_test.go`），不建平行目录。
`tests/` 只放没法放在旁边的：清单、基准、以及当夹具用的组件。

### 11.2 深挖入口（需要细节时抓这些）

所有文档都在同一个仓库里，raw 链接前缀为
`https://raw.githubusercontent.com/brickKit/brickKit/main/`。
中文文档树完整的机器可读索引见仓库根的 **[`llms.zh.txt`](llms.zh.txt)**；
英文文档树有自己独立撰写的一份，**[`llms.txt`](llms.txt)**。

| 想深挖什么 | 抓哪一份 |
| --- | --- |
| 5 分钟动手起步，在读别的之前先看这个 | `docs/zh/00-quick-start.md`（英文版把 `zh` 换 `en`） |
| 一页纸术语速查，以及贯穿全平台的那条服务名规则 | `docs/zh/01-concepts.md`（英文版把 `zh` 换 `en`） |
| `up`/`down`、本地调试、签名验证最常见的坑，以及离线检查（`brickkit lint` 与编辑器 schema）出问题时怎么办，症状 → 原因 → 解决 | `docs/zh/08-troubleshooting.md`（英文版把 `zh` 换 `en`） |
| BrickKit 和 Docker Compose、Helm、Kustomize、Tilt/Skaffold、Backstage、monorepo 工具怎么比 | `docs/zh/02-comparison.md`（英文版把 `zh` 换 `en`） |
| 为什么组件模型适合 AI 写代码，以及一套具体工作流 | `docs/zh/05-ai-development.md`（英文版把 `zh` 换 `en`） |
| 平台是什么、核心机制怎么工作（现行版本） | `docs/zh/06-architecture/`（英文版把 `zh` 换 `en`） |
| 每个错误码、每个码底下的各种情形（按 CLI 打印的确切标题）、原因与解法；哪个码值得重试；退出码；⚠️ 警告 | `docs/zh/06-architecture/10-error-codes.md`（英文版把 `zh` 换 `en`） |
| 平台为什么长成这样：贯穿一切的那个想法（声明一张图，其余派生）；它用到或刻意没用的每个工程想法（DDD、GitOps、十二要素、契约先行、六边形架构、TDD……），逐个编号介绍：从"它是什么"讲起，说清好处、代价、对应 AI 开发的什么痛点、BrickKit 怎么做、BrickKit 不做什么、AI 怎么应对；十二条原则各自的论证 | `docs/zh/06-architecture/01-design-principles.md`（英文版把 `zh` 换 `en`） |
| 动手教程 | `docs/zh/03-guide/`（英文版同上） |
| 克隆、归档、移除、还原组件源码（`add --repo` / `sync` / `remove` / `restore`、pre-commit 钩子），带真实输出的上手教程 | `docs/zh/03-guide/09-component-source.md`（英文版同上） |
| 一个带数据库和迁移的 Go 组件，深入真实走一遍 | `docs/zh/04-go-component-template.md`（英文版把 `zh` 换 `en`） |
| 测试怎么分层、种子/测试数据怎么规划、组件怎么设计、部署怎么优化 | `docs/zh/07-patterns/`（英文版同上） |
| 整个项目该选哪种部署形态——拓扑（纯独立/纯外壳/混搭）× `docker`/`k8s`，外加 `mode: debug` 调试开关、手动裸跑组件放在哪个位置 | `docs/zh/07-patterns/05-deployment-selection-guide.md`（英文版把 `zh` 换 `en`） |
| 怎么造一个能接 `servedBy` 的合格外壳 | `docs/zh/07-patterns/07-shell-implementers-guide.md`（英文版把 `zh` 换 `en`） |
| 要不要在自己项目里声明 `servedBy`、怎么声明 | `docs/zh/07-patterns/06-servedby-deployment-checklist.md`（英文版把 `zh` 换 `en`） |
| 怎么自己搭一套组件市场 | `docs/zh/07-patterns/09-deployment/self-hosted-market.md`（英文版把 `zh` 换 `en`） |
| 合并进壳里的组件怎么共用一个数据库连接池 | `docs/zh/07-patterns/08-shared-connection-pools.md`（英文版把 `zh` 换 `en`） |
| 密钥在每种部署目标上住哪、落在哪，两种接密钥管理器的方式（进程环境 vs. `existingSecret`），以及如实交代的边界 | `docs/zh/07-patterns/10-secrets.md`（英文版把 `zh` 换 `en`） |
| 调用依赖的 `*_ENDPOINT` 在重新部署时要不要客户端特殊处理——Go/Python/Node 的 HTTP 客户端真实测量出来的行为，不是猜的 | `docs/zh/07-patterns/03-service-addressing.md`（英文版把 `zh` 换 `en`） |
| 依赖解析、菱形依赖去重、循环依赖、为什么组件多不等于串行步骤多 | `docs/zh/06-architecture/02-dependency-resolution.md`（英文版把 `zh` 换 `en`） |
| 同一份 Manifest 生成出的真实 Docker Compose 与 Kubernetes 文件，逐行对照 | `docs/zh/06-architecture/03-deployment-generation.md`（英文版把 `zh` 换 `en`） |
| 资源绑定撞车时到底会发生什么、配额链到底怎么逐字段合并 | `docs/zh/06-architecture/05-resource-binding.md`（英文版把 `zh` 换 `en`） |
| 平台可能注入的每一个环境变量——每种资源 `kind` 精确的变量名、保留变量冲突警告与唯一一种阻断错误、`servedBy` 怎么把成员的配置合并到外壳身上 | `docs/zh/06-architecture/04-environment-variables.md`（英文版把 `zh` 换 `en`） |
| `component.yaml` 每个字段的类型、是否必填、默认值、校验器真正套用的约束——包括 `enum`、`items` 这两个会被解析但代码库里从没有任何地方真正读过的字段 | `docs/zh/06-architecture/07-component-yaml-reference.md`（英文版把 `zh` 换 `en`） |
| `brickkit.yaml` 每个字段的类型、是否必填、默认值、约束——包括 `mode`/`servedBy`/`replicas` 之间的每一种互斥，以及哪些字段只在某一种部署目标下生效（写在另一种下 `up` 会警告） | `docs/zh/06-architecture/08-brickkit-yaml-reference.md`（英文版把 `zh` 换 `en`） |
| 真正被签名的是什么、验签为什么不需要 cosign 依赖、公钥为什么不能来自市场 | `docs/zh/06-architecture/06-signing-and-trust.md`（英文版把 `zh` 换 `en`） |
| 每个命令完整的参数参考，带真实生成的输出——上面 §8 的详细版 | `docs/zh/06-architecture/09-cli-reference.md`（英文版把 `zh` 换 `en`） |
| 给 `component.yaml` / `brickkit.yaml` 接上编辑器的字段补全与拼写红线——`schemas/` 里的 JSON Schema、怎么接、以及它们刻意不覆盖什么 | `docs/zh/00-quick-start.md`（英文版把 `zh` 换 `en`） |
| 市场每一个 HTTP 端点、认证、错误码，以及发布时到底传了什么 | `docs/zh/09-market-api.md`（英文版把 `zh` 换 `en`） |
| 基于 BrickKit 的组件该怎么分层测试，以及让 AI 写组件时"先立规格、再写实现"的推荐顺序 | `docs/zh/07-patterns/01-testing.md`（英文版把 `zh` 换 `en`） |
| 怎么规划种子数据与测试数据 | `docs/zh/07-patterns/02-data-construction.md`（英文版把 `zh` 换 `en`） |
| 怎么做领域研究、怎么识别一个特性该做成组件家族而不是开关、怎么用 DDD 的语言对照组件边界 | `docs/zh/07-patterns/00-component-design.md`（英文版把 `zh` 换 `en`） |
| 怎么防止闭源组件的逻辑从自己的镜像里泄露出去 | `docs/zh/07-patterns/04-closed-source-image-hardening.md`（英文版把 `zh` 换 `en`） |
| 全站文档索引（带链接） | `llms.zh.txt`（英文版是 `llms.txt`） |

---

## 12. 项目状态

| | |
| --- | --- |
| 开发进度 | 计划内的每一步都已完成，延后项也已全部结清 |
| 测试 | 2000+ 个测试函数，race-clean |
| 动手教程（现行） | 14 篇，每一篇都真跑过；见 `docs/zh/03-guide/` |
| 试用指南（归档） | 23 篇，全部对着真实 Docker / Kubernetes / 活的市场跑过 |
| 设计书 | 14 本，与实现交叉复核过两轮 |
| 决策记录 | 566 条，每条都带当初的推理 |

**运行要求：** Go 1.22+、Docker 20.10+（Compose V2）。
K8s 相关指南需要 minikube；签名需要 cosign（**仅发布方**，验签用 Go 标准库）。

**许可：** Apache License 2.0。
