<div align="center">

# BrickKit

</div>

[English](README.md) | [中文](README.zh.md)

<div align="center">

**像搭积木一样构建系统。**

一个组件管理与拼装平台 —— 组件独立开发、独立部署、独立调用、按需拼装。

</div>

---

> ## 🤖 给 AI 助手的指令
>
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

---

## BrickKit 是什么

BrickKit **不是**操作系统，不是 ERP，也不是任何一个具体的业务软件。它是一个
让你**渐进式**长出架构的平台：先写一个小组件跑通，再写一个跑通，然后写一个
连接组件把它们串起来。像搭积木一样，最终拼出任何你需要的系统。

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

**它给你什么：**

- **渐进式** —— 不需要一次性设计完整系统，一块积木一块积木地加
- **语言无关** —— 任何语言只要能构建 Docker 镜像，就能成为组件
- **环境一致** —— 本地（Docker）与生产（K8s）用**同一套地址格式**，组件代码零修改
- **平台不挡路** —— 业务逻辑、通信治理、多租户全部归组件，不归平台

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

装指定版本用 `BRICKKIT_VERSION=v0.1.0`，装到别处用 `BRICKKIT_INSTALL_DIR=...`。

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
BrickKit CLI v0.1.0
支持 Manifest 版本：brickkit/v1
支持部署目标：docker, k8s
```

> stderr 上那串 JSON 是结构化日志，不影响正常输出，嫌吵加 `--log-level off`。

### 还需要什么

| | 什么时候要 |
| --- | --- |
| Docker 20.10+（含 Compose V2） | `brickkit up` 起本地容器时 |
| kubectl + 一个集群（minikube 够用） | `deploy.target: k8s` 时 |
| Go 1.22+ | **只有方式二、三**要；方式一不需要（除非组件本身是 Go 写的） |
| [cosign](https://github.com/sigstore/cosign) | **只有发布方**签名时；验签用 Go 标准库，装 CLI 的人不需要 |

> **Windows：** 有 `windows/amd64` 的 zip，[Releases](https://github.com/brickKit/brickKit/releases)
> 页面手动下。但它只验过不需要 Docker 的那部分命令 —— 起容器和 K8s 那条线在
> Windows 上**没验过**，不是不支持，是没验过。详见
> [docs/archive/planning/发布与分发.md](docs/archive/planning/发布与分发.md) §3.1
> （历史存档）。
>
> **还没有 Homebrew / Scoop / apt 包。** 它们都是 Releases 的下游，先把上游做出来。

---

## 卸载

```bash
rm "$(command -v brickkit)"
```

没有全局配置要清 —— 删掉项目目录就等于删干净了。

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

**命令共 13 条：** `init` `add` `remove` `fetch` `up` `down` `status` `sync`
`restore` `login` `logout` `publish` `version`

---

## 它刻意不做的事

这份清单和上面的功能同样重要 —— 它们不是「还没做」，而是**被论证过并拒绝**的：

| 不做 | 替代方案 |
| --- | --- |
| 注册中心 / 地址簿 | Docker DNS / K8s Service DNS |
| 常驻服务 / 控制面 | CLI 用完即走，状态外置到 `brickkit.yaml` + 底层引擎 |
| 健康检查轮询 | K8s Probe / Compose healthcheck + 重启策略 |
| API 网关 / 服务网格 | 组件之间 DNS 直连 |
| 配置中心 / 动态热更新 | 环境变量注入，改配置就重启 |
| 熔断 / 限流 / 降级 | 组件自己的业务代码 |
| 版本范围（`^1.0.0`） | 只接受精确版本，杜绝隐式升级 |
| 多环境 overlay 继承 | 每个环境一份完整自包含的配置 |
| 第三方组件安全审查 | 安装即信任，事后 `blocked` 下架 |

> **平台只做「连接器」和「翻译官」，绝不越界去做「业务逻辑」和「基础设施」已经
> 做好的事情。**

每一条的完整论证见
[`docs/zh/architecture/`](https://github.com/brickKit/brickKit/tree/main/docs/zh/architecture)。

---

## 接下来去哪

下面每一条都是可以直接点开的链接——在 GitHub 上就能读，不需要克隆仓库。
这些是 `blob/main` 链接，是给浏览器用的：用 HTTP 直接抓取会拿到一整个 GitHub
页面（几百 KB 的 HTML），不是文档的纯文本。**如果你是 AI，需要拿到文件的真实
内容，别抓这些链接——改用 [`llms.zh.txt`](llms.zh.txt)**（英文文档树有自己的
[`llms.txt`](llms.txt)）：同一份索引，但每条链接都是可以直接抓取的 raw 链接。

**架构——平台到底怎么工作，配真实代码和真实生成出来的输出**

| 文档 | 讲什么 |
| --- | --- |
| [架构总览](https://github.com/brickKit/brickKit/blob/main/docs/zh/architecture/overview.md) | 一次声明怎么变成运行中的容器——完整的真实流水线 |
| [依赖解析与启动顺序](https://github.com/brickKit/brickKit/blob/main/docs/zh/architecture/dependency-resolution.md) | 一个真实的菱形依赖、一个真实的循环依赖，以及为什么真正决定 `up` 要跑多久的是最长依赖链而不是组件数量 |
| [部署文件是怎么生成出来的](https://github.com/brickKit/brickKit/blob/main/docs/zh/architecture/deployment-generation.md) | 同一个项目分别为 Docker 和 Kubernetes 生成出来的文件，逐字节对照 |
| [资源绑定的实际机制](https://github.com/brickKit/brickKit/blob/main/docs/zh/architecture/resource-binding.md) | 资源绑定撞车时到底会发生什么、配额链到底怎么合并 |
| [签名与信任模型](https://github.com/brickKit/brickKit/blob/main/docs/zh/architecture/signing-and-trust.md) | 真正被签名的是什么，以及公钥为什么永远不能来自市场 |

**动手教程——12 篇，每一篇都对着真实 CLI 跑过，按顺序读**

| # | 文档 | 讲什么 |
| --- | --- | --- |
| 1 | [把一个项目跑起来](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/01-first-project.md) | init、add、up、curl 它、改配置、down |
| 2 | [平台是怎么决定谁跑起来的](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/02-what-runs.md) | 依赖、`enabled` 级联、`--dry-run` |
| 3 | [本地调试一个组件](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/03-local-debugging.md) | `local: true`，带断点调试 |
| 4 | [部署到 Kubernetes](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/04-kubernetes.md) | 真实的 minikube 部署，外加一个真实的 `brickkit down` 坑 |
| 5 | [升级，以及让多个版本并存](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/05-upgrades-and-versions.md) | 版本升级，以及故意让两个版本并存 |
| 6 | [拼装一个真实的系统，然后故意把它弄坏](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/06-assemble-and-break.md) | 一个真实数据库，两种真正不同的真实失败模式 |
| 7 | [消费别人的组件](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/07-consuming-artifacts.md) | 产物、API 文档、`brickkit fetch` |
| 8 | [从市场发布与安装](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/08-marketplace.md) | 真实市场、版本不可变性、私有可见性 |
| 9 | [给组件签名与验签](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/09-signing.md) | 真实的 cosign 密钥对、一次真实的验签失败 |
| 10 | [从零开发自己的第一个组件](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/10-build-your-own.md) | 四个文件，从零到真正跑起来 |
| 11 | [网络策略与最小权限](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/11-network-policy.md) | Kubernetes 上真实生效的 NetworkPolicy |
| 12 | [多项目共享](https://github.com/brickKit/brickKit/blob/main/docs/zh/guide/12-multi-project-sharing.md) | 共享资源、隔离资源、把一个组件当成别人的 API |

**Patterns——推荐实践，可选，对着真实部署验证过**

| 文档 | 讲什么 |
| --- | --- |
| [基于 BrickKit 的组件该怎么分层测试](https://github.com/brickKit/brickKit/blob/main/docs/zh/patterns/testing.md) | 怎么分层：契约、业务规则、单元、集成 |
| [怎么规划种子数据与测试数据](https://github.com/brickKit/brickKit/blob/main/docs/zh/patterns/data-construction.md) | 两条必须物理隔离的路径，以及为什么 |
| [组件设计准则](https://github.com/brickKit/brickKit/blob/main/docs/zh/patterns/component-design.md) | 怎么做领域研究，什么时候该做成组件家族而不是开关 |
| [合格外壳该满足什么](https://github.com/brickKit/brickKit/blob/main/docs/zh/patterns/shell-implementers-guide.md) | `servedBy` 对收编组件的外壳提出了什么要求 |
| [怎么声明 servedBy：部署方检查清单](https://github.com/brickKit/brickKit/blob/main/docs/zh/patterns/servedby-deployment-checklist.md) | 写给声明 `servedBy` 的人，不是造壳的人 |
| [自己搭一套 BrickKit Market](https://github.com/brickKit/brickKit/blob/main/docs/zh/patterns/deployment/self-hosted-market.md) | 部署市场本身，从本地开发到生产环境 |
| [在外壳里合并数据库连接池](https://github.com/brickKit/brickKit/blob/main/docs/zh/patterns/shared-connection-pools.md) | 合并进同一个壳、又共用 PostgreSQL 或 Oracle 的组件该怎么办 |
| [闭源组件的镜像安全规范](https://github.com/brickKit/brickKit/blob/main/docs/zh/patterns/closed-source-image-hardening.md) | 拉取镜像跟私有 Git 仓库不是同一种保证 |

以上都没覆盖到的：[`docs/archive/decisions/`](docs/archive/decisions/) 讲清楚某个历史决策当初为什么
这么定（566 条，历史存档，仅中文）。

---

## 仓库结构

```
cmd/brickkit/          CLI 入口
internal/              CLI 实现
  ├── config/            brickkit.yaml 解析与校验
  ├── manifest/          component.yaml 解析与校验
  ├── resolver/          依赖解析、拓扑排序
  ├── cascade/           启停判定：算出这次实际启动谁（跟着上层走）
  ├── inject/            环境变量注入与资源配额合并
  ├── compose/           docker-compose.yaml 生成
  ├── k8s/               Kubernetes 清单生成
  ├── engine/            docker compose / kubectl 驱动
  ├── source/            安装源：market / git / local
  ├── security/          cosign 签名与标准库验签
  └── workspace/         组件源码工作区（--repo / sync）
