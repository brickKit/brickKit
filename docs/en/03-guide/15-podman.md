# 15. Deploy with Podman instead of Docker

Same Manifest, same `brickkit.yaml` — the only thing that changes is which engine runs it, and unlike Kubernetes this isn't even a field in `brickkit.yaml` itself: `target: podman` lives in `override.yaml` (AGENTS.md §5.10), a personal, gitignored, per-developer choice, never something a teammate reviewing `brickkit.yaml` has to see. This article redeploys the `demo/hello` + `demo/caller` pair from [Article 2](02-what-runs.md) — with the real database `demo/caller` has been asking for since that article, exactly like [Article 7](07-assemble-and-break.md) bound it for Docker — against real Podman instead, and proves the same cross-container call, the same weak-dependency degradation, and the same generated file all still hold.

**Prerequisites:** a working rootless Podman install. On Ubuntu/Debian specifically, `down` needs one environment prerequisite met first — [the Podman environment checklist](../07-patterns/11-podman-environment-checklist.md) has the one-line self-check and the fix. Build the two images this series already used, this time with `podman build`, since Podman keeps its own separate image storage from Docker's:

```bash
podman build -t brickkit-demo/hello:1.0.0 tests/components/demo-hello
podman build -t brickkit-demo/caller:1.0.0 tests/components/demo-caller
```

## Deploy a real database — the identical recipe Article 7 used for Docker

```bash
podman run -d --name guide-pg -p 15432:5432 \
  -e POSTGRES_PASSWORD=devpass -e POSTGRES_DB=callerdb \
  postgres:16-alpine
```

## The project config

Same two components, same resource binding as Article 7 — `brickkit.yaml` doesn't change at all, `host.docker.internal` included:

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    expose: true
    exposePort: 18098
  - id: demo/caller
    version: 1.0.0
    expose: true
    exposePort: 18099

resources:
  - kind: database
    engine: postgresql
    id: caller-db
    host: host.docker.internal
    port: 15432
    username: postgres
    password: ${DB_PASSWORD}
    bindings:
      - componentId: demo/caller
        database: callerdb
```

`host.docker.internal` keeping its name regardless of engine isn't an oversight — it's the one literal string the platform recognizes to auto-add `extra_hosts: host.docker.internal:host-gateway` (AGENTS.md §5.2), and `host-gateway` is a magic value Podman honors identically to Docker. What actually picks the engine is `override.yaml`:

```yaml
target: podman

components:
  - id: demo/hello
  - id: demo/caller
```

## Deploy it for real

```bash
export DB_PASSWORD=devpass
brickkit up
```

```
🚀 Starting project hello-world (deploy.target: podman)
⚠️ Warning: optional dependency missing: demo/bus@1.0.0
   Affected component: demo/caller@1.0.0
   Reason: The component was not found in any install source
   Impact: This component's environment variable DEMO_BUS_ENDPOINT will not be injected
   💡 Degrading gracefully for a missing optional dependency is the component's own responsibility; to enable it, confirm it has been published and is available from an install source
📋 Component state calculation:
   ✅ demo/hello@1.0.0   starting (demo/caller needs it)
   ✅ demo/caller@1.0.0  starting (top-level)

📋 Start order (topological sort):
   1. demo-hello-1-0-0   no dependencies
   2. demo-caller-1-0-0  ← depends on 1

Can start on their own: demo-hello-1-0-0 (no dependencies)
Longest dependency chain (2 levels): demo-hello-1-0-0 → demo-caller-1-0-0

Dependency graph:
   demo/caller@1.0.0 → demo/hello@1.0.0
                     → demo/bus@1.0.0 (optional, not installed)
📄 Generated: .brickkit/generated/compose.yaml

