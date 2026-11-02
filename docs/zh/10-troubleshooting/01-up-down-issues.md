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
   建议：完整字段说明：brickkit docs 11-reference/03-deploy-yaml-schema（网页版：https://github.com/brickKit/brickKit/blob/v1.1.0/docs/zh/11-reference/03-deploy-yaml-schema.md）
```

**解决**：给其中一个换 `exposePort`（或 `localPort`）。

**症状二：端口被本机别的程序占着。** 校验通过了，`docker compose` 启动容器时失败，引擎的输出里有 `port is already allocated` 或 `address already in use`。

**解决**：找出占用它的程序（`ss -ltnp | grep <端口>`），停掉它，或者换一个 `exposePort`。

`mode: local` 的进程，`up` 在启动任何一个之前就查出来，一个都不启动：

```text
❌ 错误：mode: local 进程要用的端口，已经被这台机器上别的程序占着
   端口 8083：demo-gamma-0-1-0: 已经有别的程序在这个端口上应答
   建议：
   1. 停掉占着它的程序，再运行 up；mode: local 的进程一个都没有启动
   2. 或者把进程挪到别的端口：在部署文件里它的条目上写 localPort（component.yaml 里声明的端口挪不动）
```

这时容器已经起来了，只有本机进程在等。`extraPorts` 里的端口、外壳替成员监听的端口写在 `component.yaml` 里：要在你的机器上把它腾出来。

**症状三：外壳上两个组件用了同一个端口**——见 [外壳问题](06-shell-issues.md#外壳上两个组件要同一个端口)。

**症状四：有多个副本的组件要映射宿主机端口。**

```text
❌ 错误：demo/hello@1.0.0 有 2 个副本，却要映射宿主机端口——一个宿主机端口只能映射给一个容器
   宿主机端口：18080:8080
   建议：本机上的进程（mode: local / debug）要经宿主机端口连它：在这次读的部署文件里（本地模式下是 deploy.local.yaml）把它改成 replicas: 1，或者让那个调用方也跑在容器里
```

Docker / Podman 下一个宿主机端口只能映射给一个容器。端口要么来自它自己的 `expose`，要么来自一个本机进程要连它；建议按来源给。

**解决**：照建议做。只是自己在本机调试时，在 `deploy.local.yaml` 里写 `replicas: 1`，团队的 `deploy.yaml` 不用动。

## 迁移失败

**症状**

```text
❌ 错误：数据库迁移失败
   组件：demo/caller@1.0.0
   看日志：docker compose -p brickkit-my-shop logs demo-caller-1-0-0-migration
