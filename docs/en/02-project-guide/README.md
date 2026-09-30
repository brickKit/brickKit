# Running a project

**Who it's for**: whoever uses BrickKit to assemble existing components into a system and get it running on a
development machine and on servers. You don't necessarily write components — for that, see
[Writing components](../03-component-guide/README.md).

**Read first**: the [Quick start](../00-intro/02-quick-start.md) (one component running in five minutes) and
[The three layers](../01-three-layers/README.md) (what `brickkit.yaml`, the deploy file and `config/` each own).

This module follows a project's life: create the project → add components → debug locally → work on components in place → several environments →
start and stop → upgrade → remove → look around → check → manage source → build. Every command and every output on these
pages comes from a real run, using the same demo project, `my-shop`:

| Component | What it does |
| --- | --- |
| `demo/hello` | Answers with a greeting; the greeting is the config item `GREETING` |
| `demo/caller` | Calls `demo/hello` (a required dependency) and optionally uses `demo/bus` (an optional dependency) |
| `demo/bus` | An equally simple service playing the "message bus" |

They live on a company-internal Git server at `https://git.example.com/components/<scope>-<name>`, one tag per version.

| Page | What it covers |
| --- | --- |
| [01 Creating a project](01-init-and-project-creation.md) | `init`'s two modes, the generated skeleton and `.gitignore`, cloning an existing project |
| [02 Adding components](02-add-and-component-install.md) | Which files one `add` changes, the `$var:` prompt, `--local`, `--repo` |
| [03 Local debugging](03-local-debug-workflow.md) | `local on` → `mode: debug` → run it in your IDE → containers reach you; what to do after the team changes files |
| [04 Developing inside the project](04-focus-run.md) | `up` from a component's directory, `--focus` / `--all`, what starts and why, one `components/` |
| [05 Several environments](05-multi-env-switch.md) | One deploy file per environment, chosen with `-f`; `vars:` gives the same `config/` different values per environment |
| [06 Starting and stopping](06-up-and-down.md) | What `up` does, the image check, `--dry-run`, `down`, common start failures |
| [07 Upgrading and config migration](07-upgrade-and-migration.md) | How `upgrade` migrates config, how conflicts are resolved, how old versions stay |
| [08 Removing and archiving](08-remove-and-archive.md) | `remove`'s checks, archiving and restoring config, protecting source directories |
| [09 Status and topology](09-status-and-graph.md) | `status`, `deps`, `graph` |
| [10 Offline checks](10-lint-and-checks.md) | What `lint` checks and doesn't, putting it in CI |
| [11 Managing source](11-sync-and-restore.md) | `sync` archives unused source, `restore` puts things back, the pre-commit check |
| [12 Building and images](12-build-and-images.md) | Why `up` never builds, how to use `build`, image tag rules |

Every command's full flag list is in the [CLI reference](../07-cli-reference/README.md).
