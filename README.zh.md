<div align="center">

# BrickKit

</div>

[English](README.md) | [中文](README.zh.md)

<div align="center">

**声明组件，其余由 BrickKit 派生。**

*让系统像生命体一样，从组件中自然生长。*

BrickKit 是一个**声明式的组件拼装平台**：你声明有哪些组件、它们依赖什么，CLI
派生其余一切——启动顺序、服务地址、环境变量、部署清单、网络策略——运行交给你
已有的容器平台。没有注册中心，没有配置中心，没有网关，没有常驻进程。

每个组件是独立的领域单元，独立开发、测试、部署、调用。组件外部只依赖它的
Manifest 和公开契约，契约背后的实现可以随时整体替换或重写，换什么语言都行，
系统里其余一切都不用动。

**对 AI 友好，天然适合 AI 辅助开发。** 组件小到 AI 能一次读完，边界是一份
契约文件而不是靠猜；服务地址、变量名、部署文件这些原本要 AI 自己编的东西，
全部由平台派生。装配错误在 `brickkit up --dry-run` 时就先暴露，`brickkit
init` 还会把 AI 助手技能装进项目里。

</div>

**文档：**[快速开始](docs/zh/00-intro/02-quick-start.md) · [三层文件](docs/zh/01-three-layers/README.md) ·
[运行项目](docs/zh/02-project-guide/README.md) · [编写组件](docs/zh/03-component-guide/README.md) ·
[命令参考](docs/zh/07-cli-reference/README.md) · [排障](docs/zh/10-troubleshooting/README.md) ·
[全部文档](docs/zh/README.md) · [给 AI](llms.zh.txt)

---

## 三层文件架构

为了在项目增长时保持清晰，BrickKit 把关注点精确地分到三层文件里：

| 层 | 文件 | 职责 |
| --- | --- | --- |
| **声明** | `brickkit.yaml` | 有哪些组件、什么精确版本、谁是外壳——它就是锁文件 |
| **部署** | `deploy.yaml`（个人的 `deploy.local.yaml` 不进 Git） | 怎么部署：目标、端口、模式、外壳成员 |
| **配置** | `config/*.yaml` | 每个组件拿到哪些环境变量 |

声明组件，BrickKit 派生其余一切。一个组件的项目和五十个组件的项目，用的是同样的三层。

```mermaid
flowchart LR
    subgraph P["你的项目：三层文件"]
        L1["<b>brickkit.yaml</b><br/>有哪些组件、<br/>各是什么精确版本"]
        L2["<b>deploy.yaml</b><br/>怎么跑：目标、<br/>端口、模式、外壳"]
        L3["<b>config/</b><br/>每个组件拿到的<br/>环境变量"]
    end
    LOCAL["<b>deploy.local.yaml</b><br/>你的个人副本，不进 Git"] -.->|"本地模式开着时<br/>整份替换它"| L2
    M["每个组件的 <b>component.yaml</b><br/>它的契约：依赖、configSchema"]
    L1 --> UP{{"brickkit up"}}
    L2 --> UP
    L3 --> UP
    M --> UP
    UP -->|派生| OUT["启动顺序 · 服务地址<br/>环境变量<br/>compose.yaml 或<br/>Kubernetes 清单"]
    OUT --> ENG["<b>Docker · Podman · Kubernetes</b><br/>把组件跑起来，<br/>组件之间<br/>通过 DNS 直接调用"]
```

`brickkit up` 读三层文件和每个组件的 `component.yaml`，派生出其余的一切，交给你已有的引擎，然后退出——中间没有注册中心、
没有网关、没有常驻进程。

---

<details>
<summary>🤖 给 AI 助手的指令</summary>

