# 外壳升级

## 外壳的版本决定成员的版本

外壳的镜像里编进的是成员的某个具体版本的代码。所以"外壳这次承载的是成员的哪个版本"不是一个可以随意选的事：它必须就是外壳 `component.yaml` 里 `shell.members` 写的那个版本。

平台只**核对**，从不替你推导：部署文件与 `brickkit.yaml` 说这次承载什么版本，外壳的 `component.yaml` 说它编进了什么版本，两者对不上就报错。

## 升级外壳：换成新外壳编进的那一套

`shop/shell@0.2.0` 编进了 `shop/stock@0.2.0`：

```yaml
metadata:
  id: shop/shell
  name: Shop shell
  version: 0.2.0
  description: 把购物车与库存编进一个进程

shell:
  members:
    - shop/cart@0.1.0
    - shop/stock@0.2.0
```

```bash
brickkit upgrade shop/shell
```

```text
   ⬆️  shop/shell：0.1.0 → 0.2.0
   🔗 shop/stock@0.1.0 的 requiredBy 现在是：shop/cart, shop/order
   🔓 shop/stock@0.1.0 不在新外壳里：挪到顶层、独立运行
📝 已写：brickkit.yaml, deploy.yaml, deploy.local.yaml
```

升级外壳就是换成新外壳编进的那一套成员版本。`shop/stock@0.1.0` 还有别的组件依赖（`shop/cart`、`shop/order`），所以它留下来、挪到顶层独立运行；
没人再要的旧版本则被移除。部署文件跟着改：

```yaml
components:
  - id: shop/shell
    members:
      - id: shop/cart
        skipWaitFor: [shop/order]
      - id: shop/stock
  - id: shop/stock@0.1.0
  - id: shop/order
```

成员条目上你写过的字段（`mode`、`skipWaitFor`……）保留。构建新外壳的镜像、`up`：

```text
📋 启动顺序（拓扑排序）：
   1. shop-stock-0-1-0  无依赖
   2. shop-order-0-1-0  ← 依赖 1
   3. shop-shell-0-2-0  ← 依赖 1（承载 shop/stock@0.2.0、shop/cart@0.1.0；不等 shop/order@0.1.0）
```

一个外壳在 `brickkit.yaml` 里只能有一个版本：同一个外壳的两个版本同时承载成员，谁承载谁就说不清了。

## 只升级成员时

只升级成员（`brickkit upgrade shop/stock`）不会动外壳：外壳镜像里还是旧版本的代码。于是旧版本以 `requiredBy` 留下（外壳也在"要它"的名单里），
外壳下的成员条目钉到旧版本，新的默认版本在顶层独立运行：

```text
   ⬆️  shop/stock：0.1.0 → 0.2.0
   ✅ shop/stock@0.1.0（requiredBy: shop/cart, shop/order, shop/shell）
```

```yaml
components:
  - id: shop/shell
    members:
      - id: shop/stock@0.1.0
      - id: shop/cart
  - id: shop/order
  - id: shop/stock
```

两个版本共用一个数据库时，数据层兼容与否要由成员自己保证（见 [多版本共存](../03-component-guide/09-multi-version-coexistence.md)）。
**升级之后要自己检查成员之间的兼容性**：外壳里的成员彼此进程内调用，平台不知道 `shop/cart@0.1.0` 能不能和一个新版本的库存模块一起工作。

## 版本对不上时的三条出路

手工改部署文件时，可能让外壳去承载一个它没编进的版本。比如把上面两个 `shop/stock` 条目对调：

```text
❌ 错误：这次外壳承载的成员版本，与外壳 component.yaml 里编进的版本对不上
   文件：deploy.yaml
   components[0].members[0]：外壳 shop/shell@0.1.0 编进的是 shop/stock@0.1.0，这次承载的却是 shop/stock@0.2.0
   建议：
   1. 升级 shop/shell：换一个 component.yaml 的 shell.members 里写着 shop/stock@0.2.0 的外壳版本
   2. 把 shop/stock 的条目从 shop/shell 的 members 挪到部署文件的顶层，让它独立运行
   3. 两个版本都留：brickkit.yaml 里已经有外壳编进的版本，把两个部署条目对调——shop/shell 下面的成员条目改成 `- id: shop/stock@0.1.0`，components[2] 那个条目改成 `- id: shop/stock`；shop/stock@0.2.0 独立运行
```

| 出路 | 适合 |
| --- | --- |
| 升级外壳 | 已经有编进了新版本的外壳版本 |
| 成员移出外壳 | 暂时不合并这个成员也可以 |
| 两个版本都留 | 外壳里继续跑旧版本，新版本独立跑，等外壳跟上 |

第三条的改法写得很具体，照着改完 `up` 就能过。

## 镜像与声明对不上

改了外壳 `component.yaml` 的 `shell.members`、却没重新构建外壳镜像时，本机的镜像里还是旧的成员代码。`brickkit build` 构建外壳镜像时在镜像标签里记下了编进的成员版本，
`up` 用本机构建的外壳镜像之前核对它：

```text
❌ 错误：外壳 shop/shell@0.1.0 的本机镜像编进的成员版本，与它的 component.yaml 不一致
   镜像：shop-shell:0.1.0
   镜像里的成员：shop/cart@0.1.0,shop/stock@0.1.0
   component.yaml 声明的成员：shop/cart@0.1.0,shop/stock@0.2.0
   建议：重新构建外壳镜像：brickkit build shop/shell@0.1.0 --force
```

更好的做法是：外壳编进的成员变了，就提外壳的版本号。已发布的外壳版本，`component.yaml`、tag、镜像三者天然一致。
没有这个标签的外壳镜像（手工构建、第三方镜像）无法核对，`up` 只警告一句。
