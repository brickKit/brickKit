# 7. Consume Someone Else's Component

A component's `component.yaml` can declare `artifacts` — files a caller needs to actually integrate with it (an OpenAPI spec, a protobuf contract, an SDK), separate from the Manifest itself (AGENTS.md §6). This article covers where those files actually end up, how to grab them without installing the component at all, a real, already-built component whose entire job is displaying exactly this kind of thing for others, and what to do when the component you depend on isn't built yet: stand in a stub that carries the contract you've agreed on.

## What an artifact declaration looks like

[`demo/hello`](../../../tests/components/demo-hello/)'s Manifest:

```yaml
artifacts:
  - type: api-docs
    format: openapi
    description: HTTP API 文档
    files:
      - openapi.json
```

`type` and `format` are free-form strings the platform never interprets (AGENTS.md §6) — `api-docs`/`openapi` here, `api-contract`/`protobuf` elsewhere for a gRPC service. The platform's only job is making sure `files` actually gets to whoever asks for it.

## Where `add` puts it

Every earlier article's `brickkit add` output already showed this happening, just without pointing at it directly:

```
📦 添加 demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅（1 个文件）
```

```bash
cat .brickkit/artifacts/demo-hello-1-0-0/api-docs/openapi.json
```

```json
{
  "openapi": "3.0.0",
  "info": { "title": "demo/hello", "version": "1.0.0" },
  "paths": {
    "/healthz": { "get": { "summary": "存活检查", "responses": { "200": { "description": "ok" } } } },
    "/api/v1/hello": { "get": { "summary": "问候", "responses": { "200": { "description": "ok" } } } },
    "/api/v1/env": { "get": { "summary": "回显平台注入的环境变量", "responses": { "200": { "description": "ok" } } } }
  }
}
```

A real, complete OpenAPI document, sitting on disk under the component's own versioned service name — ready to feed to a client generator, without needing the component's source, and without needing it to even be running.

## Getting artifacts without installing anything

`brickkit fetch` is a separate command specifically for this: when you need another project's component's contract to write a client against, but you have no intention of running that component yourself (AGENTS.md §8):

```bash
brickkit fetch demo/hello@1.0.0
```

```
📦 已下载 demo/hello@1.0.0 的产物（未写入 brickkit.yaml）
   .brickkit/artifacts/demo-hello-1-0-0/
     api-docs/openapi.json

💡 这个组件不会被本项目部署。要连它，把对方给的地址填进依赖方的 config
   （跨项目共用组件）
```

`brickkit.yaml`'s `components:` list is untouched — `fetch` writes files, never config. This is the real difference from `add`: `add` says "I want to run this," `fetch` says "I just need to know its shape."

## A component whose entire job is exactly this

[`infra/api-docs`](../../../tests/components/infra-api-docs/) exists specifically to aggregate other components' docs into one browsable entry point — and every one of its dependencies is optional, on purpose, because a documentation hub that refuses to load just because one business component isn't installed defeats its own point:

```bash
brickkit up   # infra/api-docs alone, nothing it optionally depends on installed
curl http://localhost:8095/api/v1/sources
```

```json
{"sources":[
  {"componentId":"auth/password-login","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]},
  {"componentId":"authorization/rbac","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]},
  {"componentId":"department/tree","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]},
  {"componentId":"erp/backend","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]},
  {"componentId":"infra/redis-event-bus","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]},
  {"componentId":"people/basic","status":"absent","reason":"该组件未安装（平台没有注入它的地址）","kinds":[]}
],"total":6}
```

Not an error, not a blank page — a clean, complete answer naming every component it knows how to display and exactly why each one isn't showing up right now. Install any of them later, and the same endpoint would report that one as available instead, with no restart-and-hope involved on `infra/api-docs`'s side — it's just reading `*_ENDPOINT` variables that either are or aren't in its environment (AGENTS.md §5.3), the same mechanism every optional dependency in this series has used from Article 2 onward.

## When the upstream isn't ready yet: stand in a stub

Everything so far assumed that the component you depend on already exists somewhere you can install it from. Often it doesn't yet: another team is still building `demo/hello`, or has built it but not published it, while you are already writing `demo/caller`, which needs it. What the two teams *can* settle early is its **contract** — a written description of an interface: which URLs it will answer, and what shape of data each one returns. In this article the contract is an OpenAPI file, the widely used format for describing an HTTP API.

Two more words, used below in a precise sense:

- A **stub** is a stand-in *component*. It has the real component's ID and version and carries the agreed contract, but does no work — it is just files that BrickKit can read.
- A **mock** is a *running program* that answers requests with made-up data shaped the way the contract says.

