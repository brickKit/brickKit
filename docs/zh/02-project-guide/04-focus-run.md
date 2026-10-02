# 在项目里就地开发

项目大了以后，你很少孤立地改一个组件。你在改 `demo/caller`，想看它连着它真正要调的 `demo/hello` 跑起来，而另外四十个组件完全可以不启动。
这一篇讲的就是**在项目里**做这件事：站在一个组件的目录里敲 `brickkit up`，项目就只跑这一个组件——从它的源码跑——再加上它需要的那些。

这叫**焦点运行**。你正在改的组件是*焦点*；焦点用不到的组件一律不启动。

## 焦点运行还是工作台

开发一个组件时，有两种方式把它跑起来，回答的是两个不同的问题：

| | 焦点运行（本篇） | 工作台（[分形的本地开发](../03-component-guide/05-local-dev-fractal.md)） |
| --- | --- | --- |
| 在哪里干活 | 在项目里，`components/<scope>/<name>/` 下 | 在组件自己的仓库里，那里有它自己的 `brickkit.yaml` |
| 跑什么 | 焦点和它需要的组件，**用的是项目里的版本** | 工作台里声明的那些——常常是组件本身加几个替身 |
| 准备工作 | 没有：项目已经知道一切 | 一个属于它自己的小项目，写一次 |
| 适合 | "我这处改动在*这个*系统里行不行？" | 开发一个被很多项目使用的组件，按它自己的节奏 |

组件就在某个项目的 `components/` 里时，先用焦点运行。工作台留给那些在一个项目之外还有自己生命的组件。

## 在组件目录里 `up`

```bash
cd components/demo/caller
brickkit up
```

```text
📁 项目：../../..（my-shop）
✅ 本地模式已开启：命令现在读取 deploy.local.yaml
   deploy.local.yaml 从 deploy.yaml 复制而来——随意修改，它不会被提交
🎯 焦点：demo/caller（已写进 deploy.local.yaml；brickkit up --all 恢复运行全部组件）
本地模式已开启：使用 deploy.local.yaml（brickkit local off 切回 deploy.yaml）
🚀 启动项目 my-shop（target: docker）
📋 组件状态计算：
   ✅ demo/bus@1.0.0     启动（demo/caller 需要）
   ✅ demo/hello@1.0.0   启动（demo/caller 需要）
   ✅ demo/caller@1.0.0  启动（焦点）
```

逐行看：

- **`📁 项目`**——这个目录里没有 `brickkit.yaml`，于是 `brickkit` 像 `git` 一样往上找，在上三层找到了项目。所有项目命令在任何子目录里都能用；
  它打印的路径都相对你所在的目录。
- **本地模式**——焦点是个人设置，所以写在你的个人部署文件里。本地模式原来没开的话，`up` 先把它打开（和 `brickkit local on` 一样）。
- **`🎯 焦点`**——你所在的目录属于 `demo/caller`，它就是焦点。
- **原因**——每个组件都说了自己为什么启动：`（焦点）`，或者 `（demo/caller 需要）`。

焦点**从源码跑**，是你机器上的一个进程（相当于 `mode: local`）；它需要的组件跑在容器里，并且各自在你的机器上开了端口，好让你的进程连得到：

```text
🐳 正在启动（docker）...
   demo-bus-1-0-0               running（healthy）
   demo-hello-1-0-0             running（healthy）
✅ 全部组件已启动（2 个）
```

```text
正在启动 1 个本地组件——按 Ctrl+C 停止
demo-caller-1-0-0  已监听端口 8080
```

这个进程在前台跑；Ctrl+C 停掉它，容器照常运行。要打断点，就在 `deploy.local.yaml` 里给焦点写上 `mode: debug`，再从 IDE 里启动它——
见[本地调试工作流](03-local-debug-workflow.md)。你写的 `mode: debug` 焦点会保留。

## 焦点记在哪里

`deploy.local.yaml` 里的一行：

```yaml
# deploy.local.yaml
target: docker # docker | podman | k8s
focus: demo/caller

components:
  - id: demo/bus
  - id: demo/hello
  - id: demo/caller
```

