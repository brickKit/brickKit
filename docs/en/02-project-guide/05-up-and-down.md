# Starting and stopping

## What `brickkit up` does

```bash
brickkit up
```

```text
🚀 Starting project my-shop (target: docker)
⚠️ Warning: optional dependency missing: demo/bus@1.0.0
   Affected component: demo/caller@1.0.0
   Reason: demo/bus@1.0.0 is not declared in brickkit.yaml
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

🔧 Database migrations that run before startup (on failure that component won't start):
   demo/caller@1.0.0  /app/caller migrate

🐳 Starting (docker)...
   demo-hello-1-0-0             running (healthy)
   demo-caller-1-0-0            running (healthy)
✅ All components started (2)

💡 View the status: brickkit status
   View the logs: docker compose -p brickkit-my-shop logs -f
```

In order:

1. **Load and check consistency.** Read `brickkit.yaml`, the deploy file, `config/`, and every component's
   `component.yaml`. If the three layers don't agree (the deploy file lacks a component, a config file holds a conflict
   block, a `$var:` references something undefined), stop.
2. **Decide what runs.** Who starts this time: a top-level component (nothing depends on it) without a `mode` starts, and
   a component further down starts as long as something above it runs and needs it. Every line states its reason —
   "top-level", "demo/caller needs it", "mode: debug".
3. **Check dependencies and sort.** A missing required dependency is an error; a missing optional one is a warning, and
   its address isn't injected. The dependencies give the start order.
4. **Generate the deployment files.** Resolve config, inject environment variables (dependency addresses, the
   component's own config), write `.brickkit/generated/compose.yaml` (a set of Kubernetes manifests when the target is
   `k8s`). It's a generated file, rewritten every time — don't edit it by hand.
5. **Check images.** See the next section.
6. **Run database migrations.** A component that declares `migration.command` first runs its migration with the same
   image; if the migration fails, that component doesn't start.
7. **Call the engine.** `docker compose up` (or `podman compose`, `kubectl apply`), waiting for every component's health
   check to pass.

`up` is idempotent: run it again with nothing changed, and the engine finds everything in place and touches nothing.
Change the config and run it again, and only the affected containers are recreated.

## The image check: `up` never builds

```text
❌ Error: these images are built locally and have not been built yet
   demo/hello@1.0.0: demo-hello:1.0.0
   demo/caller@1.0.0: demo-caller:1.0.0
   Suggestions:
   1. up never builds on its own (building and deploying are separate): run brickkit build first
   2. Or build just one of them: brickkit build demo/hello@1.0.0
   3. Or build just one of them: brickkit build demo/caller@1.0.0
```

| Component | How `up` checks it |
| --- | --- |
| From a local source (code being developed), or its `component.yaml` says only how to build and names no `image` | The image must already be on this machine (built by `brickkit build`); otherwise it's an error |
| A Git / market component that names an `image` | The image is on this machine or can be pulled from the registry; otherwise it's an error, with a note that you can build locally instead |

Every problem is listed at once, rather than one at a time. Why it doesn't just build: see [Building and images](11-build-and-images.md).

## `--dry-run`: generate, don't start

```bash
brickkit up --dry-run
```

It goes through the first four steps, writes the deployment files, and stops:

```text
📄 Generated: .brickkit/generated/compose.yaml

🔧 Database migrations that run before startup (on failure that component won't start):
   demo/caller@1.0.0  /app/caller migrate

💡 --dry-run only generates the files and starts no component
   View it: cat .brickkit/generated/compose.yaml
```

It's for reviewing before you act: which components will start, what environment variables each one gets, which
databases will be touched. It's also the most complete check short of running — dependency resolution and the checks on
shells and members happen here, while `lint` deliberately doesn't do them (see [Offline checks](09-lint-and-checks.md)).
`--ignore-shells` together with `--dry-run` verifies that every component can still start on its own, outside its shell.

## `brickkit down`

```bash
brickkit down
```

```text
🛑 Stopping project my-shop
✅ All components stopped

💡 Data volumes were not deleted; database data is still there
   For a full cleanup, run by hand: docker volume rm <volume-name>
   Start again with: brickkit up
```

- **Volumes aren't deleted.** The database's data stays, and the next `up` uses it. To wipe it, run `docker volume rm`
  yourself — deleting data by mistake costs too much, so that step is left to a person.
- **To stop only some components:** write `mode: disable` on their deploy entries and `up`. Their containers are removed,
  components further down that ran only for them stop too, and the deploy file keeps a record, so the next `up` doesn't
  bring them back.
- **`mode: local` components** are processes watched in the foreground by the terminal that ran `up`; they can only be
  stopped there with `Ctrl+C`. `down` can't reach them and only names that session's process ID.

## Common start failures

**A required config item has no value.**

```text
❌ Error: a required component config item has no value
   Missing config: demo/widget@0.1.0 → API_URL
   Reason: The component declares it in configSchema.required without a default — the platform can't derive this one, so the project has to supply it
   Suggestions:
   1. Give it a value in config/demo-widget.yaml:
    API_URL: <value>
   2. The value may be ${ENV_VAR} (real value in .env), $var:NAME (shared, from config/vars.yaml) or file://path
```

The platform would rather stop before starting than let a component run without an address it needs, looking healthy,
with one call path that never works.

**An image doesn't exist.** See the image check above: `brickkit build` first.

**A host port conflict.** Two components opened on the same host port are caught when the deploy file is validated:

```text
❌ Error: deploy.yaml failed validation
   File: deploy.yaml
   components[1].exposePort: conflicts with components[0].exposePort (host port 18080 is already taken)
   Suggestion: Full field reference: docs/en/11-reference/03-deploy-yaml-schema.md (swap en for zh for the Chinese version)
```

When another program on the machine holds the port, Docker reports it when starting the container — pick another
`exposePort`.

**A component doesn't come up, its health check never passes.** Look at its logs first:
`docker compose -p brickkit-my-shop logs <service>` (the `-p` matters: it names this project). More cases are in
[Troubleshooting](../10-troubleshooting/01-up-down-issues.md).
