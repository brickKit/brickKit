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

项目里本来就有、后来才出现能承载它的外壳的组件，`add` **不会**替你把它挪进外壳，也不会问你要不要挪：
放不放进外壳是部署方式的决定——省内存还是要独立扩缩容、这个环境里合不合并——只有你知道，而且可能每个部署文件不一样。
要放，就把它的条目移到外壳条目的 `members` 下面。

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
`expose`、`exposePort`、`hostname`、`tlsSecret`、`replicas`、`resources`、`serviceAccountName`、`labels`。它这次没有自己的容器，这些字段无处可用。
它们留在条目上是有用的：外壳不跑、成员回落成独立组件时，它们就生效。

同样，成员 `component.yaml` 里的 `healthCheck` 在外壳里不用——健康检查的是外壳这个进程，用外壳自己的 `healthCheck`。
成员的端口要对外开放，在外壳里没有办法单独开；需要单独对外开放的成员，不要放进外壳。

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
- 跳过的依赖跟它在同一个外壳里时，调用不出进程、本来就没有等待，写了会被警告。
- Kubernetes 下 Pod 之间本来就没有启动顺序，以本机进程运行的组件没有 `depends_on`：这两种情况下写了只警告、不生效，合并造成的环也不拦。
