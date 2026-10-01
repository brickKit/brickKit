# BrickKit

> If you are an AI assistant, this is the one file to read first. After it you know how the platform is
> shaped, where each kind of answer lives, what not to read, and where the design deliberately stops.
>
> If the user is asking in Chinese, read [`AGENTS.zh.md`](AGENTS.zh.md) instead — an equivalent, independently
> written Chinese version, not a translation.
>
> Paths in this file are relative to the repository root. On the web, prefix them with
> https://raw.githubusercontent.com/brickKit/brickKit/main/ . To read all the documentation in a few fetches, start
> with [`llms/en/00-core.md`](llms/en/00-core.md) and follow its "Next" line.

## §1 What BrickKit is

BrickKit is a declarative component assembly platform. You declare which components a project uses and what
they depend on; the CLI derives everything else — start order, service addresses, environment variables,
deployment files — hands the result to Docker (or Podman) or Kubernetes, and exits.

A project is described by three layers of files, each owning one thing:

| Layer | Files | Owns |
| --- | --- | --- |
| Declaration | `brickkit.yaml` | Which components, at which exact versions, and which one is a shell. It is the lock file |
| Deployment | `deploy.yaml` (team) / `deploy.local.yaml` (personal, never committed) | How things run: target, ports, modes, shell members, Kubernetes settings |
| Configuration | `config/<component>.yaml`, `config/vars.yaml` | The environment variables each component receives |

There is no registry, no config server, no gateway and no long-running process: the CLI runs and exits, and
service discovery is Docker's or Kubernetes' own DNS.

## §2 Design principles (ten)

1. **Fail loudly**: config conflicts, missing required values, duplicate keys, failed pushes — each is reported the moment it happens, never left to surface at runtime.
2. **Minimal platform**: the platform does mechanical work and never guesses meaning — no rename detection, no type conversion, no deciding how a component degrades.
3. **What you see is what runs**: there are no hidden overrides; the file you open is the configuration in effect.
4. **Component autonomy**: what a component needs, how it degrades and what its logs look like are its own decisions; the platform asks only for a manifest, a health check and environment variables.
5. **One job per file**: each of the three layers owns one thing, and every fact is written in exactly one place.
6. **Atomic operations**: `add`, `remove`, `upgrade` and `release` either succeed completely or leave everything as it was.
7. **Explicit over implicit**: exact versions, explicit `$var:` references, explicit port exposure; nothing is auto-guessed.
8. **One model at every size**: a one-component project and a fifty-component project use the same three layers; there is no single-file mode.
9. **Build is separate from deploy**: building an image is your explicit step (`brickkit build`); `brickkit up` never builds.
10. **Read on demand, don't merge everything**: there is no command that folds the three layers into one view; to learn something, read the layer that owns it.

Each principle's argument — what it is, what it buys, what it costs, what it turned down — is in
[`docs/en/06-architecture/05-design-principles.md`](docs/en/06-architecture/05-design-principles.md).

## §3 The three layers

### 3.1 Where each answer lives

