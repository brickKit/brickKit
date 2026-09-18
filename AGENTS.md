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
> `docs/{en,zh}/<architecture|guide|patterns>/<same relative path>`. §11's table below gives
> English paths; swap `en` for `zh` to get the Chinese equivalent.

---

## 1. One-sentence positioning

**BrickKit is a component assembly and management platform. Build systems like snapping together bricks.**

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
brickkit.yaml (declaration)
   ↓ ① Cascade decision: figure out which components should actually start this time (enabled + dependency graph, top-down inheritance)
   ↓ ② Dependency resolution: recursively expand the dependency tree, error on missing required deps, topological sort gives the start order
   ↓ ③ Env-var injection: dependency addresses, resource connections, own config → environment variables
   ↓ ④ Deployment-file generation: docker-compose.yaml or K8s Deployment/Service/Ingress
   ↓ ⑤ Run migrations: a K8s Job or a one-shot Docker service; failure blocks the main service
   ↓ ⑥ Invoke the underlying engine: docker compose up -d / kubectl apply
Running containers
```

The CLI's Manifest comes from the `.brickkit/manifests/` cache — it **does not depend on** the
source directories under `components/`. The source directory only serves IDE-side development; it
has nothing to do with runtime.

---

## 3. Glossary

| Term (Chinese) | English | Definition |
| --- | --- | --- |
| 主系统 | BrickKit CLI | The local command-line tool, no long-running process |
| 组件市场 | BrickKit Market | The public component publishing and discovery platform |
| 组件 | Component | The most basic install-and-run unit, **always a container** |
| Manifest | component.yaml | A component's self-description file |
| 项目配置 | brickkit.yaml | Project-level declaration: component list, enabled state, local debug, exposure, config overrides, resources, deploy target |
| 强依赖 | Required Dependency | Missing → the CLI **errors and blocks startup** |
| 弱依赖 | Optional Dependency | `optional: true`; missing → warns but continues, and **the env var is not injected at all** |
| 版本化服务名 | Versioned Service Name | A service name carrying an exact version, e.g. `people-basic-1-0-0` |
| 本地调试模式 | Local Debug Mode | `local: true`; the component runs on the host inside an IDE, mapped into the container network via `extra_hosts` |
| 安装源 | Source | Where a component comes from: the marketplace (HTTP) / a Git repo / a local directory |
| 基础资源 | Resource | External systems a component depends on (databases, Redis, etc.), deployed by ops, bound in `brickkit.yaml` |
| 环境变量注入 | Env Injection | The CLI writes dependency addresses, resource connections, and own config into env vars when generating deployment files |
| 部署目标 | Deploy Target | `docker` or `k8s`, decides which kind of deployment file the CLI generates |
| 数据库迁移 | Migration | A component declares `migration.command`; the CLI runs it before deployment |
| 配置覆盖 | Config Override | `brickkit.yaml`'s `config` overrides the configSchema defaults. **Value types aren't validated, but key existence is** |
| 连接组件 | Connector Component | An orchestrating component that coordinates several standalone components |
| 单一组件 | Standalone Component | A component that completes one function on its own, internally transactionally self-consistent |
| 精确版本 | Exact Version | `major.minor.patch`; dependency declarations **do not accept** `^` / `~` range constraints |
| 跟着上层走 | Top-down Inheritance | Top-level components run by default; a lower one runs as long as any upstream component that needs it is running. Writing `enabled` explicitly takes precedence (§5.4) |

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

### 4.1 What the platform explicitly refuses to do (the rejection list)

This list is "platform minimalism" made concrete. **When proposing something to a user, never
suggest BrickKit do anything on this list** — every one of these has already been explicitly
argued through and rejected (reasoning in §9):

| Won't do | Alternative |
| --- | --- |
| Long-running services / control plane | The CLI runs and exits; state lives externally in `brickkit.yaml` + the underlying engine |
| Registry / address book | Docker DNS / K8s Service DNS |
| Health-check polling | K8s Probes / Compose healthcheck + restart policy |
| API gateway / service mesh / load balancing | Components call each other via DNS directly; K8s Service does native load balancing. **An out-of-band gateway hooks in via `labels` passthrough** (design/002 §4.7, §2.23) — the platform passes labels through without interpreting the routing |
| Config center / dynamic hot reload | Env-var injection; change config, then `brickkit up` restarts |
| Communication governance (circuit breaking / rate limiting / retries) | The component's own code |
| Weak-dependency degradation logic | The component's own business logic |
| Multi-tenancy | The component's own business |
| Version-range resolution (`^1.0.0`) | Only exact versions are accepted |
| Multi-environment overlay / inheritance merging | Each environment gets its own complete, self-contained `brickkit.yaml` |
| Config value type validation | configSchema is just a spec sheet |
| Third-party component security review | Install implies trust + after-the-fact `blocked` |
| Monorepo sub-directory components | One component, one Git repository |
| Consolidated deployment / monolithic shell — the platform does not, and will not, ship its own shell scaffolding or process supervisor | But a small, structural piece **is** built in: `servedBy` lets a component declare "my workload is provided by another component," and the platform correctly wires `*_ENDPOINT` addresses to it on both Docker and K8s — without ever needing to understand what's inside the shell. See §5.7 below and *Merged Component Deployment* / §2.21 of the architecture rationale for the full boundary |
| Dependency aliases (`dependencies.components[].as`) | Variable names derived from component ID are bidirectionally computable; an alias only preserves half of that. "One capability, multiple implementations" should go through a `kind` resource or a `configSchema` address field instead. See §2.24 of the architecture rationale |
| Low-code / BI / DevOps pipelines | Out of scope |
| Podman as a deploy target | Support was built and ran — `up`, `status`, real requests, idempotent reruns all passed — but `down` fails on rootless Podman with `rootless netns: kill network process: permission denied`, reproducible even with plain `podman rm -f`, outside BrickKit's own code entirely. A project that can't be torn down is worse than one that never came up — containers keep holding ports and volumes while the CLI would have reported success — so support was pulled rather than shipped half-working. `up`/`status` on a machine with only Podman installed name this exact failure and point at Docker instead of a generic "no engine found." Design/005 §7.5 sets an explicit bar for reversing this: a real machine where `podman compose down` itself works cleanly, a full lifecycle verified on it, and a repeatable check added so it can't silently regress again |

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
overwriting the former. The CLI errors on this when parsing the Manifest (design/002 §3.6). Diamond
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
`up` outright** (design/004 §5.6.3) — this is different from both a reserved-variable collision
(warns, skips) and an ordinary unset optional key (silently not injected, the component's own
"unconfigured" branch runs). A required key with no default means "the platform genuinely cannot
guess this — the project has to supply it" (the standard shape for a cross-project service address,
design/003 §4.9, since the platform has no way to derive where another project's service lives).
Missing it isn't a crash and isn't a warning — the variable simply never exists — so leaving this as
a silent skip would mean the component runs, looks healthy, and has one call path that quietly never
works. `brickkit up` errors instead, naming the exact missing item and which component declared it
required.

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

### 5.4 `enabled`: top-down inheritance

> **Top-level components run by default; a lower-level component runs as long as any upstream
> component that's running still needs it; writing `enabled` explicitly overrides all of that.**

| Value | Meaning | Behavior |
| --- | --- | --- |
| **not written** (no `enabled` field) | **Follows the top** | A top-level component (nothing depends on it) runs by default; a lower one follows whatever's above it |
| `enabled: true` | **Always runs** | Ignores what's above it. If its **required** dependencies are turned off, it **errors** (two conflicting intents) |
| `enabled: false` | **Never runs** | Whatever depends on it stops too (unless something has it pinned `enabled: true`, which then errors) |

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
  special-casing at all (§2.14 of the architecture rationale)
- Every line of CLI output carries its reason: `starting (top-level)` / `starting (enabled: true)`
  / `starting (X needs it)`

**The only way to narrow the startup scope is to change `enabled`.** There's no `--only`-style
flag — set the top-level things you don't want on `enabled: false`, and both `up` and `sync` follow
suit; to restore full scope, `git checkout brickkit.yaml`.

Components added automatically by `brickkit add` **do not get** an `enabled` field written.

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

### 5.6 Local debugging (`local: true`)

To debug a component with breakpoints in an IDE, while it's still reachable by other components on
the Docker network:

- A component marked `local: true` in `brickkit.yaml` **doesn't generate a container**
- Other containers resolve that component's versioned service name to `host-gateway` via
  `extra_hosts`
- Multiple components can be debugged locally at once, each with its own `localPort`; the CLI
  injects the matching port automatically
- The CLI generates `local-debug.env` for the IDE to load
- **Zero component-code changes** (it reads env vars exactly as it normally would)
- Values written into that file are POSIX-shell-quoted whenever they contain a character a shell
  would otherwise misparse (whitespace, `|`, `$`, an embedded literal newline, …) — a multi-line
  PEM value or a `|`-delimited list survives `set -a && source … && set +a` intact instead of
  getting cut off at its first newline or blowing up with `command not found`. A plain value with
  none of those characters is left unquoted
- The other half of that same guarantee: a `${VAR}` config value is looked up from the project
  root's `.env` file (`config.envLookup` — process env first, `.env` second) using the same
  quoted/multi-line convention real `docker compose` itself uses for that file (double-quoted
  values support `\n`/`\r`/`\t`/`\"`/`\\` escapes and may span physical lines; single-quoted values
  are kept fully literal and may also span lines) — not a naive line-by-line `KEY=value` split. This
  is the same lookup the K8s renderer uses for a plain (non-secret) env value, so a multi-line
  `.env` value lands intact in a generated Deployment's `env` list too, not just in
  `local-debug.env`
