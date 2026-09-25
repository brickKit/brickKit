# BrickKit · AI Reading Guide

English | [中文](AGENTS.zh.md)

> **This file is written for AI assistants.**
>
> If someone hands you `https://github.com/brickKit/brickKit` and asks you to understand or
> discuss this project, reading this one file is enough — it's a compressed version of the
> platform's **entire core philosophy and mechanics**.
>
> After reading it you should be able to: discuss any design question about this project in
> whatever language the user asked in, judge whether a proposal fits the platform's philosophy,
> and know exactly which document to pull for more detail (see §11 at the end).
>
> When discussing in English, use this document's English terms (component / required dependency /
> versioned service name / top-down inheritance …) — these are the platform-wide concept names,
> and they don't change just because the conversation is in English; the Chinese phrasing for these
> same terms lives in each `docs/zh/` article's own glossary.

> **Language routing: pick the document by the language the user is asking in, not by this
> file's own language.** This `AGENTS.md` and the Chinese `AGENTS.zh.md` are content-equivalent,
> each a complete, independent document — neither is a translation appendix of the other. If the
> user is asking in Chinese, read `AGENTS.zh.md` instead; if they're asking in English (or anything
> else), stay here. For deeper detail on a specific mechanism — English digs into `docs/en/`,
> Chinese digs into `docs/zh/`. The two trees are structurally identical; the path formula is
> `docs/{en,zh}/<same relative path>` (folder and file names start with a reading-order
> number, e.g. `06-architecture/00-overview.md`, identical in both trees). §11's table below
> gives English paths; swap `en` for `zh` to get the Chinese equivalent.

---

## 1. One-sentence positioning

**BrickKit is a declarative component assembly and management platform: declare the components and what they depend on, and it derives the rest. Build systems like snapping together bricks.**

Each brick (component) is manufactured, tested, deployed, and called independently. The BrickKit
CLI pulls the bricks, sorts out the order, generates the blueprints, and hands them off to Docker
or K8s to assemble. Everything else is the component's own business.

It is **not** an operating system, not an ERP, not any specific piece of business software. It's a
tool that lets you grow an architecture **incrementally**: write one small component and get it
running, write another and get it running, then write a connector component to wire them
together — and eventually assemble any system you need.

### 1.1 Analogies (for a quick mental map)

| BrickKit | Roughly equivalent to |
| --- | --- |
| BrickKit CLI | `npm` + `helm` + `docker compose` + `git clone`, but for **business components** |
| BrickKit Market (component marketplace) | npmjs.com / Docker Hub / App Store |
| Component | npm package / Docker image |
| `component.yaml` (Manifest) | `package.json` |
| `brickkit.yaml` (project config) | the "declarative input" side of `docker-compose.yaml` |
| `brickkit add` | `npm install` |
| `brickkit up` | `docker compose up -d` / `kubectl apply` |

**But there's one fundamental difference:** npm installs a code library; BrickKit installs a
**business service that can run on its own**. So it also has to manage dependency resolution,
deployment-file generation, address injection, database migrations, and startup ordering.

### 1.2 Five core values

| Value | What it means |
| --- | --- |
| Incremental construction | No need to design the whole system upfront — add one brick at a time |
| Language-agnostic | Any language that can build a Docker image can become a component |
| Environment consistency | Local (Docker) and production (K8s) use **the same address format**, zero component-code changes |
| Minimal platform | The platform stays out of the way: business logic, communication governance, and multi-tenancy are all the component's job |
| Open-source first | The CLI and the marketplace are both open source; closed-source components are distributed in a controlled way through the marketplace |

---

## 2. System composition and key architectural facts

### 2.1 Four parts

| Part | Form | Long-running? | Responsibility |
| --- | --- | --- | --- |
| **BrickKit CLI** | Local single binary | ❌ Runs and exits | Pulling, parsing, generating, invoking, publishing, source-workspace management |
| **BrickKit Market** | Independent SaaS (can be self-hosted) | ✅ | Component publishing/discovery, versions/visibility/signing, artifact storage |
| **Component layer** | Docker containers / K8s Pods | ✅ | The business logic itself, direct DNS calls between components |
| **Infrastructure layer** | PostgreSQL / Redis, etc. | ✅ | **Deployed manually by ops**, declared and bound in `brickkit.yaml` |

### 2.2 Five architectural facts that are easy to misread

These five are the key to understanding BrickKit. All of them are **deliberate trade-offs**, not
"not built yet":

1. **There is no long-running "main system service."** The CLI exits as soon as the command
   finishes. Desired state lives in `brickkit.yaml`; actual state lives in the underlying engine
   (Docker / K8s). No background process, no listening port, no attack surface.
2. **There's no self-built registry.** Service discovery = Docker Compose service DNS / K8s Service
   DNS.
3. **The platform doesn't poll for health.** Health checking is handled by K8s Probes / Compose
   healthchecks + restart policies.
4. **The CLI never mounts the Docker socket.** It only ever invokes the `docker compose` / `kubectl`
   command line — a clean permission boundary.
5. **Every component is a container.** Including frontend components — they're served by a web
   server container like nginx. There is **no** "container-less / static" component type on this
   platform.

### 2.3 Data flow (what happens during one `brickkit up`)

```
brickkit.yaml (declaration) + override.yaml (optional, local — merged in first, §7.1)
   ↓ ① Cascade decision: figure out which components should actually start this time (`mode` + dependency graph, top-down inheritance)
   ↓ ② Dependency resolution: recursively expand the dependency tree, error on missing required deps, topological sort gives the start order
   ↓ ③ Env-var injection: dependency addresses, resource connections, own config → environment variables
   ↓ ④ Deployment-file generation: docker-compose.yaml or K8s Deployment/Service/Ingress
   ↓ ⑤ Run migrations: a K8s Job or a one-shot Docker service; failure blocks the main service
   ↓ ⑥ Invoke the underlying engine: docker compose up -d / kubectl apply
Running containers
```

For a component from the marketplace or a Git source, the CLI's Manifest comes from the
`.brickkit/manifests/` cache and needs nothing under `components/`. A component that a **local**
source provides is different: `init`'s default `local-dev` points at `./components/`, so that
covers every component whose source you `--repo`-cloned or wrote there. Its `component.yaml` is
re-read from that directory on every run and never served from the cache — an edit shows up on the
next `up`, and a copy `sync` archived is still found. (Asking for a version other than the one the
directory holds is still answered from the cache; that is how multiple versions coexist.) Either
way `up` reads only the Manifest, never the component's code — the rest of the source directory
only serves development (IDE, debugging).

---

## 3. Glossary

| Term (Chinese) | English | Definition |
| --- | --- | --- |
| 主系统 | BrickKit CLI | The local command-line tool, no long-running process |
| 组件市场 | BrickKit Market | The public component publishing and discovery platform |
| 组件 | Component | The most basic install-and-run unit, **always a container** |
| Manifest | component.yaml | A component's self-description file |
| 项目配置 | brickkit.yaml | Project-level declaration: component list, enabled state, exposure, config overrides, resources, deploy target. Shared, reviewed, checked into Git |
| 本地覆盖配置 | override.yaml | Optional, gitignored, per-developer file that overrides `brickkit.yaml`'s `deploy.target` (downgrade-only) and a component's `mode`/`localPort`. `mode: debug` can **only** be set here — `brickkit.yaml` itself rejects it outright (§5.4, §5.6) |
| 强依赖 | Required Dependency | Missing → the CLI **errors and blocks startup** |
| 弱依赖 | Optional Dependency | `optional: true`; missing → warns but continues, and **the env var is not injected at all** |
| 版本化服务名 | Versioned Service Name | A service name carrying an exact version, e.g. `people-basic-1-0-0` |
| 本地调试模式 | Local Debug Mode | `mode: debug`, written in `override.yaml`; the component runs on the host inside an IDE, mapped into the container network via `extra_hosts` |
| 本地托管模式 | Local Managed Mode | `mode: local`, written in `brickkit.yaml`; BrickKit itself detects the start command, launches the component as a bare process, and supervises it — no container, no IDE required |
| 安装源 | Source | Where a component comes from: the marketplace (HTTP) / a Git repo / a local directory |
| 基础资源 | Resource | External systems a component depends on (databases, Redis, etc.), deployed by ops, bound in `brickkit.yaml` |
| 环境变量注入 | Env Injection | The CLI writes dependency addresses, resource connections, and own config into env vars when generating deployment files |
| 部署目标 | Deploy Target | `docker` or `k8s` in `brickkit.yaml`, decides which kind of deployment file the CLI generates. `override.yaml` may locally downgrade it (k8s → docker/podman; docker ↔ podman unrestricted; never upgrade back to k8s) — `podman` itself is an `override.yaml`-only value, `brickkit.yaml`'s own `deploy.target` never accepts it |
| 数据库迁移 | Migration | A component declares `migration.command`; the CLI runs it before deployment |
| 配置覆盖 | Config Override | `brickkit.yaml`'s `config` overrides the configSchema defaults. **Value types aren't validated, but key existence is** |
| 连接组件 | Connector Component | An orchestrating component that coordinates several standalone components |
| 单一组件 | Standalone Component | A component that completes one function on its own, internally transactionally self-consistent |
| 精确版本 | Exact Version | `major.minor.patch`; dependency declarations **do not accept** `^` / `~` range constraints |
| 跟着上层走 | Top-down Inheritance | Top-level components run by default; a lower one runs as long as any upstream component that needs it is running. Writing `mode` explicitly takes precedence (§5.4) |

---

## 4. Twelve design principles (the philosophical core)

Use these twelve to judge whether any design proposal actually belongs to BrickKit:

| Principle | Explanation |
| --- | --- |
| **Platform minimalism** | The CLI doesn't do anything it doesn't have to; every extra feature is extra maintenance cost forever |
| **Component autonomy** | Language, framework, API, transactions, log format are all up to the component; the platform only requires the Manifest + a health check + env vars |
| **Incremental** | You never have to design the whole system upfront; you can stop and resume at any point |
| **Environment-agnostic** | Component code never knows whether it's running under Docker or K8s |
| **Explicit over implicit** | Exact versions, explicit exposure, explicit enabling; reject anything "auto-guessed" that becomes uncontrollable |
| **Secure by default** | No port mapping = not externally reachable; no `expose` declared = no Ingress; `private` = invisible without authorization |
| **Open source first** | The CLI and marketplace are both open source; closed-source components are distributed through the marketplace in a controlled way |
| **The platform provides tools, it doesn't make decisions for you** | Multiple versions coexist by default; degradation logic belongs to the component; breaking changes are an organizational coordination problem |
| **Install implies trust** | The platform does no upfront security review, only after-the-fact `blocked` delisting |
| **configSchema is a spec sheet, not a security gate** | The CLI doesn't validate the **values** a user fills into `config` (type / enum / range). But it does check **key names** — a config key not in configSchema triggers a warning |
| **One component, one repository** | No monorepo sub-directories supported; a component is an independent unit of publishing / moving / permissions |
| **`brickkit.yaml` is the declaration** | Config is intent. Write it and it executes — the CLI never asks "are you sure?" |

> The argument behind each principle — what it buys, what it costs, what it refused — is in
> [Design principles and trade-offs](docs/en/06-architecture/01-design-principles.md) (swap `en` for
> `zh` for the Chinese version). Ignoring their 1–12 numbering, its twelve section headings
> match this table's first column word for word; `make lint` fails if they drift or the
> numbering slips.

### 4.1 What the platform explicitly refuses to do (the rejection list)

This list is "platform minimalism" made concrete. **When proposing something to a user, never
suggest BrickKit do anything on this list** — every one of these has already been explicitly
argued through and rejected (reasoning in §9):

