# 2. 平台是怎么决定谁跑起来的

第 1 篇跑的是一个没有任何依赖的单独组件。这一篇加一个真正依赖第一个组件的第二个组件，看平台拿到这样一个项目之后到底做了什么：注入了什么、一个缺失的弱依赖在实践中长什么样、以及开始故意关掉某些组件之后会发生什么。下面每一条命令、每一段输出都是真实的，来自 [`demo/hello`](../../../tests/components/demo-hello/) 和 [`demo/caller`](../../../tests/components/demo-caller/)——后者正是为了演练依赖注入而专门造的第二个测试夹具。

## 搭一个两组件的项目

```bash
mkdir hello-world && cd hello-world
brickkit init hello-world --no-skills

mkdir -p components/demo/hello components/demo/caller
cp ../tests/components/demo-hello/component.yaml  components/demo/hello/
cp ../tests/components/demo-hello/openapi.json    components/demo/hello/
cp ../tests/components/demo-caller/component.yaml components/demo/caller/
cp ../tests/components/demo-caller/openapi.json   components/demo/caller/

brickkit add --local
```

```
🔍 从本地安装源 local-dev 扫到 2 个组件
📦 添加 demo/caller@1.0.0
   ├── Manifest ✅
   ├── 依赖 demo/hello@1.0.0 ✅ 已拉取（artifacts 1 个文件）
   └── artifacts ✅（1 个文件）
⚠️ 警告：弱依赖缺失：demo/bus@1.0.0
   影响组件：demo/caller@1.0.0
   原因：该组件在所有安装源中均未找到
   影响：该组件的环境变量 DEMO_BUS_ENDPOINT 不会被注入
📦 添加 demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅（1 个文件）
✅ 已写入 brickkit.yaml（2 个组件）
```

还什么都没跑起来，就已经发生了两件事。`add --local` 扫描本地安装源时发现 `demo/caller` 依赖 `demo/hello`，顺手把它也拉了进来——你只要了一个组件，得到的是它整棵依赖子树。`demo/caller` 还**弱依赖**着 `demo/bus@1.0.0`，而这个组件在这个项目的任何安装源里都不存在——CLI 当场就警告了，不等到 `up` 才说。

## 什么都还没启动之前，看到的完整画面

```bash
brickkit up --dry-run
```

```
⚠️ 警告：弱依赖缺失：demo/bus@1.0.0
   影响组件：demo/caller@1.0.0
   ...
📋 组件状态计算：
   ✅ demo/hello@1.0.0   启动（demo/caller 需要）
   ✅ demo/caller@1.0.0  启动（顶层）

⚠️ 警告：资源依赖未满足（--dry-run 不阻断）
   demo/caller@1.0.0：需要 kind: database、engine: postgresql（brickkit.yaml 的 resources 中未声明）
📋 启动顺序（拓扑排序）：
   1. demo-hello-1-0-0   无依赖
   2. demo-caller-1-0-0  ← 依赖 1

可独立启动：demo-hello-1-0-0（无依赖）
最长依赖链（2 层）：demo-hello-1-0-0 → demo-caller-1-0-0

依赖图：
   demo/caller@1.0.0 → demo/hello@1.0.0
                     → demo/bus@1.0.0（弱，未安装）
📄 已生成：.brickkit/generated/docker-compose.yaml

🔧 启动前会执行的数据库迁移（失败则该组件不会启动）：
   demo/caller@1.0.0  /app/caller migrate
```

从上往下读：`demo/hello` 会启动，是因为 `demo/caller` 需要它，不是因为它自己是顶层——真正的顶层是 `demo/caller`，没有别的组件依赖它。启动顺序把 `demo/hello` 排在前面，因为 `demo/caller` 的强依赖必须先起来。依赖图里 `demo/bus` 这条边同时标了"弱"**和**"未安装"——这跟一个弱依赖只是这次部署没装上，是两种不同的标记，这样你能分清"这个组件哪儿都不存在"和"存在，只是这里没装"。`demo/caller` 还声明了一个这个项目还没绑定的数据库资源——`--dry-run` 对此只警告（资源绑定是独立的话题，这个系列后面会讲）；这里真跑 `up` 会在绑定好之前直接拒绝启动。

## 关掉一个强依赖

给 `brickkit.yaml` 里 `demo/hello` 那条加上 `enabled: false`，再跑一次 `--dry-run`：

```
📋 组件状态计算：
   ⬜ demo/hello@1.0.0   显式禁用（enabled: false）
   ⬜ demo/caller@1.0.0  不启动（强依赖 demo/hello 不启动）

📋 本次没有组件会启动
   顶层组件（没有别的组件依赖它们）这次都不跑：
      demo/caller@1.0.0  不启动（强依赖 demo/hello 不启动）
   顶层自己都没被关掉——要放开的是上面那行理由里点名的组件
```

`demo/caller` 自己的 `enabled` 字段根本没动过——单看它自己，作为顶层组件，默认就该跑。但它照样停了，因为它的**强依赖**被强制关掉了，"依赖方跟着不启动"（AGENTS.zh.md §5.4）不管依赖方自己的 `enabled` 写了什么都照样生效。最后一行是 CLI 主动指出该改哪儿：该动的不是 `demo/caller`——它这里的行为是结果，不是原因——该动的是 `demo/hello`。

## 两头都钉死是冲突，不是一个可以自动决定的事

保持 `demo/hello` 禁用，同时给 `demo/caller` 加上 `enabled: true`——坚持它无论如何都要跑：

```
❌ 错误：强依赖 demo/hello 被禁用
   组件：demo/caller@1.0.0（enabled: true，已钉住）
   依赖链：demo/caller → demo/hello
   被禁用的组件：demo/hello@1.0.0
   建议：
   1. 在 brickkit.yaml 中移除 demo/hello 的 enabled: false
   2. 或去掉 demo/caller 的 enabled: true，让它随上层一起不启动
```

两条显式写下的指令——"`demo/hello` 永远不跑"和"`demo/caller` 永远要跑"——一旦 `demo/caller` 真的需要 `demo/hello`，就直接互相矛盾了，平台不会悄悄替你选一个赢家（AGENTS.zh.md §5.4）。这跟上一节故意不一样：一个**没写**的 `enabled` 会安静地跟着走；一个**写了**却互相矛盾的，是硬性停止，因为现在摆在配置里的是两条互相冲突的显式意图，不再是一条。

---

这个项目只是一条直线似的两组件链条。真实的依赖图里会有菱形（同一个组件被不止一条路径依赖），循环依赖有时候也是合法的——[依赖解析与启动顺序](../06-architecture/02-dependency-resolution.md)用真实例子把这两种都讲透了，还讲了为什么八个组件启动不等于八步串行。下一篇：[本地调试一个组件](03-local-debugging.md)——这两个组件里的一个挂着断点跑在你自己的机器上，另一个照常运行，感觉不到对面到底是不是一个容器。