```

**原因与解决**

迁移以非零退出，主服务不会启动。按提示看迁移容器的日志，修好之后重新 `up`，迁移会再跑一次。详见 [迁移问题](07-migration-issues.md)。

`podman.units: quadlet` 时，报错点名迁移的单元和它的日志（`journalctl [--user] -u <单元>`），而且 `up` 在动任何组件之前就停下了：它们照旧跑着上一个版本。
迁移成功之前，`brickkit status` 会一直说上一次的迁移失败了。

## 组件日志正常，平台却说它不健康

**症状**

`up` 等到最后报 `错误：部分组件没有正常启动`，或者依赖它的组件一直不启动。而这个组件自己的日志说它已经就绪、在监听端口。

**原因**

Docker 与 Podman 下（compose 与 Quadlet 单元都一样），健康检查在容器**里面**执行，经由镜像里的 `/bin/sh`。`type: http` 执行的是：

```text
wget -q -O /dev/null http://127.0.0.1:8080/healthz || curl -fsS -o /dev/null http://127.0.0.1:8080/healthz || exit 1
```

`type: tcp` 执行的是 `nc -z 127.0.0.1 8080`。所以 `type: http` 要求镜像里有 `/bin/sh` 加 `wget` 或 `curl`，`type: tcp` 要求有
`/bin/sh` 加 `nc`。缺了，这条命令就永远失败，容器永远被判为不健康——不管组件本身多正常。`scratch`、distroless 镜像根本没有 shell，
**两种类型都过不了**；基于 `busybox` 或 `alpine` 的镜像这些都有。另一种常见的：健康检查路径写错了（`component.yaml` 的
`healthCheck.path` 与组件实际提供的不一致）。

两条命令发的都是 `GET`，与 Kubernetes 的 `httpGet` 探针一样，所以这个路径只要答 `GET`。不用 `wget --spider`，是因为
GNU wget（Debian、Ubuntu 镜像装的那个）用它发 `HEAD`，BusyBox 的发 `GET`：只为 `GET` 注册了路由的组件会在 Alpine 上健康、换成 Debian 系的镜像就永远不健康。

检查打的是 `127.0.0.1`，不是 `localhost`：容器里 `localhost` 同时指向 `127.0.0.1` 和 `::1`，Alpine 的 `wget` 与 `nc` 取到的是 `::1`，
而只监听 `0.0.0.0` 的服务（Python、Node 的常见写法）在 IPv6 上没人接。所以组件要监听 `0.0.0.0` 或 `::`——只监听 `localhost` 的服务，
别的容器本来也连不上它。

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
- 输出是 `Connection refused` 而组件确实在跑：看它监听的地址（`docker exec <容器名> netstat -ltn`），要是 `0.0.0.0:<端口>` 或 `:::<端口>`，
  不能是 `127.0.0.1` 以外的某一个地址或 `::1`；端口要是 `deployment.port`：平台把它作为 `PORT` 传进去，代码监听 `PORT` 就不会像写死在代码或镜像里的端口那样与清单对不上。

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

`up` 结束时打印的"查看日志"那一行就是正确的命令。`podman.units: quadlet` 时没有 Compose 项目：日志在 journald 里，
`journalctl --user -u 'brickkit-<项目名>_<服务名>*'`（有根 Podman 去掉 `--user`），`podman logs <容器>` 看到的也一样。

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

## Podman：先跑一遍环境检查

部署目标是 `podman` 时，下面几节的问题都出在主机环境上，不是 BrickKit 或 Podman 的 bug。遇到其中任何一个，或者第一次在一台机器上用 Podman，
先跑：

```bash
scripts/podman/check-environment.sh                  # 不写 podman.units：compose
scripts/podman/check-environment.sh --units quadlet  # podman.units: quadlet
```

**用跑 `brickkit up` 的那个用户跑它。**
脚本的 `--units` 填部署文件的 `podman.units`：compose 要 `podman.socket` 和开机单元，Quadlet 单元要 Podman 5.3 或更新、这两样都不要。 Podman 有两种运行方式，环境问题各不相同：

- **无根**（rootless，普通用户运行）：容器的网络由一个叫 `pasta` 的辅助进程在用户空间里接上，不经过宿主机的防火墙转发。
- **有根**（rootful，以 root 运行）：容器挂在宿主机的网桥上（netavark 默认叫 `podman0`、`podman1`……），容器之间的流量经过宿主机的转发链。

以普通用户跑检查的是无根，以 root 跑检查的是有根。它基本只读；为了拿到真实的答案，会起几个用完即删的测试容器和一个测试网络——光看配置文件判断不了，
拦不拦取决于内核里当前加载的策略和防火墙规则。它依次检查：`$XDG_DATA_HOME` 有没有被 snap 重定向、无根 Podman 在开机单元启动它的地方能不能建用户命名空间
（见[下面](#podman-报-failed-to-reexec-permission-denied)）、`podman.socket` 开没开、同一个网络里的两个容器能不能互相访问、容器能不能干净地删掉。
最后只以警告的形式列出部署要[在重启机器之后回来](#重启机器之后容器没有回来)还缺什么：linger、开机单元启用并等网络、Podman 的存储按时挂上，以及有根 Podman 与 Docker 同机时把防火墙规则加回去的那个单元。退出码 `0` 是正常，`1` 是复现了下面某个已知问题，`2` 是检查本身跑不起来（比如没装 Podman）。

**另外两个前提**

- **从 snap 打包的应用里开的终端**（比如 snap 安装的 VS Code）会把 `$XDG_DATA_HOME` 重定向到一个带版本号的路径，Podman 升级后报
  `database configuration mismatch`。`check-environment.sh` 会标出它；修法是在 shell 的启动文件里设
  `export XDG_DATA_HOME="$HOME/.local/share"` 与 `export XDG_CONFIG_HOME="$HOME/.config"`。
- **`podman compose` 要用 Docker 自带的 Compose V2 插件。** `podman compose` 自己没有 compose 实现，只是转发给找到的外部程序。
  执行 `podman compose version`，看到 `Executing external compose provider ".../docker-compose"` 就是这条路；
  显示的是独立的 `podman-compose` 项目时，与 BrickKit 的配合没有验证过。验证过的是无根、有根 Podman 各自与 Docker 装在同一台机器上、
  转发给 Docker 的 Compose 插件；有根 Podman 与 Docker 同机时要在 Docker 的防火墙链里放行 Podman 的网桥，见 [下面一节](#podman-上全部-healthy容器之间的调用却超时)。

## `up` 在 Podman 上失败：`failed to connect to the docker API`

**症状**

部署目标是 `podman`，`up`（或 `down`、`status`）一开始就失败，引擎的输出里有下面两种之一（`up` 是前一种，`down`、`status` 是后一种）：

```text
failed to connect to the docker API at unix:///run/user/1000/podman/podman.sock; check if the path is correct and if the daemon is running
Cannot connect to the Docker daemon at unix:///run/user/1000/podman/podman.sock. Is the docker daemon running?
```

以 root 运行时路径是 `unix:///run/podman/podman.sock`。

