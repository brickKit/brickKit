# Deploy config override file (`override.yaml`) — design

**Status: brainstorm complete, this is the design spec.** Companion doc:
[Podman engine support](2026-09-23-podman-engine-support-notes.md) (depends on this design landing
first — the `target`/engine selection mechanism below is what lets a user actually choose Podman
locally). This spec is the input to `writing-plans`, not itself an implementation plan.

## 1. Problem

Two needs converged into one design:

1. **Engine/target selection needs a home that isn't a single shared `brickkit.yaml` value.**
   `deploy.target` today is one project-wide field — if choosing Podman meant writing
   `deploy.target: podman` directly into `brickkit.yaml`, that forces the whole team and CI onto
   whatever one engine the file declares, breaking the moment one teammate's machine doesn't (or
   can't) run it.
2. **A real, independent ergonomics problem.** At 70+ components, finding one component in
   `brickkit.yaml` to flip its mode is already painful, and `mode: debug` — inherently personal,
   machine-specific (an IDE, a debugger, a `localPort` free on *this* machine) — currently has to
   live in the same shared, git-reviewed file as everything else. Committing `mode: debug` was
   never actually safe: whoever pulls that commit suddenly has that component trying to run as a
   bare process on *their* machine.

## 2. Architecture

**Two files, not a merge/overlay of many.** This is a deliberate divergence from an overlay/base
pattern (which 005 §9.9 already argued against for multi-environment config, for good reason — a
reader shouldn't need to hold a merge algorithm in their head to know what's actually running).
Here there's no merging: `override.yaml`, when present, is either silent on a topic (defer
entirely to `brickkit.yaml`) or explicit about it (fully replaces `brickkit.yaml`'s value for that
one thing). No partial-field overlay, no inheritance chain.

| | `brickkit.yaml` | `override.yaml` |
|---|---|---|
| Contains | Identity, dependencies, resources, config, structural facts (`servedBy` grouping, `deploy.target`) | Current, local, frequently-changed operational state: target/engine choice, per-component mode |
| Changes | Rarely — "80% of the file, written once" | Often — toggled per session, per developer |
| Git | Committed, reviewed | Gitignored by default |
| Required | Yes — a complete project on its own | No — optional, opt-in |
| Who edits it | Whoever owns the project | Whoever's running it right now, locally |

Mental model: *`brickkit.yaml` manages project-level config; `override.yaml` manages the user's
current config.*

## 3. `override.yaml`: identity

- **File name: `override.yaml`.** (Not `deploy.yaml` — renamed because "deploy" reads as "actually
  deploy now," like `up` does, not "a config file." `override.yaml` says precisely what it holds.)
- **Command: `brickkit override`.** Creates the file on first run; on later runs, refreshes it —
  adds a bare line for any new component, drops the line for any removed component, and reports
  `baseline` drift (§7). Re-running `brickkit override` *is* the reset/repair operation — there is
  no separate command for it, and it is deliberately **not** merged into `brickkit restore` (§9).
- **Opt-in, not auto-generated on every `up`.** `brickkit.yaml` alone is a complete, runnable
  project. Whoever wants local control turns `override.yaml` on themselves.
- **Gitignored by default.** Local machine state, not a team decision by default.

## 4. Mode placement rules

`brickkit.yaml` may declare, for a component's own deployment approach, only: nothing (follow
`target`/engine), `local`, or `disable`. **`debug` cannot appear in `brickkit.yaml` at all —
it can only be set in `override.yaml`.** This closes a real latent bug in the current model (see
§1), not a new restriction invented for this design.

`k8s` doesn't allow `local`/`debug`/target-override at all — the existing Docker-only rule
`mode: debug` already follows, now extended uniformly (target-agnostically) to `servedBy` too,
since "does this component generate its own workload" is a target-agnostic question (§6).

## 5. Target/engine selection

**Field: `target` (not `engine`) + `targetBaseline`.** Renamed mid-design once it became clear
this field can express more than "docker vs. podman" (§5.2) — `target` matches `brickkit.yaml`'s
own `deploy.target` field name, which is what it's actually overriding.

### 5.1 Why a single value, never per-component

