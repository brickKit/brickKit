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

除此之外，平台不注入任何东西。

## 依赖地址

**名字**由组件 ID 推出：`/` 和 `-` 换成 `_`，全大写，末尾加 `_ENDPOINT`。名字**不带版本**。

**值**带版本：`http://<版本化服务名>:<端口>`。服务名是组件 ID 的 `/` 和 `.` 换成 `-`，全小写，接上精确版本。
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

## 组件自己的配置

`configSchema` 的键就是环境变量名，原样注入。值怎么来，只有一条链（详见 [配置解析优先级](../01-three-layers/08-resolution-priority.md)）：

1. `config/<组件>.yaml`（兼容版本用 `config/<组件>@<版本>.yaml`）里写的值；
2. 写的是 `$var:NAME` 时，先查当前部署文件的 `vars:`，再查 `config/vars.yaml`，都没有就报错；
3. 没写，用 `configSchema` 的 `default`；
4. 都没有：可选项不注入；必填项让 `up` 拒绝启动。

值的类型：标量转成字符串（`1.10` 注入 `"1.10"`，不会变成 `1.1`）；列表和映射编码成一行 JSON。

### 不做隐式覆盖

进程环境里恰好有一个同名变量，不会覆盖 `config/` 里写的值。环境变量只在你**显式**写了 `${VAR}` 的地方参与。

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

配置值里可能有三种引用：`$var:NAME`（公共变量）、`${VAR}`（进程环境或 `.env`）、`file://路径`（文件内容）。
`$var:` 总是在 CLI 装载项目时就换成公共变量的值。另外两种，何时求值取决于值落到哪里：

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
