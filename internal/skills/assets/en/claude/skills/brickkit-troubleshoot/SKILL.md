---
name: brickkit-troubleshoot
description: Use when a BrickKit command reports an error, a component won't start, address injection isn't taking effect, dependency resolution fails, or you need to look up a problem by its error_code. Covers symptom → cause → fix for deploy files out of step with brickkit.yaml, a stale deploy.local.yaml, empty required config, upgrade config conflicts, undefined $var references, missing or stale images, local repo version mismatches, shell member mismatches, compose start cycles, release refusals, and which "bugs" are deliberate design. Applies when the user pastes what a BrickKit command printed, or asks "why won't it start / connect / release".
---

# Troubleshooting

## When to use this skill

- The user pastes a `brickkit` error
- A component won't start, or started but can't reach a dependency
- An environment variable wasn't injected, or a config value has no effect
- `brickkit release` refuses
- A Pod is stuck CrashLoopBackOff, but the container's own logs look fine

## Check this first: some "failures" are deliberate design

**1. A missing optional dependency's variable doesn't exist at all** — not an empty string.
`os.environ["X"]` crashing is intended; the fix is `os.environ.get()` in the component, never an
injected empty value.

**2. CrashLoopBackOff with healthy-looking logs** is almost always the startup grace period: 60
seconds by default. Raise `healthCheck.startPeriodSeconds` in `component.yaml` above the real cold
start. And a health check that pings a database is itself the bug — `/healthz` checks only the process.

**3. No registry, config center, gateway or long-running service** exists. Not finding one isn't a
missing install; don't suggest adding it.

**4. A component "mysteriously" started.** Start/stop follows the layer above: it runs while anything
running needs it, required or optional. Each `up` line states the reason (`starting (X needs it)`).

**5. `deploy.yaml` rejects `mode: debug`.** By design: it belongs in `deploy.local.yaml`
(`brickkit local on`, then edit that file).

**6. You edited `deploy.yaml` and nothing changed.** Local mode is probably on — every command then
reads `deploy.local.yaml` instead. `brickkit local status` shows it. `-f` ignores both the switch
and the local file.

**7. `up` refuses because a config file has a duplicate key.** Intended after `upgrade --yes`: keep
one line, delete the other and the comment. Don't run a YAML formatter over it — it silently drops
one key and hides the conflict.

**8. A `$var:` reference is "not a string" or is ignored.** It must be written without a space:
`DB_HOST: $var:DB_HOST`. With a space (`$var: DB_HOST`) YAML reads it as a map. Values in
`config/vars.yaml` can't themselves be `$var:` references.

## Symptom → cause → fix

