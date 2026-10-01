# 公共变量与 $var: 显式引用

好几个组件连同一个数据库、同一个消息队列时，地址不该在每个组件的配置里各抄一遍——改的时候总会漏一处。
BrickKit 的办法是：值写一次在 `config/vars.yaml`，每个要用它的组件**显式地**写 `$var:` 引用它。

## config/vars.yaml

一份扁平的键值文件，变量名完全由你定（字母、数字、下划线，不以数字开头）：

```yaml
# config/vars.yaml
PG_HOST: pg.internal
PG_PORT: 5432
PG_PASSWORD: ${PG_PASSWORD}     # 值可以是 ${VAR}：真值在 .env 或进程环境里
TLS_CA: file://.secrets/ca.pem   # 也可以是 file://
```

公共变量不能再引用别的公共变量（`$var:` 不能链式引用）：

```text
❌ 错误：vars.yaml 校验失败
   文件：config/vars.yaml
   B：公共变量不能再用 $var: 引用别的公共变量——这里只允许 ${ENV_VAR} 与 file://
```

一层就够：链式引用让"这个值到底是什么"要追好几步才知道，而那正是公共变量要解决的问题。

## 在组件配置里引用

```yaml
# config/people-basic.yaml
DATABASE_HOST: $var:PG_HOST
DATABASE_PORT: $var:PG_PORT
DATABASE_PASSWORD: $var:PG_PASSWORD
DATABASE_NAME: people            # 只属于这个组件的值，直接写
```

`$var:NAME` 必须是**整个值**：`$var:PG_HOST:5432` 不是合法写法。整串值要共享时，把它单独写成一个公共变量
（在 `config/vars.yaml` 里写 `PG_URL: jdbc:postgresql://pg.internal:5432/people`），再引用 `$var:PG_URL`。
`${VAR}` 模板（`jdbc:postgresql://${PG_HOST}:5432/people`）可以嵌在字符串里，但注意 `${…}` 查的是进程环境变量和
`.env`，不是公共变量，所以 `PG_HOST` 得定义在那里。

## 三种引用写法

| 写法 | 从哪里取值 | 什么时候求值 |
| --- | --- | --- |
| `$var:NAME` | 部署文件的 `vars:`，其次 `config/vars.yaml` | CLI 装载项目时 |
| `${NAME}`、`${NAME:-默认值}` | 进程环境变量，其次项目根目录的 `.env` | Docker 下由 `docker compose` 启动时展开（CLI 生成时先核对它有定义）；K8s 下由 CLI 生成清单时展开 |
| `file://路径` | 文件内容（路径相对项目根） | CLI 生成部署文件时读取 |

`${NAME:-默认值}` 在变量取不到时用默认值，所以永远不算未定义。默认值是一段纯文本，不能含 `$`、`{`、`}`。
写得像引用、却不合语法的（`${A:-${B}}`、`${1X}`、缺了 `}`），装载项目时就报错，而不是当成一段文字交给容器。

后两种主要用来放密钥，见 [敏感值](07-sensitive-values.md)。

## 找不到就大声失败

引用了一个哪里都没定义的变量，装载项目时就停下：

```text
❌ 错误：配置文件引用了哪里都没有定义的公共变量
   未定义的引用：config/demo-hello.yaml: GREETING → $var:NOPE
   建议：在 config/vars.yaml（或部署文件的 vars:）里定义这个变量
```

`lint` 也会报同样的问题。

## 部署文件的 `vars:` 按环境覆盖

`deploy.yaml`、`deploy.local.yaml`、`deploy.prod.yaml` 都可以写 `vars:`，同名时它优先于 `config/vars.yaml`：

```yaml
# deploy.prod.yaml
target: k8s
vars:
  PG_HOST: pg.prod.internal
components:
  - id: people/basic
```

用 `-f deploy.prod.yaml` 部署时，所有 `$var:PG_HOST` 都得到 `pg.prod.internal`；平常得到 `config/vars.yaml` 里的值。
在自己机器上连本地数据库，就在 `deploy.local.yaml` 的 `vars:` 里写 `PG_HOST: localhost`。

## 为什么不用隐式兜底

另一种常见设计是"组件配置里没写的键，自动去全局文件里找同名的"。BrickKit 刻意不这样做：

- **所见即所得。** 打开 `config/people-basic.yaml`，每个值从哪来一眼可知；隐式兜底要你把几个文件在脑子里合起来。
- **自包含。** 一份组件配置说清了这个组件的全部依赖值，删掉一个公共变量时，所有引用它的地方都会大声报错，而不是
  悄悄换成别的值。
- **对 AI 友好。** AI 读一份配置就知道要去哪里找值，不用猜一套查找规则。

代价是要多写几个 `$var:`。`add` 生成骨架时，发现 `config/vars.yaml` 里有同名变量会问你要不要直接引用，省掉大部分手写。

## 解析顺序

一个配置项最终取哪个值，完整规则见 [解析优先级](08-resolution-priority.md)。和 `$var:` 相关的只有一条：
`$var:NAME` 先查当前部署文件的 `vars:`，再查 `config/vars.yaml`。
