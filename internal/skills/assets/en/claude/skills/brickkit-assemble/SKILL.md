---
name: brickkit-assemble
description: Use when adding, removing or upgrading components in a BrickKit project, keeping two versions side by side, changing what's on or off, starting or stopping the stack, or checking what's running. Covers add / remove / upgrade / fetch / deps / sync / up / down / status, brickkit.yaml as a lock file, the default version and requiredBy, how add/remove/upgrade keep deploy.yaml, deploy.local.yaml and config/ in step, and the "follows the layer above" rule for what starts. Applies when the user mentions brickkit.yaml's components, requiredBy, mode, upgrade, or asks "how do I add / upgrade / turn off a component" or "why isn't it starting".
---

# Assembling a BrickKit project

## When to use this skill

- Adding a component to the project, removing one, or moving it to a newer version
- A dependency needs a different version of something already in the project
- Keeping some components from starting this run
- What `brickkit up` starts doesn't match what you expected
- Checking what's running, or what depends on what
- Tidying up the component source piling up under `components/`

## Where you'll guess wrong

**1. Versions must be exact, and `brickkit.yaml` is a lock file.**

`1.2.0` is fine; `^1.2`, `1.2.x`, `latest` are rejected by design. `brickkit add <id>` without a
version takes the latest one from the install source and **pins it**. Resolution only ever uses
versions declared in `brickkit.yaml`: a required dependency whose version isn't there is an error
that tells you to `brickkit add` it — nothing is fetched behind your back. An undeclared *optional*
dependency is simply absent.

**2. Don't hand-edit the three layers — let the commands do it.**

`brickkit add`, `remove` and `upgrade` write `brickkit.yaml`, the entries in `deploy.yaml` (and
`deploy.local.yaml` if it exists) and the skeleton in `config/` together, and restore everything if
the result wouldn't load. Adding a line to `brickkit.yaml` by hand leaves the deploy file without its
entry, and every command then refuses with `DEPLOY_INCONSISTENT`. Dependencies are never written in
`brickkit.yaml` at all — they come from each `component.yaml`; `brickkit deps` prints the tree.

**3. The default version is the line without `requiredBy`.**

A component can have several lines in `brickkit.yaml`. Exactly one has no `requiredBy` — that is the
**default version**, and a bare id means it everywhere: the bare deploy entry (`- id: erp/backend`),
the unversioned config file (`config/erp-backend.yaml`), the local repo, a bare shell member. A line
with `requiredBy: [crm/web]` is a compatibility version kept only because `crm/web` needs it; its
deploy entry is `- id: erp/backend@1.0.0` and its config file is `config/erp-backend@1.0.0.yaml`.

**4. `add` of another version is an error — use `upgrade`.**

When a *dependency* needs another version, `add` writes the `requiredBy` line itself. But
`brickkit add erp/backend@2.0.0` when `erp/backend` is already present is refused: moving the
default is `brickkit upgrade erp/backend@2.0.0`. `upgrade` with no argument moves every component
that has a newer version, never downward; the old version stays (with `requiredBy`) only if
something still depends on it, otherwise it is removed and its config archived. Try it first with
`brickkit upgrade --dry-run` — it runs on a temporary copy and writes nothing. It is all or nothing.

**5. `upgrade` migrates config key by key — and can leave a deliberate duplicate key.**

Keys you wrote are copied if the new schema still has them; keys you never wrote follow the new
defaults (that's why `add` writes optional keys as commented lines — leave them commented). If you
changed a key whose default also changed, that's a conflict: in a terminal you choose; with `--yes`
or no TTY a duplicate-key block with a comment is written into the config file, and `up` refuses
until you delete one line. That failure is intentional — don't "fix" it with a YAML formatter, which
silently drops one of the keys.

**6. `mode` lives in the deploy file, and `add` never writes it.**

Not writing `mode` means the component follows the layer above it: a top-level component (nothing depends on it)
runs; a lower one runs while anything running needs it. Don't add `mode: enabled` "to be explicit" —
it means something else:

| In the deploy entry | Meaning |
| --- | --- |
| **not written** | Follows the layer above |
| `mode: enabled` | Always runs; if a required dependency is turned off, that's an **error** (two intents conflict) |
| `mode: disable` | Never runs; what depends on it stops too, and anything pinned on top of it errors |
| `mode: local` | Always runs as a bare process BrickKit starts from the local repo (docker / podman; default version only) |
| `mode: debug` | Always runs as a process you start in your IDE (docker / podman) — **only allowed in `deploy.local.yaml`** |

To narrow what runs, put `mode: disable` on the top-level thing. For personal changes (debugging one
component, turning half the stack off on your laptop), use `brickkit local on` and edit
`deploy.local.yaml`, not the team's `deploy.yaml` — see the `brickkit-deploy` skill.

**7. Required and optional dependencies count the same for start/stop.**

`optional: true` only means a missing one warns instead of blocking, and its `*_ENDPOINT` isn't
injected while it isn't running. A component shared by several above it runs while any of them runs.

**8. `remove` archives config and may delete source.**

Its config file moves to `config/.archive/` (re-adding later migrates it back). Its deploy entries go,
versions kept only for it go too, and a removed shell's members move back to the top level. If you
remove the default and one version remains, that one becomes the default. The source directory is
deleted only when the last version goes and nothing in it would be lost (`--force` overrides).

**9. `fetch` writes no config and deploys nothing.**

It only downloads a component's artifacts into `.brickkit/artifacts/<versioned-service-name>/` — for
calling another project's service (generate a client from its contract). It isn't a lightweight `add`.

**10. `sync` only moves directories.**

It moves the source of components that won't start this run into `components/.archived/` (and back),
using exactly `up`'s decision. Containers aren't touched. `brickkit restore` puts `deploy.yaml`'s
`mode` values back to the last commit, for projects that commit `components/`.

## How the mechanism works

**Start/stop is computed as "who doesn't run"** — a least fixed point propagated from
`mode: disable`, so weak-dependency cycles need no special case. **`up`'s order is a topological
sort**. Every line of `up` output carries its reason (`starting (top-level)`,
`starting (mode: enabled)`, `starting (X needs it)`); read it first when something is off.
`brickkit up --dry-run` generates the files without starting anything; `brickkit graph` prints the
graph as Mermaid (greyed nodes won't start; shell members are drawn inside their shell).

**Coexisting versions is a project-level capability**: `erp-backend-1-0-0` and `erp-backend-2-0-0`
are two different service names. Inside one `component.yaml`, a component id can appear only once.

**Adding a shell** brings in the member versions it compiles in and nests them under the shell's
deploy entry; **upgrading a shell** switches to the members the new shell compiles in.

**Environments**: one complete deploy file per environment, `brickkit up -f deploy.prod.yaml`.
`brickkit.yaml` and `config/` are shared; no overlay, no merge. `-f` ignores local mode.

`status` and `down` read the same deploy file as `up`. `graph` and `deps` always read `deploy.yaml`,
never local mode, so their output is the same for everyone. `down` never deletes volumes.

## Where to dig deeper

- Flags and exact behavior: `brickkit <command> --help` (`add`, `remove`, `upgrade`, `deps`, `sync`,
  `up`, `local`). This skill deliberately doesn't duplicate the flag reference
- A component's own guide: `.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md`; the project
  map: `BRICKKIT.md` at the project root
- The platform's full specification: <https://github.com/brickKit/brickKit> and its root `AGENTS.md`
