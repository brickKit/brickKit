<div align="center">

# BrickKit

</div>

[English](README.md) | [中文](README.zh.md)

<div align="center">

**声明组件，其余由 BrickKit 派生。**

*让系统像生命体一样，从组件中自然生长。*

BrickKit 是一个**声明式的组件拼装平台**：你声明有哪些组件、它们依赖什么，CLI
派生其余一切——启动顺序、服务地址、环境变量、部署清单、网络策略——然后交给
Docker 或 Kubernetes，自己退出。没有注册中心，没有配置中心，没有网关，没有
常驻进程。

每个组件是独立的领域单元，独立开发、测试、部署、调用。组件外部只依赖它的
Manifest 和公开契约，契约背后的实现可以随时整体替换或重写，换什么语言都行，
系统里其余一切都不用动。

**对 AI 友好，天然适合 AI 辅助开发。** 组件小到 AI 能一次读完，边界是一份
契约文件而不是靠猜；服务地址、变量名、部署文件这些原本要 AI 自己编的东西，
全部由平台派生。装配错误在 `brickkit up --dry-run` 时就先暴露，`brickkit
init` 还会把 AI 助手技能装进项目里。

</div>

---

<details>
<summary>🤖 给 AI 助手的指令</summary>

> 如果有人把这个仓库的地址交给你，让你了解、讨论或评估 BrickKit —— **抓取任何文件
> 之前，先判断用户提问用的是什么语言。** 这份 `README.zh.md` 固定是中文，但 GitHub
> 渲染哪份 README 跟提问语言无关，别把它当成语言信号。判断完，按下表抓这一对：
>
> | 提问语言 | 抓这两份 |
> | --- | --- |
> | 中文 | <https://raw.githubusercontent.com/brickKit/brickKit/main/AGENTS.zh.md> 与 <https://raw.githubusercontent.com/brickKit/brickKit/main/llms.zh.txt> |
> | 英文（或其他语言） | <https://raw.githubusercontent.com/brickKit/brickKit/main/AGENTS.md> 与 <https://raw.githubusercontent.com/brickKit/brickKit/main/llms.txt> |
>
> `AGENTS.md`/`AGENTS.zh.md` 各自压缩了整个平台的单一文件：定位、术语表、设计
> 原则、明确列出的「不做」清单、核心机制、两份 YAML 的字段骨架、完整命令参考，
> 以及每一条「为什么这样设计」的论证。`llms.txt`/`llms.zh.txt` 各自是所属文档树
> 的完整站点索引 —— 每一条都带绝对 raw 链接与一句话说明；两者互不为对方译本。
>
> 这份 README 之下的文档是完整对称的双语：用户在用中文提问，读 `docs/zh/` 下的
> 一切；在用英文（或其他语言）提问，读 `docs/en/`。

</details>

---

## 用你已经会的工具类比一下

| BrickKit | 大致相当于 |
| --- | --- |
| BrickKit CLI | `npm` + `helm` + `docker compose` + `git clone`，但面向**业务组件** |
| BrickKit Market（组件市场） | npmjs.com / Docker Hub / App Store |
| Component（组件） | npm package / Docker image |
| `component.yaml` | `package.json` |
| `brickkit.yaml` | `docker-compose.yaml` 的「声明式输入」 |
| `brickkit add` | `npm install` |
| `brickkit up` | `docker compose up -d` / `kubectl apply` |

区别在于：npm 装的是代码库，BrickKit 装的是**能独立跑起来的业务服务**。所以
它同时要管依赖解析、部署文件生成、地址注入、数据库迁移和启动顺序。

更熟悉 Java 生态的话，也可以把它理解成面向业务组件的 Maven/Gradle——只是它拉下来、
管理依赖版本的不是 jar 包，而是一个个可以独立启动的完整服务。

---

## 核心能力

### 渐进式构建

