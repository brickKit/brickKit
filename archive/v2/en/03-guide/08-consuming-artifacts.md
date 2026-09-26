# 8. Consume Someone Else's Component

A component's `component.yaml` can declare `artifacts` — files a caller needs to actually integrate with it (an OpenAPI spec, a protobuf contract, an SDK), separate from the Manifest itself (AGENTS.md §6). This article covers where those files actually end up, how to grab them without installing the component at all, a real, already-built component whose entire job is displaying exactly this kind of thing for others, and what to do when the component you depend on isn't built yet: stand in a stub that carries the contract you've agreed on.

## What an artifact declaration looks like

[`demo/hello`](../../../tests/components/demo-hello/)'s Manifest:

```yaml
artifacts:
  - type: api-docs
    format: openapi
    description: HTTP API docs
    files:
      - openapi.json
```

`type` and `format` are free-form strings the platform never interprets (AGENTS.md §6) — `api-docs`/`openapi` here, `api-contract`/`protobuf` elsewhere for a gRPC service. The platform's only job is making sure `files` actually gets to whoever asks for it.

## Where `add` puts it

Every earlier article's `brickkit add` output already showed this happening, just without pointing at it directly:

```
📦 Adding demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅ (1 file)
```

```bash
cat .brickkit/artifacts/demo-hello-1-0-0/api-docs/openapi.json
```

```json
{
  "openapi": "3.0.0",
  "info": { "title": "demo/hello", "version": "1.0.0" },
  "paths": {
    "/healthz": { "get": { "summary": "Liveness check", "responses": { "200": { "description": "ok" } } } },
    "/api/v1/hello": { "get": { "summary": "A greeting", "responses": { "200": { "description": "ok" } } } },
    "/api/v1/env": { "get": { "summary": "Echo the environment variables injected by the platform", "responses": { "200": { "description": "ok" } } } }
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
📦 Downloaded the artifacts of demo/hello@1.0.0 (not written to brickkit.yaml)
   .brickkit/artifacts/demo-hello-1-0-0/
     api-docs/openapi.json

💡 This component won't be deployed by this project. To call it, put the address the other side gave you into the dependent's config
   (a component shared across projects)
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
  {"componentId":"auth/password-login","status":"absent","reason":"The component is not installed (the platform injected no address for it)","kinds":[]},
  {"componentId":"authorization/rbac","status":"absent","reason":"The component is not installed (the platform injected no address for it)","kinds":[]},
  {"componentId":"department/tree","status":"absent","reason":"The component is not installed (the platform injected no address for it)","kinds":[]},
  {"componentId":"erp/backend","status":"absent","reason":"The component is not installed (the platform injected no address for it)","kinds":[]},
  {"componentId":"infra/redis-event-bus","status":"absent","reason":"The component is not installed (the platform injected no address for it)","kinds":[]},
  {"componentId":"people/basic","status":"absent","reason":"The component is not installed (the platform injected no address for it)","kinds":[]}
],"total":6}
```

(The `reason` strings come from the sample component itself, which writes them in Chinese; they are translated here.)

Not an error, not a blank page — a clean, complete answer naming every component it knows how to display and exactly why each one isn't showing up right now. Install any of them later, and the same endpoint would report that one as available instead, with no restart-and-hope involved on `infra/api-docs`'s side — it's just reading `*_ENDPOINT` variables that either are or aren't in its environment (AGENTS.md §5.3), the same mechanism every optional dependency in this series has used from Article 2 onward.

## When the upstream isn't ready yet: stand in a stub

Everything so far assumed that the component you depend on already exists somewhere you can install it from. Often it doesn't yet: another team is still building `demo/hello`, or has built it but not published it, while you are already writing `demo/caller`, which needs it. What the two teams *can* settle early is its **contract** — a written description of an interface: which URLs it will answer, and what shape of data each one returns. In this article the contract is an OpenAPI file, the widely used format for describing an HTTP API.

Two more words, used below in a precise sense:

- A **stub** is a stand-in *component*. It has the real component's ID and version and carries the agreed contract, but does no work — it is just files that BrickKit can read.
- A **mock** is a *running program* that answers requests with made-up data shaped the way the contract says.

