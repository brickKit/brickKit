# 依赖解析

`brickkit.yaml` 只写"用哪些组件、哪个版本"，不写谁依赖谁——依赖是每个组件自己在 `component.yaml` 里声明的。
CLI 每次运行时把它们读出来，连成一张图：节点是组件版本，边是依赖。启停判定、启动顺序、地址注入、网络策略，都从这张图算出来。

## 强依赖与弱依赖

```yaml
dependencies:
  components:
    - shop/catalog@1.0.0          # 强依赖
    - id: shop/bus@1.0.0          # 弱依赖
      optional: true
```

| | 缺失时 | 启动顺序 | 地址变量 |
| --- | --- | --- | --- |
| 强依赖 | 报错，不启动 | 它先于依赖方启动（依赖方等它健康） | 一定注入 |
| 弱依赖 | 警告，照常启动 | 不约束启动顺序 | 它在跑时注入；不在跑时**根本不存在** |

两种依赖在启停判定里一视同仁：上面有谁需要它（不论强弱），它就跟着跑。区别只在缺失时的后果、以及要不要等它。

## 拓扑排序

启动顺序是依赖图的拓扑排序：每个组件排在它的所有强依赖之后。一个菱形的例子——`shop/portal` 依赖 `shop/order` 和 `shop/stock`，两者又都依赖 `shop/catalog`：

```text
📋 组件状态计算：
   ✅ shop/catalog@1.0.0  启动（shop/order 需要）
   ✅ shop/order@1.0.0    启动（shop/portal 需要）
   ✅ shop/stock@1.0.0    启动（shop/portal 需要）
   ✅ shop/portal@1.0.0   启动（顶层）

📋 启动顺序（拓扑排序）：
   1. shop-catalog-1-0-0  无依赖
   2. shop-order-1-0-0    ← 依赖 1
   3. shop-stock-1-0-0    ← 依赖 1
   4. shop-portal-1-0-0   ← 依赖 2, 3

可独立启动：shop-catalog-1-0-0（无依赖）
最长依赖链（3 层）：shop-catalog-1-0-0 → shop-order-1-0-0 → shop-portal-1-0-0
   不在这条链上的组件与它并行启动
```

## 菱形依赖

`shop/catalog` 被两条路径依赖，但它只有**一个**节点、只启动一次：两个依赖方写的是同一个精确版本，就是同一个组件版本。

```text
shop/portal@1.0.0
├── shop/order@1.0.0
│   └── shop/catalog@1.0.0
└── shop/stock@1.0.0
    └── shop/catalog@1.0.0（见上）
```

两条路径要的是**不同**的版本时，那就是两个节点——见下面的多版本。

## 循环依赖

几个组件互相**强依赖**成环时，谁都要等对方先起来，启动顺序无解，在解析阶段就报错：

```text
❌ 错误：检测到循环依赖
   循环路径：shop/order@1.0.0 → shop/stock@1.0.0 → shop/order@1.0.0
   原因：这几个组件互相强依赖，谁都要等对方先起来，启动顺序无解
   建议：
   1. 检查 Manifest 中的依赖声明（dependencies.components）
   2. 其中一方改成弱依赖（optional: true）即可——弱依赖不约束启动顺序，环上有一条弱边就不再是死结
```

环上只要有一条弱边，就不是死结：弱边不约束启动顺序，排序时把它去掉就排得出来。`shop/order` 强依赖 `shop/stock`，`shop/stock` 弱依赖 `shop/order`：

```text
📋 启动顺序（拓扑排序）：
   1. shop-stock-1-0-0  无依赖
   2. shop-order-1-0-0  ← 依赖 1

依赖图：
   shop/order@1.0.0 → shop/stock@1.0.0
   shop/stock@1.0.0 → shop/order@1.0.0（弱）
```

两个组件都跑：启停判定算的是"谁**不**跑"的最小不动点，环不需要任何特殊处理——没有谁把它们关掉，它们就都跑。

合并进外壳之后才出现的等待环，是另一回事，见 [成员管理](../04-shell/04-members-management.md#合并造成的启动环与-skipwaitfor)。

## 多版本

依赖写的是精确版本，所以两个组件可以依赖同一个组件的两个版本：它们是图上的两个节点，服务名不同（`demo-hello-1-0-0`、`demo-hello-1-1-0`），同时运行。
`add` 遇到这种情况时把第二个版本也加进 `brickkit.yaml`，标上 `requiredBy`（见 [多版本共存](../03-component-guide/09-multi-version-coexistence.md)）。

反过来，同一个组件的 `dependencies` 里一个组件 ID 只能出现一次：地址变量名不带版本，写两个版本会撞在同一个变量上。

## 最长链决定启动时间

组件多不等于串行步骤多。没有依赖关系的组件并行启动；真正决定一次 `up` 要等多久的，是图上**最长的那条强依赖链**——链上每一个都要等前一个健康。
`up` 把它打出来：

```text
最长依赖链（3 层）：shop-catalog-1-0-0 → shop-order-1-0-0 → shop-portal-1-0-0
   不在这条链上的组件与它并行启动
```

想让启动更快，看这条链：把链上某条不必等的强依赖改成弱依赖（组件要能在对方没起来时自己重试），链就短了。

## Manifest 从哪来

解析要每个组件版本的 `component.yaml`。来自本地源的，每次从目录里读；来自 Git 或市场的，先查项目的永久缓存，没有才去安装源取（见 [Git 仓库缓存](06-bare-repo-mechanism.md)）。
所以 `deps`、`graph`、`up --dry-run` 第一次运行可能要联网，之后同样的版本都走缓存。`lint` 不建这张图，所以从不联网。
