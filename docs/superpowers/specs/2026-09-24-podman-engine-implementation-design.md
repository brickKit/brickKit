# Podman engine implementation — design

**Status: approved design, ready for an implementation plan.** Supersedes the paused
[podman-engine-support-notes.md](2026-09-23-podman-engine-support-notes.md) — that document's
"still open" items are resolved here; it should be closed out once this plan lands (see §7).

Historical background (why Podman was pulled, the environment root cause, why the field shape is
what it is) lives in two read-only records this document does not repeat or link into current
docs: [005-部署与运行规范.md §7](../../archive/design/005-部署与运行规范.md) and
[podman-复活尝试记录-2026-09.md](../../archive/podman-复活尝试记录-2026-09.md). Anyone implementing
this plan doesn't need to open either — everything actionable from them is folded in below.

## 1. Scope

**In scope:** a real `engine.Engine` implementation for Podman, wired into the CLI so that
`override.yaml`'s `target: podman` actually runs `up`/`down`/`status` against Podman instead of
erroring with `ENGINE_MISSING`. This closes 005 §7.5's revival condition ② ("the actual
`engine.Engine` implementation").

**Out of scope, deliberately, for this round:**

- **No new CI workflow.** This repository has exactly one GitHub Actions workflow
  (`.github/workflows/release.yml`), tag-triggered only — there is no push/PR CI today, for Docker
  or Podman. Revival condition ③ ("a repeatable check") is satisfied here with unit tests that
  mirror the existing `internal/engine`/`internal/cli` testing pattern (fake `runner`, no real
  container invoked) — not a new automated real-lifecycle CI job. This is a deliberate, explicit
  choice: building the repo's first real-container-lifecycle CI job is a bigger decision than this
  round's scope and was explicitly not chosen.
- **No environment/AppArmor detection.** The CLI does not probe whether the host's Podman rootless
  networking is in a working state before attempting an operation. Doing so would mean the platform
  guessing at environment fitness — the same "auto-guessed and uncontrollable" category §4 already
  rejects, and the same reasoning `resolveEngineFor`'s existing comment already uses to keep the
  "explicitly configured" and "environment-detected" Podman error paths separate. Portability beyond
  the one machine this was verified on stays explicitly deferred, per the working notes.