> 先判断用户提问用的是什么语言——这份 README.zh.md 固定是中文，GitHub 渲染哪份 README 跟提问语言无关——再按任务选路线：
>
> | 任务 | 中文 | 英文（或其他语言） |
> | --- | --- | --- |
> | 了解、评估 BrickKit，或者读完全部 | <https://raw.githubusercontent.com/brickKit/brickKit/main/llms/zh/00-core.md>，再顺着每份的"下一份"往下读 | <https://raw.githubusercontent.com/brickKit/brickKit/main/llms/en/00-core.md> |
> | 回答一个问题 | <https://raw.githubusercontent.com/brickKit/brickKit/main/llms.zh.txt>：按说明挑页 | <https://raw.githubusercontent.com/brickKit/brickKit/main/llms.txt> |
> | 在本地克隆里开发 | `AGENTS.zh.md`：文档地图（§9）与代码地图（§10） | `AGENTS.md` |
>
> 每份文件都不超过 100 KB，一次抓取就能读完整。这份 README 之下的文档是完整对称的双语：`docs/zh/` 与 `docs/en/`。

</details>

---

## 安装

```bash
curl -fsSL https://raw.githubusercontent.com/brickKit/brickKit/main/install.sh | sh
brickkit version
brickkit lang set zh        # CLI 默认说英文；这一条让它从此说中文
```

脚本认出你的系统与架构，**校验 sha256 对不上就拒绝安装**，装进 `/usr/local/bin`（不可写则退到 `~/.local/bin`，不在 PATH
上时会提示）。

要真正跑起来，还需要 Git，以及 Docker 20.10+（含 Compose V2）或 Podman；部署到 `target: k8s` 时再加 kubectl 和一个集群。

### ⌨️ TAB 补全——已经装好了

`install.sh` 还会给 bash、zsh、fish 装好 TAB 补全。开一个新终端，命令、参数、组件 ID、版本、部署文件都能按 TAB 补出来——
不联网，在项目里的任何位置都能用：

```text
$ brickkit remove <TAB>
demo/bus     demo/caller  demo/hello
$ brickkit add demo/hello@<TAB>
demo/hello@1.0.0
$ brickkit up -f <TAB>
deploy.local.yaml  deploy.yaml
```

zsh 可能要在 `~/.zshrc` 里加两行——需要时 `install.sh` 会打印出来。用别的方式装的，或者按 TAB 没反应？
[命令补全](docs/zh/00-intro/03-shell-completion.md)里有每种 shell 的一条命令和排查清单。

<details>
<summary>其他安装方式、指定版本、依赖清单、Windows、卸载</summary>

CLI 是一个**单文件** Go 二进制，装它不需要任何运行时。它不常驻——项目的状态都在项目目录的三层文件与 `.brickkit/` 里，
组件的 Git 仓库缓存在用户级的缓存目录里（多个项目共享），真正干活时调用你机器上的 `docker` / `podman` / `kubectl`。

**先看脚本再跑：**

```bash
curl -fsSLO https://raw.githubusercontent.com/brickKit/brickKit/main/install.sh
less install.sh && sh install.sh
```

装指定版本用 `BRICKKIT_VERSION=v0.7.1`，装到别处用 `BRICKKIT_INSTALL_DIR=...`，不装补全用 `BRICKKIT_NO_COMPLETION=1`。

**有 Go 的话**——`go install` 装到 `$(go env GOPATH)/bin`（默认 `~/go/bin`）。它不走 Makefile，拿不到注入的版本号，
`brickkit version` 会显示 `v0.0.0-dev`：

```bash
go install github.com/brickkit/brickkit/cmd/brickkit@latest
```

**从源码构建**——`make build-cli` 产出 `bin/brickkit`，版本号、commit、构建时间都注入进了二进制（`make install` 则装到 GOBIN）：

```bash
git clone https://github.com/brickKit/brickKit.git
cd brickKit
make build-cli                 # 产出 bin/brickkit
sudo install -m 0755 bin/brickkit /usr/local/bin/brickkit
```

