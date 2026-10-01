# AI 专属指南

**写给谁**：在 BrickKit 项目里干活的 AI 助手，以及想让 AI 助手干得更好的人。

BrickKit 的设计处处考虑了 AI：组件小到一次读得完，边界写在文件里，地址与变量名按规则算、不用猜，每个组件带一份写给使用者的 `BRICKKIT.md`，每个项目的 `AGENTS.md` 末尾有一张组件表。
这一模块讲 AI 怎么利用这些：该读什么、按什么顺序读、什么不必读，以及开发与修改组件的工作流。

## 与 AGENTS.md 的关系

有三份写给 AI 的东西，各管一层：

| 文件 | 在哪 | 讲什么 |
| --- | --- | --- |
| BrickKit 仓库的 `AGENTS.md` / `AGENTS.zh.md` | 本仓库根目录 | BrickKit 这个平台是什么、原则是什么——讨论平台本身时读 |
| 项目里的 `AGENTS.md` 与 `.claude/skills/` | 每个项目（`brickkit init` 写入） | 在这个项目里怎么干活：团队约定、平台规则摘要与组件表、五个按任务划分的技能 |
| 这一模块 | 文档 | 上面两份背后的"为什么"与完整做法 |

项目里的那份是日常要用的：`brickkit init` 装进 `.claude/skills/`（`brickkit-assemble`、`brickkit-component`、`brickkit-deploy`、`brickkit-troubleshoot`、`brickkit-plan-change`），
再写出 `AGENTS.md`（团队自己的导读，末尾那一段由 CLI 跟着更新）和 `CLAUDE.md`，都跟着项目提交；CLI 升级后 `brickkit skills update` 刷新技能和 `AGENTS.md` 末尾那一段（`AGENTS.md` / `CLAUDE.md` 不在时顺带生成）。
它们刻意不复刻命令参数——参数去问 `brickkit <命令> --help`。

在自己的仓库里开发的组件（有 `component.yaml`、没有 `brickkit.yaml`）拿到的是在那里讲得通的一份：在那个仓库里跑 `brickkit skills update`，
只装 `brickkit-component` 这一个技能，外加组件自己的 `AGENTS.md`（末尾那一段是组件规则）和 `CLAUDE.md`。`brickkit new` 不会替你做这一步。

| 篇目 | 讲什么 |
| --- | --- |
| [01 文件路由表](01-ai-routing-table.md) | 想知道什么，读哪个文件；什么不要读 |
| [02 开发工作流](02-ai-dev-workflow.md) | 写一个新组件、改一个已有组件的步骤 |
| [03 判断需求、制定计划](03-judging-a-requirement.md) | 需求归哪个组件、合不合理、按什么顺序改 |
| [04 组件文档（AI 视角）](04-component-doc-spec.md) | 怎么读组件的 `BRICKKIT.md` 和 `AGENTS.md`，怎么写它们 |
| [05 分形读取](05-fractal-reading.md) | 从平台到项目到组件的读取顺序，与上下文窗口的管理 |
| [06 读 BrickKit 本身](06-reading-brickkit.md) | 进这个仓库的三条路线：文档合集、`llms.zh.txt` 路由、`AGENTS.zh.md` 的两张地图 |
