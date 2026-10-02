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

`brickkit local on` copies `deploy.yaml` the first time — identical except that the comment lines at the
top (the "team file" header) become a personal-file header in the CLI's language; an existing file is
reused. From then on the commands that run or check the deployment (`up`, `down`, `status`, `sync`,
`lint`, `build`) read `deploy.local.yaml` instead; `graph` and `deps` always read `deploy.yaml`. `local off` switches back and
keeps the file. When the team changes `deploy.yaml`, your copy doesn't follow: `up` refuses once the
component set differs. Run `brickkit local refresh` — it saves the old file as
`deploy.local.yaml.bak`, writes a fresh copy and **lists your old local changes** for you to re-apply
by hand. The CLI never merges. `--no-local` ignores the file for one run; `brickkit local status` shows
the switch and whether the file still matches `brickkit.yaml`. The file is never committed.

**3. `mode: debug` is only accepted in `deploy.local.yaml`.**

It is a personal fact ("I'm debugging this now"), so `deploy.yaml` rejects it. With it the component
gets no container; other containers reach your IDE process through `extra_hosts`, and a
`local-debug.<versioned-service-name>.env` is generated for the IDE to load (dependency addresses as
`localhost` ports). Set `localPort` to what your process listens on. No migration is run for it.
`mode: local` is different: BrickKit detects the start command from the local repo, launches and
supervises the process in the foreground (`Ctrl+C` stops it), and it may go in `deploy.yaml`;
`localPort` may be left out (a free port is picked). The process inherits your terminal's environment
except the names the platform owns (`COMPONENT_ID`, `COMPONENT_VERSION`, `PORT`,
`BRICKKIT_SERVED_MEMBERS`, `BRICKKIT_SERVED_MEMBERS_CONFIG`, every `*_ENDPOINT`, the component's own
`configSchema` keys) — those come only from BrickKit, so a stale `export` can't stand in for them.
Both work on docker / podman and are rejected on `target: k8s` (a Pod can't reach your machine). Both
run code from the local repo, whose `metadata.version` must equal the default version in
`brickkit.yaml` — so only the default version can run this way; a `requiredBy` version with
`mode: local` / `debug` is refused. On a mismatch: `brickkit upgrade <id>@<repo version>`, or check
out the tag of the default version.
`brickkit graph` never reads local mode, so a `mode: debug` there never shows up in the graph.

Ports on your machine, for a process run this way (`mode: local` / `debug`, or the focus component):
the process listens on its `localPort`; without one, on the component's own `deployment.port`, or the
first free port from 8081 when that's taken — it gets the number as `PORT`. Its extra ports are taken
on the host as declared. Each container it depends on is published on the host at 10000 + its
container port (5432 → 15432, 8080 → 18080), or the first free port from 18080 when that's taken, and
the env file / process gets `localhost:<that port>`; a dependency hosted in a shell is published on the
shell's container. Containers that call the process keep its service name, resolved to the host by
`extra_hosts: <service>:host-gateway`. These are the ports BrickKit assigns in one run; one already
used by another program on your machine fails when the engine binds it. A project's own port registry
can use 8080, 8081 and their neighbours as usual; a process leaves its `deployment.port` (for the first
free port from 8081, possibly another component's) only when this `up` already assigned that port —
write `localPort` to fix it — and 10000 + your container ports must stay free for the mappings.
In the values such a process receives, `host.docker.internal` (the host machine, as containers call it)
becomes `localhost`; containers get the value as written, so one `config/` serves both — don't
override it in `vars:`, which reaches the containers too.

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

The default version reads `config/<scope>-<name>.yaml`; a `requiredBy` version reads
`config/<scope>-<name>@<version>.yaml`.

A required key is written by `add` as `KEY: ""` — `up` refuses while it's empty and names it.
Optional keys are commented (`# LOG_LEVEL: info`): leave them commented to follow the component's
default, so new defaults arrive with upgrades. `$var:NAME` has **no space** after the colon — `$var: NAME` is a YAML map, not a reference. An
undefined `$var:` is an error; there is no fallback, and values in `config/vars.yaml` can't chain
another `$var:`.
Plaintext in a `secret: true` key warns: config files are committed. A `${VAR}` must be defined
(process environment, then `.env`) when the files are generated, on every target — an undefined one
stops `up` rather than let compose put in an empty string. Give it a default with `${VAR:-dev}`, or
`${VAR:-}` for a value that may be empty. On Docker the reference is then left for compose to expand
at start; on K8s the CLI resolves it and `secret: true` values go into a generated Secret (a
`secretKeyRef` in the Deployment). A Secret already put into the cluster (by Vault, ESO, …) is
referenced with `existingSecret`; the platform never reads or writes its value, and never fetches from
Vault itself — any way of getting the value into the process environment works today.

**6. There are no resource bindings.** A database or cache is deployed by ops, and the component
reads it through its own config keys (`DB_HOST`, `DB_PASSWORD`, …). The database itself is created
by you, once; tables come from the component's migration.

**7. `up` never builds images.** Local-source components (and git ones with only
`deployment.build`) need `brickkit build [<id>]` first; while the image is missing `up` stops with
`IMAGE_MISSING`. Tags equal `metadata.version`, and an existing image of that version is skipped —
changed code without a version bump needs `--force`. A shell image built with stale member versions is
`IMAGE_STALE`. Git / market components with `image:` are pulled.

**8. Shells are chosen in the deploy file.** Nest member entries under the shell entry and they run
inside it (no own container; their `*_ENDPOINT` points at the shell; their own labels / health
check don't apply, while their `expose` is opened by the shell — a mapping on the shell's container on
Docker, an Ingress to the member's Service on K8s). The hosted version must be the one the shell's `component.yaml` compiles
in; otherwise `up` stops with three ways out: upgrade the shell to one that compiles that version;
move the member entry out of the shell to run on its own; or keep both — a `brickkit.yaml` line for the
compiled version with `requiredBy: [<shell>]`, `id@that-version` nested under the shell, the other
version left at the top level. `add` of a shell writes all of this for you. A member with
`mode: debug` / `local` leaves the shell and runs as a bare process. Callers keep using the member's
own service name: on Docker / Podman the shell's container carries each hosted member's service name as
a network alias, and on Kubernetes each member gets a Service that selects the shell's Pod — seed
scripts and tests address members exactly as before.

```yaml
components:
  - id: erp/shell
    members:
      - id: erp/api          # the default version of erp/api runs inside the shell
      - id: erp/auth@1.2.0   # a requiredBy version can be hosted too
```

Merging can create a Compose `depends_on` cycle (a member inside depends on X outside, X depends on
another member inside); the error names the edges and offers three ways out — move X into the shell
too, move a member out, or `skipWaitFor: [<id>]` on an entry, which only drops the start wait (the
connection stays; the component must retry until its dependency is up). `skipWaitFor` has no effect on
Kubernetes (Pods don't wait for each other) or for a bare process, and `up` warns when it is written
there. `brickkit up --ignore-shells --dry-run` checks that everything can still stand alone.

**9. `limits` has no default.** Only `requests` does (`100m` / `128Mi`). Recommended: CPU `requests`
with no ceiling (a CPU limit throttles into p99 spikes); memory requests = limits (Guaranteed QoS).
Write it inline: `resources: { requests: { cpu: 200m, memory: 256Mi }, limits: { memory: 256Mi } }`.
The entry's quotas override the component's recommendation field by field. Only the sum of `requests`
must fit a node; `limits` may overcommit. When idle runtimes eat the memory (a JVM's floor is hundreds
of MB), host them in a shell or run fewer of them — don't merge two components' code into one.

**10. A gateway hooks in through `labels`**, quoted string values (`"true"`), copied verbatim to
Docker service labels / K8s annotations and overriding the component's `deployment.labels` key by key.
`app`, `brickkit.io/*` and `com.docker.compose.*` are the platform's own keys and are rejected. A
gateway on Docker joins the `brickkit-<project>-net` network. Don't hand-write a file-provider config
full of versioned service names — it goes stale on every version bump.

**11. Signature verification needs `installer.publicKeys` in `brickkit.yaml`.** With none configured,
nothing is verified, whatever `requireSignature` says.

**12. `focus:` is the personal "run just this one" switch.**

`focus: <id>` exists only in `deploy.local.yaml` (`deploy.yaml` rejects it, and so does
`target: k8s`). While it is set, `up` starts only that component — from its local source, as if it
were `mode: local` (a `mode: debug` you wrote is kept) — and what it needs; pinned components
(`enabled` / `local` / `debug`) still start. `brickkit up` in a component's directory or
`up --focus <id>` writes it (turning local mode on if needed), `up --all` removes it; `-f` and
`--no-local` skip it; `sync` ignores it; `local refresh` lists it among your local changes. A focus
that would break the file is refused before anything is written.

## How the mechanism works

**Targets**: `target: docker | podman | k8s` in the deploy file. Podman runs the same generated
compose file through `podman compose`. K8s settings live in the `k8s:` block (`context`, `namespace`,
`createNamespace`, `podSecurity`, `imagePullSecrets`, `ingressClass`, `ingressAnnotations`,
`serviceAccount`, `networkPolicy`) and warn on other targets; per-entry `hostname`, `tlsSecret`,
`replicas` (`> 1` adds a PDB) and `serviceAccountName` are K8s only; `exposePort` and `skipWaitFor`
are docker / podman only.

**Addresses** are `http://<versioned-service-name>:<port>` on every target, so code never changes and
several versions coexist.

**Exposing**: `expose: true`. On K8s it needs a `hostname` (an Ingress is generated; `tlsSecret`
optional); on Docker the port is mapped to the host (`exposePort` changes the host port). Not written:
not exposed. `replicas > 1` on K8s adds a PodDisruptionBudget.

**Migrations**: on K8s a separate Job (not an init container, so several replicas never migrate at
once); on Docker a one-shot container. A failure keeps the main service from starting.

**`status` / `down`** read the same deploy file as `up` (and take `-f`); `down` never deletes volumes.

**What `up` does**: load the three layers → decide who starts → check images (missing → build hint,
git images pulled) → generate files (`--dry-run` stops here) → migrations (a failure blocks the main
service) → start the engine → supervise `mode: local` processes.

## Where to dig deeper

- The documentation of this BrickKit version, offline: `brickkit docs` lists the pages — the three files `brickkit docs 01-three-layers`, local debugging `brickkit docs 02-project-guide/03-local-debug-workflow`, shells `brickkit docs 04-shell`, every deploy field `brickkit docs 11-reference/03-deploy-yaml-schema`
- Flags: `brickkit up --help`, `brickkit local --help`, `brickkit build --help`, `brickkit lint --help`
  (`lint --strict` also checks that `${VAR}` and `file://` references resolve)
- A component's configuration guide: `.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md`
- The full specification: <https://github.com/brickKit/brickKit> and its root `AGENTS.md`