这两种方式都不装补全；[命令补全](docs/zh/00-intro/03-shell-completion.md)里每种 shell 各有一条命令。

**`brickkit version`** 打印的是：

```
BrickKit CLI v0.9.0
支持 Manifest 版本：brickkit/v1
支持部署目标：docker, podman, k8s
```

**还需要什么：**

| | 什么时候要 |
| --- | --- |
| Git | 从 Git 仓库拉组件时（默认的组件来源） |
| Docker 20.10+（含 Compose V2），或 Podman | `brickkit up` 起本地容器时 |
| kubectl + 一个集群（minikube 够用） | 部署文件写 `target: k8s` 时 |
| Go 1.22+ | **只有 `go install` 和从源码构建**才要（或者组件本身是 Go 写的） |
| [cosign](https://github.com/sigstore/cosign) | **只有发布方**签名时；验签用 Go 标准库，装 CLI 的人不需要 |

**CLI 的语言：** `brickkit lang set zh` 在这台机器上从此说中文，`BRICKKIT_LANG=zh` 只管一条命令，`brickkit lang`
说出现在生效的是哪种、为什么。`BRICKKIT_LANG` 优先于保存下来的设置，保存下来的设置优先于默认的英文；错误码、命令名、参数名
不随语言变化。详见 [`brickkit lang`](docs/zh/07-cli-reference/README.md#brickkit-lang)。

**Windows：** 有 `windows/amd64` 的 zip，在 [Releases](https://github.com/brickKit/brickKit/releases) 页面手动下。但它只验过
不需要 Docker 的那部分命令——起容器和 K8s 那条线在 Windows 上**没验过**，不是不支持，是没验过。还没有 Homebrew / Scoop / apt
包：它们都是 Releases 的下游，先把上游做出来。

**卸载：** `rm "$(command -v brickkit)"`。项目自己的东西都在项目目录里，删掉项目目录就删干净了；想连组件仓库的缓存一起清，
再删掉用户缓存目录下的 `brickkit/`。

</details>

---

## 一分钟看完

```bash
brickkit init my-shop                 # 创建项目（生成三层文件骨架）
cd my-shop
# 在 brickkit.yaml 的 sources: 里启用一个提供 erp/backend 的安装源
brickkit add erp/backend@1.0.0        # 一条命令拉下整棵依赖树
brickkit build                        # 构建需要在本机构建的镜像（如果有）
brickkit up --dry-run                 # 看启动顺序（拓扑排序）
brickkit up                           # 生成部署文件 → 跑迁移 → 起容器
```

一次 `add` 拉下全部依赖。一次 `up` 把声明变成运行中的容器 —— 或者变成
Kubernetes 清单，只改一个字段：

```yaml
# deploy.yaml
target: k8s          # 原本是 docker
```

组件代码一个字都不用改：两个环境下的地址格式完全一样，都是
`http://<版本化服务名>:<端口>`（例如 `http://people-basic-1-0-0:8080`）。

**21 个命令，加 `version`、`lang` 与 `completion`：** `init` `skills` `graph` `lint` `new` `add` `remove`
`fetch` `upgrade` `up` `down` `status` `sync` `local` `restore` `deps` `build` `release`
`publish` `login` `logout`

想动手照着跑一遍？[5 分钟 Quick Start](docs/zh/00-intro/02-quick-start.md) 用仓库自带的
测试夹具走完这整条路径，每一步都是真实命令和真实输出。

---

## 用你已经会的工具类比一下

| BrickKit | 大致相当于 |
| --- | --- |
| BrickKit CLI | `npm` + `helm` + `docker compose` + `git clone`，但面向**业务组件** |
| 组件 | npm package / Docker image |
| `component.yaml` | `package.json` |
| `brickkit.yaml` | `package-lock.json`：用到的每个组件都锁在一个精确版本上 |
| `deploy.yaml` | `compose.yaml` 里"怎么跑"的那一半 |
| `brickkit add` | `npm install` |
| `brickkit up` | `docker compose up -d` / `kubectl apply` |

区别在于：npm 装的是代码库，BrickKit 装的是**能独立跑起来的业务服务**。所以
它同时要管依赖解析、部署文件生成、地址注入、数据库迁移和启动顺序。

更熟悉 Java 生态的话，也可以把它理解成面向业务组件的 Maven/Gradle——只是它拉下来、
管理依赖版本的不是 jar 包，而是一个个可以独立启动的完整服务。

组件从哪来：默认是 Git 仓库（版本就是 Git tag），也可以是本机目录；组件市场是可选的基础设施。

---

## 核心能力

### 渐进式构建

这些组件来自你们组织自己的安装源——一个 Git 组织或组件市场，在 `brickkit.yaml` 的 `sources:` 里启用
（新建的项目只有 `./components` 与 `./shell` 两个本地源）。什么都没准备、想先照着跑一遍，
[快速开始](docs/zh/00-intro/02-quick-start.md) 用的是仓库自带的示例组件。

```bash
brickkit init my-shop && cd my-shop
brickkit add people/basic@1.0.0 && brickkit up
# 一个组件跑起来了。

brickkit add department/tree@1.0.0 && brickkit up
# 两个组件跑起来了，people/basic 自动拿到了 department/tree 的地址。

brickkit add erp/backend@1.0.0 && brickkit up
# 整棵依赖树自动拉齐，拓扑排序，迁移跑完，容器全起来。
# 你没有在任何时候「设计过架构」。它自己长出来了。
```

### 环境一致

```yaml
# deploy.yaml —— 只改这一个字段
target: k8s    # 原本是 docker
```

组件代码里读到的地址，在两个环境下完全一样：

```bash
DEPARTMENT_TREE_ENDPOINT=http://department-tree-1-0-0:8080
```

零修改。不是「几乎不用改」，是零。

### 语言无关

```yaml
# people/basic 是 Python 写的
# auth/password-login 是 Go 写的
# portal/user-frontend 是纯 HTML + nginx
# 对 BrickKit 来说，它们都是「一个 Docker 镜像 + 一份 component.yaml」——
# 而且这三个都作为夹具放在本仓库的 tests/components/ 里。
```

### 平台不挡路

没有 SDK 要引入。没有 sidecar 要注入。没有 agent 要部署。

组件代码里唯一的平台痕迹是 `os.environ.get("XXX_ENDPOINT")`。

把这个环境变量去掉，组件在任何地方都能跑。

---

## 设计哲学：为什么这么少

这份清单和上面的能力同样重要 —— 它们不是「还没做」，而是**被论证过并拒绝**的：

| 不做 | 这意味着你…… |
| --- | --- |
| 注册中心 / 地址簿 | 不需要学 Eureka/Consul/Nacos，DNS 就是服务发现 |
| 常驻服务 / 控制面 | 没有后台进程要运维、没有端口要开、没有单点故障 |
| 健康检查轮询 | 不用为轮询频率、超时阈值这些运维细节操心，K8s Probe / Compose healthcheck 原生就有 |
| API 网关 / 服务网格 | 不需要维护 Kong/Traefik 的平台级配置，组件间 DNS 直连 |
| 配置中心 / 动态热更新 | 不需要部署 Apollo/Nacos Config，改 `config/` 或部署文件，然后 `brickkit up` |
| 通信治理（熔断 / 限流 / 降级） | 不需要被平台的默认策略限制，业务复杂度由业务代码自己处理 |
| 版本范围解析（`^1.0.0`） | 不需要处理「隐式升级」带来的生产事故，精确版本就是契约 |
| 多环境 overlay / 继承合并 | 不需要理解「基础层 / 覆盖层 / 合并规则」，每个环境一份完整的部署文件，Git diff 一目了然 |
| 多租户 | 不需要被平台的数据隔离模型束缚，每个组件自己决定隔离策略 |
| 第三方组件安全审查 | 不需要等平台的审核流程，安装即信任（和 npm、VS Code 插件市场同一套模型） |
| 自动构建镜像 | `brickkit up` 绝不执行 `docker build`——构建是你的显式动作（`brickkit build`），部署是平台的机械操作 |

> **平台只做「连接器」和「翻译官」，绝不越界去做「业务逻辑」和「基础设施」已经
> 做好的事情。**

十条设计原则的逐条论证见[设计原则与取舍](docs/zh/06-architecture/05-design-principles.md)。

---

## 底下的那些想法

BrickKit 不是一堆功能的堆砌。它是几个广为人知的工程想法，被始终如一地应用，
每一个都落在你能跑起来的机制上——并且它们都服务于同一个：**声明一张由组件及其
依赖构成的图，其余全部派生。**

| 想法 | BrickKit 怎么用它 | 换来什么——对你，也对 AI |
| --- | --- | --- |
| **限界上下文**（DDD） | 组件是独立单元：自己的仓库、Manifest、版本生命周期和契约 | 任何时候只需要理解一个组件 |
| **声明式期望状态** | 三层文件是唯一输入，放在 Git 里，每个环境一份完整的部署文件。CLI 用完即走，没有控制面 | 描述目标，而不是部署脚本；diff 就是评审 |
| **派生优于配置** | 启动顺序、服务地址、`*_ENDPOINT` 变量、Compose/Kubernetes 清单、网络策略，全部由依赖图算出来 | 派生值不会与来源漂移，也没有人需要去猜变量名或端口 |
| **十二要素配置** | 地址、连接信息、配置都以环境变量到达；Docker 与 Kubernetes 上是同一种地址格式 | 组件代码不知道自己跑在哪——换环境零改动 |
| **精确版本，并排共存** | 不接受范围；版本号是服务名的一部分（`people-basic-1-0-0`） | 两个版本作为两个 DNS 名字共存，AI 写的 v2 可以和 v1 并排跑，不动 v1 的调用方 |
| **契约先行** | `artifacts` 随 Manifest 携带 API 契约，`brickkit fetch` 只取契约不装组件 | 不读代码也能读懂一个组件的边界——而且既然外部从不依赖边界之外的东西，边界背后的实现就可以随意整体替换或重写 |
| **大声失败** | 未知的 Manifest 键直接拒绝；缺失的弱依赖**什么都不注入**（绝不注入空字符串）；写错的配置键会报出来；升级冲突写成重复 key，解决之前拒绝启动 | 错误在 `up` 或启动时暴露，而不是变成生产里一个悄悄的错误答案 |
| **最小权限** | 声明之前什么都不可达；可选的网络策略由依赖图生成；签名的组件只用 Go 标准库验证 | 默认更小的爆炸半径，信任锚在**你自己的**项目里 |

每个想法的通俗介绍、代价，以及 BrickKit 怎么对待它，见
[设计原则与取舍](docs/zh/06-architecture/05-design-principles.md#认识这些想法)。

## 如果你在用 AI 写代码

BrickKit 的组件模型天然适合 AI 辅助开发。

项目自带的 10 个真实组件，每个都小到 AI 能一次读完；加上明确的 `component.yaml` 契约边界，
AI 不需要在庞大的单体代码库中迷失。项目的 `AGENTS.md`——AI 编程工具最先读的那份文件——末尾一张表列出每个组件、
它干什么、文档在哪，AI 按需只读用得到的那一个。

环境变量注入意味着 AI 永远不需要处理服务发现或配置中心的复杂性；精确版本 +
多版本共存意味着 AI 生成的 v2 可以和 v1 安全并存，不会搞坏依赖 v1 的其他组件。

完整的道理和一步一步的工作流，见 [AI 专属指南](docs/zh/08-ai-guide/README.md)。

---

## 接下来去哪

**按阅读顺序**——`docs/zh/` 下文件夹前面的编号就是推荐的顺序，每个文件夹里的 README
讲清楚这一块怎么读。完整目录见 [`docs/zh/README.md`](docs/zh/README.md)。
**如果你是 AI**，从 [`llms.zh.txt`](llms.zh.txt) 开始（英文文档树有自己的 [`llms.txt`](llms.txt)）：它把一个问题路由到一页，
也列出装着全部文档的那几份合集。

| 编号 | 文档 | 你会得到什么 |
| --- | --- | --- |
| 00 | [概览与入门](docs/zh/00-intro/README.md) | 5 分钟 Quick Start、核心概念术语表、分形架构（套娃机制） |
| 01 | [三层架构详解](docs/zh/01-three-layers/README.md) | `brickkit.yaml` / `deploy.yaml` / `config/` 的写法与解析优先级 |
| 02 | [项目管理者指南](docs/zh/02-project-guide/README.md) | `init`、`add`、本地调试（`local`）、升级与配置迁移、多环境切换 |
| 03 | [组件开发者指南](docs/zh/03-component-guide/README.md) | `new`、`release`、组件的文档（`BRICKKIT.md`、`AGENTS.md`、`README.md`）、分形开发模式 |
| 04 | [外壳机制](docs/zh/04-shell/README.md) | 单体进程模型、JSON 注入、`members` 管理、特殊字符处理 |
| 05 | [数据库迁移](docs/zh/05-migration/README.md) | 迁移服务、环境变量透传、多版本迁移链 |
| 06 | [架构与深度机制](docs/zh/06-architecture/README.md) | 依赖解析、Git 缓存、设计原则、错误码 |
| 07 | [CLI 命令参考](docs/zh/07-cli-reference/README.md) | 全部 21 个命令与每条命令的参数 |
| 08 | [AI 专属指南](docs/zh/08-ai-guide/README.md) | AI 文件路由表、分形读取策略、组件文档规范 |
| 09 | [推荐实践](docs/zh/09-patterns/README.md) | 组件设计准则、测试策略、可靠调用依赖 |
| 10 | [故障排除](docs/zh/10-troubleshooting/README.md) | 症状 → 原因 → 解决 |
| 11 | [参考手册](docs/zh/11-reference/README.md) | 三个文件的完整字段规格、JSON Schema |

---

## 仓库结构

```text
cmd/brickkit/          CLI 入口
cmd/gen-schemas/       重新生成 schemas/*.json（make generate-schemas）
internal/              CLI 实现
  ├── projfile/          brickkit.yaml 解析与校验
  ├── deployfile/        deploy.yaml / deploy.local.yaml 解析与校验
  ├── configdir/         config/ 目录：命名、$var: 解析、配置迁移
  ├── project/           把三层文件装载成一个项目，跨文件一致性检查
  ├── manifest/          component.yaml 解析与校验
  ├── install/           add / remove / upgrade 要对三层文件做哪些改动
  ├── resolver/          依赖解析、拓扑排序
  ├── cascade/           启停判定：算出这次实际启动谁
  ├── shell/             外壳：成员分组、JSON 注入、地址改写
  ├── inject/            环境变量注入
  ├── compose/           compose.yaml 生成
  ├── k8s/               Kubernetes 清单生成
  ├── engine/            docker compose / podman / kubectl 驱动
  ├── source/            安装源：git / local / market，与永久缓存
  ├── release/           brickkit release：打 tag 与推送
  ├── security/          cosign 签名与标准库验签
  ├── workspace/         组件源码工作区（--repo / sync）
  ├── schemagen/         从结构体生成 JSON Schema
  └── i18n/, msgid/      CLI 的多语言消息目录
market-server/         组件市场后端（独立 Go module，可选）
schemas/               component.yaml、brickkit.yaml、deploy.yaml 的 JSON Schema（生成并签入）
tests/components/      10 个真实组件，用来测试平台本身
tests/checklist/       验收清单 → 证明它们的测试
tests/regression/      回归清单 → 证明它们的测试
deploy/market/         市场的 compose / kustomize / Helm
docs/zh/               中文文档（与英文对称镜像，不是英文的译本）
docs/en/               英文文档
tutorials/zh/, tutorials/en/   教程（尚未编写）
archive/               历史归档，不属于现行文档
```

单元测试**紧挨着被测代码**（`internal/**/*_test.go`），不建平行目录。`tests/`
只放没法放在旁边的：清单、守卫、基准，以及当夹具用的组件。

---

## 构建与测试

```bash
make build            # bin/brickkit + bin/market-server
make test             # 单元测试
make test-all         # 全部测试套件
make lint             # 静态检查与文档检查
```

一组检查持续运行，而且**每一道坏掉时都会大声报错**，而不是安静地报告零问题：

| 命令 | 守住什么 |
| --- | --- |
| `make test-regression` | 面向用户的承诺 → 证明它们的测试（`tests/regression/清单.tsv`） |
| `make test-boundary` 等 | 边界 / 错误 / 兼容 / 安全验收条目 → 证明它们的测试（`tests/checklist/清单.tsv`） |
| `make check-doc-fields` | 文档里的 YAML 片段与字段表，字段名都真的存在（真相来源是结构体本身）；字段参考写全了每个字段；错误码参考覆盖每个错误码；文档里带符号的 CLI 输出行，都是文档所在语言里真实存在的文案 |
| `make check-schemas` | `schemas/` 里的 JSON Schema 与结构体生成出来的结果一字不差（重新生成用 `make generate-schemas`） |
| `make check-docs` | 现行内容不再指向归档；每个 `§` 都写明是哪份文档的小节且真实存在；链接与 `#锚点` 不断；代码里写的文档路径都存在 |
| `make check-cli-docs` | 文档里写的每条命令 / 参数都真的存在；命令参考写全了每条命令、子命令和参数 |
| `make check-doc-tree` | 文档里画的 `.brickkit/` 目录树，与 CLI 真的会创建的一致 |
| `make check-docs-bilingual` | docs/zh 与 docs/en 保持镜像、根目录每一对多语言文件都成对、raw 链接都能解析到真实文件、英文文档里没有中文 |
| `make check-i18n` | 生产代码里没有写死的中文或英文文案、英文的每个单复数形式都配齐 |
| `make check-guards` | 架构边界、每条报错都带建议、多语言守卫 |
| `make check-install-sh` | `install.sh` 装得上，而且校验和坏掉时**真的**拒绝装 |
| `make check-llms` | `llms/` 下的文档合集与 `llms*.txt` 里的合集清单，和重新跑一遍 `make generate-llms` 的结果一字不差 |
| `make check-githooks` | 提交钩子遇到文档改动会重新生成并暂存合集，遇到半暂存或未跟踪的文档会拒绝提交 |

一份指向已不存在的测试的清单会让构建失败。一个目录变空的测试目标同样会 ——
**安静跳过的套件比没有套件更糟**，因为它还占着计分板上的一行。

---

## 项目状态

三层文件重构已完成，行为以 `tests/checklist/` 与 `tests/regression/` 下两份活的清单为准，
文档以 `docs/zh/` 与 `docs/en/` 为准。

| | |
| --- | --- |
| 测试 | 2000+ 个测试函数，race-clean |
| 教程 | 尚未编写 |

**运行要求：** Go 1.22+（只有从源码构建时）、Git、Docker 20.10+（含 Compose V2）或 Podman。
Kubernetes 需要一个集群（minikube 够用）；签名需要 [cosign](https://github.com/sigstore/cosign)
（**仅发布方** —— 验签用 Go 标准库）。

---

<div align="center">

[Apache License 2.0](LICENSE)

</div>