| Won't do | Alternative |
| --- | --- |
| Long-running services / control plane | The CLI runs and exits; state lives externally in `brickkit.yaml` + the underlying engine |
| Registry / address book | Docker DNS / K8s Service DNS |
| Health-check polling | K8s Probes / Compose healthcheck + restart policy |
| API gateway / service mesh / load balancing | Components call each other via DNS directly; K8s Service does native load balancing. **An out-of-band gateway hooks in via `labels` passthrough** — the platform passes labels through without interpreting the routing |
| Config center / dynamic hot reload | Env-var injection; change config, then `brickkit up` restarts |
| Communication governance (circuit breaking / rate limiting / retries) | The component's own code |
| Weak-dependency degradation logic | The component's own business logic |
| Multi-tenancy | The component's own business |
| Version-range resolution (`^1.0.0`) | Only exact versions are accepted |
| Multi-environment overlay / inheritance merging | Each environment gets its own complete, self-contained `brickkit.yaml` |
| Config value type validation | configSchema is just a spec sheet |
| Third-party component security review | Install implies trust + after-the-fact `blocked` |
| Monorepo sub-directory components | One component, one Git repository |
| Consolidated deployment / monolithic shell — the platform does not, and will not, ship its own shell scaffolding or process supervisor | But a small, structural piece **is** built in: `servedBy` lets a component declare "my workload is provided by another component," and the platform correctly wires `*_ENDPOINT` addresses to it on both Docker and K8s — without ever needing to understand what's inside the shell. See §5.7 below for the full boundary |
| Dependency aliases (`dependencies.components[].as`) | Variable names derived from component ID are bidirectionally computable; an alias only preserves half of that. "One capability, multiple implementations" should go through a `kind` resource or a `configSchema` address field instead |
| Low-code / BI / DevOps pipelines | Out of scope |
| Automatically verifying a machine's Podman environment can tear down cleanly, or running that verification in CI | `override.yaml`'s `target` field (§7.1) runs a real `engine.Engine` implementation for Podman — `up`, `down`, and `status` all work once the host prerequisite is met. The platform never probes for that prerequisite itself (verify it yourself with `scripts/podman/check-environment.sh`); a `down` that hits the one known failure signature still gets a translated hint pointing back at the [environment checklist](docs/en/07-patterns/11-podman-environment-checklist.md) instead of a bare `permission denied`. See §5.10 below for the full picture, including what's still explicitly not done (portability beyond the one verified environment, automated real-lifecycle CI) |
| Fetching secrets from an external store (Vault / AWS Secrets Manager SDKs) on the platform's behalf | `${VAR}` is looked up in the process environment first, `.env` second — anything that can put the value in the environment works today with zero platform code. Built in, it would mean an SDK per store, store credentials and network access on every `up` (`--dry-run` included), and a neighbour of the rejected "config center". **What is supported:** `resources[].existingSecret` and a `secret: true` config value written as `{ existingSecret, key }` reference a Secret an external system (Vault Secrets Operator, External Secrets Operator, Sealed Secrets, …) already put in the cluster — the platform never reads or writes the value either way, K8s only (§5.2) |
| Engine plugins / third-party deploy targets (an `--engine nomad`-style flag on `up`) | A target's `Down`/`Status`/orphan-pruning guarantees are what make "a project that can be torn down" true; a plugin would own them while the CLI reported success on its behalf — the same reason Podman was pulled. `deploy.target` in `brickkit.yaml` stays the declaration, never a CLI flag. New targets are built in-tree, with the full test guard set. (`engine.Engine` is already an interface; this is about who guarantees its semantics, not about code layout) |
| Incremental generation cache (`.brickkit/` hash state) | Nothing to speed up: generating 50 components through the whole pipeline takes about 2 ms (`tests/perf`), and the time users wait on is `docker compose up` / `kubectl apply`, which already touch only what changed. A cache adds state whose staleness silently produces wrong deployment files |
| Mock generation from contracts (a full `mock` command) and auto-substituting a missing required dependency (an `--with-mocks`-style flag on `up`) | The platform never parses contracts (`artifacts.format` is a free string); a stand-in swapped in for a missing required dependency contradicts "missing required dependency blocks startup" and could be deployed by mistake; a mock under another name receives no traffic because injected addresses point at the real component's versioned service name. What works today: `brickkit new <id> --contract openapi` + `mode: debug` in `override.yaml` + any mock tool (`docs/en/03-guide/08-consuming-artifacts.md`) |
| A renderer of its own for `brickkit graph` — HTML / SVG output, a built-in viewer, a flag that writes the file for you | Mermaid text is already rendered for free: GitHub renders a `.mmd` / `.mermaid` file, or a Markdown code fence tagged `mermaid`, with nothing installed. A renderer inside the CLI would be a permanent maintenance cost (layout, one more output format to keep correct) for something that costs nothing today. And stdout carrying nothing but Mermaid is what makes the shell redirect `brickkit graph > graph.mmd` produce a valid file — so no file-writing flag is needed either |
| Dependency resolution and cross-file reference checks in `brickkit lint` (does the `servedBy` target exist? can that dependency be found?) | `lint` is a promise — offline, read-only, instant, no Docker or K8s — and it adds no rule of its own: it re-runs the parse-and-validate that `up` / `add` / `publish` already apply to each file. Resolving the dependency graph needs every component's Manifest, which for a market or Git component means the network; one network call and the promise is gone. **`brickkit up --dry-run` already does this** — it has to resolve the graph anyway, and it errors, naming the culprit, on a missing `servedBy` target or a required dependency it can't find. `brickkit graph` shows the declared structure |

---

## 5. Core mechanisms (how it actually works)

### 5.1 Versioned service names and unified addressing

**Service name = transformed component ID + exact version.** Transform rule: `/` → `-`, `.` → `-`,
all lowercase.

| Component ID | Version | Service name |
| --- | --- | --- |
| `people/basic` | 1.0.0 | `people-basic-1-0-0` |
| `erp/backend` | 2.1.3 | `erp-backend-2-1-3` |

The address format is **exactly the same** in both environments: `http://<versioned-service-name>:<port>`
(locally `http://people-basic-1-0-0:8080`; the exact same string on K8s).

This one rule has two direct consequences: **multiple versions coexist for free** (`people-basic-1-0-0`
and `people-basic-2-0-0` are two non-conflicting DNS names), and **a caller always knows exactly
which version it's talking to** — there is no implicit upgrade.

⚠️ **Coexisting versions is a "project-level" capability, not a "component-level" one.**
`brickkit.yaml` can list two version entries side by side (for different callers to each use their
own), but **within a single `component.yaml`'s `dependencies`, one component ID can only appear
once** — the dependency address's variable name is based on the component ID and carries no
version, so writing two versions would collide on the same `*_ENDPOINT`, with the latter silently
overwriting the former. The CLI errors on this when parsing the Manifest. Diamond
dependencies (A depends on X@1, B depends on X@2) are unaffected — each gets its own.

### 5.2 Environment-variable injection specification

