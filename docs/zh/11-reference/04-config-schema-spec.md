# configSchema 规格

`configSchema` 是组件 `component.yaml` 里的一块，声明这个组件认哪些配置项。它的写法借用 JSON Schema（draft-07）的一个子集：一个 `type: object`，
`properties` 列出每一项，`required` 列出必填项。设计上怎么拆、怎么命名，见 [configSchema 设计准则](../03-component-guide/03-config-schema-design.md)。

## 结构

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
      minimum: 1
      maximum: 65535
      description: PostgreSQL 端口
    DB_PASSWORD:
      type: string
      secret: true
      description: 连接口令
    REGIONS:
      type: array
      items:
        type: string
      default: [cn-east, cn-north]
      description: 服务的区域
  required: [DB_HOST, DB_PASSWORD]
```

| 写法 | 含义 |
| --- | --- |
| `properties` 的键 | 配置项名，也就是注入的环境变量名，原样注入：必须是合法的环境变量名（字母、数字、下划线，不以数字开头） |
| `type` | `string` / `integer` / `number` / `boolean` / `array` / `object` |
| `default` | 没写值时用它；标量按写的原样注入成字符串（`default: 1.10` 注入的是 `1.10`，不是 `1.1`），`array` / `object` 注入成一行 JSON |
| `description` | 出现在使用方配置骨架的行尾 |
| `secret: true` | 这一项是密钥：部署时走密钥通道，从不明文写进部署文件 |
| `required` | 必填项；每一项必须在 `properties` 里声明过 |
| `enum`、`minimum`、`maximum`、`pattern`、`items` | 允许的取值——**只是说明** |

每一项字段的精确规则见 [component.yaml 字段参考](01-component-yaml-schema.md#configschema)。

## 平台只检查键名，不检查值

这是 `configSchema` 最重要的一条规则。

**键名**，平台查：

| 情况 | 结果 |
| --- | --- |
| `config/` 里写了一个 `configSchema` 里没有的键 | 警告"不会生效"，并猜你想写哪个 |
| 组件没有 `configSchema`，`config/` 里却写了配置 | 警告：整份配置都不会生效 |
| `required` 里的项没有 `default`、`config/` 里也没填 | `up` 拒绝启动，点名缺哪一项 |
| 键名撞上平台保留的名字 | `up` / `lint` 警告并忽略这一项；市场拒收 |

**值**，平台不查：`type: integer` 的项填了 `"abc"`、`enum` 之外的值、超出 `maximum` 的数，平台都照常注入。

分界线在于有没有运行时的安全网。值写错了，组件读它的时候就会失败（解析不了、校验不过），你一定会发现，而且怎么处理错值是组件自己的决定。
键名写错了，变量根本不存在，组件走它的缺省分支照常运行——没有任何东西会失败，配置只是悄悄没生效。这类错误只有平台能发现，所以平台只查这一类。

而一旦开始校验值就停不下来：类型、枚举、范围、正则、嵌套结构……JSON Schema 的能力几乎无穷，每多查一样，平台就多替组件做一个决定。

所以组件要自己在启动时校验值：

```go
port, err := strconv.Atoi(os.Getenv("DB_PORT"))
if err != nil || port < 1 || port > 65535 {
	log.Fatalf("DB_PORT must be a port number, got %q", os.Getenv("DB_PORT"))
}
```

## 与 JSON Schema 的关系

写法与 JSON Schema 兼容，但它不是被当作 JSON Schema 去**执行**的：CLI 读的只有 `properties` 的键、`type`、`default`、`required`、`secret`、`description`；
其余关键字原样保存、给人读。某一项里写了这里没列出的关键字（拼错的 `defualt`，或照 JSON Schema 习惯写的 `format`、`examples`、`oneOf`……），
解析时会被丢掉——它们不会生效。作者在自己听得到、改得动的地方会收到警告（`lint`、本地源扫描、`publish`）；安装别人的组件时不因此失败，
因为使用方改不了别人的 Manifest。

## 编辑器

`schemas/component.schema.json` 描述了 `configSchema` 这一块的结构，接进编辑器之后，写 `configSchema` 时有补全、写错关键字（`defualt`）立刻标红。
见 [JSON Schema](05-json-schemas.md)。`lint` 也会报出 `configSchema` 某一项里拼错的键：

```text
警告：configSchema 里有配置项声明的键不会生效
```
