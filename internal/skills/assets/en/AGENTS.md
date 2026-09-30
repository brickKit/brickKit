# This project is assembled with BrickKit

> Written for AI assistants. Humans can read it too, but it exists so the assistant doesn't have to guess.

BrickKit is a **declarative component assembly and management platform**: each brick (component)
is developed, deployed, and called independently; you only declare which components exist, and the
`brickkit` CLI reads what each one depends on from its own `component.yaml`, resolves the graph,
works out the start order, generates the deployment files, and hands them to Docker, Podman or
Kubernetes. The platform is deliberately minimal: **no registry, no long-running service, no config
center, no gateway** — don't look for them, and don't suggest adding them; that design was
explicitly rejected.

⚠️ "No gateway" doesn't mean "can't use a gateway": a gateway is **deployed out of band** and
discovers components through the `labels` passthrough on a deploy entry (copied verbatim into Docker
labels / K8s annotations, never interpreted). If the user wants Traefik / Prometheus, that's the path.

## Three layers: what, how, and with which values

A project splits its facts by **who changes them and why** — each question has exactly one file:

| File | Answers | In Git |
| --- | --- | --- |
| `brickkit.yaml` | **What** is in the project: name, install sources, one line per component version (exact). It is the lock file | yes |
| `deploy.yaml` | **How** the team deploys: `target` (docker / podman / k8s), one entry per component version (mode, expose, replicas, labels, quotas…), the `k8s:` block, `vars:` overrides | yes |
| `deploy.local.yaml` | Your **personal** full copy of `deploy.yaml`: while local mode is on (`brickkit local on`), the commands that run or check the deployment read it instead | **no** |
| `config/` | **Business values** per component: `config/<scope>-<name>.yaml` (keys are the env var names), `config/vars.yaml` for shared values | yes |

Dependencies are **not** in any of these — they live in each component's `component.yaml`
(`brickkit deps` prints the tree). `brickkit add` / `remove` / `upgrade` keep all three layers in
step for you; hand-editing one layer and forgetting another is what produces a
`DEPLOY_INCONSISTENT` error.

## Read these before reading code

- **`BRICKKIT.md` at the project root** — the project map. The table between
  `<!-- brickkit:managed:begin -->` and `<!-- brickkit:managed:end -->` lists every component with
  the path of its cached docs and contracts; the CLI rewrites that block on add / remove / upgrade,
  so don't edit inside it (write your own notes outside the markers).
- **`.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md`** — each component's own guide
  (purpose, dependencies, how to configure it, contracts, shell declaration), cached next to its
  `component.yaml`. Reading it is faster and more accurate than reading the component's source.
- **`brickkit.yaml`** — to know what is installed right now. More accurate than any summary.

## Five hard rules

1. **Versions must be exact.** `1.2.0` is fine; `^1.2` / `~1.2` / `latest` are rejected — by design.
2. **`brickkit.yaml` is a lock file.** Only versions declared there are used; a required dependency
   whose version isn't declared is an error telling you to `brickkit add` it. The line without
   `requiredBy` is the component's **default version**.
3. **Config keys are environment variable names**, exactly as the component's `configSchema`
   declares them (`DB_HOST`, not `dbHost`). There are no resource bindings: a database is just
   config values such as `DB_HOST` / `DB_PASSWORD`. Secrets are written as `${VAR}` (from the
   environment or `.env`) or `file://.secrets/...`, never as plaintext.
4. **Health checks only check this process.** Folding a dependency into your own health check turns
   one hiccup into a cascading outage. A cold start over 60 seconds needs a larger `startPeriodSeconds`.
5. **Start/stop follows the layer above.** Turn off a top-level component (`mode: disable` in the
   deploy file) and everything below it stops too — don't turn things off one by one.

## Don't memorize flags

**For any command's flags, ask `brickkit <command> --help`.** This file and the skills under
`.claude/skills/` deliberately don't duplicate the flag reference: a second copy goes stale, and the
stale one is what makes you confidently type an `unknown flag`.

## Which skill covers what

`.claude/skills/` has four task-scoped skills that Claude Code loads automatically when relevant:

| Skill | Use it for |
| --- | --- |
| `brickkit-assemble` | add / remove / upgrade / fetch / deps / sync / up / down / status, default versions and `requiredBy`, what starts |
| `brickkit-component` | writing or editing a `component.yaml` and its `BRICKKIT.md`, shells, migrations, releasing a version |
| `brickkit-deploy` | `deploy.yaml` / `deploy.local.yaml`, targets, local mode, `mode: debug` / `local`, environments, `config/` values and secrets, images |
| `brickkit-troubleshoot` | an error from `brickkit`, a component that won't start or connect, looking up an `error_code` |

**These files come with the CLI.** After the CLI is upgraded, `brickkit skills status` shows which of
them are out of date and `brickkit skills update` refreshes them; files you edited by hand are left
alone.

**To have Claude Code read this page too** (it only reads `CLAUDE.md`), add one line to your own
`CLAUDE.md`:

```
@AGENTS.md
```

Not adding it is fine — the skills work regardless. Your `CLAUDE.md` is yours; `brickkit` never
touches it.

The full specification lives in the BrickKit repository <https://github.com/brickKit/brickKit>; its
root `AGENTS.md` is the whole-platform digest.
