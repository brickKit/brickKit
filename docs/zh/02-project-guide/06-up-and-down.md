# 启动与停止

## `brickkit up` 做了什么

```bash
brickkit up
```

```text
🚀 启动项目 my-shop（target: docker）
⚠️ 警告：弱依赖缺失：demo/bus@1.0.0
   影响组件：demo/caller@1.0.0
   原因：demo/bus@1.0.0 没有声明在 brickkit.yaml 里
   影响：该组件的环境变量 DEMO_BUS_ENDPOINT 不会被注入
   💡 弱依赖降级由组件自行处理；如需启用，请确认该组件已发布并可从安装源获取
📋 组件状态计算：
   ✅ demo/hello@1.0.0   启动（demo/caller 需要）
   ✅ demo/caller@1.0.0  启动（顶层）

📋 启动顺序（拓扑排序）：
   1. demo-hello-1-0-0   无依赖
   2. demo-caller-1-0-0  ← 依赖 1

可独立启动：demo-hello-1-0-0（无依赖）
最长依赖链（2 层）：demo-hello-1-0-0 → demo-caller-1-0-0

依赖图：
   demo/caller@1.0.0 → demo/hello@1.0.0
                     → demo/bus@1.0.0（弱，未安装）
📄 已生成：.brickkit/generated/compose.yaml

🔧 启动前会执行的数据库迁移（失败则该组件不会启动）：
   demo/caller@1.0.0  /app/caller migrate

🐳 正在启动（docker）...
   demo-hello-1-0-0             running（healthy）
   demo-caller-1-0-0            running（healthy）
✅ 全部组件已启动（2 个）

💡 查看状态：brickkit status
   查看日志：docker compose -p brickkit-my-shop logs -f
```

按顺序：

1. **装载与一致性校验。** 读 `brickkit.yaml`、部署文件、`config/`，以及每个组件的 `component.yaml`。三层对不上（部署文件少了一个组件、
   配置文件里有冲突块、`$var:` 引用了不存在的变量）就停下。
2. **启停判定。** 谁这次启动：顶层组件（没有谁依赖它）没写 `mode` 就启动，下层组件只要上面还有谁在跑、需要它，就跟着启动。
   每一行都写着理由——"顶层"、"demo/caller 需要"、"mode: debug"。
3. **依赖检查与排序。** 强依赖缺了报错；弱依赖缺了警告、不注入它的地址。按依赖关系排出启动顺序。
4. **生成部署文件。** 解析配置、注入环境变量（依赖的地址、组件自己的配置），写出 `.brickkit/generated/compose.yaml`
   （部署目标是 `k8s` 时是一组 Kubernetes 清单）。这份文件是生成物，每次都重写，不要手改。
5. **检查镜像。** 见下一节。
6. **跑数据库迁移。** 组件声明了 `migration.command` 的，先用同一个镜像跑一次迁移；迁移失败，这个组件就不启动。
7. **调用引擎。** `docker compose up`（或 `podman compose`、`kubectl apply`），等每个组件的健康检查通过。

`up` 是幂等的：什么都没改时再执行一次，引擎发现一切都已就位，什么也不动。改了配置再执行，只有受影响的容器会重建。

## 镜像检查：`up` 从不构建

```text
❌ 错误：这些镜像要在本机构建，还没有构建
   demo/hello@1.0.0：demo-hello:1.0.0
   demo/caller@1.0.0：demo-caller:1.0.0
   建议：
   1. up 从不自动构建（构建与部署分离）：先运行 brickkit build
   2. 或者只构建其中一个：brickkit build demo/hello@1.0.0
   3. 或者只构建其中一个：brickkit build demo/caller@1.0.0
```

| 组件 | `up` 怎么检查 |
| --- | --- |
| 来自本地源（正在开发的代码），或者 `component.yaml` 只写了怎么构建、没写 `image` | 镜像必须已经在本机（由 `brickkit build` 构建），不在就报错 |
| 写了 `image` 的 Git / 市场组件 | 镜像在本机，或者能从镜像仓库拉到；拉不到就报错，并提示可以改成本机构建 |

所有问题一次列全，而不是报一个、修一个、再报下一个。为什么不顺手构建，见 [构建与镜像](12-build-and-images.md)。

## `--dry-run`：只生成，不启动

```bash
brickkit up --dry-run
```

走完前四步，写出部署文件，然后停下：

```text
📄 已生成：.brickkit/generated/compose.yaml

🔧 启动前会执行的数据库迁移（失败则该组件不会启动）：
   demo/caller@1.0.0  /app/caller migrate

💡 --dry-run 只生成文件，未启动任何组件
   查看：cat .brickkit/generated/compose.yaml
```

它适合在动手之前审一遍：这次会起哪些组件、每个组件拿到什么环境变量、会动哪些数据库。它也是最完整的离线之外的检查——
依赖解析、外壳与成员的核对都在这里做，而 `lint` 故意不做（见 [离线检查](10-lint-and-checks.md)）。
`--ignore-shells` 配合 `--dry-run`，可以验证每个组件离开外壳还能不能独立启动。

## `brickkit down`

```bash
brickkit down
```

```text
🛑 停止项目 my-shop
✅ 已停止全部组件

💡 数据卷未删除，数据库数据仍然保留
   需要彻底清理时手动执行：docker volume rm <卷名>
   重新启动：brickkit up
```

- **不删数据卷。** 数据库的数据还在；下次 `up` 接着用。要彻底清掉，自己执行 `docker volume rm`——误删数据的代价太大，这一步只能由人来做。
- **只想停几个：** 在部署文件里给它们写 `mode: disable` 再 `up`。它们的容器会被移除，下层只为它们而跑的组件也一起停下，
  而且部署文件里留下了痕迹，下次 `up` 不会又把它们拉起来。
- **`mode: local` 的组件**是运行 `up` 的那个终端在前台看护的进程，只能在那个终端里 `Ctrl+C` 停止；`down` 够不着它，只会提示那个会话的进程号。

## 常见的启动失败

**必填配置没有值。**

```text
❌ 错误：必填的组件配置没有值
   缺少配置：demo/widget@0.1.0 → API_URL
   原因：组件在 configSchema.required 里声明了它，又没有给默认值——这一项平台推导不出来，只能由项目提供
   建议：
   1. 在 config/demo-widget.yaml 里给它一个值：
    API_URL: <值>
   2. 值可以写 ${ENV_VAR}（真值放 .env）、$var:NAME（公共变量，来自 config/vars.yaml）或 file://路径
```

平台宁可在启动前拦住，也不让组件带着一个缺失的地址跑起来、看上去健康、却有一条调用路径永远不通。

**镜像不存在。** 见上面的镜像检查：先 `brickkit build`。

**宿主机端口冲突。** 两个组件对外开放在同一个宿主机端口上，校验部署文件时就报出来：

```text
❌ 错误：deploy.yaml 校验失败
   文件：deploy.yaml
   components[1].exposePort：与 components[0].exposePort 冲突（宿主机端口 18080 已被占用）
   建议：完整字段说明：brickkit docs 11-reference/03-deploy-yaml-schema（网页版：https://github.com/brickKit/brickKit/blob/v1.1.0/docs/zh/11-reference/03-deploy-yaml-schema.md）
```

端口被本机别的程序占着，则是 Docker 启动容器时报错——换一个 `exposePort`。

**组件起不来、健康检查一直不过。** 先看日志：`docker compose -p brickkit-my-shop logs <服务名>`（`-p` 不能少，它指定的是这个项目）。
更多情况见 [故障排除](../10-troubleshooting/01-up-down-issues.md)。
