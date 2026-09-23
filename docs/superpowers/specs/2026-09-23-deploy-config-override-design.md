# Deploy config override file — working notes (in progress)

**Status: in-progress brainstorm, not a finished spec.** Running capture of what's settled,
updated as the conversation moves — not a final design doc yet. "Confirmed" = decided.
"Corrected" = an earlier "confirmed" entry that turned out wrong and was fixed — kept visible
instead of silently deleted, so the reasoning for the fix isn't lost. "Open" = genuinely
undecided. [Podman engine support](2026-09-23-podman-engine-support-notes.md) is the companion
doc that depends on this one landing first.

## The problem this solves

Two things converged into one design:

1. **Engine selection (docker/podman/k8s) needs a home that isn't `brickkit.yaml` itself as a
   single shared value** — a project-wide value forces the whole team and CI onto whatever one
   engine the file declares.
2. **A real, independent ergonomics problem**: at 70+ components, finding one component in
   `brickkit.yaml` to flip its mode is already painful, and `mode: debug` — inherently personal,
   machine-specific — currently has to live in the same shared, git-reviewed file as everything
   else.

## Confirmed — identity and positioning

**File name: `override.yaml`. Command: `brickkit override`.** Originally named `deploy.yaml` /
`brickkit deploy` — renamed because "deploy" reads as "actually deploy now" (like `up` does), not
"generate a config file." `override.yaml` says precisely what it contains: a set of local
overrides on top of `brickkit.yaml`, nothing more. `brickkit override` creates it the first time,
and refreshes it on later runs (adds lines for new components, drops lines for removed ones, flags
`baseline` drift — see below). Whether "refresh" merges into the deferred `restore` command is
still open, parked for last.

**Two-file model.** `brickkit.yaml` stays the single project-level declaration — identity,
dependencies, resources, config, structural facts. Those are "80% of the file, written once,
basically never touched again" — closer to constraints than operational toggles. `override.yaml`
holds current, local, frequently-changed operational state: engine choice and per-component mode.
Mental model, in the words it was given: *"brickkit 管理项目级别的配置，新配置文件管理用户当下的
配置"* — `brickkit.yaml` manages project-level config, `override.yaml` manages the user's current
config.

**Opt-in, not auto-generated on every `up`.** `brickkit.yaml` alone is a complete, runnable
project. Whoever wants local control turns `override.yaml` on themselves via `brickkit override`.

**Gitignored by default.** Local machine state, not a team decision by default — removable from
`.gitignore` by whoever wants to share it anyway.

## Confirmed — mode placement rules

**`mode: debug` is removed from `brickkit.yaml` entirely — it can only exist in `override.yaml`.**
`brickkit.yaml` may only contain (for a component's own deployment approach): nothing (follow
`deploy.target`/engine), `local`, or `disable`. `debug` needs an IDE, a debugger, a
machine-specific `localPort` — never legitimately committable to a shared file. This closes a real
latent bug in the current model, not a new restriction invented for this design.

**k8s doesn't allow `local`/`debug`/engine-choice at all** — matches the existing Docker-only rule
`mode: debug` already follows today. This is the *same* rule everywhere `servedBy` is used too
(see below) — it's not specific to Docker/Podman, since `servedBy`'s "does this component generate
its own workload" question is target-agnostic.

## Confirmed — engine selection

**Engine is a single global value, never per-component.** One `docker compose`/`podman compose`
invocation runs everything in that file under one engine — mixing engines within one deployment
isn't possible, because containers under different engines don't share a network namespace and the
service-name DNS resolution the whole addressing model depends on (§5.1) doesn't cross that
boundary. Worked example: `erp/backend` on Podman calling its dependency `department/tree` on
Docker via `http://department-tree-1-0-0:8080` would never resolve — separate
rootless-netns/bridge networks. Rejected alternatives for *where* engine choice lives: an
environment variable (not explicit/visible enough), and a CLI flag like `--engine` (already
explicitly rejected in AGENTS.md §4.1's rejection list, same "who guarantees teardown" reasoning
that originally killed Podman support).

**`deploy.target: docker | k8s` in `brickkit.yaml` is unchanged** — still governs which deployment
*file format* gets generated. Docker and Podman consume the identical generated
`docker-compose.yaml` (confirmed directly — `podman compose` shells out to the same
`docker-compose` binary Docker uses), so engine choice doesn't change what gets generated, only
which binary executes it.

## Corrected — entries are exhaustive-with-minimal-default-lines, not sparse

**Earlier claim (now wrong): "only overridden components get a line, unoverridden ones are absent
entirely."** This was walked back. The actual model: `override.yaml`, once it exists, lists every
component the project currently has — but a component with no override is just a bare `- id: X`
line, no other fields. Only actually-overridden components (`mode: local`/`disable`) carry extra
fields. This still keeps the file light at 70-component scale (most lines are one bare id), while
also making the file itself usable as a direct, complete preview — no separate merge/preview
command needed after all (an earlier "proposed" idea in this doc, now superseded and dropped).
**Consequence:** `brickkit add`/`remove` *do* need to keep `override.yaml` in sync after all — `add`
appends a bare-id line, `remove` deletes the corresponding line, whether or not it carried an
override. (The earlier "add mostly doesn't matter under the sparse model" note is dropped along
with the sparse claim it depended on.)

