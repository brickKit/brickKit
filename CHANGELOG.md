# Changelog

All notable changes to BrickKit are documented in this file, newest first.
The format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

Before 1.0.0, minor version bumps may include breaking changes — semver's
usual guarantee about backward compatibility doesn't fully apply yet. Every
entry below is reconstructed from real commit history (`git log`, and the
tags themselves); it groups related commits into the feature or fix they
add up to, rather than listing every commit individually.

## [Unreleased]

The biggest change since 0.1.0: the single `brickkit.yaml` (plus the
personal `override.yaml`) is replaced by three layers, each owning one
thing — `brickkit.yaml` declares which components at which exact versions,
a deploy file (`deploy.yaml`, or the personal `deploy.local.yaml`) says how
they run, and `config/` holds the environment variables each component
gets. A 0.9.0 `brickkit.yaml` is rejected (its `deploy`, `resources`,
`servedBy`, `mode`, `config` … keys are unknown fields now), and there is
no automatic migration. The shortest route: move the old `brickkit.yaml`
aside, run `brickkit init` in the project directory, `brickkit add` each
component again (which writes all three layers and a config skeleton per
component), then carry your values over using the "Fields that moved out
of the old single file" table in `docs/en/01-three-layers/02-brickkit-yaml.md`.
`.brickkit/` can simply be deleted; it is rebuilt on demand.

### Removed

- `override.yaml` and the `brickkit override` command. Instead: the
  personal `deploy.local.yaml`, managed with `brickkit local on` / `off` /
  `status` / `refresh`. It *replaces* `deploy.yaml` as a whole while local
  mode is on — it is not merged field by field, so what the file says is
  what runs
- The global `--config` / `-c` flag. Instead: one complete deploy file per
  environment (`deploy.prod.yaml`, …), chosen with `-f, --file`; there is
  only ever one `brickkit.yaml`. There are no overlays or inheritance
- `up --context` and `down --context`. Instead: write `k8s.context` in the
  deploy file — another cluster is another deploy file
- `servedBy`. Instead: shells — the shell's line in `brickkit.yaml` carries
  `kind: shell` (maintained by the CLI), its members are nested under its
  entry in the deploy file as `members`, and the shell's own
  `component.yaml` lists the exact member versions it compiles in, under
  `shell.members` (see "Changed — breaking" below)
- `--ignore-served-by` on `up` and `graph`. Instead: `--ignore-shells`,
  which ignores every shell's `members` in memory for one run
- Resource bindings: `resources[]` (with `bindings`, `envPrefix` and
  `existingSecret`) in `brickkit.yaml`, `dependencies.resources` in
  `component.yaml`, the `DATABASE_*` / `REDIS_*` / `MQ_*` / `STORAGE_*` /
  `SEARCH_*` / `SMTP_*` variables injected from them, and
  `networkPolicy.egress.allowTo[].resource`. Instead: a component declares
  its connection details as ordinary `configSchema` items, the project
  writes their values in `config/` (values shared by several components
  once, in `config/vars.yaml`, referenced with `$var:NAME`), and an egress
  rule names its target with `namespace` / `cidr` and `ports`
- Git install sources of the old shape (`url` plus `ref`, one cloned
  repository holding components on a branch). Instead: a `git` source has a
  `baseUrl`, each component lives in its own repository and each version is
  a Git tag (see "Changed — breaking" below)
- `.brickkit/skills.lock`. Each skill file now ends with its own record
  line, so a teammate's fresh clone can tell CLI-written files from edited
  ones; an old lock is read once by `brickkit skills update` to recognise
  the files an earlier CLI wrote, then deleted
- `AGENTS.md` as an installed skill asset. The project's `AGENTS.md` is
  now the author's own file, with one CLI-maintained block at its end (see
  "Added" below)
- The 15-part hands-on tutorial series (`docs/{en,zh}/03-guide/`) and the
  rest of the pre-refactor documentation, retired with the model they
  described; the current tree is `docs/{en,zh}/00-intro` … `11-reference`

### Changed — breaking

- `brickkit.yaml` declares only `project`, `sources`, `components` (`id`,
  exact `version`, and `kind`, `requiredBy`, `source` where they apply) and
  `installer`. Deployment settings (`deploy.target`, `deploy.namespace`
  and the other `deploy.*` keys) become the deploy file's `target` and
  `k8s:` block; a component's `mode`, `localPort`, `expose`, `exposePort`,
  `hostname`, `tlsSecret`, `replicas`, `resources`, `serviceAccountName`
  and `labels` move to its entry in the deploy file; its `config` moves to
  `config/<scope>-<name>.yaml`. Every component version declared in
  `brickkit.yaml` must have exactly one deploy entry — `lint` and `up`
  check it
- `brickkit.yaml` is the lock file. A required dependency that isn't
  declared there is now an error naming the `brickkit add` to run; it used
  to be pulled from the install sources and run anyway, without a deploy
  entry and without its config being checked. An undeclared optional
  dependency counts as absent (a warning; no address injected). `add`
  writes dependencies for you, and adding another version of a component
  that already has a default version is refused in favour of
  `brickkit upgrade`
