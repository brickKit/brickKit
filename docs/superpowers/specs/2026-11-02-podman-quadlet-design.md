# `target: podman` 生成 Quadlet 单元：设计

来源：be-assembly-standard 反馈 FRR-025。状态：2026-11-02 维护者确认（§10 两条都按推荐）。

## 1. 要解决什么

`target: podman` 现在生成 `compose.yaml`，`up` 经 `podman compose`（转给 Docker 的 `docker-compose`，再经
`podman.socket` 连 Podman）。拿它做长期交付，开机这条路有几处是 compose + `podman-restart.service` 特有的：

| 问题 | 现在 | 用 Quadlet |
| --- | --- | --- |
| 开机拉起 | `podman-restart.service` 只拉 `restart: always` 的容器 | 每个单元 `WantedBy=`，systemd 自己拉 |
| 等网络 | 无根时开机单元不等网络，要手写 drop-in | Quadlet 自动加 `After=`/`Wants=`（root 等 `network-online.target`，用户等 `podman-user-wait-network-online.service`；本机 `podman-systemd.unit(5)` 确认） |
| 开机时的启动顺序 | 没有（`depends_on` 只在 `up` 时生效），组件靠重试 | `After=` 加 `Notify=healthy`：依赖健康之后才启动 |
| 迁移 | 只在 `up` 时跑，开机不跑 | 迁移是一次性单元，组件 `Requires=` 它：开机也先跑完 |
| 不健康 | 引擎不处理（Docker / Podman 都不重启 unhealthy 的容器） | `HealthOnFailure=kill` + `Restart=always` 重启（见 §5.3） |
| 日志 | 引擎的日志 | journald：`journalctl -u <单元>`，`podman logs` 照样能用 |
| 主机上要装的 | Podman、`docker-compose` 二进制、`podman.socket` | 只要 Podman（systemd 本来就有） |

Quadlet 是 Podman 4.4 起自带的 systemd 生成器，Podman 官方推荐的单机部署方式。

## 2. 边界

- **只为长期运行的部署**。Quadlet 部署与 `mode: local` / `mode: debug`、`focus` 不组合，装载阶段就拒绝，报错指出条目——与
  `target: k8s` 同一条规则、同一处检查（本机进程活在 `up` 的前台会话里，而 Quadlet 部署的意义是不依赖任何会话）。所以裸进程外壳、
  本机进程的 `extra_hosts` 与宿主机端口映射这些 compose 独有的部分都不进 Quadlet。
- **compose 这条路保留**，仍是 `target: podman` 的默认：开发机上的 `up`、焦点运行、本机进程与容器混跑都走它。
- 不做 `.kube`、`.pod`、`.build` 单元；镜像照旧由 `brickkit build` 显式构建，`up` 不构建。
- `existingSecret`（K8s 的 Secret 引用）与 compose 一样跳过。以后要接 Podman 已有的 secret 另议。

## 3. 部署文件怎么写

在部署文件里加一个与 `k8s:` 平行的 `podman:` 块：

```yaml
target: podman
podman:
  units: quadlet      # 不写 = compose
```

理由：引擎还是 Podman——镜像、有根 / 无根、环境检查、`brickkit build` 都照旧；变的只是“谁让它一直跑着”。`target` 的取值
（`docker | podman | k8s`）回答“交给哪个引擎”，这个字段回答“交给引擎的是什么文件”。代码里加一个
`Deploy.LongRunning()`（k8s 或 Quadlet）统一“拒绝本机进程与焦点”的判断。

`podman:` 块只在 `target: podman` 时起作用，写在别的 target 下警告并忽略（与 `k8s:` 块写在非 k8s 下同样处理；实现时核对过：那是警告，不是报错）。

## 4. 生成什么、放在哪

`up` 把单元写进项目里的 `.brickkit/generated/quadlet/`（看得见、进 diff，`up --dry-run` 只写到这里），再原样复制到 systemd 的
Quadlet 目录下本项目自己的子目录（Quadlet 递归扫子目录，`podman-systemd.unit(5)`）：

| 谁跑 `brickkit` | 安装到 | systemctl |
| --- | --- | --- |
| 普通用户（无根） | `~/.config/containers/systemd/brickkit-<项目名>/` | `systemctl --user` |
| root（有根） | `/etc/containers/systemd/brickkit-<项目名>/` | `systemctl` |

有根靠以 root 运行判断（与 compose 一样：以 root 跑就是有根）；有根 Quadlet 要写 `/etc` 并 `daemon-reload`，所以必须
`sudo brickkit up`——上一轮文档里“经套接字组不用 sudo”的做法对 Quadlet 不适用，文档写明。

