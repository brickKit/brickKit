# 本地调试工作流

你在改 `demo/hello`，想在 IDE 里打断点跑它，而项目里的其它组件照常在容器里跑、并且能调到你这个进程。这一篇从头走一遍。

背景：你的个人部署文件 `deploy.local.yaml` 是什么、为什么是"整份替换而不是合并"，见 [deploy.local.yaml](../01-three-layers/04-deploy-local-yaml.md)。

## 完整流程

### 1. 开启本地模式

```bash
brickkit local on
```

```text
✅ 本地模式已开启：命令现在读取 deploy.local.yaml
   deploy.local.yaml 从 deploy.yaml 复制而来——随意修改，它不会被提交
```

之后运行或检查部署的命令（`up`、`down`、`status`、`sync`、`lint`、`build`）都读 `deploy.local.yaml`，每次运行开头会提醒一句（`graph` 与 `deps` 仍读 `deploy.yaml`）：

```text
本地模式已开启：使用 deploy.local.yaml（brickkit local off 切回 deploy.yaml）
```

### 2. 把要调试的组件标成 `mode: debug`

```yaml
# deploy.local.yaml
components:
  - id: demo/hello
    mode: debug
    localPort: 8080
  - id: demo/caller
    expose: true
    exposePort: 18080
```

