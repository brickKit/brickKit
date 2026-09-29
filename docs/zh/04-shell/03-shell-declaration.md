# 外壳声明

关于一个外壳，三个文件各说一件事：

| 文件 | 说什么 | 谁写 |
| --- | --- | --- |
| 外壳的 `component.yaml`：`shell.members` | **外壳是什么**：构建时编进了哪些成员、各是哪个精确版本 | 外壳作者 |
| `brickkit.yaml`：`kind: shell` | 这个组件是外壳（一个标记） | CLI 维护 |
| 部署文件：外壳条目下的 `members` | **这次承载什么**：这次运行时哪些成员在外壳里 | 项目 |

平台只核对三者说的是不是同一件事，从不替你推导该怎么跑。

## 外壳是什么：`shell.members`

```yaml
# shell/shop/shell/component.yaml
metadata:
  id: shop/shell
  name: Shop shell
  version: 0.1.0
  description: 把购物车与库存编进一个进程

shell:
  members:
    - shop/cart@0.1.0
    - shop/stock@0.1.0
```

写了 `shell.members`，这个组件就是外壳。每个成员写**精确版本**，与 `dependencies` 同样的写法：外壳的镜像里编进的就是这些版本的代码，
它不能在启动时去加载任意版本——BrickKit 只支持"构建时编进"这一种外壳。

`brickkit build` 构建外壳镜像时，把编进的成员版本记在镜像标签里：

```text
io.brickkit.shell.members = shop/cart@0.1.0,shop/stock@0.1.0
```

`up` 用本机构建的外壳镜像前会核对它，见 [外壳升级](07-shell-upgrade.md#镜像与声明对不上)。

## 这次承载什么：部署文件的 `members`

```yaml
# deploy.yaml
components:
  - id: shop/shell
    members:
      - id: shop/stock
      - id: shop/cart
  - id: shop/order
```

成员的条目嵌在外壳条目下面，只嵌一层。它们是完整的部署条目，可以写 `mode`、`skipWaitFor` 等字段。条目写裸 ID 时指默认版本，写 `id@版本` 时指那个版本。

**成员关系只有这一个来源。** 外壳的 `component.yaml` 说它**能**承载谁；部署文件说它这次**确实**承载谁。
一个外壳编进了五个模块，这个项目只用其中两个，就只把那两个放在它下面——其余三个外壳这次不初始化。

## `kind: shell`

`brickkit.yaml` 里外壳的条目带一个 `kind: shell`：

```yaml
components:
  - id: shop/shell
    version: 0.1.0
    kind: shell
```

它由 CLI 在 `add` 外壳时写上；`lint`、`up`、`graph` 都核对它与外壳的 `component.yaml` 一致，也核对放在外壳下面的每个成员确实编进了这个外壳。它只是一个标记，让人读 `brickkit.yaml` 时一眼看出哪个是外壳；
谁在外壳里，不由它决定。

## 为什么分成这几处

- **能力不等于使用。** 同一个外壳镜像可以在不同的项目里承载不同的成员组合；把"承载谁"写进外壳的 `component.yaml`，就只能一个组合。
- **"编进了哪个版本"必须由外壳自己说。** 镜像里是什么代码，只有构建它的人知道；项目那边只能决定用不用、怎么用。
- **每件事只在一处写。** 成员关系只在部署文件里，编进的版本只在外壳的 `component.yaml` 里。同一件事写两处，迟早两处不一致。

三处对不上时，生成出来的东西一定是错的——镜像里根本没有成员的代码、编进的是另一个版本，或者外壳以为自己不是外壳。
所以 `up` 在生成之前就大声失败，而不是等容器跑起来才露馅。