market-server/         组件市场后端（独立 Go module）
tests/components/      10 个真实组件，用来测试平台本身
tests/checklist/       验收清单 → 证明它们的测试
deploy/market/         市场的 compose / kustomize / Helm
docs/en/               英文文档：architecture、guide、patterns
docs/zh/               中文文档：architecture、guide、patterns（与英文对称镜像，不是英文的译本）
docs/archive/          重构前的旧文档，仅作历史记录保留
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
| `make check-doc-fields` | 文档里画的 yaml 片段与字段表，字段名都真的存在（真相来源是结构体本身） |
| `make check-docs` | 悬空的小节引用与断链 |
| `make check-cli-docs` | 文档里写的每条命令 / 参数都真的存在（反过来——新增了命令却还没写进文档——这里不管，见 `docs/archive/planning/` 那段历史） |
| `make check-guide-output` | 试用指南的「✅ 预期」与 CLI 真实输出逐行一致 |
| `make check-guides` | 试用指南里的步骤仍然跑得通 |
| `make check-install-sh` | `install.sh` 装得上，而且校验和坏掉时**真的**拒绝装 |
| `make check-docs-bilingual` | docs/en 与 docs/zh 保持镜像、根目录每一对多语言文件（README、AGENTS、llms）都保持成对、llms.txt/llms.zh.txt 里每条链接都能解析到真实文件 |