BrickKit only ever deals with the stub; the mock is an ordinary process you start yourself, and BrickKit never learns it exists. Most of what follows is about keeping those two apart.

The upside: you can build and run `demo/caller` today against the agreed shape, and on the day the real component arrives nothing in `demo/caller` changes. The cost: the mock knows only what the contract says, so anything the real component does differently — or beyond it — you find out late; and the stub is fake, so it has to be taken out again (the last part shows how). It is a development-time expedient, not a replacement for verifying against the real component once that exists: [the testing patterns](../07-patterns/01-testing.md) explain why a cross-component test should hit the real dependency rather than a stand-in.

There is no dedicated command for any of this, on purpose (the reasons are near the end). The recipe chains three features you have already met: `brickkit new --contract` makes the stub, `brickkit add --local` registers it, and `mode: debug` ([Article 3](03-local-debugging.md)) tells BrickKit "I run this one myself".

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
✅ Component skeleton generated: demo/hello
   📄 components/demo/hello/component.yaml
   📄 components/demo/hello/api/openapi.yaml

Next steps:
  finish the TODOs in the skeleton
  brickkit add --local               add it to brickkit.yaml (if the local install source can scan it)
  brickkit up --dry-run               check that it passes validation
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
❌ Error: required dependency missing
   Stuck on component: demo/caller@1.0.0 (from local-dev)
   Result of this run: Aborted; brickkit.yaml was not modified
   Missing dependency: demo/hello@1.0.0
   Reason: the install source has this component, but not the version that was asked for
   Install source local-dev (local): it has 0.1.0 here
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
🔍 Found 2 components in local install source: local-dev
📦 Adding demo/caller@1.0.0
   ├── Manifest ✅
   ├── dependency demo/hello@1.0.0 ✅ pulled (artifacts: 1 file)
   └── artifacts ✅ (1 file)
⚠️ Warning: optional dependency missing: demo/bus@1.0.0
   Affected component: demo/caller@1.0.0
   Reason: The component was not found in any install source
   Impact: This component's environment variable DEMO_BUS_ENDPOINT will not be injected
   💡 Degrading gracefully for a missing optional dependency is the component's own responsibility; to enable it, confirm it has been published and is available from an install source
📦 Adding demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅ (1 file)
✅ Written to brickkit.yaml (2 components)
```

The stub is found in the local source like any other component, and `demo/caller`'s required dependency now resolves to it — that is the `dependency demo/hello@1.0.0 ✅ pulled` line. The `demo/bus` warning is `demo/caller`'s own *optional* dependency from [Article 2](02-what-runs.md) and has nothing to do with the stub. `brickkit.yaml` now lists both components.

### Step 3: tell BrickKit you'll run the stub yourself

Left alone, BrickKit would try to start a container from the stub's `image:` line — a placeholder that points at nothing. `mode: debug` ([Article 3](03-local-debugging.md)) says otherwise: "this component runs on my machine; generate no container for it, but keep it in the dependency graph". It goes in `override.yaml`, not `brickkit.yaml` — `mode: debug` is local, machine-specific state, and `brickkit.yaml` rejects it outright. `brickkit.yaml` stays exactly what `add --local` just wrote (both components, no `mode`); `localPort` (which picks the port on your machine) goes alongside `mode: debug`:

```yaml
# override.yaml
components:
  - id: demo/hello
    mode: debug
    localPort: 18081
```

```bash
brickkit up --dry-run
```

An excerpt — `...` marks lines left out:

```
🚀 Starting project hello-world (deploy.target: docker)
...
📋 Component state calculation:
   ✅ demo/hello@1.0.0   starting (mode: debug)
   ✅ demo/caller@1.0.0  starting (top-level)
...
🔧 Local debugging (mode: debug):
   demo/hello@1.0.0
      No container is generated; start it in your IDE, listening on localhost:18081
      Environment variables: .brickkit/generated/local-debug.demo-hello-1-0-0.env
      VS Code: set "envFile": "${workspaceFolder}/.brickkit/generated/local-debug.demo-hello-1-0-0.env" in launch.json
