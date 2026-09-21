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
🔍 Found 2 components in local install source: local-dev
📦 Adding demo/caller@1.0.0
   ├── Manifest ✅
   ├── dependency demo/hello@1.0.0 ✅ pulled (artifacts: 1 file)
   └── artifacts ✅ (1 file)
⚠️ Warning: optional dependency missing: demo/bus@1.0.0
   Affected component: demo/caller@1.0.0
   Reason: The component was not found in any install source
   Impact: This component's environment variable DEMO_BUS_ENDPOINT will not be injected
📦 Adding demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅ (1 file)
✅ Written to brickkit.yaml (2 components)
```

Two things already happened before anything is even running. `add --local` scanned the local source for `demo/caller`, found that it requires `demo/hello`, and pulled that in too — you only asked for one component and got its whole dependency subtree. And `demo/caller` also *optionally* depends on `demo/bus@1.0.0`, which doesn't exist anywhere in this project's sources — the CLI warns about it right away rather than waiting until `up`.

## The full picture, before starting anything

```bash
brickkit up --dry-run
```

```
⚠️ Warning: optional dependency missing: demo/bus@1.0.0
   Affected component: demo/caller@1.0.0
   ...
📋 Component state calculation:
   ✅ demo/hello@1.0.0   starting (demo/caller needs it)
   ✅ demo/caller@1.0.0  starting (top-level)

⚠️ Warning: resource dependencies are not satisfied (--dry-run doesn't block)
   demo/caller@1.0.0: needs kind: database, engine: postgresql (not declared under resources in brickkit.yaml)
📋 Start order (topological sort):
   1. demo-hello-1-0-0   no dependencies
   2. demo-caller-1-0-0  ← depends on 1

Can start on their own: demo-hello-1-0-0 (no dependencies)
Longest dependency chain (2 levels): demo-hello-1-0-0 → demo-caller-1-0-0

Dependency graph:
   demo/caller@1.0.0 → demo/hello@1.0.0
                     → demo/bus@1.0.0 (optional, not installed)
📄 Generated: .brickkit/generated/docker-compose.yaml

🔧 Database migrations that run before startup (on failure that component won't start):
   demo/caller@1.0.0  /app/caller migrate
```

Reading it top to bottom: `demo/hello` runs because `demo/caller` needs it, not because it's top-level itself — `demo/caller` is the one nothing else depends on. The start order puts `demo/hello` first, since `demo/caller`'s required dependency has to be up before it. The dependency graph marks the `demo/bus` edge as weak *and* unresolved — a different marker than a weak edge that simply isn't installed in this particular deployment, so you can tell "doesn't exist anywhere" apart from "exists, just not here." And `demo/caller` also declares a database resource this project hasn't bound yet — `--dry-run` only warns about that (binding resources is its own topic, covered later in this series); running `up` for real here would refuse to start until it's bound.

## Turning a required dependency off

Add `mode: disable` to `demo/hello`'s entry in `brickkit.yaml` and run `--dry-run` again:

```
📋 Component state calculation:
   ⬜ demo/hello@1.0.0   disabled explicitly (mode: disable)
   ⬜ demo/caller@1.0.0  not starting (required dependency demo/hello is not starting)

📋 No component will start this run
   None of the top-level components (the ones no other component depends on) run this time:
      demo/caller@1.0.0  not starting (required dependency demo/hello is not starting)
   The top level itself isn't turned off — what to release is the component named in the reason line above
```

`demo/caller` never had its own `mode` field touched — on its own, being top-level, it would run by default. It stops anyway, because its *required* dependency is force-off, and "whatever depends on it stops too" (AGENTS.md §5.4) doesn't care what the dependent's own `mode` says. The last line is the CLI actively pointing at the fix: the thing to change isn't `demo/caller`, whose behavior here is a consequence, not a cause — it's `demo/hello`.

## Pinning both sides at once is a conflict, not a decision

Leave `demo/hello` disabled, but add `mode: enabled` to `demo/caller` — insisting it must always run, no matter what:

```
❌ Error: required dependency demo/hello is disabled
   Component: demo/caller@1.0.0 (mode: enabled, pinned)
   Dependency chain: demo/caller → demo/hello
   Disabled component: demo/hello@1.0.0
   Suggestions:
   1. Remove mode: disable from demo/hello in brickkit.yaml
   2. Or remove mode: enabled from demo/caller, letting it follow whatever is above it
```

Two explicit, written instructions — "`demo/hello` never runs" and "`demo/caller` always runs" — directly contradict each other once `demo/caller` actually needs `demo/hello`, and the platform refuses to silently pick a winner (AGENTS.md §5.4). This is different from the previous section on purpose: an *unwritten* `mode` on the dependent just follows along quietly; a *written* one that conflicts is a hard stop, because now there are two explicit intents on record instead of one.

---

This project only has a straight two-component chain. Real dependency graphs have diamonds (the same component required through more than one path), and cycles are sometimes valid — [Dependency resolution and start order](../06-architecture/02-dependency-resolution.md) works through both with real examples, along with why eight components starting doesn't mean eight serial steps. Next: [Debug a component locally](03-local-debugging.md) — one of these two components running with breakpoints on your own machine, while the other keeps running exactly as if it were talking to a container.