BrickKit only ever deals with the stub; the mock is an ordinary process you start yourself, and BrickKit never learns it exists. Most of what follows is about keeping those two apart.

The upside: you can build and run `demo/caller` today against the agreed shape, and on the day the real component arrives nothing in `demo/caller` changes. The cost: the mock knows only what the contract says, so anything the real component does differently — or beyond it — you find out late; and the stub is fake, so it has to be taken out again (the last part shows how).

There is no dedicated command for any of this, on purpose (the reasons are near the end). The recipe chains three features you have already met: `brickkit new --contract` makes the stub, `brickkit add --local` registers it, and `local: true` ([Article 3](03-local-debugging.md)) tells BrickKit "I run this one myself".

### Starting point: a consumer whose dependency doesn't exist yet

From the root of the repository checkout, make a fresh project directory (the `../tests/...` paths below assume it sits next to `tests/`, as in Article 1), and put [`demo/caller`](../../../tests/components/demo-caller/) in it as the consumer:

```bash
mkdir stub-demo && cd stub-demo
brickkit init hello-world

mkdir -p components/demo/caller
cp ../tests/components/demo-caller/component.yaml components/demo/caller/
cp ../tests/components/demo-caller/openapi.json components/demo/caller/
```

Among other things, its Manifest declares:

```yaml
dependencies:
  components:
    - demo/hello@1.0.0
    - id: demo/bus@1.0.0
      optional: true
```

`demo/hello@1.0.0` is a required dependency at an exact version. If you ran `brickkit add --local` now it would stop with a required-dependency error, because no source has `demo/hello` yet — there is nothing to install.

### Step 1: make the stub

```bash
brickkit new demo/hello --contract openapi
```

```
✅ 已生成组件骨架：demo/hello
   📄 components/demo/hello/component.yaml
   📄 components/demo/hello/api/openapi.yaml

下一步：
  改完骨架里的 TODO
  brickkit add --local               把它加进 brickkit.yaml（本地安装源里能扫到它的话）
  brickkit up --dry-run               校验能不能通过
```

Two files landed in `components/demo/hello/`, the directory the `local-dev` source that `init` set up already scans: a skeleton `component.yaml` that passes validation as it is, and a placeholder contract, `api/openapi.yaml`. The skeleton has already registered that file under `artifacts` — the same kind of declaration `demo/hello` itself uses at the top of this article — so `add` will copy it into `.brickkit/artifacts/` like any other component's artifacts:

```yaml
artifacts:
  - type: api-contract
    format: openapi
    files:
      - api/openapi.yaml
```

Two edits make it a usable stub.

**The version.** The skeleton starts at `0.1.0`, but `demo/caller` asks for exactly `1.0.0` — dependency versions are exact, never ranges (AGENTS.md §9.2). In `components/demo/hello/component.yaml`, change `version: 0.1.0` (under `metadata`) to `version: 1.0.0`. Forget it and `add --local` stops, and says why. This is an excerpt; the real message goes on with suggestions, and one of them is exactly this fix:

```
❌ 错误：强依赖缺失
   卡在组件：demo/caller@1.0.0（来自 local-dev）
   本次结果：已中止，brickkit.yaml 未修改
   缺失依赖：demo/hello@1.0.0
   原因：安装源里有这个组件，但版本不是要的那个
   安装源 local-dev（local）：这里是 0.1.0
...
```

**The contract.** Replace the placeholder in `components/demo/hello/api/openapi.yaml` with what the two teams agreed. Here that is one endpoint, small on purpose:

```yaml
openapi: 3.0.3
info:
  title: demo/hello
  version: 1.0.0
paths:
  /api/v1/hello:
    get:
      summary: A greeting
      responses:
        "200":
          description: A greeting
          content:
            application/json:
              schema:
                type: object
                properties:
                  message:
                    type: string
                    example: hello from the stub
```

The skeleton's other `TODO`s (`name`, `description`, `image`, `port`) can stay; Step 3 shows why a stub never needs them.

### Step 2: register both components

```bash
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
   💡 弱依赖降级由组件自行处理；如需启用，请确认该组件已发布并可从安装源获取
📦 添加 demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅（1 个文件）
✅ 已写入 brickkit.yaml（2 个组件）
```

The stub is found in the local source like any other component, and `demo/caller`'s required dependency now resolves to it — that is the `依赖 demo/hello@1.0.0 ✅ 已拉取` line. The `demo/bus` warning is `demo/caller`'s own *optional* dependency from [Article 2](02-what-runs.md) and has nothing to do with the stub. `brickkit.yaml` now lists both components.

### Step 3: tell BrickKit you'll run the stub yourself

