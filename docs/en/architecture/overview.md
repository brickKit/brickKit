# Architecture overview

BrickKit is a **component management and assembly platform**: build systems like snapping together bricks, where each component (brick) is developed, deployed, and called independently, and the CLI's only job is to pull them, order them, generate the deployment files, and hand off to Docker or Kubernetes. It is **not** an operating system, not an ERP, and not any specific piece of business software — it's a tool for growing an architecture incrementally.

## The four parts

| Part | Form | Long-running? | Responsibility |
| --- | --- | --- | --- |
| **BrickKit CLI** | Local single binary | No — runs and exits | Fetch, resolve, generate, invoke, publish, manage the source workspace |
| **BrickKit Market** | Independent SaaS (self-hostable) | Yes | Component publishing/discovery, versions/visibility/signing, artifact storage |
| **Component layer** | Docker containers / K8s Pods | Yes | The actual business logic; components talk to each other directly over DNS |
| **Infrastructure layer** | PostgreSQL / Redis / etc. | Yes | **Deployed manually by ops**, bound by declaration in `brickkit.yaml` |

Of these four, only the CLI is not long-running — it exits as soon as a command finishes. The desired state lives in `brickkit.yaml`, the actual state lives in Docker/Kubernetes, and the CLI itself holds no state in between.

## What happens when you run `brickkit up`

```mermaid
sequenceDiagram
    participant U as User
    participant CLI as BrickKit CLI
    participant FS as brickkit.yaml
    participant Docker as Docker / K8s

    U->>CLI: brickkit up
    CLI->>FS: read declaration
    CLI->>CLI: ① cascade — decide which components actually start this time (top-down inheritance)
    CLI->>CLI: ② resolve — expand the dependency tree, topological sort
    CLI->>CLI: ③ inject — dependency addresses, resource connections, own config → env vars
    CLI->>CLI: ④ generate — docker-compose.yaml or K8s Deployment/Service/Ingress
    CLI->>Docker: ⑤ run migrations (blocks main service on failure)
    CLI->>Docker: ⑥ docker compose up -d / kubectl apply
    Docker-->>U: running containers
```

Here is that same pipeline made concrete with components that actually exist in this repository. Running `brickkit add erp/backend@1.0.0` recursively pulls in `erp/backend`'s three required dependencies (`people/basic`, `auth/password-login`, `authorization/rbac`) plus one optional one — to keep this walkthrough focused, it follows just one of those chains: [`tests/components/people-basic/`](../../../tests/components/people-basic/) is a direct dependency, and [`tests/components/department-tree/`](../../../tests/components/department-tree/) comes along transitively because `people/basic` itself requires it. That's why (among the others) [`tests/components/erp-backend/`](../../../tests/components/erp-backend/), `department-tree`, and `people-basic` end up together in one `brickkit.yaml`. Now run `brickkit up`:

