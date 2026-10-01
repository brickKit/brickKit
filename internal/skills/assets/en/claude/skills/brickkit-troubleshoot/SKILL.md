---
name: brickkit-troubleshoot
description: Use when a BrickKit command reports an error, a component won't start, address injection isn't taking effect, dependency resolution fails, or you need to look up a problem by its error_code. Covers symptom → cause → fix for deploy files out of step with brickkit.yaml, a stale deploy.local.yaml, empty required config, upgrade config conflicts, undefined $var and ${VAR} references, a commit blocked by the pre-commit check, missing or stale images, local repo version mismatches, shell member mismatches, compose start cycles, release refusals, and which "bugs" are deliberate design. Applies when the user pastes what a BrickKit command printed, or asks "why won't it start / connect / release".
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

**3. The component says it's ready, the platform says unhealthy.** On Docker / Podman the health check
runs `wget` or `curl` inside the container; an image with neither can never pass it. Add one to the
image. (Kubernetes probes are `httpGet` and don't need either.)

**4. A `mode: debug` component fails with `relation does not exist`.** It gets no migration container
(`MIGRATION_SKIPPED`); run its migration once by hand.

**5. No registry, config center, gateway or long-running service** exists. Not finding one isn't a
missing install; don't suggest adding it.

**6. A component "mysteriously" started.** Start/stop follows the layer above: it runs while anything
running needs it, required or optional. Each `up` line states the reason (`starting (X needs it)`).

**7. `deploy.yaml` rejects `mode: debug`.** By design: it belongs in `deploy.local.yaml`
(`brickkit local on`, then edit that file).

**8. You edited `deploy.yaml` and nothing changed.** Local mode is probably on — the commands that run
or check the deployment (`up`, `down`, `status`, `sync`, `lint`, `build`) then read `deploy.local.yaml`
instead. `graph` and `deps` always read `deploy.yaml`. `brickkit local status` shows the switch; `-f`
ignores both the switch and the local file.

**9. `up` refuses because a config file has a duplicate key.** Intended after `upgrade --yes`: keep
one line, delete the other and the comment. Don't run a YAML formatter over it — it silently drops
one key and hides the conflict.

**10. A `$var:` reference is "not a string" or is ignored.** It must be written without a space:
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
| "environment variables referenced in config/ or the deploy file are not defined" (`CONFIG_INVALID`) | A `${VAR}` is set neither in the process environment nor in `.env`. Checked when the files are generated, on every target — compose would otherwise put in an empty string without an error | Add it to `.env` or export it; or write a default, `${VAR:-dev}` (`${VAR:-}` for a value that may be empty) |
| "… is not a valid ${} reference" (`CONFIG_INVALID`) | A malformed reference such as `${DB_HOST` or `${DB-HOST}`, caught when the files are loaded | Write `${NAME}` or `${NAME:-default}` |
| A config key "won't take effect" warning | The key isn't in the component's `configSchema` (typo, or dropped by an upgrade) | Use the suggested key; keys are the exact env var names |
| A value you wrote has no effect, and nothing warns | You edited another version's file: the default version reads `config/<scope>-<name>.yaml`, a `requiredBy` version reads `config/<scope>-<name>@<version>.yaml` | Edit the file of the version that runs |
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
| "the focus … is not a component of this project" (`COMPONENT_NOT_FOUND`) | `focus:` in `deploy.local.yaml` names a component that isn't in `brickkit.yaml` (a typo, or it was removed) | Follow the "did you mean"; `brickkit up --focus <id>` picks another, `brickkit up --all` drops the focus |
| "the focus … is written mode: disable" (`COMPONENT_DISABLED`) | The focused entry says `mode: disable` — "run it" and "never run it" at once | Remove `mode: disable`, or focus on another component |
| "failed validation" on the `focus` field (`CONFIG_INVALID`) | `focus:` written in `deploy.yaml` (it's personal), with `target: k8s` (a cluster can't reach your machine), or not a component ID | Keep it in `deploy.local.yaml` on docker / podman; `brickkit up --focus` never writes an invalid one |
| "--focus and --all contradict each other" / "--focus and --all change deploy.local.yaml, which -f and --no-local skip" (`INVALID_ARGUMENT`) | Two intents on one command line | Pick one; drop `-f` / `--no-local` to use a focus |
| `not starting (outside the focus)` on components you expected | A focus is set: only the focus, pinned components and what they need start | `brickkit up --all`, or `mode: enabled` on that component in `deploy.local.yaml` |
| "component source is nested inside another component's directory" (`CONFIG_CONFLICT`) | A component's source sits inside another component's directory — two copies of one component; `up`, `lint` and `sync` refuse | Move or delete the nested copy yourself — ask the person first; the error says whether it exists anywhere else |
| "this workbench sits inside project …; --repo would clone a second copy here" (`CONFIG_CONFLICT`) | `add --repo` in a workbench that is itself a component of an enclosing project | Run `add --repo` in the enclosing project, or use a focus run there |
| `SUBMODULES_SKIPPED` (warning from `build`): "the source of … has git submodules, and they are empty directories here" | BrickKit never fetches git submodules | Have the component publish an image; or `git submodule update --init` in the cloned repository |
| `mode` rejected on `target: k8s` | `local` / `debug` need a process on your machine | Use docker / podman, e.g. `target:` in `deploy.local.yaml` |
| `RELEASE_BLOCKED` | Uncommitted changes, no upstream, unpushed commits, or the tag already exists / is on another commit | Commit, push, bump `metadata.version`, then `brickkit release` again |
| `RELEASE_PUSH_FAILED` | The push was rejected or unreachable; the local tag was removed | Fix access/network and rerun unchanged |
| `MIGRATION_FAILED` | The migration command failed (or a typo'd argument the entrypoint didn't reject) | Read the migration container's log, fix, rerun |
| `PORT_CONFLICT` | Duplicate `localPort` / `exposePort`, or the host port is taken | Change the port — in `deploy.local.yaml` if it's only your machine |
| `ENGINE_MISSING` | docker / podman / kubectl not on `PATH` or not running — or only Podman is installed while the deploy file says `target: docker` | Install/start it, or set `target: podman` |
| `PROJECT_MISSING` | Not in a project, or `deploy.yaml` missing — or local mode is on but `deploy.local.yaml` was deleted | `brickkit init` completes a project without touching existing files; `brickkit local on` writes the local file again |
| `LINT_FAILED` | `brickkit lint` found problems, each printed with file and field | Fix and rerun; warnings fail only with `--strict` |
| `DOC_FILE_MISSING` / `DOC_SECTION_MISSING` (lint warnings) | A required doc (`BRICKKIT.md`, `AGENTS.md`, `CLAUDE.md`, `README.md`) or one of its fixed sections is missing | Add it; `brickkit new` shows the full set, `brickkit skills update` creates a missing `AGENTS.md` / `CLAUDE.md` |
| `DOC_PATH_MISSING` / `DOC_LINK_BROKEN` | A Code map path in `AGENTS.md`, or a relative link, points at nothing — the code moved, the doc didn't | Fix the path or link in the doc |
| `DOC_LINK_NOT_PORTABLE` | `BRICKKIT.md` has a relative link (dead in other projects' caches), or a component doc links out of the component | Name files as inline code in `BRICKKIT.md`; keep component docs self-contained |
| `DOC_OUT_OF_STEP` | `component.yaml` has a dependency, required key, artifact file or shell member the doc doesn't mention where it belongs | Mention it in that section (a dependency by ID, no version) |
| `DOC_PLACEHOLDER` | A `TODO`-style placeholder is still in a doc's text | Fill it in |
| `DOC_TRANSLATION_DRIFT` | A translation lacks its primary, has a different number of `##` sections, a language version doesn't link every other, or the suffix isn't a language code | Bring the translation back in step with the primary |
| `AGENTS_BLOCK_MISSING` / `CLAUDE_IMPORT_MISSING` | `AGENTS.md` has no usable block maintained by brickkit, or `CLAUDE.md` lacks `@AGENTS.md` | `brickkit skills update` adds them (no other command edits these files) |
| `PROJECT_MAP_OBSOLETE` | The old project map `BRICKKIT.md` is still at the project root | Move your own notes into `AGENTS.md`, then delete it by hand |
| A row of the component table in `AGENTS.md` flips between commits; `skills status` calls the block outdated | A released version was edited in place in a local source (`BRICKKIT.md`, a translation or `metadata.description`), while another machine takes it from the tag. An unreleased bumped version doesn't do this | Put the change in the next version: bump `metadata.version`, `brickkit release`, `brickkit upgrade` in the project |
| `AUTH_REQUIRED` / `TOKEN_EXPIRED` | Market login needed or expired | `brickkit login` |
| `IMAGE_UNAUTHORIZED` | The image registry refused the pull, or has no such image | `docker login <registry>`, check the image reference — or build it here with `brickkit build` |
| `AUTH_FAILED` | The market refused the user name or password — or a Git remote was reached but refused the fetch: the credentials were refused, or the repository doesn't exist (hosts answer both the same way) | Check the credentials; for Git, read git's own words in the error. Retrying unchanged won't help |
| "Commit blocked: component source is committed under the archive directory …" (`CONFIG_CONFLICT`) | The pre-commit check installed by `brickkit init --hooks`: a component that should start has its source under `components/.archived/` in the commit | `brickkit restore` (puts `mode` back), or commit the directory move together with the change; `brickkit restore --check` runs the same check by hand |
| `COMPONENT_NOT_FOUND` | No install source has this id or version; a Git repository has no tag for the version | Check the id and `sources:`; ask the author to `brickkit release` that version |
| `COMPONENT_BLOCKED` | The market took this component version down | Use another version |
| `MANIFEST_INVALID` | A `component.yaml` is invalid; the error names the field | Its author fixes it (`brickkit lint` in the component's repository) |
| `SIGNATURE_INVALID` | `requireSignature` is on and a market component isn't signed | Ask the publisher for a signed version |
| `CLONE_FAILED` | `add --repo` couldn't clone: git failed, a directory of that name exists under `components/`, or the source is archived | Read git's words; move the directory away; `brickkit sync` brings archived source back |
| `SUBMODULE_GUARD` | `sync` or `remove` would move or delete a directory registered as a git submodule of the project | Deregister the submodule first |
| `ENGINE_FAILED` | `docker compose` / `podman compose` / `kubectl` failed, components came up unhealthy, or a `mode: local` process crashed | The engine's own output is in the error; then the component's logs |
| `MIGRATION_SKIPPED` (warning) | A `mode: local` / `debug` component runs no migration container | Run its migration once by hand |
| `PROJECT_EXISTS` | `brickkit init <name>` into a directory that exists and isn't empty (or is a file) | Another name — or go in and run `brickkit init` without a name, which only adds what's missing |
| `INVALID_ARGUMENT` | The command line is wrong (exit code 2): unknown command or flag, a component id or version in the wrong form | `brickkit <command> --help` |
| `INTERNAL` | Reading or writing a local file failed (disk full, no permission) — or, titled "Internal error", a CLI bug | Check disk space and permissions; report a bug with the whole output |

Every command-ending error prints a JSON line on stderr with a stable `error_code` right after the
`❌` block. Scripts decide on that code and the exit code, never on the human text. Only `NETWORK_UNREACHABLE` is worth retrying unchanged — the remote was never reached
(offline, a host name that doesn't resolve). A Git remote that answered and refused is `AUTH_FAILED`.

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
