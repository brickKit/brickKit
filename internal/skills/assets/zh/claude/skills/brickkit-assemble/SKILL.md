---
name: brickkit-assemble
description: 在 BrickKit 项目里增删组件、升级组件版本、调整启停、启动或停止整套服务、查看运行状态和依赖树时使用。覆盖 add / remove / upgrade / fetch / sync / deps / up / down / status 的适用场景，brickkit.yaml 作为锁文件、默认版本与 requiredBy 兼容版本、多版本共存，写在 deploy.yaml / deploy.local.yaml 里的 mode「启停跟着上层走」规则，以及焦点运行（在大项目里只改一个组件，up --focus / --all）。当用户问「怎么把某个组件加进来 / 升级 / 关掉 / 为什么它没起来 / 谁依赖谁」时，这个技能适用。
---

# 拼装 BrickKit 项目

## 什么时候用这个技能

- 要把一个组件加进项目、从项目里移除，或者升到新版本
- 要让某些组件这次不启动
- `brickkit up` 起来的组件跟预期不一样
- 要看当前在跑什么、谁依赖谁
- 要整理 `components/` 下堆积的组件源码

## 你会猜错的地方

**1. 增删升级走命令，别手改三份文件。**

一个组件在项目里同时出现在三处：`brickkit.yaml` 的声明、部署文件（`deploy.yaml`，存在时
还有 `deploy.local.yaml`）的条目、`config/` 下的配置文件。`add` / `remove` / `upgrade`
一次把三处改齐，改完项目装载不了就全部还原。手改其中一处，下一次 `up` 就是
`DEPLOY_INCONSISTENT`。

**2. `brickkit.yaml` 是锁文件，不写依赖。**

它只列组件和精确版本。谁依赖谁写在各组件自己的 `component.yaml` 里，看依赖树用
`brickkit deps`（或 `brickkit graph` 出 Mermaid 图）。解析时**只用这里声明过的版本**：
强依赖要的版本没声明是错误，会提示你去 `brickkit add`；没声明的弱依赖就当不存在。

**3. 版本必须精确。** `1.2.0` 可以，`^1.2`、`~1.2`、`latest` 全都不行。
`brickkit add` 不写版本时取安装源里最新的版本（git 源就是最新 tag），然后**以精确版本落盘**。

**4. 默认版本是「没有 `requiredBy` 的那一行」。**

同一个组件 ID 可以有多行：一行默认版本，其余每行带 `requiredBy: [<谁要它>]`，是只因为
某个组件依赖它才留着的兼容版本（多版本共存）。不带版本的部署条目、不带版本的配置文件
`config/<scope>-<name>.yaml`、本地仓库，指的都是默认版本；兼容版本要用
`id@版本` 的条目和 `config/<scope>-<name>@<版本>.yaml`。

**5. 换默认版本用 `upgrade`，不是再 `add` 一次。**

直接 `add` 一个已在项目里的组件的另一个版本会报错并指向 `brickkit upgrade`。
依赖链上需要另一个版本时，`add` 会自己写 `requiredBy` 行，那是正常的。

`upgrade` 要点：不写组件就升级所有有新版本的；不写版本只往上升，不会降；旧版本还有人依赖
就留作兼容版本，否则移除（配置进 `config/.archive/`）。配置逐键迁移——你写过且新 schema
还有的键原样抄过去，没写过的键跟新默认值走。**冲突**（你写过、默认值变了、你的值又不是
旧默认值）在终端里逐条问；`--yes` 或没有终端时写成两行重复键，`up` 在你删掉一行之前拒绝启动
——这是故意的，不让冲突被悄悄吞掉。先 `brickkit upgrade --dry-run` 看结果，什么都不写。
任何一步失败，三份文件全部还原。

**6. `brickkit add` 不写 `mode`，也不填配置值。**

加进来的部署条目不带 `mode`——不写就是「跟着上层走」，那是默认且推荐的状态，别为了
「显式」补 `mode: enabled`（含义完全不同，见下一条）。`config/` 下会写好骨架：必填且没默认值
的键写成 `KEY: ""`，**必须填上**，否则 `up` 拒绝并点名那个键；有默认值的可选键写成注释行，
不取消注释就跟着组件默认值走。

**7. `mode` 写在部署文件的条目上，五种取值，四种是钉死的。**

| 写法 | 含义 |
| --- | --- |
| **不写** | 跟着上层走。顶层（项目里没有谁依赖它）默认跑；下层只要还有在跑的上层需要它就跑 |
| `mode: enabled` | **一定跑**。它的强依赖被关掉时**报错**——两个意图冲突了 |
| `mode: disable` | **一定不跑**。依赖它的跟着不跑；钉住的上层（enabled / local / debug）则报错 |
| `mode: local` | 一定跑，BrickKit 从本地仓库起一个裸进程并监管（仅 Docker / Podman；只有默认版本能这样跑） |
| `mode: debug` | 一定跑，进程由你在 IDE 里自己起（仅 Docker / Podman）。**只能写在 `deploy.local.yaml`** |

想收窄这次跑哪些，给顶层写 `mode: disable` 就够了。**没有 `--only` 这类参数，别去找。**
`mode: debug` 为什么只能写在个人文件、本地模式怎么开，见 `brickkit-deploy` 技能。