## Confirmed — shell/member semantics

This took several corrections to get right; the final shape:

**A shell is an interface BrickKit provides, not a special built-in type.** Any component that
satisfies the shell-implementer rules (§5.7 / `07-shell-implementers-guide.md`) can be one —
user-implemented, auto-deployable once it follows the rules.

**Shell and its members are bundled at the image level under Docker/Podman.** They start or don't
start as one unit — there's no "half the shell's modules are compiled in, half aren't" at runtime.

**Shell off → each member deploys per its own independent declaration.** This was initially
(wrongly) ruled out on the assumption that a `servedBy` member's own `deployment.image` isn't a
real, standalone-capable artifact — **that assumption was wrong and got corrected**: every
component's Manifest, `servedBy` or not, requires a complete `deployment.image`/`port`/
`healthCheck` — no exception for members. The compose/K8s generators already know how to build a
workload from that declaration; it's the *default* path used for every ordinary component.
`servedBy` currently just skips it unconditionally. `--ignore-served-by --dry-run`'s existing,
documented purpose ("verify every component can still stand alone without servedBy") only makes
sense if standalone operation is a real, expected capability for a properly-built member — further
evidence the original objection was overstated. **What actually needs building**: today,
`internal/shell.Resolve` hard-errors when a member wants to run but its shell isn't
(`shellNotRunningError`, confirmed via code investigation — see below) instead of falling back to
the ordinary per-component generation path. Making that fallback happen is real, but narrower work
than "invent standalone capability from scratch" — it's conditionally skipping the current
skip-generation behavior, not building new generation logic.
**One known remaining gap, not yet resolved:** resource bindings. §5.7 today treats a member's own
`resources[].bindings` entry as unnecessary — the shell's binding covers it. A member running
standalone needs its own binding, which typically won't be present in `brickkit.yaml` today (it
was considered redundant when `servedBy` is in effect). This isn't a blocker to the design, but it
is a real detail — the CLI would need to error clearly on "this component needs its own binding to
run standalone" rather than silently starting with no connection.

**Shell on → members merge in, *except* a member pinned to a specific version the shell doesn't
provide.** New, sharp point from this round: BrickKit already supports multiple versions of the
same component coexisting project-wide (§5.1) — if the shell bundles version 1.0.0 of some
component but another caller elsewhere in the project needs 2.0.0 specifically, that 2.0.0 instance
can't be satisfied by the shell (which only has 1.0.0 baked in) — it deploys standalone regardless
of whether the shell itself is running. **Schema consequence, not yet worked out in detail:**
`override.yaml`'s shell `members:` list likely needs to pin the version per member, not just the
bare component ID, since "is this member satisfied by the shell" is a version-sensitive question.

**`override.yaml` structure reflects this**: a shell's entry lists its members nested underneath
it (avoids repeating "I belong to shell X" on every member line, and doubles as the visual grouping
that makes the file previewable at a glance):

```
(illustrative, not a finalized schema — field names/nesting still open)
- id: department/tree        # standalone component, no override

- id: erp/backend            # shell
  members:
    - id: people/basic
    - id: auth/rbac
      mode: disable            # exclude this one module from the shell's active member set
                                # while the shell keeps running (BRICKKIT_SERVED_MEMBERS mechanism,
                                # already exists today — not new)
```

A member's own `mode: disable` *while its shell is running* means something different from the
shell itself being off: it excludes that one module from `BRICKKIT_SERVED_MEMBERS` (an existing
mechanism — the shell, if compliant, skips initializing it), with no container generated for it at
all. The shell being off is the other, separate case described above (member deploys standalone
per its own declaration).

## Open — two points raised this round, not yet resolved

- **Does `servedBy`'s direction need to flip?** Current mechanism: a member declares
  `servedBy: <shell-id>@<version>` pointing at its shell. Raised this round: since a shell is "in
  some sense also a component, just typed as shell," should the shell instead declare its own
  `members: [...]` list? These carry the same information but very different maintenance
  properties — today, adding a member only touches that member's own file; inverted, the shell's
  file would need editing on every member added/removed. Explicitly parked as "maybe a better way
  exists" — not decided either way.
- **Does `mode: local` for a shell-type component need a different start-command mechanism?**
  Ordinary components auto-detect their start command from language marker files (`go.mod`,
  `package.json`, `pom.xml`). Raised this round: a shell is a custom, multi-module program that
  marker-file detection may not cleanly identify the right entrypoint for — the guess is the user
  will likely need to supply an explicit start command instead of relying on auto-detection. Not
  verified against the actual `mode: local` detection code yet — needs a real look before treating
  it as fact.

## Open / explicitly deferred

- **`restore` semantics.** How "reset `override.yaml` to defaults" relates to the existing
  `brickkit restore` command. Explicitly parked — "we'll talk about this last."
- **Exact YAML schema.** Shape is much clearer now (bare-id lines for defaults, nested
  `members:` under a shell entry, per-member version pinning likely needed) but not yet written as
  a real schema.