**为什么复制，不链接**：链接让开机依赖项目目录还在、还在原处；复制之后单元自足。例外是 `mount: file` 的配置项，见 §5.1。

每个 compose 概念的对应：

| compose 里 | Quadlet 里 |
| --- | --- |
| 项目网络 `brickkit-<项目名>-net` | `brickkit-<项目名>.network`（`NetworkName=` 同名）；部署文件写了 `network:` 时不生成，单元 `Network=<那个名字>` |
| 组件 service | `brickkit-<项目名>_<版本化服务名>.container`（实现后审查改：项目名与服务名之间用 `_`），`ContainerName=` 同名，`NetworkAlias=<版本化服务名>`（别的组件照旧按服务名找它） |
| 外壳承载成员的网络别名 | 外壳单元上每个成员一行 `NetworkAlias=` |
| 迁移 service | `…-migration.container`，`[Service] Type=oneshot`（见 §5.2） |
| `image` | `Image=`，精确 tag；`Pull=missing` |
| `environment` / `env_file` | 见 §5.1 |
| `ports`（`expose`） | `PublishPort=` |
| `healthcheck` | `HealthCmd=` 等，节奏同 compose（10s / 3s / 3 次 / startPeriod） |
| `restart: always` | `[Service] Restart=always` |
| `stop_grace_period` | `StopTimeout=` |
| `deploy.resources` | `PodmanArgs=--cpus=… --memory=… --memory-reservation=…`（Quadlet 没有 CPU 键，`Memory=` 要 Podman 5.5，统一用 `PodmanArgs=` 免得最低版本被它抬高） |
| `deploy.replicas` | 模板单元 `…@.container` + 实例 `…@1.container` … `…@N.container`（Quadlet 原生支持，见 §5.4） |
| `labels` | `Label=`；另加 `brickkit.io/project`、`brickkit.io/service`，`status` 靠它们 |
| 日志 | `LogDriver=journald`：`journalctl -u` 与 `podman logs` 都能看 |
| 开机拉起 | `[Install] WantedBy=default.target`（无根）/ `multi-user.target`（有根） |

## 5. 关键机制

### 5.1 环境变量与密钥

compose 把 `${VAR}` 留给 `docker compose` 在 `up` 时从进程环境与 `.env` 展开；Quadlet 的容器每次开机由 systemd 重新创建，那时
没有使用者的环境，所以 **`${VAR}` 在 `up` 时求值**——与 K8s 目标同一条规则（K8s 生成时求值进 Secret）。求出来的值不能写进单元
文件（文件人人看得到、进 diff），去处按 compose 的 `envPlacement` 同一个判定：

| 变量 | compose | Quadlet |
| --- | --- | --- |
| 普通字面量（不含换行） | `environment` | `Environment="<键>=<值>"`：`\` → `\\`、`"` → `\"`、`%` → `%%`、**`$` → `$$`**（Quadlet 把它原样放进 `ExecStart`，systemd 在那里展开 `${X}`、把 `$$` 变成 `$`——实测） |
| `secret: true`、`file://`、含 `${VAR}` 的模板、含换行的字面量 | 0600 的 env 文件 | **Podman secret**：`podman secret create --replace brickkit-<项目>-<服务>-<键>`，单元写 `Secret=<名字>,type=env,target=<键>` |
| `existingSecret` | 跳过 | 跳过 |
| `mount: file` | 绑定挂载项目里的 0600 文件 | 同样绑定挂载项目里的文件（`Volume=`），保留“换文件不用重启” |

为什么用 Podman secret 而不是 env 文件：本机实测（Podman 5.7.0）`--env-file` 原样取值——不去引号、不认 `\n`，多行的 PEM 放不进去；
Podman secret 以 `type=env` 交付多行、`$`、引号都逐字节到达容器，`--replace` 原地更新，存放在 Podman 自己的存储里（无根在用户目录，
有根在 `/var/lib/containers`），与项目目录无关。

`mount: file` 是唯一让部署依赖项目目录的地方（compose 今天也是这样），文档写明：项目目录要留在原处。

`down` 删掉本项目的 secret；`up` 删掉不再生成的。

**secret 换了值要重启组件**（Task 7 真跑撞到）：secret 进环境变量是在容器创建的那一刻，`--replace` 之后在跑的
容器手里还是旧值，而 `podman exec printenv` 看到的却是新值（exec 时重新取），很容易误判已经生效。只改 secret 的值时
单元一个字不变，`up` 不会重启。所以组件单元写一个 `Label="brickkit.io/secret-digest=<摘要>"`，与 K8s Pod 模板上的
注解同一个做法、同一个取舍；`mount: file` 的值不算（有意不重启）。

