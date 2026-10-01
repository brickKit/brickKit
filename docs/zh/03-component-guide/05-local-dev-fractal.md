# 分形的本地开发

## 组件本身也可以是一个项目

`demo/quote` 依赖 `demo/hello`。写代码时你想让它连着一个真的 `demo/hello` 跑，而不是对着文档想象。
BrickKit 的做法是**分形**的：一个组件在开发时，它自己的仓库就是一个完整的 BrickKit 项目——有 `brickkit.yaml`、部署文件、`config/`，
里面列着它的依赖。这份项目只属于作者，叫**本地联调工作台**。

使用方完全看不到它：别人 `add demo/quote` 时，CLI 只读你发布的那个 tag 里的 `component.yaml` 和 `BRICKKIT.md`（连同译本），
按 `component.yaml` 的 `dependencies` 在**他自己的项目里**解析依赖树。

## 在组件仓库里建工作台

在组件仓库根目录执行不带参数的 `init`（补全式：`component.yaml`、源码这些已有的文件一个都不动，只补上缺的）：

```bash
cd demo-quote
brickkit init
```

```text
当前目录已有文件，brickkit init 将：
   ✅ 创建  brickkit.yaml
   ✅ 创建  deploy.yaml
   ✅ 创建  config/vars.yaml
   ✅ 创建  config/.gitkeep
   ✅ 创建  .gitignore
```

组件仓库里不建项目才有的 `components/`、`shell/`，也不替你声明安装源——依赖从哪来由你决定：

```yaml
# brickkit.yaml —— 这个组件的本地联调工作台：联调时要跑的组件（它的依赖，也可以加上它自己）。
# 它与发布无关：brickkit release 只读 component.yaml。
project: demo-quote

# 这个组件的依赖从哪里取——加上它需要的安装源
sources:
  - name: company-git
    type: git
    baseUrl: https://git.example.com/components/

components: []
```

## 把依赖加进来

```bash
brickkit add demo/hello@1.1.0
```

```text
➕ 加入 demo/hello@1.1.0
   ✅ demo/hello@1.1.0
📝 已写：brickkit.yaml, deploy.yaml
📝 配置骨架：config/demo-hello.yaml
📦 产物：1 个文件，在 .brickkit/artifacts/
```

这时 `brickkit up` 就能在本地把依赖跑起来。

## 工作台的 `AGENTS.md`

工作台只有**一份** `AGENTS.md`：组件自己的那份。`init` 不动它的各节（代码地图、构建与测试、设计取舍、易错点、改代码前自查），
只改写末尾由 CLI 维护的那一段。在工作台里，这一段装两样东西：原来就有的组件规则，加上工作台的组件表——`add` 依赖时跟着填，
写明每个依赖的文档缓存在哪（`.brickkit/manifests/<id>/<version>/`）。

这样在仓库里干活的 AI 在一份文件里就能读到：这个组件怎么构建、哪些东西不能弄坏，以及它连着哪些依赖跑、去哪读它们的文档。
不会再有第二份带"项目概述""项目约定"的项目式 `AGENTS.md`：工作台不是给别人装配的项目。

## 把组件自己也加进来

依赖在容器里跑着，你的组件怎么拿到它们的地址？把组件自己也写进工作台——来源指向仓库根目录（`source.type: local`、`path: .`），
并让它以本机进程运行：

```yaml
# brickkit.yaml
components:
  - id: demo/hello
    version: 1.1.0
  - id: demo/quote
    version: 0.1.0          # 与 component.yaml 的 metadata.version 一致
    source:
      type: local
      path: .
```

```yaml
# deploy.yaml
components:
  - id: demo/hello
  - id: demo/quote
    mode: local
```

`mode: local` 让 BrickKit 从源码目录认出启动命令、自己启动并看护这个进程；依赖的地址按本机可达的方式注入给它：

```bash
brickkit up
```

```text
📋 组件状态计算：
   ✅ demo/hello@1.1.0  启动（demo/quote 需要）
   ✅ demo/quote@0.1.0  启动（顶层）
```