📌 These base resources have to be running first (the platform doesn't deploy them for you):
   caller-db    postgresql   host.docker.internal:15432  used by demo/caller
      Needs database callerdb (used by demo/caller): CREATE DATABASE "callerdb";
   The databases also have to be created beforehand; if already created there is no need to run it again — creating a database is a one-time operation
   To bring one up quickly for local development: docker compose -f deploy/dev-resources/docker-compose.yaml up -d

🔧 Database migrations that run before startup (on failure that component won't start):
   demo/caller@1.0.0  /app/caller migrate

🔍 Checking image pull permissions... ✅ All passed

🐳 Starting (podman)...
   demo-hello-1-0-0             running (healthy)
   demo-caller-1-0-0            running (healthy)
✅ All components started (2)

💡 View the status: brickkit status
   View the logs: podman compose -p brickkit-hello-world logs -f
```

`compose.yaml`, not `docker-compose.yaml` — the generated file follows the vendor-neutral Compose Specification, and Podman consumes the identical file Docker would (AGENTS.md §5.10). And the logs hint already says `podman compose`, not `docker compose` — the same file, run by the engine actually in charge this time.

## Proving the whole chain actually works

```bash
curl http://localhost:18098/api/v1/hello
```
```json
{"component":"demo/hello","greeting":"Hello","message":"Hello, I'm demo/hello@1.0.0","version":"1.0.0"}
```

`demo/caller` calling `demo/hello` — a real container-to-container call over Podman's own network and DNS, not just an environment variable that looks right:

```bash
curl http://localhost:18099/api/v1/call
```
```json
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:8080","upstream":{"component":"demo/hello","greeting":"Hello","message":"Hello, I'm demo/hello@1.0.0","version":"1.0.0"},"version":"1.0.0"}
```

And the same missing optional dependency from Article 2 is still visibly missing here too:

```bash
curl http://localhost:18099/api/v1/status
```
```json
{"component":"demo/caller","eventBus":"degraded","hello":"http://demo-hello-1-0-0:8080","version":"1.0.0"}
```

`brickkit status` confirms it from the platform's own side:

```bash
brickkit status
```
```
📊 Project status: hello-world (deploy.target: podman)

✅ Running (2 components)
 ┌─────────────┬─────────┬───────────────────┬──────────┐
 │ Component   │ Version │ Status            │ Port     │
 ├─────────────┼─────────┼───────────────────┼──────────┤
 │ demo/hello  │ 1.0.0   │ running (healthy) │ 8080/tcp │
 │ demo/caller │ 1.0.0   │ running (healthy) │ 8080/tcp │
 └─────────────┴─────────┴───────────────────┴──────────┘

📦 Resource status
 ┌───────────┬──────────┬─────────────────────────────┐
 │ Resource  │ Kind     │ Status                      │
 ├───────────┼──────────┼─────────────────────────────┤
 │ caller-db │ database │ reachable (localhost:15432) │
 └───────────┴──────────┴─────────────────────────────┘
```

## A real gotcha: `down` used to be the one thing that didn't work here

```bash
brickkit down
```
```
🛑 Stopping project hello-world
✅ All components stopped

💡 Data volumes were not deleted; database data is still there
   For a full cleanup, run by hand: podman volume rm <volume-name>
   Start again with: brickkit up
```

That command exiting `0` is the whole point of this article existing at all. Rootless Podman's network-teardown helper, `pasta`, needs a `SIGTERM` from `podman` to tear a deployment's network down cleanly — and most Ubuntu/Debian installs' default AppArmor policy blocks that one signal, which is a confirmed distro packaging gap ([containers/podman#27372](https://github.com/containers/podman/issues/27372)), not a BrickKit or Podman bug. It's exactly why `target: podman` was pulled once already and only recently came back (AGENTS.md §5.10): a project that starts but can't be torn down is worse than one that never came up. If `down` fails with `rootless netns: kill network process: permission denied` instead of the clean exit above, that's this exact prerequisite unmet — [the environment checklist](../07-patterns/11-podman-environment-checklist.md) has the fix, and BrickKit's own error already points there.

And the cleanup hint above says `podman volume rm`, not `docker volume rm` — small, but it's the same class of detail as `compose.yaml` not being named after one engine: once Podman is a real target, every hint that names a command has to name the right one.

## Cleanup

```bash
podman rm -f guide-pg
podman rmi brickkit-demo/hello:1.0.0 brickkit-demo/caller:1.0.0
```

---

This is the last article in the current series (see [the guide index](README.md) for what's still planned). For deeper detail on any single mechanism this series walked through hands-on, `docs/en/06-architecture/` and `docs/en/07-patterns/` are where it's explained properly, with real code and real generated output — AGENTS.md §11.2 has the full map.
