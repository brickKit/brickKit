# 环境变量注入契约

组件从平台拿到的一切都是环境变量。这一篇列出平台可能注入的每一个变量、它的名字怎么来、值什么时候求出来。
组件代码里唯一需要知道的平台约定，就在这一页上。

## 平台可能注入的全部变量

| 变量 | 给谁 | 值 |
| --- | --- | --- |
| `COMPONENT_ID` | 每个组件 | 组件 ID，如 `demo/hello` |
| `COMPONENT_VERSION` | 每个组件 | 精确版本，如 `1.0.0` |
| `<依赖>_ENDPOINT` | 每个这次在跑的依赖，一个 | 依赖主端口的地址，如 `http://demo-hello-1-0-0:8080` |
| `<依赖>_<端口名>_ENDPOINT` | 依赖声明的每个额外端口 | 如 `PEOPLE_BASIC_GRPC_ENDPOINT=http://people-basic-1-0-0:9090` |
| 组件自己的配置 | 每个组件 | 键就是 `configSchema` 里的键，值由 `config/` 解析而来 |
| `BRICKKIT_SERVED_MEMBERS` | 外壳 | 这次承载的成员服务名，逗号分隔 |
| `BRICKKIT_SERVED_MEMBERS_CONFIG` | 外壳 | 每个成员的配置与端口，JSON 数组（见 [JSON 配置注入](../04-shell/02-json-injection.md)） |
| `PORT` | `mode: local` 的组件 | 平台为这个本机进程选定的端口 |