**原因**

`podman compose` 把调用转给 Docker 的 Compose 插件，插件通过 Podman 的 API socket 与 Podman 通信。这个 socket 默认不开。
无根与有根各有各的 socket：无根的在 `/run/user/<uid>/` 下，是用户级的 systemd 单元；有根的是 `/run/podman/podman.sock`，是系统级的单元。
开错了那一个，报错不会变。

**解决**

BrickKit 认出这个报错，会在建议里按报错里的路径给出对应的命令：

```bash
systemctl --user enable --now podman.socket        # 无根
sudo systemctl enable --now podman.socket          # 有根
```

## Podman 上全部 healthy，容器之间的调用却超时

**症状**

部署目标是 `podman`，以 root 运行（有根），这台机器上还装着 Docker。`up` 成功、每个组件都 healthy、`status` 全绿，
但容器之间的调用全部超时，容器访问宿主机上映射出来的端口也超时；容器访问宿主机上的进程（`mode: local` 的组件）却正常。
没有任何报错：健康检查只查组件自己，查不到它连不连得上别人。

**原因**

Docker 启动时把宿主机防火墙 `FORWARD` 链的默认策略设成 `DROP`，只放行它自己网桥上的流量。有根 Podman 的容器之间的流量也走这条转发链，
于是被丢掉。netavark 在自己的规则表里放行也没用：同一个转发环节上，任何一处丢弃都是最终的。

无根 Podman 不受影响：它的网络由 `pasta` 在用户空间里转发，不经过宿主机的 `FORWARD` 链。

以 root 跑 `check-environment.sh` 会实际试一次两个容器互相访问；不通，而且 `FORWARD` 的默认策略是 `DROP`、存在 Docker 的 `DOCKER-USER` 链时，
它会认出这个问题。

**解决**（任选一条）

