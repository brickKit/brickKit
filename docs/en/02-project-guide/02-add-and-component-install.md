# Adding components

## The basics

```bash
brickkit add demo/caller
```

```text
🔎 No version given for demo/caller; the latest is 1.0.0 (install source company-git)
➕ Adding demo/caller@1.0.0
   ✅ demo/hello@1.0.0
   ✅ demo/caller@1.0.0
📝 Written: brickkit.yaml, deploy.yaml
📝 Config skeletons: config/demo-hello.yaml, config/demo-caller.yaml
📦 Artifacts: 2 files, in .brickkit/artifacts/
⚠️ Warning: optional dependency missing: demo/bus@1.0.0
   Affected component: demo/caller@1.0.0
   Reason: Failed to fetch component demo/bus
   Impact: This component's environment variable DEMO_BUS_ENDPOINT will not be injected
   💡 Degrading gracefully for a missing optional dependency is the component's own responsibility; to enable it, confirm it has been published and is available from an install source
```

**The version.** Without one, `add` takes the latest version in the install source and writes that **exact version**
into `brickkit.yaml` — like npm writing a lockfile, resolution happens once, and everyone who clones the project later
gets the same version. With a version (`brickkit add demo/caller@1.0.0`), that version is used. Ranges like `^1.0.0` or
`latest` are always refused: version ranges are the classic source of "it works here and breaks in production".

**Dependencies come along recursively.** `demo/caller` requires `demo/hello@1.0.0`, so `demo/hello` was added with it.
Its optional dependency `demo/bus` couldn't be found in the install source: a missing optional dependency only warns and
never blocks, and its address variable `DEMO_BUS_ENDPOINT` is **not injected at all** — not injected as an empty string.
The component sees "no such variable" and takes its own degraded path.

## What one `add` changes

**`brickkit.yaml`**: the declaration and the exact versions.

```yaml
components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
```

**The deploy file**: one entry per component version. Nothing written means "run it the default way". When
`deploy.local.yaml` exists, it gets the same entries.

```yaml
components:
  - id: demo/hello
  - id: demo/caller
```

**`config/`**: a skeleton generated from each component's `configSchema` (the config spec sheet the component's author
wrote). Items that are required and have no default are written as `KEY: ""` for you to fill in; the rest are comments,
and while commented they use the component's default.

```yaml
# Component: demo/caller@1.0.0
# Environment variables for this component; every key is injected as-is.
# Shared variable: $var:NAME (config/vars.yaml) · environment variable: ${NAME} · local file: file://path

# === Optional: commented keys use the component's default; uncomment to override ===
# DATABASE_HOST:  # string | PostgreSQL host name
# DATABASE_NAME:  # string | Database name
# DATABASE_PORT: 5432  # integer | PostgreSQL port (default)
# DATABASE_USER:  # string | User to connect as
# MIGRATION_SHOULD_FAIL:  # string | Test switch: set to "1" to make the migration exit non-zero, to check that a failed migration holds back the main service
```

When there are required items to fill in, `add` names the file and the keys:

```text
📝 Config skeletons: config/demo-widget.yaml
   ✏️ Fill in the required keys in config/demo-widget.yaml: API_URL
```

`up` without filling them in refuses to start and tells you which item is missing.

**`.brickkit/manifests/<id>/<version>/`**: the component's `component.yaml` and its documentation, `BRICKKIT.md` plus
every translation it carries (`BRICKKIT.zh.md` and the like), cached permanently — from a local, Git or market source
alike.

**`.brickkit/artifacts/`**: the contract files the component declares (OpenAPI, proto and the like), which you'll use when
writing a caller.

