# 组件开发者指南

**写给谁**：编写、发布组件的人。组件是 BrickKit 里最小的安装和运行单位——一个能独立跑起来的服务，带着一份说明自己的
`component.yaml`。

**先读**：[什么是 BrickKit](../00-intro/01-what-is-brickkit.md) 与 [核心概念](../00-intro/04-core-concepts.md)。
把组件拼成系统、部署出去是另一件事，见 [项目管理者指南](../02-project-guide/README.md)。

这一模块跟着一个真实的组件 `demo/quote` 从零走到发布：它每次返回一句名言，前面带上另一个组件 `demo/hello` 的问候语。
每一条命令和输出都是真跑出来的。

| 篇目 | 讲什么 |
| --- | --- |
| [01 组件仓库结构](01-component-anatomy.md) | 一个组件仓库里有什么；组件 ID、仓库名、版本 tag 怎么对应 |
| [02 component.yaml 字段](02-component-yaml-reference.md) | 每一块写什么、怎么写；精确的字段规则在参考手册 |
| [03 configSchema 设计准则](03-config-schema-design.md) | 配置项怎么拆、怎么命名；平台检查什么、不检查什么 |
| [04 创建组件骨架](04-new-and-skeleton.md) | `brickkit new`，以及从骨架到能跑的组件 |
| [05 分形的本地开发](05-local-dev-fractal.md) | 在组件仓库里建本地联调工作台，连着依赖跑自己 |
| [06 契约与产物](06-artifacts-and-contracts.md) | `artifacts`、契约先行、`brickkit fetch` |
| [07 发布](07-release-workflow.md) | `brickkit release`：检查、打 tag、推送 |
| [08 组件的文档](08-component-doc-spec.md) | 组件仓库带哪几份文档、每份写什么，`BRICKKIT.md` 怎么被带进使用方的项目 |
| [09 多版本共存](09-multi-version-coexistence.md) | 同一个组件的两个版本怎么同时运行、各自连谁 |
| [10 Git 分发](10-git-distribution.md) | 仓库地址怎么推导、缓存怎么工作、私有仓库的鉴权 |
