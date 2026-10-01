# 文件路由表

BrickKit 项目里每个问题都只有一个答案来源。先按问题找文件，别从头扫目录。

## 项目里的文件

| 想知道 | 读 |
| --- | --- |
| 项目用了哪些组件、各是哪个版本、从哪里取 | `brickkit.yaml` |
| 项目是什么、每个组件都要守的约定、到哪找东西、易错点 | 项目根的 `AGENTS.md`：团队自己写的那几节 |
| 有哪些组件、各干什么、文档和契约在哪 | 项目 `AGENTS.md` 末尾的组件表（由 CLI 维护的那一段） |
| 谁依赖谁 | `brickkit deps`（不在 `brickkit.yaml` 里） |
| 部署到哪、哪些组件跑、怎么跑 | 当前生效的部署文件：`brickkit local status` 告诉你是 `deploy.yaml` 还是 `deploy.local.yaml` |
| 某个组件拿到的配置值 | `config/<组件>.yaml`（兼容版本是 `config/<组件>@<版本>.yaml`），`$var:` 引用去 `config/vars.yaml` 与部署文件的 `vars:` |
| 这次会起什么、每个组件拿到什么环境变量 | `brickkit up --dry-run`，再看 `.brickkit/generated/compose.yaml` |
| 当前在跑的状态 | `brickkit status` |
| 一条命令有哪些参数 | `brickkit <命令> --help` |

## 依赖的组件

| 想知道 | 读 |
| --- | --- |
| 它负责什么、不负责什么、部署前要准备什么、怎么配、依赖谁 | `.brickkit/manifests/<scope>/<name>/<版本>/BRICKKIT.md`（组件带译本时，`BRICKKIT.<语言>.md` 就在旁边） |
| 它有哪些配置项、默认值、哪些必填 | 同一目录的 `component.yaml` 的 `configSchema` |
| 它的接口 | `.brickkit/artifacts/<版本化服务名>/` 下的契约文件 |
| 它的地址变量叫什么 | 按规则推：`demo/hello` → `DEMO_HELLO_ENDPOINT` |

组件源码克隆在 `components/` 下时（本地源），它的 `BRICKKIT.md` 与 `component.yaml` 就在那个目录里，而且是最新的。
要**改**这个组件，它自己的 `AGENTS.md` 也在那里：代码地图、怎么构建和测试、设计取舍。

## 不要读的

| 不要读 | 为什么 |
| --- | --- |
| 依赖组件的源码 | 内部实现不是对外承诺；读契约和 `BRICKKIT.md`。依赖一重构，照源码写的调用就坏了 |
| `components/.archived/` | 这次不跑的组件的源码，`sync` 挪进去就是为了让你不用管它 |
| 组件仓库里的 `brickkit.yaml`、`deploy.yaml`、`config/` | 那是组件作者的本地联调工作台，与使用这个组件的项目无关 |
| `~/.cache/brickkit/repos/` | bare 仓库缓存，给 CLI 用的 |
| `.brickkit/generated/` 之外的 `.brickkit/` 状态文件 | `last-run`、`local-mode` 这些是 CLI 的内部记录 |
| `config/.archive/` | 已移除组件的旧配置 |

## 什么时候读

- **开始一个任务前**：项目根的 `AGENTS.md`（团队约定，末尾一张表就知道有哪些组件）、`brickkit.yaml`。
- **要用一个组件时**：它的 `BRICKKIT.md`、`component.yaml`、契约。
- **要改一个组件的代码时**：再加上它源码目录里自己的 `AGENTS.md`。
- **来了一个新需求时**：动手之前先看 [判断需求、制定计划](03-judging-a-requirement.md)。
- **要改部署方式时**：先 `brickkit local status` 确认改哪份部署文件。
- **动手之后**：`brickkit lint`（结构）、`brickkit up --dry-run`（依赖与生成）——让 CLI 告诉你对不对，而不是靠读代码推断。

不要一开始就把所有组件的文档都读一遍：只读当前任务涉及的组件和它的直接依赖。
