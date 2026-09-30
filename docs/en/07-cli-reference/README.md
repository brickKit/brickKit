# CLI reference

The BrickKit CLI has 21 business commands, plus `version` and `lang`, which are about the CLI itself. This page covers
every command, every subcommand and every flag; it agrees with `brickkit <command> --help`, and `--help` is the
authority.

There is no long-running process: every command exits when it's done. What a project *should* look like is written in
its three layers (`brickkit.yaml`, the deploy file, `config/`; see [The three layers](../01-three-layers/README.md));
what it *actually* looks like lives in the engine underneath (Docker / Podman / Kubernetes).

## All commands

| Group | Command | In one line |
| --- | --- | --- |
| Project | [`init`](#brickkit-init) | Create a project, or complete a directory into one |
| Project | [`skills`](#brickkit-skills) | Manage the AI assistant skills installed in the project |
| Components | [`new`](#brickkit-new) | Generate a component (or shell) skeleton |
| Components | [`add`](#brickkit-add) | Fetch components and their dependencies, write the three layers |
| Components | [`remove`](#brickkit-remove) | Remove a component; its config moves to `config/.archive/` |
| Components | [`upgrade`](#brickkit-upgrade) | Move a component's default version, migrating its config |
| Components | [`fetch`](#brickkit-fetch) | Download only a component's artifacts (contracts), without adding it |
| Inspect | [`lint`](#brickkit-lint) | Offline check of the three layers and `component.yaml` files |
| Inspect | [`deps`](#brickkit-deps) | Print the dependencies as a tree |
| Inspect | [`graph`](#brickkit-graph) | Draw the dependency topology as a Mermaid diagram |
| Run | [`build`](#brickkit-build) | Build the images that are built locally |
| Run | [`up`](#brickkit-up) | Generate deployment files, run migrations, start |
| Run | [`down`](#brickkit-down) | Stop the project (volumes are kept) |
| Run | [`status`](#brickkit-status) | Show the running state |
| Run | [`local`](#brickkit-local) | Manage local mode (the personal `deploy.local.yaml`) |
| Source | [`sync`](#brickkit-sync) | Move component source not needed this run into `components/.archived/` |
| Source | [`restore`](#brickkit-restore) | Put `mode` and the source layout back to the last commit |
| Release | [`release`](#brickkit-release) | Release a component through Git: tag and push, no market |
| Release | [`publish`](#brickkit-publish) | Publish a component to a component market |
| Release | [`login`](#brickkit-login) | Log in to a component market |
| Release | [`logout`](#brickkit-logout) | Log out of a component market |
| CLI | [`version`](#brickkit-version) | The CLI version, the supported manifest version and deploy targets |
| CLI | [`lang`](#brickkit-lang) | Show or change the language the CLI speaks |

## Global flag

There is only one, and every command accepts it:

| Flag | Meaning |
| --- | --- |
| `--log-level <level>` | Level of the JSON log lines on stderr: `debug` / `info` / `warn` / `error` / `off`, default `warn` |

The default `warn` is quiet: only when something fails does a JSON log line carrying `error_code` follow the `❌` block,
for scripts to act on (the codes are in the [error-code reference](../06-architecture/09-error-codes.md)). To see the
routine log lines around each command, use `--log-level info`; to get that for a whole shell session, set
`BRICKKIT_LOG_LEVEL=info` once. `--log-level off` also drops the JSON line on failure — only for places where nothing
parses `error_code` (a pre-commit hook, say).

## Flags shared by the commands that read a deploy file

`up`, `down`, `status`, `sync` and `lint` read a deploy file and all accept the two flags below; `graph` accepts only
`-f` (it never reads local mode anyway).

| Flag | Meaning |
| --- | --- |
| `-f, --file <file>` | Read only this deploy file (such as `deploy.prod.yaml`, relative to the project root); local mode is ignored. A missing file is an error |
| `--no-local` | Ignore `deploy.local.yaml` this time and read `deploy.yaml`; the local-mode switch stays as it is |

Without either flag, local mode decides: on, `deploy.local.yaml` is read; off, `deploy.yaml`
(see [deploy.local.yaml](../01-three-layers/04-deploy-local-yaml.md)). One complete deploy file per environment, chosen
with `-f` — that is all there is to several environments:

```bash
brickkit up -f deploy.prod.yaml
```

```text
❌ Error: the deploy file given with --file does not exist
   Path: deploy.prod.yaml
   Suggestion: The path is relative to the project root; check the spelling, for example -f deploy.prod.yaml
```

---

## `brickkit init`

Create a BrickKit project, or complete a directory into one. Both modes share one completion primitive — compare against
the file list of a complete project, create what's missing, leave what exists:

| Written | Mode | What it does |
| --- | --- | --- |
| `brickkit init <name>` | Create | Make a new `<name>/` directory and generate the whole skeleton in it |
| `brickkit init` | Complete | Add whatever the current directory lacks: bring an existing project in, or give a component repository its own local workbench |

A complete project has `brickkit.yaml`, `deploy.yaml`, `config/vars.yaml`, the two local sources `components/` and
`shell/`, `BRICKKIT.md` (the project map) and `.gitignore`. `deploy.local.yaml` is never created here; `brickkit local on`
creates it when you want it.

The rules in completion mode:

- Existing files are skipped; not a byte of them changes.
- An existing `.gitignore` is checked, never modified: each missing required entry gets a loud warning — without them,
  personal deploy files and secrets get committed.
- In a non-empty directory the plan is printed first and carried out after you confirm (`--yes` skips the question).
- The project name is `--name`, otherwise the existing `brickkit.yaml`'s `project`, otherwise the directory name.
- At the end the project is loaded the way `lint` and `up` load it; if that fails, the command fails.

A project name may contain only lowercase letters, digits and hyphens, starting and ending with a letter or digit — it
becomes the name of the Kubernetes namespace and the Docker network.

`init` also installs the AI assistant skills (`.claude/skills/`, `AGENTS.md`) without touching your `CLAUDE.md`. When
the project root is the Git repository root, it also installs the pre-commit check (a pre-commit hook that calls
`brickkit restore --check`); when the project sits inside someone else's repository, install it explicitly with
`--hooks`.

```text
brickkit init [<project-name>] [flags]
```

| Flag | Meaning |
| --- | --- |
| `--name <name>` | The project name in completion mode |
| `--yes` | Complete without asking (for CI) |
| `--no-skills` | Don't install the AI assistant skills |
| `--hooks` | Only install the pre-commit hook used for the pre-commit check (to add it to an existing project) |

```bash
brickkit init my-shop                 # new my-shop/ with the whole skeleton
brickkit init                         # complete the current directory (asks first if it isn't empty)
brickkit init --name my-shop --yes    # complete without asking (CI)
brickkit init my-shop --no-skills     # without the AI assistant skills
brickkit init --hooks                 # only the pre-commit check
```

```text
✅ Project initialized: my-shop
   📄 brickkit.yaml        Project config
   📄 deploy.yaml          How it is deployed (team file, committed)
   📁 config/              Component configuration and shared vars
   📁 components/          Component source (configured as the local install source local-dev)
   📁 shell/               Shells (kind: shell), project code (the local install source local-shells)
   📁 .brickkit/           CLI working directory
   📄 BRICKKIT.md          Project map: components and where their docs are
```

## `brickkit skills`

Manage the AI assistant skills installed in this project (`.claude/skills/`, `AGENTS.md`). `init` installs these files;
they're committed with the project and shared by the team. They describe the behaviour of **this CLI version**, so
refresh them once after upgrading the CLI. Without a subcommand it's the same as `brickkit skills status`.

**A file you edited is never overwritten**: `update` lists it and skips it. To drop your local edits, delete the file and
run `update` again — there's deliberately no `--force`: deleting the file is explicit enough, and one more switch would
be one more way to lose work by accident.

It also works in a standalone component repository (a `component.yaml` and no `brickkit.yaml`): there it manages only the
`brickkit-component` skill and doesn't touch a character of the repository's own `AGENTS.md`. It never touches your
`CLAUDE.md`.

```text
brickkit skills [flags]
brickkit skills <command> [arguments]
```

### `brickkit skills status`

The state of each skill file (read-only). No flags of its own.

```text
Skill language: en (recorded in skills.lock; brickkit skills update --lang to change it)
   ┌───────────────────────────────────────────────┬────────────┐
   │ File                                          │ Status     │
   ├───────────────────────────────────────────────┼────────────┤
   │ AGENTS.md                                     │ up to date │
   │ .claude/skills/brickkit-assemble/SKILL.md     │ up to date │
```

### `brickkit skills update`

Install what's missing and refresh what's out of date; skip what you edited.

| Flag | Meaning |
| --- | --- |
| `--lang <en\|zh>` | Reinstall in this language, and remember it as the project's language from now on |

```bash
brickkit skills update
brickkit skills update --lang zh
```

## `brickkit new`

Generate a component skeleton: a `component.yaml` that passes `brickkit up --dry-run` validation, and a `BRICKKIT.md`
with the standard sections (purpose, dependencies, configuration, contracts, shell declaration) for the people and AI
assistants who will use the component.

It writes to `components/<scope>/<name>/` by default — the layout a local install source scans, so afterwards
`brickkit add --local` brings it into the project. It **doesn't** run `add` for you: writing into `brickkit.yaml` is a
separate step you can review.

```text
brickkit new <scope>/<name> [flags]
```

| Flag | Meaning |
| --- | --- |
| `--contract <openapi\|proto>` | Also generate a contract placeholder, registered under `artifacts`; none by default |
| `--shell` | Generate a shell skeleton (`shell.members` with a placeholder member to replace), written to `shell/<scope>/<name>/` by default |
| `--path <dir>` | Write somewhere else, for a component that is its own Git repository; the directory is then the repository root, without the `<scope>/<name>` layer |

```bash
brickkit new demo/widget                          # writes components/demo/widget/
brickkit new demo/widget --contract openapi       # with an OpenAPI contract placeholder
brickkit new erp/shell --shell                    # a shell skeleton in shell/erp/shell/
brickkit new demo/widget --path ../widget-repo    # somewhere else (its own repository)
```

## `brickkit add`

Fetch a component and its dependencies, and write all three layers in one go: the declaration in `brickkit.yaml`, the
entry in the deploy file (`deploy.yaml`, and `deploy.local.yaml` when it exists), the config skeleton in `config/`.

- Without a version, the latest exact version in the install source is taken and pinned in `brickkit.yaml`.
- When a dependency wants another version of a component the project already has, that version is kept with
  `requiredBy` (versions side by side).
- Adding a shell also adds the members it compiles in, at the declared versions, nested under the shell's entry as
  `members`.
- In the config skeleton, only "required with no default" keys are written as `KEY: ""`; the rest are commented out.
  When there are required items to fill in, `add` names the file and the keys.
- When `config/vars.yaml` has a shared variable of the same name, it asks whether to write a `$var:` reference instead.
- When `config/.archive/` holds this component's earlier config (left by `remove`), it's restored through the migration
  rules instead of a blank skeleton.
- If the project doesn't load after the changes, everything is put back.

```text
brickkit add [component-ID[@exact-version]] [flags]
```

| Flag | Meaning |
| --- | --- |
| `--local` | Add every component in the local install sources at once |
| `--init` | With `--local`: first give every local component without a `brickkit.yaml` its own local workbench |
| `--repo` | Also clone this component's source repository into `components/`, at this version |
| `--repo-all` | Also clone the source of every open-source component this add brings in (default versions) |
| `-y, --yes` | Non-interactive: answer yes to every question (for CI) |

```bash
brickkit add erp/backend                    # latest tag, pinned as an exact version
brickkit add erp/backend@1.0.0              # a given version
brickkit add --local                        # every component in the local sources
brickkit add --local --init                 # …with a local workbench for each first
brickkit add erp/backend@1.0.0 --repo       # and clone the source (at this version)
brickkit add erp/backend@1.0.0 --yes        # non-interactive (CI)
```

```text
🔎 No version given for demo/greeter; the latest is 0.1.0 (install source local-dev)
➕ Adding demo/greeter@0.1.0
   ✅ demo/hello@0.1.0
   ✅ demo/greeter@0.1.0
📝 Written: brickkit.yaml, deploy.yaml
📝 Config skeletons: config/demo-greeter.yaml
```

## `brickkit remove`

First checks whether another component depends on it (required), then removes it:

- The config isn't deleted but moved to `config/.archive/` — adding the component back later restores it from there.
- Its entries in the deploy files (`deploy.yaml`, and `deploy.local.yaml` when it exists) go too.
- Removing a shell moves the members it hosted back to the top level, running on their own.
- Compatibility versions kept only for it (a `requiredBy` naming only it) are removed with it.
- When the default version is removed and only one version of the component is left, that one becomes the default.
- A component's source directory (and any archived copy) is deleted only when its last version goes, and only after
  checking it could be recovered: uncommitted or unpushed changes stop the command.

```text
brickkit remove <component-ID>[@version] [flags]
```

| Flag | Meaning |
| --- | --- |
| `--force` | Delete the source directory even with uncommitted or unpushed changes |

```bash
brickkit remove erp/backend                 # when there is only one version
brickkit remove erp/backend@1.0.0           # name the version when there are several
brickkit remove erp/backend@1.0.0 --force   # delete even with unpushed changes in the source directory
```

## `brickkit upgrade`

Move a component's default version, with the three layers following:

- the version and `requiredBy` in `brickkit.yaml`;
- the deploy entries;
- the migration of `config/`: values you wrote are carried over, new keys take their defaults, keys the new version
  dropped stay in the archive and are reported.

When other components still depend on the old version, it stays as a compatibility version. Upgrading a shell switches
to the member versions the new shell compiles in. Without a component, every component with a newer version is
upgraded — all of them, or none.

A key you changed whose default the component's author also changed is a **conflict**: in a terminal you choose, one
key at a time; with `--yes` or no terminal input, both lines are written as duplicate keys, and `up` refuses to start
until you delete one (see [Where a value comes from](../01-three-layers/08-resolution-priority.md#the-same-key-twice-at-one-level)).

```text
brickkit upgrade [component-ID[@version]] [flags]
```

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Only show what would happen; change no file |
| `-y, --yes` | Non-interactive: write every config conflict as a duplicate key |

```bash
brickkit upgrade                    # every component with a newer version
brickkit upgrade erp/backend        # to the latest
brickkit upgrade erp/backend@2.0.0  # to a given version
brickkit upgrade --dry-run          # only see what would happen
```

## `brickkit fetch`

Download the artifact files a component declares (`.proto`, `openapi.json` and the like) **without adding it to this
project**. For calls across projects: you want the other side's contract to generate a client, while that service is
deployed by another project — writing it into `brickkit.yaml` would make the platform deploy another copy on your side.

Without a version, the latest version in the install source is used. Artifacts land in
`.brickkit/artifacts/<versioned-service-name>/<type>/...`, the same place `add` puts them. `.brickkit/` is a local cache
and isn't committed: teammates run the same `fetch` on their machines; to commit a contract with the project, copy the
files you need into your own directory.

The command is read-only: it doesn't change `brickkit.yaml`, generate deployment files or start anything. No flags of
its own.

```text
brickkit fetch <component-ID>[@<version>] [flags]
```

```bash
brickkit fetch infra/notifier@1.0.0   # the artifacts of a given version
brickkit fetch infra/notifier         # the artifacts of the latest version
```

## `brickkit lint`

An offline, read-only check of the YAML in this directory, with no Docker or Kubernetes needed. It works out the
situation from the current directory:

**A project (there's a `brickkit.yaml`):**

1. `brickkit.yaml`;
2. the deploy files: `deploy.yaml`, and `deploy.local.yaml` when it exists (with `-f`, only that one) — both must match
   `brickkit.yaml`, whether local mode is on or not;
3. the three layers together: exactly one deploy entry per component version, members nested under their shell, every
   `$var:` defined, `config/` files matching components, no duplicate keys left from an upgrade;
4. each component's config against its `configSchema`: required items have values, written keys are in the schema,
   shell members' values can be JSON-encoded into the shell; and the shell declarations: `kind: shell` agrees with the
   `shell` block, members nested under a shell are really compiled into it — only for components whose manifest is on
   disk; the rest are listed as unchecked;
5. every `component.yaml` under the local install source directories, added or not (`.archived/` excluded).

If `brickkit.yaml` itself fails, nothing after it can be trusted; the rest is skipped, and it says so.

**A component repository (a `component.yaml`, no `brickkit.yaml`):** only that `component.yaml`.

It checks structure: required fields, types, unknown fields (typos), version format, port ranges. There are two kinds of
warning: a mistyped key inside `configSchema` (like `defualt`) that has no effect, and a config item name that collides
with a reserved platform variable. It **doesn't** check whether dependencies resolve, or whether the member versions a
shell hosts this run match the ones it compiles in (that needs the resolved dependency graph, which `lint` deliberately
never builds — it might mean going to the network); those are left to `up --dry-run` and `graph`. Nor does it check
config values against `enum` or `minimum` (the platform checks key names, not values).

The exit code is 1 with errors, 0 with only warnings.

```text
brickkit lint [flags]
```

| Flag | Meaning |
| --- | --- |
| `--strict` | Also check references: a `${VAR}` in neither the process environment nor `.env`, a `file://` whose file doesn't exist, as warnings; and warnings count as failures (exit code 1), for a CI gate |
| `-f, --file <file>` | Check only this deploy file; see [shared flags](#flags-shared-by-the-commands-that-read-a-deploy-file) |
| `--no-local` | Ignore `deploy.local.yaml` this time |

```bash
brickkit lint
brickkit lint --strict           # warnings fail too (CI gate)
brickkit lint -f deploy.prod.yaml
```

```text
✅ brickkit.yaml
✅ deploy.yaml
✅ cross-file: brickkit.yaml ↔ deploy.yaml ↔ config/
✅ components/demo/greeter/component.yaml
✅ components/demo/hello/component.yaml

📋 Checked 5 files: 0 with errors, 0 warnings
```

## `brickkit deps`

Print the project's dependencies as a tree. `brickkit.yaml` only locks versions; who depends on whom is written in each
component's own `component.yaml`, and this command reads it from there — the same resolved dependency graph `graph`
draws (manifests not yet cached are fetched from the install sources).

| Written | Output |
| --- | --- |
| `brickkit deps` | One tree per top-level component (nothing in the project depends on it) |
| `brickkit deps <id>` | This component's tree (one per version in the project), and what depends on it |
| `brickkit deps <id>@<version>` | Only that version |

Within one output, a component version is expanded only once; later appearances are marked "(see above)"; optional
dependencies are marked "(optional)", and optional ones missing from the project "(optional, not installed)". No flags of
its own.

```text
brickkit deps [<id>[@<version>]] [flags]
```

```bash
brickkit deps
```

```text
demo/greeter@0.1.0
└── demo/hello@0.1.0
```

```bash
brickkit deps demo/hello
```

```text
demo/hello@0.1.0

Required by: demo/greeter@0.1.0
```

## `brickkit graph`

Draw the dependency topology as a Mermaid diagram on stdout:

| In the diagram | Means |
| --- | --- |
| Node | A component `id@version`; a `mode: local` component is marked as "managed locally" (with its port when `localPort` is written) |
| Solid / dashed line | Required / optional dependency (an optional one that can't be found is drawn as a "not installed" node) |
| Greyed out | Components that won't start this run (turned off with `mode: disable`, or following the ones above) |
| Group | Members a shell hosts this run are drawn inside the shell's subgraph |

`graph` **never reads local mode**: it reads `deploy.yaml` (or the file given with `-f`), so its output can be committed
and shared, and doesn't depend on who generated it. Stdout carries nothing but Mermaid, so `brickkit graph > graph.mmd`
gives you a file GitHub renders directly; warnings from dependency resolution go to stderr.

```text
brickkit graph [flags]
```

| Flag | Meaning |
| --- | --- |
| `--ignore-shells` | Ignore every shell's `members` in memory before drawing, to see each component as if it started on its own; nothing is written back |
| `-f, --file <file>` | Draw this deploy file; see [shared flags](#flags-shared-by-the-commands-that-read-a-deploy-file) |

```bash
brickkit graph
brickkit graph > graph.mmd
brickkit graph --ignore-shells
```

```text
graph TD
    demo_hello_0_1_0["demo/hello@0.1.0"]
    demo_greeter_0_1_0["demo/greeter@0.1.0"]
    demo_greeter_0_1_0 --> demo_hello_0_1_0
```

## `brickkit build`

Build the images that are built locally: components without a `deployment.image`, and components in local install
sources (code being developed, whose image has to be built from it). A git or market component with an `image` is
pulled, not built.

The image tag equals the component's `metadata.version`; a shell's image records the member versions compiled into it,
which `up` checks. An image that already exists is skipped. The source is the local repository (when it's at this
version), otherwise an export of this version's Git tag. **`up` never builds**: when an image is missing it stops and
tells you to run `build`.

```text
brickkit build [component-ID[@version]] [flags]
```

| Flag | Meaning |
| --- | --- |
| `--force` | Rebuild even when the image already exists |

```bash
brickkit build                      # every component built locally
brickkit build erp/backend          # only this component
brickkit build erp/backend --force  # rebuild after changing the code
```

## `brickkit up`

Start the project in one go:

1. load the three layers and every component's manifest;
2. decide what runs (follow the ones above: a top-level component without `mode` runs, the ones below follow);
3. check required dependencies (missing: error) and optional ones (missing: a warning, and no environment variable at all);
4. sort topologically into a start order;
5. resolve config, inject environment variables, merge resource settings, and generate the deployment files:
   `docker` / `podman` get `.brickkit/generated/compose.yaml`, `k8s` gets Kubernetes manifests;
6. for `mode: debug` components, generate `local-debug.<versioned-service-name>.env` to start them from your IDE;
7. check images: locally built images must already exist (otherwise it points at `brickkit build`), pulled images must
   be reachable;
8. call the engine; database migrations run first (a one-shot container on Docker, a Job on Kubernetes), and a failure
   holds back the main service;
9. for `mode: local` components, start and watch those processes in the foreground; `Ctrl+C` stops them.

When a component version differs from the last `up`, it says so, and `--dry-run` also prints a change summary; moving a
version is `upgrade`'s job. On the `k8s` target, before a real deployment (without `--dry-run`), `up` first confirms the
cluster it's about to deploy to (the deploy file's `k8s.context`), before generating anything.

```text
brickkit up [flags]
```

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Only generate the deployment files, don't start (prints a change summary when versions changed) |
| `--ignore-shells` | Ignore every shell's `members` in memory and run once more, to verify each component can start on its own; nothing is written back |
| `--crash-lines <N>` | When a `mode: local` component crashes, how many of its last output lines to show at the end; default 20, `0` shows only the crash information |
| `-f, --file <file>` | Use this deploy file; see [shared flags](#flags-shared-by-the-commands-that-read-a-deploy-file) |
| `--no-local` | Ignore `deploy.local.yaml` this time |

```bash
brickkit up
brickkit up --dry-run              # only generate the files, start nothing
brickkit up -f deploy.prod.yaml    # use another deploy file (one complete file per environment)
brickkit up --no-local             # ignore deploy.local.yaml this time
brickkit up --ignore-shells --dry-run
```

```text
🚀 Starting project my-shop (target: docker)
📋 Component state calculation:
   ✅ demo/hello@0.1.0    starting (demo/greeter needs it)
   ✅ demo/greeter@0.1.0  starting (top-level)

📋 Start order (topological sort):
   1. demo-hello-0-1-0    no dependencies
   2. demo-greeter-0-1-0  ← depends on 1

Can start on their own: demo-hello-0-1-0 (no dependencies)
Longest dependency chain (2 levels): demo-hello-0-1-0 → demo-greeter-0-1-0

Dependency graph:
   demo/greeter@0.1.0 → demo/hello@0.1.0
📄 Generated: .brickkit/generated/compose.yaml

💡 --dry-run only generates the files and starts no component
   View it: cat .brickkit/generated/compose.yaml
```

## `brickkit down`

Stop the project. The stop order is the reverse of the start order (dependents first, then what they depend on), left to
the engine.

**`down` doesn't delete data volumes**; database data always stays. For a full cleanup, run `docker volume rm` yourself
(or `docker compose down -v`; the same with Podman). To stop only some components, write `mode: disable` on their deploy
entries and `up` — the engine removes their containers too, and the config keeps a record, so the next `up` doesn't bring
them back. A `mode: local` process belongs to the terminal that ran `up`; `down` can't reach it and only names that
session's process ID.

```text
brickkit down [flags]
```

| Flag | Meaning |
| --- | --- |
| `-f, --file <file>` | Stop by this deploy file's target; see [shared flags](#flags-shared-by-the-commands-that-read-a-deploy-file) |
| `--no-local` | Ignore `deploy.local.yaml` this time |

```bash
brickkit down
```

## `brickkit status`

The running state of every component in the project. The CLI stores no running state itself; it asks the engine for the
deploy file's `target`:

| target | How it asks |
| --- | --- |
| `docker` | `docker compose ps -a --format json` |
| `podman` | `podman compose ps -a --format json` |
| `k8s` | `kubectl get deployments -o json` (in the project's namespace) |

The output shows running components, components not started and why (marked when the reason comes from
`deploy.local.yaml`), and local debug components (`mode: debug`). `mode: local` components are processes watched by `up`
in another terminal, so they aren't in the table; when such a session is running, it says which process.

```text
brickkit status [flags]
```

| Flag | Meaning |
| --- | --- |
| `-f, --file <file>` | Ask by this deploy file; see [shared flags](#flags-shared-by-the-commands-that-read-a-deploy-file) |
| `--no-local` | Ignore `deploy.local.yaml` this time |

```bash
brickkit status
```

## `brickkit local`

Manage local mode: your personal deploy file `deploy.local.yaml` (never committed). With local mode on, the commands
that run or check the deployment (`up`, `down`, `status`, `sync`, `lint`, `build`; not `graph` or `deps`) read
`deploy.local.yaml` instead of `deploy.yaml` — the whole file, not a merge of the two, so what the file says is what
runs. Personal facts go here: `mode: debug` on the component you're debugging, a free `localPort` on your machine, `vars`
values for your own database, even another `target` (see [deploy.local.yaml](../01-three-layers/04-deploy-local-yaml.md)).

The file's components must match those declared in `brickkit.yaml` exactly: when the team adds a component, `up` refuses
to run until you `refresh` (or edit the file by hand, or turn local mode off). A single run can ignore it with
`--no-local`; `-f <file>` ignores it too.

```text
brickkit local [flags]
brickkit local <command> [arguments]
```

None of the four subcommands has flags of its own.

### `brickkit local on`

Turn local mode on; the first time, `deploy.local.yaml` is copied from `deploy.yaml` — word for word, except that the
comment lines at the top (the team-file header `init` wrote) are replaced with a personal-file header in the CLI's
current language.

```text
✅ Local mode is on: commands now read deploy.local.yaml
   deploy.local.yaml was copied from deploy.yaml — change it as you like, it is not committed
```

### `brickkit local off`

Turn local mode off. The file stays; the next `local on` uses it again.

```text
✅ Local mode is off: commands now read deploy.yaml
   deploy.local.yaml is kept; brickkit local on uses it again
```

### `brickkit local status`

The switch, the file, and whether the file still matches `brickkit.yaml`.

```text
Local mode: on
deploy.local.yaml: present, in use
✅ deploy.local.yaml matches brickkit.yaml
```

### `brickkit local refresh`

Copy again after the team changes `deploy.yaml`. The old file is kept as `deploy.local.yaml.bak`, and every local change in
it is listed for you to merge back.

```bash
brickkit local refresh
```

## `brickkit sync`

Move component source you don't need right now from `components/` into `components/.archived/`. With many components,
source you aren't touching still piles up in `components/`: IDE indexing, global search, `grep` and an AI reading code for
you all scan it anyway. `sync` moves it into one fixed directory — out of the way until you open it, and easy to find when
you need it.

**The criterion is exactly `up`'s**: what starts this time stays active, what doesn't is archived. To narrow the scope,
change `mode` in the deploy file (turn off a top-level component and the ones below it follow), and `sync` follows.

- Both directions: archive what should be archived, bring back what should be active;
- it doesn't affect running containers or what `up` starts — it only moves directories;
- it only touches components declared in `brickkit.yaml` whose source is present;
- the whole directory moves, `.git` included, so git keeps working after archiving;
- there's deliberately no `--dry-run`: if it got it wrong, running it again brings things back.

```text
brickkit sync [flags]
```

| Flag | Meaning |
| --- | --- |
| `-f, --file <file>` | Decide by this deploy file; see [shared flags](#flags-shared-by-the-commands-that-read-a-deploy-file) |
| `--no-local` | Ignore `deploy.local.yaml` this time |

```text
📂 Workspace tidying:
   ✅ components/demo/greeter/             active
   ✅ components/demo/hello/               active
✅ Workspace tidied (2 active, 0 archived, 0 activated)
```

## `brickkit restore`

Put each component's `mode` in `deploy.yaml` back to its value in the last commit, then have the source layout follow.

Who it's for: projects that took `components/` out of `.gitignore` so component source is committed with the project.
There, `sync` moving directories shows up in the project's diff, and "turn off a few top-level components, `sync`
archives, forget to put it back, commit" is bound to happen.

It only touches the `mode` field in `deploy.yaml` (the team file), entry by entry:

| Entry | Treatment |
| --- | --- |
| In both the working tree and the last commit | `mode` goes back to the committed value (removed when the commit has none) |
| New in the working tree (just `add`ed, or a bare id turned into `id@version`) | Left exactly as it is |
| In the commit but not in the working tree | Never added back (this isn't `git revert`) |

`deploy.local.yaml` is personal and not committed, so there's no baseline to go back to; it's left alone. The values it
overwrites are printed before it acts.

```text
brickkit restore [flags]
```

| Flag | Meaning |
| --- | --- |
| `--check` | Only check, change nothing: are the `deploy.yaml` and the directory layout about to be committed consistent? If not, exit non-zero — this is what the pre-commit hook calls |

```bash
brickkit restore           # put mode and the source layout back
brickkit restore --check   # only check whether this commit is consistent
```

## `brickkit release`

Release a component without a market: releasing is pushing a Git tag to the component's own repository. The version is
`metadata.version` in `component.yaml` — the single source of truth. Only `component.yaml` is read; a `brickkit.yaml` in
the same directory (the author's local workbench) has nothing to do with the release.

Before anything is written, all of these must pass:

- `component.yaml` parses and validates;
- the component directory has no uncommitted changes (for a component in a monorepo subdirectory, only its own directory counts);
- the current branch has an upstream and no unpushed commits: the commit being tagged must already be in the remote's history;
- the tag doesn't exist yet (checked locally and on the remote).

Then the tag is created and pushed. If the push fails, the local tag is deleted — a release either completes or leaves
no trace. When the component directory is the repository root the tag is `<version>`; in a subdirectory it's
`<scope>-<name>/<version>` — exactly the name a git install source reads.

```text
brickkit release [flags]
```

| Flag | Meaning |
| --- | --- |
| `--path <dir>` | The component directory (where `component.yaml` is), default the current directory |
| `--local` | Release every component in the project's local install sources: check them all first, then tag and push one by one; already-released ones (the tag is on the current commit) are skipped; the first failed push stops it |

```bash
brickkit release                                  # the component in the current directory
brickkit release --path ./components/erp/backend
brickkit release --path svc/api                   # a component in a monorepo subdirectory (tag erp-api/<version>)
brickkit release --local                          # every local-source component in the project
```

## `brickkit publish`

Publish a component to a component market (releasing through Git is the other command, `release`). There is no public
market operated by BrickKit today; the market address points at an instance you or your organisation run.

1. Check you're logged in (`.brickkit/credentials` or the install source's `authToken`); if not, it's an error;
2. read and validate the `component.yaml` in the component directory;
3. check the image reference is valid, and that every file `artifacts` declares is there;
4. create a draft version → upload the artifacts → move it to stable;
5. set the visibility.

The three steps are on purpose: when a version moves to stable, the market checks that "the files match what
`artifacts` declares", and creating a draft first guarantees there's never a half-finished "stable but files missing"
version. `--path` can also point at an archive directory, such as `./components/.archived/erp/backend`.

```text
brickkit publish [flags]
```

| Flag | Meaning |
| --- | --- |
| `--path <dir>` | The component source directory (containing `component.yaml`), default the current directory |
| `--market <address>` | The market address, default the market install source in `brickkit.yaml` |
| `--visibility <public\|private>` | Visibility, default the market-side setting |
| `--changelog <text>` | Release notes for this version |
| `--source-type <git\|registry>` | Source type: `git` (open source) or `registry` (closed source), inferred from the component directory's git remote by default |
| `--git-url <address>` | Git repository address of an open-source component, default the component directory's origin |
| `--sign` | Sign the component with cosign before publishing |
| `--key <path>` | Path of the cosign private key, default `cosign.key` |
| `--public-key-ref <ref>` | The public key ref written into the signature, derived from `--key` (`.key` → `.pub`) by default |
| `--signed-by <identifier>` | Signer identifier, such as `release-bot@example.com` |
| `--no-pin-digest` | Don't pin the image tag to a digest (pinned by default; when skipped, the signature stays valid if the same tag is replaced on the registry) |

```bash
brickkit publish --path ./components/people/basic
brickkit publish --path ./components/people/basic --visibility private
brickkit publish --path ./components/people/basic --changelog "added a status field for people"
brickkit publish --path ./components/people/basic --sign --key cosign.key --signed-by release-bot@example.com
```

## `brickkit login`

Log in to a component market: type the user name and password in the terminal (the password is hidden), the market API
checks them, and on success the token is written to `.brickkit/credentials` (mode 0600). The market address comes from
the market install source in `brickkit.yaml`; with several, pick one with `--market`.

Token precedence: `.brickkit/credentials` over the install source's `authToken`. Before each use the token's expiry is
checked; an expired one asks you to log in again (there's no automatic refresh).

```text
brickkit login [flags]
```

| Flag | Meaning |
| --- | --- |
| `--market <address>` | The market address, default the market install source in `brickkit.yaml` |
| `--username <name>` | The user name; asked interactively if not given |
| `--password-stdin` | Read the password from standard input (for CI; not echoed, not kept in history) |

```bash
brickkit login
brickkit login --market https://market.example.com/api/v1
echo "$PASSWORD" | brickkit login --username ci-bot --password-stdin
```

## `brickkit logout`

Log out of a component market, which does two things: call the market's `POST /auth/logout` to revoke the token (the
server side), and delete `.brickkit/credentials` (the local side).

**The local copy is always deleted**, even when the market can't be reached — otherwise one network hiccup leaves someone
believing they logged out while the credential is still on disk. An unreachable market only earns a warning that the
token stays valid until it expires. When you aren't logged in, it does nothing, and that isn't a failure.

```text
brickkit logout [flags]
```

| Flag | Meaning |
| --- | --- |
| `--keep-remote` | Delete only the local credentials, without asking the market to revoke the token (for offline use) |

```bash
brickkit logout
brickkit logout --keep-remote
```

## `brickkit version`

The CLI version, the supported manifest version (`brickkit/v1`) and the deploy targets (`docker`, `podman`, `k8s`).

```text
brickkit version [flags]
```

| Flag | Meaning |
| --- | --- |
| `-v, --verbose` | Also print the Git commit and build time |

## `brickkit lang`

The language the CLI currently speaks, and where that comes from. **The CLI speaks English by default.** The language is
decided in this order: the `BRICKKIT_LANG` environment variable, then the value `brickkit lang set` saved in your user
config, then English. There's deliberately no `--lang` flag: the language has to be known before the command tree
(`--help` included) is built. Everything meant for people follows it, including the `message` of JSON log lines and the
comments in generated files; `error_code`, command names and flag names don't change.

```text
brickkit lang [flags]
brickkit lang <command> [arguments]
```

```text
Current language: en (source: default)
```

### `brickkit lang set`

Set the CLI's language (`en` or `zh`), saved in your user config and used by every command from then on; `BRICKKIT_LANG`
can still override it for one command. No flags of its own.

```text
brickkit lang set <en|zh> [flags]
```

```bash
brickkit lang set zh                 # speak Chinese from now on
BRICKKIT_LANG=en brickkit status     # English for this one command
```

---

## Everyday commands

```bash
# Project lifecycle
brickkit init my-shop
brickkit add erp/backend@1.0.0
brickkit build
brickkit up

# Several environments
brickkit up -f deploy.prod.yaml

# Local debugging
brickkit local on
brickkit up
brickkit local refresh      # after the team changes deploy.yaml
brickkit local off

# Upgrading
brickkit upgrade --dry-run
brickkit upgrade erp/backend@2.0.0

# Checks and diagnosis
brickkit lint --strict
brickkit deps erp/backend
brickkit graph > graph.mmd
brickkit up --dry-run

# Releasing
brickkit release --path ./components/erp/backend

# Contracts only (across projects)
brickkit fetch infra/notifier@1.0.0
```

## What changed since before the three-layer refactor

Projects used to have a single `brickkit.yaml`, plus a personal `override.yaml`. The three layers replaced them, and the
commands changed with them:

| Change | Command / flag | Notes |
| --- | --- | --- |
| Removed | `brickkit override` | Deleted: personal overrides became the whole-file `deploy.local.yaml`, managed by `brickkit local` |
| Removed | `--config` | Deleted: several environments became one deploy file per environment, chosen with `-f`; there is only one `brickkit.yaml` |
| Removed | `up --context` / `down --context` | Deleted: the cluster to deploy to is written in the deploy file's `k8s.context`; another cluster means another deploy file — what you see is what runs |
| Renamed | `--ignore-served-by` → `--ignore-shells` | The old flag was deleted: `servedBy` was replaced by shells (`kind: shell`) and `members` in the deploy file |
| New | `local` | Subcommands `on` / `off` / `status` / `refresh` |
| New | `upgrade` | Moves the default version, carrying config migration and conflict handling |
| New | `deps` | The dependency tree |
| New | `build` | Builds local images explicitly; `up` never builds |
| New | `release` | Releases through a Git tag; `publish`, releasing to a market, stays |
| New | `-f, --file` / `--no-local` | The commands that read a deploy file choose the file, or ignore local mode |
| Extended | `init` | Without an argument it completes the current directory; new `--name`, `--yes` |
| Extended | `add` | New `--local --init`; writes all three layers in one go |
