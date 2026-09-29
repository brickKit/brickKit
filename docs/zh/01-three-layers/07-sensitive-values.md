# 敏感值处理

`config/` 要进 Git，所以密钥（口令、令牌、私钥）不能直接写在里面。写在里面的密钥会跟着进版本库，而且没法从历史里删掉。

## 三种引用方式

| 写法 | 真值在哪 | 适合放什么 |
| --- | --- | --- |
| `${API_TOKEN}` | 进程环境变量，其次项目根目录的 `.env`（`.env` 不进 Git） | 口令、令牌 |
| `file://.secrets/tls.key` | 一个文件（路径相对项目根；`.secrets/` 默认在 `.gitignore` 里） | 证书、私钥这类多行内容 |
| `{ existingSecret: 名字, key: 键 }` | 集群里已经建好的 K8s Secret（运维、Vault、External Secrets 等负责它） | 只在 K8s 上、密钥由集群里别的系统管理时 |

`$var:` 也能间接用：`config/vars.yaml` 里写 `PG_PASSWORD: ${PG_PASSWORD}`，组件配置里写 `$var:PG_PASSWORD`。

```yaml
# config/demo-hello.yaml
GREETING: 你好
API_TOKEN: ${API_TOKEN}
TLS_KEY: file://.secrets/tls.key
```

## 哪些值算密钥

组件在 `configSchema` 里给某一项写了 `secret: true`，这一项就是密钥——判断权在组件作者手里，平台不按名字猜。
密钥在生成部署文件时走单独的通道（见下文），绝不明文写进 `compose.yaml` 或 Deployment。

`up` 还会替你看一眼已提交的文件：声明了 `secret: true`、或名字像密钥的项，在 `config/` 里写成了明文，就提醒你：

```text
⚠️ config/ 下的文件可能写了明文密钥
   配置项：demo/hello@1.0.0 → API_TOKEN
   为什么要紧：config/*.yaml 与 config/vars.yaml 是要提交进 Git 的，写在里面的密钥会跟着进版本库，而且没法从历史里删掉
```

它从不打印值本身。你本地的 `deploy.local.yaml` 不进 Git，在它的 `vars:` 里写本机口令不会被提醒；但它遮住的同名变量如果在
`deploy.yaml` 或 `config/vars.yaml` 里是明文，照样提醒——泄漏的是那两份文件。

## 不做隐式的环境变量覆盖

`config/` 里写了 `DB_HOST: pg.internal`，即使你的环境里有 `DB_HOST=localhost`，组件拿到的也是 `pg.internal`。
环境变量只在你**显式**写了 `${…}` 的地方参与。否则同一份配置在不同的人、不同的 CI 上会悄悄变成不同的值——
所见即所得就不成立了。

## Docker / Podman：值落在哪

| 值 | 去处 |
| --- | --- |
| 普通值 | 写进 `compose.yaml` 的 `environment` |
| 普通值里的 `${VAR}` | 原样写进 `compose.yaml`，`docker compose` 启动时从进程环境或 `.env` 展开 |
| `secret: true` 的值、`file://` 的内容 | 写进 `.brickkit/generated/env/<服务名>.env`，文件权限 0600，由 `env_file` 引用；`${VAR}` 在这里同样留给 compose 启动时展开，`file://` 的内容由 CLI 读出后写入 |
| `existingSecret` | Docker 没有这个概念：这一项不注入，并提醒你 |

`${VAR}` 虽然留给 compose 去展开，CLI 生成时仍会用同样的顺序（进程环境，其次 `.env`）核对它**有没有定义**。
没有的话直接报错，而不是交给 compose：compose 会把它换成空字符串、只在自己的输出里警告一句，组件就带着一个残缺的值跑起来。

```text
❌ 错误：config/ 或部署文件里引用的环境变量没有定义
   缺少的变量：PG_HOST
   原因：docker compose 启动时会把它们换成空字符串，只在它自己的输出里警告一句：组件拿到的是残缺的值，却不会有任何报错
   建议：
   1. 在项目根目录的 .env 里补上这些变量，或在当前 shell 里 export
   2. 也可以写默认值：${POSTGRES_PASSWORD:-dev}
```

K8s 下同一份配置也在生成时因为同一个原因失败——两个目标对"未定义的引用"说法一致。有意允许为空的，写成 `${VAR:-}`。

上面那个例子生成出来是这样的：

```yaml
    env_file:
      - path: .brickkit/generated/env/demo-hello-1-0-0.env
    environment:
      - COMPONENT_ID=demo/hello
      - COMPONENT_VERSION=1.0.0
      - GREETING=你好
```

```bash
stat -c '%A  %n' .brickkit/generated/env/*.env
```

```text
-rw-------  .brickkit/generated/env/demo-hello-1-0-0.env
```

```text
API_TOKEN="${API_TOKEN}"
TLS_KEY="-----BEGIN PRIVATE KEY-----\nMIIBVQ...\n-----END PRIVATE KEY-----\n"
```

多行的 PEM、`$`、引号、反斜杠都经过转义，逐字节到达容器。

## Kubernetes：值落在哪

`kubectl` 不做变量替换，所以 K8s 下 CLI 在**生成清单时**就把 `${VAR}` 求出来（进程环境优先，其次 `.env`）。
缺一个就停下，不会生成一份带着空值的清单：

```text
❌ 错误：config/ 或部署文件里引用的环境变量没有定义
   缺少的变量：API_TOKEN
   原因：生成 K8s 清单时必须求值——kubectl 不做变量替换
   建议：
   1. 在项目根目录的 .env 里补上这些变量，或在当前 shell 里 export
   2. 也可以写默认值：${POSTGRES_PASSWORD:-dev}
```

| 值 | 去处 |
| --- | --- |
| 普通值（含展开后的 `${VAR}`） | Deployment 的 `env.value` |
| `secret: true` 的值、`file://` 的内容 | 平台生成的 Secret（`.brickkit/generated/k8s/secrets/`，文件权限 0600），Deployment 用 `secretKeyRef` 引用 |
| `existingSecret` | 直接 `secretKeyRef` 到那个已有的 Secret；平台不读、不写它的值 |

```yaml
            - name: API_TOKEN
              valueFrom:
                secretKeyRef:
                  key: API_TOKEN
                  name: demo-hello-1-0-0-config-secret
```

`existingSecret` 只能用在声明了 `secret: true` 的项上；写在普通项上会被警告并忽略——它的值本来就会被当成明文处理。

## 与外壳的交互

外壳把成员的配置打包成一个 JSON 注入（`BRICKKIT_SERVED_MEMBERS_CONFIG`）。这份 JSON 由 CLI 提前求好值，
所以成员的 `${VAR}`、`file://` 在生成时就被展开并 JSON 编码——多行的 PEM 在 JSON 里是一个带 `\n` 的字符串，
能原样解析回来；含有 JSON 无法表示的控制字符时大声失败。这份 JSON 同样走密钥通道（Docker 的 0600 env 文件、K8s 的 Secret），
见 [特殊字符处理](../04-shell/06-special-characters.md)。

## 离线检查

`brickkit lint --strict` 会把进程环境和 `.env` 里都找不到的 `${VAR}`、文件不存在的 `file://` 报成警告，
而 `--strict` 下警告即失败——适合放进 CI。
