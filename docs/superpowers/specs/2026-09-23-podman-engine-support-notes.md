# Podman engine support — working notes (paused)

**Status: paused, not a finished spec.** Split off from a combined brainstorm on 2026-09-23. The
design questions below that are marked "depends on" need the companion spec —
[deploy-config-override-design](2026-09-23-deploy-config-override-design.md) (or whatever its
final filename is) — settled first, since engine selection's final shape leans on it. This
document exists so the decisions and open threads from that brainstorm aren't lost, not to
pre-empt the still-open items.

## Why this is even on the table

Motivation was concrete, not speculative: a ~70-component Go/Python project, evaluating whether
Podman's lower engine overhead is worth pursuing. Measured on one Ubuntu 26.04 machine (20-container
sample, linearly extrapolated to 70): Docker ~995MB engine overhead (dockerd + containerd fixed
cost, ~10.9MB/container `containerd-shim`) vs Podman ~158MB (no persistent daemon, ~2.2MB/container
`conmon`, one shared `aardvark-dns` process). ~800MB difference at that scale — real, not
theoretical, and worth the investment if the engineering cost is reasonable.

## What's already shipped, independent of whether the engine gets built

This part doesn't wait on the rest of the plan — it's already committed and useful on its own for
anyone running rootless Podman on Ubuntu/Debian, whether or not BrickKit ever lets you pick it:

- `scripts/podman/check-environment.sh` — read-only diagnostic (does one self-cleaning create+remove
  cycle to get a real answer, config-file reading alone can't tell you).
- `scripts/podman/fix-apparmor.sh` — the fix, clearly marked destructive.
- `docs/en/07-patterns/11-podman-environment-checklist.md` / `docs/zh/...` — bilingual prerequisite
  doc, explicit that this is *not* a BrickKit feature yet.
- `docs/archive/podman-复活尝试记录-2026-09.md` — the full investigation trail, including two real
  incidents worth reading before anyone touches AppArmor policy on a machine with snap apps installed.

## The technical root cause (for whoever picks this back up)

Rootless Podman's `pasta` network helper needs a `SIGTERM` from `podman` to cleanly tear down a
deployment's network namespace on `down`/`rm`. Ubuntu/Debian's default AppArmor policy blocks that
signal — confirmed as a distro packaging gap, not a Podman or BrickKit bug, against upstream
([containers/podman#27372](https://github.com/containers/podman/issues/27372),
[Debian #1100135](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1100135)). Two independent
fixes exist (both currently applied together, never isolated): removing a stale `podman` stub
profile (Debian's own diagnosis, needs the file to still exist when `apparmor_parser -R` runs — an
easy trap), and `aa-complain` on `pasta`'s own profile. A third, unrelated prerequisite:
`podman.socket` has to be enabled for `podman compose` (which shells out to the same
`docker-compose` binary Docker uses) to connect at all.

## What's settled

- **Podman and Docker consume the identical `docker-compose.yaml`.** Confirmed directly — `podman
  compose` on this machine literally shells out to the same `docker-compose` binary Docker uses.
  This is the load-bearing fact for everything below: unlike Docker vs K8s (structurally different
  generated files), adding Podman needs zero changes to compose-file generation, only to which
  binary executes it.
- **Engine choice cannot be per-component.** A single `docker compose`/`podman compose` invocation
  runs everything in that file under one engine — there's no way to mix engines within one
  deployment, because the containers wouldn't share a network namespace and service-name DNS
  resolution (the addressing model in §5.1) would break across the boundary. Engine is necessarily
  a single, project-wide (or, per the companion design, locally-overridable) value — never a
  per-component field.
- **`deploy.target: docker | k8s` stays as-is.** Engine selection is a layer *under* `docker`, not
  a third value parallel to `k8s` in the same sense — though the exact field shape (a third
  `deploy.target` value vs. a separate field) is one of the still-open items below; what's settled
  is only that it's not a CLI flag (`--engine`) and not an environment variable — both rejected
  explicitly in favor of something explicit and project-visible.
- **`mode: local`/`mode: debug`'s `host-gateway` mechanism already works identically under
  Podman** (already documented, confirmed by earlier investigation) — this is real, existing
  scope reduction: whatever ships doesn't need new code for that interaction.
- **k8s doesn't allow `local`/`debug`; docker and podman both do** — same rule `mode: debug` already
  follows today (Docker-only), extended naturally to podman since it shares the same
  container-network-based deployment shape k8s doesn't have.
- **005 §7.5's three revival conditions, reassessed with real evidence (not just theory) on this one
  machine:**
  1. A machine where `down` works cleanly — met (10x sequential + 5x concurrent lifecycle tests,
     stable).
  2. Full lifecycle (`up` → `status` → real business request → idempotent rerun → `down` → no
     residue) — met, verified today using an actual BrickKit-generated compose file
     (`brickkit add` + `brickkit up --dry-run` against `tests/components/demo-hello`) run through
     real `podman compose`, including a cross-container request over the service-name DNS BrickKit's
     whole addressing model depends on.
  3. A repeatable check so it can't silently regress — **not met**, nothing built yet (see below).
- **Priority call from the person driving this:** get the architecture and the CI guard right
  first; portability beyond this one Ubuntu 26.04 machine is deliberately deferred, to be filled in
  machine-by-machine later rather than gating the initial design.
- **AGENTS.md §4.1's rejection of "Engine plugins / third-party deploy targets (an `--engine
  nomad`-style flag)" does not block this** — its own text says new targets are fine "built in-tree,
  with the full test guard set." That's exactly the plan here (a real `engine.Engine` implementation
  living in `internal/engine`, not a plugin), so this isn't a case of quietly overriding that
  principle — it's the case the principle already carved out.

## Still open — pick these up after the config-override design lands

- **Exact field shape for engine selection in `brickkit.yaml`.** Depends on the companion design:
  if the local override file can cleanly override a project-declared default engine, the "whole
  team/CI forced onto one engine" problem that a naive single shared field would create mostly goes
  away — a project can default to `docker` for universality while an individual developer opts into
  `podman` locally without ever touching the shared file. Worth confirming this actually resolves it
  once that design is settled, rather than assuming it does.
- **CI regression guard design (condition 3).** The AppArmor fix is a host-level, sudo-requiring
  change — CI runners don't have it by default. Needs either a dedicated container/VM image with the
  fix baked in, or an explicit CI job that runs `scripts/podman/fix-apparmor.sh` first. Nothing
  designed yet.
- **Portability beyond this one machine.** Explicitly deferred by design (not by neglect). Fedora/RHEL
  use SELinux, not AppArmor — this whole class of bug may not exist there, or may need a completely
  different fix. Ubuntu versions other than 26.04 untested. The plan should make this gap legible
  (e.g., the CLI should fail with a clear, specific error on an unverified platform, not a silent
  wrong-looking failure) rather than pretending it's solved everywhere.
- **The actual `internal/engine` implementation.** Conceptually should reuse ~100% of
  `internal/compose`'s existing file-generation code (same output file, different binary executes
  it) — the new work is almost entirely in `internal/engine` (a Podman-backed `Engine`
  implementation satisfying the same interface Docker's does) plus whatever the CLI needs to resolve
  "which engine for this run" per the config-override design. Not designed at the code level yet.
- **Documentation follow-through once this actually ships:** `docs/en/07-patterns/11-podman-environment-checklist.md`
  currently disclaims "not a BrickKit feature yet" — that framing needs revising. AGENTS.md §4.1's
  Podman rejection-list entry and `docs/archive/design/005-部署与运行规范.md §7`'s "why not Podman"
  both need a forward pointer once this lands, the way `mode: local`'s eventual addition required
  updating docs that previously said it didn't exist.
