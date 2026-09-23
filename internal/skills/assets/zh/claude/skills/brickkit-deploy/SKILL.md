---
name: brickkit-deploy
description: 把 BrickKit 项目部署到 Docker 或 Kubernetes、绑定数据库/缓存/消息队列等基础资源、配置密钥、对外暴露服务时使用。含两条部署路径的差别、地址注入的格式、资源绑定怎么声明、以及生产环境的密钥处理。当用户提到 deploy / target / k8s / compose / ingress / 资源绑定，或问「怎么上线」时，这个技能适用。
---

# 部署与资源绑定

## 什么时候用这个技能

- 要把项目跑到 Docker 或 Kubernetes 上
- 组件报「资源依赖未满足」
- 要接数据库、缓存、消息队列、对象存储、搜索、SMTP
- 要把某个组件暴露到集群外
- 要处理密码、Token 这类密钥
- 要配多个环境（开发 / 生产）

## 你会猜错的地方

**1. `brickkit.yaml` 自己的 `deploy.target` 只接受两个值：`docker` 和 `k8s`。**

`deploy.target` 必填。`override.yaml` 能在本地多加一个选项：它自己的 `target` 字段接受
`docker` / `podman` / `k8s`，但只能是对 `brickkit.yaml` 取值的一次**降级**（k8s → docker/
podman 允许，反过来绝不允许）。目前还没有真正能跑的 Podman 引擎——对着 podman target 跑
`up --dry-run` 能生成 compose 文件，但真跑 `up` 会明确报错（`ENGINE_MISSING`），不会做
任何实际的事。

**2. 资源本身由运维部署，平台不装数据库。**

`brickkit.yaml` 的 `resources` 是**声明与绑定**，不是「让平台起一个 postgres」。
平台管的是连接身份（地址、账号、密码怎么注入进组件），产品特有的旋钮走组件的
`configSchema`。库本身要使用者先建一次。

**3. `kind` 是封闭枚举，六类：** `database` / `cache` / `mq` / `storage` / `search` / `smtp`。

绑定里「这个组件占哪一块」这一格**按 kind 用不同的名字，只能写一个**：

| kind | 那一格叫 | 注入成 |
| --- | --- | --- |
| `database` | `database` | `DATABASE_NAME` |
| `mq` | `vhost` | `MQ_VHOST` |
| `storage` | `bucket` | `STORAGE_BUCKET` |
| `search` | `index` | `SEARCH_INDEX` |
| `cache` / `smtp` | **没有这一格** | 写了会报错 |

用错名字会报错，并点名该用哪个。

**4. `kind` 与 `engine` 要与组件声明的完全一致。**

组件在 `component.yaml` 里声明 `kind: database` + `engine: postgresql`，
项目里给的资源必须两项都对得上，否则报「资源依赖未满足」。

**5. 密码必须通过环境变量引用，不能写明文。**

```yaml
password: ${DB_PASSWORD}
```

`.env` 已经在 `.gitignore` 里。

**6. `publicKeys` 是唯一让验签真正生效的字段。**

一个公钥都没配时，签名校验**整体失效**，`requireSignature: true` 也一并不起作用——
没有信任锚点就没有可校验的对象。CLI 会警告一句，但那时它已经什么都没验过了。

公钥必须配在项目里、而不是跟着签名从市场取——否则就成了市场自己给自己发证,
市场被攻破时攻击者把组件和公钥一起换掉，验签照样通过。

**7. 没有 overlay / 继承 / 合并机制。**

多环境是**每个环境一份完整自包含的配置文件**，比如 `brickkit.prod.yaml`，
用 `brickkit up --config brickkit.prod.yaml` 指定。别去找「只覆盖差异」的写法，
那是被明确拒绝的设计。

**8. `limits` 没有默认值，都没写就不生成。**

只有 `requests` 有默认（`100m` / `128Mi`）。平台不猜 `limits`——猜一个数字的后果是
去 OOMKill 一个跑得好好的组件。建议**相反地设**：CPU 设 requests、不设上限
（CPU limit 走 CFS quota，节点空闲时也会限流成 p99 毛刺）；内存 requests = limits
（拿 Guaranteed QoS，缺内存时最后被驱逐）。

**9. 别为省内存去合并组件。**

