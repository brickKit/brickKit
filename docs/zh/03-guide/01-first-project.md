# 1. 把一个项目跑起来

这篇走一遍从空目录到一个真正跑起来、能访问到的容器之间最短的真实路径：初始化项目、加一个组件、启动它、用 HTTP 跟它说上话、改它的配置、再把它停掉。下面每一条命令、每一段输出都是真实的——对着 [`demo/hello`](../../../tests/components/demo-hello/) 跑出来的，这是本仓库自带的一个小 HTTP 组件，专门为了让这类教程不用依赖任何外部镜像仓库或市场。

**前置条件：** 已经构建好 BrickKit CLI（`make build-cli`，或者装了一个正式发布版），Docker 在跑。

## 构建组件的镜像

真实使用场景里，你 `brickkit add` 一个组件的时候，它的镜像早就已经在某个仓库里了——平台从来不会替你构建镜像（AGENTS.zh.md §6）。这里因为 `demo/hello` 是这个仓库自带的测试夹具，没有发布到任何地方，所以先自己构建一次：

```bash
docker build -t brickkit-demo/hello:1.0.0 tests/components/demo-hello
```

## 初始化项目

```bash
mkdir hello-world && cd hello-world
brickkit init hello-world
```

`<项目名称>` 这个参数**不是**要创建的目录——跟一些工具的 `init <name>` 会顺手建一个文件夹不一样，BrickKit 的 `init` 永远是在**当前目录**里操作，`<项目名称>` 只是用来设置生成出来的 `brickkit.yaml` 里的 `project:` 字段（后面会用在 Docker 网络名和 K8s 命名空间名上）。先自己建好这个空目录，是正确的用法，不是绕过什么限制。

```
✅ 项目已初始化：hello-world
   📁 brickkit.yaml        项目配置
   📁 components/          组件源码（已配为本地安装源 local-dev）
   📁 .brickkit/           CLI 工作目录
   📁 .claude/skills/      AI 助手技能（4 个）
   📁 AGENTS.md            AI 助手项目导读
```

`init` 顺手把 `components/` 配成了生成出的 `brickkit.yaml` 里的一个 `local` 类型安装源——下一步就是往这里放东西。

## 添加一个组件

按 `<scope>/<name>/component.yaml` 这个本地安装源要求的布局，把组件的 Manifest 和产物复制进去：

```bash
mkdir -p components/demo/hello
cp ../tests/components/demo-hello/component.yaml components/demo/hello/
cp ../tests/components/demo-hello/openapi.json components/demo/hello/
brickkit add --local
```

```
🔍 从本地安装源 local-dev 扫到 1 个组件
📦 添加 demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅（1 个文件）
✅ 已写入 brickkit.yaml（1 个组件）
```

`brickkit.yaml` 里的 `components:` 现在多了一条。这时候还什么都没跑起来——`add` 永远只写配置（AGENTS.zh.md §8）。

## 先看看会发生什么，再真正启动

```bash
brickkit up --dry-run
```

```
🚀 启动项目 hello-world（deploy.target: docker）
📋 组件状态计算：
   ✅ demo/hello@1.0.0  启动（顶层）

📋 启动顺序（拓扑排序）：
   1. demo-hello-1-0-0  无依赖

可独立启动：demo-hello-1-0-0（无依赖）
📄 已生成：.brickkit/generated/compose.yaml
```

`--dry-run` 把该算的都算了、部署文件也写出来了，但什么都不启动——你还在检查的时候，想跑多少次都安全。给 `brickkit.yaml` 里那个组件条目加上 `expose: true` 和 `exposePort: 8080`，这样它才能真的从你自己的机器上访问到（默认不暴露是一个刻意的默认值——AGENTS.zh.md §4——不是漏掉了什么）：

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    expose: true
    exposePort: 8080
```

然后真正启动它：

```bash
brickkit up
```

```
🐳 正在启动（docker）...
   demo-hello-1-0-0             running（healthy）
✅ 全部组件已启动（1 个）
```

## 跟它说上话

```bash
curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"Hello","message":"Hello, I'm demo/hello@1.0.0","version":"1.0.0"}
```

`demo/hello` 声明了一个 `configSchema` 属性 `greeting`，注入为环境变量 `GREETING`——上面这条响应就是把它原样读回来给你看。再确认一下还注入了什么：

```bash
curl http://localhost:8080/api/v1/env
```

```json
{"env":{"COMPONENT_ID":"demo/hello","COMPONENT_VERSION":"1.0.0","GREETING":"Hello"}}
```

`COMPONENT_ID` 和 `COMPONENT_VERSION` 是每个组件无条件都会拿到的两个平台级变量（AGENTS.zh.md §5.2）。

## 改配置，不碰任何代码

给同一个组件条目加一个 `config:` 块：

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    expose: true
    exposePort: 8080
    config:
      greeting: "Howdy"
```

```bash
brickkit up
curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"Howdy","message":"Howdy, I'm demo/hello@1.0.0","version":"1.0.0"}
```

再跑一次 `up` 正是让配置改动真正生效的方式——这里没有热重载机制可等（AGENTS.zh.md §9.8），而且不管改没改东西，再跑一次永远是安全的。

## 查看状态，然后停掉

```bash
brickkit status
```

```
✅ 运行中（1 个组件）
 ┌────────────┬───────┬───────────────────┬───────────────────────────────────────────────┐
 │ 组件       │ 版本  │ 状态              │ 端口                                          │
 ├────────────┼───────┼───────────────────┼───────────────────────────────────────────────┤
 │ demo/hello │ 1.0.0 │ 运行中（healthy） │ 0.0.0.0:8080->8080/tcp, [::]:8080->8080/tcp    │
 └────────────┴───────┴───────────────────┴───────────────────────────────────────────────┘
```

```bash
brickkit down
```

```
🛑 停止项目 hello-world
✅ 已停止全部组件

💡 数据卷未删除，数据库数据仍然保留
   需要彻底清理时手动执行：docker volume rm <卷名>
   重新启动：brickkit up
```

`down` 只停容器，从来不会主动删卷（AGENTS.zh.md §8）——`demo/hello` 正好没有任何数据要保留，但换成一个带真实数据库的组件，同一条命令的行为完全一样：停掉，不清空。

---

下一篇：[平台是怎么决定谁跑起来的](02-what-runs.md)——同一个项目长成好几个真实互相依赖的组件，以及开始关掉某些部分时，`mode` 到底在做什么。
