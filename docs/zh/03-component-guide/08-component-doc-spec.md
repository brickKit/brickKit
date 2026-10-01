# 组件的文档

一个组件有三类读者，要的东西各不一样：

- **使用它的项目**，以及在那些项目里写代码的 AI。它们想知道这个组件干什么、要准备什么、怎么配、怎么调——而不必读它的源码。
- **开发它的人**，很多时候是 AI。它想很快找到某个功能的代码、跑通测试，知道哪些东西不能弄坏。
- **GitHub 上的人**，在决定用不用它。他们要一个简短的回答，和其余内容在哪的指引。

所以一个组件为每类读者各带一份文档，再加上清单文件。这一篇讲每份写什么、要写多种语言时怎么放、`brickkit lint` 查什么。
`brickkit new` 会把每一份都生成成骨架（见[创建组件骨架](04-new-and-skeleton.md)）。

## 谁读哪份

| 文件 | 必需 | 读者 | 写什么 |
| --- | --- | --- | --- |
| `component.yaml` | ✅ | CLI | 依赖、配置项、端口、镜像——见 [component.yaml 字段指南](02-component-yaml-reference.md) |
| 契约文件 | 有对外接口时 | 调用方和它们的 AI | 接口与事件格式，登记在 `artifacts` 下——见[契约与产物](06-artifacts-and-contracts.md) |
| `BRICKKIT.md` | ✅ | 使用它的项目；随每个版本发布 | 负责什么、要准备什么、怎么配置和调用 |
| `AGENTS.md` | ✅ | 开发它的 AI | 代码在哪、怎么构建和测试、哪些不能弄坏 |
| `CLAUDE.md` | ✅ | Claude Code | 只有一行 `@AGENTS.md` |
| `README.md` | ✅ | GitHub 上的人 | 简短的介绍，和其余文件的指路表 |
| `docs/` | 可选 | 要深入的人 | 设计、编号的决策记录、数据模型 |
| `CHANGELOG.md` | 可选，从不检查 | 任何人 | 有就在 README 里链上 |

之所以要有 `CLAUDE.md`：Claude Code 读 `CLAUDE.md`，其他 AI 工具读 `AGENTS.md`。用一行把后者引进前者，内容只写一份。

## 一个事实只写一处

同一件事写在两个文件里，文档就是这样变错的：一份改了，另一份没改，下一个读的人信了过时的那份。所以每个事实只有一个家，其余文件只点它的名、或者链过去。

| 事实 | 它的家 |
| --- | --- |
| 依赖、配置项、端口、镜像、外壳成员 | `component.yaml` |
| 接口与事件格式 | 契约文件 |
| 负责什么、不负责什么；怎么用、怎么配、要准备什么 | `BRICKKIT.md` |
| 代码在哪；怎么构建、测试、修改 | `AGENTS.md` |
| 为什么这样设计（几句话写不下时） | `docs/` |
| 什么时候改了什么 | Git——tag 与提交 |

讲清一个事实不等于重抄它。`BRICKKIT.md` 的配置指南讲 `component.yaml` 声明的某个键怎么选值；在那里再写一遍它的类型或默认值，就是重抄。
另外，任何文档都不写历史（"v1.0.12：……""第三阶段加的"）：文档只写现在是什么样。

## `BRICKKIT.md`

它是唯一离开仓库的文档。项目添加这个组件时，CLI 把它缓存到那个项目里、组件的 `component.yaml` 旁边——单独一份，周围没有仓库。共六节，按这个顺序：

| 小节 | 写什么 |
| --- | --- |
| `组件定位` | 一两句话说它解决什么问题，再两张短表：它**负责**什么、**不负责**什么——每条"不负责"写明归谁（另一个组件、调用方、某个人） |
| `部署前准备` | `brickkit up` 之前必须已经有的东西：带 schema 和角色的数据库、第三方账号、证书。每样写清由谁准备、怎么确认好了。什么都不用时写"除下面的配置外无需准备。" |
| `依赖说明` | 每条依赖写 ID、拿来做什么；可选依赖写缺席时会怎样 |
| `配置指南` | `configSchema` 说不清的：怎么选值、改了会怎样、哪些值要一起配。每个 `required` 键都要在这里出现 |
| `契约索引` | `artifacts` 下的每个文件（照 `artifacts` 里的写法写），和它描述的主要接口；发布的事件；消费的事件 |
| `外壳声明` | "不是外壳。"，或者编进了哪些成员 |