```bash
brickkit init my-shop && brickkit add people/basic@1.0.0 && brickkit up
# 一个组件跑起来了。

brickkit add department/tree@1.0.0 && brickkit up
# 两个组件跑起来了，people/basic 自动拿到了 department/tree 的地址。

brickkit add erp/backend@1.0.0 && brickkit up
# 整棵依赖树自动拉齐，拓扑排序，迁移跑完，容器全起来。
# 你没有在任何时候「设计过架构」。它自己长出来了。
```

### 环境一致

```yaml
# brickkit.yaml —— 只改这一个字段
deploy:
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

## 一分钟看完

```bash
brickkit init my-shop                 # 创建项目
brickkit add erp/backend@1.0.0        # 一条命令拉下整棵依赖树
brickkit up --dry-run                 # 看启动顺序（拓扑排序）
brickkit up                           # 生成部署文件 → 跑迁移 → 起容器
```

一次 `add` 拉下全部依赖。一次 `up` 把声明变成运行中的容器 —— 或者变成
Kubernetes 清单，只改一个字段：

```yaml
deploy:
  target: k8s        # 原本是 docker
```

组件代码一个字都不用改：两个环境下的地址格式完全一样，都是
`http://<版本化服务名>:<端口>`（例如 `http://people-basic-1-0-0:8080`）。

**17 条命令，外加 `version` 与 `lang`：** `init` `skills` `graph` `lint` `new` `add` `remove`
`fetch` `up` `down` `status` `sync` `override` `restore` `login` `logout` `publish`

想动手照着跑一遍？[5 分钟 Quick Start](docs/zh/00-quick-start.md) 用仓库自带的
测试夹具走完这整条路径，每一步都是真实命令和真实输出。

---

## 设计哲学：为什么这么少

这份清单和上面的能力同样重要 —— 它们不是「还没做」，而是**被论证过并拒绝**的：

| 不做 | 这意味着你…… |
| --- | --- |
| 注册中心 / 地址簿 | 不需要学 Eureka/Consul/Nacos，DNS 就是服务发现 |
| 常驻服务 / 控制面 | 没有后台进程要运维、没有端口要开、没有单点故障 |
| 健康检查轮询 | 不用为轮询频率、超时阈值这些运维细节操心，K8s Probe / Compose healthcheck 原生就有 |
| API 网关 / 服务网格 | 不需要维护 Kong/Traefik 的平台级配置，组件间 DNS 直连 |
| 配置中心 / 动态热更新 | 不需要部署 Apollo/Nacos Config，改 `brickkit.yaml` 然后 `brickkit up` |
| 通信治理（熔断 / 限流 / 降级） | 不需要被平台的默认策略限制，业务复杂度由业务代码自己处理 |
| 版本范围解析（`^1.0.0`） | 不需要处理「隐式升级」带来的生产事故，精确版本就是契约 |
| 多环境 overlay / 继承合并 | 不需要理解「基础层 / 覆盖层 / 合并规则」，每个环境一份完整配置，Git diff 一目了然 |
| 多租户 | 不需要被平台的数据隔离模型束缚，每个组件自己决定隔离策略 |
| 第三方组件安全审查 | 不需要等平台的审核流程，安装即信任（和 npm、VS Code 插件市场同一套模型） |

> **平台只做「连接器」和「翻译官」，绝不越界去做「业务逻辑」和「基础设施」已经
> 做好的事情。**

