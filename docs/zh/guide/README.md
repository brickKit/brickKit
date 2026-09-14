# 动手教程

一组教程，每一篇都对着真实的 CLI 真跑过一遍——不是描述一个机制，是跟着走一遍那个机制。已经写好的每一篇里，每一条命令、每一段输出，写的时候都真的执行过，跟旧的（现已归档的）23 篇系列对自己的要求一样。

这个系列是逐篇写出来的；下面还没挂链接的篇目已经规划好但还没写——完整的推进节奏见 [`docs/superpowers/specs/2026-09-13-bilingual-docs-restructure-design.md`](../../../docs/superpowers/specs/2026-09-13-bilingual-docs-restructure-design.md) §7。在此之前，旧的（已冻结、只有中文、可能与当前 CLI 行为不一致的）23 篇系列仍然可以在 [`docs/archive/guide/`](../../archive/guide/) 读到。

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
12. 多项目共享

（第 4 篇已经用真实组件把 Kubernetes 部署走过一遍了，所以这个系列不会像旧的 23 篇系列那样再单独重复一篇"同一套系统部署到 K8s"。）

不算教程步骤，但会被反复引用：一份排障速查表，等上面这个系列攒够真实、反复出现的失败模式之后再写，不靠猜。
