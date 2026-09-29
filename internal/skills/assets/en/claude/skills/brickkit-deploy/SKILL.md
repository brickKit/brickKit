---
name: brickkit-deploy
description: Use when deploying a BrickKit project to Docker, Podman or Kubernetes, editing deploy.yaml or deploy.local.yaml, turning local mode on/off/refresh, debugging a component in an IDE (mode debug) or running it as a bare process (mode local), setting up several environments with -f, filling config/ values and secrets, building images, hosting members in a shell, or exposing a service. Covers what goes in which file, targets and the k8s block, config value forms ($var, ${VAR}, file://, existingSecret), images and brickkit build, shells and skipWaitFor. Applies when the user mentions deploy / target / k8s / compose / ingress / secrets / local on / debug, or asks "how do I go live" or "how do I debug this one component".
---

# Deployment, local mode and configuration

## When to use this skill

- Running the project on Docker, Podman or Kubernetes, or going to production
- Changing how one component is deployed (expose, replicas, quotas, labels, mode)
- Debugging one component in an IDE, or running it outside a container
- Filling in configuration and secrets under `config/`
- Several environments (dev / staging / prod)
- Hosting several components inside one shell

## What goes where

| You want to change | Write it in |
| --- | --- |
| which components/versions exist | `brickkit.yaml` (via `brickkit add` / `upgrade`) |
| how the team deploys them: `target`, entries, `k8s:`, `vars:` | `deploy.yaml` |
| something only true on your machine: `mode: debug`, a free `localPort`, your own database host, another target | `deploy.local.yaml` (after `brickkit local on`) |
| a component's business values | `config/<scope>-<name>.yaml`, shared values in `config/vars.yaml` |

## Where you'll guess wrong

**1. The deploy file lists every component version exactly once.**

One entry per version in `brickkit.yaml`, members nested under their shell counting too. A bare
`- id: erp/backend` is the default version; a `requiredBy` version needs its own
`- id: erp/backend@1.0.0`. Missing or extra entries fail loudly (`DEPLOY_INCONSISTENT`).
`add` / `remove` / `upgrade` maintain this for you — don't add components by writing entries.

**2. `deploy.local.yaml` replaces `deploy.yaml`; it is never merged.**

`brickkit local on` copies `deploy.yaml` the first time — identical except that the "team file" header
`init` wrote becomes a personal-file header (an existing file is reused)
and from then on **every** command reads `deploy.local.yaml` instead. `local off` switches back and
keeps the file. When the team changes `deploy.yaml`, your copy doesn't follow: `up` refuses once the
component set differs. Run `brickkit local refresh` — it saves the old file as
`deploy.local.yaml.bak`, writes a fresh copy and **lists your old local changes** for you to re-apply
by hand. The CLI never merges. `--no-local` ignores the file for one run.

**3. `mode: debug` is only accepted in `deploy.local.yaml`.**

It is a personal fact ("I'm debugging this now"), so `deploy.yaml` rejects it. With it the component
gets no container; other containers reach your IDE process through `extra_hosts`, and a
`local-debug.<versioned-service-name>.env` is generated for the IDE to load (dependency addresses as
`localhost` ports). Set `localPort` to what your process listens on. No migration is run for it.
`mode: local` is different: BrickKit detects the start command from the local repo, launches and
supervises the process in the foreground (`Ctrl+C` stops it), and it may go in `deploy.yaml`.
Both work on docker / podman and are rejected on `target: k8s`. Both run code from the local repo,
whose `metadata.version` must equal the default version in `brickkit.yaml` — so only the default
version can run this way; a `requiredBy` version with `mode: local` / `debug` is refused.
`brickkit graph` never reads local mode, so a `mode: debug` there never shows up in the graph.

**4. Environments are whole files: `brickkit up -f deploy.prod.yaml`.**

No overlay, no inheritance. `brickkit.yaml` and `config/` are shared; per-environment differences in
config go through `$var:NAME` references, overridden by the deploy file's `vars:` block. `-f` ignores
local mode entirely.

**5. Config keys are env var names; values take five forms.**

```yaml
DB_PORT: 5432                          # literal
DB_HOST: $var:DB_HOST                  # from config/vars.yaml, overridden by the deploy file's vars:
DB_PASSWORD: ${DB_PASSWORD}            # process environment, then .env (never committed)
TLS_CERT: file://.secrets/cert.pem     # file contents, path relative to the project root
API_TOKEN: { existingSecret: api, key: token }   # K8s only, secret keys only
```

A required key is written by `add` as `KEY: ""` — `up` refuses while it's empty and names it.
Optional keys are commented (`# LOG_LEVEL: info`): leave them commented to follow the component's
default, so new defaults arrive with upgrades. `$var:NAME` has **no space** after the colon — `$var: NAME` is a YAML map, not a reference. An
undefined `$var:` is an error; there is no fallback, and values in `config/vars.yaml` can't chain
another `$var:`.
Plaintext in a `secret: true` key warns: config files are committed. On Docker, `${VAR}` is left for
compose to resolve at start; on K8s the CLI resolves it and `secret: true` values go into a Secret.

**6. There are no resource bindings.** A database or cache is deployed by ops, and the component
reads it through its own config keys (`DB_HOST`, `DB_PASSWORD`, …). The database itself is created
by you, once; tables come from the component's migration.

**7. `up` never builds images.** Local-source components (and git ones with only
`deployment.build`) need `brickkit build [<id>]` first. Tags equal `metadata.version`, and an
existing image of that version is skipped — changed code without a version bump needs `--force`. A shell image built with stale member versions is `IMAGE_STALE`.

**8. Shells are chosen in the deploy file.** Nest member entries under the shell entry and they run
inside it (no own container; their `*_ENDPOINT` points at the shell; their own expose / labels /
health check don't apply). The hosted version must be the one the shell's `component.yaml` compiles
in. A member with `mode: debug` / `local` leaves the shell and runs as a bare process. Merging can
create a Compose `depends_on` cycle; the error names the edges and offers `skipWaitFor: [<id>]` on an
entry, which only drops the start wait — the component must retry until its dependency is up.
`brickkit up --ignore-shells --dry-run` checks that everything can still stand alone.

**9. `limits` has no default.** Only `requests` does (`100m` / `128Mi`). Recommended: CPU `requests`
with no ceiling; memory requests = limits. Write it inline:
`resources: { requests: { cpu: 200m, memory: 256Mi } }`. Don't merge components just to save memory.

**10. A gateway hooks in through `labels`**, quoted string values, copied verbatim to Docker labels /
K8s annotations. A gateway on Docker joins the `brickkit-<project>-net` network. Don't hand-write a
file-provider config full of versioned service names — it goes stale on every version bump.

**11. Signature verification needs `installer.publicKeys` in `brickkit.yaml`.** With none configured,
nothing is verified, whatever `requireSignature` says.

## How the mechanism works

**Targets**: `target: docker | podman | k8s` in the deploy file. Podman runs the same generated
compose file through `podman compose`. K8s settings live in the `k8s:` block (`context`, `namespace`,
`createNamespace`, `podSecurity`, `imagePullSecrets`, `ingressClass`, `ingressAnnotations`,
`serviceAccount`, `networkPolicy`) and warn on other targets; per-entry `hostname`, `tlsSecret`,
`replicas` (`> 1` adds a PDB) and `serviceAccountName` are K8s only; `exposePort` and `skipWaitFor`
are docker / podman only.

**Addresses** are `http://<versioned-service-name>:<port>` on every target, so code never changes.

**What `up` does**: load the three layers → decide who starts → check images (missing → build hint,
git images pulled) → generate files (`--dry-run` stops here) → migrations (a failure blocks the main
service) → start the engine → supervise `mode: local` processes.

## Where to dig deeper

- Flags: `brickkit up --help`, `brickkit local --help`, `brickkit build --help`, `brickkit lint --help`
  (`lint --strict` also checks that `${VAR}` and `file://` references resolve)
- A component's configuration guide: `.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md`
- The full specification: <https://github.com/brickKit/brickKit> and its root `AGENTS.md`