- What it does **not** rewrite: a config value that's an opaque string literal pointing at
  something outside brickKit's own dependency graph (an out-of-band container's address, say,
  written assuming a container network — `http://host.docker.internal:8000`). brickKit doesn't
  parse config string contents, so switching that component to `local: true` doesn't retarget the
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

`local: true` is untouched by this — same field, same meaning, same code
paths as always; `servedBy` is a wholly separate, independent mechanism that
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
itself must get right: [Building a qualified shell](docs/en/patterns/shell-implementers-guide.md).
For whoever is deciding whether and how to declare `servedBy` on their own project:
[Declaring servedBy: a deployment checklist](docs/en/patterns/servedby-deployment-checklist.md).
For deciding a whole project's deployment shape in the first place — topology
(independent / shell-merged / mixed) × `docker`/`k8s`, the `local: true` debug
toggle, and where running components by hand fits in — start one level up:
[Choosing a deployment shape](docs/en/patterns/deployment-selection-guide.md).

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
catch the recurring mistake of "an archive-state change got committed but `enabled` didn't come
along with it" (design/004 §3.14).

### 5.9 Marketplace, signing, and the trust model

The marketplace is an independent public platform — **it is not a component and doesn't need to be
installed**. It only answers two questions: **what's available to install? who's allowed to install
it?** It doesn't install components, run components, or manage running state.

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
of its own, covered in [Self-hosting the BrickKit Market](docs/en/patterns/deployment/self-hosted-market.md).

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
      enum: [...]                # optional
  required: [<required items>]

