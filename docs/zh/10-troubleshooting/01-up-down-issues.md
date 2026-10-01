# up / down 常见问题

`up` 按固定的顺序工作：装载与校验三层文件 → 决定谁这次启动 → 解析依赖 → 生成部署文件 → 检查镜像 → 跑迁移 → 调用引擎。
报错出现在哪一步，大致就说明了问题在哪一层。完整流程见 [启动与停止](../02-project-guide/06-up-and-down.md)。

## 镜像不存在

**症状**

```text
❌ 错误：这些镜像要在本机构建，还没有构建
   demo/hello@1.0.0：demo-hello:1.0.0
   demo/caller@1.0.0：demo-caller:1.0.0
```

或者 `错误：镜像拉取未授权`、`错误：镜像不存在`。

**原因与解决**

`up` 从不构建镜像。要本机构建的，先 `brickkit build`；要拉取的，确认登录了镜像仓库、地址与 tag 对。详见 [构建问题](05-build-issues.md)。

## 必填配置缺失

**症状**

```text
❌ 错误：必填的组件配置没有值
   缺少配置：demo/widget@0.1.0 → API_URL
   原因：组件在 configSchema.required 里声明了它，又没有给默认值——这一项平台推导不出来，只能由项目提供
   建议：
   1. 在 config/demo-widget.yaml 里给它一个值：
    API_URL: <值>
   2. 值可以写 ${ENV_VAR}（真值放 .env）、$var:NAME（公共变量，来自 config/vars.yaml）或 file://路径
```

**原因**

组件声明了一个必填、没有默认值的配置项——通常是一个平台推导不出来的地址（另一个项目部署的服务）或凭据。
这种项缺了，组件照样能起来、看上去健康，却有一条调用路径永远不通；所以平台在启动之前就拦下，而不是让它悄悄漏掉。

**解决**

按提示在 `config/<组件>.yaml` 里填上。刚 `upgrade` 过的组件，新版本新增的必填项会以 `KEY: ""` 的形式出现在配置文件里，也要填上。

## 端口冲突

**症状一：两个组件要同一个宿主机端口。**

```text
❌ 错误：deploy.yaml 校验失败
   文件：deploy.yaml
   components[1].exposePort：与 components[0].exposePort 冲突（宿主机端口 18080 已被占用）
   建议：完整字段说明：docs/zh/11-reference/03-deploy-yaml-schema.md（英文版把 zh 换成 en）
```

**解决**：给其中一个换 `exposePort`（或 `localPort`）。

**症状二：端口被本机别的程序占着。** 校验通过了，`docker compose` 启动容器时失败，引擎的输出里有 `port is already allocated` 或 `address already in use`。

**解决**：找出占用它的程序（`ss -ltnp | grep <端口>`），停掉它，或者换一个 `exposePort`。

