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
✅ Project initialized: hello-world
   📁 brickkit.yaml        Project config
   📁 components/          Component source (configured as the local install source local-dev)
   📁 .brickkit/           CLI working directory
   📁 .claude/skills/      AI assistant skills (4)
   📁 AGENTS.md            AI assistant project guide
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
🔍 Found 1 component in local install source: local-dev
📦 Adding demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅ (1 file)
✅ Written to brickkit.yaml (1 component)
```

`brickkit.yaml` now has one entry under `components:`. Nothing is running yet — `add` only ever writes configuration (AGENTS.md §8).

## See what would happen, then actually start it

```bash
brickkit up --dry-run
```

```
🚀 Starting project hello-world (deploy.target: docker)
📋 Component state calculation:
   ✅ demo/hello@1.0.0  starting (top-level)

📋 Start order (topological sort):
   1. demo-hello-1-0-0  no dependencies

Can start on their own: demo-hello-1-0-0 (no dependencies)
📄 Generated: .brickkit/generated/docker-compose.yaml
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
🐳 Starting (docker)...
   demo-hello-1-0-0             running (healthy)
✅ All components started (1)
```

## Talk to it

```bash
curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"Hello","message":"Hello, I'm demo/hello@1.0.0","version":"1.0.0"}
```

`demo/hello` declares one `configSchema` property, `greeting`, injected as the environment variable `GREETING` — and the response above is reading it straight back to you. Confirm the rest of what got injected:

```bash
curl http://localhost:8080/api/v1/env
```

```json
{"env":{"COMPONENT_ID":"demo/hello","COMPONENT_VERSION":"1.0.0","GREETING":"Hello"}}
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
      greeting: "Howdy"
```

```bash
brickkit up
curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"Howdy","message":"Howdy, I'm demo/hello@1.0.0","version":"1.0.0"}
```

Running `up` again is how a config change actually takes effect — there's no hot-reload mechanism to wait on (AGENTS.md §9.8), and running it again is always safe, whether or not anything actually changed.

## Check status, then stop

```bash
brickkit status
```

```
✅ Running (1 component)
 ┌────────────┬─────────┬───────────────────┬─────────────────────────────────────────────┐
 │ Component  │ Version │ Status            │ Port                                        │
 ├────────────┼─────────┼───────────────────┼─────────────────────────────────────────────┤
 │ demo/hello │ 1.0.0   │ running (healthy) │ 0.0.0.0:8080->8080/tcp, [::]:8080->8080/tcp │
 └────────────┴─────────┴───────────────────┴─────────────────────────────────────────────┘
```

```bash
brickkit down
```

```
🛑 Stopping project hello-world
✅ All components stopped

💡 Data volumes were not deleted; database data is still there
   For a full cleanup, run by hand: docker volume rm <volume-name>
   Start again with: brickkit up
```

`down` stops containers; it never deletes volumes on its own (AGENTS.md §8) — `demo/hello` happens to have no data to keep, but the same command against a component with a real database behaves identically: stopped, not wiped.

---

Next: [How the platform decides what runs](02-what-runs.md) — the same project, grown to several components with real dependencies between them, and what `enabled` actually does when you start turning pieces off.