deployment:                      # required
  type: container                # fixed as container (frontend components too)
  image: <image reference>       # required
  port: 8080                     # required, main port (health check + _ENDPOINT var)
  extraPorts:                    # optional, e.g. gRPC
    - name: grpc
      port: 9090
  resources:                     # optional, **recommended values**, the CLI passes them through without validation
    requests: { cpu: "100m", memory: "128Mi" }   # recommend writing only requests (design/002 §4.6)
  # limits are better left to the deployer: quotas merge field-by-field, and a component
  # that writes limits.cpu can never have it removed by the project config
  labels:                        # optional, deployment metadata passthrough, the platform doesn't interpret key/value (design/002 §4.7)
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

> **`startPeriodSeconds` is the only overridable timing parameter under `healthCheck`** (design/002
> §9.3). `interval` / `timeout` / `failureThreshold` are fixed by the platform; their product gives a
> startup budget of 30 seconds — a component with a cold start longer than that (Spring Boot /
> Django / .NET) will make `up` fail under Docker, and permanently CrashLoopBackOff under K8s, while
> the container's own logs look perfectly healthy the whole time. The grace period only delays
> "declaring it dead," never "declaring it alive," so setting it generously costs nothing.

> **A component's entrypoint must fail fast on an argument it doesn't recognize** (design/002
> §8.5.1) — this is a hard requirement on the component author, not something the platform can
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
> unrecognized key is rejected on the spot (design/002 §2.2.1), not silently ignored — so the
> skeleton above must be copy-pasteable exactly as-is. There used to be two "reserved" fields,
> `observability` and `compatibility.minCliVersion`; both were removed (design/002 §2.3): neither was
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
or use `enabled: false` to run fewer of them).

