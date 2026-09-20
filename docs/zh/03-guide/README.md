# 动手教程

一组教程，每一篇都对着真实的 CLI 真跑过一遍——不是描述一个机制，是跟着走一遍那个机制。每一条命令、每一段输出，写的时候都真的执行过。

**文件名前的编号就是阅读顺序：**一篇接着一篇，后面的会用到前面学过的东西。所有教程都默认你已经构建好了 BrickKit CLI，并且 Docker 在跑（第 1 篇的前置条件）；下表最后一列，写的是某一篇**额外**需要的东西。

| 编号 | 教程 | 你会学到 | 另外需要 |
| --- | --- | --- | --- |
| 01 | [把一个项目跑起来](01-first-project.md) | `init`、`add`、`up`，用 HTTP 跟它说上话，改配置，`down` | —— |
| 02 | [平台是怎么决定谁跑起来的](02-what-runs.md) | 依赖、`enabled` 级联、`--dry-run` | —— |
| 03 | [本地调试一个组件](03-local-debugging.md) | `local: true`：让一个组件跑在你的 IDE 里，其余照常在容器里 | —— |
| 04 | [部署到 Kubernetes](04-kubernetes.md) | 同一份声明，只改 `deploy.target`；外加一个 `brickkit down` 和共用命名空间之间的真实的坑 | minikube 和 `kubectl` |
| 05 | [升级，以及让多个版本并存](05-upgrades-and-versions.md) | 改版本号就是升级；两个版本故意一起跑 | —— |
| 06 | [拼装一个真实的系统，然后故意把它弄坏](06-assemble-and-break.md) | 绑定一个真实数据库，见两种不同的真实失败 | 一个 PostgreSQL（教程里用 `docker run` 起） |
| 07 | [消费别人的组件](07-consuming-artifacts.md) | 产物与 API 文档；不安装就拿到它们（`fetch`）；上游还没做好时先立个桩顶上（`brickkit new --contract` 加 `local: true`） | python3（只有桩那一节里的 mock 用得到） |
| 08 | [管理组件源码](08-component-source.md) | `add --repo` 克隆源码；`sync` 只留手边要动的；`remove` 的几道保护；源码跟着项目提交时的 `restore` 与提交钩子 | `git`（不需要 Docker） |
| 09 | [从市场发布与安装](09-marketplace.md) | 发布、安装、退出登录；版本不可变；私有可见性 | 一个市场（教程里用 `docker compose up` 起） |
| 10 | [给组件签名与验签](10-signing.md) | 生成密钥对、签名、验签，以及验签失败长什么样 | cosign（只有发布方需要） |
| 11 | [从零开发自己的第一个组件](11-build-your-own.md) | 四个文件，从空目录到跑起来 | —— |
| 12 | [网络策略与最小权限](12-network-policy.md) | 从依赖图生成 NetworkPolicy，并证明它真的挡住了什么 | 会执行 NetworkPolicy 的 minikube（`--cni=calico`） |
| 13 | [多项目共享](13-multi-project-sharing.md) | 共享资源与隔离资源；把别的项目的组件当成别人的 API | 一个 Redis（教程里用 `docker run` 起） |

**为什么每一篇都用这么小的组件：**第一篇之后的每一篇，用的都是同样这两三个最小夹具组件（`demo/hello`、`demo/caller`、`infra/redis-event-bus`），而不是搭一个更接近真实业务的大组件。每一篇要讲的是一个平台机制，不是一个业务场景，夹具就该刻意保持很小。

**跟着走到一半出岔子了：**[故障排除](../08-troubleshooting.md)按症状列了 `up` / `down`、本地调试和签名验证最常见的坑，外加离线检查（`lint` 与编辑器 schema）的坑，症状 → 真实原因 → 解决。