- Install sources: `sources[].id` is now `sources[].name`. A `git` source
  takes `baseUrl`; the repository of `erp/backend` is
  `<baseUrl>erp-backend`, and a version is the tag `<version>`, or
  `<scope>-<name>/<version>` for a component in a monorepo subdirectory. A
  component whose repository name can't be derived names its own `source:`
  (`type: git` + `repo`, or `type: local` + `path`) on its entry
- `configSchema` keys are the environment variable names, injected as-is:
  there is no camelCase → `UPPER_SNAKE_CASE` conversion any more, and a key
  that isn't a valid environment variable name is rejected. Component
  authors: rename `defaultPageSize` to `DEFAULT_PAGE_SIZE` to keep the
  variable your code reads. The reserved set is now `COMPONENT_ID`,
  `COMPONENT_VERSION`, `BRICKKIT_SERVED_MEMBERS`,
  `BRICKKIT_SERVED_MEMBERS_CONFIG`, `PORT` and the `*_ENDPOINT` suffix
- `mode` lives in the deploy file. `mode: debug` can only be written in
  `deploy.local.yaml` (it was `override.yaml`-only in 0.8.0/0.9.0) and is
  rejected in `deploy.yaml`; `mode: local` may go in the team file.
  `target: podman` can be written in any deploy file (it was
  `override.yaml`-only); the override's "target may only be downgraded"
  rule is gone with it — a deploy file simply says its target
- Shells: the shell's `component.yaml` must declare `shell.members`, the
  exact `<id>@<version>` of every member compiled into its image, and `up` /
  `graph` stop when the project hosts a different version, offering three
  ways out (upgrade the shell, move the member out, keep both versions).
  Members' migrations run with each member's own image and config, so every
  member needs an image of its own (`image` or `build`).
  `BRICKKIT_SERVED_MEMBERS_CONFIG` items now carry `config` — the member's
  variables with their values already evaluated (`$var:`, `${VAR}` and
  `file://` resolved), dependency addresses included — instead of
  `configEnvVars`, which named prefixed variables in the shell's
  environment; shell code must read values from the JSON. The JSON is
  placed as a secret (a 0600 env file on Docker, a Secret on Kubernetes)
- Components with no `deployment.image`, and every version served by a
  local install source, need an image built on this machine: run
  `brickkit build`. `up` never builds, never substitutes a registry image
  for a local source, and lists everything missing in one `IMAGE_MISSING`
  error with the build commands
- Docker: a `${VAR}` that is defined neither in the process environment nor
  in `.env` now stops generation, as it already did on Kubernetes — it used
  to be handed to `docker compose`, which replaced it with an empty string
  and warned only in its own output. A malformed `${…}` fails when the
  project is loaded. `${VAR:-default}` is a reference with a default on
  every target (Docker used to treat it as literal text)
- `brickkit lint --strict` now also fails on documentation warnings (the
  `DOC_*` checks below) and on unresolvable references (a `${VAR}` that is
  undefined, a `file://` whose file is missing). A CI gate running
  `lint --strict` over local components that have no `BRICKKIT.md` /
  `AGENTS.md` / `CLAUDE.md` / `README.md` will start failing; drop
  `--strict` or add the documents (`brickkit new` writes skeletons)
- `brickkit restore`, `restore --check` and the pre-commit hook compare the
  `mode` values in `deploy.yaml` (the team file), not `brickkit.yaml`
- `brickkit init` writes the project's own `AGENTS.md` (ending with the
  CLI-maintained block) and `CLAUDE.md` (`@AGENTS.md`). An `AGENTS.md` an
  earlier CLI installed and nobody edited since (recognised from the old
  `.brickkit/skills.lock` on this machine) is replaced with the new skeleton
  by `init` or `brickkit skills update`; any other `AGENTS.md` is left alone
  and gets an `AGENTS_BLOCK_MISSING` warning until `skills update` appends
  the block
- An extra port whose name has a `-` gives a valid variable name: port
  `admin-api` of `erp/api` is `ERP_API_ADMIN_API_ENDPOINT` (it was
  `ERP_API_ADMIN-API_ENDPOINT`, which no shell can read). Callers reading the
  old name must switch
- A `mode: local` process still inherits the terminal's environment, but no
  longer the names the platform owns: `COMPONENT_ID`, `COMPONENT_VERSION`,
  `PORT`, `BRICKKIT_SERVED_MEMBERS(_CONFIG)`, every `*_ENDPOINT` and the
  component's own `configSchema` keys come only from the platform. An
  `*_ENDPOINT` exported in your shell used to reach the process even when that
  optional dependency wasn't running, and a stale export could fill in a
  config item that `config/` left empty

### Added