它在哪里被读，决定了两条规则：

- **不放相对链接。** 在别的项目的缓存里，`[设计](docs/design.md)` 什么都指不到。仓库里的文件用行内代码写名字（`api/openapi.yaml`）；绝对地址可以用。
- **"不负责"那张表最要紧。** 一个项目手里往往只有这份缓存，新需求该归哪个组件就靠它判断（见[判断需求、制定计划](../08-ai-guide/03-judging-a-requirement.md)）。

## `AGENTS.md`

它是开发者的导读——在这个仓库里干活的 AI 每次会话最先读的就是它。开头是组件 ID，和一句指路：用法与边界看 `BRICKKIT.md`，
依赖与配置看 `component.yaml`。然后是五节：

| 小节 | 写什么 |
| --- | --- |
| `代码地图` | 只用表格。第一张写每个路径管什么；第二张写每个功能从哪个文件开始。路径用反引号包起来，目录以 `/` 结尾。lint 会查每条路径都在：第一张表第一列里反引号包的都算路径（`main.go`、`Dockerfile` 这样的顶层文件也查），别的格子里只有含 `/` 的才算——别处不带 `/` 的 `runMode` 这类是函数名、类型名，不是路径。以 `/` 开头的（`/api/v1/call` 这样的 HTTP 路由）一律不是路径；含空格、`*` 或 `://` 的跳过不查 |
| `构建与测试` | 构建、测试、本地运行、契约检查的确切命令，以及成功时看到什么 |
| `设计取舍` | 为什么不依赖某个组件；否决过哪些做法、为什么。写不下的放进 `docs/`，这里链过去 |
| `易错点` | 一张表：不许 / 症状 / 原因。只写这个组件特有的——整个项目的规则写在项目的 `AGENTS.md` 里 |
| `改代码前自查` | 三到八条只针对这个组件的检查 |

末尾是由 brickkit 维护的一段，夹在 `<!-- brickkit:managed:begin lang=… -->` 与 `<!-- brickkit:managed:end -->` 之间：每个组件作者都要守的几条平台规则。
brickkit 只写这对标记之间的内容；这段文字比当前 CLI 写的旧了，`brickkit skills status` 会说，`brickkit skills update` 会刷新。

`AGENTS.md` 只写一份，用团队干活用的语言，不翻译：AI 读哪种语言都行，多一份就多一样要对齐的东西。AI 可能只读到其中一段，所以有两个习惯：
不写"如上所述"，每条"不许"都写上症状和原因。

## `README.md`

一页短文，主要的活是把人送到对的文件。三节：

| 小节 | 写什么 |
| --- | --- |
| `在项目里使用` | `brickkit add <id>@<version>` 和 `brickkit up`，再指向"部署前准备" |
| `文档` | 一张表：想知道什么 → 能回答它的那份文件（`BRICKKIT.md`、契约、`component.yaml`、`AGENTS.md`） |
| `开发` | 克隆下来怎么连同依赖一起跑起来，再指向 `AGENTS.md` |

标题下的第一句与 `metadata.description` 是同一句话。项目会把这句话和可选的 `metadata.repository`（组件的仓库或主页）列进自己 `AGENTS.md` 末尾的组件表。

## `docs/`

可选。上面几份写不下时再建：`docs/design.md`（设计变了，先改它再改代码）、`docs/decisions/NNNN-<标题>.md`（一个决策一份，编号，永不重排）、
`docs/data-model.md`。里面的每个文件都要能从 `AGENTS.md` 或 `README.md` 链过去。组件的设计放在组件仓库里，才会跟着组件的版本走。

## 写多种语言

- **不带后缀的那份是主语言**，由作者定。译本就放在旁边，在 `.md` 前面加语言代码：`README.zh.md`、`BRICKKIT.zh.md`、`docs/design.zh.md`。
  原文和译本在同一个目录里，相对链接就完全一样。
- 语言代码用小写：`zh`、`ja`、`pt-br`。lint 把 `.md` 前最后一个点后面那段当语言，所以 `README.zh-CN.md` 会挨警告（应写
  `README.zh-cn.md`）。管到的是根目录的 `README.*`、`BRICKKIT.*`、`AGENTS.*`，以及 `docs/` 里有译本的目录下的文件；
  `docs/` 别处的 `v1.2-notes.md` 这种名字就只是名字。
