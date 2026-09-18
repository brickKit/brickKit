# Quick Start (5 minutes)

The shortest real path from an empty directory to an HTTP-reachable container, using this repository's own test fixture, [`demo/hello`](../../tests/components/demo-hello/). Every command and every output block below is real — nothing here is invented.

**Prerequisites:** the BrickKit CLI built (`make build-cli`, or an installed release), and Docker running.

## 1. Build the fixture's image

`demo/hello` is this repository's own fixture and isn't published anywhere, so build it once from the repo root:

```bash
docker build -t brickkit-demo/hello:1.0.0 tests/components/demo-hello
```

In real use, a component's image already exists in a registry by the time you `brickkit add` it — a Manifest's `deployment.image` always points at something already built, never something the CLI builds for you. This step only exists because `demo/hello` isn't published anywhere.

## 2. Initialize a project

```bash
mkdir hello && cd hello
brickkit init hello
```

```
✅ 项目已初始化：hello
   📁 brickkit.yaml        项目配置
   📁 components/          组件源码（已配为本地安装源 local-dev）
   📁 .brickkit/           CLI 工作目录
   📁 .claude/skills/      AI 助手技能（4 个）
   📁 AGENTS.md            AI 助手项目导读

下一步：
  brickkit add --local               把 components/ 下的组件全加进来
  brickkit add people/basic@1.0.0    从安装源添加组件
  brickkit up                        一键启动
```

`init` already wired `components/` up as a `local`-type install source — that's where the next step looks.

## 3. Add the component

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

`--local` doesn't take a component ID — its meaning is "add every component in this local source at once," not "add this one component in local debug mode." That distinction is the single easiest thing to get wrong here.

## 4. Turn on exposure

Not exposing anything by default is a deliberate security default (AGENTS.md §4), not a missing step. Open `brickkit.yaml` and add two fields to the `demo/hello` entry:

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    expose: true
    exposePort: 8080
```

## 5. Start it

```bash
brickkit up
```

```
🚀 启动项目 hello（deploy.target: docker）
📋 组件状态计算：
   ✅ demo/hello@1.0.0  启动（顶层）

📋 启动顺序（拓扑排序）：
   1. demo-hello-1-0-0  无依赖

可独立启动：demo-hello-1-0-0（无依赖）
📄 已生成：.brickkit/generated/docker-compose.yaml

🔍 检测镜像拉取权限... ✅ 全部通过

🐳 正在启动（docker）...
   demo-hello-1-0-0             running（healthy）
✅ 全部组件已启动（1 个）

💡 查看状态：brickkit status
   查看日志：docker compose -p brickkit-hello logs -f
```

## 6. Talk to it

```bash
curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"}
```

**Done.** Your first component is running, reachable over plain HTTP — no CLI-internal channel involved.

## Tear it down

```bash
brickkit down
```

```
🛑 停止项目 hello
✅ 已停止全部组件

💡 数据卷未删除，数据库数据仍然保留
   需要彻底清理时手动执行：docker volume rm <卷名>
   重新启动：brickkit up
```

## Where to go next

- Want to understand what just happened? → [Core Concepts](concepts.md)
- Want the fuller walkthrough? → [Tutorial series](guide/README.md) (this Quick Start is the core path of [article 1](guide/01-first-project.md), which also covers dependencies, config changes, and K8s deployment)
- Hit a problem? → [Troubleshooting](troubleshooting.md)