1. **在 Docker 的 `DOCKER-USER` 链里放行 Podman 的网桥。** Docker 留这条链就是给你加自己的规则用的：

   ```bash
   sudo iptables -I DOCKER-USER -i podman+ -j ACCEPT
   sudo iptables -I DOCKER-USER -o podman+ -j ACCEPT
   ```

   规则重启后会丢，而且 Docker 每次启动都重建自己的链，所以要在 Docker 启动之后、Podman 的开机单元拉起容器之前把它们加回去。一个一次性单元就能做到
   （BrickKit 不碰主机防火墙；这是一种做法，真重启验证过）：

   ```bash
   sudo tee /etc/systemd/system/podman-docker-forward.service >/dev/null <<'UNIT'
   [Unit]
   Description=Let rootful Podman's bridges through Docker's DOCKER-USER chain
   After=docker.service
   Before=podman-restart.service

   [Service]
   Type=oneshot
   RemainAfterExit=yes
   ExecStart=/bin/sh -c 'for d in -i -o; do iptables -C DOCKER-USER $d podman+ -j ACCEPT 2>/dev/null || iptables -I DOCKER-USER $d podman+ -j ACCEPT; done'

   [Install]
   WantedBy=multi-user.target
   UNIT
   sudo systemctl daemon-reload
   sudo systemctl enable --now podman-docker-forward.service
   ```

   每条规则只在缺的时候才加，再跑一次什么也不变。开机时它紧跟 Docker 跑，Podman 的开机单元紧跟它；`podman.units: quadlet` 生成的单元自己就排在它之后。主机的防火墙由别的东西管着时（firewalld、保存下来的
   `iptables` 规则集），把这两条规则放到那里；这个单元没启用时，[环境检查](#podman先跑一遍环境检查)会给出警告。
2. **改用无根 Podman**，以普通用户运行 `brickkit up`。
3. **不在同一台机器上同时用有根 Podman 和 Docker。**

## `down` 在 Podman 上失败：`permission denied`

**症状**

部署目标是 `podman`。`up`、`status`、正常的请求都没问题，`down`（或者 `up` 清理多余容器时）却失败，引擎的输出里有：

```text
Error: removing container ...: 1 error occurred:
	* rootless netns: kill network process: permission denied
```

BrickKit 认出这个特征，会在报错的建议里加一句：

```text
这是一个已知的 Ubuntu/Debian AppArmor 缺口，拦住了 rootless Podman 的网络拆卸（containers/podman#27372），不是 BrickKit 或 Podman 的 bug——修法：brickkit docs 10-troubleshooting/01-up-down-issues（网页版：…）
```

在受影响的机器上它**每次**都复现，不是偶发。容器本身通常已经没了（Podman 会退回用 `SIGKILL`），但网络资源未必释放，而命令以失败退出——
很容易被误认成"这次根本没停下来"。

**原因**

无根 Podman 用一个共享的 `pasta` 进程给桥接网络上的容器接网络。这些容器里最后一个离开时，Podman 给 `pasta` 发一个 `SIGTERM`，让它把网络拆掉。
`pasta` 跑在 passt 包带的 AppArmor 配置下（`/etc/apparmor.d/usr.bin.pasta`），这份配置不允许接收来自 `podman` 配置的信号，内核就拒绝了——内核日志里是
`apparmor="DENIED" operation="signal" ... profile="pasta" ... signal=term peer="podman"`。这是发行版打包安全策略时的缺口，Podman 上游有完全相同的报告
（[containers/podman#27372](https://github.com/containers/podman/issues/27372)），Debian 也追查过同一个报错
（[Debian #1100135](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1100135)）。

**先确认自己是否受影响**：以普通用户跑 `scripts/podman/check-environment.sh`（见 [上面](#podman先跑一遍环境检查)），它会实际删一次测试容器。没有容器在跑时它能走到拆网络那一步；
有容器占着共享网络——或者之前一次失败的拆卸留下的计数说有——时，它会说现在查不了，以及怎样清掉。

**修复**

给 pasta 的配置加一条规则——允许接收来自 `podman` 的 `SIGTERM`——再加载它：

```bash
sudo sed -i '/^profile pasta .*{$/a\  signal (receive) set=(term) peer=podman,' /etc/apparmor.d/usr.bin.pasta
sudo apparmor_parser -r /etc/apparmor.d/usr.bin.pasta
```

在 Ubuntu 26.04（Podman 5.7.0、passt 0.0~git20260120）上验证过，删的是桥接网络上的最后一个容器：配置保持原样时，拆卸以上面的报错失败，内核记下被拒的信号；
加了这条规则后成功，`pasta` 仍处于 enforce。这份配置没有本地 include，所以规则只能写进包里的文件：以后升级 passt 时会问你保留哪一版。
改的是 `usr.bin.pasta`，不是 `usr.bin.passt`——同一个包里名字几乎一样的两个程序。不要为这个删掉 `podman` 的 AppArmor 配置（`/etc/apparmor.d/podman`）：
它与这个信号无关，而且在 Ubuntu 23.10 起正是它让 Podman 能建用户命名空间（见[下一节](#podman-报-failed-to-reexec-permission-denied)）。

把 `pasta` 切成 complain 模式（`aa-complain /usr/bin/pasta`）也能让拆卸成功，但 pasta 配置里的其余限制也一起不再执行；`fix-apparmor.sh` 的早先版本就是这样做的，现在的版本会把它切回 enforce。上面那两条命令不会去掉这个标志。
想从包里原样的配置重新开始，就删掉文件、让 apt 放回去——`--force-confmiss` 恢复缺了的配置文件，而单纯重装（哪怕加 `--force-confnew`）不会动改过的那份——再加规则：

```bash
sudo rm /etc/apparmor.d/usr.bin.pasta
sudo apt-get install --reinstall -o Dpkg::Options::=--force-confmiss passt
```

**Podman 还有别的问题时**，`scripts/podman/fix-apparmor.sh` 在一次完整的重置里做同样的事。**跑之前先读一遍——它是破坏性的**：删除你已有的 Podman 容器、镜像、卷，
用 apt 重装 `podman`，重新生成基础配置。需要 `sudo`，只在 Ubuntu / Debian 上验证过，不碰 Docker。除了上面那条规则，它还在 `podman` 配置缺了时把它恢复，
并处理三件与这个信号无关、但同样会让 `up` 失败或卡住的事：给 `registries.conf` 配上默认的镜像仓库（否则拉 `alpine:3` 这种不带仓库前缀的镜像会失败，
或者弹出交互式选择，把非交互的 `up` 卡住）；设置 `policy.json`（否则 Podman 拒绝拉任何镜像）；启用无根的 `podman.socket`
（见 [上面](#up-在-podman-上失败failed-to-connect-to-the-docker-api)）。

## Podman 报 `failed to reexec: Permission denied`

**现象**

部署目标是 `podman`，每一个 Podman 调用都以这一行失败——`brickkit up`（查镜像时）、`build`、开机时的 `podman-restart.service`
（`journalctl --user -u podman-restart.service` 里）、在 ssh 或普通终端里跑的 `podman info`：

```text
failed to reexec: Permission denied
```

内核日志里有这次拒绝：`apparmor="DENIED" operation="exec" ... profile="unprivileged_userns" name="/proc/self/exe"`。同样的命令在以 snap 安装的应用
（比如 VS Code）的终端里、或者已经有容器在跑时，可能照常能用。BrickKit 认得这一行，会在建议里指到这里。

**原因**

Ubuntu 23.10 起用 AppArmor 限制无特权用户命名空间（`sysctl kernel.apparmor_restrict_unprivileged_userns` 打印 `1`）。无根 Podman 先建一个用户命名空间，
再重新执行自己；一个没有"允许 `userns`"的 AppArmor 配置的进程，建用户命名空间时会被转进 `unprivileged_userns` 配置，而那个配置拒绝这第二次执行。
apparmor 包给 Podman 带了这样一份配置，`/etc/apparmor.d/podman`（`flags=(unconfined)` 加 `userns`）；它缺了或没加载时，在没有别的东西允许用户命名空间的地方
Podman 就会失败。snap 自己的配置允许用户命名空间；有容器在跑时 Podman 加入已有的命名空间、不新建——所以从某些终端看起来一切正常。

`scripts/podman/fix-apparmor.sh` 的早先版本删掉过这份配置，而软件包升级不会补回被删掉的配置文件。

**解决**

恢复 apparmor 包自带的这份配置（`--force-confmiss` 恢复缺了的配置文件，`--force-confold` 保留你改过的），再加载它：

```bash
sudo apt-get install --reinstall -o Dpkg::Options::=--force-confmiss -o Dpkg::Options::=--force-confold apparmor
sudo apparmor_parser -r /etc/apparmor.d/podman
```

在开机单元跑 Podman 的地方——用户的 systemd 里——检查：`systemd-run --user --wait --pipe podman unshare cat /proc/self/attr/current` 应当打印
`podman (unconfined)`。环境检查也查这一项。在整台机器上关掉这项限制（`kernel.apparmor_restrict_unprivileged_userns=0`）同样有效，但那是对所有程序放开，不只是 Podman。

## 重启机器之后容器没有回来

**现象**

部署原来在跑；机器重启之后，`brickkit status` 显示组件没有运行，或者在运行、调用却失败——`brickkit up` 一次又都好了。

**原因**

开机时 BrickKit 什么都不启动：启动容器的是引擎，而且要设好了才会。先看开机单元的日志——无根 Podman 用 `journalctl --user -b -u podman-restart.service`
（有根 Podman 去掉 `--user`；Docker 用 `journalctl -b -u docker`）——再对照下面你看到的。`podman.units: quadlet` 时没有开机单元：
每个单元自己起来、自己等网络，所以看单元自己的日志，`journalctl --user -b -u 'brickkit-<项目名>_*'`（有根 Podman 去掉 `--user`）。下表关于 linger、
`failed to reexec` 与存储挂载的几行同样适用；关于开机单元与重启策略的不适用。

| 你看到的 | 原因 | 解决 |
| --- | --- | --- |
| 开机单元一条日志都没有（Docker：守护进程没起来） | 引擎没设开机启动。无根 Podman 要给这个用户开 linger（否则这个用户的服务要等有人登录才启动），并启用用户单元 `podman-restart.service`；有根 Podman 要系统单元；Docker 要 Docker 服务 | `sudo loginctl enable-linger <用户>` 与 `systemctl --user enable podman-restart.service`；有根：`sudo systemctl enable podman-restart.service`；Docker：`sudo systemctl enable docker` |
| 开机单元以 `failed to reexec: Permission denied` 失败 | AppArmor 不让 Podman 建用户命名空间 | [上一节](#podman-报-failed-to-reexec-permission-denied) |
| 容器都起来了，组件却反复重启、连不上宿主机上的任何东西（`host.docker.internal`、宿主机上发布的端口） | 无根 Podman 的网络辅助进程（`pasta`）在机器拿到地址之前就起来了，之后也不会再拿。开机单元写的是 `After=network-online.target`，这在用户的 systemd 里不起作用；Podman 为此带了 `podman-user-wait-network-online.service`，但开机单元不依赖它 | 让开机单元等网络（见下）；这一次把容器停掉再起来——`systemctl --user restart podman-restart.service` 两样都做，新的 `pasta` 就有网络了 |
| 有根 Podman 与 Docker 同机：全部 healthy，容器之间的调用却超时——重启之前是好的 | 在 Docker 的 `DOCKER-USER` 链里放行 Podman 网桥的规则丢了：它们只在内存里，Docker 每次启动都重建自己的链 | 在 Docker 之后把规则加回去的那个单元：[Podman 上全部 healthy，容器之间的调用却超时](#podman-上全部-healthy容器之间的调用却超时) |
| Podman 报没有容器，或者存储不见了 | Podman 的存储在一块以 `nofail` 挂载的盘上，可能在开机单元启动之后才挂上（无根：在用户的 systemd 启动之后） | 无根：`sudo systemctl edit user@<uid>.service`；有根：`sudo systemctl edit podman-restart.service`，Quadlet 单元则是 drop-in `/etc/systemd/system/brickkit-.service.d/storage.conf`（[第 7 步](../09-patterns/06-deployment-selection.md#为有根-podman-准备主机)）；在 `[Unit]` 下加 `RequiresMountsFor=<那个挂载点>` |
| 开机后几个组件重启一两次，然后转 healthy | 开机时引擎启动容器不按 `up` 等待的那个启动顺序（`depends_on`）；有组件比它的依赖先起来 | 不用处理——重启策略会一直重试；组件应当重试连接，而不是退出 |
| 什么都没回来，重启前跑过 `brickkit down` | `down` 删掉了容器，开机时没有东西可启动 | `brickkit up` |
| Podman 上容器的重启策略是 `unless-stopped`（`podman inspect -f '{{.HostConfig.RestartPolicy.Name}}' <容器>`） | 容器是旧版 BrickKit 建的；Podman 的开机单元只启动策略为 `always` 的容器 | 跑一次 `brickkit up`：重启策略变了的容器会被重建 |

让无根的开机单元等网络的 drop-in 是[为无根 Podman 准备主机](../09-patterns/06-deployment-selection.md#为无根-podman-准备主机)的第 5 步。

上面每一项开机设置缺了时，`scripts/podman/check-environment.sh` 都会给出警告。按顺序的完整设置见[为有根 Podman 准备主机](../09-patterns/06-deployment-selection.md#为有根-podman-准备主机)、[为无根 Podman 准备主机](../09-patterns/06-deployment-selection.md#为无根-podman-准备主机)。

## Quadlet 单元起不来

**症状**

部署文件写了 `podman.units: quadlet`，`up` 以下面某一种失败：

| 你看到的 | 原因 | 解决 |
| --- | --- | --- |
| `这些单元没有起来`，每个带一条 `journalctl` 命令 | 容器起来又退出了，或者在单元的启动时限内一直没有 healthy | 用那条命令看单元的日志；`systemctl --user --failed 'brickkit-<项目名>_*'` 列出所有失败的单元 |
| `Podman <版本> 太旧，podman.units: quadlet 要 5.3 或更新的版本` | 单元用到 Podman 5.3 才齐的键（`Notify=healthy`、模板、`NetworkAlias=`、无根时等网络） | 升级 Podman；或者这台机器不写 `podman.units`，用 compose |
| `单元写不进 /etc/containers/systemd/…` | 有根 Podman：单元装进系统的 systemd，只有 root 能写 | `sudo brickkit up` |
| 重新加载之后紧接着 `Unit brickkit-….service not found` | Podman 的 Quadlet 生成器拒绝了一个单元文件，没有为它生成服务 | `/usr/libexec/podman/quadlet -dryrun -user`（有根 Podman 去掉 `-user`）会说出原因；单元在 `.brickkit/generated/quadlet/` |

**值得知道**

- `up` 把单元复制进 `~/.config/containers/systemd/brickkit-<项目名>/`（有根：`/etc/containers/systemd/brickkit-<项目名>/`）。这个目录就是部署出去的东西，`down` 删掉它。
  在那里手改的，下一次 `up` 会覆盖。
- 用跑 `up` 的那个用户跑 `down` 和 `status`：有根的单元在 `/etc/containers/systemd`，无根的在那个用户的主目录里，换一个用户的 `status` 会说什么都没在跑。
- 作为 Podman secret 交付的值，在容器创建时进去。`podman exec <容器> printenv` 会重新取 secret，可能看到比在跑的进程更新的值；进程自己的环境在容器里的
  `/proc/1/environ`。组件的 secret 变了，`up` 就重启它。

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
- Kubernetes 上，改的是 `existingSecret` 指向的那份外部 Secret：平台读不到它的值，也就不知道它变了。用环境变量读它的 Pod 要重启才用上新值
  （`kubectl rollout restart deployment/<服务名>`）；项目自己 `config/` 里的密钥变了，`up` 会滚动更新，不用管。
- 改的是一项以文件交付的密钥（组件在 `configSchema` 里声明了 `mount: file`）：`up` 只把文件换成新内容，**不重启组件**。
  只在启动时读一次文件的组件要重启才用上新值（`brickkit down` 再 `brickkit up`）；Kubernetes 上文件要等 kubelet 同步，通常在一分钟之内。
  见 [以文件交付](../01-three-layers/07-sensitive-values.md#以文件交付mount-file)。
