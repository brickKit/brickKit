# Deploy config override file — working notes (in progress)

**Status: in-progress brainstorm, not a finished spec.** This is a running capture of what's been
settled in conversation, kept up to date as we go, not a final design doc yet. Sections marked
"confirmed" are decided; "proposed" means I raised it and it hasn't been explicitly agreed yet;
"open" means genuinely undecided. [Podman engine support](2026-09-23-podman-engine-support-notes.md)
is the companion doc that depends on this one landing first.

## The problem this solves

Two things converged into one design:

1. **Engine selection (docker/podman/k8s) needs a home that isn't `brickkit.yaml` itself as a
   single shared value** — a project-wide field would force the whole team and CI onto whatever
   one engine the file declares, which breaks the moment one teammate's machine can't run Podman
   (or doesn't want to).
2. **A real, independent ergonomics problem**: at 70+ components, finding one component in
   `brickkit.yaml` to flip its mode is already painful, and the field that's easiest to want to
   toggle for personal, momentary reasons (`mode: debug`, temporarily disabling something to save
   local resources) currently has to live in the same shared, git-reviewed file as everything else
   — mixing a personal, ephemeral choice into a shared, permanent declaration.

## Confirmed

**Two-file model, not a merge/overlay of many files.** `brickkit.yaml` stays the single
project-level declaration — identity, dependencies, resources, config, structural facts like
`servedBy` grouping. Those are described as "80% of the file, written once, basically never
touched again" — closer to constraints than operational toggles. A new, optional file
(`deploy.yaml`, confirmed name — a project-root file, deliberately *not* named like the
`.brickkit/generated/` cache convention or the `brickkit.<env>.yaml` full-alternate-config
convention, since it's neither) holds current, local, frequently-changed operational state:
engine choice and per-component mode overrides. Mental model, in the words it was given: *"brickkit
管理项目级别的配置，新配置文件管理用户当下的配置"* — `brickkit.yaml` manages project-level config,
`deploy.yaml` manages the user's current config.

**`deploy.yaml` is opt-in, not auto-generated on every `up`.** `brickkit.yaml` alone is a complete,
runnable project — someone who "just wants to deploy" never needs to touch `deploy.yaml`. Whoever
wants local control (a developer who wants to debug one component, someone who wants Podman
locally) turns it on themselves.

**Gitignored by default.** It's local machine state, not a team decision — if someone wants to
share it anyway they remove it from `.gitignore` themselves; the default assumption is personal.

**`mode: debug` is removed from `brickkit.yaml` entirely — it can only exist in `deploy.yaml`.**
`brickkit.yaml` may only contain (for a component's deployment approach): nothing (follow
`deploy.target`/engine), `local`, or `disable`. `debug` requires an IDE, a debugger, and a
machine-specific `localPort` — it was never legitimately a value that should be committable to a
shared file; whoever pulls a commit with `mode: debug` on it would suddenly have that component
try to run as a bare process on *their* machine. This isn't a new restriction invented for this
design — it's closing a real latent bug in the current model.

**Engine is a single global value, never per-component.** A single `docker compose`/`podman
compose` invocation runs everything in that file under one engine — mixing engines within one
deployment isn't possible, because containers under different engines don't share a network
namespace, and the service-name DNS resolution the whole addressing model depends on (§5.1)
doesn't cross that boundary. Concrete example worked through: if `erp/backend` used Podman and its
dependency `department/tree` used Docker, `erp/backend` calling `http://department-tree-1-0-0:8080`
would never resolve — the two containers are on entirely separate rootless-netns/bridge networks.
Rejected alternatives for *where* engine choice lives: an environment variable (not explicit/visible
enough — rejected outright), and a CLI flag like `--engine` (already explicitly rejected in
AGENTS.md §4.1's rejection list, for the same "who guarantees teardown" reasoning that originally
killed Podman support the first time).

**`deploy.target: docker | k8s` in `brickkit.yaml` is unchanged.** It still governs which
deployment *file format* gets generated (compose vs. K8s manifests) — that's a structural fact,
unlike engine choice. Docker and Podman consume the identical generated `docker-compose.yaml` (confirmed
directly — `podman compose` shells out to the same `docker-compose` binary Docker uses), so choosing
between them doesn't change what gets generated, only which binary executes it. `deploy.yaml`'s
engine field is meaningful only when the effective target is `docker` — k8s doesn't allow
`local`/`debug`/engine-choice at all, matching the existing Docker-only rule `mode: debug` already
follows today.

**`deploy.yaml`'s component entries are sparse, not exhaustive.** Only components with an
explicit override (`local` or `disable`) get a line. The default (no override — normal
container deployment following the current engine/target) has *no entry at all* — it's not a
line saying "follow project," it's the absence of a line. This was a deliberate simplification:
an exhaustive per-component table (every one of 70 components gets a row even when 68 of them are
just "default") would recreate the exact "huge file, hard to find things" problem this whole
feature exists to solve, just in a second file.

**Staleness detection is structural/content-based, never timestamp-based.** File mtimes don't
survive `git checkout`/`pull`/`clone` reliably (they all reset to "now" regardless of actual edit
history), so time-based drift detection would misfire constantly across a team. Two structural
checks instead:
- **Dangling entries**: a `deploy.yaml` entry referencing a component ID no longer declared in
  `brickkit.yaml` at all — always an error, flag it.
- **`baseline` field, for the engine value and for component-level `local`/`disable` overrides
  only** (confirmed this round — *not* for the (non-existent) entries of un-overridden components,
  since those aren't snapshots and can't go stale by construction). Each override records what
  `brickkit.yaml` declared at the time the override was last confirmed — the engine's `baseline`
  records what `deploy.target` was; a component override's `baseline` records what that
  component's own `mode` (or lack of one) was in `brickkit.yaml`. The check compares the recorded
  `baseline` against `brickkit.yaml`'s *current* value for that same thing — a mismatch means
  `brickkit.yaml` changed since this override was last reviewed, independent of whether the
  override's own current value happens to differ from the old default (which is expected and
  fine) or coincidentally matches it. Worked example: `people/basic` has no `mode` in
  `brickkit.yaml` (`baseline: <unset>` recorded); later `brickkit.yaml` is edited to add
  `mode: local` for it directly; `deploy.yaml` never asked about this — flagged, regardless of
  what `deploy.yaml`'s own override (if any) currently says, because the thing that changed is the
  project's own declaration, not the user's override.
- This whole check is deterministic, offline, content-only — fits naturally as a new `brickkit
  lint` rule (and a non-blocking note from `up` when `deploy.yaml` is in use), not a new kind of
  mechanism.

**`brickkit remove`**: if the removed component has an entry in `deploy.yaml`, that entry is
removed too (straightforward under the dangling-entry rule — an orphaned entry for a component
that no longer exists would just get flagged as an error anyway, so removing it proactively is
strictly better).

**`brickkit sync`**: conditional source — `deploy.yaml` absent → cascade decision reads
`brickkit.yaml` alone, exactly as today; present → cascade decision also reads its overrides.

## Proposed, not yet confirmed

- **"Quick preview" of full deployment state (including shell/component grouping) is served by a
  live command that merges `brickkit.yaml` + `deploy.yaml` on demand (e.g. `brickkit status` or a
  dedicated preview command), not by reading the raw `deploy.yaml` file.** Since the file itself is
  now sparse by design, it can't show "the full picture" on its own — I proposed resolving the
  original "want to quickly preview shells and components" goal this way instead, but this hasn't
  been explicitly confirmed yet.

## Open / explicitly deferred

- **`restore` semantics.** How "reset `deploy.yaml` to defaults" relates to (replaces, extends, or
  stays separate from) the existing `brickkit restore` command (today: "restores `mode` and the
  component-source layout to the last commit"). Explicitly parked — "we'll talk about this last."
- **Command surface for creating/initializing `deploy.yaml` in the first place.** Not yet named —
  a new command, or a flag somewhere? Undecided.
- **Exact YAML schema.** Sketched informally through examples in conversation, not finalized as a
  real schema (field names, types, where the engine value sits relative to the component list,
  etc.).
- **Whether `brickkit add` still needs special handling for `deploy.yaml`, now that entries are
  sparse.** Earlier in the conversation (before the sparse-file simplification) it was said that
  `add` should auto-append an entry to `deploy.yaml`. Under the sparse model, a normally-added
  component needs *no* entry at all (default = no line) — so this earlier statement may no longer
  apply in the common case. Not yet revisited with the sparse model in mind; flagging so it isn't
  silently carried forward as still-true.
