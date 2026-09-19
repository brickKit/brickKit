# 2. How the Platform Decides What Runs

Article 1 ran a single component with no dependencies. This one adds a second component that actually depends on the first, and walks through what the platform does with that: what gets injected, what a missing optional dependency looks like in practice, and what happens once you start turning components off on purpose. Every command and output below is real, from [`demo/hello`](../../../tests/components/demo-hello/) and [`demo/caller`](../../../tests/components/demo-caller/) — a second fixture built specifically to exercise dependency injection.

## Set up a two-component project

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

Two things already happened before anything is even running. `add --local` scanned the local source for `demo/caller`, found that it requires `demo/hello`, and pulled that in too — you only asked for one component and got its whole dependency subtree. And `demo/caller` also *optionally* depends on `demo/bus@1.0.0`, which doesn't exist anywhere in this project's sources — the CLI warns about it right away rather than waiting until `up`.

## The full picture, before starting anything

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

Reading it top to bottom: `demo/hello` runs because `demo/caller` needs it, not because it's top-level itself — `demo/caller` is the one nothing else depends on. The start order puts `demo/hello` first, since `demo/caller`'s required dependency has to be up before it. The dependency graph marks the `demo/bus` edge as weak *and* unresolved — a different marker than a weak edge that simply isn't installed in this particular deployment, so you can tell "doesn't exist anywhere" apart from "exists, just not here." And `demo/caller` also declares a database resource this project hasn't bound yet — `--dry-run` only warns about that (binding resources is its own topic, covered later in this series); running `up` for real here would refuse to start until it's bound.

## Turning a required dependency off

Add `enabled: false` to `demo/hello`'s entry in `brickkit.yaml` and run `--dry-run` again:

```
📋 组件状态计算：
   ⬜ demo/hello@1.0.0   显式禁用（enabled: false）
   ⬜ demo/caller@1.0.0  不启动（强依赖 demo/hello 不启动）

📋 本次没有组件会启动
   顶层组件（没有别的组件依赖它们）这次都不跑：
      demo/caller@1.0.0  不启动（强依赖 demo/hello 不启动）
   顶层自己都没被关掉——要放开的是上面那行理由里点名的组件
```

`demo/caller` never had its own `enabled` field touched — on its own, being top-level, it would run by default. It stops anyway, because its *required* dependency is force-off, and "whatever depends on it stops too" (AGENTS.md §5.4) doesn't care what the dependent's own `enabled` says. The last line is the CLI actively pointing at the fix: the thing to change isn't `demo/caller`, whose behavior here is a consequence, not a cause — it's `demo/hello`.

## Pinning both sides at once is a conflict, not a decision

Leave `demo/hello` disabled, but add `enabled: true` to `demo/caller` — insisting it must always run, no matter what:

```
❌ 错误：强依赖 demo/hello 被禁用
   组件：demo/caller@1.0.0（enabled: true，已钉住）
   依赖链：demo/caller → demo/hello
   被禁用的组件：demo/hello@1.0.0
   建议：
   1. 在 brickkit.yaml 中移除 demo/hello 的 enabled: false
   2. 或去掉 demo/caller 的 enabled: true，让它随上层一起不启动
```

Two explicit, written instructions — "`demo/hello` never runs" and "`demo/caller` always runs" — directly contradict each other once `demo/caller` actually needs `demo/hello`, and the platform refuses to silently pick a winner (AGENTS.md §5.4). This is different from the previous section on purpose: an *unwritten* `enabled` on the dependent just follows along quietly; a *written* one that conflicts is a hard stop, because now there are two explicit intents on record instead of one.

---

This project only has a straight two-component chain. Real dependency graphs have diamonds (the same component required through more than one path), and cycles are sometimes valid — [Dependency resolution and start order](../architecture/dependency-resolution.md) works through both with real examples, along with why eight components starting doesn't mean eight serial steps. Next: [Debug a component locally](03-local-debugging.md) — one of these two components running with breakpoints on your own machine, while the other keeps running exactly as if it were talking to a container.