改掉它之前它一直在：下一次 `up` 还是跑这个焦点，除非你在另一个组件的目录里运行——那会把焦点换过去。团队的 `deploy.yaml` 一个字节都不动。
`up`、`status`、`down`、`lint`、`build` 都会提醒一句（`sync` 不管焦点，见 [下文](#别的命令怎么对待焦点)）：

```text
🎯 焦点：demo/caller
```

## `--focus` 与 `--all`

在项目的任何位置，`--focus` 不用换目录就能设焦点：

```bash
brickkit up --focus demo/hello
```

```text
🎯 焦点：demo/hello（已写进 deploy.local.yaml；brickkit up --all 恢复运行全部组件）
本地模式已开启：使用 deploy.local.yaml（brickkit local off 切回 deploy.yaml）
🚀 启动项目 my-shop（target: docker）
📋 组件状态计算：
   ⬜ demo/bus@1.0.0     不启动（焦点之外）
   ✅ demo/hello@1.0.0   启动（焦点）
   ⬜ demo/caller@1.0.0  不启动（焦点之外）
```

`demo/hello` 什么都不需要，所以只有它自己跑。`--all` 去掉焦点，所有组件重新都跑：

```bash
brickkit up --all
```

```text
📋 组件状态计算：
   ✅ demo/bus@1.0.0     启动（demo/caller 需要）
   ✅ demo/hello@1.0.0   启动（demo/caller 需要）
   ✅ demo/caller@1.0.0  启动（顶层）
```

`--focus` 和 `--all` 只有 `up` 有，也不能和 `-f`、`--no-local` 一起用：那两个的意思是"别读我的个人文件"，而焦点就写在个人文件里。

哪些组件启动，算法和平时一样，只是起点不同：没有焦点时，所有顶层组件启动；有焦点时，只有焦点，加上 `mode` 写明了总要运行的组件
（`enabled`、`local`、`debug`）。它们需要的组件照常跟着启动。外壳也算在内：焦点是外壳时，它带着成员一起跑（`启动（由 … 承载）`）；
焦点需要的组件由某个外壳承载时，那个外壳跟着启动来承载它（`启动（承载 …）`），而不是让成员单独跑。焦点不能是一个 `mode: disable`
的组件——那自相矛盾，`up` 会拒绝，而且不改任何文件。

## 别的命令怎么对待焦点

| 命令 | 有焦点时 |
| --- | --- |
| `status`、`down`、`lint`、`build` | 打出 `🎯 焦点` 那一行。`down` 停的是整个项目，有没有焦点都一样 |
| `lint` | 还会检查焦点有没有源码可以跑 |
| `sync` | **不看焦点**：它保留没有焦点时项目要跑的全部组件的源码，所以换焦点从来不会让目录搬来搬去 |
| `local refresh` | 把焦点列为你的本地修改之一，方便你把它放回新文件 |
| `remove` | 移除焦点组件时，焦点跟着一起去掉，并说一句 |
| `graph`、`deps` | 不受影响——它们读 `deploy.yaml` |
| 不带参数的 `build`、`deps`、`lint` | 用你所在目录的那个组件（`lint --all` 查整个项目） |

焦点设在 `demo/hello` 上时，`sync` 照样三个都留着：

```text
📂 工作区整理：
   ✅ components/demo/bus/                 活跃
   ✅ components/demo/caller/              活跃
   ✅ components/demo/hello/               活跃
✅ 工作区整理完成（3 个活跃，0 个归档，0 个激活）
```

`local refresh` 重新复制 `deploy.yaml`，焦点就在它提醒你带过去的东西里：

```text
✅ 已从 deploy.yaml 生成最新的 deploy.local.yaml。旧文件已备份至 deploy.local.yaml.bak。
ℹ️ 旧文件中有 1 处本地修改，请把仍然需要的手动合并到新的 deploy.local.yaml 中：
   - [deploy] focus: demo/hello（当前未设置）
```

焦点运行要在你的机器上起一个进程、让别的组件连过来，所以只适用于 `target: docker` 和 `podman`。`target: k8s` 时什么都不写，`up` 直接停下：

```text
❌ 错误：deploy.local.yaml 校验失败
   文件：deploy.local.yaml
   focus：焦点运行要在你的机器上起进程，集群够不着；改用 target docker 或 podman
   建议：完整字段说明：docs/zh/11-reference/03-deploy-yaml-schema.md（英文版把 zh 换成 en）
```

## 版本往前走

焦点跑的是它目录里的代码，所以那份代码必须是项目声明的版本。你在 `demo/hello` 的 `component.yaml` 里把版本改成 `1.1.0`、而
`brickkit.yaml` 里还是 `1.0.0` 时，`up` 宁可停下，也不去跑一个项目不认识的版本：

```text
❌ 从本地仓库运行的代码与这次运行的版本对不上
   文件：deploy.local.yaml
   components[1]：demo/hello@1.0.0 从本地仓库 components/demo/hello 运行，仓库里却是 1.1.0
   建议：
   1. 要跑仓库里的版本：brickkit upgrade demo/hello@1.1.0
   2. 要跑项目里的版本：git -C components/demo/hello checkout 1.0.0
```

`brickkit upgrade demo/hello@1.1.0` 把整个项目换到新版本——连同配置迁移（见[升级与配置迁移](07-upgrade-and-migration.md)）——还要求 `1.0.0`
的组件则把那个版本并排留着。项目一次一个显式的 `upgrade` 往前走；不会因为某个目录变了就跟着变。

改完需求、发布之前的测试也是这么走的：在组件的 `component.yaml` 里升版本，`brickkit upgrade` 把项目换到这个版本，然后跑起来——从源码跑用焦点运行
（或 `mode: local`），跑镜像就 `brickkit build <id>` 再 `brickkit up`。测试期间这个还没发布的版本想改几次改几次。`brickkit release` 之前，
这个版本只在你的 `components/` 里：同事拉到写着它的 `brickkit.yaml`，会得到 `COMPONENT_NOT_FOUND`。所以先发布组件，再提交项目的升级。
版本一旦发布，内容就定了——再改，不管是代码还是文档，就是下一个版本。

## 只有一个 `components/`

组件源码只放一处：项目的 `components/`。组件仓库本身是工作台时，它可以有自己的 `components/`——那份东西一旦出现在项目里，同一个组件就可能有两份，
谁也说不清跑的是哪一份。`up`、`lint`、`sync` 都会拒绝：

```text
❌ 错误：组件源码嵌在另一个组件的目录里
   components/demo/caller/components/demo/hello：demo/hello，在 demo/caller 里面；项目的 components/ 里也有 demo/hello
   挪走或删掉之前：它不是一个 Git 仓库——这些文件没有别的副本
   建议：
   1. 项目的 components/ 里已经有 demo/hello（components/demo/hello）：把还要的改动搬过去，再 rm -rf components/demo/caller/components/demo/hello
   2. 组件源码只放一处：项目的 components/。嵌套的那份请自己挪走或删掉——BrickKit 不替你挪
```

BrickKit 从不替你挪动或删除嵌套的那份：里面可能有别处都没有的改动——"挪走或删掉之前"那一行就说了有没有。建议给出了每一份的命令：
项目里已经有这个组件时删掉它（先把还要的改动搬过去），项目里没有时把它挪进项目的 `components/`。同样的道理，`brickkit add --repo`
在一个嵌在别的项目 `components/` 里的工作台中会拒绝运行：它会再克隆出一份。

## Git submodule

BrickKit 从不拉取 git submodule。`add --repo` 克隆时不带它们，并点名跳过了哪些；`build` 在组件源码里有空目录的 submodule 时给出警告。
真的需要时自己 `git submodule update --init`——更好的做法是让组件发布镜像，构建它就什么额外的东西都不需要。

`up` 的全部参数见[命令参考](../07-cli-reference/README.md#brickkit-up)。