**8. 强依赖和弱依赖在启停上一视同仁。**

上层只是弱依赖它，它照样跟着跑。`optional: true` 只管两件事：解析时取不到只警告、
它没在跑时不注入那个 `*_ENDPOINT` 变量。被多个上层共用的组件，只要还有一个上层在跑它就跑。

**9. `remove` 不删配置，但可能删源码目录。**

配置移进 `config/.archive/`（再次 add 时自动迁回骨架）；部署条目一起删；删外壳时它的
成员挪回顶层独立运行；只因它而保留的兼容版本一并移除；删的是默认版本且只剩一个版本时，
剩下那个转正。多版本共存时要写 `id@版本`。组件的最后一个版本走了才删它的源码目录（包括 `sync`
归档的那一份），而且先确认删了还找得回来：里面有未提交、未推送的改动时整个 `remove` 在改任何文件
之前就停下（`--force` 才照删）。

**10. `fetch` 不装进项目。** 它只把产物下到 `.brickkit/artifacts/<版本化服务名>/`，给跨项目调用
别人的服务生成客户端用，不改 `brickkit.yaml`、不部署。不是「add 的轻量版」。

**11. `sync` 只动目录，不碰容器。** 它把这次不启动的组件源码挪进 `components/.archived/`，
该启动的移回来，判据与 `up` 完全一致。把 `components/` 提交进 Git 的项目，用 `brickkit restore`
把 `deploy.yaml` 里的 `mode` 恢复到上次提交的样子，源码布局跟着回去。

**12. `up` 从不构建镜像。** 本地源的组件要先 `brickkit build`，见 `brickkit-deploy` 技能。

**13. 只改一个组件时，用焦点运行，别起整套。**

在组件目录里 `brickkit up`（或在项目任何位置 `brickkit up --focus <id>`），就把 `focus: <id>` 写进
`deploy.local.yaml`，只启动这个组件——从源码跑——和它需要的；其余的都打印 `不启动（焦点之外）`。焦点一直在，
直到 `brickkit up --all`。项目命令在任何子目录里都能用（往上找最近的 `brickkit.yaml`）；不带参数的 `build`、
`deps` 指的就是你所在的组件。`sync` 不看焦点，换焦点不会挪目录。

**14. 只有一个 `components/`。**

组件源码只放在项目的 `components/` 里（在项目的任何子目录里 `add --repo` 都克隆到那里；所在的工作台本身是
外层项目的一个组件时它会拒绝——到外层项目里去跑）。永远不要把组件复制或克隆进另一个
组件的目录：`up`、`lint`、`sync` 会拒绝嵌套的副本，BrickKit 也从不替你挪——删之前先问人，那里可能是某人改动的
唯一一份。git submodule 从不拉取。

## 机制是怎么运作的

**启停判定算的是「谁不跑」。** 从 `mode: disable` 出发向上传播，得到一个最小不动点，剩下的
都跑。所以两个组件互相弱依赖成环时不需要任何特例——环上没有更上层的东西，都算顶层，都跑。

**CLI 输出里每一行都带理由**：`启动（顶层）` / `启动（mode: enabled）` / `启动（X 需要）`。
组件没起来、或莫名其妙起来了，先读这个理由。

**`up` 的顺序是拓扑排序**（依赖先起）。`brickkit up --dry-run` 只生成部署文件供审查，不真起。
`brickkit graph` 把依赖图打成 Mermaid（灰色的节点这次不启动，外壳的成员画在外壳里面）。
`brickkit status` 读引擎的真实状态，不启动的组件也列出来；`down` 不删 volume，数据保留。
`graph` 和 `deps` 永远读 `deploy.yaml`、不看本地模式，所以人人看到的都一样。

**多版本共存是项目级能力。** `brickkit.yaml` 里可以并列 `people/basic` 的 1.0.0 与 2.0.0，
它们是两个互不冲突的服务名（`people-basic-1-0-0`、`people-basic-2-0-0`）。但**同一份
`component.yaml` 的 `dependencies` 里一个组件 ID 只能出现一次**（原因见 `brickkit-component` 技能）。

**加一个外壳**会把它编进的成员版本一起加进来，并嵌到外壳的部署条目下面；**升级外壳**会换成新外壳编进的成员。

**多环境**是每个环境一份完整的部署文件（`deploy.prod.yaml`），用 `brickkit up -f deploy.prod.yaml`
指定；`brickkit.yaml` 与 `config/` 各环境共用，环境差异的值走部署文件的 `vars:`。没有 overlay、
没有合并。`-f` 不看本地模式。

## 去哪查更细的

- 参数：`brickkit <命令> --help`。这份技能刻意不复刻参数清单
- 某个组件怎么用、要配什么：`.brickkit/manifests/<scope>/<name>/<版本>/BRICKKIT.md`；
  项目里有哪些组件、各干什么：`AGENTS.md` 末尾的组件表（`add` / `remove` / `upgrade` 维护；
  自己的内容写在标记之外）
- 来了新需求、要跨组件改动：`brickkit-plan-change` 技能
- 完整规范与「为什么这样设计」：<https://github.com/brickKit/brickKit> 根目录 `AGENTS.zh.md`
