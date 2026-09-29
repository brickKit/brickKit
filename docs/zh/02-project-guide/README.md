# 项目管理者指南

**写给谁**：用 BrickKit 把现成的组件拼成一个系统、并让它在开发机和服务器上跑起来的人。你不一定写组件——
写组件看 [组件开发者指南](../03-component-guide/README.md)。

**先读**：[快速开始](../00-intro/02-quick-start.md)（五分钟跑通一个组件）和 [三层架构](../01-three-layers/README.md)
（`brickkit.yaml`、部署文件、`config/` 各管什么）。

这一模块按一个项目的生命周期排：建项目 → 加组件 → 本地调试 → 多环境 → 启停 → 升级 → 移除 → 查看 → 检查 → 管源码 → 构建。
每一篇的命令和输出都是真跑出来的，用的是同一个演示项目 `my-shop`：

| 组件 | 做什么 |
| --- | --- |
| `demo/hello` | 回一句问候语；问候语由配置项 `GREETING` 决定 |
| `demo/caller` | 调用 `demo/hello`（强依赖），可选地用 `demo/bus`（弱依赖） |
| `demo/bus` | 一个同样简单的服务，扮演"消息总线" |

它们放在一个公司内部的 Git 服务上，地址是 `https://git.example.com/components/<scope>-<name>`，每个版本一个 tag。

| 篇目 | 讲什么 |
| --- | --- |
| [01 创建项目](01-init-and-project-creation.md) | `init` 的两种模式、生成的骨架与 `.gitignore`、克隆已有项目 |
| [02 添加组件](02-add-and-component-install.md) | `add` 一次改了哪些文件、`$var:` 提示、`--local`、`--repo` |
| [03 本地调试](03-local-debug-workflow.md) | `local on` → `mode: debug` → 在 IDE 里跑 → 容器连得上你；团队改了文件之后怎么办 |
| [04 多环境](04-multi-env-switch.md) | 每个环境一份部署文件，`-f` 选；`vars:` 让同一份 `config/` 在不同环境取不同的值 |
| [05 启动与停止](05-up-and-down.md) | `up` 的流程、镜像检查、`--dry-run`、`down`、常见启动失败 |
| [06 升级与配置迁移](06-upgrade-and-migration.md) | `upgrade` 怎么迁移配置、冲突怎么解决、旧版本怎么留下 |
| [07 移除与归档](07-remove-and-archive.md) | `remove` 的检查、配置归档与恢复、源码目录的保护 |
| [08 状态与拓扑](08-status-and-graph.md) | `status`、`deps`、`graph` |
| [09 离线检查](09-lint-and-checks.md) | `lint` 查什么、不查什么、怎么放进 CI |
| [10 源码管理](10-sync-and-restore.md) | `sync` 归档不用的源码、`restore` 还原、提交前检查 |
| [11 构建与镜像](11-build-and-images.md) | 为什么 `up` 从不构建、`build` 怎么用、镜像 tag 规则 |

每条命令的完整参数见 [CLI 命令参考](../07-cli-reference/README.md)。
