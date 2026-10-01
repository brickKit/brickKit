## BrickKit

这个项目用 [BrickKit](https://github.com/brickKit/brickKit) 组装：每个组件在自己的 `component.yaml` 里描述自己；`brickkit` 命令行解出依赖图、生成部署文件后就退出。没有注册中心、配置中心、网关，不用去找。

- `brickkit.yaml` 列出组件和它们的精确版本（它就是锁文件）；`deploy.yaml` 写怎么运行（本地模式开着时，你机器上用 `deploy.local.yaml` 整份替换它）；`config/<scope>-<name>.yaml` 是每个组件的环境变量，`config/vars.yaml` 是共用的值。
- `brickkit add` / `remove` / `upgrade` 会让三层文件一起改：别手改一层、忘了另一层。
- 配置项的键就是组件 `configSchema` 声明的环境变量名；密钥写成 `${VAR}` 或 `file://.secrets/…`，不写明文。
- 健康检查只查自己这个进程。只想改一个组件时，在它的目录里 `brickkit up`；`brickkit up --all` 回到全部运行。
- 按任务分的技能在 `.claude/skills/brickkit-*`；参数问 `brickkit <命令> --help`。

## 组件

组件的文档是 `BRICKKIT.md`（译本 `BRICKKIT.<语言>.md`）：本地源里正好是这个版本时在源码目录里，否则在 `.brickkit/manifests/<id>/<version>/`；契约在 `.brickkit/artifacts/<服务名>/`（ID 里的 `/` 和 `.` 换成 `-`，再接上 `.` 换成 `-` 的版本）。要改某个组件，读它自己的 `AGENTS.md`。
