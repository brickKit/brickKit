# 离线检查

## `brickkit lint`

不联网、不需要 Docker 或 Kubernetes、不改任何文件，几秒钟把项目里的 YAML 检查一遍：

```bash
brickkit lint
```

```text
✅ brickkit.yaml
✅ deploy.yaml
✅ deploy.local.yaml
✅ 跨文件：brickkit.yaml ↔ deploy.local.yaml ↔ config/
✅ 跨文件：brickkit.yaml ↔ deploy.yaml ↔ config/
✅ ./（文档）
✅ components/demo/hello/component.yaml
✅ components/demo/hello/（文档）

📋 检查了 8 个文件：0 个有错误，0 条警告
```

在项目目录里（有 `brickkit.yaml`），它依次查：

| 查什么 | 例子 |
| --- | --- |
| `brickkit.yaml` 的结构 | 必填字段、类型、版本号只能是精确版本 |
| 部署文件的结构 | 未知字段（拼写笔误）、`mode: debug` 写进了 `deploy.yaml`、`localPort` 没配 `mode`、宿主机端口冲突 |
| 三层对不对得上 | 每个组件版本在部署文件里恰好一个条目；外壳成员写在外壳下面；`config/` 的文件对得上组件 |
| `config/` 的引用 | `$var:` 都有定义；没有升级遗留的冲突块（重复键） |
| 配置与 `configSchema` | 必填项有值；写下的键在 schema 里（拼错的键不会生效）；外壳成员的值能编码进外壳。只查 `component.yaml` 已经在盘上的组件版本（在 `.brickkit/manifests/` 里，或本地源里正好是这个版本）；其余的在提示里列为未检查 |
| 外壳声明 | `brickkit.yaml` 的 `kind: shell` 与组件的 `shell` 块一致；放在外壳下面的成员确实编进了这个外壳（同样只查 `component.yaml` 在盘上的） |
| 本地源里的 `component.yaml` | 每一份都查，不管有没有 `add` 过（`.archived/` 不查） |
| 文档 | 项目自己的文档（`./（文档）`），以及本地源里每个组件的文档，见 [文档检查](#文档检查) |

`deploy.local.yaml` 存在时也查——不管本地模式开没开，它都必须与 `brickkit.yaml` 一致。给了 `-f` 就只查那一份部署文件。
`brickkit.yaml` 自己没通过时，后面的都不可信，跳过并说明原因。

在组件仓库里（有 `component.yaml`、没有 `brickkit.yaml`），查那一份 `component.yaml` 和组件的文档。

在工作台里（组件仓库里又有 `brickkit.yaml`），三层文件按项目查，组件的 `component.yaml` 和文档也一起查。

### 项目里的一个组件

在项目里某个组件的目录下，`brickkit lint` 不带参数**只查这个组件**——和 `build`、`deps` 同一条规则。`brickkit lint <id>`
在哪里都能这样查：

```text
📁 项目：../../..（shop）
🔎 只查 demo/hello（brickkit lint --all 查整个项目）
✅ component.yaml
✅ ./（文档）
✅ demo/hello：配置（config/ ↔ configSchema）

📋 检查了 3 个文件：0 个有错误，0 条警告
```

查的是：本地源里有它时（不管 add 过没有），它的 `component.yaml` 和文档；它进了 `brickkit.yaml` 之后，按它的 `configSchema`
查它的配置。项目里别处的问题不报：一个一个重建组件时，刚改好的这个不该因为另一个还没改完而变红。项目本身仍然得能按 `up` 的方式装载——
装载不了，这个组件也跑不起来，所以这类错误照报。从 git 或市场来的组件，`component.yaml` 和文档归它的作者查，这里只查它的配置。

`brickkit lint --all` 在哪里都查整个项目。CI 在项目根目录跑，两者没有区别。

一个典型的错误：

```text
✅ brickkit.yaml
❌ [CONFIG_INVALID] 错误：deploy.yaml 校验失败
   文件：deploy.yaml
   components[0].exposed：未知字段（第 7 行），是不是想写 expose？
   建议：完整字段说明：brickkit docs 11-reference/03-deploy-yaml-schema（网页版：https://github.com/brickKit/brickKit/blob/v1.1.0/docs/zh/11-reference/03-deploy-yaml-schema.md）
✅ deploy.local.yaml
✅ ./（文档）
✅ components/demo/hello/component.yaml
✅ components/demo/hello/（文档）

📋 检查了 6 个文件：1 个有错误，0 条警告
❌ 错误：结构检查未通过
   已检查：6 个文件
   有错误：1 个文件
   建议：按上面逐条列出的位置修改，再执行 brickkit lint
```

未知字段是错误而不是警告：`exposed: true` 写错了，组件就不会对外开放，而且不会有任何别的地方告诉你。

## 文档检查

`lint` 也读文档，读法是机械的：必需的文件在不在、固定的小节有没有、里面的路径和链接指不指得到地方、文档是不是还在说
`component.yaml` 说的事。读哪些，看你在哪里跑：

| 在哪里 | 查哪些文档 |
| --- | --- |
| 组件仓库 | 组件的 `BRICKKIT.md`（连同译本）、`AGENTS.md`、`CLAUDE.md`、`README.md`，有 `docs/` 的话也查 |
| 项目 | 项目的 `AGENTS.md`、`CLAUDE.md`、`README.md`、它们的译本和 `docs/` 下的每个文件；项目根残留的旧项目地图 `BRICKKIT.md`；以及本地源里每个组件的文档 |
| 工作台 | 组件的文档，和组件仓库一样 |

每一组文档在报告里占一行——项目是 `✅ ./（文档）`，组件是 `✅ components/demo/hello/（文档）`——有问题时那一行换成具体的警告：

```text
⚠️ [DOC_LINK_NOT_PORTABLE] BRICKKIT.md 在别的项目缓存里是单独读的，相对链接 openapi.json 在那里是死的：用行内代码写文件名，或用绝对地址
   文件：components/demo/hello/BRICKKIT.md
   行：46
```

**文档方面的发现一律是警告。** 它从不挡住 `lint`、`up` 或 `release`：文档落后一点是要修的问题，不是停掉一次部署的理由。
想让 CI 把住这道关的团队，用 `--strict` 把警告变成失败。

报告里的每一条——不管是错误还是警告，查的是文档还是三层文件——标题行开头都用方括号带着它的
[错误码](../06-architecture/09-error-codes.md)。错误码在哪种语言、哪个版本下都一样，所以脚本和 CI 规则该匹配的是它，
要查资料时查的也是它；后面那句话是给人读的。文档检查用的是这几个：

| 代码 | 意思 |
| --- | --- |
| `DOC_FILE_MISSING` | 缺了必需的文档 |
| `DOC_SECTION_MISSING` | 文档缺了一个固定小节（中英文标题都认） |
| `DOC_PATH_MISSING` | 组件 `AGENTS.md` 代码地图里的路径不存在 |
| `DOC_LINK_BROKEN` | 相对链接指向的文件不存在 |
| `DOC_LINK_NOT_PORTABLE` | `BRICKKIT.md` 里有相对链接（它是单独被读的，链接在那里是死的），或者文档链到了组件目录外面 |
| `DOC_OUT_OF_STEP` | `component.yaml` 里有的依赖、必填配置项、契约文件或外壳成员，文档里没提 |
| `DOC_PLACEHOLDER` | 正文里还留着 `TODO`（或 `TBD`、`FIXME`）——骨架有意在每一节都留了一条 |
| `DOC_TRANSLATION_DRIFT` | 译本（`BRICKKIT.zh.md`、`README.zh.md`、`docs/zh/` 下的一页）和它的原文对不上了，或者某一页在某棵 `docs/<语言>/` 树里缺了 |
| `AGENTS_BLOCK_MISSING` | `AGENTS.md` 里没有可用的、由 CLI 维护的那一段，组件表不会自动更新 |
| `CLAUDE_IMPORT_MISSING` | `CLAUDE.md` 里没有 `@AGENTS.md` |
| `PROJECT_MAP_OBSOLETE` | 项目根还留着旧版的项目地图 `BRICKKIT.md` |

每一条要你做什么，见 [错误码](../06-architecture/09-error-codes.md#文档检查)。文档本身怎么写，见
[组件的文档](../03-component-guide/08-component-doc-spec.md) 与 [项目的 `AGENTS.md`](01-init-and-project-creation.md#项目的-agentsmd)、[项目的其他文档](01-init-and-project-creation.md#项目的其他文档)。

## 提示

除了错误和警告，`lint` 还会说三件事：两件关于 `brickkit.yaml`，一件关于事件。它们都符合规则，`up` 也照常跑，所以只是提示（`ℹ️`），
`--strict` 不因为它们失败；但多半不是你想要的，而且别处不会有任何东西说出来——`brickkit.yaml` 只在你显式 `add` / `upgrade` / `remove`
时才变，事件平台从不核对。

| 提示 | 什么时候出现 | 怎么办 |
| --- | --- | --- |
| 源码是另一个版本 | 组件的本地源目录里 `metadata.version` 已经升了，项目钉的还是原来那个——以容器方式跑的仍是钉着的版本 | 要跑新版本：`brickkit upgrade <id>@<源码的版本>` |
| 兼容版本没人要了 | 带 `requiredBy` 的那一行列出的组件，按它们现在的 `component.yaml` 已经没有谁依赖这个版本（见[多版本共存](../03-component-guide/09-multi-version-coexistence.md)） | `brickkit remove <id>@<版本>` |
| 订阅的事件没人发布 | 组件的 [`events.subscribes`](../03-component-guide/02-component-yaml-reference.md#events我发布订阅哪些事件) 里有一项，项目里没有任何组件发布收得到的事件。项目里有组件的 `component.yaml` 还不在盘上时不提（发布方可能正是它） | 事件来自项目之外就不用管；否则对一下发布方 `events.publishes` 里的名字，或者把发布方加进项目 |

## `--strict`

`--strict` 多查一类东西——引用的**值**在不在：进程环境与 `.env` 里都找不到的 `${VAR}`、文件不存在的 `file://`。
并且警告也算失败：

```bash
brickkit lint --strict
```

```text
⚠️ [CONFIG_INVALID] 警告：demo/caller@1.0.0 的 DATABASE_NAME 指向的 file:// 文件不存在
   路径：.secrets/dbname
   建议：file:// 的路径相对项目根；这类文件不要进 Git（比如放在 .secrets/ 下）
⚠️ [CONFIG_INVALID] 警告：demo/caller@1.0.0 的 DATABASE_USER 引用的环境变量在这里没有设置
   变量：DB_USER
   建议：先查进程环境，再查项目根目录的 .env——在其中一处设好它
```

```text
📋 检查了 6 个文件：0 个有错误，2 条警告
❌ 错误：结构检查未通过
   已检查：6 个文件
   警告：2 条（--strict：警告也算失败）
   建议：按上面逐条列出的位置修改，再执行 brickkit lint
```

不带 `--strict` 时这两项不查：在开发机上，生产的口令本来就不在环境里。（Docker 下 `up` 会检查这次部署要用的 `${VAR}` 有没有定义——
见 [敏感值](../01-three-layers/07-sensitive-values.md)。）

## 退出码

| 结果 | 退出码 |
| --- | --- |
| 没有问题，或只有警告 | 0 |
| 有错误 | 1 |
| `--strict` 下有警告 | 1 |

## `lint` 不查什么

- **依赖能不能解析、外壳这次承载的成员版本对不对。** 这要把所有组件的 `component.yaml` 都拿到、建出完整的依赖图，再算出这次谁在跑——
  对 Git 或市场上的组件来说就是联网。`lint` 承诺离线、只读、立即返回，所以不建这张图。这些留给 `brickkit up --dry-run`：
  它反正要解析依赖图，缺了强依赖、外壳承载的版本与编进的版本对不上都会点名报错。
- **配置的值合不合 `configSchema` 的约束**（`enum`、`minimum`、`pattern`……）。平台只检查键名、不检查值：值错了，组件自己会大声报错；
  键名错了则什么都不会发生，只是你的配置悄悄没生效——所以只查后者。见 [configSchema 规范](../11-reference/04-config-schema-spec.md)。

## 放进 CI

```bash
brickkit lint --strict
brickkit lint --strict -f deploy.prod.yaml
brickkit up --dry-run -f deploy.prod.yaml
```

- `lint --strict` 作为门禁：结构错误、未定义的引用和文档警告都挡在合并之前。CI 里要有这些 `${VAR}` 的值（或者这一步只查结构，去掉 `--strict`）。
- 每个环境的部署文件用 `-f` 各查一遍：默认只查 `deploy.yaml`（以及存在时的 `deploy.local.yaml`），不会去查 `deploy.prod.yaml`。
- 再加一步 `up --dry-run`，把依赖解析也查了——它需要能访问安装源，但不需要能访问集群。