### 5.2 迁移与启动顺序

- 迁移单元：`Type=oneshot`，**不写 `RemainAfterExit`**——跑完回到 inactive，下一次 `start` 会再跑一遍。用组件自己的镜像、同一份环境变量，
  `Exec=` 覆盖 entrypoint 的规则与 compose 相同。
- 组件单元：`Requires=` + `After=` 自己的迁移（失败就不启动，开机也一样）；同一组件的迁移链（多版本并存）用同样的 `Requires=`/`After=`
  串起来；外壳还 `Requires=`/`After=` 它承载的成员的迁移。
- 强依赖（含外壳的 `WaitFor`）：`Wants=` + `After=`，**不用 `Requires=`**：`Requires=` 会在依赖被显式停止或重启时连带停掉、重启这个组件，
  而平台的约定是组件自己重试连接（与 compose 的 `depends_on` 只管启动顺序一致）。依赖有健康检查时写 `Notify=healthy`，于是 `After=`
  等到它健康——对应 compose 的 `service_healthy`；没有健康检查的只等启动，对应 `service_started`。
- 不写 `RemainAfterExit` 的迁移、`Requires=` 的组件：迁移跑完自然转 inactive 不会连带停组件（`Requires=` 只对显式 stop / restart
  传递）；`systemctl restart` 一个组件会先把它 inactive 的迁移拉起来再跑一遍；迁移非 0 退出时组件起不来（`A dependency job for … failed`），
  `Wants=` 它的依赖方照常运行，`restart` 也不连带重启依赖方。**实测**（2026-11-02，Podman 5.7.0，用户 systemd）。
- `restart` 是先停再启：迁移失败时旧容器已经停了，而 compose 下迁移失败旧容器还在跑。所以 `up` 先单独把迁移跑完（§6），全部成功才动组件。
- 动组件时带 `--job-mode=ignore-requirements`：普通方式 `start` 一个组件——哪怕它已经在跑——systemd 也会把它 `Requires=` 的、
  已经回到 inactive 的迁移再拉起来跑一遍（Task 7 真跑：一次 `up` 跑了两遍）。这个模式不拉依赖，但单元之间的 `After=` 照旧遵守
  （实测：依赖健康之后才启动）。开机时没有这个模式，`Requires=` 让迁移先跑，正是要的。
- 试过、不用的：迁移留存（`RemainAfterExit=yes`）加组件 `Requisite=`——迁移的重启与失败会连带重启、停掉组件（实测）。
- 组件单元 `TimeoutStartSec=` 按 startPeriod 加 interval × retries 再加余量算（`Notify=healthy` 下启动作业要等到健康）；oneshot 没有启动超时，
  与 compose 的迁移一样。
- 开机时每个迁移都会再跑一次。迁移本来就要求可重复执行（FRR-019 写进了组件规范），这里比 compose 更严格地保证了顺序。

### 5.3 健康检查与重启

compose 的健康检查不杀容器，所以声明了 `readinessCheck` 时探它（问的是“能接流量了吗”）。Quadlet 上要不要 `HealthOnFailure=kill`
取决于探的是什么：

- 组件**没有**单独的 `readinessCheck`：探 `healthCheck`，写 `HealthOnFailure=kill`——它是存活检查，不健康就杀掉、由 `Restart=always`
  拉起，与 K8s 的 liveness 一样。
- 组件写了 `readinessCheck`：探它（`Notify=healthy` 要的就是就绪），**不杀**——K8s 也不因就绪失败重启 Pod。
  Podman 一个容器只有一个常规健康检查，两个都要的话只能取舍；这样取舍保证不会因为“暂时不接流量”被杀。

### 5.4 副本

`replicas: N`（N > 1）生成模板 `…@.container`（`ContainerName=…-%i`，`NetworkAlias=<服务名>`：netavark 的 DNS 对同名别名返回全部
副本）和 N 个实例链接 `…@1.container` … `…@N.container`；依赖它的单元对每个实例 `Wants=`/`After=`。多副本加宿主机端口照旧在生成期报
`PORT_CONFLICT`（compose 已有的检查共用）。

### 5.5 有根与 Docker 同机

