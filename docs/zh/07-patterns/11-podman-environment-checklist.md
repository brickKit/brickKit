# Podman 在 Linux 上：一份环境检查清单

> **这现在是一个真实的 BrickKit 部署选项，只有一个前提条件。** `override.yaml` 的
> `target: podman` 跑的是一个真正的 `engine.Engine` 实现——`up`、`down`、`status` 全都能用。
> 这篇文档现在讲的是那唯一的环境前提：rootless Podman 的 `down` 需要 AppArmor 允许它的
> `pasta` 辅助进程收到能干净拆掉网络的信号，而大多数 Ubuntu/Debian 默认会拦下这个信号。
> 设 `target: podman` 之前先跑一遍这篇的检查清单；如果 `brickkit down` 依然报出这篇描述的
> 那个具体错误，下面的修法就是该用的那个。

## 问题

rootless Podman（不用 root 权限运行）依赖一个叫 `pasta` 的用户态辅助进程，来让容器能访问网络。
停止或删除容器时，Podman 需要给 `pasta` 发一个 `SIGTERM`，才能把这次部署的网桥、IP 分配、
nftables 规则干净地拆掉。

在 Ubuntu 上（至少到 26.04 为止实测确认），AppArmor 默认的安全策略会拦截 `pasta` 接收这个信号：

```
$ podman rm -f some-container
Error: removing container ...: 1 error occurred:
	* rootless netns: kill network process: permission denied
```

**这在受影响的机器上是每次都会复现的，不是偶发的。** `up`、`status`、正常的业务请求都完全
正常——只有停止/删除这一步会失败。这个失败模式比听起来更麻烦：容器本身通常已经没了（Podman
会退回用 `SIGKILL`），但网络资源未必真的释放掉了，而命令自己的退出码却是失败的——很容易被
误认成"这次部署根本没停下来"。