一份指向已不存在的测试的清单会让构建失败。一个目录变空的测试目标同样会 ——
**安静跳过的套件比没有套件更糟**，因为它还占着计分板上的一行。

---

## 项目状态

计划内的每一步都已完成，延后项也已全部结清；旧的开发计划与开发进度已冻结为
历史记录，分别存放在
[`docs/archive/planning/`](docs/archive/planning/) 与
[`docs/archive/decisions/`](docs/archive/decisions/)。后续行为以 `tests/checklist/`
与 `tests/regression/` 下的两份活的清单为准 —— 曾经承担这个角色的设计书，
已被 `docs/en/architecture/` 与 `docs/zh/architecture/` 取代（旧文原样保留、
只读，存放在 [`docs/archive/design/`](docs/archive/design/)）。

| | |
| --- | --- |
| 测试 | 1762 个测试函数，race-clean |
| 试用指南 | 23 篇，全部对着真实 Docker / Kubernetes / 活的市场跑过 —— 已归档到 `docs/archive/guide/`，由 `docs/{en,zh}/guide/` 取代 |
| 设计书 | 14 本，与实现交叉复核过两轮 —— 已归档到 `docs/archive/design/`，由 `docs/{en,zh}/architecture/` 取代 |
| 决策记录 | 566 条，每条都带当初的推理，归档在 `docs/archive/decisions/` |

**运行要求：** Go 1.22+、Docker 20.10+（含 Compose V2）。Kubernetes 相关指南
需要 minikube；签名需要 [cosign](https://github.com/sigstore/cosign)（**仅
发布方** —— 验签用 Go 标准库）。

---

<div align="center">

[Apache License 2.0](LICENSE)

</div>
