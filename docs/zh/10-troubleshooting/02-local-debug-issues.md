# 本地调试问题

本地调试是把一个组件用 `mode: debug` 拉到你的机器上、在 IDE 里跑，其余组件照常在容器里跑。完整流程见 [本地调试工作流](../02-project-guide/03-local-debug-workflow.md)。

## `deploy.local.yaml` 过期

**症状**

`git pull` 之后，`up`（以及别的命令）拒绝执行：

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

**原因**

队友加了（或删了）一个组件。`deploy.local.yaml` 是整份替换 `deploy.yaml` 的，每个组件版本都必须恰好有一个条目；
CLI 不知道新组件在你这里该怎么跑，所以停下来，而不是替你猜。

**解决**

多数情况用方案 A：

```bash
brickkit local refresh
```

```text
✅ 已从 deploy.yaml 生成最新的 deploy.local.yaml。旧文件已备份至 deploy.local.yaml.bak。
ℹ️ 旧文件中有 2 处本地修改，请把仍然需要的手动合并到新的 deploy.local.yaml 中：
   - [demo/hello] mode: debug（当前未设置）
   - [demo/hello] localPort: 8080（当前未设置）
```

照着清单把还要的几行抄回新文件。只差一两个条目时，方案 B 手动补更快。

## `mode: debug` 不生效

**症状一：写在 `deploy.yaml` 里，被拒绝。**

```text
❌ 错误：deploy.yaml 校验失败
   文件：deploy.yaml
   components[0].mode：mode: debug 只能写在 deploy.local.yaml 里：它记录的是"我此刻在本机调试这个组件"，不是团队决策。先 brickkit local on，再到那里设置
```

**原因**：`mode: debug` 是你个人此刻的事实，只能写在个人文件里。**解决**：`brickkit local on`，在 `deploy.local.yaml` 里写。

**症状二：写在 `deploy.local.yaml` 里了，组件却还是在容器里跑。**

**原因**：本地模式没开，命令读的是 `deploy.yaml`，根本没看 `deploy.local.yaml`。或者这次用了 `-f` / `--no-local`，本地模式被跳过。

**解决**：确认读的是哪份文件：

```bash
brickkit local status
```

```text
本地模式：关闭
deploy.local.yaml：存在（本地模式关闭时不读取）
```

`brickkit local on` 打开它。本地模式开着时，每条命令的第一行会写 `本地模式已开启：使用 deploy.local.yaml`，没有这一行就是没读它。

**症状三：部署目标是 `k8s`，被拒绝。** 集群里的 Pod 连不到你的笔记本，`mode: debug` 与 `mode: local` 只在 `docker` / `podman` 下有效。
个人文件里可以把 `target` 改成 `docker`，在本机调试。

## 容器里的组件连不上你的进程

**症状**

调用方（容器里）请求你的本机进程，得到 connection refused、超时，或者 502/503。

**原因与解决**，按出现频率：

1. **端口对不上。** `localPort` 必须是你的进程**实际监听**的端口。进程写死监听 8080，`localPort` 就得是 8080。
   平台把调用方拿到的地址端口换成 `localPort`，两者不一致，请求就落到一个没人听的端口上。
2. **进程只监听了 `127.0.0.1`。** 容器是经宿主机的网桥地址（`host-gateway`）过来的，不是从回环地址过来的，只听 `127.0.0.1` 的进程收不到。
   调用方看到的是：

   ```text
   wget: can't connect to remote host (172.17.0.1): Connection refused
   ```

   而你在本机 `curl localhost:<端口>` 却是通的——这正是它难查的地方。让进程监听所有网卡（`0.0.0.0`）：Go 写 `:8080` 而不是 `127.0.0.1:8080`；
   `uvicorn`、Flask 开发服务器默认只听 `127.0.0.1`，要加 `--host 0.0.0.0`；Node 的 `listen(port)` 不写主机名时默认就是所有网卡。
3. **进程没起来，或者起在了另一个终端、另一台机器。** 在你的机器上先 `curl http://localhost:<localPort>/healthz`，能通再看容器那一侧。

