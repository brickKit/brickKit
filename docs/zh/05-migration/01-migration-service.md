# 迁移容器

## 声明

```yaml
# component.yaml
migration:
  command: ["/app/stock", "migrate"]
```

命令写成数组，就是容器里要执行的那条命令。它用的是组件**自己的镜像**：迁移脚本和组件代码在同一个仓库、同一个镜像里，
永远是同一个版本——代码要的表结构，正是这个版本的迁移建出来的。它是镜像里的命令，组件从源码以进程运行时照原样跑不了，那是
`local.migrationCommand` 的事（见[不跑迁移容器的情况](#不跑迁移容器的情况)）。

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
   组件：shop/stock@0.2.0
   看日志：docker compose -p brickkit-shop logs shop-stock-0-2-0-migration
   建议：
   1. 迁移失败时主服务不会启动：它要等迁移成功结束
   2. 修好之后重新 brickkit up：迁移容器会再跑一次
```

```text
shop-stock-0-2-0-migration-1  | shop/stock: migration 002 failed: column "warehouse" already exists
```

修好之后再 `up` 就行：迁移容器每次 `up` 都会跑。所以**迁移必须是幂等的**——已经做过的变更再跑一遍什么都不做。
常见的做法是一张迁移记录表，记下已经执行过哪些步骤。几个组件共用一个库时，这张表的主键要带上组件标识，否则它们的记录会互相覆盖。

## 不跑迁移容器的情况

**规则：代码在哪里跑，迁移就在哪里跑。** 来自镜像的代码用那个镜像的 `migration.command` 迁移；来自源码的代码从源码迁移。
从源码运行的组件有两种：

- 以本机进程运行的组件：`mode: local`、`mode: debug`，或者焦点运行里的焦点组件；
- 外壳以这样的进程运行、从成员的本地仓库编进代码时，那个成员（见[与外壳的交互](04-shell-interaction.md#外壳以本机进程运行时)）。

两种都没有迁移容器。它们的 `migration.command` 也不能直接拿到本机来跑：它在组件的镜像里运行（`/app/stock migrate` 是镜像里的
路径），而你跑的源码里的迁移可能比镜像里的新——镜像里甚至可能是同一版本的另一份代码。迁移得从源码运行，怎么运行只有组件自己
知道，平台不猜。所以由组件在 `local.migrationCommand` 里写明，写法同 `local.runCommand`（argv；程序含路径分隔符时相对组件目录）。
它要求组件有 `migration`；带着代码（`deployment.build`）、有 `migration` 却没写它的组件，`brickkit lint` 会警告：

```yaml
migration:
  command: ["/app/stock", "migrate"]               # 在镜像里
local:
  migrationCommand: ["go", "run", ".", "migrate"]  # 从源码
```

**`mode: local`**（成员则是它的外壳为 `mode: local`）。`up` 在容器起来之后、启动这个进程之前，在组件目录里跑它——外壳先按启动顺序
跑各成员的，再跑自己的。它拿到的环境变量与进程相同；成员拿到的是它在外壳里收到的那一份，`up` 同时写进
`local-debug.<成员的服务名>.env`。输出行首是 `<服务名>-migration`。失败了，进程就不启动，会话以 `MIGRATION_FAILED` 结束——和迁移
容器失败挡住主服务是同一条规则。`up --dry-run` 把它列在启动前会执行的迁移里。它和容器一样每次 `up` 都跑，所以同样要幂等。

**`mode: debug`。** 进程由你自己启动，`up` 什么也不跑，而是提醒你，并写出命令和该用的环境变量文件（成员的提醒还会点名它的外壳）：

```text
⚠️ 提示：mode: debug 组件的数据库迁移不会自动执行
   组件：shop/stock@0.2.0
   原因：mode: debug 的组件不生成容器，它的迁移容器也一并跳过
   建议：
   1. 从源码手动运行它的迁移，在它的源码目录里：go run . migrate
   2. 环境变量用 local-debug.shop-stock-0-2-0.env 里的那一份
```

在组件目录里：`set -a && source <项目>/.brickkit/generated/local-debug.<服务名>.env && set +a`，再执行那条命令。

**没写 `local.migrationCommand` 时**，两种 mode 下迁移都不会跑——也不拿镜像代替——`up` 会说明为什么给不出命令：

```text
⚠️ 提示：mode: local 组件的数据库迁移不会自动执行
   组件：shop/stock@0.2.0
   原因：mode: local 的组件不生成容器，它的迁移容器也一并跳过
   建议：
   1. 它的 migration.command（/app/stock migrate）在它的镜像里运行，照原样在这台机器上跑不了
   2. 要从源码运行，组件要在 component.yaml 里写 local.migrationCommand（见它的 BRICKKIT.md）；mode: local 时 up 会在启动进程之前自己跑它
```

这时去组件的 `BRICKKIT.md` 里找怎么从源码运行迁移；组件是你自己的，就写上 `local.migrationCommand`。

**它在顺序里的位置。** 从源码运行的迁移在引擎跑完容器之后才跑，所以不在[版本链](03-multi-version-chain.md)里。同一组件**更新**的版本
这次在容器里迁移时，两者会从新往旧跑；`up` 在启动任何东西之前停下（`CONFIG_INVALID`），点名两个版本——这次不跑更新的那个，
或者这一版改用镜像跑。旧版本在容器里不要紧：它先跑。