**Core rule: the env var *name* carries no version (it's based on the component ID); the *value*
carries the version (it points at a specific service).**

```bash
DEPARTMENT_TREE_ENDPOINT=http://department-tree-1-0-0:8080
```

| Category | Naming rule | Example |
| --- | --- | --- |
| A dependency component's main port | `{component-ID-prefix}_ENDPOINT` (`/` and `-` → `_`, all uppercase) | `people/basic` → `PEOPLE_BASIC_ENDPOINT` |
| A dependency component's extra port | `{component-ID-prefix}_{NAME-uppercase}_ENDPOINT` | `PEOPLE_BASIC_GRPC_ENDPOINT` |
| Platform-wide variables | Fixed | `COMPONENT_ID`, `COMPONENT_VERSION` |
| Resource connections | Prefixed by resource type (the `kind` name is the prefix) | `DATABASE_*`, `REDIS_*`, `MQ_*`, `STORAGE_*`, `SEARCH_*`, `SMTP_*` |
| Own config | configSchema camelCase key → uppercase snake_case | `defaultPageSize` → `DEFAULT_PAGE_SIZE` |

**Reserved-variable protection (two layers of defense):** `COMPONENT_ID`, `COMPONENT_VERSION`,
`BRICKKIT_SERVED_MEMBERS`, `BRICKKIT_SERVED_MEMBERS_CONFIG` (exact match), `*_ENDPOINT` (suffix match), `DATABASE_*` / `REDIS_*` /
`MQ_*` / `STORAGE_*` / `SEARCH_*` / `SMTP_*` / `{envPrefix}_*` (prefix match). A configSchema key, once uppercased, must
not collide with these — **the marketplace refuses this at publish time**, and **the CLI warns and
skips that config item at injection time** (the platform-injected value wins).

**A third, distinct failure mode: `configSchema.required` naming a key with no `default` blocks
`up` outright** — this is different from both a reserved-variable collision
(warns, skips) and an ordinary unset optional key (silently not injected, the component's own
"unconfigured" branch runs). A required key with no default means "the platform genuinely cannot
guess this — the project has to supply it" (the standard shape for a cross-project service address,
since the platform has no way to derive where another project's service lives).
Missing it isn't a crash and isn't a warning — the variable simply never exists — so leaving this as
a silent skip would mean the component runs, looks healthy, and has one call path that quietly never
works. `brickkit up` errors instead, naming the exact missing item and which component declared it
required.

**Secrets take a different road from ordinary values.** Resource passwords (`DATABASE_PASSWORD`, …)
are always secrets; a component's own config item is a secret only if its `configSchema` says
`secret: true` (the platform never guesses from the name). On `deploy.target: k8s` a secret goes into
a generated `Secret` (file mode 0600) and the Deployment holds a `secretKeyRef`; everything else is a
plain `env` value. On Docker, `${VAR}` in `config` and `resources[].password` is **never** resolved by
the CLI when it writes `docker-compose.yaml` — `docker compose` resolves it at start (process
environment first, `.env` second). `brickkit.yaml` only ever holds the reference. There is no
"fetch from Vault" built in and there won't be (§4.1): anything that can put the value in the process
environment works — and for a Secret an external system (Vault Secrets Operator, External Secrets
Operator, Sealed Secrets, …) already created in the cluster, `resources[].existingSecret` and a
`secret: true` config value's `{ existingSecret, key }` form reference it directly, K8s only; the
platform never reads or writes the value either way. Details: [Secrets](docs/en/07-patterns/10-secrets.md).

The table above states the naming *shape*; the full dictionary — every resource `kind`'s exact
variable names, real generated examples for each warning and the one hard error, and how `servedBy`
merges a member's config onto its shell — is
[Environment Variable Contract](docs/en/06-architecture/04-environment-variables.md).

### 5.3 Required vs. optional dependencies

| | Declaration | CLI behavior when missing |
| --- | --- | --- |
| Required | `- department/tree@1.0.0` | **Errors and blocks startup** |
| Optional | `- id: infra/redis@1.0.0` + `optional: true` | Warns but continues, **the env var is not injected at all** |

⚠️ **"Not injected at all" is not the same as "injected as an empty string."** This is one of the
most commonly misunderstood parts of BrickKit's design. Component code must read env vars
defensively (Python's `os.environ.get()`, Java's `System.getenv()`); using `os.environ["X"]` throws
a `KeyError` and crashes the component immediately — **this is deliberate**, see §9.13 for why.

Degradation logic (does the component query the database, return an empty list, or write to a
local file for later retry when Redis is down) is the component's own business code; the platform
doesn't manage it.

### 5.4 `mode`: top-down inheritance

> **Top-level components run by default; a lower-level component runs as long as any upstream
> component that's running still needs it; writing `mode` explicitly overrides all of that.**

| Value | Meaning | Behavior |
| --- | --- | --- |
| **not written** (no `mode` field) | **Follows the top** | A top-level component (nothing depends on it) runs by default; a lower one follows whatever's above it |
| `mode: enabled` | **Always runs** | Ignores what's above it. If its **required** dependencies are turned off, it **errors** (two conflicting intents) |
| `mode: disable` | **Never runs** | Whatever depends on it stops too (unless something has it pinned — `mode: enabled`, `mode: debug`, or `mode: local` — which then errors) |
| `mode: debug` | **Always runs, as a process you start yourself** | Pinned exactly like `mode: enabled` (ignores what's above it; errors if a required dependency is turned off), but generates no container — you run it on the host, in your IDE (§5.6). Docker only |
| `mode: local` | **Always runs, as a process BrickKit starts and supervises itself** | Pinned exactly like `mode: enabled` (ignores what's above it; errors if a required dependency is turned off), but generates no container — BrickKit detects the start command from the component's own source, launches it, and supervises it (§5.6). Docker only |

⚠️ **`mode: debug` can only be written in `override.yaml` — `brickkit.yaml` itself rejects it
outright, at parse time.** Every other value in this table (not written / `enabled` / `disable` /
`local`) is written directly in `brickkit.yaml` as always. The reason is what `mode: debug` *is*: a
personal "I'm running this one on my own machine right now" fact, never something a teammate
reviewing `brickkit.yaml` should have to see or be affected by — exactly the kind of thing
`override.yaml` exists for (optional, gitignored, per-developer; §5.6). `mode: local` stays in
`brickkit.yaml` because it isn't personal — the process still runs and is still reachable the same
way for anyone who runs `up`.

**Required and optional dependencies are treated the same way here:** if something upstream weakly
depends on it, it still follows along and runs. `optional: true` only controls two things — a
missing optional dependency only warns (doesn't block) at resolution time, and its `*_ENDPOINT` is
not injected while it isn't running.

A few points:

- When shared by multiple upstream components, it runs as long as at least one of them is
  running — a shared lower-level component is never accidentally taken down
- When two components depend on each other (only possible via a weak-dependency cycle), there's
  nothing above the cycle, so both are top-level and both run
- The implementation computes "**who doesn't run**" (a least fixed point), so cycles need no
  special-casing at all
- Every line of CLI output carries its reason: `starting (top-level)` / `starting (mode: enabled)`
  / `starting (mode: debug)` / `starting (X needs it)` (`mode: local` shares `mode: debug`'s exact
  wording — the reason names *why* it's pinned, not which of the two bare-process modes it is)

**The only way to narrow the startup scope is to change `mode`.** There's no `--only`-style
flag — set the top-level things you don't want on `mode: disable`, and both `up` and `sync` follow
suit; to restore full scope, `git checkout brickkit.yaml` (or, if the `mode: disable` was written in
`override.yaml` instead, just delete that line — `override.yaml` isn't in Git, so there's nothing to
check out).

Components added automatically by `brickkit add` **do not get** a `mode` field written.

### 5.5 Deployment-file generation and database migrations

A component's repository **never ships** any environment-specific deployment file. The CLI reads
the unified `component.yaml` and dynamically generates `docker-compose.yaml` or K8s
`Deployment/Service/Ingress` according to `deploy.target`. Switching environments only means
changing one field:

```yaml
deploy:
  target: k8s        # was docker
```

**Migrations:** a component declares `migration.command`, and the CLI runs it before the main
service starts — K8s generates an independent **Job** (not an InitContainer, see §9.6 for why) and
`kubectl wait`s on it; Docker uses a one-shot service. **A failed migration blocks the main service
from starting.** On K8s, the CLI first runs `kubectl delete job --ignore-not-found` to clean up any
leftover old Job, guaranteeing idempotency.

**Exposure:** not exposed by default. With `expose: true`, K8s generates an Ingress (`hostname`
required); Docker maps the port to the host (customizable via `exposePort`; the CLI errors on port
conflicts).

### 5.6 Bare processes: local debugging (`mode: debug`) and local execution (`mode: local`)

Both values mean "this component runs as a plain OS process on your own machine, not in a
container, and everything else still reaches it correctly." They split on *who starts it*:
`mode: debug` is for when **you** start it yourself — in an IDE, breakpoints and all — and
BrickKit only routes to it; `mode: local` is for when you just want it running, hands-off —
BrickKit detects the start command, launches the process, and supervises it. Same shape, opposite
ownership of the startup step.

**They also split on which file declares them.** `mode: local` is written directly in
`brickkit.yaml`, like any other field. `mode: debug` can **only** ever be written in
`override.yaml` (§3) — `brickkit.yaml` rejects it outright, at parse time, unconditionally,
regardless of `deploy.target`. The reason is what the value itself represents: "I'm debugging this
one on my own machine right now" is a fact about *this developer, this moment* — the opposite of
something a teammate reviewing a `brickkit.yaml` diff should ever have to see, be blocked by, or
accidentally inherit from a `git pull`. `override.yaml` is optional, gitignored, and per-developer
for exactly this reason. `brickkit override` generates/refreshes it from `brickkit.yaml`'s current
component list — every component gets a bare `- id: <id>` line, and `mode: debug` (plus
`localPort`) is added by hand on top of that.

**`mode: debug`:** to debug a component with breakpoints in an IDE, while it's still reachable by
other components on the Docker network:

- A component marked `mode: debug` in `override.yaml` **doesn't generate a container**
- Other containers resolve that component's versioned service name to `host-gateway` via
  `extra_hosts`
- Multiple components can be debugged locally at once, each with its own `localPort`; the CLI
  injects the matching port automatically
- The CLI generates `local-debug.env` for the IDE to load
- **Zero component-code changes** (it reads env vars exactly as it normally would)
- It is **pinned** like `mode: enabled` (§5.4): it keeps running whatever is above it, and turning
  off one of its **required** dependencies is an error rather than a silent choice
- **Docker only**: `mode: debug` together with an *effective* `deploy.target: k8s` is rejected when
  `override.yaml` is checked against `brickkit.yaml` (so `brickkit lint` catches it too) — a cluster
  Pod has no route to a process on your own machine. Since `override.yaml` can only ever *downgrade*
  `deploy.target` (k8s → docker/podman, never the reverse — §3's glossary entry), the effective
  target can only be k8s when `brickkit.yaml` itself already says k8s and `override.yaml` doesn't
  touch `target` at all
- Values written into that file are POSIX-shell-quoted whenever they contain a character a shell
  would otherwise misparse (whitespace, `|`, `$`, an embedded literal newline, …) — a multi-line
  PEM value or a `|`-delimited list survives `set -a && source … && set +a` intact instead of
  getting cut off at its first newline or blowing up with `command not found`. A plain value with
  none of those characters is left unquoted
- The other half of that same guarantee: a `${VAR}` config value is looked up from the project
  root's `.env` file (`internal/cli/up_k8s.go`'s `envLookup` — the lookup function shared by the K8s
  renderer and local-debug generation; the Docker compose file itself is evaluated by `docker
  compose`, in that same order — process env first, `.env` second) using the same
  quoted/multi-line convention real `docker compose` itself uses for that file (double-quoted
  values support `\n`/`\r`/`\t`/`\"`/`\\` escapes and may span physical lines; single-quoted values
  are kept fully literal and may also span lines) — not a naive line-by-line `KEY=value` split. This
  is the same lookup the K8s renderer uses for a plain (non-secret) env value, so a multi-line
  `.env` value lands intact in a generated Deployment's `env` list too, not just in
  `local-debug.env`
- What it does **not** rewrite: a config value that's an opaque string literal pointing at
  something outside brickKit's own dependency graph (an out-of-band container's address, say,
  written assuming a container network — `http://host.docker.internal:8000`). brickKit doesn't
  parse config string contents, so switching that component to `mode: debug` doesn't retarget the
  literal to a host-reachable address — the developer has to edit it themselves (typically to
  `localhost`)
- A dependency that's a `servedBy` member (§5.7) has no compose service of its own — its host-port
  mapping gets opened on its **shell's** compose service instead, using the member's own declared
  port (the same port the shell is already required to listen on: `checkPortConflicts` in
  `internal/shell/shell.go` validates this at generation time, and it's the same assumption the K8s
  renderer already relies on to route `*_ENDPOINT` addresses into a shell — nothing new is being
  assumed about shell internals here). Both the dependency's main-port and extra-port `*_ENDPOINT`
  variables in `local-debug.<service>.env` resolve to a real `http://localhost:<port>`, exactly like
  an ordinary dependency. Before this was fixed, the extra-port variable didn't even fail loudly: it
  silently guessed a `localhost:<port>` value that looked entirely plausible and had nothing
  listening behind it (brickKit feedback: local component addresses go wrong when depending on a
  servedBy member)

**`mode: local`:** the "just run it for me" half of the same idea —

- A component marked `mode: local` **doesn't generate a container**, same as `mode: debug`
- BrickKit reads the component's own source directory (`go.mod`, `package.json`, `pom.xml`, …) and
  works out how to start it on its own — nothing in `component.yaml` declares this command; each
  language ecosystem has its own marker file BrickKit recognizes
- BrickKit launches the process itself and supervises it for the lifetime of that `brickkit up`
  run: `brickkit up` stays in the foreground, streaming the process's own output with its service
  name prefixed on every line, exactly like `docker compose up` without `-d`
- **`localPort` is optional** — by default BrickKit picks a free port for it (preferring the
  component's own declared `deployment.port`); write `localPort` only to pin a specific one
- A small session-lock file under `.brickkit/` is the only thing that lets a *second* terminal
  recognize a local session is running: `status` excludes `mode: local` components from its
  container table but doesn't report them as down either; `graph` marks them with their own label
  and color, pinned and never greyed out; `down` cannot reach across into another terminal's
  process tree, so it prints the same session hint instead of silently doing nothing
- Stopping it is `Ctrl+C` in the terminal running `up` — a clean stop prints nothing extra (no
  crash summary for an exit that was asked for); an unexpected crash prints one, with the exit
  reason and the process's own recent output, controlled by `--crash-lines` (default 20 lines,
  `0` = the exit reason alone)
- The process tree is scoped to that one terminal session on purpose — it never tries to outlive
  the `up` that started it, unlike a container
- **Docker only**, pinned like `mode: enabled` (§5.4) — same rules, same rejection under
  `deploy.target: k8s`, for the same reason: a cluster Pod has no route to a process on your own
  machine
- Hands-on walkthrough with real output: [Run a component locally, hands-off](docs/en/03-guide/04-local-execution.md)

### 5.7 Consolidated deployment (`servedBy`)

A component can declare `servedBy: <shell-component-id>@<version>` to mean
"my workload is provided by that other component" — the shell it names is an
ordinary component, with its own image, port, and health check. The platform:

- Skips generating a workload (container/Deployment) and a migration
  container/Job for the `servedBy` component.
- Still computes a correct `*_ENDPOINT` for anything depending on it — the
  address points at the shell's actual location, using the `servedBy`
  component's own declared port.
- Merges `*_ENDPOINT`-class variables into the shell's shared OS
  environment unprefixed (component IDs already make these names unique),
  and merges each member's own `configSchema`-derived config values in
  too — but always under a component-ID prefix
  (`{EnvPrefix(componentId)}_{name}`, the same prefix `*_ENDPOINT`
  variables use), so two independently-authored modules reusing the same
  config key can never collide. Never merges `COMPONENT_ID`/
  `COMPONENT_VERSION`, resource-connection variables, or (see below)
  `labels` on a member's behalf — those stay the shell's own, single set
  of platform/resource variables. `BRICKKIT_SERVED_MEMBERS_CONFIG` (two
  bullets down) is an index into the prefixed config variables, not a
  second copy of their values.
- Treats a `servedBy` member's own resource bindings (`dependencies.resources`
  in its `component.yaml`) as satisfied once the shell's own componentId is
  bound to the same `kind`+`engine` resource — the member doesn't also need
  its own (redundant, and functionally inert) entry in that resource's
  `bindings`. The real connection env vars only ever land in the shell's
  container regardless of which componentId the binding is written under, so
  requiring a second copy would only exist to satisfy the checker, not to
  produce anything real.
- Writes `BRICKKIT_SERVED_MEMBERS` (a reserved variable) into the shell's
  environment: a comma-separated list of the versioned service names
  currently part of the deployment. A compliant shell may read it to skip
  initializing (including skipping migrations and resource connections for)
  any compiled-in module not on the list — this is optional; the platform
  never checks whether a shell actually honors it.
- Also writes `BRICKKIT_SERVED_MEMBERS_CONFIG` (a reserved variable): a JSON
  array, one element per member on that same list, each carrying
  `componentId`/`version`/`httpPort`/`extraPorts`/`configEnvVars`
  (`configEnvVars` maps that member's own original configSchema key to the
  name of the prefixed environment variable above where its actual value
  lives — not the value itself). This is the index a shell author actually
  needs to wire each module up — without it, the only option was
  hand-computing an equivalent JSON blob before every deployment and
  pasting it into a string field, which goes stale the moment a member's
  version, config, or `servedBy` membership changes and only surfaces as a
  crash-loop at the next real startup. Carrying only variable names (never
  values) is deliberate, not incidental: a member's config value is often a
  `${VAR}` reference to a secret that brickKit's Docker Compose generation
  deliberately leaves unresolved (so the generated file stays safe to open
  and diff) — embedding such a placeholder's eventual value inside a JSON
  string would let `docker compose`'s own blind, structure-unaware
  variable substitution corrupt that JSON the moment the value contains a
  quote, backslash, or newline. Empty deployments still get `[]`, not a
  missing variable.

`mode: debug` is untouched by this — same meaning, same code
paths as ever; `servedBy` is a wholly separate, independent mechanism that
happens to share the same "in the dependency graph but generates no workload"
shape.

**What a `servedBy` component's own `expose`/`exposePort`/`hostname`/
`replicas`/`resources`/`serviceAccountName`/`labels` do:** nothing — the
platform warns, it doesn't error and doesn't silently ignore. These fields
describe "how my own container/Pod is deployed," and a `servedBy` component
has none. `labels` used to be the one exception (merged from every member,
same collision rule as `*_ENDPOINT` vars) until real multi-component testing
showed that rule was unworkable: labels whose value is legitimately supposed
to differ per component (`prometheus.io/port` being the everyday case — its
value is each component's own port number) triggered "same key, different
value" collisions on essentially every real merge. Carving out an exceptions
list for "keys known to vary by nature" would mean the platform interpreting
specific label semantics, which directly contradicts labels' whole reason
for existing (§9.22: the platform stays deliberately ignorant of what any
label key means) — so `labels` was moved into this same-as-`expose` bucket
instead of growing that list.

Full field-level detail, the validation rules, and what a shell implementation
itself must get right: [Building a qualified shell](docs/en/07-patterns/07-shell-implementers-guide.md).
For whoever is deciding whether and how to declare `servedBy` on their own project:
[Declaring servedBy: a deployment checklist](docs/en/07-patterns/06-servedby-deployment-checklist.md).
For deciding a whole project's deployment shape in the first place — topology
(independent / shell-merged / mixed) × `docker`/`k8s`, the `mode: debug`
toggle, and where running components by hand fits in — start one level up:
[Choosing a deployment shape](docs/en/07-patterns/05-deployment-selection-guide.md).

### 5.8 Component source workspace

| Command | Behavior |
| --- | --- |
| `brickkit add <id>@<ver> --repo` | Additionally clones that component's full Git repo into `components/` (open-source components only) |
| `brickkit add <id>@<ver> --repo-all` | Clones every open-source repo in the recursive dependency tree (closed-source ones are skipped with a note) |
| `brickkit sync` | Bidirectionally archives / activates based on the cascade decision: what's not starting moves to `components/.archived/`, what needs to run moves back |
| `brickkit remove <id>` | Automatically deletes the corresponding source directory, **including an already-archived copy** |

`brickkit add` **doesn't clone source by default** (only pulls the Manifest + artifacts).
`brickkit sync` is a **separate command**, deliberately not folded into `brickkit up` (see §9.17 for
why). The CLI **doesn't manage Git permissions** — forking, remotes, pushing are all the user's own
business.

**For projects that remove `components/` from `.gitignore`** (so component source travels with the
project in version control), `sync`'s whole-directory moves land in the project's diff — the
pre-commit hook installed by `brickkit restore` and `brickkit init --hooks` exists specifically to
catch the recurring mistake of "an archive-state change got committed but `mode` didn't come
along with it".

A hands-on walkthrough of all of this with real output — cloning, pushing changes back, archiving,
the guards on `remove`, `restore` and the hook — is
[Manage component source](docs/en/03-guide/09-component-source.md).

### 5.9 Marketplace, signing, and the trust model

The marketplace is an independent public platform — **it is not a component and doesn't need to be
installed**. It only answers two questions: **what's available to install? who's allowed to install
it?** It doesn't install components, run components, or manage running state.

**There is no BrickKit-operated public instance today.** `sources[].type: market`, `brickkit login`,
`add`, `publish` and `fetch` all need a market URL because none is built into the CLI — point them
at an instance you or your organization self-hosts (below), or one a team/vendor already runs for
you.

| Capability | Description |
| --- | --- |
| Publish / discover | Upload the Manifest + image reference (+ signature); search, tags, namespace filtering |
| Version management | Exact version list + state: `draft` / `stable` / `deprecated` / `blocked` |
| Visibility | `public` / `private` |
| Signing | The publisher signs with **cosign**; **the installer verifies with the Go standard library** (no need to install cosign); enforced in production |
| Artifact storage | Open source: registers the Git repo URL; closed source: the marketplace stores the Manifest + image reference |

**Trust model: install implies trust.** The platform does no upfront security review (no scanning,
no sandboxing, no static analysis) — it only handles the last line of defense: when a component is
confirmed malicious, it gets marked `blocked`, blocking new installs. In one sentence: **the
platform provides a "marketplace," not a "vault."**

Authentication: `brickkit login` prompts interactively in the terminal for credentials; the token is
stored in `.brickkit/credentials`.

Running the marketplace itself (as opposed to using one someone else runs) is a separate deployment
of its own, covered in [Self-hosting the BrickKit Market](docs/en/07-patterns/09-deployment/self-hosted-market.md).

---

### 5.10 Podman as a deploy engine

`override.yaml`'s `target: podman` (§7.1) is a real, working deploy engine — not just a
validated-but-inert config value. Once set, `up`, `down`, and `status` all run against Podman
instead of Docker, using the exact same generated `docker-compose.yaml` `podman compose` already
consumes identically to Docker — **as long as `podman compose` resolves to the `docker-compose`
binary as its external provider**, which is what this was verified against; the environment
checklist below has the one-line check and what a different provider (`podman-compose`, the
separate Python implementation) means for BrickKit.

**The main prerequisite:** rootless Podman's network-teardown helper, `pasta`, needs a `SIGTERM`
from `podman` to tear a deployment's network down cleanly on `down`. Most Ubuntu/Debian installs'
default AppArmor policy blocks that signal — a confirmed distro packaging gap
([containers/podman#27372](https://github.com/containers/podman/issues/27372)), not a BrickKit or
Podman bug. [The environment checklist](docs/en/07-patterns/11-podman-environment-checklist.md)
has the diagnostic (`scripts/podman/check-environment.sh`), the fix
(`scripts/podman/fix-apparmor.sh`), and the compose-provider check above.

**What the CLI does and doesn't do about this:** it never probes for that prerequisite itself —
doing so would mean guessing at environment fitness instead of the project explicitly declaring
it (§4's "explicit over implicit"). What it does do: if a real `down`/`up` hits that exact known
failure signature, the error's hint points back at the checklist instead of leaving a bare
`permission denied` for the user to puzzle over.

**What's still explicitly out of scope:** any portability guarantee beyond the one environment
this was verified on (other distributions, other AppArmor configurations), and any automated
real-container-lifecycle CI check — this repository's test suite verifies the Podman engine the
same way it verifies Docker's: unit tests against a fake runner, not a real container.

---

## 6. `component.yaml` (Manifest) field skeleton

```yaml
apiVersion: brickkit/v1
kind: Component

metadata:
  id: <scope>/<name>             # required, the component's unique identifier
  name: <display name>           # required
  version: <major.minor.patch>   # required, exact version
  description: <description>     # required
  vendor: <publisher>            # optional
  license: <license>             # optional
  apiDocs: <API docs URL>        # optional

tags: [<tag>]                     # optional, used for marketplace search

artifacts:                       # optional, artifacts shipped with the component (API contract / SDK / docs)
  - type: api-contract           # required, free-form string
    format: protobuf             # optional, free-form string
    description: <description>   # optional
    files: [<path relative to repo root>]   # required

dependencies:                    # optional
  components:
    - department/tree@1.0.0                # required dependency (exact version)
    - id: infra/redis-event-bus@1.0.0      # optional dependency
      optional: true
  resources:
    - kind: database             # database / cache / mq / storage / search / smtp
      engine: postgresql

configSchema:                    # optional, the "spec sheet" for its own config (values aren't type-checked, but key names must match)
  type: object                   # a config item name must not collide with a reserved variable (see §5.2)
  properties:
    defaultPageSize:
      type: integer              # string | integer | number | boolean | array | object
      default: 20
      description: <description>
      enum: [...]                # optional — enum, items, minimum, maximum, pattern are
      minimum: 1                 #   documentation only: parsed and stored, never enforced
      maximum: 100               #   (minimum/maximum are numbers, pattern is a string,
      pattern: <regex>           #   items is `{ type: <type> }` on array properties)
      secret: true               # optional — a credential: on K8s its value goes through a generated
                                 #   Secret (secretKeyRef), never plaintext env. Not validated (§5.2)
  required: [<required items>]

deployment:                      # required
  type: container                # fixed as container (frontend components too)
  image: <image reference>       # required
  port: 8080                     # required, main port (health check + _ENDPOINT var)
  extraPorts:                    # optional, e.g. gRPC
    - name: grpc
      port: 9090
  resources:                     # optional, **recommended values**, the CLI passes them through without validation
    requests: { cpu: "100m", memory: "128Mi" }   # recommend writing only requests
  # limits are better left to the deployer: quotas merge field-by-field, and a component
  # that writes limits.cpu can never have it removed by the project config
  labels:                        # optional, deployment metadata passthrough, the platform doesn't interpret key/value
    prometheus.io/scrape: "true" # value must be a string — don't drop the quotes
    prometheus.io/port: "9090"   # Docker → service labels; K8s → Pod annotations

migration:                       # optional
  command: ["<command>", "<args>"]   # array form
  # ⚠️ the migration container and the main container are the same image — the entrypoint
  # must fail fast (non-zero exit) on any argument it doesn't recognize, never fall through
  # to "start the service" (see the callout below)

healthCheck:                     # required
  type: http                     # http | tcp | none
  path: /healthz                 # required for http
  startPeriodSeconds: 60         # optional, startup grace period (seconds), default 60
  # ⚠️ only checks that this process itself is alive; never check the database / a dependency component / any external system
```

> **`startPeriodSeconds` is the only overridable timing parameter under `healthCheck`.**
> `interval` / `timeout` / `failureThreshold` are fixed by the platform (10s / 3s / 3); their product
> is only 30 seconds, so every component gets a startup grace period of **60 seconds** by default,
> and this field overrides it. A component whose cold start outlasts 60 seconds (a heavy Spring Boot /
> Django / .NET app) has to raise it, or `up` fails under Docker and the Pod permanently
> CrashLoopBackOffs under K8s, while the container's own logs look perfectly healthy the whole time.
> The grace period only delays "declaring it dead," never "declaring it alive," so setting it
> generously costs nothing.

> **A component's entrypoint must fail fast on an argument it doesn't recognize** — this is a
> hard requirement on the component author, not something the platform can
> enforce. The migration container and the main service container run from the exact same image,
> distinguished only by the command-line argument the platform passes. If the entrypoint's dispatch
> logic falls through to "start the service" on an unrecognized argument (a typo in
> `migration.command`, say) instead of erroring, the migration container silently becomes a second
> service container: it never exits, the main service waits forever for
> `service_completed_successfully`, and the whole deployment hangs at `Created` — while that
> container's own logs still say the component is ready, which is exactly what makes this so
> misleading to debug. Validate the argument **before** reading environment variables or opening a
> database connection, too — otherwise a typo'd argument surfaces as a misleading "failed to connect
> to the database" instead of the actual problem.

> **That's the complete field list.** The Manifest **has no extension-field mechanism** — an
> unrecognized key is rejected on the spot, not silently ignored — so the
> skeleton above must be copy-pasteable exactly as-is. There used to be two "reserved" fields,
> `observability` and `compatibility.minCliVersion`; both were removed: neither was
> ever read anywhere, and the second was worse — it looked like a safety gate, but a component
> declaring `minCliVersion: 2.0.0` would install just fine on CLI 0.1.0.

**Resource-quota priority chain:** `brickkit.yaml`'s `resources` > `component.yaml`'s `resources` >
the CLI's own default.

⚠️ **Only `requests` has a default (`100m` / `128Mi`); `limits` has none.** If neither is written,
**no `limits` is generated at all** — a quota ceiling is a business judgment call, and the platform
guessing a number risks OOMKilling a perfectly healthy component (it genuinely needs 600Mi, and
512Mi was the platform's invented figure). The recommended pattern is the **opposite**: set a CPU
`requests` and **no ceiling** (a CPU limit runs through the CFS quota, which throttles into p99
spikes even when the node is idle); for memory, **requests = limits** (this earns Guaranteed QoS,
so it's the last thing evicted when memory runs short).

**How many components fit:** the only hard constraint is "the sum of every Pod's `requests` on a
node ≤ that node's allocatable capacity" — the sum of `limits` can far exceed capacity
(overcommitting is normal usage). 20 components at default requests add up to only 2 cores / 2.5G.
The real cost is each process's **memory floor**, which is almost entirely decided by language: Go
8–20MB, Python/Node 40–90MB, JVM 200–450MB — 20 idle Spring Boot instances alone eat 4–9G. **Don't
merge components just to save memory** — that's solving the wrong problem (switch runtime instead,
or use `mode: disable` to run fewer of them).

**⚠️ Health-check prohibition:** `/healthz` only checks that this process itself is alive. Checking a
database or a dependency component inside a health check causes production cascading failures — one
downstream hiccup gets every upstream marked unhealthy and restarted at once.

**⚠️ A component with a cold start longer than 60 seconds must raise `startPeriodSeconds`.**
`interval` / `timeout` / `failureThreshold` are fixed by the platform (10s / 3s / 3); their product is
only 30 seconds — too short for anything slow to start — so the platform gives every component a
**60-second startup grace period** by default, and most components never need to touch it. Exceed
60 seconds: under Docker, it's marked `unhealthy`, failing `up -d --wait` and stalling any dependent
waiting on `service_healthy`; under K8s, the startup probe gives up, the Pod gets killed and
restarted, runs through the same 60 seconds again → **permanent CrashLoopBackOff**, while the
container's own logs look completely normal the whole time. A heavy Spring Boot app, a Django
project that preloads a lot, or .NET's first JIT pass can reach that. The grace period only delays
"declaring it dead," never "declaring it alive" (a component that's ready in two seconds still turns
healthy in two seconds), so setting it generously costs nothing.

This is the skeleton — every field's exact type, required-ness, default, and validation constraint
(the port ranges, the regexes, which fields silently do nothing without another field set) is
[07-component-yaml-reference.md](docs/en/06-architecture/07-component-yaml-reference.md).

---

## 7. `brickkit.yaml` (project config) field skeleton

```yaml
project: my-shop                 # required, used for the K8s namespace and Docker network naming

deploy:
  target: docker                 # required: docker | k8s
  # ↓ the following only apply to K8s
  context: <kubeconfig context>   # optional, pin which cluster to deploy to
  namespace: <namespace>          # optional, defaults to brickkit-<project name>
  createNamespace: true          # optional, set false if you only have namespace-level permissions
  podSecurity: restricted        # optional, only "restricted" is supported today
  imagePullSecrets: [<secret>]   # optional
  ingressClass: <class name>
  ingressAnnotations: { <key>: <value> }
  serviceAccount: { enabled: true }        # one SA per component, no mounted token — opt-in (see below)
  networkPolicy:                           # generates network policies from the dependency graph
    enabled: true
    ingressController: { namespace: <ns>, podSelector: {<key>: <value>} }
    allowFrom: [ { name: <who this is for>, namespace: <ns>, podSelector: {...}, ports: [...] } ]
    egress:
      enabled: true
      allowTo: [ { name: <who this is for>, resource: "<resources[].id>" } ]

sources:                         # install sources
  - id: <source ID>
    type: market                 # market | git | local
    url: <API URL or Git URL>    # required for market/git
    path: <local path>           # required for local
    authToken: ${ENV_VAR}        # optional; prefers .brickkit/credentials if already logged in
    ref: <branch / tag / commit> # optional, git sources only; defaults to the default branch (pin a tag in production)
    enabled: true

components:
  - id: people/basic
    version: 1.0.0               # required, exact version
    mode: local                  # optional here: enabled | disable | local (§5.4). mode: debug is
                                  #   NOT legal in this file — it can only be set in override.yaml (§7.1)
    localPort: 8081              # host port when mode: local (optional — auto-assigned by default).
                                  #   mode: debug's localPort lives in override.yaml too, next to its mode
    servedBy: <id>@<version>     # optional, this component's workload is provided by that other component
    expose: false                # optional, default false
    hostname: <domain>           # required when expose + k8s
    exposePort: 8080             # optional, Docker only
    tlsSecret: <Secret name>     # optional, K8s + expose only, the Ingress's TLS cert
    replicas: 3                  # optional, K8s only, default 1; >1 auto-generates a PDB
    serviceAccountName: <SA name> # optional, K8s only; use an SA ops already created, the platform only references it, never creates one
    config:                      # optional, overrides configSchema defaults (values aren't type-checked; a wrong key name warns)
      defaultPageSize: 50
    resources:                   # optional, overrides the component's recommended quota
      requests: { cpu: "200m", memory: "256Mi" }
      limits:   { cpu: "1", memory: "1Gi" }
    labels:                      # optional, deployment metadata passthrough, overrides the component's deployment.labels key by key
      traefik.enable: "true"     # the platform doesn't interpret key/value; value must be a string

resources:                       # base-resource declarations and bindings (the resource itself is deployed by ops)
  - kind: database
    engine: postgresql
    id: main-db
    host: <host>
    port: 5432
    username: <username>
    password: ${DB_PASSWORD}     # must be referenced via an env var
    existingSecret: <K8s Secret name>  # optional, K8s only, mutually exclusive with password —
                                       #   reference a Secret already created by ops/Vault Secrets Operator/ESO instead
    bindings:
      - componentId: people/basic
        # ↓ the following four are **the same slot** (which spot this component occupies in the
        #   resource) — use the one matching `kind`, only one is allowed. A wrong name errors and
        #   names the right one to use
        database: people         # kind: database → DATABASE_NAME (the user creates the database itself, once)
      # vhost: orders            # kind: mq      → MQ_VHOST
      # bucket: media-prod       # kind: storage → STORAGE_BUCKET
      # index: products          # kind: search  → SEARCH_INDEX
      #                            kind: cache / smtp have no such slot; writing one errors
        envPrefix: <prefix>      # optional, disambiguates env vars when there are multiple resources of the same kind

installer:
  requireSignature: true         # optional, default true
  publicKeys:                    # publisher public keys the project trusts: publicKeyRef → public-key file path
    keys/vendor.pub: keys/vendor.pub
```

> ⚠️ **`serviceAccount.enabled` is opt-in — skip it, and every Pod runs under the namespace's
> `default` ServiceAccount, whose token is auto-mounted as normal**. This is easy
> to misread against §4's "secure by default" principle: that principle covers what a component
> can reach (no dependency edge, no resource binding, no exposure — all opt-in on the *other* side),
> not this specific K8s default. The platform doesn't flip this on for you for the same reason it
> doesn't force `podSecurity: restricted` on by default — either one can stop an already-working
> component from starting, and that's a real, project-specific cost the platform isn't in a position
> to weigh on your behalf. Write `serviceAccount: { enabled: true }` deliberately if a component
> genuinely has no business ever calling the Kubernetes API.

> ⚠️ **`publicKeys` is the only field that actually makes signature verification take effect.** With
> zero public keys configured, signature verification **is disabled entirely**, and
> `requireSignature: true` does nothing either — there's no trust anchor to check against
> — there's no trust anchor to check against. The CLI warns once about this, but by then it hasn't verified anything.
>
> Public keys have to be configured here, rather than pulled alongside the signature from the
> marketplace — otherwise the marketplace would be issuing its own certificates to itself, and if
> the marketplace were ever compromised, an attacker could swap out both the component and the
> public key together, and verification would still pass.

**Multiple environments:** each environment gets its own **fully self-contained** `brickkit.yaml`
(e.g. `brickkit.prod.yaml`), selected with `brickkit up --config brickkit.prod.yaml`. **There's no
overlay / inheritance / merge mechanism** (see §9.9 for why).

This is the skeleton — every field's exact type, required-ness, default, and validation constraint
(every `mode`/`servedBy`/`replicas` mutual exclusion, the binding-slot rules, which fields only take
effect under one deploy target and warn when written under the other) is
[08-brickkit-yaml-reference.md](docs/en/06-architecture/08-brickkit-yaml-reference.md).

### 7.1 `override.yaml` field skeleton

An optional, **gitignored**, per-developer file that sits alongside `brickkit.yaml` and locally
overrides two things: `deploy.target` (downgrade-only) and a component's `mode`/`localPort`. Never
merged or overlaid field-by-field with `brickkit.yaml` — a topic is either untouched here (defer
entirely to `brickkit.yaml`) or written here (fully replaces `brickkit.yaml`'s value for that one
topic). `brickkit override` creates it on first run and refreshes it on every later run — that's
also its reset/repair operation, there's no separate second command.

```yaml
target: podman                # optional: docker | podman | k8s — must be a DOWNGRADE from
                               # brickkit.yaml's own deploy.target: k8s → docker/podman is allowed,
                               # the reverse is always rejected, docker ↔ podman is unrestricted.
                               # Omit it to just follow brickkit.yaml's own deploy.target
targetBaseline: docker        # deploy.target's value when this override was last confirmed —
                               # feeds the non-blocking drift note below, never itself enforced

components:                   # brickkit override writes one line per component ID in
                               # brickkit.yaml — coexisting versions of the same ID share one line,
                               # since there are NO version numbers anywhere in this file (an
                               # override applies to every version of that ID uniformly)
  - id: department/tree       # standalone component, no override — bare line

  - id: erp/backend           # a shell — itself a plain component, no override here either
    members:                  # servedBy members nested under their shell, one level only
      - id: people/basic         # servedBy member, no override — bare line
      - id: auth/rbac
        mode: disable            # excluded from this run's BRICKKIT_SERVED_MEMBERS while the
                                  # shell keeps running — not guaranteed (AGENTS.md §5.7)

  - id: infra/redis-event-bus
    mode: local
    localPort: 8082            # this machine's port 8080 (brickkit.yaml's suggested default)
                                # was already taken — overridden here instead

  - id: payment/gateway
    mode: debug                 # the ONLY file mode: debug can ever be written in (§5.4, §5.6)
    localPort: 9091
    baseline: local              # brickkit.yaml's own mode for this component when this override
                                   # was last confirmed — feeds the drift note, never enforced
```

- `mode: debug` together with an effective `deploy.target: k8s` is rejected (only reachable when
  `brickkit.yaml` itself is already k8s and `target` is left unset here — a downgrade-only override
  can never produce k8s any other way)
- A `localPort` with no `mode` at all, or an out-of-range one, is rejected the same as it would be
  in `brickkit.yaml`
- An entry naming a component ID that doesn't exist in `brickkit.yaml` (a dangling reference — the
  component was removed, or the ID was mistyped) is rejected
- Drift is content-based, never timestamp-based — `baseline`/`targetBaseline` are compared against
  `brickkit.yaml`'s *current* values every run, and a mismatch prints a note (never blocks): the
  override might still be exactly what's wanted, or it might be stale and worth a second look
- `brickkit up` (and `sync`/`status`/`down`) apply it automatically when present; **only for a run
  against the default `brickkit.yaml`** — `--config brickkit.prod.yaml` ignores any `override.yaml`
  present, with a printed note, so a personal local override can never leak into a named-environment
  run
- `brickkit graph` deliberately never reads it — its output is a shareable, committed artifact
  (`brickkit graph > graph.mmd`) that must not vary by who generated it locally
- `brickkit add`/`remove`/`lint`/`restore` also integrate with it (§8's command table has each
  one's exact behavior) — all four ignore it entirely, the same way `up` does, when `--config`
  points anywhere other than the default `brickkit.yaml`

This is the skeleton — the full field-by-field shape lives in `schemas/override.schema.json`
(generated, checked in, same mechanism as `brickkit.yaml`'s own schema).

---

## 8. The CLI command set (17 commands + `version` + `lang`)

| Command | Core behavior |
| --- | --- |
| `brickkit init <name>` | Generates a `brickkit.yaml` skeleton and a `.brickkit/` directory, and installs the AI assistant skills (`--no-skills` to skip) |
| `brickkit skills` | View/refresh the AI assistant skills installed in the project (`status` / `update`). In a standalone component repo (a `component.yaml`, no `brickkit.yaml`) it manages just the `brickkit-component` skill. Never overwrites something hand-edited; never touches the user's own `CLAUDE.md` |
| `brickkit graph` | Prints the project's dependency topology as Mermaid text on stdout: solid edges for required dependencies, dashed for optional (an optional one that can't be found is drawn as a "not installed" node), greyed-out nodes for components that won't start this run, `mode: local` components marked with their own "managed locally" label and color, `servedBy` members grouped inside their shell. **Nothing but Mermaid on stdout**, so `brickkit graph > graph.mmd` writes a file GitHub renders. It reads the same resolved graph as `brickkit up --dry-run` (so it needs the network for market/Git Manifests not yet cached), and generates no deployment files and touches no engine. `--ignore-served-by` draws every component standalone |
| `brickkit lint` | **Offline, read-only** structure check of the YAML in the current directory — no network, no Docker/K8s. In a project: `brickkit.yaml`, then `override.yaml` if present (§7.1 — a dangling entry errors, a stale baseline warns, `--strict` escalates the warning; skipped, with a note, when `--config` points elsewhere), then every `component.yaml` under the `local` install sources (whether or not they've been added; `.archived/` is skipped). In a standalone component repo (a `component.yaml`, no `brickkit.yaml`): just that file. Reports required fields, types, unknown keys (typos), version format, port ranges, plus two kinds of warning — a misspelled key inside a `configSchema` property (it won't take effect) and a config key that collides with a reserved variable. Adds almost no rule of its own (the `override.yaml` staleness check is the one exception); exit `1` on errors (`LINT_FAILED`), warnings alone exit `0`, `--strict` makes them fail too (a CI gate). **Does not** resolve dependencies or check `servedBy` targets exist — both need the resolved dependency graph, which `lint` deliberately never builds (that can mean a network call for a market or Git-sourced component) — that's `up --dry-run` |
| `brickkit new <scope>/<name>` | Generates a component's minimal skeleton — a `component.yaml` that already passes validation, plus (with `--contract openapi\|proto`) a placeholder contract file registered under `artifacts`. Writes to `components/<scope>/<name>/` by default (the same layout a `local` source scans); `--path` writes elsewhere with no nesting, for a standalone component repository. No Dockerfile, no source code — the platform doesn't pick a language for you, and it never runs `add` on your behalf |
| `brickkit add <id>[@ver]` | Recursively pulls dependencies, downloads artifacts, writes them into the config (**doesn't write a `mode` field**). If no version is given, takes the latest installable version from the source and pins it to disk as an **exact version**. If `override.yaml` exists (§7.1) with only bare-id defaults, refreshes it to include the new component; with real overrides, leaves it untouched and prints a reminder instead. Ignores `override.yaml` entirely when `--config` points elsewhere |
| `brickkit remove <id>` | Checks for required-dependency callers before removing, automatically deletes the source directory (including an archived copy). Must specify a version when multiple versions coexist. When the removed version is the component's last one, also deletes its `override.yaml` line (§7.1) if present — a removed `servedBy` shell's nested members are promoted to top-level entries, not deleted. Ignores `override.yaml` entirely when `--config` points elsewhere |
| `brickkit fetch <id>[@version]` | Only downloads the component's artifacts into `.brickkit/artifacts/<versioned-service-name>/`, **doesn't write to `brickkit.yaml`, doesn't deploy**. Used when calling another project's service across project boundaries |
| `brickkit up` | Apply `override.yaml` (if present, and only for the default `brickkit.yaml` — §7.1, printing any drift notes along the way) → cascade decision → generate deployment files → generate `local-debug.env` → check image permissions → run migrations → invoke the engine → launch and supervise any `mode: local` components in the foreground (§5.6) |
| `brickkit down` | Applies `override.yaml`'s `deploy.target` downgrade first (§7.1), then stops all containers under the effective target. **Doesn't delete volumes, data is preserved.** Cannot reach a `mode: local` process running in another terminal — prints a hint naming that session's PID instead |
| `brickkit status` | Applies `override.yaml` (§7.1), reads the underlying engine, shows a running-state table (including multi-version detection; components not running are listed too). A component not running because of a `mode: disable` set in `override.yaml` is labeled `(override.yaml)`, distinct from one disabled in `brickkit.yaml` itself. Excludes `mode: local` components from that table (they're not containers) but prints a hint when one is running elsewhere |
| `brickkit sync` | Applies `override.yaml` (§7.1), then bidirectionally archives / activates component source based on the cascade decision. Takes no arguments |
| `brickkit override` | Creates `override.yaml` on first run, refreshes it on every later run (also its own reset/repair operation — §7.1). Every component in `brickkit.yaml` gets a line; existing customizations (`mode`/`localPort`/`baseline`) are preserved, a component removed from `brickkit.yaml` loses its line, a new one gets a bare `- id:`. Refuses to run against a non-default `--config`. Prints any drift notes (§7.1) after writing |
| `brickkit restore` | Restores `mode` and the component-source layout to the last commit. `--check` is for the pre-commit hook to judge whether this commit is self-consistent. If `override.yaml` exists (§7.1) and a `mode` actually changed, prints any resulting drift notes — the same non-blocking check `up`/`lint` run, not a generic "may be stale" claim |
| `brickkit login` | Interactive terminal login to the marketplace, token stored in `.brickkit/credentials` |
| `brickkit logout` | Revokes the marketplace token server-side, then deletes `.brickkit/credentials` locally. The local deletion always happens, even if the marketplace is unreachable — otherwise a network blip leaves someone believing they've logged out while the credential still sits on disk. Doing nothing when already logged out is not a failure |
| `brickkit publish` | Uploads the Manifest + image reference + artifacts to the marketplace (requires login first) |

Two more commands sit outside those 17 because they are about the CLI itself, not your project: `brickkit version`, and `brickkit lang` — which shows or changes the language the CLI speaks. **The CLI is English by default.** The language is `BRICKKIT_LANG` (env var) if set, else what `brickkit lang set en|zh` saved in the per-user config file, else English; there is deliberately no `--lang` flag (the language must be known before the command tree — `--help` included — is built). Everything a person reads follows it, including the `message` of the JSON log lines and the comments in generated files; the `error_code`, command and flag names never do.

**Common flags:**

```bash
brickkit up --config brickkit.prod.yaml           # multi-environment
brickkit up --dry-run                             # only generate deployment files, for review
brickkit up --context prod-cluster                # override deploy.context for this one run (k8s only)
brickkit up --ignore-served-by --dry-run          # verify every component can still stand alone without servedBy
brickkit up --crash-lines 0                       # mode: local crash summary prints the exit reason only, no output lines (default: 20 lines)
brickkit graph > graph.mmd                        # dependency topology as Mermaid text (GitHub renders a .mmd file)
brickkit graph --ignore-served-by                 # draw every component standalone, as if no servedBy were declared
brickkit lint                                     # offline structure check of brickkit.yaml + the local sources' component.yaml files
brickkit lint --strict                            # warnings fail too (exit 1) — for a CI gate
brickkit override                                 # create/refresh override.yaml from brickkit.yaml's current component list
brickkit down --context prod-cluster              # same override, for tearing down a specific cluster
brickkit add people/basic@1.1.0 --yes             # non-interactive (CI/CD)
brickkit add --local                              # add every component in a local source at once
brickkit add erp/backend@1.0.0 --repo-all         # clone the source of every open-source dependency
brickkit fetch infra/notifier@1.0.0               # fetch artifacts only (cross-project calls, not installed into the project)
brickkit remove people/basic@1.0.0                # specify the version to remove when multiple versions coexist
brickkit remove people/basic@1.0.0 --force        # delete the source directory even with uncommitted or unpushed changes
brickkit login --market https://market.example.com/api/v1   # only needed with more than one market source configured
brickkit logout                                   # revoke the market token and delete local credentials
brickkit logout --keep-remote                     # delete local credentials only, without calling the market (offline)
brickkit publish --path ./components/people/basic --market https://market.example.com/api/v1 --visibility private --changelog "added X"
brickkit publish --path ./components/people/basic --git-url https://github.com/org/people-basic --sign --key cosign.key --signed-by release-bot@example.com --public-key-ref keys/vendor.pub
brickkit version --verbose                        # also print the git commit hash and build time
brickkit lang set zh                              # the CLI speaks Chinese from now on (BRICKKIT_LANG=en overrides it per command)
brickkit up --log-level info                      # every command has this flag: default is warn (quiet); info brings back the routine per-command lifecycle lines
BRICKKIT_LOG_LEVEL=debug brickkit up              # env var sets a more verbose default for a whole shell session instead of typing the flag every time
```

### 8.1 A one-minute example

```bash
brickkit init my-shop                 # create the project
brickkit add erp/backend@1.0.0        # one command pulls the entire dependency tree
brickkit up --dry-run                 # see the start order (topological sort)
brickkit up                           # generate deployment files → run migrations → start containers
```

---

## 9. Twenty-three "whys" (architectural defenses)

This section is the most valuable part of BrickKit's philosophy. Each entry is a defense of one
design that **looks counter-intuitive at first glance**. When a user asks "why doesn't it …", the
answer is almost always here.

**9.1 Why no registry, and no health-check polling?**
Building your own registry means the platform has to be a long-running, highly-available cluster —
a huge jump in operational cost — while Docker/K8s's native DNS and Probes are already good enough,
and respond faster than "poll `/healthz` every 10 seconds." Components are also therefore
**zero-intrusion** — they don't need any registration SDK at all.

**9.2 Why force exact versions, rejecting `^1.0.0`?**
Range versions are the classic culprit behind "works on my machine, breaks in production." Exact
versions also let the version number be baked straight into the service name, making version
coexistence a zero-cost, natural capability.
⚠️ Note the distinction: `brickkit add people/basic` (omitting the version) **is allowed** — the CLI
asks a specific version at the moment of `add` and pins the exact version into the config, exactly
like npm writing a lockfile. What's rejected is **writing a range constraint in the config**
(`brickkit add people/basic@^1.0.0` still errors). The difference is **when resolution happens**:
once, versus every time.

**9.3 Why do multiple versions coexist by default, instead of erroring?**
Versioned service names mean coexistence needs no extra machinery at all. And if `brickkit.yaml`
lists two version entries, that's the user's intent — the CLI doesn't need to ask "are you sure?"
again. Breaking changes are an **organizational coordination problem**; version coexistence is just
a physical buffer, and data-layer compatibility is the user's own responsibility.

**9.4 Why don't components ship their own deployment files?**
One Manifest, two environments. A component developer doesn't have to maintain both a
`docker-compose.yaml` and a full set of K8s manifests. A component only describes "what I am, what
I need" — it doesn't care "where I run."

**9.5 Why is the CLI a run-and-exit local tool, not a long-running server?**
No background process = no single point of failure, no resource footprint, no listening port, no
attack surface. `brickkit.yaml` is the single source of truth, and slots perfectly into Git for
version control and code review. If a visual console is ever needed, it should be an ordinary
frontend component + API component, not something hard-coded into the platform core.

**9.6 Why does K8s migration use a Job instead of an InitContainer?**
An InitContainer is **Pod-scoped**. With `replicas: 3`, three InitContainers would run the same
migration script **concurrently**, which very easily deadlocks or corrupts the schema. A Job is
**cluster-scoped**, and the CLI serializes it, guaranteeing it runs exactly once across the whole
cluster.

**9.7 Why no communication governance or weak-dependency degradation?**
Circuit-breaker thresholds, rate-limiting algorithms, and retry backoff strategies all vary by
business — a platform-wide "one size fits all" is both hard to get right and limits flexibility.
Degradation is pure business logic anyway: if Redis goes down, component A wants to query the
database, component B wants to return an empty list, component C wants to write to a local file and
retry later — the platform has no way to universally define what "degradation" means.

**9.8 Why no config center (dynamic hot reload)?**
A config center means long-lived connections, config pushes, version comparisons, and a whole heavy
mechanism. And for 90% of basic config (connection-pool size, timeouts), a restart is already the
only safe way to make it take effect. If a business toggle genuinely needs millisecond-level hot
updates, the component can just poll Redis itself.

**9.9 Why doesn't multi-environment support introduce overlay inheritance?**
Overlays require a developer to understand "base layer / override layer / merge rules / array-merge
strategy" — you can't see the full picture by opening one file, you have to mentally reconstruct the
inheritance chain. A self-contained, complete file is what-you-see-is-what-you-get; a Git diff is
immediately legible, and a change to a base layer can never **implicitly** affect production.

**9.10 Why are frontend and backend components indistinguishable to the platform?**
A frontend component equally needs to listen on a port, provide a health check, be exposed via
Ingress, and get its backend address through env vars. Inventing a special "static" type would fill
the CLI with `if type == static` branches, and the architecture would start to crack.

**9.11 Why doesn't the platform do third-party component security review?**
Same as the VSCode extension marketplace, npm, or GitHub — using it means you trust it. Open-source
component code is fully transparent; closed-source components rest on a commercial agreement. An
upfront review either has an extremely high false-positive rate (blocking legitimate components) or
an extremely high false-negative rate (letting carefully disguised malicious code through).
`blocked` is a low-cost, sufficiently effective last line of defense.

**9.12 Why doesn't the CLI validate config value types?**
Once you open that door, you have to keep asking: validate `enum`? `minimum`? `pattern`? `required`?
JSON Schema's capability is enormous, and the CLI would keep bloating. And it's component
autonomy — the component decides for itself how to handle a bad config value (error, degrade, fall
back to a default) — the platform shouldn't overstep. **The spec sheet has already been handed to
you; if you don't read it or misread it, the platform doesn't bail you out.**

⚠️ **But this doesn't extend to "key names" — the CLI warns there.** The line is drawn at **whether
there's a runtime safety net**: get the type wrong, and the component gets `"abc"` and crashes trying
`int()` on it — you'll definitely notice. Get a key name wrong, and **there is no runtime failure at
all** — the variable simply never appears, the component falls into `os.environ.get(k, default)`'s
default branch, and runs completely normally, just not the way you configured it. So `brickkit up`
says "a config item won't take effect," and guesses which one you meant (`greetting` →
`greeting`?). If the component **has no `configSchema` declared at all** but the project writes
`config` anyway, the whole block doesn't take effect, and that also warns.

This also isn't on the same slippery slope as above: `type` / `enum` / `minimum` are all **constraints**
from JSON Schema — open that door and you have to keep going; whereas "does this key exist in
`properties`" is a single existence check with no follow-up — the same reasoning as the Manifest
rejecting unknown fields.

**9.13 Why does a missing weak dependency inject nothing at all, instead of an empty string?**
This is the entry that most fully embodies BrickKit's philosophy. The most dangerous thing about
injecting an empty string is that it **manufactures a silent failure**: a developer forgets to check
for empty, writes `requests.get(f"{ENDPOINT}/healthz")`, and the empty string just concatenates into
`/healthz` — the request hits the container's **own** port 8080, and its own `/healthz` returns
200 — the developer then mistakenly believes the weak dependency is healthy. This class of bug is
extremely hard to track down. **Better to let the component "crash loudly" at startup than "fail
quietly" at runtime.**

**9.14 Why is `mode` one field, and why is its rule phrased as "follows the top"?**
`mode` used to be two switches — `enabled` (unwritten / `true` / `false`) and `local: true` — and
nothing stopped them from disagreeing: `enabled: false` next to `local: true` says "never run this"
and "I'm running this myself" at once. One field whose value names the **role a component plays this
run** makes that combination impossible to write: unwritten follows the top, `enabled` and `debug` pin
it running (one in a container, one as a process you start yourself), `disable` pins it off. The rule
itself is phrased as a **user-first** inheritance model rather than the **implementation-first**
derivation it once was ("skip it if nothing enabled needs it"). The two map one-to-one onto each other
case by case, but only the former is actually readable: the decision a user has to make is just "do I
want this top-level thing" — everything below it follows along, no need to compute it yourself. This wasn't wordsmithing for its own sake — the original phrasing genuinely misled people,
and reading it carefully would lead you to the wrong conclusion that "this rule must be broken." One
substantive rule changed alongside it: what counts as a "dependency" in the decision switched from
"required dependencies only" to "required and optional treated the same" — otherwise a weak
dependency that `add` wrote into the config wouldn't start by default, and you'd install a component
only to find half its functionality mute.

**9.15 Why doesn't it fall apart at 50 components?**
① **Locality principle**: component A only needs to know the API contract of its direct
dependencies B, C, D — what's behind B is none of A's business. A single developer's cognitive
boundary is always "my direct dependencies" (usually 1–3). ② **On-demand activation**: a 50-component
project might only have 4 containers running locally in local development; the other 46 "don't
exist." ③ **The CLI encapsulates transitive dependencies**: one `add` command and the whole subgraph
is there. ④ **`brickkit sync`** keeps the source directory down to just the 2–3 things you're
actually looking at right now. **That's the essence of "building with bricks" — you never need to
look at every brick at once.**

**9.16 Why no monorepo support?**
A component is an independent **publishing unit** (its own version — how would you even tag a
monorepo?), an independent **moving unit** (`sync`'s archive move is the whole repo directory,
`.git` and all), and an independent **permission unit** (its own visibility and publisher). Note:
multiple parts of the same piece of business (proto + backend code + migration scripts) belong to
**the same component** — they don't need to be split apart.

**9.17 Why isn't `brickkit sync` folded into `brickkit up`?**
Separation of concerns: `up` manages runtime, `sync` manages the source directory. And `up` never
needs a component's code, only its Manifest (§2.3: cached for marketplace/Git components, re-read
from the source directory for locally-provided ones — `sync`-archived copies included). If `up` moved
files around automatically, users would be confused about why their files suddenly moved.

**9.18 Why doesn't `add --repo` clone all the source automatically?**
Most users only want to **use** a component, not **modify** it. A single `add` can recursively pull
in 5–10 dependencies; cloning all of them is slow and eats disk space. **Source is a "development
time" need, not an "install time" one.**

**9.19 Why doesn't the CLI manage Git permissions after cloning?**
Users might be on GitHub / GitLab / Gitee / a self-hosted Gitea — the CLI can't possibly support
every platform's fork API. Forking, remotes, branching strategy, PR flow are all part of the Git
workflow, and none of BrickKit's business.

**9.20 Why does `brickkit remove` automatically delete the source directory?**
`remove`'s semantics are "remove completely." Leaving the source behind creates a "zombie
directory" — not in `brickkit.yaml`, but the files are still there. Worse, the archive directory
starts with a `.` (hidden by default in file managers), so once a component is removed, `sync` no
longer recognizes it — even harder to spot than an ordinary zombie directory. Got uncommitted
changes? Commit them before removing — the platform provides tools, it doesn't make that call for
you.

**9.21 Why doesn't the platform offer a full consolidated-deployment command, when it does offer
`servedBy`?**
The need is real: a JVM component's memory floor is 200–450MB, so 20 of them is 4–9G, and that
genuinely might not fit during a private on-prem delivery. The platform has **already done the
hardest half of this for free** — a caller only reads `*_ENDPOINT`; whether the other end is 10
containers, 1 container, or 10 modules inside one JVM is something it has no way to know. `servedBy`
(§5.7) closes the one gap that was actually the platform's own — correctly routing addresses to a
merged unit without borrowing `mode: debug` and without the K8s-side gap that had no equivalent at
all. Everything past that (avoid port collisions inside the merged process, take over health checks
and migrations, isolate each module's config) still lives entirely in the shell author's own code —
not one more line of platform code is needed there, and the platform still never has to understand
which things *can* be merged, what supervisor manages them, or how any given framework starts
multiple listeners. Building a full `--consolidated` command would still mean starting to understand
"the shell" in exactly the way this platform argues against — `servedBy` is
deliberately the smallest structural piece that helps, not a step toward that larger, rejected
command.

**9.22 The platform doesn't do gateways — so why add `labels` passthrough?**
Because it lets the platform **keep not understanding gateways**. The standard way tools like
Traefik / Prometheus hook in is reading container labels. Not doing routing is the right call, but
if the platform **also** doesn't offer a passthrough, "not doing routing" turns into "also not
letting you do it yourself" — the user is forced back to hand-writing a file-provider config, which
has to be full of **versioned service names** (`erp-sales-1-0-0`) — and that mapping silently expires
every time the component bumps a version, **with the platform saying not a word about it**. The
passthrough moves this perishable mapping back into the place that already has to be edited on every
version bump. What the platform has to understand to add it is **exactly the same as before**:
nothing. It's just a `map[string]string`. This is the same posture as `deployment.resources`
("passed through, not validated") and `deploy.ingressAnnotations` ("passed through verbatim").
**What should be rejected is a semantic-layer custom field, not a deployment-layer passthrough** —
the former requires the platform to grow understanding it doesn't have; the latter openly says the
platform isn't looking.

**9.23 Why no dependency aliases (`as:`)?**
"The variable name is derived from the component ID" is now a **bidirectional** rule: see
`people/basic` and you know the variable is `PEOPLE_BASIC_ENDPOINT`; see `PEOPLE_BASIC_ENDPOINT` and
you know exactly which component it points at. `as: iam` only preserves half of that —
`IAM_ENDPOINT` can't be traced back to which component it points at from anywhere, and tracing
"where did this address point wrong" is exactly where that investigation starts. It would also
require re-deriving reserved-variable protection (§5.2's two layers of defense) and dependency
deduplication (keyed by component ID) from scratch, when both mechanisms' current
shape is entirely built on "the name is computed from the ID." The platform already offers cheaper
places for the cases that genuinely need to swap implementations: an event bus / object storage /
cache / search go through a `kind` resource (change one `engine` field); a service that needs an
address but not a dependency edge (like IAM) goes through a non-reserved key in `configSchema`. For
anything that sits on a real dependency edge, the implementation's name showing up in the variable
name isn't a flaw — **it's the fact of that dependency.**

### 9.24 One-sentence summary

> **The platform only ever plays "connector" and "translator" — it never oversteps into "business
> logic" or things "infrastructure" has already solved.**

---

## 10. Notes for discussing this with a user

This section is a reminder for you (the AI) — all of these are real pitfalls people have actually
hit:

| Scenario | The right move |
| --- | --- |
| A user asks "can we add a registry / config center / gateway" | First explain this was **explicitly argued through and rejected** (§4.1 + §9), then discuss the problem they're actually trying to solve |
| Writing a dependency declaration | **Only exact versions.** `^1.0.0` / `~1.0.0` / `latest` all error |
| Writing a health check | `/healthz` only checks this process. **Never** ping a database or a dependency component inside it |
| Reading a weak dependency's env var | Must use `os.environ.get()` / `System.getenv()`. **Never** `os.environ["X"]` |
| The component image has no `wget` / `curl` | The Compose healthcheck will call it unhealthy — if the component's own logs say "ready" but the platform says unhealthy, this is usually why |
| A component's cold start takes longer than a minute (a heavy Spring Boot / Django / .NET app) | Raise `healthCheck.startPeriodSeconds` above the real cold start. The default grace period is 60 seconds; exceed it and, under K8s, the component permanently CrashLoopBackOffs. Under 60 seconds, leave it alone |
| A user wants to call two versions of X from within one component | Not possible — within `dependencies`, one component ID can only appear once (the variable name carries no version, so it would collide). Version coexistence is a **project-level** capability |
| Changed `config`, but "nothing happened" | Check the key name first — `brickkit up` warns "a config item won't take effect" and guesses which one you meant |
| Writing a binding for MQ / object storage / search | The slot is called `vhost` / `bucket` / `index` respectively, not `database`. A wrong one errors and names the right one |
| The user says "frontend doesn't need to be packaged as an image, right?" | There's no static type on this platform. Frontend = an nginx container, `port: 80` |
| The user wants to put multiple components in one repo | Not supported. One component, one Git repository |
| The user asks who creates the `database` | **The user creates the database itself, once** (ops side); tables are created by the component's migration |
| Two components share one database | The migration state table's primary key **must include a component identifier**, or migrations will clobber each other |
| `docker compose logs` shows nothing | Missing `-p brickkit-<project-name>` — compose is looking at a different project |
| Edited a local source's `component.yaml` but `up` doesn't react | Local sources don't get cached; confirm the component actually comes from that local source |
| `mode: debug` and the caller keeps getting 503 | The process's actual listening port doesn't match `localPort` |
| A `mode: debug` component reports `relation does not exist` | Debug components don't generate a migration container; you have to run the migration by hand once |
| A `mode: debug` component's own config still points at `host.docker.internal` for some out-of-band dependency | That's a string literal the user wrote; brickKit doesn't parse config values, so it doesn't get rewritten when the component becomes `mode: debug`. Edit that literal yourself (usually to `localhost`) |
| A `mode: debug` component depends on a `servedBy` member | Works: its `*_ENDPOINT` resolves to a real `localhost:<port>` — the CLI opens the mapping on the shell's compose service, since the member has none of its own (§5.6) |
| A user asks "should I use `mode: debug` or `mode: local`" | Debugging with breakpoints → `mode: debug`, written in `override.yaml` only (you start it, in an IDE). Just want it running without a container and without babysitting a terminal command yourself → `mode: local`, written in `brickkit.yaml` (BrickKit detects the start command, launches it, supervises it). Both are Docker-only, both pinned like `mode: enabled` |
| A user asks "why won't `brickkit.yaml` accept `mode: debug`" | By design (§5.4, §5.6) — `mode: debug` is a personal, per-developer fact ("I'm debugging this on my machine right now"), never something a teammate reviewing `brickkit.yaml` should see. It can only be written in `override.yaml` (optional, gitignored). Run `brickkit override` to generate/refresh that file, then add `mode: debug` + `localPort` under the component's entry |
| `brickkit override` refuses to write, saying a target upgrade is rejected | `override.yaml`'s `target` can only ever downgrade `brickkit.yaml`'s own `deploy.target` (k8s → docker/podman; docker ↔ podman is fine either way; never back to k8s). Fix the `target` value in `override.yaml`, or remove it to just follow `brickkit.yaml` |
| A user edited `override.yaml` but nothing changed | Check `--config` first — `override.yaml` only ever applies to a run against the **default** `brickkit.yaml`; `--config brickkit.prod.yaml` ignores it and prints a note saying so |
| A user asks "why can't `brickkit down` stop my `mode: local` component" | By design — `down` only ever touches containers; a `mode: local` process belongs to whichever terminal ran `up` and is only ever reachable from there. `status`/`down`/`graph` all print a hint naming that session's PID when one is running, but none of them can reach across into it |
| A `mode: local` component crashes and the summary is too noisy (or not noisy enough) | `--crash-lines N` on `brickkit up` controls how many of the process's own recent output lines the crash summary keeps (default 20, `0` = exit reason only) |
| Discussing signing | The publisher needs **cosign** installed; **the installer doesn't** (verification uses the Go standard library) |
| The user wants the platform to help with security review | Install implies trust. The platform only steps in after the fact with `blocked` |
| A user asks "can I merge multiple components into one instance to save memory" | First ask if it's JVM (20 Go/Rust components are only 0.4G, not worth it); then suggest GraalVM native images and on-demand activation. If they still want to merge: **`servedBy` (§5.7) is the supported path** — it handles address routing correctly on both Docker and K8s; everything else (module isolation, config, migrations ordering inside the shell) is still their own code, see the shell implementer's guide. `mode: disable` is unrelated to this — it still can't be used as a "I'm taking this over myself" switch |
| A user's `brickkit` output is in a language they didn't expect (or a script that greps the output broke) | The language is chosen per run: `BRICKKIT_LANG` beats the saved `brickkit lang set` value beats the English default — `brickkit lang` prints which one is in effect and why. For scripts, don't grep the human text; key off the exit status and the stable `error_code` in the JSON log line on stderr, or pin `BRICKKIT_LANG=en` |
| A user says commands print noisy `{"time":...,"level":"INFO",...}` lines they don't want to see | The CLI already defaults to `--log-level warn` — the routine per-command lifecycle lines (`Command started`, `Command finished`, and similar) are quiet out of the box, so this only happens when something overrides the default: an explicit `--log-level info`/`debug` on the command, or `BRICKKIT_LOG_LEVEL` set to one of those somewhere in the shell/CI environment. Find that override first; removing it (or passing `--log-level warn` explicitly) is the fix. `--log-level off` goes further and also silences the `error_code` line on an actual failure — reach for that only in a context (a pre-commit hook, say) where a human just wants the ❌ message and nothing is parsing `error_code` |
| A user asks "which of independent/shell-merged/mixed, or docker/k8s, should I actually use" | This is the topology × deploy-target decision `docs/en/07-patterns/05-deployment-selection-guide.md` exists to answer — walk through its matrix rather than improvising an answer inline. Its one hard rule worth remembering directly: `mode: debug` (the debug toggle, written in `override.yaml`) only exists under an *effective* `deploy.target: docker`; it's rejected outright under `k8s` |
| A user's upstream component isn't built or published yet and they ask for a mock, or for the CLI to substitute one | Not a platform feature — the platform never parses contracts and never swaps in a stand-in for a missing required dependency (§4.1). The path that already works: `brickkit new <id> --contract openapi` for a stub carrying the agreed contract, `brickkit add --local`, `mode: debug` + `localPort` on the stub in `override.yaml`, and any mock tool listening on that port. Walkthrough with real output: `docs/en/03-guide/08-consuming-artifacts.md` |
| A user asks "how do I run everything locally without Docker/K8s at all" | That's the one shape the platform doesn't manage or inject anything for — see `deployment-selection-guide.md`'s "Running components by hand" section. The one thing worth telling them: `brickkit up --dry-run` after a temporary `mode: debug` entry in `override.yaml` for the component in question dumps the exact env vars a real deployment would inject, as a cheat sheet — then revert the edit, don't actually deploy that way |
| A user pastes a `brickkit` error, or asks how to script around failures (retry vs. alert) | Every command-ending error carries a stable `error_code` in the JSON log line on stderr, right after the `❌` block. Look it up in `docs/en/06-architecture/10-error-codes.md` (swap `en` for `zh`) — it lists each code's situations by the exact title the CLI prints, with cause and fix. Only `NETWORK_UNREACHABLE` is worth retrying unchanged; codes are stable and only ever added |

---

## 11. Repository map and where to dig deeper

### 11.1 Code structure

```
cmd/brickkit/          CLI entry point
cmd/gen-schemas/       regenerates schemas/*.json (make generate-schemas); a dev tool, not built into the CLI
tools/i18n/            CLI i18n migration toolkit (AST-based extraction/rewrite of user-visible strings, test-expectation helpers); a dev tool, not built into the CLI
internal/               CLI implementation
  ├── config/            brickkit.yaml parsing and validation
  ├── override/           override.yaml parsing, single-file validation, cross-file checks against
  │                        brickkit.yaml (target downgrade, k8s+debug/local, dangling entries), drift
  ├── manifest/           component.yaml parsing and validation
  ├── schemagen/          generates the JSON Schemas from the config / manifest / override Go structs
  │                        by reflection
  ├── resolver/           dependency resolution, topological sort
  ├── shell/              servedBy grouping/merging, shared by compose and k8s renderers
  ├── cascade/            cascade decision: figures out who actually starts this time (follows the top)
  ├── skills/            embedded AI-assistant skill assets + the five-state check (brickkit skills)
  ├── inject/             env-var injection and resource-quota merging
  ├── compose/            docker-compose.yaml generation
  ├── k8s/                Kubernetes manifest generation
  ├── engine/             docker compose / kubectl driver
  ├── source/             install sources: market / git / local
  ├── security/           cosign signing and standard-library verification
  ├── workspace/          component source workspace (--repo / sync)
  └── market/             marketplace client
market-server/          the component marketplace backend (an independent Go module)
schemas/                JSON Schema for component.yaml, brickkit.yaml, and override.yaml — generated,
                         checked in; editors use it for completion and typo detection
docs/en/, docs/zh/      current documentation (architecture / guide / patterns, bilingual mirror)
docs/archive/           historical record, not part of current docs
tests/components/       10 real components used to test the platform itself
tests/checklist/        acceptance checklists → the tests that prove them
deploy/market/          the marketplace's compose / kustomize / Helm
```

Unit tests sit **right next to the code they test** (`internal/**/*_test.go`), no parallel test
directory. `tests/` only holds what can't live next to the code: checklists, benchmarks, and
components used as fixtures.

### 11.2 Where to dig deeper (grab these when you need detail)

Every document lives in this same repository; the raw-link prefix is
`https://raw.githubusercontent.com/brickKit/brickKit/main/`.
The complete machine-readable index for this (English) tree is at the repo root,
**[`llms.txt`](llms.txt)**; the Chinese tree has its own, independently written,
**[`llms.zh.txt`](llms.zh.txt)**.

| What you want to dig into | Grab this |
| --- | --- |
| A 5-minute hands-on start, before reading anything else | `docs/en/00-quick-start.md` (swap `en` for `zh`) |
| A one-page glossary and the service-naming rule everything else builds on | `docs/en/01-concepts.md` (swap `en` for `zh`) |
| The most common `up`/`down`, local-debug, and signature failures, plus the offline checks (`brickkit lint` and the editor schemas) going wrong, symptom → cause → fix | `docs/en/08-troubleshooting.md` (swap `en` for `zh`) |
| How BrickKit compares to Docker Compose, Helm, Kustomize, Tilt/Skaffold, Backstage, monorepo tooling | `docs/en/02-comparison.md` (swap `en` for `zh`) |
| Why the component model suits AI-written code, and a concrete workflow for it | `docs/en/05-ai-development.md` (swap `en` for `zh`) |
| What the platform is, how the core mechanisms work (current) | `docs/en/06-architecture/` (swap `en` for `zh` for the Chinese version) |
| Every error code, the situations behind each (by the exact title the CLI prints), cause and fix; which code is worth retrying; exit statuses; the ⚠️ warnings | `docs/en/06-architecture/10-error-codes.md` (swap `en` for `zh`) |
| Why the platform is shaped this way: the one idea underneath (declare a graph, derive the rest); each engineering idea it draws on or deliberately leaves alone (DDD, GitOps, twelve-factor, contract-first, hexagonal architecture, TDD…) each a numbered entry explained from scratch — what it is, its upside and cost, the AI-development pain it maps to, what BrickKit does, what it deliberately doesn't do, and how an AI copes; and the argument behind each of the twelve principles | `docs/en/06-architecture/01-design-principles.md` (swap `en` for `zh`) |
| Hands-on tutorials | `docs/en/03-guide/` (same swap) |
| Cloning, archiving, removing and restoring component source (`add --repo` / `sync` / `remove` / `restore`, the pre-commit hook), hands-on with real output | `docs/en/03-guide/09-component-source.md` (swap `en` for `zh`) |
| A deep, real walkthrough of a Go component with a database and migrations | `docs/en/04-go-component-template.md` (swap `en` for `zh`) |
| How to layer tests, plan seed/test data, design components well, tune deployment | `docs/en/07-patterns/` (same swap) |
| Which deployment shape to pick for a whole project — topology (independent / shell-merged / mixed) × `docker`/`k8s`, plus the `mode: debug` toggle and where running components by hand fits in | `docs/en/07-patterns/05-deployment-selection-guide.md` (swap `en` for `zh`) |
| How to build a shell that qualifies for `servedBy` | `docs/en/07-patterns/07-shell-implementers-guide.md` (swap `en` for `zh`) |
| Whether and how to declare `servedBy` on your own project | `docs/en/07-patterns/06-servedby-deployment-checklist.md` (swap `en` for `zh`) |
| How Podman works as a deploy engine, and the one environment prerequisite it needs | `docs/en/07-patterns/11-podman-environment-checklist.md` (swap `en` for `zh`) |
| How to self-host the component marketplace | `docs/en/07-patterns/09-deployment/self-hosted-market.md` (swap `en` for `zh`) |
| How to share one database connection pool across components merged into a shell | `docs/en/07-patterns/08-shared-connection-pools.md` (swap `en` for `zh`) |
| Where secrets live and end up on each deploy target, the two ways a secret manager plugs in (process environment vs. `existingSecret`), and the honest limits | `docs/en/07-patterns/10-secrets.md` (swap `en` for `zh`) |
| Whether calling a dependency's `*_ENDPOINT` needs special client-side handling across a redeploy — real measured Go/Python/Node HTTP client behavior, not assumed | `docs/en/07-patterns/03-service-addressing.md` (swap `en` for `zh`) |
| Dependency resolution, diamond dedup, cycles, and why more components doesn't mean more serial steps | `docs/en/06-architecture/02-dependency-resolution.md` (swap `en` for `zh`) |
| Real generated Docker Compose and Kubernetes files, side by side, from the same Manifest | `docs/en/06-architecture/03-deployment-generation.md` (swap `en` for `zh`) |
| What actually happens when a resource binding collides, and how the quota chain really merges field by field | `docs/en/06-architecture/05-resource-binding.md` (swap `en` for `zh`) |
| The full dictionary of every environment variable the platform can inject — every resource `kind`'s exact variable names, the reserved-variable warnings and the one case that's a hard error, and how `servedBy` merges a member's config onto the shell | `docs/en/06-architecture/04-environment-variables.md` (swap `en` for `zh`) |
| Every `component.yaml` field's type, required-ness, default, and the exact constraint the validator applies — including the two fields (`enum`, `items`) that parse but are never actually read anywhere | `docs/en/06-architecture/07-component-yaml-reference.md` (swap `en` for `zh`) |
| Every `brickkit.yaml` field's type, required-ness, default, and constraint — including every `mode`/`servedBy`/`replicas` mutual exclusion and which fields only take effect under one deploy target (written under the other, `up` warns) | `docs/en/06-architecture/08-brickkit-yaml-reference.md` (swap `en` for `zh`) |
| What actually gets signed, why verification needs no cosign dependency, and why the public key can't come from the marketplace | `docs/en/06-architecture/06-signing-and-trust.md` (swap `en` for `zh`) |
| Every command's full flag reference, with real generated output — the detailed complement to §8 above | `docs/en/06-architecture/09-cli-reference.md` (swap `en` for `zh`) |
| Editor completion and red-squiggle typo detection for `component.yaml` / `brickkit.yaml` — the JSON Schemas in `schemas/`, how to wire them up, and what they deliberately don't cover | `docs/en/00-quick-start.md` (swap `en` for `zh`) |
| Every marketplace HTTP endpoint, auth, error codes, and what publishing sends over the wire | `docs/en/09-market-api.md` (swap `en` for `zh`) |
| How to layer tests for a component built on BrickKit, and a recommended spec-first order for having an AI write one | `docs/en/07-patterns/01-testing.md` (swap `en` for `zh`) |
| How to plan seed data and test data | `docs/en/07-patterns/02-data-construction.md` (swap `en` for `zh`) |
| How to research a domain, recognize when a feature needs a component family, not a flag, and map component boundaries onto DDD's vocabulary | `docs/en/07-patterns/00-component-design.md` (swap `en` for `zh`) |
| How to keep a closed-source component's logic from leaking out of its own image | `docs/en/07-patterns/04-closed-source-image-hardening.md` (swap `en` for `zh`) |
| The full site index (with links) | `llms.txt` (Chinese: `llms.zh.txt`) |

---

## 12. Project status

| | |
| --- | --- |
| Development progress | Every planned step is done, deferred items have all been closed out |
| Tests | 2,000+ test functions, race-clean |
| Hands-on guides (current) | 14 articles, every one run for real; see `docs/en/03-guide/` |
| Hands-on guides (archived) | 23 articles, every one run against real Docker / Kubernetes / a live marketplace |
| Design books (archived) | 14 volumes, cross-checked against the implementation twice |
| Decision record | 566 entries, each carrying the reasoning behind it at the time |

**Runtime requirements:** Go 1.22+, Docker 20.10+ (Compose V2). K8s-related guides need minikube;
signing needs cosign (**publishers only** — verification uses the Go standard library).

**License:** Apache License 2.0.
