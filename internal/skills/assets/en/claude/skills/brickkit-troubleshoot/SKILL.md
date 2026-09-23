---
name: brickkit-troubleshoot
description: Use when a BrickKit command reports an error, a component won't start, address injection isn't taking effect, dependency resolution fails, or you need to look up a problem by its error code. Covers the mapping from error code to the fix, the order to check things in for common failures, and which "bugs" are actually deliberate design. Applies when the user pastes error output from a BrickKit command, or asks "why won't it start / connect / why can't it find it."
---

# Troubleshooting

## When to use this skill

- The user pastes a `brickkit` error
- A component won't start, or started but can't reach a dependency
- An environment variable wasn't injected
- What's running after `up` doesn't match expectations
- A Pod is stuck CrashLoopBackOff, but the container's own logs look fine

## Check this first: some "failures" are deliberate design

Before digging in, confirm it isn't one of these **by-design** behaviors. Treating them as bugs and
"fixing" them only makes things worse.

**1. A missing optional dependency's environment variable doesn't exist at all — it's not an empty string.**

`os.environ["X"]` throws a `KeyError` and crashes the component — this is deliberate, so "the
dependency isn't there" surfaces at startup instead of becoming a runtime mystery pointed at an
empty address. The fix is in the component's own code: switch to `os.environ.get()` and write
degradation logic — **not** having the platform inject an empty value.

**2. A Pod is permanently CrashLoopBackOff while the container's own logs look completely normal.**