**`AGENTS.md`**: the component table at its end — the block the CLI maintains — gets a row for each component added:
version, what it does, which docs it carries, its home page (see
[The project's `AGENTS.md`](01-init-and-project-creation.md#the-projects-agentsmd)). Nothing outside the block changes,
and an `AGENTS.md` without the block is left as it is.

If the project doesn't load after the changes (say, the new component conflicts with an existing one), `add` puts every
changed file back — all of it, or nothing.

## The shared-variable prompt

Several components often connect to the same database. When `config/vars.yaml` already has a shared variable with the
same name as a config item, `add` asks whether to reference it directly:

```yaml
# config/vars.yaml
DATABASE_HOST: pg.internal
DATABASE_PORT: 5432
```

```text
config/vars.yaml already has DATABASE_HOST; reference it in config/demo-caller.yaml? [y/N] y
config/vars.yaml already has DATABASE_PORT; reference it in config/demo-caller.yaml? [y/N] 
```

An item you said yes to becomes a `$var:` reference; the others stay commented:

```yaml
DATABASE_HOST: $var:DATABASE_HOST  # string | PostgreSQL host name
# DATABASE_PORT: 5432  # integer | PostgreSQL port (default)
```

Why ask instead of referencing automatically: where each value in a config file comes from should be visible at a
glance. The rules of `$var:` are in [Shared variables and $var:](../01-three-layers/06-vars-and-var-ref.md).

## Shells

Adding a shell (a component that compiles several components into one process) also adds the members it compiles in, at
the versions the shell declares, and in the deploy file they're **nested under the shell's entry as `members`** — which
component hosts which member is written there and only there. The other way round, adding an ordinary component never
puts it into a shell for you: that's a decision about how to deploy, made by you in the deploy file. See
[Shells](../04-shell/README.md).

## Two versions of the same component

`demo/caller@1.0.0` wants `demo/hello@1.0.0`. If the project's `demo/hello` is already 1.1.0, `add` doesn't fail: it keeps
1.0.0 as well, marked with who needs it:

```yaml
components:
  - id: demo/hello
    version: 1.1.0
  - id: demo/hello
    version: 1.0.0
    requiredBy: [demo/caller]
```

Both versions run at once, as the services `demo-hello-1-1-0` and `demo-hello-1-0-0`, without clashing. The one without
`requiredBy` is the **default version**. See [Several versions side by side](../03-component-guide/09-multi-version-coexistence.md).

## `--local`: add everything in the local sources at once

```bash
brickkit add --local
```

Scans every local install source (`components/` and `shell/` by default) and adds each `<scope>/<name>/component.yaml` at
the **real version** written in it. A component you just wrote with `brickkit new`, or one you put into `components/` by
hand, joins the project with this one command.

`--local --init` first gives every local component without a `brickkit.yaml` its own local workbench in its directory;
see [Developing inside a component](../03-component-guide/05-local-dev-fractal.md).

## Cloning source: `--repo`, `--repo-all`

By default `add` takes only the component's `component.yaml` and contract files and **doesn't clone its source**: most of
the time you use a component rather than change it. When you want to change its code:

```bash
brickkit add demo/hello@1.0.0 --repo
```

```text
✅ demo/hello@1.0.0 is already in the project; none of the three files needs a change
📥 Cloned the source of demo/hello@1.0.0 into components/demo/hello/ (checked out 1.0.0)
```

When the component is already in the project, `--repo` only does the cloning. The cloned directory has this version's
tag checked out; it's a complete Git repository, and branches, commits and pushes are yours to manage — the CLI doesn't
handle Git permissions.

Note: `components/` is a local install source, and it comes before the Git source. After cloning, the component resolves
from your working copy — the code you change is what `brickkit build` puts into the image, and the "latest version"
`upgrade` sees is the working copy's version (see [Upgrading](07-upgrade-and-migration.md#when-a-component-comes-from-a-local-source)).

`--repo-all` clones the source of every open-source component this add brings in (each at its default version).

Wherever you run it inside the project — even in another component's directory — `add` works on the project, so the
clone always lands in the project's own `components/`: component source lives in one place (see
[Developing inside the project](04-focus-run.md#one-components)).

**Git submodules are never fetched.** The clone leaves them as empty directories and says which:

```text
📥 Cloned the source of demo/lib@1.0.0 into components/demo/lib/ (checked out 1.0.0)
   ℹ️  demo/lib@1.0.0 has git submodules, not fetched: third_party/sdk (git submodule update --init fetches them if you need them)
```

Fetch them in the cloned repository yourself if the code needs them.

## `--yes`: non-interactive

```bash
brickkit add demo/caller@1.0.0 --yes
```

Answers yes to every question, for CI. Without `--yes` and with no terminal input (in a script, say), every question is
answered no.