- **The three layers.**
  - `config/<scope>-<name>.yaml`: one flat file per component, every key
    injected as an environment variable as-is; a non-default version uses
    `config/<scope>-<name>@<version>.yaml`. `add` writes a skeleton from the
    component's `configSchema` — required items empty, optional ones
    commented out with their defaults (an item you never uncomment follows
    the default of the component's current version). Values: `$var:NAME`
    (from `config/vars.yaml`, or the deploy file's `vars:`, which wins),
    `${NAME}` (process environment, then `.env`) and `file://path`; the
    environment never overrides a value implicitly. A required item with no
    value stops `up` and names it; unknown keys, duplicate keys and orphaned
    files are reported
  - Deploy files: `target` (`docker` / `podman` / `k8s`), `k8s:`
    (`context`, `namespace`, `createNamespace`, `podSecurity`,
    `imagePullSecrets`, `ingressClass`, `ingressAnnotations`,
    `networkPolicy`, `serviceAccount`), `vars:` and one entry per
    component version (a bare ID means the default version, `id@version`
    any other). Fields that only mean something on Kubernetes warn under
    another target, so one entry moves between targets unchanged
  - `-f, --file <file>` and `--no-local` on `up`, `down`, `status`, `sync`
    and `lint` (`graph` takes `-f`): pick one deploy file, or ignore
    `deploy.local.yaml` for one run (`lint` checks both files whenever
    `deploy.local.yaml` exists; `--no-local` only makes `deploy.yaml` the
    one its config checks use). `graph` and `deps` never read
    `deploy.local.yaml`, so their output is the same for everyone
  - `brickkit local`: `on` copies `deploy.yaml` to `deploy.local.yaml` the
    first time; `refresh` regenerates it after the team changes
    `deploy.yaml`, keeps the old one as `deploy.local.yaml.bak` and lists
    every local change in it, judged against the copy the file was made from
    (`.brickkit/deploy.local.base.yaml`). While local mode is on,
    `deploy.local.yaml` must match `brickkit.yaml` entry for entry, and
    `up` says to `refresh` when the team added a component
  - `schemas/deploy.schema.json` for deploy files; `brickkit.schema.json`
    follows the new `brickkit.yaml`
- **Git distribution without a market.** Git sources read each version
  from its tag through a user-level bare-repository cache shared by every
  project on the machine (`<user cache dir>/brickkit/repos`), fetched
  incrementally; manifests are cached permanently per version under
  `.brickkit/manifests/<scope>/<name>/<version>/`, together with the
  component's `BRICKKIT.md`. Git runs non-interactively with your own
  machine's setup, and its errors are passed through verbatim, with SSH /
  HTTPS / CI hints — or network hints, plus the versions already cached,
  when the remote can't be reached. A refused fetch is `AUTH_FAILED`; only
  a failed connection is `NETWORK_UNREACHABLE`
- `brickkit release [--path <dir>] [--local]`: check (manifest valid,
  directory clean, branch pushed, tag not taken locally or on the remote) →
  tag → push; a failed push deletes the tag. `--local` releases every
  component in the project's local install sources, skipping released ones
  and stopping at the first failure
- Several versions of one component in a project: exactly one line in
  `brickkit.yaml` is the default version, the others carry `requiredBy:
  [<who needs it>]`, maintained by `add`, `upgrade` and `remove`
- `brickkit upgrade [<id>[@<version>]] [--dry-run] [-y]`: moves a
  component's default version (the latest tag, a named version, or — with
  no argument — every default that has a newer one) and migrates its config
  key by key between the old and new `configSchema`: values you wrote stay
  (with their comments), keys the new schema dropped go, untouched items
  follow the new defaults. A key whose default the author changed while you
  changed it too is chosen interactively, or (`-y`, or no terminal) written
  as a duplicate key with a comment — and `up` refuses to start until you
  resolve it. Upgrading a shell moves its member entries along; versions
  nobody needs any more are removed. `--dry-run` writes nothing
- `brickkit add` writes `brickkit.yaml`, `deploy.yaml` (and
  `deploy.local.yaml` when it exists) and the config skeletons in one
  all-or-nothing step, offers to reference a shared variable of the same
  name with `$var:` (`--yes` always does), nests a shell's members under it,
  and restores a config archived by `remove` through the same migration as
  `upgrade`. `add --local --init` first gives every local component without
  a `brickkit.yaml` its own workbench. `remove` moves the config to
  `config/.archive/`, removes the deploy entries, moves a removed shell's
  members back to the top level and refuses while a remaining component
  requires the one being removed
- `brickkit build [<id>] [--force]`: builds, in dependency order, the images
  that are built locally, tagged with `metadata.version` and labelled — from
  the local repository when it holds that version, otherwise from the tag
  exported out of the bare-repository cache; a shell image records the
  member versions compiled in, and `up` checks them (`IMAGE_STALE`,
  `IMAGE_UNVERIFIED`). `component.yaml` gains `deployment.build`
  (`context`, `dockerfile`); `deployment.image` becomes optional — write at
  least one of the two
- `brickkit deps [<id>]`: the dependency tree
- `up` checks, before starting anything, that the local repository of every
  `mode: local` / `mode: debug` component (and of a bare-process shell's
  members) holds exactly the default version this run expects, with
  `brickkit upgrade` or a `git checkout` of the right tag as the way out
- Shells: a shell can run as a bare process (`mode: local` / `debug`) with
  its members; a member set to `mode: debug` / `local` leaves the shell and
  runs as a process of its own; a shell hosts one version of a member while
  other versions run standalone; members migrate before the shell starts
  (Kubernetes Jobs, one-shot runs on Docker); `skipWaitFor` on a deploy
  entry drops the start-up wait for named dependencies (to break a start
  cycle that merging into a shell can create) while keeping the connection;
  `brickkit new --shell` writes a shell skeleton under `shell/`
- Focus runs: `brickkit up` inside a component's directory, or
  `up --focus <id>`, writes `focus: <id>` into `deploy.local.yaml` (turning
  local mode on if needed) and starts only that component, from its source,
  and what it needs; `up --all` removes the focus. `focus` is personal-only,
  `sync` ignores it, it doesn't work with `target: k8s`, and removing the
  focused component removes the focus
- Project commands work from any subdirectory: they walk up to the nearest
  `brickkit.yaml` (like `git`, not stopping at `.git`), say
  `📁 Project: …`, and print paths relative to where you are. In a
  component's directory, `build` and `deps` without an argument mean that
  component
- One `components/`: a component copy nested inside another component's
  directory is reported by `up`, `lint` and `sync` (with whether its bytes
  exist anywhere else) and never moved for you; `add --repo` always clones
  into the project's `components/`. `build` and `add --repo` warn
  (`SUBMODULES_SKIPPED`) when a component's Git submodules were not fetched
- "Did you mean" suggestions for a mistyped component ID in `remove`,
  `upgrade`, `deps`, `build`, `add` and `--focus`
- `brickkit completion bash|zsh|fish|powershell [--no-descriptions]`: TAB
  completes commands, flags, component IDs, versions, deploy files and
  languages, offline. `install.sh` installs it for bash, zsh and fish where
  each shell loads it, without touching rc files
- `brickkit init` without a name completes the current directory
  (`--name`, `--yes`): it adds only what is missing, never changes a byte
  of an existing file (except the CLI-maintained block of `AGENTS.md`),
  and warns loudly about missing `.gitignore` entries (`.brickkit/`,
  `deploy.local.yaml`, `deploy.local.yaml.bak`, `.secrets/`, `.env`,
  `config/.archive/`; `components/` is ignored by default but may be left
  out, to commit component source with the project). New projects get `deploy.yaml`,
  `config/vars.yaml` and two local sources, `components/` and `shell/`
- `brickkit lint` checks the three layers together: both deploy files
  against `brickkit.yaml` (whether local mode is on or not), one entry per
  version, every `$var:` defined, required config values present, written
  keys known to the schema, shell members' values encodable as JSON, and
  `kind: shell` against the manifest's `shell` block
- **Component documentation spec.** A component carries one document per
  reader: `BRICKKIT.md` for the projects that use it (six sections, no
  relative links; translations `BRICKKIT.<lang>.md`), `AGENTS.md` +
  `CLAUDE.md` for the AI developing it (a code map and four more sections),
  and `README.md` for people on GitHub. `brickkit new` writes all four;
  `add` caches `BRICKKIT.md` and its translations per version; `lint`
  checks the documents as warnings (`DOC_FILE_MISSING`,
  `DOC_SECTION_MISSING`, `DOC_PATH_MISSING`, `DOC_LINK_BROKEN`,
  `DOC_LINK_NOT_PORTABLE`, `DOC_OUT_OF_STEP`, `DOC_PLACEHOLDER`,
  `DOC_TRANSLATION_DRIFT`, `AGENTS_BLOCK_MISSING`, `CLAUDE_IMPORT_MISSING`,
  `PROJECT_MAP_OBSOLETE`). `metadata.repository` is a new optional field
- The component table at the end of the project's `AGENTS.md`: a
  CLI-maintained block (platform rules, one row per component with what it
  does from `metadata.description` and a Home link from
  `metadata.repository`, and the project's recorded language), kept current
  by `add`, `remove` and `upgrade`; `skills status` reports it when it is
  out of date and `skills update` rewrites it in place. (Builds from `main`
  briefly kept this table in a project-level `BRICKKIT.md`; such a file is
  reported as `PROJECT_MAP_OBSOLETE` and never deleted for you)
- AI-assistant skills: a fifth skill, `brickkit-plan-change` (judging a new
  requirement and planning a change across components);
  `brickkit-component` covers the component documents; the skills'
  language is recorded in the `AGENTS.md` block (`lang=`); `skills update`
  creates a missing `AGENTS.md` / `CLAUDE.md` or appends what each lacks; a
  language without its own skill assets falls back to the source language
- The component market validates manifests with the CLI's own rules, stores
  and serves `BRICKKIT.md` and its translations (`publish` sends them; a
  market that predates them publishes without and `publish` warns), caps
  JSON request bodies at 8 MiB, sends `nosniff` on documents, scopes the
  audit log to what the caller is accountable for, and has an API reference.
  An interrupted `publish` resumes only when `component.yaml`, `BRICKKIT.md`
  and every translation are byte-for-byte what the draft registered
- Documentation rewritten for the three-layer model in English and Chinese
  (`docs/{en,zh}/00-intro` … `11-reference`), with focus runs, shell
  completion and a page on judging a requirement; `AGENTS.md` gains a doc
  map and a code map; `llms/{en,zh}/` bundles (starting at
  `llms/en/00-core.md`) let a web reader fetch everything in a few requests

### Changed

- Every CLI error ends with what to do next, and errors show paths relative
  to where you stand
- `up` owns only this run's output: switching target removes the other
  target's secret-bearing files, a component leaving `mode: debug` loses its
  env file, and when nothing runs in a container any more the previous run's
  containers are stopped (volumes kept)
- `secret: true` values and `file://` contents reach Docker containers
  through 0600 env files under `.brickkit/generated/env/` rather than the
  Compose file; `$` in values is escaped for Compose
- On Kubernetes, `up` warns that `skipWaitFor` has no effect (Pods don't wait
  for each other at start) instead of ignoring it silently, and the start
  order no longer says which dependencies a component "does not wait for"
- `lint`'s Code map check also covers top-level files: every entry in the
  first column of the path table is a path, even without a `/`
- The `BRICKKIT.md` skeleton `brickkit new` writes has a two-column
  Configuration table (`Variable` / `Meaning`): whether a key is required is
  already in `component.yaml`
- A new project's `.gitignore` still ignores `components/`, but completing a
  project with `init` no longer warns when an existing `.gitignore` leaves it
  out: committing component source with the project is a team's choice
- The CLI's messages live in YAML catalogs (`internal/i18n/locales/en.yaml`
  and `zh.yaml`) with generated message IDs; adding a language needs no Go
  code, and `lang set` and `skills update --lang` list the registered
  languages

### Fixed

- A numeric `configSchema` default is injected as the text written:
  `default: 1.10` is `1.10` (it was `1.1`) and `default: 007` is `007`, the
  same as a value written in `config/`
- A failed migration under Docker is named, as it already was on Kubernetes
- Engine output inside an error block keeps its line breaks

## [0.9.0] - 2026-09-25

### Added

- `target: podman` (still set in `override.yaml`) runs for real: `up`,
  `down` and `status` drive `podman compose` instead of stopping with "not
  implemented". It assumes `podman compose` dispatches to Docker's Compose
  V2 plugin, not to the separate `podman-compose` project. Hints follow the
  engine in use (the logs command, `volume rm`, `login`, the install hint
  for a missing binary), and a `down` failure matching Podman's known
  AppArmor signature points at the Podman environment checklist
- Guide article 15: deploying with Podman instead of Docker

### Changed — breaking

- The generated Compose file is now `.brickkit/generated/compose.yaml`,
  was `docker-compose.yaml` — the file follows the Compose Specification,
  which Docker and Podman both read. BrickKit always passes `-f`, so only
  scripts or tools that open the generated file by name need the new name

## [0.8.0] - 2026-09-24

### Added

- `override.yaml`: a personal, git-ignored file next to `brickkit.yaml` for
  what only applies to your machine — a component's `mode` and `localPort`
  (shell members nested under their shell) and a `target` that may only be
  downgraded (`k8s` → `docker` / `podman`). `up`, `down`, `sync` and
  `status` apply it after checking it against `brickkit.yaml`: an entry for
  an undeclared component, or a target upgrade, stops the command. Each
  entry records a baseline, so `up` and `lint` print a note (never block)
  when `brickkit.yaml` changed underneath it. With `--config` naming another
  file, `override.yaml` is ignored, with a warning
- `brickkit override`: creates `override.yaml`, and on later runs refreshes
  it — keeping your customisations by component ID, adding new components,
  dropping removed ones, re-nesting `servedBy` members and printing the
  drift notes
- `add` keeps `override.yaml` in step (or, when it holds real
  customisations, leaves it and says so), `remove` drops the component's
  entry, `lint` reports dangling entries and stale baselines, `restore`
  suggests a refresh after a `mode` change, and `status` labels components
  disabled through `override.yaml`
- `target: podman` in `override.yaml`: deployment files are generated, and
  a real run stops with a clear "not implemented yet" instead of silently
  running under Docker
- A Podman environment checklist (English/Chinese) with
  `scripts/podman/check-environment.sh` and `scripts/podman/fix-apparmor.sh`

### Changed — breaking

- `brickkit.yaml` no longer accepts `mode: debug`, under any target: "I am
  debugging this on my machine" moves to `override.yaml`
  (`brickkit override`, then set `mode: debug` on the component there)
- A `servedBy` member whose named shell isn't running this cycle (most
  often `mode: disable`) no longer blocks `up`/`graph` with
  `Error: the shell servedBy points to is not currently running`. It now
  falls back to deploying standalone from its own image instead of merging
  into the shell, and `up` prints a warning naming the component and the
  shell instead. This means a `servedBy` member's own image now has to be a
  real, independently-runnable artifact — not just a formality — and any
  resource dependency it declares needs its *own* binding to fall back on,
  since the shell's binding no longer counts once the shell isn't running
  (`up` still blocks the run if that binding is missing, just with a
  different error). `brickkit graph` stops grouping the member under the
  shell in this case too. The "named shell doesn't exist at all" case is
  unchanged and still rejects

