# 多版本的迁移顺序

## 问题

同一个组件的两个版本同时运行时（见 [多版本共存](../03-component-guide/09-multi-version-coexistence.md)），它们通常连同一个库。
两个版本的迁移如果同时跑，就在同一批表上并发改结构——轻则一方报错，重则把 schema 改坏。

## 按版本串起来

平台把同一个组件的各个版本的迁移按**版本号**串成一条链：低版本的迁移先跑完，高版本的才开始。`shop/stock` 的 0.1.0 与 0.2.0 都声明了迁移时，生成的是：

```yaml
  shop-stock-0-2-0-migration:
    command:
      - migrate
    depends_on:
      shop-stock-0-1-0-migration:
        condition: service_completed_successfully
    entrypoint:
      - /app/stock
    image: shop-stock:0.2.0
```

两个迁移容器的日志，按先后：

```text
shop-stock-0-1-0-migration-1  | shop/stock: migrations applied
shop-stock-0-2-0-migration-1  | shop/stock: migrations applied
```

规则：

- 按版本号比，不是按名字比（`1.10.0` 排在 `1.9.0` 之后）。
- 没有声明迁移的版本跳过，链照样接上。
- **不同组件的迁移不串**：它们各自的链并行跑——两个组件就算共用一个库，也是各管各的表。

低版本先跑的道理：旧版本的迁移建立它需要的结构，新版本的迁移在此之上继续。反过来，旧版本的迁移可能对着一个已经被新版本改过的结构失败。

## 数据兼容是组件作者的事

平台只保证两个版本的迁移不会同时改结构。两个版本同时读写同一批表时是否兼容——新版本加的列旧版本认不认、旧版本写进去的数据新版本读不读得了——
平台无从知道，只能由组件作者设计：通常意味着迁移只做"加法"（加列、加表），不删不改旧版本还在用的结构，等旧版本退役后再清理。
