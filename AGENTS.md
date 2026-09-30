# BrickKit

> If you are an AI assistant, this is the one file to read first. After it you know how the platform is
> shaped, where each kind of answer lives, what not to read, and where the design deliberately stops.
>
> If the user is asking in Chinese, read [`AGENTS.zh.md`](AGENTS.zh.md) instead — an equivalent, independently
> written Chinese version, not a translation.

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
- `$var:NAME` takes its value from `config/vars.yaml` (or the deploy file's `vars:`); `${NAME}` from the process environment, then `.env`; `file://path` reads a file. The environment never overrides a value implicitly.
- `brickkit up` never builds an image: when an image that has to be built locally is missing, it stops and tells you to run `brickkit build`.
- `brickkit init <name>` creates a new directory; `brickkit init` without a name completes the current directory, adding only what is missing and never touching an existing byte, and warns loudly when `.gitignore` lacks a required entry.

### 3.3 Fractal structure

- **While you develop it**, a component's repository can itself be a complete BrickKit project, with its own three layers for local integration work.
- **When a project uses it**, the project reads only its `component.yaml` (the contract) and its `BRICKKIT.md` (the documentation).
- **The spec nests, the files don't**: a component's own three layers never travel into the project that uses it; only the contract and the docs do.
- `brickkit add` caches each component's `BRICKKIT.md` permanently under `.brickkit/manifests/`.

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

## §5 Commands (21, plus version and lang)

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
| `up` | Generate deployment files → run migrations → start containers (never builds) |
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

`version` prints the version; `lang` shows or sets the language the CLI speaks (`lang set en|zh`).

### Flags

- The only global flag is `--log-level` (level of the JSON log lines on stderr, default `warn`).
- `-f, --file <path>`: the commands that read a deploy file (`up`, `down`, `status`, `sync`, `lint`, `graph`) use it to pick one deploy file, ignoring `deploy.local.yaml` and the local-mode switch entirely.
- `--no-local`: `up`, `down`, `status`, `sync` and `lint` ignore `deploy.local.yaml` for this run, without changing the local-mode switch.
- `--dry-run`: `up` generates the deployment files without running them; `upgrade` works everything out without writing.

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

## §9 Documentation index

- [docs/en/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/README.md): the English docs index and reading order
- [docs/en/00-intro/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/00-intro/README.md): overview and getting started
- [docs/en/00-intro/02-quick-start.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/00-intro/02-quick-start.md): from an empty directory to running containers in five minutes
- [docs/en/00-intro/04-core-concepts.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/00-intro/04-core-concepts.md): glossary of core concepts
- [docs/en/00-intro/06-fractal-architecture.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/00-intro/06-fractal-architecture.md): the fractal structure — developing a component vs. using one
- [docs/en/01-three-layers/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/01-three-layers/README.md): the three layers in depth
- [docs/en/01-three-layers/08-resolution-priority.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/01-three-layers/08-resolution-priority.md): where a config value comes from, in order
- [docs/en/02-project-guide/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/02-project-guide/README.md): guide for people running a project
- [docs/en/03-component-guide/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/03-component-guide/README.md): guide for component authors
- [docs/en/04-shell/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/04-shell/README.md): shells
- [docs/en/05-migration/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/05-migration/README.md): database migrations
- [docs/en/06-architecture/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/06-architecture/README.md): architecture and the mechanisms underneath
- [docs/en/06-architecture/03-env-injection-contract.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/06-architecture/03-env-injection-contract.md): every environment variable the platform injects
- [docs/en/06-architecture/09-error-codes.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/06-architecture/09-error-codes.md): error codes — and which ones are worth retrying
- [docs/en/07-cli-reference/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/07-cli-reference/README.md): the complete CLI reference
- [docs/en/08-ai-guide/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/08-ai-guide/README.md): guide written for AI assistants
- [docs/en/09-patterns/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/09-patterns/README.md): recommended practices
- [docs/en/10-troubleshooting/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/10-troubleshooting/README.md): troubleshooting — symptom → cause → fix
- [docs/en/11-reference/README.md](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/11-reference/README.md): reference — every field of the three files

The page-by-page index is [`llms.txt`](llms.txt).
