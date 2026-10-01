# Cache design

## Two caches

| Where | Holds | Shared by |
| --- | --- | --- |
| The project's `.brickkit/` | The `component.yaml`, docs and contracts of the component versions this project uses; generated deployment files; this machine's state | This project only |
| The user-level `~/.cache/brickkit/repos/` | Bare repositories of component Git repositories | Every project on this machine, and components' own workbenches |

Bare repositories are covered in [The Git repository cache](06-bare-repo-mechanism.md). This page is about the project's
`.brickkit/`.

## What's in `.brickkit/`

```text
.brickkit/
├── manifests/<scope>/<name>/<version>/
│   ├── component.yaml     this version's Manifest (a permanent cache)
│   ├── BRICKKIT.md        only when the component carries docs
│   ├── BRICKKIT.<lang>.md each translation it carries (BRICKKIT.zh.md …)
│   └── signature.json     where it came from, and signature information
├── artifacts/<versioned service name>/<type>/…   contract files the component declares (downloaded by add / fetch)
├── generated/
│   ├── compose.yaml       the deployment file for Docker / Podman
│   ├── env/               env files holding secrets (0600)
│   ├── k8s/               Kubernetes manifests
│   └── local-debug.<service name>.env   environment variables of a mode: debug component
├── last-run               the component versions of the last up; the baseline for version-change notices
├── local-mode             local mode is on while this exists
├── deploy.local.base.yaml the deploy.yaml that deploy.local.yaml was last copied from; refresh tells local changes by it
├── session.lock           a mode: local session is running
└── credentials            the component market login token (0600)
```

## Caching rules

**Manifests are cached permanently and never expire.** An exact version is immutable: whatever `demo/hello@1.0.0`'s
`component.yaml` is today, it always is — a released version doesn't change (`release` refuses to move an existing tag).
So there's no such thing as "the cache expired"; fetched once, it's used from then on.

**Local sources are never cached.** For components from a local install source (`components/`, `shell/`), the
`component.yaml` is read from the directory afresh every time: that's code under development, and a change should take
effect at once. When a local source directory at exactly this version exists, the doc path points at it too, rather than
at the snapshot in the cache.

**Generated files are rewritten every time.** Everything under `generated/` is output of `up`, regenerated every time;
hand edits are overwritten by the next `up`.

**Documentation is cached with the Manifest, from every kind of source.** `add` and `fetch` write `BRICKKIT.md` and
every `BRICKKIT.<lang>.md` next to `component.yaml` — from a local source, a Git tag or the market alike — and nothing
else: a component's `AGENTS.md` and `README.md` are for its own repository.

## `.brickkit/` stays out of Git

The `.gitignore` `init` generates leaves it out. What's inside either can be obtained again (Manifests, contracts,
generated files) or is the state of this machine and this person (the local-mode switch, the session lock, the login
token).

After a fresh `git clone` of a project, `.brickkit/` doesn't exist, and that's fine: on the first command, the CLI fetches
the Manifests and contracts it needs from the install sources again, by `brickkit.yaml`. It's the same reasoning as
`package.json` in Git and `node_modules` out of it: `brickkit.yaml` is the lock file, `.brickkit/` is what gets installed
from it.

## The AI assistant skills carry their own record

The skill files under `.claude/skills/` are committed with the project, and the CLI has to know, for each one, whether it
wrote it and whether someone has edited it since — an edited file is never overwritten. Earlier versions kept that record
in `.brickkit/skills.lock`. But `.brickkit/` stays out of Git: a teammate's fresh clone had no lock, and from then on the
skills there could never be refreshed.

Now each skill file carries its record on its last line:

```text
<!-- brickkit:skill version=<v> sum=sha256:<hex> -->
```

`version` is the CLI that wrote the file; `sum` is the fingerprint of the content above the line. When
`brickkit skills status` or `brickkit skills update` reads the file and the content still matches the fingerprint, it
is the CLI's own and can be refreshed; when it doesn't, someone edited it, and it is skipped. The record travels with the file, so every clone knows.

The skills' language isn't in the skill files either: it is the `lang=` recorded in the block at the end of the project's
`AGENTS.md`. An old `.brickkit/skills.lock` is read once, to recognise the files an earlier CLI wrote, and then deleted by
`brickkit skills update`.

## What can be deleted

| Delete | Consequence |
| --- | --- |
| All of `.brickkit/` | Safe. The next command fetches Manifests and contracts again and regenerates the deployment files; local mode turns off, and the market needs a new login |
| `manifests/`, `artifacts/` | Safe; fetched again as needed (from the user-level bare repositories, usually without the network) |
| `generated/` | Safe; the next `up` regenerates it |
| `~/.cache/brickkit/repos/` | Safe; re-cloned the next time it's needed |

Deleting caches is never the way to fix a problem — exact versions in the cache don't "go bad". When a change in a local
source doesn't take effect, first check the component really comes from the local source (the order of install sources in
`brickkit.yaml`).
