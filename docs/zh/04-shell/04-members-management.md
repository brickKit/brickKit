# 成员管理

## 加入外壳

`add` 一个外壳时，它编进的成员按外壳声明的版本一起加进项目，而且在部署文件里**直接嵌在外壳条目下面**：

```bash
brickkit add --local
```

```text
➕ 加入 shop/cart@0.1.0, shop/shell@0.1.0, shop/stock@0.1.0
   ✅ shop/stock@0.1.0（在外壳 shop/shell 里）
   ✅ shop/cart@0.1.0（在外壳 shop/shell 里）
   ✅ shop/shell@0.1.0
📝 已写：brickkit.yaml, deploy.yaml
📝 配置骨架：config/shop-stock.yaml
```

```yaml
# deploy.yaml
components:
  - id: shop/shell
    members:
      - id: shop/stock
      - id: shop/cart
```

项目里本来就有、版本正好是外壳编进的那一个的成员，**也会被挪到外壳下面**，输出里会说：

```text
   🔗 shop/stock 挪进了外壳 shop/shell
```

加外壳就是决定把它的成员合进去；先加哪个、后加哪个，结果都一样。只有 `add` 外壳本身才会挪：外壳已经在项目里之后，
`add` 不会再把任何条目挪到它下面，你移出来独立运行的成员会一直留在外面（之后新加的、外壳编进的组件一开始就嵌在下面）。
已经嵌在别的外壳下面的成员不动，会给一条提示。`add` 写的是 `deploy.yaml`（和 `deploy.local.yaml`）；用 `-f`
挑的其他部署文件会列为"没有改"。某个环境里想让成员独立运行，就在那个文件里把它的条目移回顶层（见[移出外壳](#移出外壳)）。

## 移出外壳

把成员的条目从 `members` 下面挪到顶层：

```yaml
components:
  - id: shop/shell
    members:
      - id: shop/cart
  - id: shop/stock          # 独立运行
```

下一次 `up`，`shop/stock` 用它自己的镜像在自己的容器里跑，外壳只承载 `shop/cart`，指向 `shop/stock` 的地址改回它自己的服务名。
**它的配置一个字都不用改**：`config/shop-stock.yaml` 写的是它要什么值，与它跑在哪里无关。部署文件的 `members` 只决定"怎么运行"，不改变配置本身。

同样的道理，在某一份部署文件里把成员条目写成 `mode: disable`，外壳这次就不承载它；在 `deploy.local.yaml` 里写成 `mode: debug` 或 `mode: local`，
它这次在你的机器上以本机进程运行，外壳也不承载它（见 [本地调试](../02-project-guide/03-local-debug-workflow.md#外壳成员的调试)）。

## 外壳不跑时

外壳被 `mode: disable` 关掉、或者这次没有启动时，它下面的成员不会跟着消失——它们**回落成独立组件**，各自按自己的条目部署。

## 删除外壳

```bash
brickkit remove shop/shell
```

外壳的条目删掉，它承载的成员挪回部署文件顶层、独立运行，成员的配置保留。

## 外壳里的成员哪些字段不生效

成员条目是完整的部署条目，但外壳承载它时，描述"它自己的容器怎么部署"的字段**不生效，也不警告**：
`replicas`、`resources`、`serviceAccountName`、`labels`。它这次没有自己的容器，这些字段无处可用。
它们留在条目上是有用的：外壳不跑、成员回落成独立组件时，它们就生效。

同样，成员 `component.yaml` 里的 `healthCheck` 在外壳里不用——健康检查的是外壳这个进程，用外壳自己的 `healthCheck`。

**对外开放是例外：外壳替成员开。** 外壳进程按每个成员声明的端口监听（生成期就核对过，成员的 `*_ENDPOINT` 也靠这一条），
所以成员条目上的 `expose`、`exposePort`、`hostname`、`tlsSecret` 照常生效，落在外壳身上：

| 目标 | 成员写了 `expose: true` 时 |
| --- | --- |
| Docker / Podman | 外壳的容器多一条端口映射：宿主机的 `exposePort`（没写就是成员的主端口）→ 容器里成员的主端口。和别的 `expose` 撞端口时在生成期报错 |
| Kubernetes | 成员有一条自己的 Ingress（`hostname`、`tlsSecret` 照写），指向成员自己的 Service——那个 Service 选中的是外壳的 Pod。开了 `networkPolicy` 时，外壳的策略放 ingress controller 进成员的端口 |

所以一个小的对外组件（BFF、接 webhook 的适配器）也可以进外壳省内存。外壳以裸进程跑（`mode: local` / `debug`）时，成员的端口本来就开在
宿主机上，`expose` 只给一条警告，说清地址。

## 合并造成的启动环与 `skipWaitFor`

组件之间没有环，放进外壳之后却可能出现环。`shop/cart`（在外壳里）依赖 `shop/order`（在外面），而 `shop/order` 又依赖 `shop/stock`（在外壳里）：

```text
shop/cart ──→ shop/order ──→ shop/stock
（外壳里）       （外面）       （外壳里）
```

外壳作为一个整体启动：它继承了每个成员的依赖，所以外壳要等 `shop/order`；依赖成员的组件也就依赖外壳，所以 `shop/order` 要等外壳。
Docker Compose 下这是一个永远起不来的等待环，`up` 在生成之前就拦下：

```text
❌ 错误：把成员放进外壳 shop/shell@0.1.0 之后，外壳要等一个组件，而那个组件又在等外壳
   依赖：shop/order@0.1.0 → shop/stock@0.1.0（在外壳 shop/shell@0.1.0 里）
   依赖：shop/cart@0.1.0（在外壳 shop/shell@0.1.0 里） → shop/order@0.1.0
   原因：组件之间并没有互相依赖成环，但外壳容器是作为一个整体启动的：它继承了每个成员的依赖，依赖成员的组件也就依赖外壳。在 Docker Compose 下这成了一个永远起不来的 depends_on 环
   建议：
   1. 把夹在中间的那个组件也放进这个外壳（把它的条目移到外壳下面），或者把其中一个成员移出外壳（把它的条目移到顶层）
   2. 或者在 component.yaml 里把其中一条依赖改成弱依赖（optional: true）：弱依赖不约束启动顺序
   3. 或者接受代价：在部署文件里给 shop/cart@0.1.0 的条目写上 skipWaitFor: [shop/order]。它就不等 shop/order 直接启动，必须自己重试到对方就绪
```

前两条出路改变结构；第三条保留结构、去掉一处等待：

```yaml
components:
  - id: shop/shell
    members:
      - id: shop/stock
      - id: shop/cart
        skipWaitFor: [shop/order]
  - id: shop/order
```

```text
📋 启动顺序（拓扑排序）：
   1. shop-shell-0-1-0  无依赖（承载 shop/stock@0.1.0、shop/cart@0.1.0；不等 shop/order@0.1.0）
   2. shop-order-0-1-0  ← 依赖 1
```

`skipWaitFor` 只去掉**启动时的等待**：外壳不再等 `shop/order` 就启动。连接不受影响——`shop/cart` 照样拿到 `SHOP_ORDER_ENDPOINT`，照样连得到它。

**代价由写它的人承担。** 外壳启动时 `shop/order` 可能还没就绪，所以 `shop/cart` 的代码必须能重试：初始化时连不上就退出的代码，在这里会反复重启。
写之前确认你的成员能做到这一点。

规则：

- 列出的必须是这个组件版本真实的**强依赖**——拼错的名字、弱依赖（本来就不等）、根本不依赖的组件，写了都会报错，免得你以为那条等待已经去掉了。
- 跳过的依赖跟它在同一个外壳里时，调用不出进程、本来就没有等待；在 Docker / Podman 上写了会被警告。
- Kubernetes 上的 Pod 启动时本来就不互相等，没有等待可跳过。`up` 给写了它的每个条目一条警告——`shop/cart@0.1.0 写了
  skipWaitFor，但 Kubernetes 上的 Pod 启动时本来就不互相等：在这里不起作用`——启动顺序里也没有"不等谁"的说明。
  条目不会被拒：同一份部署文件换回 Docker / Podman，它又会生效。
- 以本机进程运行的组件（或在以进程运行的外壳里）同样没有 `depends_on`，`up` 会警告它的 `skipWaitFor` 这次不起作用。
- 这两种情况下合并造成的环都不拦：启动时谁也不等，就不会卡死。
