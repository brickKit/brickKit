# 5. Upgrade and Run Multiple Versions Side by Side

An "upgrade" on this platform is nothing more than changing the version string in one place — there's no separate upgrade command, and no migration tool that needs to know about a previous state. This article does that twice: once as a normal in-place bump, and once deliberately keeping the old version running alongside the new one — both are the exact same mechanism, exact-version pinning (AGENTS.md §5.1), used two different ways.

## Bump a version in place

Starting from Article 1's single-component project (`demo/hello@1.0.0`, already `add`ed and running), edit both the local source's Manifest and `brickkit.yaml` to `2.0.0` — the version bump and the image tag bump go together, since `metadata.version` and `deployment.image` are two independent fields that happen to need to move in step:

```yaml
# components/demo/hello/component.yaml
metadata:
  version: 2.0.0
deployment:
  image: brickkit-demo/hello:2.0.0
```

```yaml
# brickkit.yaml
components:
  - id: demo/hello
    version: 2.0.0
```

```bash
docker build -t brickkit-demo/hello:2.0.0 tests/components/demo-hello
brickkit up
```

```
⬆️ 检测到版本变更（004 §3.5.1）：
   demo/hello: 1.0.0 → 2.0.0

📋 组件状态计算：
   ✅ demo/hello@2.0.0  启动（顶层）
...
🐳 正在启动（docker）...
   demo-hello-2-0-0             running（healthy）
✅ 全部组件已启动（1 个）
```

```bash
docker compose -p brickkit-hello-world ps -a
```

```
NAME                                       STATUS
brickkit-hello-world-demo-hello-2-0-0-1    Up (healthy)
```

The old `demo-hello-1-0-0` container isn't in this list at all — not stopped, not sitting around exited, just gone. Because the versioned service name changed from `demo-hello-1-0-0` to `demo-hello-2-0-0`, the regenerated compose file no longer declares the old service, and `brickkit up` cleans up the resulting orphan rather than leaving a stale container behind for you to notice later.

## Run two versions on purpose

A `local` source directory holds exactly one `component.yaml` (a real, easy-to-miss trap — see [Building a Qualified Shell](../patterns/shell-implementers-guide.md#multiple-versions-mixed-deployment-shapes) for the same lesson learned the hard way in a different context), so two versions need two separate source directories:

```bash
mkdir -p components-v1/demo/hello
cp tests/components/demo-hello/component.yaml components-v1/demo/hello/    # still says 1.0.0
cp tests/components/demo-hello/openapi.json   components-v1/demo/hello/
docker build -t brickkit-demo/hello:1.0.0 tests/components/demo-hello
```

```yaml
# brickkit.yaml
sources:
  - id: local-dev
    type: local
    path: ./components        # has version 2.0.0
  - id: local-dev-v1
    type: local
    path: ./components-v1     # has version 1.0.0

components:
  - id: demo/hello
    version: 2.0.0
    expose: true
    exposePort: 8082
  - id: demo/hello
    version: 1.0.0
    expose: true
    exposePort: 8081
```

```bash
brickkit up
```

```
📋 组件状态计算：
   ✅ demo/hello@2.0.0  启动（顶层）
   ✅ demo/hello@1.0.0  启动（顶层）
...
   demo-hello-1-0-0             running（healthy）
   demo-hello-2-0-0             running（healthy）
✅ 全部组件已启动（2 个）
```

Both versions are genuinely independent, addressable containers — not a "canary" of one underlying deployment, two full ones:

```bash
curl http://localhost:8081/api/v1/hello
curl http://localhost:8082/api/v1/hello
```
```json
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"}
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@2.0.0","version":"2.0.0"}
```

Both images here were built from the exact same source — the `version` field each one reports comes entirely from the `COMPONENT_VERSION` environment variable the platform injects per-Manifest (AGENTS.md §5.2), not from anything baked into the image. `brickkit status` shows both under the same component ID, side by side:

```bash
brickkit status
```

```
✅ 运行中（2 个组件）
 ┌────────────┬───────┬───────────────────┬───────────────────────────────────────────────┐
 │ 组件       │ 版本  │ 状态              │ 端口                                          │
 ├────────────┼───────┼───────────────────┼───────────────────────────────────────────────┤
 │ demo/hello │ 1.0.0 │ 运行中（healthy） │ 0.0.0.0:8081->8080/tcp, [::]:8081->8080/tcp    │
 │ demo/hello │ 2.0.0 │ 运行中（healthy） │ 0.0.0.0:8082->8080/tcp, [::]:8082->8080/tcp    │
 └────────────┴───────┴───────────────────┴───────────────────────────────────────────────┘
```

## Removing one version out of several

```bash
brickkit remove demo/hello
```

```
❌ demo/hello 存在多个版本（2.0.0, 1.0.0），请指定版本：
   建议：brickkit remove demo/hello@2.0.0
```

With two versions of the same ID installed, `remove` refuses to guess which one you meant — and hands you the exact command to run instead of just naming the ambiguity:

```bash
brickkit remove demo/hello@1.0.0
```

```
✅ 已移除 demo/hello@1.0.0
```

`demo-hello-1-0-0`'s container is still running after this — `remove`, like `add`, only ever writes `brickkit.yaml` (AGENTS.md §8); the running state doesn't change until the next `brickkit up`, which regenerates the deployment file without that entry and cleans up the resulting orphan exactly the way the in-place version bump did earlier.

---

Next in this series (see [the guide index](README.md)): a real multi-component system — assembling several of these together, and then deliberately breaking the connections between them to see exactly how each failure mode actually looks.
