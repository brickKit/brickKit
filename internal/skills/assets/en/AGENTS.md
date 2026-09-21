# This project is assembled with BrickKit

> Written for AI assistants. Humans can read it too, but it exists so the assistant doesn't have to guess.

BrickKit is a **declarative component assembly and management platform**: each brick (component)
is developed, deployed, and called independently; you only declare which components exist and what
they depend on, and the `brickkit` CLI pulls the bricks, resolves the dependency graph, works out
the start order, generates the deployment files, and hands them to Docker or Kubernetes to run —
everything else is derived from that graph. The platform is deliberately minimal: **no registry, no
long-running service, no config center, no gateway** — don't look for them, and don't suggest adding
them; that is a design that has already been explicitly rejected.

⚠️ "No gateway" doesn't mean "can't use a gateway": a gateway is **deployed out of band**, and
discovers components through the `labels` passthrough on a component entry (the platform copies them
verbatim into Docker labels / K8s annotations, without interpreting the keys or values). If the user
wants to wire up Traefik / Prometheus, that's the path — don't talk them out of it.

## Two YAML files

| File | What it is | Who writes it |
| --- | --- | --- |
| `brickkit.yaml` | Project config: which components are installed, on/off, resource bindings, deploy target | The project maintainer |
| `component.yaml` | A component's Manifest: dependencies, ports, config items, migrations | The component's author |

**To know what this project currently has installed, read `brickkit.yaml`** — it's right there, and
more accurate than any summary.

## Four hard rules

1. **Versions must be exact.** `1.2.0` is fine; `^1.2` / `~1.2` / `latest` are all rejected.
   Range versions were considered and rejected — this isn't an unfinished feature.
2. **Never touch a reserved variable.** These environment variables are injected by the platform;
   a same-named item in a component's `configSchema` is ignored: `COMPONENT_ID`,
   `COMPONENT_VERSION`, anything ending in `_ENDPOINT`, and anything starting with `DATABASE_` /
   `REDIS_` / `MQ_` / `STORAGE_` / `SEARCH_` / `SMTP_`.
3. **Health checks have a prohibition.** Never fold a dependency's availability into your own
   health check — that turns one component's hiccup into a cascading outage. A component whose
   cold start exceeds the default 60 seconds needs a larger `startPeriodSeconds`.
4. **Start/stop follows the layer above it.** Turn off a top-level component and everything below
   it stops too. To narrow the scope, edit `enabled` in `brickkit.yaml` — don't turn things off one
   by one.

## Don't memorize flags

**For any command's flags, ask `brickkit <command> --help`.** This file and the skills under
`.claude/skills/` deliberately don't duplicate the flag reference: duplicating it is a promise to
maintain two copies, and the stale one is what makes you confidently type an `unknown flag`.

## Where the finer detail lives

`.claude/skills/` has four task-scoped skills (assemble / write a component / deploy /
troubleshoot) that Claude Code loads automatically when relevant — nothing to configure on your
side.

**To have Claude Code read this page too** (it only reads `CLAUDE.md`, not this file), add one
line to your own `CLAUDE.md`:

```
@AGENTS.md
```

Not adding it is fine too — the four skills work regardless. Your `CLAUDE.md` is your own workflow
file; `brickkit` never touches it.

The full spec lives in the repository's docs: <https://github.com/brickKit/brickKit>
(the root `AGENTS.md` is the whole-site digest; `docs/{en,zh}/06-architecture/`, `patterns/`,
`guide/` are the current authoritative documentation.)