### Changed

- The README reorganised: analogy, demo and expectations first, theory and
  installation later

### Fixed

- `make cover-check` swallowing `go test` output on failure, and the
  multi-line `.env` tests picking up an ambient `APP_TOKEN_SIGNING_KEY_PEM`

## [0.7.1] - 2026-09-22

### Changed

- The default `--log-level` is now `warn`, was `info`. The routine
  per-command lifecycle lines (`Command started`, `Command finished`, and
  the handful of other `logging.Info` calls like "component added" or
  "workspace tidied") are pure JSON echoes of what stdout already shows in
  human-readable form, and are quiet by default now. A failing command's
  `error_code` line is unaffected — it's `error`-level (`warn` for a
  warning-class result), so it still prints by default; scripts keying off
  it need no change. Anything that was relying on the old default to see
  the lifecycle lines should pass `--log-level info` explicitly, or set
  `BRICKKIT_LOG_LEVEL=info`

## [0.7.0] - 2026-09-22

### Added

- `mode: local`: BrickKit starts the component as a process on your
  machine, from its local source, and supervises it in the foreground —
  after the containers are up, in dependency order. The start command is
  detected from the source (Go, Rust, .NET, Node, Java with the Spring Boot
  plugin, Python/Django, Ruby/Rails), or set in `component.yaml`'s new
  `local:` block (`language`, `runCommand`); `up --dry-run` prints it. The
  process gets the same variables a container would, plus `PORT` (now a
  reserved name); a `${VAR}` that can't be resolved is an error rather than
  a literal placeholder, and inherited `NODE_OPTIONS` / `JAVA_TOOL_OPTIONS`
  / `JDK_JAVA_OPTIONS` are cleared so nothing waits for a debugger. When one
  process crashes, all stop and the last screen shows its exit code or
  signal and its last output lines (`up --crash-lines N`, default 20, `0`
  for none). One foreground session per project, guarded by a lock that
  names the holder's PID; `status` and `down` say when a session is
  running, and `graph` draws such nodes as "managed locally". A project of
  only `mode: local` components never calls the container engine. Docker
  targets only, and not combined with `servedBy` or `replicas`
