# 与外壳的交互

外壳成员的代码跑在外壳进程里，但它的**迁移不在外壳里跑**。平台照常为成员生成它自己的迁移容器：

- 用**成员自己的镜像**：每个成员即使平时总在外壳里，也必须有自己的镜像（`deployment.image` 或 `deployment.build`）——
  除非外壳以本机进程运行、成员的代码来自源码（见[下面](#外壳以本机进程运行时)）。
- 用**成员自己的配置**：与它独立运行时一模一样的那组环境变量。
- **外壳等它成功结束才启动**：成员的代码一加载，表结构就得已经就位。

`shop/stock` 在外壳 `shop/shell` 里，生成的是：

```yaml
  shop-shell-0-1-0:
    depends_on:
      shop-stock-0-1-0-migration:
        condition: service_completed_successfully
```

```yaml
  shop-stock-0-1-0-migration:
    command:
      - migrate
    entrypoint:
      - /app/stock
    image: shop-stock:0.1.0
```

外壳的日志之前，是成员迁移的日志：

```text
shop-stock-0-1-0-migration-1  | shop/stock: migrations applied
shop-shell-0-1-0-1            | 2026/09/29 12:02:06 serving shop/cart@0.1.0 on :8081
shop-shell-0-1-0-1            | 2026/09/29 12:02:06 serving shop/stock@0.1.0 on :8082
```

这样分的好处：

- **外壳不必懂每个成员怎么迁移。** 成员的迁移命令、迁移用的环境，都是成员自己的。
- **一个成员迁移失败，外壳不启动，报错点名那个成员。** 而不是外壳起来之后某个模块对着一张不存在的表报错。
- **成员移出外壳时什么都不用改。** 它的迁移本来就是单独跑的。

同一个成员的另一个版本在外壳外面独立运行时，两个版本的迁移照样按版本号串成一条链（见 [多版本的迁移顺序](03-multi-version-chain.md)）。

## 外壳以本机进程运行时

外壳是 `mode: local` 或 `mode: debug`（或者焦点运行的焦点）时，没有外壳容器可以等；成员的迁移从哪来，跟着它的代码从哪来：

| 成员 | 外壳进程跑的是 | 成员的迁移 |
| --- | --- | --- |
| 有本地仓库（在 `components/` 下，或在某个本地源里） | 那份工作区里的代码 | 从源码跑：成员的 `local.migrationCommand`。不需要镜像 |
| 来自 Git 或市场，没有本地仓库 | 发布的那一版代码 | 它的迁移容器，用它自己的镜像，由 `up` 在其余容器之后单独跑一次 |

从源码运行的成员不用它的镜像：镜像是某个时候用这个版本构建的，编进外壳的工作区代码之后可能已经改了——机器上甚至可能有一个
同名同版本、代码不同的镜像。和代码对不上的迁移比不跑更糟，因为没有任何东西会报出来。

从源码运行的成员在它自己的目录里迁移，用的是它在外壳里拿到的那份环境——`BRICKKIT_SERVED_MEMBERS_CONFIG` 里的值，地址是
这台机器上连得通的形式——`up` 同时把它写进 `.brickkit/generated/local-debug.<成员的服务名>.env`：

- **外壳 `mode: local`**：启动外壳之前，`up` 按启动顺序跑各成员的迁移，再跑外壳自己的；每条的输出行首是 `<服务名>-migration`。
  有一条失败，会话就以 `MIGRATION_FAILED` 结束，外壳不启动。
- **外壳 `mode: debug`**：外壳由你启动，`up` 什么也不跑，提醒里给出每个成员的命令和 env 文件。
- **成员没写 `local.migrationCommand`**：它的迁移不跑，`up` 会提醒，并点名外壳。

字段与提醒见[不跑迁移容器的情况](01-migration-service.md#不跑迁移容器的情况)。

