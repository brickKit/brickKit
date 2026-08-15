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

| 能力 | 状态 |
| --- | --- |
| 签名校验（`--sign` / `requireSignature`） | ⬜ Step 20 |
| Podman | 代码已就绪，本机未装，没在真 Podman 上跑过 |

完整的进度与延后清单见 [开发进度.md](../开发进度.md)。