Left alone, BrickKit would try to start a container from the stub's `image:` line — a placeholder that points at nothing. `local: true` ([Article 3](03-local-debugging.md)) says otherwise: "this component runs on my machine; generate no container for it, but keep it in the dependency graph". `localPort` picks the port on your machine:

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    local: true
    localPort: 18081
  - id: demo/caller
    version: 1.0.0
```

```bash
brickkit up --dry-run
```

An excerpt — `...` marks lines left out:

```
🚀 启动项目 hello-world（deploy.target: docker）
...
📋 组件状态计算：
   ✅ demo/hello@1.0.0   启动（demo/caller 需要）
   ✅ demo/caller@1.0.0  启动（顶层）
...
🔧 本地调试（local: true）：
   demo/hello@1.0.0
      不生成容器；请在 IDE 里启动它，监听 localhost:18081
      环境变量：.brickkit/generated/local-debug.demo-hello-1-0-0.env
      VS Code：launch.json 里配 "envFile": "${workspaceFolder}/.brickkit/generated/local-debug.demo-hello-1-0-0.env"
📄 已生成：.brickkit/generated/docker-compose.yaml
...
```

The stub still takes part in the status calculation and the dependency graph. What changed is "不生成容器" (no container is generated) and where it is expected: `localhost:18081`. That is `localPort`, not the `8080` written in the stub's own Manifest — a `local: true` component's `image` and `port` are never used, which is why the skeleton's `TODO`s can stay. (A real `brickkit up` doesn't check the stub's image either: with a throwaway PostgreSQL bound as in [Article 6](06-assemble-and-break.md), the image check passed even though nothing had ever built the placeholder image.) The message talks about "your IDE" because `local: true` was made for debugging; for a mock it just means "any program you start on your own machine".

### Step 4: start a mock on that port

This step is not BrickKit's. Anything that listens on port 18081 of your machine and answers the way the contract says will do; BrickKit doesn't start it, watch it, or know what it is. A short Python script is enough to try the idea:

```python
# mock_hello.py: answers the one endpoint the contract describes
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/api/v1/hello":
            self.send_error(404)
            return
        body = json.dumps({"message": "hello from the stub"}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

# 0.0.0.0 means "every network interface"; see the note below
HTTPServer(("0.0.0.0", 18081), Handler).serve_forever()
```

```bash
python3 mock_hello.py
```

Leave it running in its own terminal.

The script listens on `0.0.0.0` rather than `127.0.0.1` on purpose. `demo/caller` will reach your machine from *outside* it — from inside a container — and a program that listens only on the loopback address never sees that traffic. Both were tried: bound to `127.0.0.1`, the mock answered a request from the host itself but the caller got a `502` (it couldn't reach its dependency); bound to `0.0.0.0`, the caller got the mock's answer.

Hand-written answers stop scaling after a few endpoints, so real projects usually reach for a tool that reads the contract itself. One example is Prism. It is third-party and unrelated to BrickKit, and this is the command line it was run with while writing this (check its own documentation if the flags have changed); it answered the same request with the `example` value from the contract above:

```bash
npx --yes @stoplight/prism-cli mock -p 18081 -h 0.0.0.0 components/demo/hello/api/openapi.yaml
```

### What `demo/caller` is told

`brickkit up --dry-run` also wrote `.brickkit/generated/docker-compose.yaml`. Looking for the stub's name in it (this is `grep` output from a real run, not something BrickKit prints — so unlike the blocks above, nothing checks it automatically):

```bash
grep -n "DEMO_HELLO_ENDPOINT\|extra_hosts\|demo-hello-1-0-0" .brickkit/generated/docker-compose.yaml
```

```
27:      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:18081
28:    extra_hosts:
29:      - demo-hello-1-0-0:host-gateway
50:      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:18081
51:    extra_hosts:
52:      - demo-hello-1-0-0:host-gateway
```

Two settings matter, and each appears twice: once for `demo/caller`'s own container and once for its migration container, which runs from the same image (AGENTS.md §5.5 and §6). The environment variable hands the caller the same kind of address it would get for a real `demo/hello` — the versioned service name — but with your `localPort`. `extra_hosts` is what makes that name mean something: `host-gateway` is Docker's special value for "the host machine, seen from inside a container", so `demo-hello-1-0-0` resolves to your machine, and port 18081 is where the mock listens ([Article 3](03-local-debugging.md) went through this in detail). `demo/caller` reads `DEMO_HELLO_ENDPOINT` as it always does and has no idea the other end is a stand-in.

### Prove it end to end (needs Docker and Python 3)

The steps above needed neither. To watch a real container hit the mock, there is a catch: `demo/caller`'s image needs a real database to come up under `brickkit up` ([Article 6](06-assemble-and-break.md) binds one), yet started by hand it answers without one. So skip `brickkit up` for this check and run the image directly, carrying exactly the two settings from the `grep` above:

```bash
docker build -t brickkit-demo/caller:1.0.0 ../tests/components/demo-caller

docker run -d --name stub-demo-caller \
  --add-host demo-hello-1-0-0:host-gateway \
  -e DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:18081 \
  brickkit-demo/caller:1.0.0

docker exec stub-demo-caller wget -qO- http://localhost:8080/api/v1/call
```

`--add-host` is `docker run`'s spelling of `extra_hosts`, and `-e` is the injected variable. `/api/v1/call` is the endpoint that makes `demo/caller` call its dependency and show what came back (Article 6 used it too):

```
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:18081","upstream":{"message":"hello from the stub"},"version":"1.0.0"}
```

`upstream` is the mock's answer, fetched from inside a container by a component that believes it is talking to `demo/hello`. In the terminal running the mock, the request shows up as a log line whose client address is not `127.0.0.1` — it came from the container (the address itself will differ on your machine):

```
172.17.0.2 - - [19/Sep/2026 18:12:21] "GET /api/v1/hello HTTP/1.1" 200 -
```

The same check was also run through a full `brickkit up`, with a throwaway PostgreSQL bound as in Article 6: only `demo/caller` started (there is nothing to start for the stub), and `curl` against it — exposed with `expose: true`, as in the earlier articles — returned the same `upstream`.

Clean up with `docker rm -f stub-demo-caller`, and stop the mock with Ctrl-C.

### Why there is no mock command

You might expect a `mock` command that builds a fake server from the contract, or a switch on `up` that swaps a stand-in in for any required dependency that is missing. Neither exists, for three reasons:

- **The platform never reads a contract's content.** `artifacts.format` is a free-form string that BrickKit carries along and never interprets (AGENTS.md §6). Generating mocks would mean understanding OpenAPI, protobuf, gRPC and whatever format comes next — a job that is never finished, and one that dedicated tools already do. BrickKit's part stays small: getting the address to whatever you run.
- **Swapping in a stand-in automatically contradicts two of its principles.** A missing required dependency is supposed to stop `up`, not be papered over (AGENTS.md §5.3), and explicit beats implicit (§4): one misuse and a component that answers everything with made-up data gets deployed for real. The recipe above is explicit instead. The substitution is written into `brickkit.yaml`, where a reviewer sees it; it generates no container; and pointing the project at Kubernetes is refused while `local: true` is still there (a Pod can't reach a process on your machine — the CLI says so and stops).
- **A mock under its own name would never receive traffic.** The address `demo/caller` is given is built from the real component's versioned service name, `demo-hello-1-0-0` (AGENTS.md §5.1). The stub keeps that name, and `extra_hosts` points that very name at your machine. A mock that answered to some other name would sit there unused.

### When the real component arrives

Taking the stub out again is a few edits and one command — and the command is the one people skip:

1. Delete the stub's source, `components/demo/hello/`.
2. Delete `local: true` and `localPort` from `demo/hello`'s entry in `brickkit.yaml`.
3. Delete `.brickkit/artifacts/demo-hello-1-0-0/`, the copy of the stub's contract that `add` made.
4. Make sure a source that carries the real `demo/hello@1.0.0` is listed under `sources:`, then run `brickkit add demo/hello@1.0.0 --yes`.

Don't skip that last command. A component that comes from a marketplace or a Git source has its Manifest read from the copy cached under `.brickkit/manifests/` rather than fetched again on every run (AGENTS.md §2.3), and the copy cached for `demo/hello@1.0.0` is still the stub's. With a Git source listed and only steps 1 and 2 done, `brickkit up --dry-run` carried on quietly and generated a service that runs the stub's placeholder image, `demo/hello:0.1.0`. Because the component is already in `brickkit.yaml`, `brickkit add` asks whether to refresh that cache, and its `--yes` flag answers for you; it then reads the Manifest again from the first source that still has the component (with the stub directory gone, no longer `local-dev`). This was checked with a Git source — a local bare repository standing in for the real upstream. Step 3 matters for a quieter reason: without it the stub's placeholder contract stays behind in `.brickkit/artifacts/demo-hello-1-0-0/api-contract/`, and anyone generating a client from that directory would be reading the placeholder.

---

Next: [Manage component source](08-component-source.md) — cloning another team's component source, keeping only what you're working on, pushing changes back, deleting it cleanly, and the commit hook that guards source committed along with the project.