📄 Generated: .brickkit/generated/compose.yaml
...
```

The stub still takes part in the status calculation and the dependency graph. What changed is "No container is generated" and where it is expected: `localhost:18081`. That is `localPort`, not the `8080` written in the stub's own Manifest — a `mode: debug` component's `image` and `port` are never used, which is why the skeleton's `TODO`s can stay. (A real `brickkit up` doesn't check the stub's image either: with a throwaway PostgreSQL bound as in [Article 7](07-assemble-and-break.md), the image check passed even though nothing had ever built the placeholder image.) The message talks about "your IDE" because `mode: debug` was made for debugging; for a mock it just means "any program you start on your own machine".

The `...` lines also hide one more warning, `⚠️ Warning: resource dependencies are not satisfied (--dry-run doesn't block)`: `demo/caller` declares that it needs a database, and this project hasn't bound one. It has nothing to do with the stub, and the end-to-end section below comes back to it.

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

The script listens on `0.0.0.0` rather than `127.0.0.1` on purpose. `127.0.0.1` is the loopback address — "this machine talking to itself" — and a program bound to it only accepts connections made from the machine itself, while `0.0.0.0` means every network interface. `demo/caller` will reach your machine from *outside* it, from inside a container, so a mock bound only to the loopback address would never see that traffic. Both were tried: bound to `127.0.0.1`, the mock answered a request from the host itself but the caller got a `502` (it couldn't reach its dependency); bound to `0.0.0.0`, the caller got the mock's answer.

Hand-written answers stop scaling after a few endpoints, so real projects usually reach for a tool that reads the contract itself. One example is Prism. It is third-party and unrelated to BrickKit. The command below was verified with version `5.16.0` while writing this (check its own documentation if the flags have changed), and it answered the same request with the `example` value from the contract above. Note that `npx --yes` skips the confirmation prompt, so it downloads and runs third-party code without asking:

```bash
npx --yes @stoplight/prism-cli@5.16.0 mock -p 18081 -h 0.0.0.0 components/demo/hello/api/openapi.yaml
```

### What `demo/caller` is told

`brickkit up --dry-run` also wrote `.brickkit/generated/compose.yaml`. Looking for the stub's name in it (this is `grep` output from a real run, not something BrickKit prints — so unlike the blocks above, nothing checks it automatically):

```bash
grep -n "DEMO_HELLO_ENDPOINT\|extra_hosts\|demo-hello-1-0-0" .brickkit/generated/compose.yaml
```

```
27:      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:18081
28:    extra_hosts:
29:      - demo-hello-1-0-0:host-gateway
50:      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:18081
51:    extra_hosts:
52:      - demo-hello-1-0-0:host-gateway
```

Two settings matter, and each appears twice: once for `demo/caller`'s own container and once for its migration container — the one-shot container that runs the component's database setup before the main one starts, from the same image (AGENTS.md §5.5 and §6). The environment variable hands the caller the same kind of address it would get for a real `demo/hello` — the versioned service name, the component ID plus its exact version — but with your `localPort`. `extra_hosts` is what makes that name mean something: it adds one entry, name to address, to the container's own hosts table, and `host-gateway` is Docker's special value for "the host machine, seen from inside a container" — so `demo-hello-1-0-0` resolves to your machine, and port 18081 is where the mock listens ([Article 3](03-local-debugging.md) went through this in detail). `demo/caller` reads `DEMO_HELLO_ENDPOINT` as it always does and has no idea the other end is a stand-in.

### Prove it end to end (needs Docker and Python 3)

The steps above needed neither. To watch a real container hit the mock, there is a catch: `demo/caller`'s image needs a real database to come up under `brickkit up` (that is the warning the `...` above left out; [Article 7](07-assemble-and-break.md) binds one), yet started by hand it answers without one. So skip `brickkit up` for this check and run the image directly, carrying exactly the two settings from the `grep` above:

```bash
docker build -t brickkit-demo/caller:1.0.0 ../tests/components/demo-caller

docker run -d --name stub-demo-caller \
  --add-host demo-hello-1-0-0:host-gateway \
  -e DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:18081 \
  brickkit-demo/caller:1.0.0

docker exec stub-demo-caller wget -qO- http://localhost:8080/api/v1/call
```

`--add-host` is `docker run`'s spelling of `extra_hosts`, and `-e` is the injected variable. `/api/v1/call` is the endpoint that makes `demo/caller` call its dependency and show what came back (Article 7 used it too):

```
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:18081","upstream":{"message":"hello from the stub"},"version":"1.0.0"}
```

`upstream` is the mock's answer, fetched from inside a container by a component that believes it is talking to `demo/hello`. In the terminal running the mock, the request shows up as a log line whose client address is not `127.0.0.1` — it came from the container (the address itself will differ on your machine):

```
172.17.0.2 - - [19/Sep/2026 18:12:21] "GET /api/v1/hello HTTP/1.1" 200 -
```

The same check was also run through a full `brickkit up`, with a throwaway PostgreSQL bound as in Article 7: only `demo/caller` started (there is nothing to start for the stub), and `curl` against it — exposed with `expose: true`, as in the earlier articles — returned the same `upstream`.

Clean up with `docker rm -f stub-demo-caller`, and stop the mock with Ctrl-C.

### Why there is no mock command

You might expect a `mock` command that builds a fake server from the contract, or a switch on `up` that swaps a stand-in in for any required dependency that is missing. Neither exists, for three reasons:

- **The platform never reads a contract's content.** `artifacts.format` is a free-form string that BrickKit carries along and never interprets (AGENTS.md §6). Generating mocks would mean understanding OpenAPI, protobuf, gRPC and whatever format comes next — a job that is never finished, and one that dedicated tools already do. BrickKit's part stays small: getting the address to whatever you run.
- **Swapping in a stand-in automatically contradicts two of its principles.** A missing required dependency is supposed to stop `up`, not be papered over (AGENTS.md §5.3), and explicit beats implicit (§4): one misuse and a component that answers everything with made-up data gets deployed for real. The recipe above is explicit instead: the stub is registered as a real, reviewed dependency in `brickkit.yaml`, where a reviewer sees it — only the fact that you're personally running it as a bare process right now lives in `override.yaml`, local state nobody expects to find in a review in the first place. It still generates no container, and pointing the project at Kubernetes is still refused while `mode: debug` is set for it. (A Pod is the unit Kubernetes runs your containers in; one running on a cluster's servers can't reach a process on your machine, and the CLI says so and stops.)
- **A mock under its own name would never receive traffic.** The address `demo/caller` is given is built from the real component's versioned service name, `demo-hello-1-0-0` (AGENTS.md §5.1). The stub keeps that name, and `extra_hosts` points that very name at your machine. A mock that answered to some other name would sit there unused.

### When the real component arrives

Taking the stub out again is a few edits and one command — and the command is the one people skip:

1. Delete the stub's source, `components/demo/hello/`.
2. Delete `demo/hello`'s entry from `override.yaml` (or the whole file, if it holds nothing else).
3. Delete `.brickkit/artifacts/demo-hello-1-0-0/`, the copy of the stub's contract that `add` made.
4. Make sure a source that carries the real `demo/hello@1.0.0` is listed under `sources:`, then run `brickkit add demo/hello@1.0.0 --yes`.

Don't skip that last command. A component that comes from a marketplace or a Git source has its Manifest read from the copy cached under `.brickkit/manifests/` rather than fetched again on every run (AGENTS.md §2.3), and the copy cached for `demo/hello@1.0.0` is still the stub's. (A `local` source is the exception: it is re-read on every run, so a real upstream that is itself a local source doesn't have this trap.) With a Git source listed and only steps 1 and 2 done, `brickkit up --dry-run` carried on quietly and generated a service that runs the stub's placeholder image, `demo/hello:0.1.0`. Because the component is already in `brickkit.yaml`, `brickkit add` asks whether to refresh that cache, and its `--yes` flag answers for you; it then reads the Manifest again from the first source that still has the component (with the stub directory gone, no longer `local-dev`). This was checked with a Git source — a local bare repository standing in for the real upstream. Step 3 matters for a quieter reason: without it the stub's placeholder contract stays behind in `.brickkit/artifacts/demo-hello-1-0-0/api-contract/`, and anyone generating a client from that directory would be reading the placeholder.

---

Next: [Manage component source](09-component-source.md) — cloning another team's component source, keeping only what you're working on, pushing changes back, deleting it cleanly, and the commit hook that guards source committed along with the project.