- 译本按文件可选。常见的做法：`README` 和 `BRICKKIT` 有译本（人和别的团队会读），`AGENTS.md` 和 `docs/` 没有。
- **两份对不上时以原文为准。** 改了原文，就在同一个提交里改译本。
- 有译本的文件在开头放一行，链接所有语言版本，比如 `[English](README.md) · [中文](README.zh.md)`——`BRICKKIT*.md` 除外，它根本不放相对链接。每个语言版本都放完整的这一行：有三种语言，每份都链接另外两份。
- 小节标题中英文都认。

`brickkit add` 会把每份 `BRICKKIT.<语言>.md` 连同 `BRICKKIT.md` 一起缓存，`brickkit publish` 会把它们一起上传。

## `brickkit lint` 查什么

`lint` 只查程序能确定的事；写得好不好是评审的事。查出的每一条都是警告——`up` 和 `release` 从不因为文档停下；想让 CI 守住的团队用
`lint --strict`，把警告算成失败。它在组件仓库、工作台里查组件的文档，在项目里查每个本地源组件的文档。
`component.yaml` 写错了也照样查文档，只是跳过与清单的比对（`DOC_OUT_OF_STEP`）。

| 错误码 | 意思 |
| --- | --- |
| `DOC_FILE_MISSING` | 缺了一份必需的文档 |
| `DOC_SECTION_MISSING` | 缺了一个固定小节 |
| `DOC_PATH_MISSING` | 代码地图里的某条路径已经不存在（哪些算路径：见上面 `代码地图` 那一行） |
| `DOC_LINK_BROKEN` | 某条相对链接指向的东西不存在 |
| `DOC_LINK_NOT_PORTABLE` | `BRICKKIT.md` 里有相对链接，或者文档链出了组件目录 |
| `DOC_OUT_OF_STEP` | `component.yaml` 里的依赖、必填键、契约文件或外壳成员，文档该提的地方没提 |
| `DOC_PLACEHOLDER` | 正文里（代码之外）还留着 `TODO`、`TBD`、`FIXME`、`待补`、`后补`、`待填` 这些占位词 |
| `DOC_TRANSLATION_DRIFT` | 译本没有原文、小节数不同、某个语言版本没链接其余每一份，或者文件的语言后缀不是小写语言代码（`README.zh-CN.md`） |
| `AGENTS_BLOCK_MISSING` | `AGENTS.md` 里没有由 brickkit 维护的那一段 |
| `CLAUDE_IMPORT_MISSING` | `CLAUDE.md` 没有引入 `AGENTS.md` |

