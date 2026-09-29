# configSchema 设计准则

`configSchema` 是组件写给使用者的**配置说明书**：这个组件认哪些环境变量、各是什么意思、缺省是什么、哪些必须由项目提供。
使用者照着它在 `config/<组件>.yaml` 里填值（`add` 按它生成骨架），平台照着它把值注入成环境变量。

## 平台怎么对待它

**平台不理解配置的含义。** `DB_HOST` 是数据库地址还是缓存地址、`TIMEOUT` 是秒还是毫秒，平台不知道也不猜——那是组件自己的事。
平台只做两件事：把键原样注入成环境变量；按说明书检查使用者写下的**键名**。

**只检查键名，不检查值。** 这是刻意划的线：

- 使用者把键名写错了（`QUOTE_PREFX`），**运行时什么都不会发生**——变量根本不存在，组件走它自己的缺省分支，看上去一切正常，只是配置没生效。
  这种错误只有平台能发现，所以平台一定要说：

  ```text
  ⚠️ demo/quote@0.1.0：QUOTE_PREFX 不在组件的 configSchema 里，不会生效
     文件：config/demo-quote.yaml
     已声明的配置项：QUOTE_PREFIX
     建议：是不是想写 QUOTE_PREFIX？
  ```

- 使用者把值写错了（端口写成 `"abc"`），组件启动时解析它就会**大声失败**——组件自己就能发现，而且怎么处理错值（报错、降级、回落到缺省）是组件的决定。
  平台一旦开始检查值，就要一路检查下去：类型、`enum`、`minimum`、`pattern`……JSON Schema 的能力无穷无尽，而每多检查一样，平台就多替组件做一个决定。

所以 `enum`、`minimum`、`maximum`、`pattern`、`items` 可以写、也应该写，但它们是**给人读的说明**，不是安检机。

唯一的例外是**必填项**：`required` 里的键、又没有 `default`，使用者没填时 `up` 拒绝启动并点名缺哪一项。
这一类同样是"运行时发现不了"的——一个没配置的上游地址不会让组件崩溃，只会让某一条调用路径永远不通。

## 命名

- **键名就是环境变量名，原样注入。** 用大写加下划线（`DB_HOST`），不做 camelCase 转换——组件代码里读的就是这个名字，一眼能对上。
- **不要撞上平台保留的名字。** 平台自己要注入 `COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`，
  以及所有以 `_ENDPOINT` 结尾的依赖地址。撞上了，你的配置项会被忽略、平台的值优先，`lint` 会警告：

  ```text
  ⚠️ 配置冲突：组件 demo/widget 的配置项已被忽略
     组件：demo/widget
     配置项：UPSTREAM_ENDPOINT
     冲突的保留模式：*_ENDPOINT
     处理：该配置项已被忽略，平台注入的值优先
     来源：components/demo/widget/component.yaml
     建议：
     1. 修改 configSchema 中的配置项名称，避开平台保留变量
     2. 例如改为 UPSTREAM_BASE_URL
  ```

- **一个组件的变量名只是它自己的。** 不需要加组件前缀（`QUOTE_` 之类）：每个组件在自己的容器里读自己的环境。
  几个组件要同一个值（同一个数据库地址），是使用方在 `config/vars.yaml` 里用 `$var:` 解决的事，不是组件改名字的理由。

## 拆多细

一项配置对应**一个使用者会单独决定的值**。

**太笼统：** 一个 `CONFIG` 塞一整段 JSON。

```yaml
    CONFIG:
      type: string
      description: 所有配置，JSON 格式
```

使用者看不出里面有哪些项、哪些必填；改一个值要重写整段；键名检查帮不上任何忙；升级时默认值变了也只能整段冲突。

**太细碎：** 把连接字符串的每个片段都拆出来，`DB_SSL_MODE`、`DB_POOL_MIN`、`DB_POOL_MAX`、`DB_POOL_IDLE_TIMEOUT`……几十项，
而使用者真正会改的只有两三个。没人需要改的就留在代码里，或者给一个合理的 `default`。

**刚好：** 使用者会因为环境不同而改的，各占一项；几乎没人改的，给默认值。

## 好的设计

**数据库连接**：地址与凭据分开，口令标成密钥。

```yaml
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
      description: 库名。库由使用方创建一次；表由组件的迁移创建
    DB_USER:
      type: string
      description: 连接用户
    DB_PASSWORD:
      type: string
      secret: true
      description: 连接口令
  required: [DB_HOST, DB_NAME, DB_USER, DB_PASSWORD]
```

`secret: true` 的值在部署时走密钥通道（Docker 下 0600 的 env 文件，Kubernetes 下生成的 Secret），从不明文写进部署文件；
使用方在 `config/` 里写 `${DB_PASSWORD}` 引用它（见 [敏感值](../01-three-layers/07-sensitive-values.md)）。判断哪一项是密钥的只有你——平台不按名字猜。

**读写分离**：组件自己决定要不要分开读写，平台不懂"主从"。要支持，就声明两组地址，读库缺省回落到写库：

```yaml
    DB_WRITE_HOST:
      type: string
      description: 写库主机
    DB_READ_HOST:
      type: string
      description: 读库主机；不配就读写都走 DB_WRITE_HOST
```

**多个同类实例**：两个缓存各一组字段，用业务含义区分，而不是 `CACHE_1`、`CACHE_2`：

```yaml
    SESSION_REDIS_URL:
      type: string
      description: 会话缓存
    RATE_LIMIT_REDIS_URL:
      type: string
      description: 限流计数
```

**功能开关**：布尔项给默认值，说清楚打开之后多了什么。

```yaml
    EXPORT_ENABLED:
      type: boolean
      default: false
      description: 打开后提供 /api/v1/export，导出会占用较多内存
```

**跨项目的服务地址**：你要调用的服务由别的项目部署时，它不在你的依赖里，平台也不会为它注入 `*_ENDPOINT`。
把地址声明成一个必填、没有默认值的配置项——平台推导不出它，只能由使用方填：

```yaml
    NOTIFIER_URL:
      type: string
      description: 通知服务的地址（由运维平台项目部署）
  required: [NOTIFIER_URL]
```

## 写好 `description` 与 `default`

- `description` 会出现在使用方的配置骨架里，每一行的末尾。写它的业务含义和单位，而不是重复键名。
- 默认值说不清的（取值之间怎么选、改了会怎样），写进组件的 [BRICKKIT.md](08-component-doc-spec.md) 的"配置指南"。
- **改默认值是一件要慎重的事。** 使用方没写过的键会自动跟随新默认值；写过的键、而你又改了默认值，升级时就是一处冲突，要他逐条决定
  （见 [升级与配置迁移](../02-project-guide/06-upgrade-and-migration.md)）。

完整的字段规则见 [configSchema 规范](../11-reference/04-config-schema-spec.md)。