平台做的是：在其它容器里把你这个组件的版本化服务名解析到宿主机（`extra_hosts: <服务名>:host-gateway`），并把端口换成 `localPort`。
调用方拿到的地址字符串完全不变——所以排查时先问"这个名字解析到了我的机器吗、那个端口上有人在听吗"，而不是泛泛地查网络。

## 本机进程报 `relation does not exist`

**症状**

调试的组件一启动就报表不存在（PostgreSQL 的 `relation "…" does not exist`，或其它数据库的同类错误）。`up` 的输出里有一条提示：

```text
⚠️ 提示：mode: debug 组件的数据库迁移不会自动执行
```

**原因**

迁移是在组件的迁移容器里跑的；组件以本机进程运行时没有容器，迁移也一并跳过。

**解决**

自己跑一次迁移：加载 `local-debug.<服务名>.env` 里的变量，在本机执行组件的迁移命令（`component.yaml` 的 `migration.command`）。

```bash
set -a && source .brickkit/generated/local-debug.demo-caller-1-0-0.env && set +a
go run . migrate
```

## 本机进程连不上配置里写的某个地址

**症状**

本机进程连依赖都正常，唯独连某个"外部"服务失败，而那个地址是你在 `config/` 里手写的，比如 `http://host.docker.internal:8000`。

**原因**

平台只改写它自己算出来的依赖地址（`*_ENDPOINT`）。你手写在配置里的字符串，平台不解析、不改写，原样交给进程——而那个地址假设的是容器网络，
在宿主机上解析不了。

**解决**

在 `deploy.local.yaml` 的 `vars:` 里给它一个本机能用的值（配置里写 `$var:NAME` 引用它），调试时就是 `localhost`，团队的部署文件不受影响：

```yaml
# config/demo-caller.yaml
REPORT_URL: $var:REPORT_URL
```

```yaml
# deploy.local.yaml
vars:
  REPORT_URL: http://localhost:8000
```

## 焦点运行起不来

**现象**

在组件目录里 `up`，或者 `up --focus`，停在下面其中一种：

```text
❌ 错误：焦点 demo/lb 不是这个项目的组件
   建议：
   1. brickkit up --focus <id> 换一个焦点；brickkit up --all 运行全部组件
   2. 你是不是想写：demo/lib？
```

```text
❌ 错误：焦点 demo/lib 的条目写着 mode: disable
   文件：deploy.local.yaml
   建议：去掉 demo/lib 的 mode: disable，或换一个焦点
```

**原因与解决**

- **不是这个项目的组件**：`deploy.local.yaml` 里的 `focus:` 写的组件不在 `brickkit.yaml` 里——拼错了，或者团队把它删了。
  用 `--focus` 换一个，或者 `brickkit up --all` 去掉焦点。
- **`mode: disable`**：同时说了"跑它"和"永远别跑它"。去掉条目上的 `mode: disable`，或者换一个焦点。
- **`focus` 字段上的"校验失败"**：焦点写在了 `deploy.yaml` 里（它属于你的个人文件），或者你的个人文件是 `target: k8s`——
  集群够不着你机器上的进程。
- **`从本地仓库运行的代码与这次运行的版本对不上`**：焦点从源码跑，而源码要么不在（`brickkit add <id> --repo`），要么是和
  `brickkit.yaml` 不同的版本（运行报错建议的那条 `upgrade`）。见[在项目里就地开发](../02-project-guide/04-focus-run.md#版本往前走)。

## 有焦点时，某个组件没启动

**现象**

一个你以为会启动的组件，列成了 `不启动（焦点之外）`。

**原因**

有焦点时，起点只有焦点，加上 `mode` 写明总要运行的组件（`enabled`、`local`、`debug`）；别的组件只有被它们需要时才启动。
焦点不依赖的组件——比如调用焦点的那些——都不会启动。

**解决**

在 `deploy.local.yaml` 里给那个组件写上 `mode: enabled`，让它在你的运行里总是启动；或者用 `brickkit up --all` 运行全部组件。

## 外壳里的成员怎么单独调试

在 `deploy.local.yaml` 里，给外壳条目 `members` 下的那个成员写 `mode: debug` 和 `localPort`。这次外壳不再承载它，它在你的机器上运行，
别的组件拿到的它的地址指向你的进程。见 [本地调试工作流](../02-project-guide/03-local-debug-workflow.md#外壳成员的调试)。