**⚠️ Health-check prohibition:** `/healthz` only checks that this process itself is alive. Checking a
database or a dependency component inside a health check causes production cascading failures — one
downstream hiccup gets every upstream marked unhealthy and restarted at once.

**⚠️ A component with a cold start longer than 30 seconds must set `startPeriodSeconds`.**
`interval` / `timeout` / `failureThreshold` are fixed by the platform (10s / 3s / 3); their product is
the default startup budget of 30 seconds. Exceed it: under Docker, it's marked `unhealthy`, failing
`up -d --wait` and stalling any dependent waiting on `service_healthy`; under K8s, the Pod gets
killed and restarted, runs through the same 30 seconds again → **permanent CrashLoopBackOff**, while
the container's own logs look completely normal the whole time. Spring Boot / Django preloading /
.NET's first JIT pass are all squarely in range. The grace period only delays "declaring it dead,"
never "declaring it alive" (a component that's ready in two seconds still turns healthy in two
seconds), so setting it generously costs nothing.

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
    enabled: true                # optional, see §5.4 for how to write it
    local: false                 # optional, local debug mode
    localPort: 8081              # host port when local: true
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
      traefik.enable: "true"     # the platform doesn't interpret key/value; value must be a string (design/003 §4.11)

resources:                       # base-resource declarations and bindings (the resource itself is deployed by ops)
  - kind: database
    engine: postgresql
    id: main-db
    host: <host>
    port: 5432
    username: <username>
    password: ${DB_PASSWORD}     # must be referenced via an env var
    bindings:
      - componentId: people/basic
        # ↓ the following four are **the same slot** (which spot this component occupies in the
        #   resource) — use the one matching `kind`, only one is allowed. A wrong name errors and
        #   names the right one to use (design/006 §5.2)
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
> `default` ServiceAccount, whose token is auto-mounted as normal** (design/008 §9.4). This is easy
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
> (design/008 §8.5). The CLI warns once about this, but by then it hasn't verified anything.
>
> Public keys have to be configured here, rather than pulled alongside the signature from the
> marketplace — otherwise the marketplace would be issuing its own certificates to itself, and if
> the marketplace were ever compromised, an attacker could swap out both the component and the
> public key together, and verification would still pass (design/003 §3.6).

**Multiple environments:** each environment gets its own **fully self-contained** `brickkit.yaml`
(e.g. `brickkit.prod.yaml`), selected with `brickkit up --config brickkit.prod.yaml`. **There's no
overlay / inheritance / merge mechanism** (see §9.9 for why).

---

## 8. The CLI command set (13 commands + `version`)

