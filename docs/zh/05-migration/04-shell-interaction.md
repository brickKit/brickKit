# 与外壳的交互

外壳成员的代码跑在外壳进程里，但它的**迁移不在外壳里跑**。平台照常为成员生成它自己的迁移容器：

- 用**成员自己的镜像**：每个成员即使平时总在外壳里，也必须有自己的镜像（`deployment.image` 或 `deployment.build`）。
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

外壳以本机进程运行时（`mode: debug` / `mode: local`），它没有容器可以等；成员的迁移容器由 `up` 在启动其余容器之后单独跑一次。