| You see | Cause | Fix |
| --- | --- | --- |
| `DEPLOY_INCONSISTENT`: the deploy file doesn't match the components in `brickkit.yaml` | A component line was added/removed by hand, or a `requiredBy` version has no `id@version` entry | Add/remove the listed entries — or redo the change with `brickkit add` / `remove`, which keep all three layers in step |
| `DEPLOY_INCONSISTENT`: `deploy.local.yaml` is out of date | Local mode is on and the team changed `brickkit.yaml` | `brickkit local refresh` (re-apply the listed old changes by hand), or edit the file, or `brickkit local off` |
| "a required component config item has no value" (`CONFIG_INVALID`) | `add` wrote `KEY: ""` for a required key without default | Fill it in `config/<scope>-<name>.yaml` — often as `${VAR}` or `$var:NAME` |
| "unresolved configuration conflicts" (`CONFIG_CONFLICT`) | `upgrade` with `--yes` / no TTY met a key you changed whose default also changed | Keep one of the two lines in the config file, delete the other and the comment |
| "references shared variables that are defined nowhere" (`CONFIG_INVALID`) | A `$var:NAME` has no value in `config/vars.yaml` nor in the deploy file's `vars:` | Define it; there is no implicit fallback |
| A config key "won't take effect" warning | The key isn't in the component's `configSchema` (typo, or dropped by an upgrade) | Use the suggested key; keys are the exact env var names |
| `DEPENDENCY_MISSING` naming a version not declared | `brickkit.yaml` is a lock file; that version isn't in it | `brickkit add` the component at that version |
| `add` refuses: the project already has this component | A direct `add` of another version | `brickkit upgrade <id>@<version>` |
| `IMAGE_MISSING` | The image needs a local build; `up` never builds | `brickkit build <id>` |
| Code changes don't show up after `brickkit build` | An image of that version already exists and is skipped | `brickkit build <id> --force` (or bump the version) |
| `IMAGE_STALE` | A locally built shell image contains other member versions than its `component.yaml` | `brickkit build <shell> --force` |
| `IMAGE_UNVERIFIED` (warning) | A shell image without the member-version label (not built by `brickkit build`) | Fine if you trust it; rebuild with `brickkit build` to make it checkable |
| "Code that runs from a local repository does not match this run" (`CONFIG_INVALID`) | A `mode: local` / `debug` component (or bare-process shell member) has a repo `metadata.version` ≠ the default version, or a `requiredBy` version was given `mode: local` / `debug` (only the default runs from the repo) | `brickkit upgrade <id>@<repo version>`, or check out the tag matching the default version |
| Shell member versions differ from the ones the shell compiles in (`CONFIG_INVALID`) | The deploy file nests a member version the shell's `component.yaml` doesn't declare | Upgrade the shell to one that compiles that version; or move the member entry out of the shell; or keep both — add a `brickkit.yaml` line for the compiled version with `requiredBy: [<shell>]`, nest `id@thatversion` under the shell, leave the other top-level |
| `DEPENDENCY_CYCLE`: the shell waits for a component that waits for the shell | Hosting members merged their dependencies into one container, forming a Compose `depends_on` loop | Move the outside component into the shell too; or move a member out; or `skipWaitFor: [<id>]` on the entry (only drops the start wait — the component must retry) |
| `DEPENDENCY_CYCLE` in plain resolution | A cycle made only of required dependencies | Make at least one edge optional |
| `COMPONENT_DISABLED` | A pinned component (`enabled` / `local` / `debug`) needs a required dependency that is `disable` | Remove one of the two conflicting intents |
| `mode` rejected on `target: k8s` | `local` / `debug` need a process on your machine | Use docker / podman, e.g. `target:` in `deploy.local.yaml` |
| `RELEASE_BLOCKED` | Uncommitted changes, no upstream, unpushed commits, or the tag already exists / is on another commit | Commit, push, bump `metadata.version`, then `brickkit release` again |
| `RELEASE_PUSH_FAILED` | The push was rejected or unreachable; the local tag was removed | Fix access/network and rerun unchanged |
| `MIGRATION_FAILED` | The migration command failed (or a typo'd argument the entrypoint didn't reject) | Read the migration container's log, fix, rerun |
| `PORT_CONFLICT` | Duplicate `localPort` / `exposePort`, or the host port is taken | Change the port — in `deploy.local.yaml` if it's only your machine |
| `ENGINE_MISSING` | docker / podman / kubectl not on `PATH` or not running | Install/start it |
| `PROJECT_MISSING` | Not in a project, or `deploy.yaml` missing | `brickkit init` completes a project without touching existing files |
| `LINT_FAILED` | `brickkit lint` found problems, each printed with file and field | Fix and rerun; warnings fail only with `--strict` |
| `AUTH_REQUIRED` / `TOKEN_EXPIRED` / `IMAGE_UNAUTHORIZED` | Market login / registry access | `brickkit login` / `docker login <registry>` |

Every command-ending error prints a JSON line on stderr with a stable `error_code` right after the
`❌` block; only `NETWORK_UNREACHABLE` is worth retrying unchanged.

## Where to check, and in what order

**A component won't start:**

1. `brickkit status` — was it judged as starting at all? (It reads the same deploy file as `up`.)
2. The reason on its `up` output line (top-level / mode / X needs it)
3. `brickkit lint` — offline check of all three layers together
4. The component's own log: `docker compose -p brickkit-<project> logs <service>` — without `-p`,
   compose looks at a different project and shows nothing
5. Startup longer than 60 seconds (point 2 above)
6. Its environment: is each dependency running? A missing optional one injects no `*_ENDPOINT`

**Can't reach a dependency**: the address is `http://<versioned-service-name>:<port>` everywhere;
the service name is the id with `/` and `.` → `-` plus the exact version (`erp/api` + `1.0.0` →
`erp-api-1-0-0`), the variable name carries no version (`ERP_API_ENDPOINT`). A member hosted in a
shell is reached at the shell's address. **To see the generated files** without starting anything:
`brickkit up --dry-run`. **To see the graph**: `brickkit graph` or `brickkit deps`.

## Where to dig deeper

- Flags: `brickkit <command> --help`
- A component's own notes (configuration, known pitfalls): `.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md`
- The full specification and the reasoning behind each design choice:
  <https://github.com/brickKit/brickKit> and its root `AGENTS.md`
