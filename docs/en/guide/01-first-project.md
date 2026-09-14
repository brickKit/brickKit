# 1. Get a Project Running

This walks through the shortest real path from an empty directory to a running, reachable container: initialize a project, add one component, start it, talk to it over HTTP, change its config, and tear it down. Every command and every output below is real — run against [`demo/hello`](../../../tests/components/demo-hello/), a small HTTP component that ships in this repository specifically so tutorials like this one don't depend on any external registry or marketplace.

**Prerequisites:** the BrickKit CLI built (`make build-cli`, or an installed release), and Docker running.

## Build the component's image

In real use, a component's image already exists in a registry by the time you `brickkit add` it — a Manifest's `deployment.image` (AGENTS.md §6) is always a reference to something already built, never something the CLI builds for you. Here, since `demo/hello` is this repository's own fixture rather than something published anywhere, build it once yourself:

```bash
docker build -t brickkit-demo/hello:1.0.0 tests/components/demo-hello
```

## Initialize a project

```bash
mkdir hello-world && cd hello-world
brickkit init hello-world
```

The `<name>` argument is *not* a directory to create — unlike tools where `init <name>` scaffolds a new folder for you, BrickKit's `init` always operates on the current directory, and `<name>` only sets `project:` inside the generated `brickkit.yaml` (used later for the Docker network and Kubernetes namespace names). Making the empty directory yourself first is the correct pattern, not a workaround.

```
✅ 项目已初始化：hello-world
   📁 brickkit.yaml        项目配置
   📁 components/          组件源码（已配为本地安装源 local-dev）
   📁 .brickkit/           CLI 工作目录
   📁 .claude/skills/      AI 助手技能（4 个）
   📁 AGENTS.md            AI 助手项目导读
```

`init` also wired up `components/` as a `local`-type install source in the generated `brickkit.yaml` — that's where the next step looks.

## Add a component

Copy the component's Manifest and artifacts into the local source, matching the `<scope>/<name>/component.yaml` layout a local source expects:

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

`brickkit.yaml` now has one entry under `components:`. Nothing is running yet — `add` only ever writes configuration (AGENTS.md §8).

## See what would happen, then actually start it

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
📄 已生成：.brickkit/generated/docker-compose.yaml
```

`--dry-run` computes everything and writes the deployment file, but starts nothing — safe to run as often as you like while you're still checking things over. Add `expose: true` and `exposePort: 8080` to the component's entry in `brickkit.yaml` so it's actually reachable from your machine (not exposed by default is a deliberate default — AGENTS.md §4 — not an oversight):

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    expose: true
    exposePort: 8080
```

Then start it for real:

```bash
brickkit up
```

```
🐳 正在启动（docker）...
   demo-hello-1-0-0             running（healthy）
✅ 全部组件已启动（1 个）
```

## Talk to it

```bash
curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"}
```

`demo/hello` declares one `configSchema` property, `greeting`, injected as the environment variable `GREETING` — and the response above is reading it straight back to you. Confirm the rest of what got injected:

```bash
curl http://localhost:8080/api/v1/env
```

```json
{"env":{"COMPONENT_ID":"demo/hello","COMPONENT_VERSION":"1.0.0","GREETING":"你好"}}
```

`COMPONENT_ID` and `COMPONENT_VERSION` are the two platform-wide variables every component gets, unconditionally (AGENTS.md §5.2).

## Change config, without touching any code

Add a `config:` block to the same component entry in `brickkit.yaml`:

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    expose: true
    exposePort: 8080
    config:
      greeting: "Hello"
```

```bash
brickkit up
curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"Hello","message":"Hello，我是 demo/hello@1.0.0","version":"1.0.0"}
```

Running `up` again is how a config change actually takes effect — there's no hot-reload mechanism to wait on (AGENTS.md §9.8), and running it again is always safe, whether or not anything actually changed.

## Check status, then stop

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

`down` stops containers; it never deletes volumes on its own (AGENTS.md §8) — `demo/hello` happens to have no data to keep, but the same command against a component with a real database behaves identically: stopped, not wiped.

---

Next in this series (see [the guide index](README.md) for the full planned list): the same project, grown to several components with real dependencies between them, and what `enabled` actually does when you start turning pieces off.
