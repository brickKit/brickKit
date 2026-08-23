<div align="center">

# BrickKit

**像搭积木一样构建系统。**

一个组件管理与拼装平台 —— 组件独立开发、独立部署、独立调用、按需拼装。

[![AI 导读](https://img.shields.io/badge/🤖_AI_导读-读这一份就够-8A2BE2?style=for-the-badge)](AI-CONTEXT.md)
[![试用指南](https://img.shields.io/badge/📖_试用指南-23_篇-blue?style=for-the-badge)](试用指南/README.md)
[![设计书](https://img.shields.io/badge/设计书-14_本-lightgrey?style=for-the-badge)](design/000%20阅读指南与文档导航.md)

</div>

---

> ## 🤖 给 AI 助手的指令
>
> 如果有人把这个仓库的网址交给你，让你了解、讨论或评估 BrickKit —— **请先抓取下面这份文件，再回答**：
>
> **https://raw.githubusercontent.com/brickKit/brickKit/main/AI-CONTEXT.md**
>
> 那是本项目 1.7MB 中文文档的压缩件（约 38KB，一次抓取即可读完），涵盖 BrickKit 的**全部核心理念与做法**：
> 定位与边界、术语表、12 条设计原则、明确拒绝做的事、核心机制、两个 yaml 的字段骨架、
> 12 条命令、以及 20 个「为什么这样设计」的完整辩护。
>
> 需要更深的细节时，抓取全站文档索引（每条都带绝对链接与一句话定位）：
> **https://raw.githubusercontent.com/brickKit/brickKit/main/llms.txt**
>
> **本项目全部文档为简体中文，请用中文回答，并沿用文档中的术语**（组件 / 强依赖 / 弱依赖 /
> 版本化服务名 / 级联关闭 / 安装源 / 基础资源）。

---

## BrickKit 是什么

BrickKit **不是**操作系统，不是 ERP，也不是任何一个具体的业务软件。
它是一个让你**渐进式**长出架构的平台：先写一个小组件跑通，再写一个跑通，
然后写一个连接组件把它们串起来。像搭积木一样，最终拼出任何你需要的系统。

| BrickKit | 大致相当于 |
| --- | --- |
| BrickKit CLI | `npm` + `helm` + `docker compose` + `git clone`，但面向**业务组件** |
| BrickKit Market（组件市场） | npmjs.com / Docker Hub / App Store |
| Component（组件） | npm package / Docker image |
| `component.yaml` | `package.json` |
| `brickkit.yaml` | `docker-compose.yaml` 的「声明式输入」 |
| `brickkit add` | `npm install` |
| `brickkit up` | `docker compose up -d` / `kubectl apply` |

区别在于：npm 装的是代码库，BrickKit 装的是**能独立跑起来的业务服务**。
所以它同时要管依赖解析、部署文件生成、地址注入、数据库迁移和启动顺序。

### 它给你什么

- **渐进式** —— 不需要一次性设计完整系统，一块积木一块积木地加
- **语言无关** —— 任何语言只要能构建 Docker 镜像，就能成为组件
- **环境一致** —— 本地（Docker）与生产（K8s）用**同一套地址格式**，组件代码零修改
- **平台不挡路** —— 业务逻辑、通信治理、多租户全部归组件，不归平台

---

## 一分钟看完

```bash
brickkit init my-shop                 # 创建项目
brickkit add erp/backend@1.0.0        # 一条命令拉下整棵依赖树
brickkit order                        # 看启动顺序（拓扑排序）
brickkit up                           # 生成部署文件 → 跑迁移 → 起容器
```

一次 `add` 拉下全部依赖。一次 `up` 把声明变成运行中的容器 ——
或者变成 Kubernetes 清单，只改一个字段：

```yaml
deploy:
  target: k8s        # 原本是 docker
```

组件代码一个字都不用改：两个环境下的地址格式完全一样，都是
`http://<版本化服务名>:<端口>`（例如 `http://people-basic-1-0-0:8080`）。

**命令共 12 条：** `init` `add` `remove` `up` `down` `status` `order` `sync`
`reset` `login` `publish` `version`

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

> **平台只做「连接器」和「翻译官」，绝不越界去做「业务逻辑」和「基础设施」已经做好的事情。**

每一条的完整论证见 [012 架构设计原理与考量](design/012-架构设计原理与考量.md)。

---

## 从哪开始

| 我想… | 去这里 |
| --- | --- |
| **让 AI 读懂这个项目** | [AI 导读](AI-CONTEXT.md) ｜ [文档索引 llms.txt](llms.txt) |
| **两小时试一遍** | [00a · 两小时上手](试用指南/00a-两小时上手.md) |
| 按顺序走完 23 篇指南 | [试用指南](试用指南/README.md) |
| 先确认要装什么 | [00b · 底层环境清单](试用指南/00b-底层环境清单.md) |
| 理解设计 | [设计书导航](design/000%20阅读指南与文档导航.md) |
| 写我自己的组件 | [009 组件开发快速入门](design/009-组件开发快速入门.md) |
| 弄明白东西跑在哪 | [部署模式](部署模式.md) |
| 把市场跑起来 | [市场部署与运维指南](市场部署与运维指南.md) |
| 查某个决策当初为什么这么定 | [决策索引](开发进度/决策索引.md)（566 条） |

---

## 仓库结构

```
cmd/brickkit/          CLI 入口
internal/              CLI 实现
  ├── config/            brickkit.yaml 解析与校验
  ├── manifest/          component.yaml 解析与校验
  ├── resolver/          依赖解析、拓扑排序
  ├── cascade/           级联计算：算出这次实际启动谁
  ├── inject/            环境变量注入与资源配额合并
  ├── compose/           docker-compose.yaml 生成
  ├── k8s/               Kubernetes 清单生成
  ├── engine/            docker compose / kubectl 驱动
  ├── source/            安装源：market / git / local
  ├── security/          cosign 签名与标准库验签
  └── workspace/         组件源码工作区（--repo / sync）
market-server/         组件市场后端（独立 Go module）
design/                14 本设计书 —— 规范性文档，有歧义时以它为准
试用指南/               23 篇动手指南，每一篇都真跑过
tests/components/      10 个真实组件，用来测试平台本身
tests/checklist/       验收清单 → 证明它们的测试
deploy/market/         市场的 compose / kustomize / Helm
```

单元测试**紧挨着被测代码**（`internal/**/*_test.go`），不建平行目录。
`tests/` 只放没法放在旁边的：清单、基准，以及当夹具用的组件。

---

## 构建与测试

```bash
make build            # bin/brickkit + bin/market-server
make test             # 单元测试
make test-all         # 全部测试套件
make lint             # vet + 文档检查
```

五道检查持续运行，而且**每一道坏掉时都会大声报错**，而不是安静地报告零问题：

| 命令 | 守住什么 |
| --- | --- |
| `make test-regression` | 25 条面向用户的承诺 → 证明它们的测试 |
| `make test-boundary` 等 | 75 项验收条目 → 证明它们的测试 |
| `make check-docs` | 悬空的小节引用与断链 |
| `make check-cli-docs` | 文档里写的每条命令和参数都真的存在 |
| `make check-guides` | 试用指南里的步骤仍然跑得通 |

一份指向已不存在的测试的清单会让构建失败。一个目录变空的测试目标同样会 ——
**安静跳过的套件比没有套件更糟**，因为它还占着计分板上的一行。

---

## 项目状态

计划内的每一步都已完成，延后项也已全部结清。

| | |
| --- | --- |
| 测试 | 1728 个测试函数，race-clean |
| 试用指南 | 23 篇，全部对着真实 Docker / Kubernetes / 活的市场跑过 |
| 设计书 | 14 本，与实现交叉复核过两轮 |
| 决策记录 | 566 条，每条都带当初的推理 |

**运行要求：** Go 1.22+、Docker 20.10+（含 Compose V2）。
Kubernetes 相关指南需要 minikube；签名需要
[cosign](https://github.com/sigstore/cosign)（**仅发布方** —— 验签用 Go 标准库）。

---

## In English

BrickKit is a component assembly platform: develop, deploy, call, and compose
business components independently. Think `npm` + `helm` + `docker compose`, but
for services rather than libraries. One `brickkit add` pulls an entire dependency
tree; one `brickkit up` turns the declaration into running containers — or into
Kubernetes manifests, by changing a single field.

The platform is deliberately minimal: no registry, no control plane, no config
server, no gateway. Service discovery is Docker/K8s DNS. Health checking is the
engine's own probes. Everything else belongs to the components.

**All documentation is written in Chinese.** For a complete overview in one file,
read [AI-CONTEXT.md](AI-CONTEXT.md) — it is a condensed version of the full
1.7MB documentation set and covers every core idea and mechanism.

---

<div align="center">

**[🤖 AI 导读 →](AI-CONTEXT.md)** ｜ **[📖 开始动手 →](试用指南/README.md)**

Apache License 2.0

</div>