- Guide article 4: `mode: local` end to end, smoke-tested against the real
  CLI
- `make check-cross-build`: the CLI and its tests must build and vet on
  Linux, macOS and Windows
- Documentation notes that SchemaStore now lists `brickkit.yaml` (editors
  validate it with no setup; `component.yaml` still needs the schema wired
  by hand), and that no public market is operated by BrickKit

### Changed — breaking

- `components[].enabled` and `components[].local` are replaced by one
  field, `components[].mode`, with the values `enabled`, `disable` and
  `debug` (and `local`, above); leaving it out still means "follow the
  top". Rename `enabled: true` to `mode: enabled`, `enabled: false` to
  `mode: disable` and `local: true` to `mode: debug` (`localPort` is
  unchanged). There is no compatibility shim — the old keys are rejected as
  unknown fields. Two behaviours change with it: `mode: debug` is now pinned
  exactly like `mode: enabled` (it keeps running whatever is above it, and
  turning off one of its required dependencies is an error, where
  `local: true` used to be skipped like any other component), and
  `mode: debug` together with `deploy.target: k8s` is rejected when
  `brickkit.yaml` is parsed — so `brickkit lint` catches it — instead of
  only when the deployment files are generated

## [0.6.0] - 2026-09-21

### Added

- `brickkit lang` shows the language the CLI speaks and where that came
  from; `brickkit lang set <en|zh>` stores it in a machine-level file
  (`brickkit/config.json` under the user config directory). `BRICKKIT_LANG`
  wins over the file, the file over the default; an unknown value is
  ignored rather than blocking startup
