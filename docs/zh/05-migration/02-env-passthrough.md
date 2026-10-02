# 环境变量透传

## 平台不拼连接串

迁移要连数据库。平台不知道你用的是 PostgreSQL 还是 MySQL、连接串长什么样、要不要 SSL——它也不需要知道。
**迁移容器拿到的环境变量与主服务一模一样**：同一份配置、同一份依赖地址、同一个密钥文件。组件从环境变量里取出主机、端口、库名、用户、口令，自己拼连接串。

## 一个完整的例子

组件声明它要哪些配置：

```yaml
# component.yaml
configSchema:
  type: object
  properties:
    DB_HOST:
      type: string
      description: PostgreSQL 主机名
    DB_PORT:
      type: integer
      default: 5432
      description: PostgreSQL 端口
    DB_NAME:
      type: string
      description: 库名
    DB_USER:
      type: string
      description: 连接用户
    DB_PASSWORD:
      type: string
      secret: true
      description: 连接口令
  required: [DB_HOST, DB_NAME, DB_USER, DB_PASSWORD]

migration:
  command: ["/app/orders", "migrate"]
```

使用方填值——几个组件共用一个库时，地址写成公共变量：

```yaml
# config/vars.yaml
PG_HOST: pg.internal
PG_PASSWORD: ${PG_PASSWORD}
```

```yaml
# config/shop-orders.yaml
DB_HOST: $var:PG_HOST
DB_NAME: orders
DB_USER: orders
DB_PASSWORD: $var:PG_PASSWORD
```

迁移容器和主服务拿到的是同一组环境变量：

```yaml
  shop-orders-0-1-0-migration:
    command:
      - migrate
    entrypoint:
      - /app/orders
    env_file:
      - path: .brickkit/generated/env/shop-orders-0-1-0.env
    environment:
      - COMPONENT_ID=shop/orders
      - COMPONENT_VERSION=0.1.0
      - DB_HOST=pg.internal
      - DB_NAME=orders
      - DB_PORT=5432
      - DB_USER=orders
```

`DB_PASSWORD` 是密钥：Docker 下两者引用同一个 0600 的 env 文件（里面是 `DB_PASSWORD="${PG_PASSWORD}"`，由 `docker compose`
启动时从环境里填上），Kubernetes 下两者引用同一个生成的 Secret。组件代码里：

```go
dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
	os.Getenv("DB_USER"), url.QueryEscape(os.Getenv("DB_PASSWORD")),
	os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_NAME"))
```

## 为什么这样分工

- **连接串的格式是组件的事。** 同一个 PostgreSQL，Go 的驱动、JDBC、SQLAlchemy 要的写法各不相同；平台替你拼，只能拼成其中一种。
- **一份配置两处用。** 迁移和主服务读的是同一组变量，不会出现"迁移连的是 A 库、服务连的是 B 库"。
- **库由使用方准备。** 平台不创建数据库：`DB_NAME` 指向的库要先存在（运维创建一次），表由迁移创建。

依赖地址也一样透传：迁移需要调用别的组件时（比如迁移前先向某个服务注册），它同样拿到 `*_ENDPOINT`。

## 迁移要走另一条连接时

服务经过连接池（比如 transaction 模式的 PgBouncer），迁移却必须直连数据库（DDL、咨询锁、会话级设置）；或者迁移要用权限更高的角色。
平台不给迁移单独改环境变量——迁移读到的每个值，都应该能在这个组件的 `config/` 文件里看到。这是组件自己的一个事实，就在 `configSchema` 里声明成它自己的键：

```yaml
configSchema:
  properties:
    DB_HOST:
      type: string
      description: 服务连的地址（可以是连接池）
    DB_MIGRATION_HOST:
      type: string
      description: 迁移直连数据库的地址；不填时迁移也用 DB_HOST
```

迁移命令读 `DB_MIGRATION_HOST`，为空就退回 `DB_HOST`。这样它出现在说明书里，项目知道要填，`lint` 也核对键名。