每一条要你做什么，见[错误码](../06-architecture/09-error-codes.md#文档检查)。

## 一个完整的例子

`demo/quote` 每次返回一句名言，前面加上 `demo/hello` 的问候语。它的仓库：

```text
demo-quote/
├── component.yaml
├── BRICKKIT.md
├── AGENTS.md
├── CLAUDE.md
├── README.md
├── api/openapi.yaml
├── main.go
└── Dockerfile
```

`BRICKKIT.md`：

```markdown
# demo/quote

## 组件定位

每次返回一句名言，前面加上 demo/hello 的问候语；用在页面顶部的"每日一句"。

负责：
- 挑选、拼装名言

不负责：
- 问候语本身：demo/hello
- 在页面上怎么展示：调用方

## 部署前准备

除下面的配置外无需准备。

## 依赖说明

- demo/hello（必需）：提供问候语。它连不上时接口照常返回，问候语换成"(demo/hello unreachable)"。

## 配置指南

| 变量名 | 说明 |
|---|---|
| QUOTE_PREFIX | 名言前面的文字。不要前缀就设成空字符串 "" |

## 契约索引

- `api/openapi.yaml`：`GET /api/v1/quote`，返回 `{"greeting": "...", "quote": "..."}`。不发也不收事件。

## 外壳声明

不是外壳。
```

`AGENTS.md`：

```markdown
# demo/quote

返回一句名言，前面加上 demo/hello 的问候语。怎么用、边界：BRICKKIT.md。依赖与配置：component.yaml。

## 代码地图

| 路径 | 管什么 |
|---|---|
| `main.go` | HTTP 服务、名言列表、对 demo/hello 的调用 |
| `api/openapi.yaml` | 契约 |

| 功能 | 从这里开始 | 然后 |
|---|---|---|
| 名言接口 | `main.go`（`/api/v1/quote`） | `api/openapi.yaml` |

## 构建与测试

`go build ./...`；`go test ./...` 打印 `ok`。连同 demo/hello 一起跑：在这个目录里 `brickkit up`（在项目里是焦点运行，单独的仓库里是工作台）。

## 设计取舍

demo/hello 挂了时降级而不是报错：名言是内容，问候语只是点缀。

## 易错点

| 不许 | 症状 | 原因 |
|---|---|---|
| 在 `/healthz` 里检查 demo/hello | demo/hello 一重启，demo/quote 就变成不健康 | 健康检查只查自己这个进程 |

## 改代码前自查

1. 改动会不会改变 `/api/v1/quote` 的响应？在同一个提交里改 `api/openapi.yaml` 和 BRICKKIT.md 的契约索引。
2. 加了新配置项？在 `configSchema` 里声明，并在 BRICKKIT.md 的配置指南里讲清楚。

<!-- brickkit:managed:begin lang=zh -->
……由 brickkit 写……
<!-- brickkit:managed:end -->
```

`README.md`：

```markdown
# demo/quote

每次返回一句名言，前面加上 demo/hello 的问候语。

## 在项目里使用

    brickkit add demo/quote@0.1.0
    brickkit up

先做准备：见 BRICKKIT.md 的"部署前准备"。

## 文档

| 想知道 | 读 |
|---|---|
| 负责什么、不负责什么，怎么配置，要准备什么 | BRICKKIT.md |
| 接口 | api/openapi.yaml |
| 依赖什么（BRICKKIT.md 里有说明） | component.yaml |
| 怎么开发 | AGENTS.md |

## 开发

克隆下来，`brickkit init` 建工作台，`brickkit up` 连同 demo/hello 一起跑起来；然后读 AGENTS.md。
```

真实的文件里，"文档"表里的每个文件名都是指向那个文件的相对链接。

注意 `BRICKKIT.md` 配置指南那一行：设成空字符串就去掉前缀。`component.yaml` 写得出 `default: "Quote of the day: "`，写不出这一点——而这恰恰是使用者最想知道的。

## 它怎么到使用者手里

项目 `add` 或 `fetch` 这个组件时，CLI 从那个版本（本地源的目录、git 的 tag、或者市场）取出 `BRICKKIT.md` 和每份 `BRICKKIT.<语言>.md`，缓存到

```text
.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md
```

项目的 `AGENTS.md` 末尾有一张由 brickkit 维护的组件表：每个组件、版本、它的 `metadata.description`、带着哪些文档（`BRICKKIT.md +zh`）、
它的 `metadata.repository`。项目里的 AI 从这张表开始，然后只读任务碰到的那几个组件的 `BRICKKIT.md`。本地源里正好是这个版本时，
源码目录里那份才是正在改的、该读的那份。

缓存跟着它来自的那个版本走：重新取这个版本时，版本里已经没有的译本会从缓存删掉；文件旁边的 `docs.list` 记着缓存里有哪几份。
缓存里没有这份记录的组件（这台机器没缓存过它，或者是旧版 CLI 缓存的），表里保留原来的"文档"一栏，不去猜。

所以文档和代码一样，是版本的一部分。正常的改法是朝一个新版本改：先升 `metadata.version`，从本地源测，再 `brickkit release`——
发布之前，没有这份源码的机器根本没有这个版本，表里那一行原样不动。出问题的是**原地改一个已经发布的版本**：一个版本号对应了两份内容，
有本地源的机器把新的那一行写进项目的 `AGENTS.md`，从 tag 取这个版本的机器又把旧的写回去，两边的 `brickkit skills status`
都说对方写的维护段过期了。已经发布的 `BRICKKIT.md`、译本或 `metadata.description`，改在下一个版本里。

`brickkit publish` 把 `BRICKKIT.md` 和译本随版本一起上传：译本最多 16 份，每份不超过 256 KiB，全部加起来不超过 1 MiB。续传一次中断的发布时，每一份都要与 draft 登记的比对，有一份不同就停下。
市场太旧、存不了译本时，版本照样发布但不带译本，`publish` 会警告。
