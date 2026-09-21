---
name: brickkit-assemble
description: Use when adding or removing components in a BrickKit project, changing what's on or off, starting or stopping the whole stack, or checking what's running. Covers when to use add / remove / fetch / sync / up / down / status, how dependency resolution and start order are computed, and the "follows the layer above it" rule for what starts. Applies when the user mentions brickkit.yaml's components / enabled fields, or asks "how do I add / turn off a component" or "why isn't it starting".
---

# Assembling a BrickKit project

## When to use this skill

- Adding a component to the project, or removing one
- Keeping some components from starting this run
- What `brickkit up` starts doesn't match what you expected
- Checking what's currently running
- Tidying up the component source piling up under `components/`

## Where you'll guess wrong

**1. Versions must be exact — there are no version ranges.**

`1.2.0` is fine. `^1.2`, `~1.2`, `1.2.x`, `latest` are all rejected — this isn't unfinished, it was
argued through and rejected. `brickkit add` without a version takes the latest installable version
from the install source, then **pins the exact version to disk**.

**2. `brickkit add` never writes an `enabled` field.**

A component that gets added has no `enabled` in the config. That's not an oversight — not writing
it means "follow the layer above it," which is the default and recommended state. Don't add
`enabled: true` just to "be explicit" — it means something entirely different (see the next point).

**3. `enabled` has three states, and two of them are pinned.**

| Written as | Meaning |
| --- | --- |
| **not written** | Follows the layer above it. Top-level (nothing depends on it) runs by default; a lower one follows whatever's above it |
| `enabled: true` | **Always runs**, ignoring what's above it. If its required dependency is turned off, it **errors** — two conflicting intents |
| `enabled: false` | **Never runs**. Whatever depends on it stops too; anything pinned `enabled: true` on top of it errors |

To narrow what runs this time, turning off `enabled: false` on the top-level thing is enough —
everything below it stops too. **Don't turn things off one by one.**

**4. Required and optional dependencies are treated the same for start/stop.**

If something above it only weakly depends on it, it still follows along and runs. `optional: true`
only controls two things: a missing one only warns (doesn't block) at resolution time, and its
`*_ENDPOINT` variable isn't injected while it isn't running. It has nothing to do with whether it
starts.

**5. A component shared by several things above it is never taken down by accident.**

As long as at least one thing above it is still running, it runs. So turning off one of them never
drags a shared lower-level component down with it.

**6. `sync` only moves directories, it never touches containers.**

It moves the source of components that aren't starting this run into `components/.archived/`, using
exactly the same decision `up` uses. No running container is affected, and it never changes who
`up` would start. The whole directory moves, `.git` included.

**7. `remove` deletes the source directory too, including an archived copy.**

It doesn't just drop the entry from the config. You must specify a version when multiple versions
coexist.

**8. `fetch` writes no config and deploys nothing.**

It only downloads the artifacts to `.brickkit/artifacts/<versioned-service-name>/`. Use it to call
another project's service across project boundaries — it isn't "a lightweight `add`."

## How the mechanism works

**Start/stop is computed as "who doesn't run."** Starting from `enabled: false` and propagating
upward gives a least fixed point; everything else runs. So two components that weakly depend on
each other in a cycle need no special-casing — nothing sits above the cycle, so both are top-level
and both run.

**`up`'s order is a topological sort** (dependencies start first). To see who would start this run,
and in what order, `brickkit up --dry-run` only generates the deployment files for review — it
starts nothing. To see the dependency graph directly (who depends on whom, which edges are
optional, which nodes are greyed out because they won't start this run), use `brickkit graph` — it
prints Mermaid text, and saving it as a `.mmd` file lets GitHub render it directly.

**Every line of CLI output carries its reason**: `starting (top-level)` / `starting (enabled:
true)` / `starting (X needs it)`. When a component doesn't come up, read that reason first — it
states exactly where the decision came from.

**Coexisting versions is a project-level capability.** `brickkit.yaml` can list `people/basic@1.0.0`
and `@2.0.0` side by side, for different callers to each use their own — they're two
non-conflicting service names. But **within a single `component.yaml`'s `dependencies`, one
component ID can only appear once** (see the `brickkit-component` skill for why).

**No overlay / inheritance / merge.** Multiple environments means each environment gets its own
complete, self-contained config file, selected with `--config`, e.g.
`brickkit up --config brickkit.prod.yaml`.

## Where to dig deeper

(The `docs/...` and `AGENTS.md` paths below all live in the BrickKit repository
<https://github.com/brickKit/brickKit>; every article under `docs/` has an `en/` and a `zh/`
version, content-equivalent.)

- Flags: `brickkit <command> --help`. This skill deliberately doesn't duplicate the flag reference
- Every command's full behavior: `docs/en/06-architecture/09-cli-reference.md`
- The complete rules for `enabled` and start/stop: `docs/en/06-architecture/08-brickkit-yaml-reference.md`
  (the `enabled` field), root `AGENTS.md` §5.4
- Install, assemble, upgrade, and dependency-resolution detail: `docs/en/06-architecture/02-dependency-resolution.md`,
  `docs/en/03-guide/06-assemble-and-break.md`, `docs/en/03-guide/05-upgrades-and-versions.md`
