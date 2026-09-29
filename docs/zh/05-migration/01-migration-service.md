# 迁移容器

## 声明

```yaml
# component.yaml
migration:
  command: ["/app/stock", "migrate"]
```

命令写成数组，就是容器里要执行的那条命令。它用的是组件**自己的镜像**：迁移脚本和组件代码在同一个仓库、同一个镜像里，
永远是同一个版本——代码要的表结构，正是这个版本的迁移建出来的。

入口程序要按参数区分"跑迁移"与"启动服务"，而且**不认识的参数必须立刻失败**：

```go
if len(os.Args) > 1 {
	if os.Args[1] != "migrate" {
		log.Fatalf("unknown argument %q", os.Args[1]) // 不认识的参数立刻失败
	}
	fmt.Println("shop/stock: migrations applied")
	return
}
```

否则一个拼错的参数会让迁移容器变成第二个一直在跑的服务：它永远不结束，主服务永远等不到它，而日志看起来一切正常。

## 平台生成了什么

Docker 下，每个声明了迁移的组件多一个一次性的 service：

```yaml
  shop-stock-0-2-0-migration:
    command:
      - migrate
    entrypoint:
      - /app/stock
    environment:
      - COMPONENT_ID=shop/stock
      - COMPONENT_VERSION=0.2.0
      - STOCK_WAREHOUSE=上海仓
    image: shop-stock:0.2.0
    networks:
      - brickkit-net
    restart: "no"
```

主服务**等它成功结束**，而不是等它"起来了"：

```yaml
  shop-stock-0-2-0:
    depends_on:
      shop-stock-0-2-0-migration:
        condition: service_completed_successfully
```

Kubernetes 下它是一个 Job：`up` 先删掉上一次留下的同名 Job，再创建、等它完成，然后才部署主服务。Job 不重试（`backoffLimit: 0`），
而且只跑一次——不论主服务有几个副本（用 Pod 的 InitContainer 的话，三个副本会并发跑三次同一个迁移）。

`up` 会提前告诉你这次会动哪些库：

```text
🔧 启动前会执行的数据库迁移（失败则该组件不会启动）：
   shop/stock@0.1.0  /app/stock migrate
```

## 迁移失败

迁移以非零退出时，主服务不启动：

```text
❌ 错误：数据库迁移失败
   组件：demo/caller@1.0.0
   看日志：docker compose -p brickkit-my-shop logs demo-caller-1-0-0-migration
   建议：
   1. 迁移失败时主服务不会启动：它要等迁移成功结束
   2. 修好之后重新 brickkit up：迁移容器会再跑一次
```

修好之后再 `up` 就行：迁移容器每次 `up` 都会跑。所以**迁移必须是幂等的**——已经做过的变更再跑一遍什么都不做。
常见的做法是一张迁移记录表，记下已经执行过哪些步骤。几个组件共用一个库时，这张表的主键要带上组件标识，否则它们的记录会互相覆盖。

## 不跑迁移容器的情况

组件以本机进程运行时（`mode: debug`、`mode: local`）没有容器，迁移容器也一并跳过，`up` 会提醒你：

```text
⚠️ 提示：mode: local 组件的数据库迁移不会自动执行
```

这时迁移要你自己跑一次：用 `local-debug.<服务名>.env` 里的环境变量，在本机执行组件的迁移命令。
