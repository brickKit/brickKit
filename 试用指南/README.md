# BrickKit 试用指南

按顺序走一遍，就能把**目前已实现的每一项能力**亲手试到。

每一节的结构都一样：

| 小节 | 内容 |
| --- | --- |
| 🎯 这一节验证什么 | 一句话 |
| ▶️ 操作 | 可直接复制的命令 |
| ✅ 预期 | 你应该看到什么 |
| ⏹️ 怎么停 | **每一节结束时怎么收尾**，不留后台进程 |
| 📁 代码在哪 | 这个能力由哪些文件实现，想深看时从这里进 |

---

## 顺序

先做 `00`，其余按编号走。`01`–`06` 只要 Docker，`07` 需要 minikube，`09` 需要把市场后端跑起来，
`11` 接着 `09` 做（发布者侧还要装 cosign，使用者侧不需要）。

**`12`–`16` 是"把前面学的放在一起看"**：一个八组件的真实项目长什么样、
组件之间的关系在故障时有什么区别、怎么调用别人写的组件、同一套东西搬到 K8s，
以及**怎么写你自己的组件**。

`12`–`14` 只要 Docker；`15` 需要 minikube；`16` 从头开始，只要 Docker。
想直接动手写组件的话，`16` 可以跳过前面单独看。

| # | 文件 | 内容 | 前置 |
| --- | --- | --- | --- |
| 00 | [00-准备.md](00-准备.md) | 编译 CLI、准备试验场、确认环境 | — |
| 01 | [01-初始化项目.md](01-初始化项目.md) | `init`、目录结构、`.gitignore`、`reset` | 00 |
| 02 | [02-添加与移除组件.md](02-添加与移除组件.md) | 安装源、`add`、产物下载、`remove`、`sync` 整理源码工作区 | 01 |
| 03 | [03-依赖与启动顺序.md](03-依赖与启动顺序.md) | `order`、强/弱依赖、循环依赖、多版本共存 | 02 |
| 04 | [04-组件开启模式.md](04-组件开启模式.md) | **enabled 三态、级联禁用、`--only`、expose** | 03 |
| 05 | [05-Docker启动全流程.md](05-Docker启动全流程.md) | `up` / `status` / `down`、迁移、资源、建库 | 04 |
| 06 | [06-本地调试.md](06-本地调试.md) | `local: true`、`local-debug.env`、双向打通 | 05 |
| 07 | [07-K8s部署.md](07-K8s部署.md) | minikube 上的完整部署、迁移 Job、Ingress | 05 |
| 08 | [08-升级与多版本.md](08-升级与多版本.md) | 改版本号即升级、兼容性检查、多版本共存 | 05 |
| 09 | [09-组件市场.md](09-组件市场.md) | 起市场后端、`login`、`publish`、从市场安装 | 01 |
| 10 | [10-排障速查.md](10-排障速查.md) | 出错了先看这里 | — |
| 11 | [11-组件签名.md](11-组件签名.md) | **cosign 签名发布、验签安装、篡改后被拦住** | 09 |
| 12 | [12-完整装配.md](12-完整装配.md) | **八个组件一次装起来**：递归依赖、资源绑定、密钥、config 覆盖 | 05 |
| 13 | [13-依赖组合实验.md](13-依赖组合实验.md) | **亲手弄坏它**：强依赖 / 弱依赖 / 缓存 / 数据源四种故障表现 | 12 |
| 14 | [14-查看组件文档.md](14-查看组件文档.md) | 产物契约、文档中心、gRPC Reflection —— 怎么调别人的组件 | 12 |
| 15 | [15-K8s完整装配.md](15-K8s完整装配.md) | 同一套八个组件搬到 Kubernetes：Ingress、迁移 Job、`${VAR}` 求值 | 12 + minikube |
| 16 | [16-开发自己的组件.md](16-开发自己的组件.md) | **从零写一个组件**：四条硬约束、装进项目、本地调试、发布 | 01 |
| 17 | [17-网络策略与最小权限.md](17-网络策略与最小权限.md) | 依赖声明变成网络边界：谁进得来谁进不来、拿掉 API 令牌 | 15 + 能执行策略的 CNI |
| 18 | [18-多项目与共享.md](18-多项目与共享.md) | **两个项目怎么共享**：先问状态在哪、共享资源、一个字段就隔离 | 05 |

---

## 选择速查：我该怎么选？

按编号走一遍是学，这张表是**用**——遇到岔路时直接查。