- **No change to `brickkit.yaml`'s own `deploy.target` semantics.** `podman` remains an
  `override.yaml`-only value (§3/§7.1's existing glossary entry); this is settled and not
  revisited.

## 2. Architecture: reuse, not a new implementation

`internal/engine/compose.go`'s `Compose` struct is already engine-agnostic — `name`/`bin`/`base`/
`runner` — and `NewDocker()` is a five-line constructor over it:

```go
func NewDocker() *Compose {
    return &Compose{name: Docker, bin: "docker", base: []string{"compose"}, runner: run}
}
```

`podman compose` on the verified machine shells out to the identical `docker-compose` binary
Docker uses, and every behavior `Compose`'s methods already encode was learned generically (not
Docker-specifically): the no-`-f` rule on `Down`/`Status` (identity is the project label, not the
regenerable file), never passing `-v` on `Down`, the two `ps --format json` output shapes, and the
P27 stdout/stderr-merge fix (compose tools that print a banner to stderr on success must not have
that banner corrupt JSON parsing — this exact bug was first caught via `podman compose`'s banner).
All of this needs zero changes to support Podman. `NewPodman()` is the same constructor shape with
`bin: "podman"`:

```go
func NewPodman() *Compose {
    return &Compose{name: Podman, bin: "podman", base: []string{"compose"}, runner: run}
}
```

Add a `Podman = "podman"` name constant alongside the existing `Docker`/`K8s` constants in
`internal/engine/engine.go` (matching `internal/compose`'s existing `EnginePodman`, which is
already used for dry-run file generation and needs no change).

### 2.1 Two things that are *not* reuse-as-is

**(a) The missing-binary hint is hardcoded to Docker.** `Compose.exec()`'s `isMissingBinary` branch
always returns `EngineHintInstallDocker` ("Install Docker 20.10+ and retry"), regardless of which
binary was actually missing. Once a Podman engine exists, an environment with `target: podman` and
no `podman` binary must not be told to install Docker. This hint has to become bin-aware — either
parametrized (`"Install %[1]s and retry"` filled with `c.bin`) or split into two catalog entries
selected by `c.bin`. Both the English and Chinese catalogs (`internal/i18n/catalog_en.go`,
`catalog_zh.go`) need the change; `make check-i18n` gates bilingual parity.

**(b) The known `down` failure signature gets a translated hint, not raw passthrough.** The
documented root cause (rootless Podman's `pasta` helper needs a `SIGTERM` from `podman` to tear
down cleanly; Ubuntu/Debian's default AppArmor policy blocks that signal — a confirmed distro
packaging gap, not a BrickKit or Podman bug) produces a specific, recognizable error text:
`rootless netns: kill network process: permission denied`. Rather than let a user hit that raw
string cold, `exec()`'s generic failure path gets one more pattern-match layer — directly
analogous to `imageError()`'s existing "unauthorized" / "no such host" / "manifest unknown"
text-matching, same philosophy, no new error code (`CodeEngineFailed` still, richer `Hint` only):
when the output contains this signature, the hint points at
`docs/en/07-patterns/11-podman-environment-checklist.md` (zh equivalent) and the two scripts
(`scripts/podman/check-environment.sh`, `fix-apparmor.sh`) that already exist for exactly this.

This match is **not gated on `c.bin == "podman"` or any platform check** — the text only ever comes
from Podman's own rootless networking stack, so matching on content alone is sufficient and keeps
the logic in the same shape as every other `exec()`/`imageError()` pattern branch.

## 3. CLI wiring

### 3.1 `resolveEngineFor` (`internal/cli/up_k8s.go`)

The explicit-target branch currently reads:

```go
if cfg != nil && cfg.Deploy.Target == config.TargetPodman {
    if opts.Engine != nil {
        return opts.Engine, nil
    }
    return nil, podmanTargetNotImplemented()
}
```

The last line becomes `return engine.NewPodman(), nil`. `podmanTargetNotImplemented()` and its two
dedicated message ids (`CliUpPodmanTargetNotImplemented`, `CliUpPodmanTargetDryRunStillWorks`) are
deleted outright — they have no remaining caller and no reason to be kept as a compatibility shim.
If `podman` genuinely isn't installed, or fails for some other reason, the ordinary `exec()` error
path (§2.1a) now produces the right message on its own; a dedicated "not implemented" message no
longer describes reality.

### 3.2 `Detect()` / `podmanNotSupported()` (`internal/engine/compose.go`)

This is a *different* code path from §3.1 — it fires when nothing in config explicitly asked for
Podman, but Docker isn't on `PATH` and Podman is. It must keep erroring rather than silently
switching engines: picking an engine because of what happens to be installed, instead of what the
project explicitly declared, is exactly the "auto-guessed and uncontrollable" shape §4 rejects, and
`resolveEngineFor`'s own existing comment already draws this line for the opposite direction
(config-explicit vs. environment-detected must never be blurred together).

What changes is the **wording**, because the underlying claim changes. Today's message
(`EnginePodmanUnsupported` + detail keys `EnginePodmanDetectedDetail`/`EnginePodmanStuckDetail`/
`EnginePodmanWhyDetail`) says, in effect, "built, ran partway, permanently stuck, don't bother." That
framing is no longer true. The replacement message says: Podman was detected, but nothing enabled
it — run `brickkit override` and set `target: podman` in `override.yaml` to actually use it —
plus a pointer to the environment checklist doc, since a real attempt may still hit the AppArmor
prerequisite on an unfixed machine (in which case §2.1b's translated hint takes over from there).

## 4. Testing (unit-level, mirrors the existing pattern — no new CI)

- **`internal/engine/compose_test.go`**: `NewPodman()` invokes the `podman` binary (assert via fake
  `runner` capturing the invoked name) — not `docker`. Missing-binary hint names the actual missing
  binary, not always Docker. A simulated `down` failure whose output contains the AppArmor
  signature carries the checklist-doc hint; ordinary `down` failures (e.g. plain "no such project")
  do not gain that hint (the match must be specific, not a catch-all).
- **`internal/engine/detect_test.go`**: update the existing "only Podman installed" case to assert
  the new wording (pointing at `override.yaml`/`brickkit override`, not "stuck").
- **`internal/cli/podman_target_test.go`**: the current four tests assert that real (non-dry-run)
  `up`/`down`/`status` against `target: podman` fail with the old "not implemented" message — this
  is exactly backwards now and gets rewritten: dry-run behavior is unchanged; real `up`/`down`/
  `status` with an injected fake engine (`opts.Engine`) now **succeed**; one new test asserts that
  `resolveEngineFor`, with no injected engine and `target: podman`, returns an engine whose
  `Name() == "podman"` (i.e., real dispatch happens, not the old error).
- **`tests/checklist/清单.tsv`**: add rows under the existing `compat` category for the new/changed
  tests above, in the same `<category>\t<id>\t<description>\t...\t<test name>` shape the 34.x rows
  already use. Exact numbering is an implementation-plan detail.

### 4.1 One-time manual re-verification (not automated, still required)

The 2026-09 investigation verified the environment fix and a full lifecycle by hand, with raw
`podman` commands and one `brickkit up --dry-run`-generated file. That verification predates this
code. Once this implementation lands, do one more full manual pass **through the real code path**
on this same machine: `brickkit add` a test component → `brickkit up` (real, not dry-run, with
`override.yaml`'s `target: podman`) → `status` → one cross-container request over the service-name
address → an idempotent rerun of `up` → `down` → confirm no residue. This is the step that actually
closes revival condition ② against the shipped implementation, not just the environment; it belongs
in the implementation plan as a manual acceptance step, not a test file.

## 5. Documentation updates

Current (non-archived) docs get rewritten to stand on their own — none of them should send a reader
to the two archived documents to understand current behavior (established convention; the archived
docs are explicitly read-only historical records and are never a dependency for understanding
current state):

- **`AGENTS.md` §4.1** — the Podman rejection-list row changes from "built, ran, pulled" framing to
  describing what now works (`target: podman` in `override.yaml`, real engine, the environment
  prerequisite, the translated-hint behavior on the known failure). §11's doc-pointer table gains a
  row for the environment checklist if it doesn't already have a clear one.
- **`docs/en/06-architecture/00-overview.md`** (+ zh) — the "Podman as a deploy target" and "Engine
  plugins" sections update the same way.
- **`docs/en/06-architecture/10-error-codes.md`** (+ zh) — the `ENGINE_MISSING` table gains rows for
  the new wording (§3.2's reworded detection message, §2.1a's bin-aware missing-binary hint) and the
  translated down-failure hint from §2.1b.
- **`docs/en/07-patterns/11-podman-environment-checklist.md`** (+ zh) — reframed from "not a
  BrickKit feature yet" to "these prerequisites are what make `target: podman` actually work."
- **`docs/archive/design/005-...md` §7 and `docs/archive/podman-复活尝试记录-2026-09.md`** — left
  untouched; both already declare themselves read-only historical records, and that convention
  holds.
- **`docs/superpowers/specs/2026-09-23-podman-engine-support-notes.md`** — closed out once this
  plan ships: its "still open" items are all resolved by this document, and a paused note that no
  longer has an open question is stale, not historical record.

## 6. What this does *not* claim

This does not claim Podman works anywhere other than the one machine the environment fix was
verified on. It does not add any code that detects, warns about, or works around AppArmor state —
the translated hint in §2.1b only fires after a real failure with that specific text, never
pre-emptively. Portability to other distributions (Fedora/RHEL's SELinux instead of AppArmor,
other Ubuntu versions) remains exactly as open as the working notes already said, and nothing here
narrows or widens that gap.

## 7. Follow-through

Once this plan's implementation is committed: close out
`2026-09-23-podman-engine-support-notes.md` (mark resolved, referencing this document, per this
repo's existing convention for paused plans that reach a decision — see `改进计划.md` handling).