This is almost always a startup-budget problem. The platform fixes `interval`/`timeout`/`failureThreshold`
at 10s/3s/3, whose product is only 30 seconds, so it gives every component a **60-second** startup
grace period by default. A component whose cold start exceeds it (a heavy Spring Boot app, Django
preloading a lot, .NET's first JIT pass) gets killed and restarted, and runs through the same 60
seconds again.

The fix: in `component.yaml`'s `healthCheck`, set `startPeriodSeconds` larger than the real cold
start (default 60; setting it generously costs nothing — the grace period only delays "declaring it
dead," never "declaring it alive").

**3. There's no registry, no long-running service, no config center, no gateway.**

Not finding them isn't a missing install. Service discovery uses native Docker/K8s DNS, health
checking uses Probes / healthchecks + restart policies. Don't suggest adding them — that's a design
that was argued through and rejected.

**4. A component "mysteriously" started anyway.**

Start/stop "follows the layer above it": as long as one thing above it is still running, it runs,
required and optional dependencies treated the same. Read the reason on each line of CLI output —
`starting (top-level)` / `starting (mode: enabled)` / `starting (X needs it)` — it states exactly
where the decision came from.

**5. A `configSchema` item silently has no effect.**

It collided with a platform-reserved variable. The CLI warns and skips that config item, and the
platform-injected value wins. Check whether the item's name, uppercased, hits `DATABASE_*` /
`REDIS_*` / `MQ_*` / `STORAGE_*` / `SEARCH_*` / `SMTP_*` / `*_ENDPOINT` / `COMPONENT_ID` /
`COMPONENT_VERSION`. `databaseTimeout` → `DATABASE_TIMEOUT`, for instance, collides.

**6. `brickkit.yaml` refuses `mode: debug` — this isn't a bug to route around.**

It's rejected outright, at parse time, unconditionally. `mode: debug` can **only** be written in
`override.yaml` (optional, gitignored, per-developer — run `brickkit override` to create/refresh
it). `mode: local` has no such restriction and stays in `brickkit.yaml` as always. Don't suggest
editing `brickkit.yaml` to add `mode: debug`, and don't treat the rejection as something to work
around — direct the user to `override.yaml` instead.

**7. `override.yaml` silently does nothing — check `--config` first.**

`override.yaml` only ever applies to a run against the **default** `brickkit.yaml`. A run with
`--config brickkit.prod.yaml` ignores any `override.yaml` present and prints a note saying so —
that's by design (a personal local override must never leak into a named-environment run), not a
bug.

## Error code → what to do

The CLI's errors carry an error code. Look it up by code, it's faster than by wording.

| Error code | Meaning and first step |
| --- | --- |
| `DEPENDENCY_MISSING` | A required dependency wasn't found in any install source. Check the install source config, whether the component exists, and whether the version is stable |
| `RESOURCE_UNBOUND` | A component's required resource isn't declared or bound in `brickkit.yaml`'s `resources`. `kind` + `engine` must match the component's declaration **exactly**. To skip it for now, set `mode: disable` — a component that isn't starting doesn't go through this check |
| `COMPONENT_DISABLED` | A pinned component (`mode: enabled` or `mode: debug`) hit a required dependency that's turned off — two conflicting intents. Either remove that `mode: disable`, or don't pin the dependent |
| `VERSION_AMBIGUOUS` | Multiple versions coexist and none was specified. Add the exact version |
| `DEPENDENCY_CYCLE` | A cycle made entirely of required dependencies. A cycle is only valid when at least one edge in it is optional |
| `MANIFEST_INVALID` | Something's wrong with `component.yaml`. Note it has **no extension-field mechanism** — an unrecognized key is rejected on the spot |
| `COMPONENT_NOT_FOUND` | The component isn't in any install source |
| `CONFIG_CONFLICT` | A config item collided with a reserved variable (see point 5 above). This is a warning, not a blocker |
| `IMAGE_UNAUTHORIZED` | The image pull wasn't authorized. `docker login <registry>` first, then rerun |
| `MIGRATION_FAILED` | The migration script failed. Check the migration log, fix it, rerun |
| `PORT_CONFLICT` | A port collided — usually a duplicate `localPort` or `exposePort` |
| `SIGNATURE_INVALID` | Signature verification failed. Note: with zero `publicKeys` configured, verification is disabled entirely — that case doesn't produce this code, just a warning that nothing was verified |
| `AUTH_REQUIRED` / `TOKEN_EXPIRED` | Run `brickkit login` again |
| `ENGINE_MISSING` | Docker / kubectl isn't on `PATH`, or isn't running |
| `PROJECT_MISSING` | The current directory isn't a BrickKit project, or `--config` points at the wrong file |
| `PROJECT_EXISTS` | Already initialized — no need to run `init` again |
| `LINT_FAILED` | `brickkit lint` found problems. Each one is already printed to stdout (naming its file and field) — fix them, then rerun. Warnings alone don't fail by default, unless `--strict` is given |
| `CLONE_FAILED` | Two common causes: the component is closed-source (no Git repository, but **installs and works fine regardless**), or the target directory already exists |

## Where to check, and in what order

**A component won't start** — check these in order, from most to least common:

1. `brickkit status` — was it even judged as "starting"? Components that aren't starting are
   listed too
2. The **reason** on that line of CLI output (top-level / mode / X needs it)
3. The component's own log — did the process itself come up
4. Whether startup exceeded the default 60-second grace period (see point 2 above)
5. Environment variables — is the dependency actually running? A missing optional dependency
   **injects nothing** for that `*_ENDPOINT`
6. Resource bindings — do `kind` and `engine` match

**Can't reach a dependency**: the address format is identical locally and on K8s, always
`http://<versioned-service-name>:<port>`, where the service name is the component ID with `/` and
`.` turned into `-`, plus the exact version (`people/basic` + `1.0.0` →
`people-basic-1-0-0`). The variable name is based on the component ID and carries no version
(`PEOPLE_BASIC_ENDPOINT`); only the value carries the version. Check against these two rules if
something doesn't line up.

**To see the generated result without starting anything**: `brickkit up --dry-run`.

**To see how the dependencies connect** (who depends on whom, which edges are optional, which
won't start this run, how `servedBy` groups things): `brickkit graph`. It prints Mermaid text, and
saving it as a `.mmd` file lets GitHub render it directly.

## Where to dig deeper

(The `docs/...` and `AGENTS.md` paths below all live in the BrickKit repository
<https://github.com/brickKit/brickKit>; every article under `docs/` has an `en/` and a `zh/`
version, content-equivalent.)

- Flags: `brickkit <command> --help`
- The full treatment of common errors (symptom → cause → fix, with real output samples): `docs/en/08-troubleshooting.md`
- The authoritative definition of error codes (constant names, wording): `internal/clierr/clierr.go`
- The full argument for "why is it designed this way" (for when the user asks "why doesn't it..."):
  root `AGENTS.md` §9 (the twenty-three "whys")
- `override.yaml` itself (schema, downgrade-only target rule, the multi-environment `--config`
  guard, the `brickkit override` command): root `AGENTS.md` §7.1
