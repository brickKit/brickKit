# Patterns（推荐实践）

这里是**推荐实践**，不是 BrickKit 平台本身的硬性要求——`brickkit up` 不会检查你有没有照做。每一篇的主体都来自真实生产部署踩出来的经验；个别小节是推荐的工作顺序或概念对照表，没有同样的实战背书，会在该小节开头明说。

## 先搞清楚要解决什么问题

| 你在做的事 | 看这篇 |
| --- | --- |
| 刚拿到一个需求，不确定该拆成几个组件 | [组件设计准则](00-component-design.md) |
| 要给组件写测试，不知道分几层、每层测什么，或者想让 AI 来写组件 | [分层测试](01-testing.md) |
| 要规划种子数据 / 测试数据，怕污染生产 | [种子数据与测试数据规划](02-data-construction.md) |
| 组件要闭源，怕镜像里的代码被反编译出来 | [闭源组件镜像安全](04-closed-source-image-hardening.md) |
| 一个项目要用几十个组件，内存/端口扛不住 | [怎么选部署形态](05-deployment-selection-guide.md) → 可能要看 `servedBy` |
| 已经决定要用 `servedBy` 合并部署 | [声明 servedBy 的部署检查清单](06-servedby-deployment-checklist.md) |
| 要自己写一个"外壳"组件去承载别的组件 | [合格外壳该满足什么](07-shell-implementers-guide.md) |
| 合并进外壳的几个组件要共用一个数据库连接池 | [在外壳里合并数据库连接池](08-shared-connection-pools.md) |
| 要自己部署一套 BrickKit Market | [自己搭一套 BrickKit Market](09-deployment/self-hosted-market.md) |
| 要调用依赖的 `*_ENDPOINT` 地址，不确定 HTTP 客户端要不要为重新部署做特殊处理 | [怎么可靠地调用一个依赖的地址](03-service-addressing.md) |

## 按角色分类

### 组件开发者

- [组件设计准则](00-component-design.md) — 怎么做领域研究，什么时候该做成组件家族而不是一个开关，以及用 DDD 的语言怎么对照组件边界
- [分层测试](01-testing.md) — 后端契约/业务规则/单元/集成四层，前端单元/组件/端到端/视觉回归四层，以及让 AI 写组件的推荐顺序
- [种子数据与测试数据规划](02-data-construction.md) — 两条必须物理隔离的路径
- [闭源组件镜像安全](04-closed-source-image-hardening.md) — 拉取镜像跟私有 Git 仓库不是同一种保证
- [怎么可靠地调用一个依赖的地址](03-service-addressing.md) — Go/Python/Node 的 HTTP 客户端在依赖容器被换掉时的真实测量结果

### 项目部署方 / 平台管理员

- [怎么选部署形态](05-deployment-selection-guide.md) — 拓扑（独立 / 外壳合并 / 混合）× `docker`/`k8s` 的组合怎么选
- [声明 servedBy 的部署检查清单](06-servedby-deployment-checklist.md) — 决定要不要用、怎么用的检查清单
- [合格外壳该满足什么](07-shell-implementers-guide.md) — 写给要自己造"外壳"组件的人
- [在外壳里合并数据库连接池](08-shared-connection-pools.md) — 合并进同一个外壳的组件怎么共用连接池

### 运维

- [自己搭一套 BrickKit Market](09-deployment/self-hosted-market.md) — 部署市场本身，从本地开发到生产环境

## 按主题分类

### 组件设计与质量

- [组件设计准则](00-component-design.md)
- [分层测试](01-testing.md)
- [种子数据与测试数据规划](02-data-construction.md)
- [闭源组件镜像安全](04-closed-source-image-hardening.md)
- [怎么可靠地调用一个依赖的地址](03-service-addressing.md)

### 部署形态与合并部署

- [怎么选部署形态](05-deployment-selection-guide.md)
- [声明 servedBy 的部署检查清单](06-servedby-deployment-checklist.md)
- [合格外壳该满足什么](07-shell-implementers-guide.md)
- [在外壳里合并数据库连接池](08-shared-connection-pools.md)
- [自己搭一套 BrickKit Market](09-deployment/self-hosted-market.md)