**症状三：外壳上两个组件用了同一个端口**——见 [外壳问题](06-shell-issues.md#外壳上两个组件要同一个端口)。

## 迁移失败

**症状**

```text
❌ 错误：数据库迁移失败
   组件：demo/caller@1.0.0
   看日志：docker compose -p brickkit-my-shop logs demo-caller-1-0-0-migration
```

**原因与解决**

迁移以非零退出，主服务不会启动。按提示看迁移容器的日志，修好之后重新 `up`，迁移会再跑一次。详见 [迁移问题](07-migration-issues.md)。

## 组件日志正常，平台却说它不健康

**症状**

`up` 等到最后报 `错误：部分组件没有正常启动`，或者依赖它的组件一直不启动。而这个组件自己的日志说它已经就绪、在监听端口。

**原因**

Docker 下，健康检查在容器**里面**执行，经由镜像里的 `/bin/sh`。`type: http` 执行的是：

```text
wget -q --spider http://localhost:8080/healthz || curl -fsS http://localhost:8080/healthz || exit 1
```

`type: tcp` 执行的是 `nc -z localhost 8080`。所以 `type: http` 要求镜像里有 `/bin/sh` 加 `wget` 或 `curl`，`type: tcp` 要求有
`/bin/sh` 加 `nc`。缺了，这条命令就永远失败，容器永远被判为不健康——不管组件本身多正常。`scratch`、distroless 镜像根本没有 shell，
**两种类型都过不了**；基于 `busybox` 或 `alpine` 的镜像这些都有。另一种常见的：健康检查路径写错了（`component.yaml` 的
`healthCheck.path` 与组件实际提供的不一致）。

K8s 下没有这个问题：探针（`httpGet`、`tcpSocket`）由 kubelet 从容器外面发起，不需要镜像里有任何工具。

**解决**

- 看容器的健康检查记录：`docker inspect --format '{{json .State.Health}}' <容器名>`，最后几条的输出会直接说 `wget: not found`、
  `nc: not found`，或者 `/bin/sh` 不存在。
- 查镜像里有什么：`docker run --rm --entrypoint sh <镜像> -c 'command -v wget curl nc'`（连这条都跑不起来，就是镜像里没有 shell）。
- 给镜像补上工具：最终阶段基于 `busybox` 或 `alpine`（几 MB，自带 `sh`、`wget`、`nc`）。从 `http` 改成 `tcp`，只在镜像里有 `nc`
  却既没有 `wget` 也没有 `curl` 时才有用。
- 镜像必须保持没有 shell 时，剩下的选择是 `healthCheck.type: none`：依赖它的组件只等它的容器启动、不等它就绪，K8s 下它也不再有探针——
  所以依赖方要自己在它起来的过程中重试。
- 路径对不上就改 `healthCheck.path`。

## 冷启动超过一分钟的组件被判失败

**症状**

一个启动慢的组件（重型 Spring Boot、预加载很多东西的 Django、.NET 的第一次 JIT）：Docker 下 `up` 报它不健康；Kubernetes 下 Pod 反复重启、一直 `CrashLoopBackOff`。
而容器自己的日志从头到尾看起来都正常，只是还没来得及启动完。

**原因**

健康检查的节奏是平台固定的：10 秒一次、3 秒超时、连续 3 次失败算不健康。平台给每个组件留了 60 秒的启动宽限期，在这之内的失败不算数。
冷启动超过 60 秒，宽限期结束时它还没就绪，就被判死了；在 Kubernetes 下被杀掉重启，又从头开始，永远起不来。

**解决**

在组件的 `component.yaml` 里调大宽限期，留出余量：

```yaml
healthCheck:
  type: http
  path: /healthz
  startPeriodSeconds: 180
```

宽限期只推迟"判它死"，从不推迟"判它活"：两秒就就绪的组件照样两秒变健康，所以设得宽松没有代价。

## `up` 卡住不动，迁移容器一直在跑

**症状**

`up` 一直不返回。`docker compose -p brickkit-<项目> ps` 里主服务停在 `Created`，迁移容器却在 `running`；迁移容器的日志说服务已就绪、在监听端口。

**原因**

迁移容器和主服务用的是同一个镜像，只是命令参数不同。组件的入口程序遇到不认识的参数（`migration.command` 拼错了）时没有报错退出，而是**照常启动了服务**。
于是迁移容器变成了第二个一直在跑的服务，永远不结束；主服务要等它"成功结束"才启动，就永远等下去。

**解决**

- 入口程序对不认识的参数必须立刻失败退出（非零退出码），见 [迁移容器](../05-migration/01-migration-service.md)。这要在读环境变量、连数据库**之前**判断，
  否则一个拼错的参数会表现成一句误导人的"连不上数据库"。
- 核对 `migration.command` 的拼写。
- 先 `Ctrl+C`，再 `brickkit down` 清掉卡住的容器。

## `docker compose logs` 什么都没有

**症状**

组件明明在跑，`docker compose logs` 却什么都不输出，或者说找不到服务。

**原因**

`docker compose` 按**项目名**找容器。BrickKit 生成的 Compose 项目名是 `brickkit-<项目名>`，而你在项目目录里直接执行时，Compose 用的是目录名，看的是另一个（空的）项目。

**解决**

带上 `-p`：

```bash
docker compose -p brickkit-my-shop logs -f demo-caller-1-0-0
```

`up` 结束时打印的"查看日志"那一行就是正确的命令。

## 部署到了错的集群（或者差点）

**症状**

```text
❌ 错误：当前连着的不是配置里指定的集群
   部署文件要求（k8s.context）：prod-cluster
   当前 context：dev-cluster
   建议：
   1. 切过去：kubectl config use-context prod-cluster
   2. 要部到的就是 dev-cluster 的话：用一份 k8s.context 写着它的部署文件（brickkit up -f <文件>）
   3. 确认无误前不要继续——部到错误的集群是不可逆的
```

**原因**

部署文件的 `k8s.context` 写着一个集群，而 `kubectl` 当前的上下文是另一个。部错集群无法撤回，所以 `up` 先核对，对不上就停下。

**解决**

`kubectl config use-context <部署文件里写的那个>`，或者改用写着当前集群的那份部署文件（`-f`）。命令行上没有临时换集群的参数：一个集群一份部署文件。

## `down` 在 Podman 上失败：`permission denied`

**症状**

部署目标是 `podman`。`up`、`status`、正常的请求都没问题，`down`（或者 `up` 清理多余容器时）却失败，引擎的输出里有：

```text
Error: removing container ...: 1 error occurred:
	* rootless netns: kill network process: permission denied
```

BrickKit 认出这个特征，会在报错的建议里加一句：

```text
这是一个已知的 Ubuntu/Debian AppArmor 缺口，拦住了 rootless Podman 的网络拆卸（containers/podman#27372），不是 BrickKit 或 Podman 的 bug——修法见 docs/zh/10-troubleshooting/01-up-down-issues.md
```

在受影响的机器上它**每次**都复现，不是偶发。容器本身通常已经没了（Podman 会退回用 `SIGKILL`），但网络资源未必释放，而命令以失败退出——
很容易被误认成"这次根本没停下来"。

**原因**

不用 root 运行的 Podman 靠一个叫 `pasta` 的辅助进程给容器接网络。停止容器时，Podman 要给 `pasta` 发一个 `SIGTERM`，让它把网络拆干净。
Ubuntu / Debian 默认的 AppArmor 策略拦下了这个信号。这是发行版打包安全策略时的缺口，Podman 上游有完全相同的报告
（[containers/podman#27372](https://github.com/containers/podman/issues/27372)），Debian 也追查过同一个报错
（[Debian #1100135](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1100135)）。

**先确认自己是否受影响**

```bash
scripts/podman/check-environment.sh
```

它基本只读；为了拿到真实的答案，会起一个用完即删的测试容器——光看配置文件判断不了，拦不拦取决于内核里当前加载的策略。
退出码 `0` 是正常，`1` 是复现了这个问题，`2` 是检查本身跑不起来（比如没装 Podman）。

**修复**

```bash
scripts/podman/fix-apparmor.sh
```

**跑之前先读一遍脚本——它是破坏性的。** 它把 Podman 重置成干净状态：删除你已有的 Podman 容器、镜像、卷，用 apt 重装 `podman` 与 `apparmor-utils`，
重新生成基础配置。需要 `sudo`，只在 Ubuntu / Debian 上验证过，不碰 Docker。它用了两条路径，两条一起用验证过能解决问题：

1. **删掉那份占位的 `podman` AppArmor profile**（Debian 的结论）：它本身什么都不限制，却给 `podman` 进程挂了个名字，触发了 AppArmor 的跨 profile 信号仲裁。
   脚本先写一份极简的同名文件、从内核卸载它，再删文件——顺序反过来的话，卸载命令读不到文件，内核里的旧 profile 会悄悄留着。
2. **把 `pasta` 切成 complain 模式**（`aa-complain /usr/bin/pasta`）：AppArmor 对这一个进程只记录、不拦截。系统其它部分不受影响；
   代价是这一个进程从此只被监控、不再被限制。

脚本还顺手处理了三件与这个信号无关、但同样会让 `up` 失败或卡住的事：给 `registries.conf` 配上默认的镜像仓库（否则拉 `alpine:3` 这种不带仓库前缀的镜像会失败，
或者弹出交互式选择，把非交互的 `up` 卡住）；设置 `policy.json`（否则 Podman 拒绝拉任何镜像）；启用 `podman.socket`
（`podman compose` 通过它与 Podman 通信，没有它会报 `failed to connect to the docker API`）。只缺 socket 时单独执行：

```bash
systemctl --user enable --now podman.socket
```

**另外两个前提**

- **从 snap 打包的应用里开的终端**（比如 snap 安装的 VS Code）会把 `$XDG_DATA_HOME` 重定向到一个带版本号的路径，Podman 升级后报
  `database configuration mismatch`。`check-environment.sh` 会标出它；修法是在 shell 的启动文件里设
  `export XDG_DATA_HOME="$HOME/.local/share"` 与 `export XDG_CONFIG_HOME="$HOME/.config"`。
- **`podman compose` 要用 Docker 自带的 Compose V2 插件。** `podman compose` 自己没有 compose 实现，只是转发给找到的外部程序。
  验证过的是 Docker 与 Podman 装在同一台机器上、它转发给 Docker 的 Compose 插件的情况。执行 `podman compose version`，
  看到 `Executing external compose provider ".../docker-compose"` 就是这条路；显示的是独立的 `podman-compose` 项目时，与 BrickKit 的配合没有验证过。

## `down` 停不掉 `mode: local` 的组件

**症状**

`brickkit down` 之后，一个 `mode: local` 的组件还在跑；`down` 只打印了一个会话的进程号。

**原因**

`mode: local` 的进程由运行 `up` 的那个终端在前台看护，它不是容器。`down` 只管容器，也伸不进另一个终端的进程树。

**解决**

到那个终端里 `Ctrl+C`。正常停止时不会打印崩溃摘要。

## 改了配置，`up` 之后没有变化

**原因与解决**

- 键名写错了：`up` 的输出里会有"不在组件的 configSchema 里，不会生效"的警告，见 [配置冲突问题](03-config-conflict-issues.md#配置写了却没有生效)。
- 改的不是这次读的文件：本地模式开着时读的是 `deploy.local.yaml`，而 `vars:` 你改在了 `deploy.yaml` 里。`brickkit local status` 看这次读哪份。
- 改的是代码而不是配置：代码改了要 `brickkit build --force`，见 [构建问题](05-build-issues.md)。