- Every message the CLI prints — errors, hints, tables, `--help` — comes
  from English and Chinese message catalogs, and English messages get their
  singular and plural forms right
- The AI-assistant skills come in English and Chinese. A project remembers
  the language it was installed in (in `skills.lock`), so teammates with
  differently configured CLIs don't rewrite each other's committed files;
  `brickkit skills update --lang <en|zh>` switches it and `skills status`
  shows it. A project installed before this keeps Chinese

### Changed — breaking

- The CLI speaks English by default; it used to print Chinese only. Run
  `brickkit lang set zh`, or set `BRICKKIT_LANG=zh`, to keep Chinese. The
  `message` of the JSON log lines follows the language; their keys and
  `error_code` stay English and unchanged, so scripts keying off them need
  no change

### Changed

- `install.sh` and the market server's user-facing messages (HTTP errors,
  logs, audit details) are in English. The market doesn't follow the CLI's
  language, so its own words relayed by the CLI stay English in Chinese
  mode too
- The English documentation shows real English CLI output, and the README
  explains how to switch the CLI's language

### Fixed

- Five Mermaid diagrams in the Chinese documentation failing to render
  (unquoted subgraph titles containing full-width punctuation)

## [0.5.0] - 2026-09-20

### Added

- `brickkit graph`: prints the project's dependency topology as Mermaid text
  on stdout — required dependencies solid, optional ones dashed (an optional
  dependency that can't be found is drawn as a "not installed" node),
  components that won't start this run greyed out, and `servedBy` members
  grouped inside their shell. Nothing but Mermaid goes to stdout, so
  `brickkit graph > graph.mmd` gives a file GitHub renders;
  `--ignore-served-by` draws every component standalone
- `brickkit lint`: an offline, read-only structure check of `brickkit.yaml`
  and of the `component.yaml` files under the project's local install sources
  — or, in a standalone component repository (a `component.yaml` and no
  `brickkit.yaml`), of that one file. It reports what `up`, `add` and
  `publish` would reject (missing required fields, wrong types, unknown keys,
  malformed versions, out-of-range ports), plus two kinds of warning: a
  misspelled key inside a `configSchema` property, and a config key that
  collides with a reserved variable — checked for every key `configSchema`
  declares, a wider net than the warning `up` prints. It exits `1` on errors;
  `--strict` makes warnings fail too, for a CI gate. Error code
  `LINT_FAILED`. It does not resolve dependencies, check that a `servedBy`
  target exists, or run the few combination rules that are only checked when
  deployment files are generated (a running `expose: true` component needing
  `ingressController` under a network policy, for one) — those stay with
  `brickkit up --dry-run`
- `schemas/component.schema.json` and `schemas/brickkit.schema.json`: JSON
  Schemas for `component.yaml` and `brickkit.yaml`, generated from the CLI's
  own Go structs (`make generate-schemas`). An editor with YAML-schema support
  can offer field completion and mark unknown fields, wrong types and
  malformed versions as you type; the Quick Start shows how to wire them up
- `brickkit new <scope>/<name>`: a minimal component skeleton — a
  `component.yaml` that passes validation, under `components/<scope>/<name>/`
  (where a local install source already looks), or in `--path <dir>` for a
  component that is its own repository; `--contract openapi|proto` adds a
  contract placeholder registered under `artifacts`. It refuses an existing
  directory, writes no source code or Dockerfile, and doesn't add the
  component to the project
- `secret: true` on a `configSchema` property: on Kubernetes the value goes
  into a generated Secret (`<versioned-service-name>-config-secret`) and
  reaches the Deployment as a `secretKeyRef`, no longer as plain text. The
  plaintext-secret warning honours it whatever the key is called
- `existingSecret`: point at a Secret another system (Vault Secrets
  Operator, External Secrets, Sealed Secrets …) already put in the cluster —
  `resources[].existingSecret` in place of `password`, or a `secret: true`
  config value written as `{ existingSecret, key }`. Kubernetes only
  (ignored with a warning on Docker); the CLI never reads or writes the value
- `minimum`, `maximum` and `pattern` on `configSchema` properties —
  documentation only, like `enum`; they used to be dropped silently
- A warning, with a guess at the intended key, for a misspelled key inside
  a `configSchema` property (`defualt`, `descripton`), shown by `publish`
  and when local install sources are scanned
- `brickkit skills` in a standalone component repository (a
  `component.yaml`, no `brickkit.yaml`): it manages the `brickkit-component`
  skill there
- Documentation (English/Chinese): Quick Start, Core Concepts,
  Troubleshooting, a comparison page, an AI-assisted development guide,
  Design Principles and Trade-offs, an error-code reference, full field
  references for `component.yaml` and `brickkit.yaml` (guarded against
  drift from the Go structs), the environment-variable injection contract,
  measured results on calling dependency addresses from different
  languages, a secrets guide, a deployment-selection guide, the Market API
  reference, a Go component template, a tutorial on managing component
  source, and `CONTRIBUTING.md` and this changelog. The `docs/{en,zh}`
  folders and files are numbered in reading order

### Changed

- `${VAR}` in `components[].config` and `resources[].password` is no longer
  expanded when `brickkit.yaml` is parsed; each renderer resolves it. A
  variable set in the process environment (typical in CI) therefore no
  longer lands in plain text in the generated Compose file — the same as a
  variable from `.env` already did
- A dependency written in mapping form with `version:` or `ref:` next to
  `id` is rejected instead of the extra key being silently dropped; the
  version goes into the ID (`id: <id>@<version>`)
- GitHub Release notes are in English

### Fixed

- `brickkit publish --sign` crashing under cosign v3: the CLI's own
  `sign-blob` call still used `--output-signature`, which cosign v3 ignores
  without `--bundle`. It now writes a bundle and reads the signature from
  either the v2 or the v3 bundle layout
- An empty-string config value (`default: ""`, or a `config` override of
  `""`) silently missing from the Compose file and local-debug env files,
  while Kubernetes got it
- The rename suggestion for a config key that collides with a reserved
  variable colliding again — with a resource prefix (`redisEndpoint` →
  `redisBaseUrl` → `REDIS_*`) or with the project's own `envPrefix`
- `brickkit new --path` with an absolute path writing under the current
  directory
- `brickkit add --repo` claiming there was no Git address when the source
  was already on disk (or archived by `sync`) in the default layout
- `brickkit init`'s hook line running path and description together; the
  AI context files `init` installs pointing at pre-restructure document
  paths; CLI messages and `--help` pointing at archived design documents

## [0.4.6] - 2026-09-17

### Fixed

- The release workflow's `cosign sign-blob` step crashing under cosign v3,
  which ignores `--output-signature` unless `--bundle` is also given; the
  release now publishes a single Sigstore bundle (the same problem in
  `brickkit publish --sign` itself was fixed in 0.5.0)
- A `local: true` component depending on a `servedBy` member getting a
  plausible-looking but unreachable `localhost:<port>` address for that
  member's extra ports — the host-port mapping now opens on the shell's
  compose service, using the member's own declared port
- `golangci-lint` findings (errcheck/staticcheck/unused)
- The release workflow referencing a nonexistent `cosign-installer@v4` tag

### Changed

- The release workflow upgraded to the Node 24–based major version of its
  GitHub Actions

## [0.4.5] - 2026-09-17

### Fixed

- `readDotEnv` splitting `.env` files by physical line, truncating any
  value that legitimately spans multiple lines (e.g. a PEM key)

## [0.4.4] - 2026-09-17

### Fixed

- `local-debug.*.env` serialization truncating or mis-parsing multi-line
  values and values containing shell-special characters

## [0.4.3] - 2026-09-16

### Changed

- A `servedBy` member's own config values now land in the shell's
  environment under a component-ID-prefixed variable name instead of
  unprefixed, and `BRICKKIT_SERVED_MEMBERS_CONFIG`'s config field was
  renamed to `configEnvVars` — this closes a real bug where a `${VAR}`
  secret placeholder in a member's config could corrupt the generated
  JSON once Docker Compose's own text substitution touched it
- Added the project's own development principle to `CLAUDE.md`: prefer the
  architecturally correct fix over the smallest patch, regardless of the
  extra work involved

## [0.4.2] - 2026-09-15

### Added

- Structured, per-member config injection for `servedBy` shells
  (`BRICKKIT_SERVED_MEMBERS_CONFIG`) and an `--ignore-served-by` validation
  flag for `brickkit up --dry-run`
- Documentation for how an out-of-band tool should construct a `servedBy`
  component's address

## [0.4.1] - 2026-09-14

### Fixed

- `collectTargets` not accounting for `servedBy` semantics, causing a real
  `up` to fail with "no such service"
- Resource-binding validation not accounting for `servedBy` semantics
- `servedBy` label merging missing its exclusion rule — a member's own
  labels no longer participate in the shell's label merge

## [0.4.0] - 2026-09-14

### Added

- The `servedBy` companion documentation set: a deployment checklist, a
  self-hosted-market deployment guide, shared-connection-pool methodology,
  seed/test-data planning methodology, and component design guidelines
  (English/Chinese pairs)
- Four architecture articles: dependency resolution, deployment-file
  generation, resource-binding mechanics, and signing/trust
  (English/Chinese pairs)
- The 12-part hands-on tutorial series under `docs/{en,zh}/guide/`, from
  the first running project through Kubernetes, local debugging,
  versioning, signing, building a component from scratch, network policy,
  and multi-project sharing
- The full CLI command reference (`docs/{en,zh}/architecture/cli-reference.md`)
- A topic-based documentation index for casual (non-AI) readers in the README
- Per-language `llms.txt`/`llms.zh.txt`, matching the README/AGENTS split

### Fixed

- A closed-source image hardening claim that was already true by default
  (digest pinning), corrected to reflect actual `brickkit publish` behavior
- A ~1/16-probability false positive in `check-install-sh.sh`
- A `TestUpDryRunIsRepeatable` flake racing against the wall clock

## [0.3.0] - 2026-09-14

### Added

- `servedBy`: components can declare that their workload is provided by
  another (shell) component, generating no container/Deployment of their
  own while still getting correctly routed `*_ENDPOINT` addresses on both
  Docker and Kubernetes — plus `internal/shell` (grouping, validation,
  merging), Compose and K8s renderer support, and `BRICKKIT_SERVED_MEMBERS`
  injection
- "Building a Qualified Shell" and closed-source image hardening guides
  (English/Chinese pairs)

### Changed

- The full documentation tree restructured: `design/`, the old hands-on
  guide, and dev-progress logs archived as historical record; `AI-CONTEXT.md`
  renamed to `AGENTS.md` with language
  routing rules; `README.md`/`README.zh.md` and `AGENTS.md`/`AGENTS.zh.md`
  became independently-written language pairs instead of one file with
  translated sections; `docs/en/`/`docs/zh/` established as the current,
  symmetric documentation tree with dedicated CI gates keeping them
  mirrored

## [0.2.2] - 2026-09-08

### Fixed

- A local source's cached `metadata.id` silently going stale after a
  component was renamed, instead of erroring

## [0.2.1] - 2026-09-06

### Fixed

- Three gaps around Git submodule registration: `restore --check` false
  positives, and `sync`/`remove` silently corrupting state instead of
  blocking it

## [0.2.0] - 2026-09-03

### Added

- `brickkit restore` and `brickkit restore --check`, a pre-commit
  consistency gate for projects that keep component source under version
  control (catches an archive-state move committed without its matching
  `enabled` change), plus `brickkit init --hooks` to install it
- `labels` passthrough on a component's deployment metadata — the platform
  doesn't interpret keys/values, so an out-of-band gateway (Traefik,
  Prometheus, etc.) can still hook in without the platform ever having to
  understand routing

## [0.1.1] - 2026-09-02

### Added

- AI-assistant skills: `brickkit init` now generates `.claude/skills/`
  (four skill files, each scoped to "what an AI is likely to guess wrong"),
  plus `brickkit skills status`/`update` and a `skills.lock` tracking
  whether a project's copy is still the one the platform shipped

## [0.1.0] - 2026-09-01

Initial release. The whole platform, built from an empty repository:

### Added

- CLI commands: `init`, `add`, `remove`, `fetch`, `up`, `down`, `status`,
  `sync`, `login`, `logout`, `publish`, `version`
- Docker Compose and Kubernetes deployment-file generation from one
  `component.yaml`/`brickkit.yaml` pair, including local debug mode
  (`local: true`) and database migrations
- Recursive dependency resolution with required/optional dependencies and
  topological sort
- Environment-variable injection for dependency addresses, resource
  connections, and a component's own config
- Versioned service names, giving multi-version coexistence for free
- The component marketplace (`market-server`): publishing, discovery,
  versions, visibility, cosign signing with Go-standard-library
  verification on the installer side
- NetworkPolicy/ServiceAccount generation from the dependency graph
- Ten real fixture components (`tests/components/`) used to test the
  platform itself against real Docker and Kubernetes deployments