`mode: debug` 的意思是"这个组件我自己在本机启动"：平台不给它生成容器，但照样把它算作在运行，别的组件照样拿到它的地址。
`localPort` 是你的进程**实际监听**的端口。`demo/hello` 在代码里写死监听 8080，所以这里写 8080；两者对不上，调用方只会得到连接被拒绝。
进程还要监听**所有网卡**（`0.0.0.0`）：容器是经宿主机的网桥地址过来的，只听 `127.0.0.1` 的进程收不到（见 [本地调试问题](../10-troubleshooting/02-local-debug-issues.md#容器里的组件连不上你的进程)）。

`mode: debug` 只能写在 `deploy.local.yaml` 里——"我正在自己机器上调试它"是你个人的事实，不该出现在团队评审的文件里。

### 3. 启动其余的组件

```bash
brickkit up
```

```text
📋 组件状态计算：
   ✅ demo/hello@1.0.0   启动（mode: debug）
   ✅ demo/caller@1.0.0  启动（顶层）
```

```text
🔧 本地调试（mode: debug）：
   demo/hello@1.0.0
      不生成容器；请在 IDE 里启动它，监听端口 8080，而且要监听所有网卡（0.0.0.0）——只听 127.0.0.1 的进程，容器连不到
      环境变量：.brickkit/generated/local-debug.demo-hello-1-0-0.env
      VS Code：launch.json 里配 "envFile": "${workspaceFolder}/.brickkit/generated/local-debug.demo-hello-1-0-0.env"
```

```text
🐳 正在启动（docker）...
   demo-caller-1-0-0            running（healthy）
✅ 全部组件已启动（1 个）
```

`up` 为 `demo/hello` 生成了一份环境变量文件：它在容器里本该拿到的每个变量，一个不少。

```text
COMPONENT_ID=demo/hello
COMPONENT_VERSION=1.0.0
GREETING=来自本机进程
```

（`GREETING` 的值来自 `config/demo-hello.yaml`——调试时想换值，照常改配置文件、重新 `up`。）

### 4. 在 IDE 里启动它

VS Code 把上面那行 `envFile` 配进 `launch.json`；命令行里这样：

```bash
cd components/demo/hello
set -a && source ../../../.brickkit/generated/local-debug.demo-hello-1-0-0.env && set +a
go run .
```

组件源码从哪来：`brickkit add demo/hello@1.0.0 --repo` 把它克隆进 `components/`（见 [添加组件](02-add-and-component-install.md#克隆源码--repo--repo-all)）。

### 5. 验证容器里的组件连得到你

```bash
curl http://localhost:18080/api/v1/call
```

```json
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:8080","upstream":{"component":"demo/hello","greeting":"来自本机进程","message":"来自本机进程, I'm demo/hello@1.0.0","version":"1.0.0"},"version":"1.0.0"}
```

容器里的 `demo/caller` 调的还是 `http://demo-hello-1-0-0:8080`——地址没变，组件代码一行没改。平台在它的容器里把这个服务名解析到宿主机
（Docker 的 `extra_hosts: demo-hello-1-0-0:host-gateway`），并把端口换成你的 `localPort`。

`status` 把它单独列出来：

```text
🔧 本地调试（mode: debug，不由平台启动）
 ┌────────────┬───────┬────────────────────────────────┐
 │ 组件       │ 版本  │ 本地地址                       │
 ├────────────┼───────┼────────────────────────────────┤
 │ demo/hello │ 1.0.0 │ localhost:8080（IDE 调试模式） │
 └────────────┴───────┴────────────────────────────────┘
```

几个要知道的限制：

- **只在 Docker / Podman 下有效。** Kubernetes 集群里的 Pod 到不了你的笔记本；部署目标是 `k8s` 时 `mode: debug` 被拒绝。
- **调试的组件不跑数据库迁移。** 容器里的迁移步骤跟着容器一起没了；组件有迁移时，第一次要自己手动跑一次。
- **配置里写死的字符串不会被改写。** 如果你在 `config/` 里写了一个指向容器网络的地址（比如 `http://host.docker.internal:8000`），
  它原样到达你的进程；在本机跑时要自己改成 `localhost`。平台只改写它自己算出来的依赖地址。

## 团队改了文件之后：严格一致性校验

你开着本地模式，队友加了一个组件并推送。你拉下来之后：

```bash
git pull
brickkit up
```

```text
❌ 错误：deploy.local.yaml 已过期，与 brickkit.yaml 的组件对不上
   文件：deploy.local.yaml
   缺少条目：demo/bus
   原因：你开启了本地模式，而 brickkit.yaml 变了，你的 deploy.local.yaml 没有同步
   建议：
   1. 方案 A（推荐）：brickkit local refresh 按 deploy.yaml 重新生成 deploy.local.yaml，旧文件备份为 deploy.local.yaml.bak
   2. 方案 B：在 deploy.local.yaml 里手动增删下面列出的条目
   3. 方案 C：brickkit local off 切回团队的 deploy.yaml
```

为什么是报错、而不是悄悄把新组件补上：本地文件是整份替换 `deploy.yaml` 的，CLI 不知道新组件在你这里该怎么跑——
按团队的写法？还是你另有打算？猜错了就是在你不知情时跑了一个你没想跑的东西。所以它停下来，把三条出路都列出来。

## `brickkit local refresh`

```bash
brickkit local refresh
```

```text
✅ 已从 deploy.yaml 生成最新的 deploy.local.yaml。旧文件已备份至 deploy.local.yaml.bak。
ℹ️ 旧文件中有 2 处本地修改，请把仍然需要的手动合并到新的 deploy.local.yaml 中：
   - [demo/hello] mode: debug（当前未设置）
   - [demo/hello] localPort: 8080（当前未设置）
```

`refresh` 做三件事：

1. 把旧文件原样备份成 `deploy.local.yaml.bak`（上一份备份会被覆盖）；
2. 从 `deploy.yaml` 重新复制出一份完整的新文件——**不合并**，新文件就是团队文件的副本；
3. 拿旧文件和它当初复制自的那一份对比，把你以前的每一处、与团队文件现在不同的本地修改列出来：`mode`、`localPort`、`vars` 的值、换过的 `target`、挪过位置的条目、删掉的字段。

你只需要照着这张清单，把还想要的几行抄回新文件，不用把两份 YAML 从头到尾比一遍。抄回去之后：

```yaml
components:
  - id: demo/hello
    mode: debug
    localPort: 8080
  - id: demo/caller
    expose: true
    exposePort: 18080
  - id: demo/bus
```

为什么不自动合并：合并规则再聪明，也会有它猜错的时候，而猜错的结果就是"文件里写的不是实际跑的"。
整份替换加一张清单，结果永远一眼可见。

## 外壳成员的调试

外壳承载的成员也可以单独调试：在 `deploy.local.yaml` 里给外壳条目 `members` 下的那个成员写 `mode: debug` 和 `localPort`。
这次它在宿主机上由你启动，外壳**不再承载它**（它不出现在外壳收到的成员列表里），其余成员照常由外壳承载，别的组件拿到的它的地址指向你的进程。
外壳怎么工作见 [外壳机制](../04-shell/README.md)。

## 关闭本地模式

```bash
brickkit local off
```

```text
✅ 本地模式已关闭：命令现在读取 deploy.yaml
   deploy.local.yaml 保留着；brickkit local on 会接着用它
```

文件留着，下次 `local on` 原样接着用。只想让一次运行忽略它，不必关：`brickkit up --no-local`。