- **① cascade** finds none of the three declare `enabled`, and each is either at the top of the dependency chain or required by something that is, so all three start;
- **② resolve** expands the dependency tree and topologically sorts it, which forces the start order `department-tree` → `people-basic` → `erp-backend` (dependencies before dependents);
- **③ inject** writes `DEPARTMENT_TREE_ENDPOINT=http://department-tree-1-0-0:8080` for `people/basic`, and a similar address pointing at `people-basic` for `erp/backend`;
- **④ generate** translates each of these components' `component.yaml` into its own service in `docker-compose.yaml` — one part of the full service set generated for `erp/backend`'s complete dependency tree (see [Deployment file generation](deployment-generation.md) for what that translation actually produces, on both targets, down to the byte);
- **⑤ run migrations** runs the migration commands `department-tree` and `people-basic` each declare (`erp/backend` itself has no `migration` field, so it's skipped) — `auth/password-login` and `authorization/rbac` each declare their own migration too, run the same way;
- **⑥** finally, `docker compose up -d` brings these containers up (along with the rest of `erp/backend`'s dependency set).

**Why all three start is worth spelling out, because the general rule behind it has three states, not one.** Not writing `enabled` on a component at all means it *follows the top*: a component nothing else depends on runs by default, and a lower-level one runs as long as some running component above it still needs it — which is exactly why `department-tree` starts here even though nothing in this project's `brickkit.yaml` says so directly, it's just true that `people-basic` needs it and `people-basic` is itself needed by the top-level `erp-backend`. The other two states are explicit and override this entirely: `enabled: true` pins a component to always run regardless of what's above it (and errors if a required dependency it needs was turned off — two conflicting intents can't both hold), and `enabled: false` pins it to never run, taking everything that depends on it down too. If this project's `brickkit.yaml` set `department-tree`'s `enabled: false` instead of leaving it unwritten, `people-basic` — which requires it — would fail to resolve at all, and the CLI would say so by name rather than generating a broken deployment and letting it fail at container start. Every line of real CLI output states which of these three reasons applies (`starting (top-level)` / `starting (enabled: true)` / `starting (X needs it)`), because which of the three is true is never something a reader should have to infer from the file alone.

Among the resulting service names are `erp-backend-1-0-0`, `department-tree-1-0-0`, and `people-basic-1-0-0` — the next section explains exactly how each name is derived. For a harder version of steps ① and ② — a real diamond dependency, a real cycle, and turning a component off — see [Dependency resolution and start order](dependency-resolution.md).

## Versioned service names and the unified address format

**Service name = transformed component ID + exact version.** The transformation is three rules: `/` → `-`, `.` → `-`, all lowercase. `erp/backend` at version `1.0.0` becomes `erp-backend-1-0-0`.

The address format is **identical** under Docker and Kubernetes: `http://<versioned-service-name>:<port>`. Locally that's `http://department-tree-1-0-0:8080`; on a K8s cluster it's the exact same string — component code never needs to know which environment it's running in. This rule produces two direct consequences: **multiple versions coexist for free** (`people-basic-1-0-0` and `people-basic-2-0-0` are two non-conflicting DNS names), and **a caller always knows exactly which version it's talking to** — there is no implicit upgrade.

A real example beats the transformation rule on its own. Below is the actual `dependencies.components` block from [`tests/components/erp-backend/component.yaml`](../../../tests/components/erp-backend/component.yaml):

```yaml
dependencies:
  components:
    # 强依赖：注入 PEOPLE_BASIC_ENDPOINT（HTTP，8080）
    # 与 PEOPLE_BASIC_GRPC_ENDPOINT（gRPC，来自它声明的 extraPorts，9090）。
    # 本组件用的是后者
    - people/basic@1.0.0
    # 强依赖：注入 AUTH_PASSWORD_LOGIN_ENDPOINT
    - auth/password-login@1.0.0
    # 强依赖：注入 AUTHORIZATION_RBAC_ENDPOINT（gRPC 与 HTTP 共用主端口）
    - authorization/rbac@1.0.0
    # **弱依赖**：没装它时平台完全不注入 INFRA_REDIS_EVENT_BUS_ENDPOINT，
    # 本组件据此降级——审批照常成功，只是不发事件（003 §4.3）
    - id: infra/redis-event-bus@1.0.0
      optional: true
```

(The comments are in Chinese in the source file, quoted here verbatim rather than translated, since this is meant to be the literal file content.) The four dependency lines map to four environment variable names, each **derived directly from the component ID** (`/` and `-` become `_`, uppercased, with `_ENDPOINT` appended) — no separate variable-name declaration is needed. This is also why BrickKit has no dependency aliasing: once that two-way mapping between variable name and component ID is broken, seeing `IAM_ENDPOINT` no longer tells you which component it points at.

### External tools connecting to a component's port directly

This transformation rule isn't only for the platform's own `*_ENDPOINT` injection. When you write a standalone script or tool — a local dev script, an ops tool, a one-off debugging session, not another BrickKit component — and need to bypass a component's business API to hit its gRPC/HTTP port directly, the address is computed with the exact same rule the platform uses internally: `http://<versioned-service-name>:<the component's own declared port>`. This holds whether the component is currently deployed standalone or merged into a shell via `servedBy` (see the [shell implementer's guide](../patterns/shell-implementers-guide.md)) — the shell container's network alias uses this exact same computed name. An external tool never needs to know or care whether the component is currently merged, or whether it has its own container.

The one precondition: this code has to run inside the Docker network BrickKit manages — for example, `docker run --rm --network <project-network-name> <image> ...` to join it temporarily. Don't assume the component's port is published to the host: `expose: true` only takes effect for a standalone-deployed component — a `servedBy` member is completely unaffected by it, and there is never a `localhost:<port>` to hit.

## What the platform deliberately doesn't do, and why

| Doesn't do | Why | Symptom if you route around it |
| --- | --- | --- |
| A long-running daemon / control plane | The CLI runs and exits; state lives externally in `brickkit.yaml` plus the underlying engine. No background process means no single point of failure, no idle resource usage, no listening port | Build your own persistent orchestration daemon to solve "nobody remembers to run `up`," and you've recreated the exact single point of failure and attack surface BrickKit was designed to avoid |
| Service registry / address book | Docker Compose service DNS and Kubernetes Service DNS already provide service discovery for free; the platform doesn't need to build and keep highly available a registry of its own | Wire a custom service-discovery SDK into component code and the component is no longer a "zero-touch" deployment — it now depends on something that must be kept alive by hand, while the free DNS it replaced sits unused |
| API gateway / service mesh / load balancing | Components call each other directly over DNS; Kubernetes Service already load-balances. Gateway routing rules vary too much by business to standardize without forcing a bad fit | Skip the platform's `labels` passthrough and hand-write a Traefik file-provider config with versioned service names baked in, and that config silently goes stale the moment a component's version bumps — the platform won't warn you |
| Version-range resolution (`^1.0.0`) | Ranges are the classic cause of "works on my machine, breaks in production"; exact versions also let the version number be embedded directly in the service name, making multi-version coexistence free | Write `^1.0.0` / `~1.0.0` / `latest` in `dependencies` or `brickkit.yaml` and the CLI rejects it immediately at resolve time — the problem never survives to reach runtime |
| Multi-environment overlay inheritance | An overlay forces you to mentally reconstruct "base layer / override layer / merge rules" before you can read the final config, and a Git diff of the base layer can't tell you which environments it silently affects | Reuse config via `base.yaml` + `prod-overlay.yaml` and the only thing you save is typing a few lines — the cost is that a change to the base layer can silently affect environments you can't identify by looking at the diff, and the CLI provides no command to merge such files anyway |
| Config value type validation | Once value validation is allowed, the questions never stop — validate `enum`? `minimum`? `pattern`? — and JSON Schema's full complexity ends up inside the CLI; a component should decide for itself whether to error or fall back to a default on bad config | Declare `type: integer` in `configSchema` and let a user fill in a string — the CLI won't stop it. The component crashes only when its own code calls `int()` on that string — discovered at runtime, not at `brickkit up` |
| Weak-dependency fallback logic | Whether a missing Redis means "query the database instead," "return an empty list," or "write to a local file and retry" is a pure business decision, and the right answer differs by component | Expect the platform to "automatically" handle a missing weak dependency and nothing happens — the platform's only action is to skip injecting the corresponding `*_ENDPOINT` entirely. Component code that reads it with `os.environ["X"]` crashes immediately with a `KeyError` (this is intentional — safer than silently injecting an empty string) |
| Health-check polling | K8s Probes and Docker Compose's own `healthcheck` + restart policy already do this natively; a platform-built poller would mean either a background process (the same daemon problem this list starts with) or a client library every component would have to embed | Have a component call a dependency's `/healthz` from inside its own health check "just to be safe," and you've built exactly the cascading-restart failure mode the platform's own health-check rule (check only the process itself, never a downstream dependency) exists to prevent — one flaky dependency now makes every upstream component look unhealthy and restart at once |
| Communication governance (circuit breaking / rate limiting / retries) | Retry backoff and circuit-breaker thresholds vary by business need; a platform-wide default is both hard to get universally right and takes away flexibility a component might genuinely need | Wait for the platform to retry a failed call on a component's behalf, and nothing happens — a transient network blip surfaces directly as an unhandled error in whichever component made the call, because retry/backoff logic was never the platform's job |
| Multi-tenancy | Whether to isolate tenants with a shared schema and a tenant column, a schema per tenant, or a database per tenant is a business/domain decision with no single answer that fits every component's data model | Look for a `tenant_id` field or an isolation switch anywhere in `brickkit.yaml` or a component's Manifest, and there isn't one — tenant isolation lives entirely inside each component's own schema and business logic |
| Third-party component security review | The same trust model as npm, the VS Code Marketplace, or GitHub: installing means trusting. An upfront scan either has too many false positives (blocking legitimate components) or too many false negatives (missing deliberately disguised malicious code) | Assume every Market component has passed some kind of security audit before publish, and you'd be wrong — `brickkit add` performs no scanning at all; the only real enforcement is after the fact, marking a component `blocked` once it's confirmed malicious |

---

To dig into the full argument behind any one of these decisions, see [`docs/archive/design/012-架构设计原理与考量.md`](../../archive/design/012-架构设计原理与考量.md) (historical record, Chinese only). For the complete field or command reference, see sections 6, 7, and 8 of `AGENTS.md` (the `component.yaml` skeleton, the `brickkit.yaml` skeleton, and the CLI command set).