除此之外，平台不往容器里注入任何东西。`mode: local` 的进程拿到的是同样这些变量，只是垫在启动 `up` 的那个终端的环境之上——去掉平台管的每一个名字，再加几个语言辅助变量，
见 [`mode: local` 进程继承什么](#mode-local-进程继承什么)。

## 依赖地址

**名字**由组件 ID 推出：`/` 和 `-` 换成 `_`，全大写，末尾加 `_ENDPOINT`。名字**不带版本**。
额外端口的变量在中间插进端口名，规则相同：`-` 换成 `_`，全大写（端口 `admin-api` → `PEOPLE_BASIC_ADMIN_API_ENDPOINT`），所以每个名字在 shell 里都能用 `$NAME` 读到。

**值**带版本：`http://<版本化服务名>:<端口>`。服务名是组件 ID 的 `/` 和 `.` 换成 `-`，全小写，接上精确版本——版本里的点也换成 `-`（`demo/hello@1.0.0` → `demo-hello-1-0-0`）。
这个字符串在 Docker 和 Kubernetes 上一模一样。

| 依赖 | 变量 | 值 |
| --- | --- | --- |
| `demo/hello@1.0.0` | `DEMO_HELLO_ENDPOINT` | `http://demo-hello-1-0-0:8080` |
| `people/basic@2.1.0`（额外端口 `grpc: 9090`） | `PEOPLE_BASIC_GRPC_ENDPOINT` | `http://people-basic-2-1-0:9090` |

名字与组件 ID 双向可推：看到 `PEOPLE_BASIC_ENDPOINT` 就知道它指向 `people/basic`。所以同一个组件的 `dependencies` 里一个 ID 只能出现一次。

几种特殊情况下，地址仍然是这个格式，只是指向别处：

| 依赖这次 | 调用方拿到的地址指向 |
| --- | --- |
| 被外壳承载 | 外壳：`http://<外壳服务名>:<成员自己的端口>`（外壳在那个端口上替成员监听） |
| 以本机进程运行（`mode: debug` / `mode: local`） | 端口换成它的本机端口；容器里的服务名解析到宿主机 |

**弱依赖没在跑时，变量根本不存在**——不是空字符串。组件读它必须能处理"没有这个变量"：

```python
bus = os.environ.get("DEMO_BUS_ENDPOINT")   # 没在跑时为 None
if bus is None:
    ...  # 降级
```

空字符串会造成一类极难查的 bug：`f"{ENDPOINT}/healthz"` 变成 `/healthz`，请求打到组件**自己**身上，拿到 200，你以为依赖是好的。

组件没有声明依赖、而由项目在配置里用 `$endpoint:<组件 ID>` 填的地址（槽位家族的成员就是这样被引用的），值按同一条规则算：
同样带版本号、同样跟着外壳与本机进程改写，变量名则是组件自己声明的那个配置键，不另外注入 `*_ENDPOINT`。
见 [另一个组件的地址](../01-three-layers/06-vars-and-var-ref.md)。

## 组件自己的配置

`configSchema` 的键就是环境变量名，原样注入。值怎么来，只有一条链（详见 [配置解析优先级](../01-three-layers/08-resolution-priority.md)）：

1. `config/<组件>.yaml`（兼容版本用 `config/<组件>@<版本>.yaml`）里写的值；
2. 写的是 `$var:NAME` 时，先查当前部署文件的 `vars:`，再查 `config/vars.yaml`，都没有就报错；
3. 没写，用 `configSchema` 的 `default`；
4. 都没有：可选项不注入；必填项让 `up` 拒绝启动。

声明了 `mount: file` 的密钥是例外：注入的是**文件的路径**（`/run/brickkit/secrets/<版本化服务名>/<键>`；本机进程拿到的是
宿主机上的绝对路径），值在那个文件里，逐字节。见 [以文件交付](../01-three-layers/07-sensitive-values.md#以文件交付mount-file)。

值的类型：标量按你写的原文注入（`1.10` 注入的就是 `1.10`，不会变成 `1.1`），不管它写在 `config/` 里还是 `configSchema` 的 `default` 里；列表和映射编码成一行 JSON。

### 不做隐式覆盖

进程环境里恰好有一个同名变量，不会覆盖 `config/` 里写的值，也不会替一个没有值的配置项补上值。环境变量只在你**显式**写了 `${VAR}` 的地方参与。
`mode: local` 的进程也一样，尽管它别的方面继承终端的环境（见[下文](#mode-local-进程继承什么)）。

## 保留的名字

平台自己要注入的名字，组件的配置项不能用：

| 规则 | 名字 |
| --- | --- |
| 完全相同 | `COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG` |
| 后缀 | `*_ENDPOINT` |

撞上了怎么办，取决于在哪一步发现：

| 什么时候 | 怎么处理 |
| --- | --- |
| `up` / `lint` | 警告，这一项被忽略，平台注入的值优先 |
| 发布到组件市场 | 拒收 |

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

`lint` 把 `configSchema` 里声明的每一项都查一遍；`up` 只在这一项真的会被注入时才碰到它。

## 值在什么时候求出来

配置值里可能有四种引用：`$var:NAME`（公共变量）、`${VAR}`（进程环境或 `.env`）、`file://路径`（文件内容）、
`{ existingSecret: 名字, key: 键 }`（集群里已有的 Kubernetes Secret）。
`$var:` 总是在 CLI 装载项目时就换成公共变量的值。`existingSecret` 的值 CLI 从头到尾不读：Kubernetes 上由 Deployment 经 `secretKeyRef` 引用那个 Secret；
其余地方这一项都不注入（Docker 上会警告；外壳的 JSON 需要真值，所以直接拒绝；见 [敏感值](../01-three-layers/07-sensitive-values.md)）。
`${VAR}` 和 `file://` 何时求值，取决于值落到哪里：

| 值落到哪里 | `${VAR}` | `file://` |
| --- | --- | --- |
| Docker / Podman 的普通值 | 原样写进 `compose.yaml`，`docker compose` 启动时展开；CLI 生成时核对它有定义，没有就停下 | CLI 读出内容，按密钥写进 0600 的 env 文件 |
| Docker / Podman 的密钥（`secret: true`） | 原样写进 0600 的 env 文件，`docker compose` 启动时展开；同样先核对有定义 | 同上 |
| Kubernetes | CLI 生成清单时展开（`kubectl` 不做替换）；取不到就停下 | CLI 读出内容 |
| 外壳的 `BRICKKIT_SERVED_MEMBERS_CONFIG` | CLI 生成时展开并做 JSON 编码；取不到就停下 | CLI 读出内容并做 JSON 编码 |
| `mode: local` 进程的环境 | CLI 启动进程前展开；取不到就拒绝启动 | CLI 读出内容 |
| `mode: debug` 的 `local-debug.*.env` | CLI 生成时展开；取不到就留着占位符，并在输出里点名 | CLI 读出内容 |

外壳的 JSON 必须提前求值：`docker compose` 的替换是不懂 JSON 的纯文本替换，一个带引号或换行的值替换进去，JSON 就坏了
（见 [特殊字符](../04-shell/06-special-characters.md)）。Kubernetes 必须提前求值：`kubectl` 根本不做替换。

密钥在哪一种情况下都不会明文出现在 `compose.yaml` 或 Deployment 里：Docker 下它在 0600 的 env 文件里，Kubernetes 下它在生成的 Secret 里，
Deployment 用 `secretKeyRef` 引用（见 [敏感值](../01-three-layers/07-sensitive-values.md)）。

## 迁移容器与本机进程拿到的是同一份

组件的迁移容器拿到的环境与主服务一模一样（同一份内联变量、同一个 env 文件）。`mode: debug` 的组件没有容器，
平台把它本该拿到的变量写进 `.brickkit/generated/local-debug.<服务名>.env`，供你在 IDE 里加载；依赖地址在这份文件里是本机可达的 `http://localhost:<端口>`。

## `mode: local` 进程继承什么

容器拿到的是本页列出的这些变量，加上镜像自己设的，你终端里的一个都不带进去。`mode: local` 的进程——`brickkit up` 在本机用组件源码启动的那种——不一样：它从启动 `up` 的那个终端的环境出发，
因为本机上的程序要有你的 `PATH`、`HOME`、工具链和代理设置才跑得起来。

平台管的名字不继承。进程启动前，这些名字先从终端的环境里拿掉，能到达进程的只有平台自己给的值：

| 从终端环境里拿掉 | 为的是 |
| --- | --- |
| `COMPONENT_ID`、`COMPONENT_VERSION`、`PORT`、`BRICKKIT_SERVED_MEMBERS`、`BRICKKIT_SERVED_MEMBERS_CONFIG`、所有以 `_ENDPOINT` 结尾的名字 | 平台这次不给某个变量时（弱依赖没在跑），进程里就真的没有这个变量，和容器里一模一样；shell 里遗留的 `export DEMO_BUS_ENDPOINT=…` 没法让一个停着的依赖看起来还在 |
| 组件自己 `configSchema` 里的键 | 配置项的值只从[上面那条链](#组件自己的配置)来；shell 里一个过时的 `export DB_HOST=…` 永远顶替不了它 |

在此之上，平台还按它在源码目录里认出的语言（或 `component.yaml` 的 `local:` 块里写的 `language`）补几个变量；手写了 `runCommand` 又没写 `language` 的，一个都没有：

| 语言 | 设置 | 为什么 |
| --- | --- | --- |
| `java`（Spring Boot） | `SERVER_PORT=<PORT 的值>` | Spring Boot 从 `SERVER_PORT` 取监听端口 |
| `java` | `JAVA_TOOL_OPTIONS=`、`JDK_JAVA_OPTIONS=`（清空） | shell 里遗留一个带 `suspend=y` 的调试代理，进程就会卡住，等一个没人会连的调试器 |
| `node` | `NODE_OPTIONS=`（清空） | 同理，防 shell 里遗留的 `--inspect-brk` |
| `python`（Django） | `PYTHONUNBUFFERED=1` | 进程的输出接的是管道不是终端，不加这个，Python 会把日志攒成大块才吐出来 |

`mode: debug` 的组件由你自己在 IDE 里启动，它继承什么由 IDE 决定；平台负责的那部分，就是上面说的 `local-debug.<服务名>.env` 文件。