| 我要决定的事 | 选项 | 去哪 |
| --- | --- | --- |
| 部署到哪 | `docker` / `k8s` | [05](05-Docker启动全流程.md) · [07](07-K8s部署.md) · [15](15-K8s完整装配.md) |
| 这个组件要不要跑 | `enabled` 三态（不写 / true / false） | [04](04-组件开启模式.md) |
| 要不要对外暴露 | `expose` + `exposePort`（Docker）/ `hostname`（K8s） | [04](04-组件开启模式.md) · [07](07-K8s部署.md) |
| 组件跑容器里还是跑我的 IDE | `local: true` | [06](06-本地调试.md) |
| 数据库/Redis 谁来起 | `host` 含不含点 | [05](05-Docker启动全流程.md) |
| 连本机上已有的库 | `host: host.docker.internal` | [05](05-Docker启动全流程.md) |
| 一个组件要连两个同类库 | `envPrefix` | [05](05-Docker启动全流程.md) |
| 组件从哪来 | `local` / `git` / `market` 三种安装源 | [02](02-添加与移除组件.md) |
| 升级，还是两个版本并存 | 改版本号 / 写两条 | [08](08-升级与多版本.md) |
| K8s 升级后旧版本怎么办 | 自动清理，并打印清掉了什么 | [08.5](08-升级与多版本.md) |
| 我写组件时依赖写强还是弱 | `optional: true` | [13](13-依赖组合实验.md) · [16](16-开发自己的组件.md) |
| 我写组件时资源是加速器还是数据源 | 挂了该降级还是该 503 | [13](13-依赖组合实验.md) |
| 怎么调用别人写的组件 | 产物契约 / 文档中心 / grpcurl | [14](14-查看组件文档.md) |
| 要不要验签 | `requireSignature` + `publicKeys` | [11](11-组件签名.md) |
| 要不要网络策略、要不要拿掉 API 令牌 | `networkPolicy` / `serviceAccount` | [17](17-网络策略与最小权限.md) |
| 监控 / 备份要连进来 | `networkPolicy.allowFrom` | [17.2](17-网络策略与最小权限.md) |
| 集群那些字段（命名空间 / PSA / 私有镜像 / TLS） | `namespace`、`podSecurity`、`imagePullSecrets`、`tlsSecret`… | [07](07-K8s部署.md) |
| **两个项目要用"同一个东西"** | 先问状态在哪，三种形态 | [18](18-多项目与共享.md) |
| 出错了 | — | [10](10-排障速查.md) |

---

## 全局约定（先读这一段）

### 所有命令都是"跑完就退出"

BrickKit CLI **不是常驻服务**。`brickkit up` 会一直等到组件健康才返回，除此之外没有任何后台进程。所以：

- **想中断某条正在跑的命令：** `Ctrl-C`。
- **`brickkit up` 中途 Ctrl-C 了怎么办：** 容器可能已经起了一半。执行 `brickkit down` 收尾即可，不会有残留。

### 真正需要"关掉"的只有三样

| 东西 | 怎么停 | 怎么彻底删掉 |
| --- | --- | --- |
| 项目里的组件容器 | `brickkit down` | `docker compose -p brickkit-<项目名> -f .brickkit/generated/docker-compose.yaml down -v`（`-v` 连数据卷一起删） |
| minikube 集群 | `minikube stop` | `minikube delete` |
| 市场后端 | `cd deploy/market && docker compose down` | `docker compose down -v` |

`brickkit down` **不会删数据卷**，数据库里的数据一直在——这是有意的（004 §3.6）。

### 每一节都可以独立重来

试验场在 `试用指南/playground/` 下，随时可以整个删掉重建：

```bash
cd <仓库根目录>
./试用指南/准备.sh --reset      # 删掉旧的试验场，重新准备一份干净的
```

---

## 代码位置地图

想看某个能力怎么实现的，从这里进：

| 能力 | 代码位置 | 设计书 |
| --- | --- | --- |
| 命令入口、参数、输出 | [internal/cli/](../internal/cli/) | 004 |
| `brickkit.yaml` 解析与校验 | [internal/config/](../internal/config/) | 003 |
| `component.yaml` 解析与校验 | [internal/manifest/](../internal/manifest/) | 002 |
| 安装源（market / git / local） | [internal/source/](../internal/source/) | 003 §6 |
| 依赖解析、拓扑排序 | [internal/resolver/](../internal/resolver/) | 004 §5 |
| 级联启停（enabled 三态） | [internal/cascade/](../internal/cascade/) | 003 §4.3 |
| 环境变量注入、资源配额合并 | [internal/inject/](../internal/inject/) | 004 §5.6、006 §5 |
| 生成 docker-compose.yaml | [internal/compose/](../internal/compose/) | 005 §3 |
| 生成 K8s 清单 | [internal/k8s/](../internal/k8s/) | 005 §5 |
| 调 docker / podman / kubectl | [internal/engine/](../internal/engine/) | 005 §7 |
| 升级兼容性检查 | [internal/upgrade/](../internal/upgrade/) | 002 §7.7 |
| 配置备份与恢复 | [internal/backup/](../internal/backup/) | 003 §9 |
| 组件源码工作区（`sync` / `--repo`） | [internal/workspace/](../internal/workspace/) | 004 §3.9 |
| 市场后端 | [market-server/](../market-server/) | 007 |
| 试验用的真实组件 | [tests/components/](../tests/components/) | 009 |

设计书全在 [design/](../design/)，导航见 [design/000 阅读指南与文档导航.md](../design/000%20阅读指南与文档导航.md)。

---

## 还没实现的（试的时候别找）

### 还没实现的

| 能力 | 状态 |
| --- | --- |
| 多副本 / HPA | ⬜ `replicas` 目前固定为 1（005 §5.8）。PodDisruptionBudget 也因此刻意不生成，见 [17.6](17-网络策略与最小权限.md) |
| 镜像签名 | ⬜ 目前签的是 **Manifest**，不是镜像。镜像签名存在 registry 里、由集群准入控制器校验，CLI 碰不到（P29） |
| 市场侧密码学校验 | ⬜ 市场只做结构校验。它手里没有可信公钥，真实校验在 CLI 侧（P30）。签名本身可用，见 [11-组件签名.md](11-组件签名.md) |
| `podman compose` 端到端 | ⚠️ Podman 已装且单项验过，但整条 `brickkit up` 走 podman 没跑通（本机 docker CLI 版本与 podman 兼容 API 不合，P27） |

完整的进度与延后清单见 [开发进度/](../开发进度/README.md)——
其中 [延后实现清单](../开发进度/延后实现清单.md) 是 P 编号的权威出处。