One `docker compose`/`podman compose` invocation runs everything in that file under one engine —
containers under different engines don't share a network namespace, and the service-name DNS
resolution the whole addressing model depends on (§5.1 of the platform's own address spec) doesn't
cross that boundary. Worked example: `erp/backend` on Podman calling its dependency
`department/tree` on Docker via `http://department-tree-1-0-0:8080` would never resolve — separate
rootless-netns/bridge networks. `target` is necessarily one project-wide (locally-overridable)
value.

Rejected alternatives for *where* this choice lives: an environment variable (not
explicit/visible enough), and a CLI flag like `--engine`/`--target` (AGENTS.md §4.1 already
rejects this shape of flag — "`deploy.target` stays the declaration, never a CLI flag" — for the
same "who guarantees teardown" reasoning that originally killed Podman support). `override.yaml`
is the explicit, visible, file-based alternative that respects that constraint.

### 5.2 Downgrade-only: the resolution to the k8s risk

`target` may take `docker`, `podman`, or `k8s` — but **only in the downgrade direction**:

- `brickkit.yaml`'s `deploy.target: k8s` → `override.yaml` may set `target: docker` or
  `target: podman`. **Always safe**: if the project's default is k8s, every k8s-specific field
  (`namespace`, `ingressClass`, `podSecurity`, …) is already populated by construction — the
  project couldn't run in its own declared default otherwise. Compose generation needs none of
  those fields, so downgrading never hits a missing-configuration case.
- `brickkit.yaml`'s `deploy.target: docker` or `podman` → `override.yaml` **may not** set
  `target: k8s`. If a project's default is docker/podman, its k8s-specific fields are typically
  unpopulated (nothing requires them). Allowing a local upgrade to k8s would risk generating
  incomplete manifests against fields that were never required to exist. **To use k8s, the project
  itself must declare `deploy.target: k8s` in `brickkit.yaml`** (which forces populating the
  necessary fields as part of that same edit) — there is no local shortcut around it.
- `brickkit up` (and `brickkit override` itself, when writing/validating `target`) must reject an
  attempted upgrade with a clear error naming the direction that's disallowed and why.

Real, motivating use case for the allowed direction: a project defaults to k8s (CI/staging), but a
developer wants `mode: debug` on one component — unavailable under k8s — so they downgrade to
`docker` locally, which is both simpler (no local cluster needed) and unlocks debug in the same
move.

### 5.3 `targetBaseline`, only when `target` is set

Records what `brickkit.yaml`'s `deploy.target` was when this override was last confirmed. Always
present once `target` is set (`deploy.target` is a required field in `brickkit.yaml`, never
"unset," unlike a component's `mode`). Drift check: current `deploy.target` vs. recorded
`targetBaseline` — mismatch means the project's own declared target changed since this override
was last reviewed, independent of whether `target`'s current value still makes sense given that
change (§7 covers the general mechanism).

## 6. Shell/member semantics

This is the part of the design that took the most correction; stated as final rules, with the
reasoning kept because it's what makes the rules non-obvious but correct.

### 6.1 The platform's control boundary

**BrickKit's control over a running workload stops at "start it or don't." What runs inside is
never something the platform can reach into — container or bare process, no exception.**

- **`servedBy` stays as-is: a member declares `servedBy: <shell-id>@<version>`, not the reverse.**
  Considered flipping it (a shell declaring its own `members: [...]`) since a shell is "in some
  sense also a component." Rejected: member-declares-shell gives "a component belongs to at most
  one shell" as a **syntactic** guarantee (one field, one value — literally not expressible
  otherwise); the flipped direction would make it a **semantic** rule requiring active
  cross-shell-list validation to catch a component appearing under two different shells — violable
  until something checks for it, not violable by construction. `override.yaml`'s nested
  `members:` display (§8) already provides the readability a flip would have bought, so there's no
  remaining reason to also change `brickkit.yaml`'s direction.
- **Shell and its members are bundled at the image level under Docker/Podman** — they start or
  don't start as one unit; there's no "half compiled in" at runtime.
- **Shell running → each member's own `mode: disable` (if set) excludes it from
  `BRICKKIT_SERVED_MEMBERS`, an existing, already-shipped mechanism — but it is a *hint*, never
  enforced.** AGENTS.md §5.7 already says this plainly: "A compliant shell **may** read it to skip
  initializing... this is **optional**; the platform **never checks** whether a shell actually
  honors it." Two mechanisms were explored and rejected as ways to get real enforcement — kept for
  the record since the *reasoning* is the reusable part:
  - *"`sync` archives the disabled member's local checkout, so the shell's build can't find it."*
    Rejected: `sync` only moves the developer-facing checkout under `components/<scope>/<name>/` —
    a workspace §9.17 already establishes no build or deploy step reads. A shell importing a
    member's code does so through the language's own package manager (a Go module fetched from the
    member's own published repository, an npm dependency, a Maven artifact) — entirely independent
    of BrickKit's local checkout. Proof by construction: a machine that never ran `brickkit sync`,
    or never installed BrickKit at all, builds the shell identically.
  - *A dedicated compile-command field for `mode: local`, so BrickKit orchestrates build-then-run.*
    Rejected: puts BrickKit back in the business of deciding *when* to recompile — exactly the kind
    of per-language build-lifecycle judgment §4.1 already argues the platform shouldn't make. The
    single start command (auto-detected or user-supplied) is responsible for guaranteeing it
    reflects current code on every invocation (`go run .`, `mvn spring-boot:run`, `npm run dev`, or
    a watch-mode tool are all already self-contained single commands).
- **Shell not running → each member deploys per its own independent Manifest declaration, treated
  exactly like an ordinary standalone component.** Initially (wrongly) ruled out on the assumption
  that a member's `deployment.image` isn't a real, standalone-capable artifact. **Corrected**:
  every component's Manifest, `servedBy` or not, requires a complete
  `deployment.image`/`port`/`healthCheck` — no exception for members. The compose/K8s generators
  already know how to build a workload from that declaration; it's the *default* path used for
  every ordinary component. `servedBy` currently just skips it unconditionally.
  `--ignore-served-by --dry-run`'s existing, documented purpose ("verify every component can still
  stand alone without servedBy") only makes sense if standalone operation is a real, expected
  capability for a properly-built member.
  **What actually needs building**: `internal/shell.Resolve` today hard-errors
  (`shellNotRunningError`, confirmed via code) when a member wants to run but its shell doesn't.
  Making that a fallback to the ordinary per-component generation path is real work, but narrower
  than "invent standalone capability from scratch" — it's conditionally skipping the current
  skip-generation behavior.
- **Shell running → members merge in, *except* a member pinned to a version the shell doesn't
  provide** — BrickKit already supports multiple versions of the same component coexisting
  project-wide; a version instance the shell doesn't bundle deploys standalone regardless of the
  shell's own state. **This is fully automatic, computed from `brickkit.yaml`'s own dependency
  declarations — `override.yaml` never represents or lets a user toggle it.** `override.yaml` only
  speaks for the project's own on/off switches, not for instances a required-dependency edge
  forces to run. (This is why component entries in `override.yaml` use bare `id`, never
  `id@version` — see §8.)

### 6.2 Known remaining gap: resource bindings

§5.7 today treats a member's own `resources[].bindings` entry as unnecessary — the shell's binding
covers it. A member falling back to standalone needs its own binding, typically absent in
`brickkit.yaml` today (considered redundant under `servedBy`). **Not a blocker, but a real detail
not yet fully designed**: the CLI needs to error clearly ("this component needs its own resource
binding to run standalone") rather than silently starting with no connection. Exact error timing
(generation-time vs. a `lint` check) is left for the implementation plan.

### 6.3 `mode: local` for a shell needs no new mechanism

Verified against the actual code (`internal/runcmd`, `internal/manifest/types.go`), not
speculation. Detection is architecturally blind to `servedBy`/shell status — zero references to
`ServedBy` anywhere in the detection path; a shell and an ordinary component are probed
identically. Detection failures happen for structural reasons unrelated to being a shell (multiple
`cmd/*` entrypoints in Go, Maven/Gradle multi-module layouts) — a shell is *plausibly more likely*
to hit these given its aggregating nature, but that's a consequence of its own complexity, not a
shell-specific code path. An explicit override already exists and needs no new design:
`component.yaml`'s `local:` block already has `runCommand` (bypasses detection) and `language`
(disambiguates the adapter) — usable today by any component, shell or not. If a shell's structure
defeats auto-detection, its author sets `local.runCommand` in the shell's *own* `component.yaml` —
a structural, rarely-changed fact (same bucket as `resources`), not something that belongs in
`override.yaml`. (One real, separate gap: no existing doc discusses `mode: local` × `servedBy`
interaction — a documentation task for whenever this ships, not a design question.)

## 7. Staleness / drift detection

**Content-based, never timestamp-based.** File mtimes don't survive `git checkout`/`pull`/`clone`
reliably (they reset to "now" regardless of actual edit history), so time-based drift detection
would misfire constantly across a team.

Two structural checks:

- **Dangling entries**: an `override.yaml` entry referencing a component ID no longer declared in
  `brickkit.yaml` — always an error.
- **`baseline` mismatch** (the field is `targetBaseline` for the target override, `baseline` for a
  component-level override): each override records what `brickkit.yaml` declared for that same
  thing at the moment the override was last confirmed. The check compares that recorded value
  against `brickkit.yaml`'s *current* value — a mismatch means `brickkit.yaml` changed since the
  override was last reviewed, independent of whether the override's own current value happens to
  differ from (expected, fine) or coincidentally match the old default.
  - **`baseline` is written only when `brickkit.yaml` already has an explicit, specific value for
    that thing at override-set time** (a deliberate choice, made knowingly): `targetBaseline` is
    always present once `target` is set (`deploy.target` is required, never unset); a
    component-level `baseline` is often absent, since most components have no `mode` in
    `brickkit.yaml` at all. **Consequence, accepted deliberately**: this gives up detecting the one
    case worked through earlier in this brainstorm — `brickkit.yaml` going from "no `mode`" to an
    explicit `mode: local` for a component that has an unrelated override sitting on it in
    `override.yaml` won't be flagged, because no baseline was ever recorded for the "absent" state.
    Traded for a cleaner file (no absent-state placeholder noise on the common case).

Both checks are deterministic, offline, content-only — fit naturally as a new `brickkit lint` rule
(and a non-blocking note from `up` when `override.yaml` is in use), not a new kind of mechanism.

## 8. `override.yaml` schema

Entries are **exhaustive with minimal default lines**, not sparse. Every component the project
currently has gets a line; an unoverridden component is a bare `- id: X`. Only actually-overridden
components carry extra fields. This keeps the file light at 70-component scale (most lines are one
bare id) while making the file itself directly usable as a preview — no separate merge/preview
command needed. A shell's members are nested under its own entry (avoids repeating "I belong to
shell X" on every member line, and doubles as the grouping that makes the file scannable).

```yaml
# override.yaml — local deployment overrides on top of brickkit.yaml.
# Generated/refreshed by `brickkit override`. Not authoritative — brickkit.yaml
# stays the source of truth for everything not listed here.

target: podman               # docker | podman | k8s — downgrade-only from brickkit.yaml's
                              # own deploy.target (§5.2); omit to just follow deploy.target
targetBaseline: docker        # deploy.target's value when this override was last confirmed
                              # (always present once `target` is set)

components:
  - id: department/tree      # standalone component, no override — bare line

  - id: erp/backend           # shell — itself a plain component, no override here either
    members:
      - id: people/basic         # servedBy member, no override — bare line
      - id: auth/rbac
        mode: disable             # excluded from this run's BRICKKIT_SERVED_MEMBERS while
                                    # the shell keeps running — not guaranteed (§6.1). No
                                    # `baseline`: brickkit.yaml never had a `mode` for it.

  - id: infra/redis-event-bus
    mode: local
    localPort: 8082              # this machine's port 8080 (brickkit.yaml's suggested
                                   # default) was already taken — overridden here instead

  - id: payment/gateway
    mode: debug
    localPort: 9091
    baseline: local                # brickkit.yaml already declares `mode: local` for this
                                     # component — this override upgrades it to `debug` for
                                     # active debugging; `baseline` lets drift detection
                                     # notice if that project-level declaration changes
```

`localPort` for `mode: local` can still live in `brickkit.yaml` as the project's suggested default
(unchanged from today — most developers' machines don't collide with it); `override.yaml` only
carries `localPort` when overriding it, or when the override itself is `mode: debug` (which can
only ever exist in `override.yaml`, so its `localPort` has nowhere else to live).

No version numbers anywhere in this file — see §6.1's last bullet.

## 9. Command integration

| Command | Change |
|---|---|
| `brickkit override` | New. Creates on first run, refreshes on later runs (also the reset/repair operation — see §3) |
| `brickkit add` | Appends a bare-id line to `override.yaml` if it exists |
| `brickkit remove` | Deletes the corresponding line from `override.yaml` if it exists |
| `brickkit sync` | Conditional source: `override.yaml` absent → cascade reads `brickkit.yaml` alone, unchanged; present → cascade also reads its overrides |
| `brickkit up` | Reads `override.yaml` when present and applies target/mode overrides. **Must ignore `override.yaml` (with a warning) when `--config` points at a non-default file** — prevents a personal local override accidentally applying to a `brickkit.prod.yaml`-style run (§10) |
| `brickkit status` | Reads `override.yaml` so a component that isn't running because of a local `disable` is labeled as such, not left unexplained |
| `brickkit graph` | **Does not** read `override.yaml` — stays scoped to `brickkit.yaml` alone. `brickkit graph > graph.mmd` is a shareable, committed artifact; its output must not vary by who generated it locally |
| `brickkit lint` | New rule: the two staleness checks from §7 |
| `brickkit restore` | Scope unchanged (still resets `mode` + component-source layout to last git commit). New: prints a hint afterward suggesting `brickkit override` be re-run, since `restore` just rewrote the values `override.yaml`'s baselines were tracking |
| `brickkit up --ignore-served-by` | **Kept, not removed.** Serves a different purpose than `override.yaml`'s per-component `disable`: bulk, one-shot verification ("does every member work standalone") typically run in CI with `--dry-run`. Replicating that via `override.yaml` would mean manually disabling every shell in the project, one at a time, easy to under-cover by omission — not a real substitute |

## 10. Multi-environment safety

`override.yaml` is scoped to local/dev usage against the *default* `brickkit.yaml` only. A
`brickkit.prod.yaml`-style environment file should deploy exactly as written, never modified by a
developer's personal local overrides. **Rule: `override.yaml` only applies when `up` is run
against the default `brickkit.yaml`** (no `--config`, or `--config` explicitly pointing at it).
Whenever `--config` points elsewhere and an `override.yaml` happens to exist in the directory, `up`
must **warn explicitly that it's being ignored** — neither silently applying it (risk: an
accidental personal override reaching a prod-style deployment) nor silently ignoring it without
saying so (risk: a developer assumes their override is in effect when it isn't).

## 11. Explicitly out of scope for this design

- **The actual Podman `engine.Engine` implementation.** Covered by the companion doc.
- **CI regression guard for Podman.** Companion doc.
- **Backward compatibility / migration for existing `mode: debug` usage in `brickkit.yaml`.**
  Explicitly not needed right now — there is currently exactly one user of this codebase. Revisit
  if/when this ships to others.
- **Redesigning `brickkit.yaml`'s broader structure for ergonomics beyond what's described here.**
  An earlier, much larger version of this brainstorm considered splitting far more of
  `brickkit.yaml` into a second file; that was narrowed down to exactly what's in this doc. Nothing
  else about `brickkit.yaml`'s shape is in scope.
- **Resource-binding error UX for standalone-fallback members** (§6.2) — flagged as real, not
  designed in detail; leave for the implementation plan.

## 12. Testing considerations for the eventual plan

- `internal/shell.Resolve`'s new standalone-fallback path (§6.1) needs the same rigor as any other
  generation-path change — unit tests plus a real end-to-end run (a shell disabled, its member
  actually starting standalone and answering a real request), mirroring how the Podman
  investigation validated real lifecycle behavior rather than trusting the design on paper.
- `brickkit override`/`add`/`remove`/`sync`/`lint`/`status`/`up`/`restore` each get new test
  coverage for their `override.yaml` interactions described in §9.
- The multi-environment safety guard (§10) needs an explicit test: `override.yaml` present,
  `--config brickkit.prod.yaml` used, assert the warning fires and the override doesn't apply.
- The target downgrade-only rule (§5.2) needs a test asserting the upgrade direction is rejected
  with a clear error, and the downgrade direction succeeds without requiring k8s-specific fields to
  be present.