硬约束只有「一个节点上所有 Pod 的 `requests` 之和 ≤ 节点 allocatable」，`limits` 之和
可以远超容量，超卖是正常用法。真正的成本是每个进程的内存地板，几乎完全由语言决定：
Go 8–20MB、Python/Node 40–90MB、JVM 200–450MB。20 个 Spring Boot 光空转就 4–9G。
那时该换运行时或用 `mode: disable` 少跑几个，合并组件是解错了题。

**10. 平台不做网关，但 `labels` 是给网关准备的透传口。**

用户想接 Traefik / Caddy / Prometheus 时，**别劝他放弃，也别建议平台加网关**——
给组件条目写 `labels`，平台原样搬进 Docker 的 service `labels` /
K8s 的 Deployment 与 Pod `annotations`，网关带外部署、自己接进
`brickkit-<项目名>-net` 就能发现它们。平台不解释键值（003 §4.11、012 §2.23）。

```yaml
  - id: erp/sales
    version: 1.0.0
    labels:
      traefik.enable: "true"
      traefik.http.routers.erp-sales.rule: "PathPrefix(`/erp/sales`)"
```

三个坑：**值必须加引号**（`traefik.enable: true` 当场报错）；**平台自己的键会被拒**
（`app`、`brickkit.io/*`、`com.docker.compose.*`）；**钉在 `mode: debug`（写在 `override.yaml`
里）或 `mode: local` 的组件上写了会警告**——两者都不生成容器，没有挂标签的对象。

别退回去手写 file-provider 配置：那份文件里必须写满**版本化服务名**
（`erp-sales-1-0-0`），组件每升一次版本就静默过期一次，而平台一个字都不会提醒。

## 机制是怎么运作的

**地址格式两个环境完全一样**：`http://<版本化服务名>:<端口>`。本地是
`http://people-basic-1-0-0:8080`，K8s 上也是同一个字符串。所以组件代码零修改。
这也意味着多版本天然共存——它们是两个互不冲突的 DNS 名。

**暴露到集群外**靠组件条目上的 `expose: true`。K8s 下还必须给 `hostname`；
`exposePort` 只在 Docker 下生效；`tlsSecret` 只在 K8s + expose 时用。

**`up` 做的事按顺序是**：启停判定 → 生成部署文件 → 生成 `local-debug.env` →
检测镜像权限 → 执行迁移 → 调用引擎。想只看生成结果不真起，用 `--dry-run`。

**本地调试**是在 **`override.yaml`** 里给组件写 `mode: debug`——它绝不能写在 `brickkit.yaml`
本身（解析阶段就直接拒绝）。跑一次 `brickkit override` 创建/刷新那份文件，再在对应组件条目下
加 `mode: debug`（和 `localPort`）。该组件**不生成容器**，而是跑在你宿主机的 IDE 里，
用 `extra_hosts` 把它的版本化服务名映射进容器网络。多个组件可以同时本地调试，
各给一个 `localPort`。CLI 生成 `local-debug.env` 供 IDE 加载。仅限 Docker：
`mode: debug` 配上一个**生效**的 `deploy.target: k8s`，会在拿 `override.yaml` 跟
`brickkit.yaml` 做交叉校验时被拒绝——只有 `brickkit.yaml` 自己本来就是 k8s、且
`override.yaml` 没碰 `target` 时才可能达成这个组合，因为 `override.yaml` 只能把 target
往下降，绝不会往上升。

**K8s 特有的那些**（`context`、`namespace`、`podSecurity`、`ingressClass`、
`serviceAccount`、`networkPolicy`、`replicas`）都在 `deploy` 段或组件条目里，
Docker 下写了不生效。`replicas > 1` 时自动生成 PDB。

## 去哪查更细的

（下面的 `docs/...` 与 `AGENTS.zh.md` 路径都在 BrickKit 仓库 <https://github.com/brickKit/brickKit> 里；`docs/` 下每篇有 `en/` 与 `zh/` 两份，内容对等。）

- 参数：`brickkit up --help`、`brickkit down --help`
- 两条部署路径的生成细节、Ingress、迁移 Job：`docs/zh/06-architecture/03-deployment-generation.md`
- 网络策略：`docs/zh/03-guide/13-network-policy.md`
- 六类资源怎么声明、绑定、注入，密钥管理：`docs/zh/06-architecture/05-resource-binding.md`
- `brickkit.yaml` 每个字段的完整参考：`docs/zh/06-architecture/08-brickkit-yaml-reference.md`
- `override.yaml` 本身（schema、降级专用的 target 规则、`brickkit override` 命令）：
  根目录 `AGENTS.zh.md` §7.1