```text
🐳 正在启动（docker）...
   demo-hello-1-1-0             running（healthy）
✅ 全部组件已启动（1 个）

💡 查看状态：brickkit status
   查看日志：docker compose -p brickkit-demo-quote logs -f

正在启动 1 个本地组件——按 Ctrl+C 停止
demo-quote-0-1-0 | 2026/09/29 13:41:29 demo/quote listening on :8080
demo-quote-0-1-0  已监听端口 8080
```

```bash
curl http://localhost:8080/api/v1/quote
```

```json
{"greeting":"Hi","quote":"今日名言：能跑起来的比完美的好"}
```

问候语"Hi"来自容器里的 `demo/hello@1.1.0`。改了代码，`Ctrl+C` 再 `brickkit up` 就是新代码。

几点说明：

- **想打断点，用 `mode: debug`。** 平台不启动它，只生成一份环境变量文件，你在 IDE 里加载它、自己启动（见 [本地调试](../02-project-guide/03-local-debug-workflow.md)）。
  `mode: debug` 是你个人的事，写在 `deploy.local.yaml`（`brickkit local on`）里。
- **`mode: local` 下平台注入 `PORT`**：它为这个进程选定的本机端口（优先用 `deployment.port`）。代码读 `PORT`、读不到再用缺省端口，就不会和别的进程抢。
- **这份工作台要不要提交进组件仓库**，由你定：提交了，协作者 clone 下来就能一条 `brickkit up` 跑起来；它不会影响发布，也不会被使用方看到。

## 工作台还是焦点运行

开发一个组件时，工作台不是把它跑起来的唯一办法。组件本来就在某个项目的 `components/` 里时，在它的目录里 `brickkit up` 就是一次**焦点运行**：
这个组件从源码跑，加上它需要的组件，用的是项目里的版本——不用写任何工作台。见[在项目里就地开发](../02-project-guide/04-focus-run.md)。

| 用 | 什么时候 |
| --- | --- |
| 焦点运行 | 组件属于一个项目，要回答的是"我这处改动在这个系统里行不行？" |
| 工作台 | 组件在任何单个项目之外都有自己的生命：很多项目用它，它按自己的节奏开发 |

两者不混用。在工作台里，命令用的是工作台——最近的 `brickkit.yaml` 说了算，它上面的一概不看。而项目里的工作台不能再带一个装满副本的
`components/`：组件源码只放一处，就是项目的 `components/`。`up`、`lint`、`sync` 遇到嵌在别的组件目录里的组件会拒绝；
`add --repo` 也不会往一个嵌在别的项目里的工作台里克隆。

## 一个仓库、两种身份

组件仓库里同时有 `component.yaml` 和 `brickkit.yaml` 时，CLI 这样认：

| 命令 | 把这个目录当成 | 读什么 |
| --- | --- | --- |
| `up`、`add`、`down`、`status`…… | 项目（你的工作台） | `brickkit.yaml`、部署文件、`config/` |
| `release` | 组件 | 只读 `component.yaml`，工作台里的一切都不参与 |
| `lint` | 项目 | 工作台的三层文件，外加仓库根的 `component.yaml` 和组件的文档 |

这保证了发布的纯粹：你在工作台里临时加的组件、调的配置，不会混进发布出去的版本。

## 项目里有很多本地组件时：`add --local --init`

一个项目的 `components/` 与 `shell/` 下有好几个你们自己写的组件时，不必一个个进去 `init`：

```bash
brickkit add --local --init
```

```text
   🧰 已在 components/demo/widget 建好工作台（加入 0 个依赖）
   🧰 已在 shell/erp/shell 建好工作台（加入 0 个依赖）
   💡 把每个组件仓库里新生成的工作台文件提交进去——组件目录里有未提交的文件时，brickkit release 会拒绝发布
➕ 加入 demo/widget@0.1.0, erp/shell@0.1.0
   ✅ demo/widget@0.1.0（在外壳 erp/shell 里）
   ✅ erp/shell@0.1.0
📝 已写：brickkit.yaml, deploy.yaml
```

它扫描所有本地源，给每个还没有 `brickkit.yaml` 的组件建好工作台，把**各自** `component.yaml` 里的依赖加进各自的工作台，
安装源继承外层项目（路径换算成相对那个目录）；然后照常把这些组件加进外层项目。之后 `cd components/demo/widget && brickkit up`
就能单独拉起这一个组件的依赖树。