这是发行版打包 AppArmor 策略时的缺口，不是 Podman 自己的 bug，也不是某台机器独有的巧合——
Podman 上游有完全相同的报告（[containers/podman#27372](https://github.com/containers/podman/issues/27372)），
维护者自己的结论是"看起来不是我们的问题，是发行版的安全策略没配对"。

## 检查自己是否受影响

```bash
scripts/podman/check-environment.sh
```

这个脚本基本是只读的，唯一的例外是：为了拿到一个真实的答案，它会起一个用完即删的测试容器
（自动清理）。光看配置文件判断不出 AppArmor 的拦截到底会不会真的触发——这取决于内核里当前
加载的策略，不是磁盘上写了什么。

## 修复方法

```bash
scripts/podman/fix-apparmor.sh
```

**跑之前先读一遍脚本——它是破坏性的。** 它会把 Podman 重置成一个干净的状态：删除你已有的
Podman 容器/镜像/卷，重装 `podman` 和 `apparmor-utils` 这两个包，重新生成基础配置。只在
Ubuntu/Debian（apt 系）上验证过。不会碰 Docker，也不会碰 `/etc/cni`。

脚本里其实用了两条独立的技术路径，从两个不同的角度打向同一个问题。脚本两条都用了，所以下面
说的是"这个组合我们验证过能用"，不是"随便哪一条单独用就够"——我们从没把两条分开单独测过。

**1. 删掉那份过期的 `podman` profile——这是 Debian 官方的诊断结论。** Debian 追查过一模一样的
报错签名（[Debian #1100135](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1100135)），查到
一个更深一层的原因：Ubuntu/Debian 给 `podman` 二进制挂了一个摆设性质的占位 AppArmor profile
（`/etc/apparmor.d/podman`，`flags=(unconfined)`——本身什么都不限制，唯一作用是给这个进程挂个
名字，不让它显示成"unconfined"）。但正是这个挂名触发了 AppArmor 的跨 profile 信号仲裁——
`pasta` 的 profile 里从没写过"允许来自一个叫 podman 的东西发来的信号"，于是 `podman` 一旦带上
这个标签，信号就被拒绝了。Debian 自己的修法是直接把这份占位 profile 删掉——反正它本来就什么
都没限制，删了也不损失什么。脚本做的是同一件事，但有一个细节很关键：`apparmor_parser -R <路径>`
必须先**读到**这个文件，才知道要从内核卸载哪个 profile。如果先删文件、再执行 `-R`，这条命令
读到的是一个不存在的文件，内核里那份旧 profile 其实还留着——这个坑很容易踩，而且踩中之后这条
修法会悄悄不生效，不会有任何醒目的提示。脚本的做法是先写一份内容极简、能被正常解析的新文件，
卸载**这一份**，卸载成功之后才删掉文件。

**2. 把 `pasta` 本身切成 complain 模式。** 把其它清理/重装步骤都做完之后，这一步才是那一行：

```bash
sudo aa-complain /usr/bin/pasta
```

`aa-complain` 是 Ubuntu/Debian 官方提供的标准工具，专门用来把某一个 AppArmor profile 切换成
**complain 模式**（只记录违规、不强制拦截），不需要手动编辑 profile 文件（这条路很容易出错：
`passt` 和 `pasta` 是同一个包里两个名字极像、完全独立的二进制，各自有自己的 profile 文件）。

**这个取舍需要说清楚：** complain 模式意味着 AppArmor 不再对 `pasta` 这一个进程做任何强制
限制，只是记录它做了什么。系统其它部分不受影响，但这一个进程从此只被监控、不再被限制。这是
"精确诊断出到底缺哪条信号权限、只放行那一条"和"用标准工具、接受这一个进程不再被强制限制"
之间的取舍。

脚本里还顺手修了两个跟上面这个信号问题**完全无关**、但对 `brickkit up` 这种非交互场景同样
致命的坑：

- **`registries.conf`**：没有配置 `unqualified-search-registries`，拉一个没写仓库前缀的镜像
  （`alpine:latest`——BrickKit 生成的 compose 文件里就是这种写法）要么直接失败，要么弹出一个
  交互式的"你想从哪个仓库拉"的选择——这会让非交互式的 `brickkit up` 卡住不动。
- **`policy.json`**：没有签名校验策略，Podman 会直接拒绝拉取任何镜像。脚本设成了
  `insecureAcceptAnything`，关闭签名校验——这是把 Podman 的默认行为拉平到 Docker 现在的默认
  水平（Docker Content Trust 默认也是关闭的），不是让它比 Docker 更不安全。

脚本里还会启用 `podman.socket`。这条跟 AppArmor 完全无关，而且卡得更早：`podman compose` 走的
是跟 Docker 共用的同一个 `docker-compose` 二进制，这个二进制要通过一个 Docker-API 兼容的 socket
跟 Podman 通信。这个 socket 默认不启动——没有它，`podman compose up`/`down` 会直接报
"failed to connect to the docker API … no such file or directory"，连上面那个 AppArmor 的坑
都还没轮到它发作。

```bash
systemctl --user enable --now podman.socket
```

## 这份脚本不覆盖的一个坑

如果你是从一个 snap 打包的应用启动的终端里测试（VS Code 用 snap 安装是最常见的情况），可能
会先遇到一个完全无关的报错：

```
Error: database static dir "…/snap/code/254/…/containers/storage/libpod" does not match
our static dir "…/snap/code/264/…/containers/storage/libpod": database configuration mismatch
```

snap 会把 `$XDG_DATA_HOME` 重定向到一个跟版本号绑定的路径（`~/snap/<应用>/<revision>/…`）；
Podman 把存储路径记进了数据库，revision 号一变就对不上，直接拒绝启动。`check-environment.sh`
会帮你标出这个问题。修法是改 shell 的启动文件，脚本本身不处理这个：

```bash
export XDG_DATA_HOME="$HOME/.local/share"
export XDG_CONFIG_HOME="$HOME/.config"
```
