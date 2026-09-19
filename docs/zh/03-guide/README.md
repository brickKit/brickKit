# 动手教程

一组教程，每一篇都对着真实的 CLI 真跑过一遍——不是描述一个机制，是跟着走一遍那个机制。每一条命令、每一段输出，写的时候都真的执行过。这个系列已经写完：12 篇，比旧的 23 篇系列更少、范围划得更紧（为什么见列表后面那条说明）。

1. [把一个项目跑起来](01-first-project.md)——init、add、up、用 HTTP 跟它说上话、改配置、down
2. [平台是怎么决定谁跑起来的](02-what-runs.md)——依赖、`enabled` 级联、`--dry-run`
3. [本地调试一个组件](03-local-debugging.md)——`local: true`
4. [部署到 Kubernetes](04-kubernetes.md)——外加一个 `brickkit down` 和共用命名空间之间的真实的坑
5. [升级，以及让多个版本并存](05-upgrades-and-versions.md)
6. [拼装一个真实的系统，然后故意把它弄坏](06-assemble-and-break.md)——这次配一个真实数据库，两种不同的真实失败模式
7. [消费别人的组件](07-consuming-artifacts.md)——产物与 API 文档
8. [从市场发布与安装](08-marketplace.md)——外加版本不可变性与私有可见性
9. [给组件签名与验签](09-signing.md)
10. [从零开发自己的第一个组件](10-build-your-own.md)
11. [网络策略与最小权限](11-network-policy.md)
12. [多项目共享](12-multi-project-sharing.md)

跟旧的 23 篇系列相比，有两处刻意的不同，都在各自发生的地方解释过，这里不重复：第 4 篇已经把真实组件部署到了 Kubernetes，所以后面不会再像旧系列那样单独重复一篇"同一套系统部署到 K8s"；从第一篇之后的每一篇，用的都是同样这两三个最小夹具组件（`demo/hello`、`demo/caller`、`infra/redis-event-bus`），而不是搭一个更接近真实业务的大组件——每一篇要讲的是一个平台机制，不是一个业务场景，夹具就该刻意保持很小。

不算教程步骤，但跟着教程走到一半出岔子时天然会用到：[故障排除](../08-troubleshooting.md)——`up`/`down` 和签名验证最常见的坑，症状 → 真实原因 → 解决。