有根 Podman 与 Docker 同机时要的 `podman-docker-forward.service`（v1.11.4 排障页的单元）今天写的是 `Before=podman-restart.service`。
Quadlet 的单元由生成器起名，那个单元不知道它们；所以有根时每个生成的单元写 `After=podman-docker-forward.service`——单元不存在时
systemd 忽略这条顺序，存在时保证防火墙规则先加上。文档给那个单元写明这一点。

## 6. 命令

- **`up`**：生成 → 写进 `.brickkit/generated/quadlet/` → 检查镜像（照旧）→ 写 / 换 Podman secret → 把单元同步到安装目录（删掉不再
  生成的，先 `stop` 再删）→ `daemon-reload` → 一条 `systemctl start` 起网络单元、跑全部迁移单元（与 compose 每次 `up` 都跑迁移一致；任何一个失败
  就停在这里，组件一个都不动，旧版本照跑）→ 对内容变了的组件单元 `restart`、其余 `start`，都带 `--job-mode=ignore-requirements`
  （§5.2），交给 systemd 按 `After=` 排序并等待。失败时按单元名认出是哪个迁移或组件，
  报 `MIGRATION_FAILED` 或启动失败，并给出 `journalctl [--user] -u <单元>`。`up --wait` 的语义由 `Notify=healthy` 的启动作业给出。
- **`down`**：`stop` 本项目全部单元 → 删安装目录 → `daemon-reload` → 删 secret 与网络（停掉网络单元不删网络，实测；`podman network rm`）。与 compose 的 `down` 一样不删数据卷（BrickKit
  本来就不建卷）。
- **`status`**：`podman ps -a --filter label=PODMAN_SYSTEMD_UNIT`（Quadlet 给容器打的标签），在代码里按安装目录里的单元筛——
  Podman 的多个 `label` 过滤条件要同时满足，不是任意一个（实测）；取状态与健康（与 compose 同一张表），另外列出 `failed` 的单元。
  表格之后说出上一次失败的迁移：Quadlet 下迁移失败时组件照跑旧版本，表格全绿（compose 下非 0 退出的迁移容器同样列出）。
- **compose ↔ Quadlet 切换**：同一个项目两种都留着会在开机时撞端口。`up` 先清掉另一种的残留：Quadlet `up` 删掉 compose 项目的容器
  （按 compose 的项目标签，用 `podman rm`，不需要 `docker-compose`），compose `up` 发现本项目的 Quadlet 安装目录就先 `stop` 并删掉。
  打印一行说清删了什么。现有的 `pruneOtherTarget`（compose ↔ k8s 的生成文件）一起扩成三种。
- **`lint`**：检查 `podman:` 块的字段、与 `mode: local` / `debug` / `focus` 的冲突（与装载同一处）。

## 7. 代码结构

- **抽出共用的容器计划**。`internal/compose` 的 `plan` 现在混着两类东西：每个单机容器目标都要的（组件、迁移链、外壳分组与别名、
  环境变量的去处、端口与副本检查、健康检查命令、配额换算），和只有 compose 有的（本机进程、`extra_hosts`、裸进程外壳、`RunAfter`、
  compose 的 `$` 转义与 env 文件格式）。把前一类挪进新包 `internal/container`（名字待定），compose 与 Quadlet 都渲染它；后一类留在
  compose。这样两个渲染器不会各算一遍迁移顺序、外壳等待——那是最容易各改各的地方。
- 新包 `internal/quadlet`：纯函数，进去是共用计划，出来是单元文件内容、要建的 secret 列表、警告。
- `internal/engine` 加 `Quadlet` 引擎：`Up` / `Down` / `Status` / `CheckImage` 用 `systemctl` 与 `podman` 命令（不经 socket）。
- `deployfile` 加 `podman:` 块与 `LongRunning()`；`cascade` 的拒绝规则改用它。
- 生成的 JSON Schema、`brickkit version` 的能力列表、字段参考页随之更新。

## 8. 主机要求

- **Podman 5.3 起**，`up` 检查 `podman version`，不够就报错说要几。按 Podman 的发布说明：`Notify=healthy`、模板单元、`StopTimeout=` 5.0，
  `NetworkAlias=` 5.2，无根单元自动等网络（`podman-user-wait-network-online.service`）5.3；`Secret=` 4.5、子目录递归 4.7。
  （Ubuntu 24.04 的 4.9 不够，26.04 是 5.7，Debian 13 是 5.4。）
- 不需要 `docker-compose` 与 `podman.socket`。
- 无根仍要：linger、Ubuntu 23.10 起的 AppArmor `podman` 配置、`nofail` 盘的 `RequiresMountsFor`。不再要：`podman-restart.service`
  与等网络的 drop-in。环境检查脚本按部署文件选的方式分别报（Quadlet 时不查 `podman-restart.service`、socket）。

