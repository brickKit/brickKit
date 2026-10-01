# 三层架构总览

## 目录结构

一个项目的全部文件，每一项在做什么：

```text
my-shop/
├── brickkit.yaml          声明：安装源、组件与精确版本（锁文件，进 Git）
├── deploy.yaml            团队的部署文件：target、组件怎么跑（进 Git）
├── deploy.local.yaml      你个人的部署文件（brickkit local on 生成，不进 Git）
├── deploy.prod.yaml       另一个环境的部署文件（可选，用 -f 指定，进 Git）
├── config/
│   ├── vars.yaml          公共变量：多个组件共用的值（进 Git）
│   ├── erp-backend.yaml   组件 erp/backend 默认版本的配置（进 Git）
│   ├── erp-backend@1.0.0.yaml   只给 1.0.0 这个版本用的配置（多版本共存时）
│   └── .archive/          remove 时归档的配置，重新 add 时恢复（默认不进 Git）
├── components/            本地安装源：组件源码（各自是独立的 Git 仓库，默认不进项目的 Git）
├── shell/                 本地安装源：外壳组件
├── AGENTS.md              项目的 AI 导读；末尾的组件表由 CLI 维护（进 Git）
├── CLAUDE.md              只有一行 @AGENTS.md，让 Claude Code 也读 AGENTS.md（进 Git）
├── .claude/skills/        AI 助手技能 brickkit-*（进 Git）
├── .env                   本机的环境变量值，${VAR} 从这里取（不进 Git）
├── .secrets/              file:// 引用的密钥文件（不进 Git）
└── .brickkit/             CLI 的缓存与生成物（不进 Git）
    ├── manifests/         每个组件版本的 component.yaml、BRICKKIT.md 及其译本（永久缓存）
    ├── artifacts/         下载的契约文件
    ├── generated/         生成的 compose.yaml / K8s 清单 / 0600 的 env 文件
    └── local-mode         本地模式开关的状态
```

## 每类信息归哪个文件

| 你要写的是 | 归属 | 例子 |
| --- | --- | --- |
| 项目用哪些组件、什么版本 | `brickkit.yaml` | `components: [{id: erp/backend, version: 1.2.0}]` |
| 去哪里找组件 | `brickkit.yaml` | `sources: [{name: org, type: git, baseUrl: …}]` |
| 部署到哪（Docker / Podman / K8s） | 部署文件 | `target: k8s` |
| 某个组件跑不跑、怎么跑 | 部署文件 | `mode: disable`、`expose: true`、`replicas: 3` |
| 谁是外壳、它承载哪些成员 | 部署文件（`members`）；`brickkit.yaml` 里只有 CLI 维护的 `kind: shell` 标记 | `members: [{id: erp/api}]` |
| K8s 的命名空间、网络策略 | 部署文件的 `k8s:` 块 | `k8s: {namespace: shop}` |
| 组件拿到的环境变量 | `config/<组件>.yaml` | `DB_HOST: pg.internal` |
| 多个组件共用的值 | `config/vars.yaml` | `PG_HOST: pg.internal` |
| 只在某个环境不同的公共值 | 那个环境的部署文件的 `vars:` | `vars: {PG_HOST: pg.prod}` |

## 判断一个字段归哪里的四个问题

1. 它在说"**这个项目由什么组成**"吗？→ `brickkit.yaml`
2. 它在说"**怎么部署、怎么编排**"吗？→ 部署文件
3. 它是"**组件进程要读的环境变量**"吗？→ `config/<组件>.yaml`
4. 它是"**好几个组件都要用的同一个值**"吗？→ `config/vars.yaml`，各组件用 `$var:` 引用

判断不清的时候，想想谁会改它、多久改一次：组件版本由负责装配的人改、要评审；部署方式因环境而异；
配置里有密钥。三件事放在一个文件里，最后谁都改它、谁都读不懂它——这就是"职责单一"这条原则。

## 该读哪个文件

| 想知道什么 | 读哪个文件 |
| --- | --- |
| 项目里有哪些组件、什么版本 | `brickkit.yaml` |
| 某个组件这次怎么部署 | 本地模式开着时读 `deploy.local.yaml`，否则 `deploy.yaml`；命令用了 `-f` 就是那份 |
| 某个组件拿到哪些环境变量 | `config/<组件>.yaml`，里面 `$var:` 引用的值在 `config/vars.yaml` 或部署文件的 `vars:` |
| 某个组件需要哪些配置、有什么依赖 | 它的 `component.yaml`：`.brickkit/manifests/<scope>/<name>/<版本>/` 下，本地源组件在源码目录里 |
| 某个组件怎么用 | 它的 `BRICKKIT.md`（译本叫 `BRICKKIT.<语言>.md`），位置同上 |
| 团队约定，以及项目里有哪些组件、文档在哪 | 项目根的 `AGENTS.md`：前面是作者写的几节，末尾是组件表 |

**为什么没有"合并视图"命令**（一条命令把三层合成一份给你看）：它会成为第四份需要理解、需要信任的东西，
而且总有一天和真正生效的文件对不上。每个问题只有一个文件负责回答，按需去读那一个——这是"按需检索优于全量合并"。
真正生成出来的结果（`compose.yaml`、K8s 清单）在 `.brickkit/generated/` 里，`brickkit up --dry-run`
只生成不执行，想看最终效果看那里。

## 什么进 Git、什么不进

| 文件 | 进 Git | 原因 |
| --- | --- | --- |
| `brickkit.yaml` | ✅ | 项目由什么组成，是要评审的决定 |
| `deploy.yaml`、`deploy.<环境>.yaml` | ✅ | 团队怎么部署，是共享的决定 |
| `config/`（含 `vars.yaml`） | ✅ | 配置是项目的一部分；密钥不要直接写进去，用 `${VAR}` 或 `file://` |
| `config/.archive/` | ❌ | 已移除组件的旧配置，只为以后重新 `add` 时恢复用，属于本机 |
| `components/` | ❌（默认） | 里面每个组件是独立的 Git 仓库，有自己的历史；要让源码随项目一起提交，`brickkit init --hooks` 装上提交前检查 |
| `AGENTS.md`、`CLAUDE.md` | ✅ | 团队写的项目 AI 导读；末尾的组件表只放每台机器上都一样的事实 |
| `.claude/skills/` | ✅ | AI 助手技能；每份文件最后一行记着是哪个版本的 CLI 写的，同事新克隆下来也分得清有没有被改过 |
| `deploy.local.yaml` | ❌ | 个人的临时部署方式（"我正在调这个组件"），不是团队决定 |
| `.env`、`.secrets/` | ❌ | 本机的密钥与环境变量值、`file://` 引用的密钥文件 |
| `.brickkit/` | ❌ | 缓存与生成物，随时可以由 CLI 重新取回、重新生成 |

`brickkit init` 生成的 `.gitignore` 已经写好了这些条目；在已有目录里补全式 `init` 时，`.gitignore` 缺必需条目
会大声警告——缺了它们，个人文件和密钥会被提交。