每一条的完整论证见
[`docs/zh/06-architecture/`](https://github.com/brickKit/brickKit/tree/main/docs/zh/06-architecture)。

---

## 底下的那些想法

BrickKit 不是一堆功能的堆砌。它是几个广为人知的工程想法，被始终如一地应用，
每一个都落在你能跑起来的机制上——并且它们都服务于同一个：**声明一张由组件及其
依赖构成的图，其余全部派生。**

| 想法 | BrickKit 怎么用它 | 换来什么——对你，也对 AI |
| --- | --- | --- |
| **限界上下文**（DDD） | 组件是独立单元：自己的仓库、Manifest、版本生命周期和契约 | 任何时候只需要理解一个组件 |
| **声明式期望状态** | `brickkit.yaml` 是唯一输入，放在 Git 里，每个环境一份完整文件。CLI 用完即走，没有控制面 | 描述目标，而不是部署脚本；diff 就是评审 |
| **派生优于配置** | 启动顺序、服务地址、`*_ENDPOINT` 变量、Compose/Kubernetes 清单、网络策略，全部由依赖图算出来 | 派生值不会与来源漂移，也没有人需要去猜变量名或端口 |
| **十二要素配置** | 地址、资源连接、配置都以环境变量到达；Docker 与 Kubernetes 上是同一种地址格式 | 组件代码不知道自己跑在哪——换环境零改动 |
| **精确版本，并排共存** | 不接受范围；版本号是服务名的一部分（`people-basic-1-0-0`） | 两个版本作为两个 DNS 名字共存，AI 写的 v2 可以和 v1 并排跑，不动 v1 的调用方 |
| **契约先行** | `artifacts` 随 Manifest 携带 API 契约；市场要求提供 API 的闭源组件必须声明它 | 不读代码也能读懂一个组件的边界——而且既然外部从不依赖边界之外的东西，边界背后的实现就可以随意整体替换或重写 |
| **大声失败** | 未知的 Manifest 键直接拒绝；缺失的弱依赖**什么都不注入**（绝不注入空字符串）；写错的 config 键会警告 | 错误在 `up` 或启动时暴露，而不是变成生产里一个悄悄的错误答案 |
| **最小权限** | 声明之前什么都不可达；可选的网络策略由依赖图生成；cosign 签名的组件只用 Go 标准库验证 | 默认更小的爆炸半径，信任锚在**你自己的**项目里 |

每个想法的通俗介绍、代价，以及 BrickKit 怎么对待它，见
[设计原则与取舍](docs/zh/06-architecture/01-design-principles.md#认识这些想法)。

## 如果你在用 AI 写代码

BrickKit 的组件模型天然适合 AI 辅助开发。

实测项目自带的 10 个真实组件，代码行数在 200 到 3500 行之间。这种极小的上下文
规模，加上明确的 `component.yaml` 契约边界，意味着 AI 可以一次性完整读取并理解
整个组件，不需要在庞大的单体代码库中迷失。

环境变量注入意味着 AI 永远不需要处理服务发现或配置中心的复杂性；精确版本 +
多版本共存意味着 AI 生成的 v2 可以和 v1 安全并存，不会搞坏依赖 v1 的其他组件。

完整的道理和一步一步的工作流，见 [AI 辅助开发](docs/zh/05-ai-development.md)。

---

## 架构一瞥

```mermaid
graph LR
    subgraph 开发者
        A[写组件] --> B[brickkit add]
        B --> C[brickkit up]
    end

    subgraph CLI
        C --> D[解析依赖]
        D --> E[拓扑排序]
        E --> F[注入环境变量]
        F --> G[生成部署文件]
    end

    subgraph 运行环境
        G --> H[docker compose up / kubectl apply]
        H --> I[组件 A]
        H --> J[组件 B]
        H --> K[组件 C]

        I <-->|DNS 直连| J
        J <-->|DNS 直连| K
    end

    subgraph 基础设施
        I --> L[(PostgreSQL)]
        J --> L
        K --> M[(Redis)]
    end
```

---

## 安装

CLI 是一个**单文件** Go 二进制，装它不需要任何运行时。它不常驻、不写全局配置——
所有状态都在你项目目录的 `brickkit.yaml` 与 `.brickkit/` 里，真正干活时调用你
机器上的 `docker` / `kubectl`。

### 方式一：一行装进终端（推荐，不需要 Go）

```bash
curl -fsSL https://raw.githubusercontent.com/brickKit/brickKit/main/install.sh | sh
```

脚本认出你的系统与架构，下对应的包，**校验 sha256 对不上就拒绝安装**，装进
`/usr/local/bin`（不可写则退到 `~/.local/bin` 并提示 PATH）。

不想走管道，先下再看再跑也一样：

```bash
curl -fsSLO https://raw.githubusercontent.com/brickKit/brickKit/main/install.sh
less install.sh && sh install.sh
```

装指定版本用 `BRICKKIT_VERSION=v0.7.1`，装到别处用 `BRICKKIT_INSTALL_DIR=...`。

### 方式二：`go install`（有 Go 的话）

```bash
go install github.com/brickkit/brickkit/cmd/brickkit@latest
```

装到 `$(go env GOPATH)/bin`（默认 `~/go/bin`）。它不走 Makefile，所以拿不到
注入的版本号，`brickkit version` 会显示 `v0.0.0-dev` —— 想要真版本号就用方式
一或方式三。

### 方式三：从源码构建

```bash
git clone https://github.com/brickKit/brickKit.git
cd brickKit
make build-cli                 # 产出 bin/brickkit
sudo install -m 0755 bin/brickkit /usr/local/bin/brickkit
```

或者 `make install` 装到 GOBIN —— 与方式二同一个位置，但版本号、commit、构建
时间都注入进了二进制。

### 验证

```bash
brickkit version
```

```
BrickKit CLI v0.7.1
支持 Manifest 版本：brickkit/v1
支持部署目标：docker, k8s
```

> stderr 上那串 JSON 是结构化日志，不影响正常输出，嫌吵加 `--log-level off`。

### 切换 CLI 的语言

CLI 默认说英文。想让它说中文：

```bash
brickkit lang set zh                # 从此在这台机器上都说中文
BRICKKIT_LANG=zh brickkit status    # 只管这一次
brickkit lang                       # 现在生效的是哪种语言，为什么
```

`BRICKKIT_LANG` 优先于保存下来的设置，保存下来的设置优先于默认的英文。
错误码、命令名、参数名不随语言变化。
详见 [`brickkit lang`](docs/zh/06-architecture/09-cli-reference.md#brickkit-lang)。

### 还需要什么

| | 什么时候要 |
| --- | --- |
| Docker 20.10+（含 Compose V2） | `brickkit up` 起本地容器时 |
| kubectl + 一个集群（minikube 够用） | `deploy.target: k8s` 时 |
| Go 1.22+ | **只有方式二、三**要；方式一不需要（除非组件本身是 Go 写的） |
| [cosign](https://github.com/sigstore/cosign) | **只有发布方**签名时；验签用 Go 标准库，装 CLI 的人不需要 |

> **Windows：** 有 `windows/amd64` 的 zip，[Releases](https://github.com/brickKit/brickKit/releases)
> 页面手动下。但它只验过不需要 Docker 的那部分命令 —— 起容器和 K8s 那条线在
> Windows 上**没验过**，不是不支持，是没验过。
>
> **还没有 Homebrew / Scoop / apt 包。** 它们都是 Releases 的下游，先把上游做出来。

---

## 卸载

```bash
rm "$(command -v brickkit)"
```

没有全局配置要清 —— 删掉项目目录就等于删干净了。

---

## AI-ready 的文档体系

每一份文档都有对应的 AI 可读索引（`llms.txt`），整个平台可以压缩进一个文件供
AI 理解（`AGENTS.md`），`brickkit init` 自动为你的项目生成 AI 助手技能文件
（`.claude/skills/`）。

当你用 AI 辅助开发组件时，AI 不需要读完你的整个代码库——它只需要读当前组件的
Manifest 和依赖方的 API 契约，就能写出一个完整的、可独立运行的组件。

---

## 接下来去哪

下面每一条都是可以直接点开的链接——在 GitHub 上就能读，不需要克隆仓库。
这些是 `blob/main` 链接，是给浏览器用的：用 HTTP 直接抓取会拿到一整个 GitHub
页面（几百 KB 的 HTML），不是文档的纯文本。**如果你是 AI，需要拿到文件的真实
内容，别抓这些链接——改用 [`llms.zh.txt`](llms.zh.txt)**（英文文档树有自己的
[`llms.txt`](llms.txt)）：同一份索引，但每条链接都是可以直接抓取的 raw 链接。

更想按主题找文档而不是往下翻这一页？[`docs/README.md`](https://github.com/brickKit/brickKit/blob/main/docs/README.md) 是一份进入两棵语言树的短导航页。

**按阅读顺序**——`docs/` 下文件夹和文件名前面的编号就是推荐的顺序，每个文件夹里的 README 会把它讲清楚。

| 编号 | 文档 | 你会得到什么 |
| --- | --- | --- |
| 00 | [Quick Start](https://github.com/brickKit/brickKit/blob/main/docs/zh/00-quick-start.md) | 5 分钟，从空目录到一个可以 curl 通的容器，每一步都真跑过 |
| 01 | [核心概念](https://github.com/brickKit/brickKit/blob/main/docs/zh/01-concepts.md) | 一页术语表，加上那条贯穿一切的命名规则 |
| 02 | [和现有方案对比](https://github.com/brickKit/brickKit/blob/main/docs/zh/02-comparison.md) | BrickKit 和 Compose、Helm、Kustomize 等在哪里重叠，又在哪里不重叠 |
| 03 | [动手教程](https://github.com/brickKit/brickKit/blob/main/docs/zh/03-guide/README.md) | 13 篇教程，每一篇都对着真实的 CLI 真跑过 |
| 04 | [用 Go 写一个组件](https://github.com/brickKit/brickKit/blob/main/docs/zh/04-go-component-template.md) | 带读一个带数据库的、有测试覆盖的真实组件 |
| 05 | [AI 辅助开发](https://github.com/brickKit/brickKit/blob/main/docs/zh/05-ai-development.md) | 组件模型为什么适合 AI 写代码，以及具体怎么用 |
| 06 | [架构](https://github.com/brickKit/brickKit/blob/main/docs/zh/06-architecture/README.md) | 平台怎么工作、为什么这样设计，以及字段、命令、错误码的完整参考 |
| 07 | [推荐实践](https://github.com/brickKit/brickKit/blob/main/docs/zh/07-patterns/README.md) | 来自真实部署验证过的做法 |
| 08 | [故障排除](https://github.com/brickKit/brickKit/blob/main/docs/zh/08-troubleshooting.md) | 症状 → 原因 → 解决 |
| 09 | [Market API](https://github.com/brickKit/brickKit/blob/main/docs/zh/09-market-api.md) | 市场的每一个 HTTP 接口 |

只想查某个命令怎么用？看[命令一览](https://github.com/brickKit/brickKit/blob/main/docs/zh/06-architecture/09-cli-reference.md#命令一览)。

---

## 仓库结构

```
cmd/brickkit/          CLI 入口
cmd/gen-schemas/       重新生成 schemas/*.json（make generate-schemas）
internal/              CLI 实现
  ├── config/            brickkit.yaml 解析与校验
  ├── manifest/          component.yaml 解析与校验
  ├── schemagen/         从这两个结构体生成 JSON Schema
  ├── resolver/          依赖解析、拓扑排序
  ├── shell/             servedBy 分组与合并，compose 和 k8s 渲染器共用
  ├── cascade/           启停判定：算出这次实际启动谁（跟着上层走）
  ├── inject/            环境变量注入与资源配额合并
  ├── compose/           docker-compose.yaml 生成
  ├── k8s/               Kubernetes 清单生成
  ├── engine/            docker compose / kubectl 驱动
  ├── source/            安装源：market / git / local
  ├── security/          cosign 签名与标准库验签
  └── workspace/         组件源码工作区（--repo / sync）
market-server/         组件市场后端（独立 Go module）
schemas/               component.yaml 与 brickkit.yaml 的 JSON Schema（生成出来并签入仓库）
tests/components/      10 个真实组件，用来测试平台本身
tests/checklist/       验收清单 → 证明它们的测试
deploy/market/         市场的 compose / kustomize / Helm
docs/en/               英文文档：architecture、guide、patterns
docs/zh/               中文文档：architecture、guide、patterns（与英文对称镜像，不是英文的译本）
docs/archive/          历史记录，不属于现行文档
```

单元测试**紧挨着被测代码**（`internal/**/*_test.go`），不建平行目录。`tests/`
只放没法放在旁边的：清单、基准，以及当夹具用的组件。

---

## 构建与测试

```bash
make build            # bin/brickkit + bin/market-server
make test             # 单元测试
make test-all         # 全部测试套件
make lint             # vet + 文档检查
```

一组检查持续运行，而且**每一道坏掉时都会大声报错**，而不是安静地报告零问题：

| 命令 | 守住什么 |
| --- | --- |
| `make test-regression` | 面向用户的承诺 → 证明它们的测试（`tests/regression/清单.tsv`） |
| `make test-boundary` 等 | 边界 / 错误 / 兼容 / 安全验收条目 → 证明它们的测试（`tests/checklist/清单.tsv`） |
| `make check-doc-fields` | 文档里画的 yaml 片段与字段表，字段名都真的存在（真相来源是结构体本身）；文档里引用的报错标题和带符号的 CLI 输出行，都是文档所在语言里真实存在的文案 |
| `make check-schemas` | `schemas/` 里的 JSON Schema 与 config、manifest 两个结构体生成出来的结果一字不差，schema 里写的必填字段、封闭取值、正则与范围也与真实校验器一致（重新生成用 `make generate-schemas`） |
| `make check-docs` | 悬空的小节引用与断链 |
| `make check-cli-docs` | 文档里写的每条命令 / 参数都真的存在（反过来——新增了命令却还没写进文档——这里不管） |
| `make check-guides` | 试用指南里的步骤仍然跑得通 |
| `make check-guide-output` | 教程（`docs/{en,zh}/03-guide/`）里嵌的 CLI 输出块，逐行对得上真实输出——docs/en 对 CLI 的英文输出，docs/zh 对它的中文输出 |
| `make check-i18n` | 生产代码里没有写死的中文、测试里没有中文短语的否定断言（对英文输出它们永远空转成立）、英文的每个单复数形式都配齐；外加 `tools/i18n/` 里那套一次性迁移工具的测试 |
| `make check-install-sh` | `install.sh` 装得上，而且校验和坏掉时**真的**拒绝装 |
| `make check-docs-bilingual` | docs/en 与 docs/zh 保持镜像、根目录每一对多语言文件（README、AGENTS、llms）都保持成对、llms.txt/llms.zh.txt 里每条链接都能解析到真实文件、docs/en 里没有中文 |

一份指向已不存在的测试的清单会让构建失败。一个目录变空的测试目标同样会 ——
**安静跳过的套件比没有套件更糟**，因为它还占着计分板上的一行。

---

## 项目状态

计划内的每一步都已完成，延后项也已全部结清。后续行为以 `tests/checklist/`
与 `tests/regression/` 下的两份活的清单为准，文档以 `docs/en/` 与 `docs/zh/` 为准。

| | |
| --- | --- |
| 测试 | 2000+ 个测试函数，race-clean |
| 试用指南（现行） | 13 篇，每一篇都真跑过；见 `docs/zh/03-guide/` |
| 试用指南（已归档） | 23 篇，全部对着真实 Docker / Kubernetes / 活的市场跑过 |
| 设计书（已归档） | 14 本，与实现交叉复核过两轮 |
| 决策记录（已归档） | 566 条，每条都带当初的推理 |

**运行要求：** Go 1.22+、Docker 20.10+（含 Compose V2）。Kubernetes 相关指南
需要 minikube；签名需要 [cosign](https://github.com/sigstore/cosign)（**仅
发布方** —— 验签用 Go 标准库）。

---

<div align="center">

[Apache License 2.0](LICENSE)

</div>