## 9. 测试与验证

- 渲染：每种组合的单元文件（独立、外壳、副本、迁移链、多版本、`network:`、`expose`、`mount: file`、有根 / 无根）对照期望输出；
  拒绝本机进程与焦点的报错。
- 环境变量：多行、`$`、引号、`%`、`${VAR}` 经 Podman secret 与 `Environment=` 逐字节到达容器——真 Podman 测。
- 本机真跑（无根，用户 systemd）：`up`、改一个组件后 `up`（只重启它、迁移再跑）、迁移失败、`down`、compose ↔ Quadlet 切换。
- §5.2 那条“`Requires=` + 不留存的 oneshot”的行为先做最小实验再定。
- 真重启：请 be-assembly-standard 用同一个格（P9-S）有根、无根各重启一次——不配 `podman-restart` 与等网络的 drop-in，组件按依赖顺序
  回来、全部 healthy；`down` 之后再重启什么都不回来。
- 文档：部署选择页“一台机器”与“重启机器之后”、排障页、字段参考、CLI 参考、`brickkit-deploy` 技能；`check-environment.sh`。

## 10. 维护者的决定（2026-11-02）

1. **字段写法**：`podman: { units: quadlet }`（§3），不加新的 target 值。新 target 会让 `target` 同时承担“引擎”和“文件格式”两件事，
   有根 / 无根、环境检查、`build` 的说法都要再拆一遍。
2. **做完之后的推荐**：单机用 Podman 交付时，文档推荐 Quadlet；compose 留给开发机与本机进程混跑（开发机与交付机本来就是两份部署文件）。


## 11. 实现之后的审查（2026-11-02）

独立审查（一个只看代码与本文档的子代理）提出 11 条，核实后这样处理（实现与上文不一致之处以这里为准）：

1. **名字前缀相撞**：项目 `shop` 按前缀删 secret 会删掉 `shop-x` 的；单元名也能撞（`shop` 的 `x-api` 与 `shop-x` 的 `api`）。
   改成 `brickkit-<项目名>_<服务名>`、secret `…_<键>`：项目名与服务名里都没有 `_`。
2. **升级时旧版本先被停**：服务名带版本号，单元名就换了，旧单元在迁移之前就被停、删。改成迁移全部成功之后才卸下不再生成的单元。
3. **迁移失败后再 up，组件跑着旧配置却报成功**：“变没变”拿磁盘上的文件比，而失败的那次已经写进去了。安装目录里加 `.brickkit-pending-restart`，
   记下写了还没重启的单元，重启成功才划掉。
4. **空值的 secret 让 up 失败**：Podman 不收空 secret。空值写进单元。
5. **迁移命令里的换行把单元截断**：`quote()` 按 C 的写法转义换行、回车、制表符（实测 Quadlet 还原 `\n`）。
6. **库暂时没起来时组件永远起不来**：组件 `Requires=` 迁移，开机或崩溃重启时迁移失败，组件以“依赖失败”结束，`Restart=` 不重试。
   改成迁移留存（`RemainAfterExit=yes`）、组件 `Wants=` + `After=` 迁移：`up` 仍单独先跑迁移把关；开机时迁移失败不挡组件；组件崩溃重启不重跑迁移。
   实测：`up` 的 `restart` 迁移不连带组件、崩溃重启不重跑、迁移失败组件照跑、开机迁移失败组件照起。`--job-mode=ignore-requirements` 因此不再需要。
7. **第一次拉大镜像超过启动时限**：`up` 先把缺的镜像拉好（`QuadletUnits.Images`）。
8. **停一个没加载的单元以 5 退出，up 与 down 永远卡住**：只停 `LoadState=loaded` 的。
9. **换 CLI 语言重启全部组件**：文件头与 `Description=` 改成固定英文。
10. **换成 `target: docker` 后单元还在**：Docker 的 compose 引擎也卸下它们。有根与无根是两个目录，`down`/`status` 要用跑 `up` 的那个用户——写进排障页。
    换成 `k8s` 不卸（compose ↔ k8s 今天也不停本机的容器，部署目标换到别处不等于这台机器上的要停）。
11. **`down` 半路失败**：删网络失败时 secret 也没删。改成能做的都做完、报第一个错；`up` 删 compose 留下的容器放到迁移成功之后。

审查中修 #3、#6 时顺带查出一个本轮引入的缺陷：重写 `Up` 时建 secret 传了 `nil` 作为标准输入，单元测试抓到了。