| To find out | Read |
| --- | --- |
| Which components the project uses, and their versions | `brickkit.yaml` |
| How a component is deployed (target, ports, mode, shell members) | `deploy.local.yaml` when local mode is on, otherwise `deploy.yaml` (a file named with `-f` wins over both) |
| The environment variables a component gets (connection details, secrets, switches) | `config/<component ID with / replaced by ->.yaml`; a versioned `config/<…>@<version>.yaml` wins for that version |
| Shared variables | `config/vars.yaml` (a deploy file's `vars:` overrides entries of the same name) |
| A component's dependencies, capabilities and `configSchema` | `.brickkit/manifests/<scope>/<name>/<version>/component.yaml`; for a component from a local source, the `component.yaml` in its source directory |
| How to use a dependency and what its config items mean | `.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md` |
| Which components the project has and where their docs are | `BRICKKIT.md` at the project root |

### 3.2 Rules that matter

- `brickkit.yaml` is the lock file: every component version in use is written there. An undeclared required dependency is an error (the error shows the `add` to run); an undeclared optional dependency simply does not exist.
- `deploy.local.yaml` **replaces** `deploy.yaml` as a whole; it does not override fields. With local mode on, the commands that run or check the deployment (`up`, `down`, `status`, `sync`, `lint`, `build`) read it and not `deploy.yaml`; `graph` and `deps` always read `deploy.yaml`, so their output is the same for everyone. It must match `brickkit.yaml` entry for entry, so after the team adds a component you run `brickkit local refresh`.
- `mode: debug` is written only in `deploy.local.yaml`: "I'm debugging this on my machine right now" is a personal fact and never goes into Git.
- `focus: <id>` is written only in `deploy.local.yaml` — `brickkit up` in a component's directory or `up --focus <id>` writes it, `up --all` removes it. While it is set, only that component (from its source) and what it needs start; `sync` ignores it, and it doesn't work with `target: k8s`.
- Project commands work from any subdirectory: they walk up to the nearest `brickkit.yaml` (like `git`, not stopping at `.git`) and say `📁 Project: …` when they did; paths they print are relative to where you are. `release`, `publish`, `init` and `skills` act on the current directory.
- `$var:NAME` takes its value from `config/vars.yaml` (or the deploy file's `vars:`); `${NAME}` from the process environment, then `.env`; `file://path` reads a file. The environment never overrides a value implicitly.
- `brickkit up` never builds an image: when an image that has to be built locally is missing, it stops and tells you to run `brickkit build`.
- `brickkit init <name>` creates a new directory; `brickkit init` without a name completes the current directory, adding only what is missing and never touching an existing byte, and warns loudly when `.gitignore` lacks a required entry.

### 3.3 Fractal structure

- **While you develop it**, a component's repository can itself be a complete BrickKit project, with its own three layers for local integration work.
- **When a project uses it**, the project reads only its `component.yaml` (the contract) and its `BRICKKIT.md` (the documentation).
- **The spec nests, the files don't**: a component's own three layers never travel into the project that uses it; only the contract and the docs do.
- `brickkit add` caches each component's `BRICKKIT.md` permanently under `.brickkit/manifests/`.
- **Inside a project, a focus run; for a standalone component, a workbench.** A focus run needs no files of its own; a workbench is the component repository's own `brickkit.yaml`, and the nearest `brickkit.yaml` always wins.
- **One `components/`**: component source lives only in the project's `components/`. A component nested inside another component's directory is refused by `up`, `lint` and `sync` and never moved for you; `add --repo` always clones into the project's `components/`. Git submodules are never fetched.

### 3.4 How an AI should read a project

1. This file: the rules and where things live.
2. The project's root `BRICKKIT.md`: which components exist and where each one's documentation is.
3. On demand, one component's `BRICKKIT.md` (under `.brickkit/manifests/`) to understand that component.

Don't load every component's documentation at once: read only the components the question is about.

## §4 Shells at a glance

- A shell is an ordinary component that compiles several member components into **one process** (1:N), to save memory and CPU.
- Members are listed under the shell's entry in the deploy file (`members`) — the only place membership is written. The shell's line in `brickkit.yaml` carries `kind: shell`, maintained by the CLI.
- The shell's `component.yaml` lists, in `shell.members`, the **exact versions** it compiles in. If the project hosts a different version, the CLI stops with three ways out.
- Members' configuration reaches the shell as `BRICKKIT_SERVED_MEMBERS_CONFIG`, a JSON value the CLI evaluates in advance; multi-line secrets (PEM), quotes and `$` encode fine, and a value that isn't valid UTF-8 (binary) fails loudly.
- Callers' `*_ENDPOINT` addresses are pointed at the shell by the platform; the shell author only routes each request to the right member inside the process.
- Members' migrations still run on their own, with each member's own image and config — so every member needs its own image (`image` or `build`).
- A shell is a component too: it has its own `configSchema` and its own `config/` file.

## §5 Commands (21, plus version, lang and completion)

| Command | What it does |
| --- | --- |
| `init` | With a name: create a directory with the three-layer skeleton. Without: complete the current directory |
| `skills` | Show or refresh the AI assistant skills installed in the project (`status` / `update`) |
| `graph` | Dependency topology as Mermaid |
| `lint` | Offline, read-only check of the three layers and `component.yaml` files |
| `new` | Component skeleton (`--shell` for a shell) |
| `add` | Fetch a component and its dependencies, write the three layers |
| `remove` | Remove a component; its config moves to `config/.archive/` |
| `fetch` | Download only a component's artifacts (contracts), without adding it to the project |
| `upgrade` | Move to another version and migrate config between the old and new `configSchema`; a conflict becomes a duplicate key that stops `up` |
| `up` | Generate deployment files → run migrations → start containers (never builds); in a component's directory or with `--focus`, only that component and what it needs |
| `down` | Stop containers (volumes are kept) |
| `status` | Running-state table |
| `sync` | Tidy the component source workspace: sources not used this run go to `.archived/`, the ones needed come back |
| `local` | Local mode: `on` / `off` / `status` / `refresh` |
| `restore` | Put `mode` and the source layout back to the last commit |
| `deps` | Dependency tree |
| `build` | Build the images that are built locally, explicitly (`--force`) |
| `release` | Check → Git tag → push; a failed push deletes the tag. `--local` releases every component in local sources |
| `publish` | Publish to a component market (optional infrastructure) |
| `login` | Log in to a component market |
| `logout` | Log out of a component market (revokes the token, deletes local credentials) |

`version` prints the version; `lang` shows or sets the language the CLI speaks (`lang set en|zh`); `completion` prints the TAB-completion script for a shell (`install.sh` installs it for bash, zsh and fish).

### Flags

- The only global flag is `--log-level` (level of the JSON log lines on stderr, default `warn`).
- `-f, --file <path>`: the commands that read a deploy file (`up`, `down`, `status`, `sync`, `lint`, `graph`) use it to pick one deploy file, ignoring `deploy.local.yaml` and the local-mode switch entirely.
- `--no-local`: `up`, `down`, `status`, `sync` and `lint` ignore `deploy.local.yaml` for this run, without changing the local-mode switch.
- `--dry-run`: `up` generates the deployment files without running them; `upgrade` works everything out without writing.
- `--focus <id>` / `--all`: `up` sets or removes the focus in `deploy.local.yaml`; neither goes with `-f` or `--no-local`.
- No argument, in a component's directory: `build` and `deps` mean that component.

Every command's full flag list is in [`docs/en/07-cli-reference/README.md`](docs/en/07-cli-reference/README.md).

### Removed commands and flags

| Removed | What to do instead |
| --- | --- |
| The `override` command and `override.yaml` | `deploy.local.yaml` + `brickkit local` |
| `resources`, `servedBy`, `deploy:` in `brickkit.yaml` | Connection details go into `config/`, shell members into the deploy file's `members`, deployment settings into `deploy.yaml` |
| `--config` | One deploy file per environment, chosen with `-f` |
| `up --context` | Write `k8s.context` in the deploy file; one deploy file per cluster |

## §6 What the platform won't do

| Won't do | Why |
| --- | --- |
| Service registry / discovery | Docker's and Kubernetes' DNS already are service discovery |
| A long-running daemon | The CLI runs and exits; state lives in files and in the engine |
| Health-check polling | Kubernetes probes and Compose healthchecks do it natively |
| API gateway, service mesh | Components call each other by DNS; an external gateway hooks in through `labels` passthrough |
| Config server / hot reload | Change `config/` or the deploy file, then `up` |
| Circuit breaking, rate limiting, retries, degradation | The component's own business logic |
| Version ranges (`^1.0.0`) | Only an exact version is a contract |
| Multi-environment overlays and inheritance | One complete deploy file per environment |
| Building images automatically | Building is your explicit step |
| A merged-view command | Read on demand instead |
| A single-file mode | One model at every size: always three layers |
| Implicit config overrides | What you see is what runs |
| Git authentication | Your machine's own Git setup is used; its errors are passed through verbatim |
| Validating config value types and ranges | `configSchema` is a spec sheet: key names are checked, values are not |

## §7 Config migration and conflicts

- An upgrade compares the old and new versions' `configSchema` and migrates your config mechanically, key by key.
- A key you changed whose default the author also changed becomes a duplicate key with a comment; `up` refuses to start until you resolve it (fail loudly).
- `remove` archives the config into `config/.archive/`; adding the component again restores it through the same migration.
- No rename detection, no type conversion, no semantic analysis.

## §8 Distribution without a market

- A component is a Git repository; a version is a Git tag (a component in a monorepo subdirectory uses `<scope>-<name>/<version>`).
- Fetching goes through a user-level bare-repository cache (`<user cache dir>/brickkit/repos`, shared by every project on the machine), fetched incrementally as needed.
- A manifest is read from, in order: the project's permanent cache `.brickkit/manifests/` → the local bare repository → an incremental fetch → a first clone.
- The CLI does no authentication of its own; `git`'s errors are passed through verbatim.
- `brickkit release`: check → tag → push, deleting the tag if the push fails; `--local` releases the components in local sources, stopping at the first failure.
- A component market (`publish` / `login` / `logout`, `sources[].type: market`) is optional infrastructure that works alongside Git sources.

## §9 Doc map

To find out → read. The page-by-page list, one line per page, is [`llms.txt`](llms.txt).

| To find out | Read |
| --- | --- |
| The platform in one read | [`llms/en/00-core.md`](llms/en/00-core.md): this file plus the core pages |
| Getting it running | [`docs/en/00-intro/02-quick-start.md`](docs/en/00-intro/02-quick-start.md) |
| Words and concepts | [`docs/en/00-intro/04-core-concepts.md`](docs/en/00-intro/04-core-concepts.md) |
| The three files, every field | [`docs/en/01-three-layers/README.md`](docs/en/01-three-layers/README.md), [`docs/en/01-three-layers/09-field-reference.md`](docs/en/01-three-layers/09-field-reference.md), [`docs/en/11-reference/README.md`](docs/en/11-reference/README.md) |
| Where a config value comes from | [`docs/en/01-three-layers/08-resolution-priority.md`](docs/en/01-three-layers/08-resolution-priority.md) |
| Running a project: init, add, local, up, upgrade … | [`docs/en/02-project-guide/README.md`](docs/en/02-project-guide/README.md) |
| Working on one component inside a project | [`docs/en/02-project-guide/04-focus-run.md`](docs/en/02-project-guide/04-focus-run.md) |
| Writing a component | [`docs/en/03-component-guide/README.md`](docs/en/03-component-guide/README.md) |
| Shells | [`docs/en/04-shell/README.md`](docs/en/04-shell/README.md) |
| Database migrations | [`docs/en/05-migration/README.md`](docs/en/05-migration/README.md) |
| Architecture, principles, the env contract, error codes | [`docs/en/06-architecture/README.md`](docs/en/06-architecture/README.md), [`docs/en/06-architecture/05-design-principles.md`](docs/en/06-architecture/05-design-principles.md), [`docs/en/06-architecture/03-env-injection-contract.md`](docs/en/06-architecture/03-env-injection-contract.md), [`docs/en/06-architecture/09-error-codes.md`](docs/en/06-architecture/09-error-codes.md) |
| Every command and flag | [`docs/en/07-cli-reference/README.md`](docs/en/07-cli-reference/README.md) |
| How an AI works with BrickKit | [`docs/en/08-ai-guide/README.md`](docs/en/08-ai-guide/README.md) |
| Recommended practices | [`docs/en/09-patterns/README.md`](docs/en/09-patterns/README.md) |
| Something failed | [`docs/en/10-troubleshooting/README.md`](docs/en/10-troubleshooting/README.md) |
| Building, testing, conventions for contributors | [`CONTRIBUTING.md`](CONTRIBUTING.md) |
| Where a feature lives in the code | §10 below |

## §10 Code map

One Go module, `github.com/brickkit/brickkit`. The CLI starts in `cmd/brickkit/`; every command is one file,
`internal/cli/<command>.go`.

### Packages

| Package | Owns |
| --- | --- |
| `internal/cli/` | The command tree: one file per command, flags, output; errors shown relative to where you stand (`shown.go`); TAB completion candidates (`complete.go`) |
| `internal/project/` | Loading a project (brickkit.yaml + deploy file + config/) into one `Project`; finding the root upward (`findroot.go`); consistency checks; the component table in the project's `AGENTS.md` (`agents.go`) |
| `internal/project/projecttest/` | Test helper: a three-layer project built from a few lines of YAML, then loaded |
| `internal/projfile/` | `brickkit.yaml`: sources, components, versions, `kind: shell` |
| `internal/deployfile/` | Deploy files (`deploy.yaml`, `deploy.local.yaml`, `-f`): fields, validation, `focus`, the local-change diff |
| `internal/configdir/` | `config/`: per-component env files, `vars.yaml`, value resolution, skeletons, migration on upgrade |
| `internal/manifest/` | `component.yaml`: parsing, validation, the scaffolds `brickkit new` writes |
| `internal/resolver/` | Dependency resolution into a graph; start order |
| `internal/cascade/` | What runs this time: `mode`, "follows the layer above", focus reachability, shell hosting |
| `internal/inject/` | Each component's environment variables: dependency addresses, config, resources |
| `internal/shell/` | Shell grouping and the members' JSON config |
| `internal/compose/` | Rendering `compose.yaml` for docker / podman |
| `internal/k8s/` | Rendering Kubernetes manifests |
| `internal/deploy/` | Naming rules and file headers shared by both targets |
| `internal/docspec/` | The component and project documentation spec as data: files, sections, heading names per language, translation names |
| `internal/doccheck/` | lint's documentation checks: files, sections, code-map paths, links, manifest facts, placeholders, translations |
| `internal/agentsmd/` | The CLI-maintained block at the end of `AGENTS.md` (platform rules, component table, recorded language), and the `AGENTS.md` / `CLAUDE.md` skeletons |
| `internal/engine/` | Docker, Podman and kubectl: detection and invocation |
| `internal/procsup/` | Supervising `mode: local` processes in the foreground |
| `internal/runcmd/` | Working out how to start a component from its source |
| `internal/sessionlock/` | One foreground local session per project |
| `internal/install/` | What `add` / `remove` / `upgrade` change in the three layers |
| `internal/source/` | Install sources (local / git / market), the manifest and artifact caches, the bare-repository cache, offline versions |
| `internal/source/gittest/` | Test helper: real Git "remotes" — local bare repositories tagged per version, reached over `file://` |
| `internal/gitrepo/` | Read-only git queries (status, submodules) |
| `internal/workspace/` | Component source under `components/`: archive, activate, deletion risk |
| `internal/release/` | `brickkit release`: checks, tag, push, rollback |
| `internal/market/` | Market client: login and publish |
| `internal/security/` | Component signatures: signing and verification |
| `internal/skills/` | AI-assistant skills installed into user projects (`assets/`) |
| `internal/clierr/` | The error type, error codes and how errors render |
| `internal/i18n/` | Message catalogs (`locales/en.yaml`, `locales/zh.yaml`) and language resolution |
| `internal/msgid/` | Message keys (`messages_gen.go` is generated) |
| `internal/msgid/msgidgen/` | Renders `messages_gen.go` from the English catalog (build time only: `cmd/gen-msgid`, the checks) |
| `internal/logging/` | JSON log lines on stderr |
| `internal/suggest/` | "Did you mean" suggestions |
| `internal/envref/` | `${VAR}` references |
| `internal/yamlfile/` | The read / parse / decode pipeline the three layers share |
| `internal/yamlcheck/` | Misspelled or unknown YAML fields |
| `internal/yamlcomment/` | Comment blocks in generated YAML, `.env`, `.gitignore` |
| `internal/schemagen/` | JSON Schemas generated from the Go structs into `schemas/` |
| `internal/userconfig/` | Machine-level preferences (the CLI's language) |
| `internal/version/` | Version and capability constants |
| `internal/llmsgen/` | The documentation bundles in `llms/` and the bundle list in `llms*.txt` |
| `internal/mdtext/` | Markdown scanning shared by the doc bundles and lint's doc checks: fences, links, sections, table cells |
| `cmd/brickkit/` | The CLI's `main` |
| `cmd/gen-msgid/` | Generates `internal/msgid/messages_gen.go` |
| `cmd/gen-schemas/` | Generates `schemas/*.json` |
| `cmd/gen-llms/` | Generates `llms/` |

Elsewhere: `market-server/` (the optional component market, its own Go module), `tools/i18n/` (one-off i18n
migration scripts), `scripts/` (lint checks, install checks, release), `install.sh`, `.githooks/` (the commit hook).

### Features → code

| Feature | Start here | Then |
| --- | --- | --- |
| `up` | `internal/cli/up.go` (`up_local.go`, `up_k8s.go`, `up_upgrade.go`) | `cascade`, `inject`, `compose`, `k8s`, `engine`, `procsup` |
| `down`, `status` | `internal/cli/down.go`, `internal/cli/status.go`, `internal/cli/lifecycle.go` | `engine`, `sessionlock` |
| `add`, `remove`, `upgrade` | `internal/cli/add.go`, `internal/cli/remove.go`, `internal/cli/upgrade.go`, `internal/cli/install_apply.go` | `install`, `configdir`, `source` |
| `fetch` | `internal/cli/fetch.go`, `internal/cli/artifacts.go` | `source` |
| `build` | `internal/cli/build.go` | `source`, `engine`, `gitrepo` |
| `lint` | `internal/cli/lint.go`, `internal/cli/lint_config.go` | `project`, `yamlcheck` |
| `graph`, `deps` | `internal/cli/graph.go`, `internal/cli/deps.go`, `internal/cli/topology.go` | `resolver`, `cascade` |
| `sync`, `restore` | `internal/cli/sync.go`, `internal/cli/restore.go`, `internal/cli/restore_check.go` | `workspace` |
| `local` | `internal/cli/local.go` | `deployfile` |
| `init`, `new` | `internal/cli/init.go`, `internal/cli/new.go`, `internal/cli/hooks.go` | `project`, `manifest`, `skills` |
| `release`, `publish`, `login`, `logout` | `internal/cli/release.go`, `internal/cli/publish*.go`, `internal/cli/login.go`, `internal/cli/logout.go` | `release`, `market`, `security` |
| `skills`, `lang`, `version` | `internal/cli/skills.go`, `internal/cli/lang.go`, `internal/cli/version.go` | `skills`, `i18n`, `userconfig` |
| Finding the project upward | `internal/project/findroot.go`, `internal/cli/root.go` | |
| Focus run | `internal/cli/focus.go` | `internal/cascade/cascade.go`, `internal/deployfile/focus.go` |
| One `components/` (nested copies) | `internal/cli/nested.go` | `internal/project/nested.go` |
| TAB completion | `internal/cli/complete.go` | `internal/source/cached.go`, `install.sh` |
| "Did you mean" | `internal/cli/didyoumean.go` | `suggest` |
| How errors look | `internal/clierr/clierr.go`, `internal/cli/shown.go` | |
| Adding a message | `internal/i18n/locales/en.yaml`, `internal/i18n/locales/zh.yaml`, then `make generate-msgid` | `msgid` |
| Documentation bundles | `cmd/gen-llms/`, `internal/llmsgen/` | `.githooks/pre-commit`, `make generate-llms` |

### Tests and checks

- Unit tests sit next to the code (`*_test.go`); CLI tests run commands in-process against `internal/cli/testdata/`.
- `tests/components/`: real components (Go, Python, nginx) used as fixtures; `tests/checklist/`: the regression
  list `make test-all` runs; `tests/docfields/`: guards that the docs match the code.
- `make lint` (every static check, `scripts/check-*.py`) and `make test-all`; `make hooks` once per clone.
