# BrickKit 文档

这里是 BrickKit 的中文文档。文件夹前面的编号就是推荐的阅读顺序；每个文件夹里的 `README.md`
讲清楚这一块有哪几篇、先读哪篇。英文文档在 `docs/en/`，与这里一一对应，但各自独立撰写，不是译本。

## 全部模块

| 编号 | 模块 | 读完你会得到什么 |
| --- | --- | --- |
| 00 | [概览与入门](00-intro/README.md) | BrickKit 是什么、5 分钟跑起第一个组件、术语速查、分形架构 |
| 01 | [三层架构详解](01-three-layers/README.md) | `brickkit.yaml`、`deploy.yaml`、`deploy.local.yaml`、`config/` 各管什么、怎么写、按什么顺序取值 |
| 02 | [项目管理者指南](02-project-guide/README.md) | 建项目、加组件、本地调试、多环境、启动停止、升级、移除、构建镜像 |
| 03 | [组件开发者指南](03-component-guide/README.md) | 组件仓库长什么样、`component.yaml` 怎么写、怎么发布、怎么写 `BRICKKIT.md` |
| 04 | [外壳机制](04-shell/README.md) | 把多个组件编进一个进程：成员管理、配置注入、升级 |
| 05 | [数据库迁移](05-migration/README.md) | 迁移怎么跑、拿到哪些变量、多版本时的顺序 |
| 06 | [架构与深度机制](06-architecture/README.md) | 从声明到运行容器的流水线、依赖解析、注入契约、设计原则、错误码 |
| 07 | [CLI 命令参考](07-cli-reference/README.md) | 每条命令、每个参数 |
| 08 | [AI 专属指南](08-ai-guide/README.md) | AI 该读哪些文件、按什么顺序读、怎么写组件文档 |
| 09 | [推荐实践](09-patterns/README.md) | 组件怎么切、测试怎么分层、种子数据、可靠地调用依赖、部署形态怎么选 |
| 10 | [故障排除](10-troubleshooting/README.md) | 症状 → 原因 → 解决 |
| 11 | [参考手册](11-reference/README.md) | 三个文件的完整字段规格、`configSchema` 规格、JSON Schema、市场 API |

## 按你是谁来读

**第一次接触 BrickKit：** [BrickKit 是什么](00-intro/01-what-is-brickkit.md) →
[快速开始](00-intro/02-quick-start.md) → [核心概念](00-intro/03-core-concepts.md) →
[三层架构总览](01-three-layers/01-overview.md)，然后按需翻 [项目管理者指南](02-project-guide/README.md)。

**要把组件装配成一个系统：** [三层架构详解](01-three-layers/README.md) →
[项目管理者指南](02-project-guide/README.md) → [CLI 命令参考](07-cli-reference/README.md)，
出问题时查 [故障排除](10-troubleshooting/README.md)。

**要写一个组件：** [组件开发者指南](03-component-guide/README.md) →
[component.yaml 字段参考](11-reference/01-component-yaml-schema.md) →
[推荐实践](09-patterns/README.md)；要把几个组件编进一个进程，再读 [外壳机制](04-shell/README.md)。

**你是 AI 助手：** 先读仓库根目录的 [`AGENTS.zh.md`](../../AGENTS.zh.md)，再读
[AI 专属指南](08-ai-guide/README.md)。要抓取文件的原始内容，用 [`llms.zh.txt`](../../llms.zh.txt)
里的 raw 链接。

## 教程

动手教程（`tutorials/zh/`）尚未编写。在那之前，[快速开始](00-intro/02-quick-start.md) 与各篇指南里的
每一条命令和每一段输出都是真跑出来的。
