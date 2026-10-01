# The three layers at a glance

## Directory layout

Every file in a project, and what each one does:

```text
my-shop/
├── brickkit.yaml          declaration: install sources, components and exact versions (the lock file, in Git)
├── deploy.yaml            the team's deploy file: target, how each component runs (in Git)
├── deploy.local.yaml      your personal deploy file (created by brickkit local on, not in Git)
├── deploy.prod.yaml       another environment's deploy file (optional, chosen with -f, in Git)
├── config/
│   ├── vars.yaml          shared variables: values several components use (in Git)
│   ├── erp-backend.yaml   config for the default version of erp/backend (in Git)
│   ├── erp-backend@1.0.0.yaml   config used only by version 1.0.0 (when versions coexist)
│   └── .archive/          config archived by remove, restored by a later add (not in Git by default)
├── components/            a local install source: component source (each its own Git repository, not in the project's Git by default)
├── shell/                 a local install source: shell components
├── AGENTS.md              the project's AI guide; the table of components at its end is maintained by the CLI (in Git)
├── CLAUDE.md              one line, @AGENTS.md, so Claude Code reads AGENTS.md too (in Git)
├── .claude/skills/        the AI assistant skills, brickkit-* (in Git)
├── .env                   this machine's environment values, where ${VAR} is looked up (not in Git)
├── .secrets/              secret files referenced with file:// (not in Git)
└── .brickkit/             the CLI's caches and generated files (not in Git)
    ├── manifests/         each component version's component.yaml, BRICKKIT.md and its translations (permanent cache)
    ├── artifacts/         downloaded contract files
    ├── generated/         the generated compose.yaml / Kubernetes manifests / 0600 env files
    └── local-mode         the state of the local-mode switch
```

## Which file each kind of information belongs in

| What you're writing | Belongs in | Example |
| --- | --- | --- |
| Which components the project uses, at which versions | `brickkit.yaml` | `components: [{id: erp/backend, version: 1.2.0}]` |
| Where to look for components | `brickkit.yaml` | `sources: [{name: org, type: git, baseUrl: …}]` |
| Where to deploy (Docker / Podman / Kubernetes) | the deploy file | `target: k8s` |
| Whether a component runs, and how | the deploy file | `mode: disable`, `expose: true`, `replicas: 3` |
| Which component is a shell and which members it hosts | the deploy file (`members`); `brickkit.yaml` only carries the `kind: shell` marker the CLI maintains | `members: [{id: erp/api}]` |
| Kubernetes namespace, network policies | the deploy file's `k8s:` block | `k8s: {namespace: shop}` |
| The environment variables a component gets | `config/<component>.yaml` | `DB_HOST: pg.internal` |
| A value several components share | `config/vars.yaml` | `PG_HOST: pg.internal` |
| A shared value that differs in one environment | that environment's deploy file, under `vars:` | `vars: {PG_HOST: pg.prod}` |

## Four questions to place a field

1. Does it say **what this project is made of**? → `brickkit.yaml`
2. Does it say **how to deploy and orchestrate**? → the deploy file
3. Is it **an environment variable the component's process reads**? → `config/<component>.yaml`
4. Is it **one value several components need**? → `config/vars.yaml`, referenced from each with `$var:`

When it's unclear, think about who changes it and how often: component versions are changed by whoever assembles the
system, and get reviewed; how things are deployed varies by environment; configuration holds secrets. Put all three in
one file and in the end everyone edits it and no one can read it — that's the "one job per file" principle.

## Which file to read

| To find out | Read |
| --- | --- |
| Which components the project has, and their versions | `brickkit.yaml` |
| How a component is deployed this time | `deploy.local.yaml` when local mode is on, otherwise `deploy.yaml`; if the command was given `-f`, that file |
| Which environment variables a component gets | `config/<component>.yaml`; values its `$var:` references point to are in `config/vars.yaml` or the deploy file's `vars:` |
| What config a component needs and what it depends on | Its `component.yaml`: under `.brickkit/manifests/<scope>/<name>/<version>/`, or in the source directory for a local-source component |
| How to use a component | Its `BRICKKIT.md` (translations as `BRICKKIT.<lang>.md`), in the same place |
| The team's conventions, and which components the project has with where their docs are | `AGENTS.md` at the project root: the author's sections, then the component table |

**Why there is no "merged view" command** (one command folding the three layers into one for you to look at): it would
be a fourth thing to understand and to trust, and one day it would disagree with the files that actually take effect.
Each question has exactly one file responsible for answering it; read that one when you need it — "read on demand,
don't merge everything". What really gets generated (`compose.yaml`, Kubernetes manifests) is in
`.brickkit/generated/`; `brickkit up --dry-run` generates without running, so that's where to look for the final result.

## What goes into Git and what doesn't

| File | In Git | Why |
| --- | --- | --- |
| `brickkit.yaml` | ✅ | What the project is made of is a decision to review |
| `deploy.yaml`, `deploy.<env>.yaml` | ✅ | How the team deploys is a shared decision |
| `config/` (including `vars.yaml`) | ✅ | Configuration is part of the project; don't write secrets into it directly — use `${VAR}` or `file://` |
| `config/.archive/` | ❌ | Old config of removed components, kept only to restore on a later `add`; it belongs to this machine |
| `components/` | ❌ (by default) | Each component in it is its own Git repository with its own history; to commit source with the project, `brickkit init --hooks` installs a pre-commit check |
| `AGENTS.md`, `CLAUDE.md` | ✅ | The project's AI guide, written by the team; the component table at the end holds only facts that are the same on every machine |
| `.claude/skills/` | ✅ | The AI assistant skills; each file ends with a line recording which CLI version wrote it, so a teammate's fresh clone can tell whether it was edited |
| `deploy.local.yaml` | ❌ | Your temporary personal way of deploying ("I'm debugging this component"), not a team decision |
| `.env`, `.secrets/` | ❌ | This machine's secrets and environment values, and the secret files `file://` refers to |
| `.brickkit/` | ❌ | Caches and generated files, which the CLI can fetch or regenerate at any time |

The `.gitignore` that `brickkit init` generates already has these entries; when `init` completes an existing directory
and its `.gitignore` lacks a required entry, it warns loudly — without them, personal files and secrets get committed.