| Command | Core behavior |
| --- | --- |
| `brickkit init <name>` | Generates a `brickkit.yaml` skeleton and a `.brickkit/` directory, and installs the AI assistant skills (`--no-skills` to skip) |
| `brickkit skills` | View/refresh the AI assistant skills installed in the project (`status` / `update`). Never overwrites something hand-edited; never touches the user's own `CLAUDE.md` |
| `brickkit add <id>[@ver]` | Recursively pulls dependencies, downloads artifacts, writes them into the config (**doesn't write an `enabled` field**). If no version is given, takes the latest installable version from the source and pins it to disk as an **exact version** |
| `brickkit remove <id>` | Checks for required-dependency callers before removing, automatically deletes the source directory (including an archived copy). Must specify a version when multiple versions coexist |
| `brickkit fetch <id>[@version]` | Only downloads the component's artifacts into `.brickkit/artifacts/<versioned-service-name>/`, **doesn't write to `brickkit.yaml`, doesn't deploy**. Used when calling another project's service across project boundaries (design/003 §4.9) |
| `brickkit up` | Cascade decision → generate deployment files → generate `local-debug.env` → check image permissions → run migrations → invoke the engine |
| `brickkit down` | Stops all components. **Doesn't delete volumes, data is preserved** |
| `brickkit status` | Reads the underlying engine, shows a running-state table (including multi-version detection; components not running are listed too) |
| `brickkit sync` | Bidirectionally archives / activates component source based on the cascade decision. Takes no arguments |
| `brickkit restore` | Restores `enabled` and the component-source layout to the last commit. `--check` is for the pre-commit hook to judge whether this commit is self-consistent (design/004 §3.14) |
| `brickkit login` | Interactive terminal login to the marketplace, token stored in `.brickkit/credentials` |
| `brickkit logout` | Revokes the marketplace token server-side, then deletes `.brickkit/credentials` locally. The local deletion always happens, even if the marketplace is unreachable — otherwise a network blip leaves someone believing they've logged out while the credential still sits on disk. Doing nothing when already logged out is not a failure |
| `brickkit publish` | Uploads the Manifest + image reference + artifacts to the marketplace (requires login first) |

**Common flags:**

```bash
brickkit up --config brickkit.prod.yaml           # multi-environment
brickkit up --dry-run                             # only generate deployment files, for review
brickkit up --context prod-cluster                # override deploy.context for this one run (k8s only)
brickkit up --ignore-served-by --dry-run          # verify every component can still stand alone without servedBy
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
answer is almost always here. (Full argument in the architecture rationale document, design/012.)

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
rejecting unknown fields (design/002 §2.2.1).

**9.13 Why does a missing weak dependency inject nothing at all, instead of an empty string?**
This is the entry that most fully embodies BrickKit's philosophy. The most dangerous thing about
injecting an empty string is that it **manufactures a silent failure**: a developer forgets to check
for empty, writes `requests.get(f"{ENDPOINT}/healthz")`, and the empty string just concatenates into
`/healthz` — the request hits the container's **own** port 8080, and its own `/healthz` returns
200 — the developer then mistakenly believes the weak dependency is healthy. This class of bug is
extremely hard to track down. **Better to let the component "crash loudly" at startup than "fail
quietly" at runtime.**

**9.14 Why does `enabled` have three states, but the rule is phrased as "follows the top"?**
There are still three states (unwritten / `true` / `false`), and the resulting decision hasn't
changed a single bit — but the phrasing flipped from an **implementation-first** derivation ("skip
it if nothing enabled needs it") to a **user-first** inheritance model. The two map one-to-one onto
each other case by case, but only the latter is actually readable: the decision a user has to make is
just "do I want this top-level thing" — everything below it follows along, no need to compute it
yourself. This wasn't wordsmithing for its own sake — the original phrasing genuinely misled people,
and reading it carefully would lead you to the wrong conclusion that "this rule must be broken." One
substantive rule changed alongside it: what counts as a "dependency" in the decision switched from
"required dependencies only" to "required and optional treated the same" — otherwise a weak
dependency that `add` wrote into the config wouldn't start by default, and you'd install a component
only to find half its functionality mute. See §2.14 of the architecture rationale for the full story.

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
Separation of concerns: `up` manages runtime, `sync` manages the source directory. And `up` doesn't
depend on the source directory at all (the Manifest is read from the `.brickkit/manifests/` cache).
If `up` moved files around automatically, users would be confused about why their files suddenly
moved.

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
merged unit without borrowing `local: true` and without the K8s-side gap that had no equivalent at
all. Everything past that (avoid port collisions inside the merged process, take over health checks
and migrations, isolate each module's config) still lives entirely in the shell author's own code —
not one more line of platform code is needed there, and the platform still never has to understand
which things *can* be merged, what supervisor manages them, or how any given framework starts
multiple listeners. Building a full `--consolidated` command would still mean starting to understand
"the shell" in exactly the way §2.21 of the architecture rationale argues against — `servedBy` is
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
platform isn't looking. Full argument in §2.23 of the architecture rationale.

**9.23 Why no dependency aliases (`as:`)?**
"The variable name is derived from the component ID" is now a **bidirectional** rule: see
`people/basic` and you know the variable is `PEOPLE_BASIC_ENDPOINT`; see `PEOPLE_BASIC_ENDPOINT` and
you know exactly which component it points at. `as: iam` only preserves half of that —
`IAM_ENDPOINT` can't be traced back to which component it points at from anywhere, and tracing
"where did this address point wrong" is exactly where that investigation starts. It would also
require re-deriving reserved-variable protection (§5.2's two layers of defense) and dependency
deduplication (design/002 §3.6, keyed by component ID) from scratch, when both mechanisms' current
shape is entirely built on "the name is computed from the ID." The platform already offers cheaper
places for the cases that genuinely need to swap implementations: an event bus / object storage /
cache / search go through a `kind` resource (change one `engine` field); a service that needs an
address but not a dependency edge (like IAM) goes through a non-reserved key in `configSchema`. For
anything that sits on a real dependency edge, the implementation's name showing up in the variable
name isn't a flaw — **it's the fact of that dependency.** See §2.24 of the architecture rationale.

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
| A component's cold start takes tens of seconds (Spring Boot / Django / .NET) | Write `healthCheck.startPeriodSeconds`. The default budget is only 30 seconds; exceed it under K8s and it permanently CrashLoopBackOffs |
| A user wants to call two versions of X from within one component | Not possible — within `dependencies`, one component ID can only appear once (the variable name carries no version, so it would collide). Version coexistence is a **project-level** capability |
| Changed `config`, but "nothing happened" | Check the key name first — `brickkit up` warns "a config item won't take effect" and guesses which one you meant |
| Writing a binding for MQ / object storage / search | The slot is called `vhost` / `bucket` / `index` respectively, not `database`. A wrong one errors and names the right one |
| The user says "frontend doesn't need to be packaged as an image, right?" | There's no static type on this platform. Frontend = an nginx container, `port: 80` |
| The user wants to put multiple components in one repo | Not supported. One component, one Git repository |
| The user asks who creates the `database` | **The user creates the database itself, once** (ops side); tables are created by the component's migration |
| Two components share one database | The migration state table's primary key **must include a component identifier**, or migrations will clobber each other |
| `docker compose logs` shows nothing | Missing `-p brickkit-<project-name>` — compose is looking at a different project |
| Edited a local source's `component.yaml` but `up` doesn't react | Local sources don't get cached; confirm the component actually comes from that local source |
| `local: true` and the caller keeps getting 503 | The process's actual listening port doesn't match `localPort` |
| A `local: true` component reports `relation does not exist` | Local components don't generate a migration container; you have to run the migration by hand once |
| A `local: true` component's own config still points at `host.docker.internal` for some out-of-band dependency | That's a string literal the user wrote; brickKit doesn't parse config values, so it doesn't get rewritten when the component becomes `local: true`. Edit that literal yourself (usually to `localhost`) |
| A `local: true` component depends on a `servedBy` member | Works: its `*_ENDPOINT` resolves to a real `localhost:<port>` — the CLI opens the mapping on the shell's compose service, since the member has none of its own (§5.6) |
| Discussing signing | The publisher needs **cosign** installed; **the installer doesn't** (verification uses the Go standard library) |
| The user wants the platform to help with security review | Install implies trust. The platform only steps in after the fact with `blocked` |
| A user asks "can I merge multiple components into one instance to save memory" | First ask if it's JVM (20 Go/Rust components are only 0.4G, not worth it); then suggest GraalVM native images and on-demand activation (§2.15 of the architecture rationale). If they still want to merge: **`servedBy` (§5.7) is the supported path** — it handles address routing correctly on both Docker and K8s; everything else (module isolation, config, migrations ordering inside the shell) is still their own code, see the shell implementer's guide. `enabled: false` is unrelated to this — it still can't be used as a "I'm taking this over myself" switch |
| A user asks "which of independent/shell-merged/mixed, or docker/k8s, should I actually use" | This is the topology × deploy-target decision `docs/en/patterns/deployment-selection-guide.md` exists to answer — walk through its matrix rather than improvising an answer inline. Its one hard rule worth remembering directly: `local: true` (the debug toggle) only exists under `deploy.target: docker`; it's rejected outright, at generation time, under `k8s` |
| A user asks "how do I run everything locally without Docker/K8s at all" | That's the one shape the platform doesn't manage or inject anything for — see `deployment-selection-guide.md`'s "Running components by hand" section. The one thing worth telling them: `brickkit up --dry-run` after a temporary `local: true` on the component in question dumps the exact env vars a real deployment would inject, as a cheat sheet — then revert the edit, don't actually deploy that way |

---

## 11. Repository map and where to dig deeper

### 11.1 Code structure

```
cmd/brickkit/          CLI entry point
internal/               CLI implementation
  ├── config/            brickkit.yaml parsing and validation
  ├── manifest/           component.yaml parsing and validation
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
docs/en/, docs/zh/      current documentation (architecture / guide / patterns, bilingual mirror)
docs/archive/           historical record: the old design books (design/), the old hands-on guides (试用指南/), the decision index, deployment methodology
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
| A 5-minute hands-on start, before reading anything else | `docs/en/quick-start.md` (swap `en` for `zh`) |
| A one-page glossary and the service-naming rule everything else builds on | `docs/en/concepts.md` (swap `en` for `zh`) |
| The most common `up`/`down`, local-debug, and signature failures, symptom → cause → fix | `docs/en/troubleshooting.md` (swap `en` for `zh`) |
| How BrickKit compares to Docker Compose, Helm, Kustomize, Tilt/Skaffold, Backstage, monorepo tooling | `docs/en/comparison.md` (swap `en` for `zh`) |
| Why the component model suits AI-written code, and a concrete workflow for it | `docs/en/ai-development.md` (swap `en` for `zh`) |
| What the platform is, how the core mechanisms work (current) | `docs/en/architecture/` (swap `en` for `zh` for the Chinese version) |
| Hands-on tutorials | `docs/en/guide/` (same swap) |
| A deep, real walkthrough of a Go component with a database and migrations | `docs/en/go-component-template.md` (swap `en` for `zh`) |
| How to layer tests, plan seed/test data, design components well, tune deployment | `docs/en/patterns/` (same swap) |
| Which deployment shape to pick for a whole project — topology (independent / shell-merged / mixed) × `docker`/`k8s`, plus the `local: true` debug toggle and where running components by hand fits in | `docs/en/patterns/deployment-selection-guide.md` (swap `en` for `zh`) |
| How to build a shell that qualifies for `servedBy` | `docs/en/patterns/shell-implementers-guide.md` (swap `en` for `zh`) |
| Whether and how to declare `servedBy` on your own project | `docs/en/patterns/servedby-deployment-checklist.md` (swap `en` for `zh`) |
| How to self-host the component marketplace | `docs/en/patterns/deployment/self-hosted-market.md` (swap `en` for `zh`) |
| How to share one database connection pool across components merged into a shell | `docs/en/patterns/shared-connection-pools.md` (swap `en` for `zh`) |
| Dependency resolution, diamond dedup, cycles, and why more components doesn't mean more serial steps | `docs/en/architecture/dependency-resolution.md` (swap `en` for `zh`) |
| Real generated Docker Compose and Kubernetes files, side by side, from the same Manifest | `docs/en/architecture/deployment-generation.md` (swap `en` for `zh`) |
| What actually happens when a resource binding collides, and how the quota chain really merges field by field | `docs/en/architecture/resource-binding.md` (swap `en` for `zh`) |
| What actually gets signed, why verification needs no cosign dependency, and why the public key can't come from the marketplace | `docs/en/architecture/signing-and-trust.md` (swap `en` for `zh`) |
| Every command's full flag reference, with real generated output — the detailed complement to §8 above | `docs/en/architecture/cli-reference.md` (swap `en` for `zh`) |
| Every marketplace HTTP endpoint, auth, error codes, and what publishing sends over the wire | `docs/en/market-api.md` (swap `en` for `zh`) |
| How to layer tests for a component built on BrickKit | `docs/en/patterns/testing.md` (swap `en` for `zh`) |
| How to plan seed data and test data | `docs/en/patterns/data-construction.md` (swap `en` for `zh`) |
| How to research a domain and recognize when a feature needs a component family, not a flag | `docs/en/patterns/component-design.md` (swap `en` for `zh`) |
| How to keep a closed-source component's logic from leaking out of its own image | `docs/en/patterns/closed-source-image-hardening.md` (swap `en` for `zh`) |
| The old design books' original reasoning (historical record, may not match current implementation) | `docs/archive/design/`, Chinese only |
| The old hands-on guides, as originally written (historical record) | `docs/archive/guide/`, Chinese only |
| The full site index (with links) | `llms.txt` (Chinese: `llms.zh.txt`) |
| Platform philosophy and overall architecture (the root document) | `docs/archive/design/001-平台理念与总体架构.md` |
| Every `component.yaml` field and rule | `docs/archive/design/002-组件规范.md` |
| Every `brickkit.yaml` field and rule | `docs/archive/design/003-项目配置规范.md` |
| CLI commands, the dependency-resolution engine, generation logic | `docs/archive/design/004-CLI 设计.md` |
| The AI-assistant skills installed into a project: what gets installed, how to refresh, why it never touches `CLAUDE.md` | `docs/archive/design/004-CLI 设计.md` §3.2.1 |
| Docker / K8s deployment, local debugging, migrations | `docs/archive/design/005-部署与运行规范.md` |
| Declaring and binding resources like databases / Redis | `docs/archive/design/006-基础资源规范.md` |
| Marketplace API, data model, permissions, signing | `docs/archive/design/007-组件市场设计.md` |
| Trust model, secrets, reserved-variable protection | `docs/archive/design/008-安全与治理.md` |
| Writing your first component, step by step | `docs/archive/design/009-组件开发快速入门.md` |
| Building, signing, publishing to the marketplace | `docs/archive/design/010-组件发布与上架指南.md` |
| Installing, assembling, debugging, updating, rolling back | `docs/archive/design/011-组件安装与拼装指南.md` |
| Running multiple components as one instance (not platform-supported, DIY) | `docs/archive/planning/组件合并部署.md`; why it's not built: §2.21 of `docs/archive/design/012` |
| **The complete argument behind every "why"** | `docs/archive/design/012-架构设计原理与考量.md` |
| Glossary / full Manifest & config reference / env-var spec / generated-artifact examples / communication-practice templates | `docs/archive/design/附录合集.md` |
| Reading paths by role and scenario | `docs/archive/design/000 阅读指南与文档导航.md` |
| Walking through it hands-on (23 articles) | `docs/archive/guide/README.md` |
| Why a particular decision was made the way it was (566 entries) | `docs/archive/decisions/决策索引.md` |
| How the marketplace itself gets deployed | `docs/archive/planning/市场部署与运维指南.md` |
| Where the marketplace and a project each run | `docs/archive/planning/部署模式.md` |

**Documentation authority order (inside the historical record, for understanding archived content
only):** before archival, the design books under `design/` were the normative spec; `试用指南/` was
executable verification; `开发进度/` was the execution ledger; where they conflicted, `design/` won.
These now correspond respectively to `docs/archive/design/`, `docs/archive/guide/`,
`docs/archive/decisions/`. To understand the current implementation, use the rows at the top of
this table pointing at the new `docs/en/architecture/` structure instead.

---

## 12. Project status

| | |
| --- | --- |
| Development progress | Every planned step is done, deferred items have all been closed out |
| Tests | 2,000+ test functions, race-clean |
| Hands-on guides (current) | 12 articles, every one run for real; see `docs/en/guide/` |
| Hands-on guides (archived) | 23 articles, every one run against real Docker / Kubernetes / a live marketplace |
| Design books (archived) | 14 volumes, cross-checked against the implementation twice |
| Decision record | 566 entries, each carrying the reasoning behind it at the time |

**Runtime requirements:** Go 1.22+, Docker 20.10+ (Compose V2). K8s-related guides need minikube;
signing needs cosign (**publishers only** — verification uses the Go standard library).

**License:** Apache License 2.0.
