# The routing table

Every question in a BrickKit project has one source of answers. Find the file by the question; don't scan directories from
the top.

## The project's files

| To find out | Read |
| --- | --- |
| Which components the project uses, each at which version, and where they come from | `brickkit.yaml` |
| Where each component's docs and contracts are | `BRICKKIT.md` at the project root (the table the CLI maintains) |
| Who depends on whom | `brickkit deps` (it's not in `brickkit.yaml`) |
| Where it's deployed, which components run, how they run | The deploy file in effect: `brickkit local status` tells you whether it's `deploy.yaml` or `deploy.local.yaml` |
| The config values a component gets | `config/<component>.yaml` (`config/<component>@<version>.yaml` for a compatibility version); for `$var:` references, `config/vars.yaml` and the deploy file's `vars:` |
| What will start this time, and which environment variables each component gets | `brickkit up --dry-run`, then `.brickkit/generated/compose.yaml` |
| What's running now | `brickkit status` |
| Which flags a command has | `brickkit <command> --help` |

## Components depended on

| To find out | Read |
| --- | --- |
| What it is, how to configure it, what it depends on | `.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md` |
| Its config items, defaults, which are required | `configSchema` in the `component.yaml` in the same directory |
| Its interface | The contract files under `.brickkit/artifacts/<versioned service name>/` |
| What its address variable is called | Derive it by the rule: `demo/hello` → `DEMO_HELLO_ENDPOINT` |

When a component's source is cloned under `components/` (a local source), its `BRICKKIT.md` and `component.yaml` are in
that directory, and they're the newest ones.

## What not to read

| Don't read | Why |
| --- | --- |
| The source of components depended on | The internal implementation isn't a promise to the outside; read the contracts and `BRICKKIT.md`. Once the dependency refactors, calls written from its source break |
| `components/.archived/` | Source of components not running this time; `sync` moved them there precisely so you needn't care |
| The `brickkit.yaml`, `deploy.yaml` and `config/` in a component repository | That's the component author's local workbench, unrelated to the project using the component |
| `~/.cache/brickkit/repos/` | The bare-repository cache, for the CLI |
| `.brickkit/` state files outside `.brickkit/generated/` | `last-run`, `skills.lock` and the like are the CLI's internal records |
| `config/.archive/` | Old config of removed components |

## When to read

- **Before starting a task**: the project root's `BRICKKIT.md` (one table tells you which components exist) and
  `brickkit.yaml`.
- **When using or changing a component**: its `BRICKKIT.md`, `component.yaml` and contracts.
- **When changing how things are deployed**: `brickkit local status` first, to confirm which deploy file to change.
- **After acting**: `brickkit lint` (structure) and `brickkit up --dry-run` (dependencies and generation) — let the CLI tell
  you whether it's right, instead of inferring it from reading code.

Don't read every component's docs at the start: read only the components the current task involves, and their direct
dependencies.
