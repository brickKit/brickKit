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
IAM_URL: $endpoint:infra/iam     # 也可以是另一个组件的地址（见下文）
```

公共变量不能再引用别的公共变量（`$var:` 不能链式引用）：

```text
❌ 错误：vars.yaml 校验失败
   文件：config/vars.yaml
   B：公共变量不能再用 $var: 引用别的公共变量——这里只允许 ${ENV_VAR}、file:// 与 $endpoint:
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

## 四种引用写法

| 写法 | 从哪里取值 | 什么时候求值 |
| --- | --- | --- |
| `$var:NAME` | 部署文件的 `vars:`，其次 `config/vars.yaml` | CLI 装载项目时 |
| `${NAME}`、`${NAME:-默认值}` | 进程环境变量，其次项目根目录的 `.env` | Docker 下由 `docker compose` 启动时展开（CLI 生成时先核对它有定义）；K8s 下由 CLI 生成清单时展开 |
| `file://路径` | 文件内容（路径相对项目根） | CLI 生成部署文件时读取 |
| `$endpoint:<scope>/<name>` | 项目里另一个组件的地址（见下一节） | CLI 生成部署文件时算 |

`${NAME:-默认值}` 在变量取不到时用默认值，所以永远不算未定义。默认值是一段纯文本，不能含 `$`、`{`、`}`。
写得像引用、却不合语法的（`${A:-${B}}`、`${1X}`、缺了 `}`），装载项目时就报错，而不是当成一段文字交给容器。

`${NAME}` 与 `file://` 主要用来放密钥，见 [敏感值](07-sensitive-values.md)。

## 另一个组件的地址：`$endpoint:`

一个组件依赖另一个组件时，它的地址由平台算好、注入成 `*_ENDPOINT`。可有时组件**不能**对某个具体组件声明依赖：
它要的是"一个身份服务的地址"，至于装的是哪个实现，由项目决定（见[槽位家族](../09-patterns/01-component-design.md)）。
组件这时在 `configSchema` 里声明一个地址配置项，项目来填。`$endpoint:` 让项目按组件 ID 填，不必手写地址：

```yaml
# config/vars.yaml —— 槽位由谁填，只写在这一个地方
IAM_URL: $endpoint:infra/iam-casdoor
IAM_JWKS_URL: $endpoint:infra/iam-casdoor/.well-known/jwks.json
AUTHZ_URL: $endpoint:infra/authz
```

```yaml
# config/erp-sales.yaml
IAM_URL: $var:IAM_URL
AUTHZ_URL: $var:AUTHZ_URL
```

| 写法 | 得到 |
| --- | --- |
| `$endpoint:infra/authz` | `http://infra-authz-2-0-1:8223`——项目里它的默认版本、主端口 |
| `$endpoint:infra/authz@2.0.0` | 指定版本（项目里要有这个版本） |
| `$endpoint:infra/authz:grpc` | 名为 `grpc` 的额外端口 |
| `$endpoint:infra/iam-casdoor/.well-known/jwks.json` | 地址后面接上路径。组件 ID 固定两段，第二段之后的第一个 `/` 就是路径的开头 |

值与依赖地址 `*_ENDPOINT` 是同一条规则算出来的，所以手写地址会出的问题它都没有：

- **跟着版本走。** 地址里是带版本号的服务名；`brickkit upgrade` 之后不用改任何地址。
- **跟着部署方式走。** 目标被外壳承载时指向外壳；引用方在本机跑（`mode: local` / `debug`、焦点运行）时换成本机能连上的地址；
  引用自己时就是自己的地址（比如把自己的回调地址交给外部系统）。同一个外壳里的各成员拿到的值完全相同。
- **平台知道这条边。** 被引用的组件跟着引用它的组件一起跑（和弱依赖一样"跟着上层走"），焦点运行会带上它，
  K8s 的 `networkPolicy` 放行这条连接，`graph` / `deps` 画得出来。

它和依赖有两处不同，都是有意的：

- **不进启动顺序，可以成环。** 引用不生成 `depends_on`：两个组件互相引用（权限服务要身份服务的公钥，身份服务登录时又要问权限服务）
  是正常的形状，双方都应当按"对方暂时不在就退避重试"来写。需要对方先起来的，应当在 `component.yaml` 里声明依赖。
- **不注入 `*_ENDPOINT`。** 组件只拿到它自己声明的那个配置项。

被引用的组件这次不跑（写了 `mode: disable`，或者引用它的都没跑）时，这个值按"没给"处理：可选项不注入，组件自己降级；
必填项启动前报错，说清是谁没跑。指向项目里没有的组件、或者组件没有的端口名，`up` 与 `lint` 都报错。

它不是依赖别名：变量名仍由组件自己定，指向谁写在 `config/` 或 `vars.yaml` 里，打开文件就看得到。

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
