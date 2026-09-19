# Design principles and trade-offs

The [overview](overview.md) explains *what* the platform does. This page explains *why it is shaped this way*: the one idea that ties the design together, the twelve principles every proposal is checked against, and — just as deliberately — the places where the platform stays silent and leaves the decision to you.

[AGENTS.md](../../../AGENTS.md) carries the same twelve principles in compressed form, one line each, written so an AI assistant can judge a proposal against them. This page is the argument behind those lines: what each one means, what it buys, what it costs, and what it refused. The twelve headings below match AGENTS.md §4 word for word, and `make lint` fails if they drift apart.

## The one idea underneath: declare a graph, derive the rest

You write two kinds of declaration. Each component's `component.yaml` says what it is and what it depends on; the project's `brickkit.yaml` says which versions you want, what is enabled, what is exposed, and where the databases are. Together they describe a graph. Everything else the platform produces is *derived* from that graph, on every run, by a rule you can state in one line:

| Derived | From | Rule |
| --- | --- | --- |
| Which components run | `enabled` plus the dependency graph | Top-level components run by default; a lower one runs while anything above it needs it; an explicit `enabled` overrides. Computed as a least fixed point, so a cycle needs no special case |
| Start order | The dependency graph | Topological sort. A missing required dependency is an error at resolve time, not a surprise at runtime |
| Service name and address | Component ID + exact version | `/` and `.` become `-`, lowercase, then `http://<name>:<port>` — the same string on Docker and on Kubernetes |
| `*_ENDPOINT` variable names | Component ID | `/` and `-` become `_`, uppercase, `_ENDPOINT` appended |
| Resource connection variables | A resource's `kind` and its binding | The `kind` name is the prefix: `DATABASE_*`, `REDIS_*`, … |
| `docker-compose.yaml` or Kubernetes manifests | The Manifest and `deploy.target` | Regenerated on every run; a component never ships one |
| Network policies | The dependency graph | Opt-in (`deploy.networkPolicy`): traffic is allowed along the dependency edges you declared |
| Which source directories are active | The cascade result | `brickkit sync` archives what isn't starting and restores what is |

Deriving instead of configuring buys three things:

- **A derived value cannot disagree with its source.** The variable name always matches the component ID, the address always matches the version, the network policy always matches the dependencies. There is no second copy to forget to update. Compare a hand-written gateway config with versioned service names baked in, which goes stale the moment a component's version bumps and says nothing about it.
- **The derivation runs both ways.** From `people/basic` you can compute `PEOPLE_BASIC_ENDPOINT`; from `PEOPLE_BASIC_ENDPOINT` you know exactly which component it points at. That is why dependency aliases were refused: an alias keeps only half of that.
- **The CLI can stay stateless.** Every artifact is a pure function of the declarations, so the CLI reads them, generates, hands off to Docker or Kubernetes, and exits. Nothing has to be remembered between runs, so nothing needs a resident process.

**Deriving is not guessing.** A derived value is a pure function of what you declared. The platform never infers intent: it won't pick a version for you (`^1.0.0` is an error), open a port you didn't expose, decide that a missing optional dependency should fall back to something, or start a component because it thinks you meant to. Where a value can't be computed from declarations, you declare it. That is how this idea and the principle "Explicit over implicit" fit together — the second is what keeps the first from sliding into guessing.

The cost is that what can't be derived has to be written down, and the escape hatches are deliberately narrow: `labels` and `deploy.ingressAnnotations` are passed through to the engine verbatim, never interpreted.

## If you arrive from a paradigm you already know

You probably came in through an idea you already trust. This table says what BrickKit takes from it and, on purpose, where it says nothing.

| Paradigm | What BrickKit takes | Where it stays silent | Read more |
| --- | --- | --- | --- |
| Domain-driven design | A component ≈ a bounded context: its own repository, Manifest, version lifecycle and contract | What is inside — aggregates, entities — and whether contexts share a database or get merged into one process (`servedBy`) | [Component design guidelines](../patterns/component-design.md#if-your-team-thinks-in-ddd) |
| Declarative desired state (GitOps) | `brickkit.yaml` is the single declaration, lives in Git, and each environment gets its own complete file — the diff is the review | A reconciliation loop. `up` is a one-shot apply and the CLI exits; nothing watches for drift | [Architecture overview](overview.md) |
| Twelve-factor configuration | Config and addresses arrive as environment variables (factor III); databases and caches are attached resources bound by declaration (IV); the same address format in dev and prod (X) | What config a component has — `configSchema` is a spec sheet, not a gate | [Environment variable contract](environment-variables.md) |
| Contract-first / API-first | `artifacts` ships contract files (OpenAPI, protobuf, anything) with the Manifest, and `add`/`fetch` download them. The Market requires a closed-source component that provides an API to declare an `api-contract` artifact — the code may be closed, the contract may not | Which format. `type` and `format` are free-form strings the CLI never parses | [AI-assisted development guide](../ai-development.md) |
| Design by contract | `configSchema` declares each key's type, `enum`, `minimum`, `maximum` and `pattern`, where a person or an AI can read them | Enforcement. The CLI checks that a key *name* exists (a typo warns) and never checks a *value* — see [configSchema is a spec sheet, not a security gate](#configschema-is-a-spec-sheet-not-a-security-gate) | [Component YAML reference](component-yaml-reference.md) |
| Hexagonal / layered architecture | — | A component's internal structure. It is the author's | [Writing a Go component](../go-component-template.md) shows a real layered one |
| Type-driven development | — | Language. Anything that builds a container image is a component | [Build your first component](../guide/10-build-your-own.md) |
| Immutable versions / lockfiles | Exact versions only; `add` resolves once and pins the result. The version is part of the service name, so two versions coexist as two DNS names | Data compatibility between coexisting versions — that is yours | [Core concepts](../concepts.md) |
| Least privilege / zero trust | Nothing is reachable until declared; optional NetworkPolicy from the dependency graph; the CLI never mounts the Docker socket | Enforcement. A NetworkPolicy does nothing on a cluster whose CNI doesn't enforce them (the CLI warns about it) | [Network policy and least privilege](../guide/11-network-policy.md) |
| Supply-chain security | cosign signatures at publish; verification with the Go standard library alone; the trust anchor is a public key in *your* project | Upfront scanning or sandboxing. Installing implies trust; afterwards the Market can mark a component `blocked` | [Signing and the trust model](signing-and-trust.md) |
| Microservices / service mesh | Independent build, deploy and call | A registry, gateway, mesh, circuit breaking. DNS is service discovery; communication governance is the component's own code | [What the platform deliberately doesn't do](overview.md#what-the-platform-deliberately-doesnt-do-and-why) |

## The twelve principles

### Platform minimalism

The CLI does nothing it doesn't have to. Every feature is maintenance forever, and the platform's whole job is to connect components and translate their declarations for Docker or Kubernetes.

**Why.** What the underlying engines already do well — DNS, health probes, restart policies, load balancing — is better done by them than by a platform-built copy. A platform that grows a runtime becomes one more thing you must keep alive.

**Cost.** The things people reach for first aren't here: no gateway, no config center, no registry. [The refusal table](overview.md#what-the-platform-deliberately-doesnt-do-and-why) says what to do instead, and what you'd see if you built one anyway.

**Refused.** A resident control plane, a registry, health-check polling, a config center, a service mesh.

### Component autonomy

Language, framework, API style, transactions and log format belong to the component. The platform asks for exactly three things: a Manifest, a health check, and that the component reads its environment variables.

**Where the boundary is.** What affects how components work *with each other* — how they are declared, addressed, configured and depended on — the platform manages. What affects how a component is written *inside* — architecture, modeling, layering, testing — the platform doesn't enforce ([patterns/](../patterns/README.md) offers advice, but nothing checks it). That is not the same as asking nothing of the container: what the platform specifies is how a container looks *at the boundary*. The entrypoint must fail fast on an argument it doesn't recognize (the migration container and the main service run from the same image); a weak dependency's variable must be read with `.get()` (when the dependency is missing, the platform doesn't inject it at all); `/healthz` checks only the process itself; a cold start longer than 30 seconds needs `startPeriodSeconds`. Those are contracts at the boundary, not interference with how the code inside is structured. Once the platform starts managing what is inside, it stops being an orchestrator and becomes a framework — and a framework can only produce components of one shape.

**Why.** That is what makes "any language that can build a Docker image" true. It is also why a frontend component is just another container listening on a port: there is no "static" component type, and so no `if type == static` branches spreading through the CLI (AGENTS.md §9.10).

**Cost.** The platform can't help with anything inside the boundary — no shared SDK, no validated business config, no opinion on how you structure code.

**Refused.** SDKs, sidecars and agents; special component types; platform-defined degradation behavior.

### Incremental

You never have to design the whole system first. Add one component and run it; add another and run that; stop and resume at any point.

**Why.** `brickkit add` pulls a component's whole dependency subtree in one command; `enabled` follows the top, so what nobody needs isn't running; `brickkit sync` keeps the source directory down to what you're actually looking at. This is also why fifty components doesn't mean fifty things to hold in your head: you only need the contract of your direct dependencies (usually one to three), a fifty-component project may run four containers locally, and the transitive dependencies are the CLI's problem (AGENTS.md §9.15).

**Cost.** The platform bounds what you must know *per component*, not how large the whole gets. Nothing stops you from growing something you can't picture in one sitting.

**Refused.** A design phase that must be complete before anything runs.

### Environment-agnostic

Component code never learns whether it runs under Docker or Kubernetes. The address it reads is `http://<versioned-service-name>:<port>` in both, byte for byte.

**Why.** One Manifest, two environments. A component describes what it is and what it needs, never where it runs, so it ships no `docker-compose.yaml` and no Kubernetes manifests (AGENTS.md §9.4). Switching environments is one field, `deploy.target`.

**Cost.** The platform has to generate correct files for both engines, and anything only one engine can do isn't expressible from the component side. Engine-specific hints go through the `labels` passthrough, uninterpreted.

**Refused.** Per-environment component files; engine-specific fields in the Manifest.

### Explicit over implicit

Exact versions, explicit exposure, explicit enabling. Anything the platform would have to guess, it makes you say.

**Why.** Version ranges are the classic cause of "works on my machine, breaks in production" (AGENTS.md §9.2). Pinning still happens once, at `add`, the way a lockfile does it: leave the version off and the CLI asks the source for the latest installable one and writes the exact result to disk. Exposure is opt-in because a guess about it would be a security decision made silently.

**Cost.** More to write down. Even an unwritten `enabled` is deliberate: it means "follow the top", not "unset".

**Refused.** Version ranges (an error, not a warning), implicit exposure, dependency aliases, anything auto-detected.

### Secure by default

Nothing is reachable until you say so. No port mapping means no outside access; no `expose` means no Ingress; `private` means invisible without authorization.

**Why.** The safe state should be the one you get by writing nothing. The CLI runs and exits with no listening port and never mounts the Docker socket, so the platform adds no attack surface of its own.

**Where it stops.** This principle covers *reachability*, not every Kubernetes default. `serviceAccount.enabled` and `podSecurity: restricted` are opt-in, because either can stop an already-working component from starting, and that is a project-specific cost the platform can't weigh for you. Skip `serviceAccount: { enabled: true }` and every Pod runs under the namespace's `default` ServiceAccount, its token mounted as usual.

**Refused.** Anything that opens a path by default.

### Open source first

The CLI and the Market are open source. Closed-source components are distributed through the Market in a controlled way.

**Why.** A platform you hand your deployments to should be one you can read. Closed source is a commercial fact, not something to forbid, so the Market stores the Manifest and the image reference in place of a Git repository. The one thing it won't let stay closed is the contract: a closed-source component that provides an API must declare an `api-contract` artifact ([the marketplace guide](../guide/08-marketplace.md) shows the rejection).

**Cost.** Keeping a closed-source component closed is the publisher's job, and pulling an image hands over every byte it will run — see [Protecting closed-source components](../patterns/closed-source-image-hardening.md).

**Refused.** A proprietary CLI or Market.

### The platform provides tools, it doesn't make decisions for you

Mechanism, not policy. Multiple versions coexist by default; degradation logic belongs to the component; a breaking change is an organizational coordination problem, and version coexistence is only a physical buffer for it.

**Why.** `people-basic-1-0-0` and `people-basic-2-0-0` are two different DNS names, so coexistence costs the platform nothing, and there is nothing for it to ask "are you sure?" about — listing both in `brickkit.yaml` *is* the intent (AGENTS.md §9.3). What a component does when Redis is down — query the database, return an empty list, write to a file and retry — is business logic, and the right answer differs per component (AGENTS.md §9.7).

**Cost.** The platform won't save you from a data-layer incompatibility between v1 and v2, or from a missing fallback. Both are yours.

**Refused.** Weak-dependency degradation, communication governance (circuit breaking, rate limiting, retries), multi-tenancy.

### Install implies trust

The same model as npm, the VS Code Marketplace and GitHub: installing a component means trusting it. The platform does no upfront review — no scanning, no sandboxing, no static analysis — and steps in only afterwards: a component confirmed malicious is marked `blocked`, which stops new installs.

**Why.** An upfront review has either too many false positives, blocking legitimate components, or too many false negatives, missing carefully disguised malicious code. `blocked` is cheap and effective as a last line (AGENTS.md §9.11). The Market is a marketplace, not a vault.

**What it does provide.** Signing. The publisher signs with cosign; the installer verifies with nothing but the Go standard library. The public key has to live in *your* project (`installer.publicKeys`), not come from the Market — otherwise a compromised Market could swap the component and the key together and verification would still pass. With zero keys configured, verification is off entirely, and the CLI says so once. [Signing and the trust model](signing-and-trust.md) has the details.

**Cost.** You are responsible for who you install from.

**Refused.** Third-party security review as a platform service.

### configSchema is a spec sheet, not a security gate

`configSchema` documents a component's own configuration: types, defaults, and `enum`, `minimum`, `maximum` and `pattern` (parsed and stored, never enforced). The CLI does not validate the *values* you put in `config`. It does check the *key names*: a key that isn't in `properties` triggers a warning, with a guess at which one you meant.

**Why the line falls there.** It is drawn at whether there is a runtime safety net. A wrong *type* fails loudly — the component gets `"abc"`, calls `int()`, crashes, and you notice. A wrong *key name* fails silently: the variable never appears, the component falls into its default branch and runs perfectly normally, just not the way you configured it. So the platform catches the failure that would otherwise be invisible and leaves the visible one alone. It is also a single existence check with no follow-up questions, unlike value validation, where opening the door means answering whether to check `enum`, `minimum`, `pattern`, and eventually all of JSON Schema inside the CLI (AGENTS.md §9.12).

**One narrow exception.** A key listed in `required` that has no `default` blocks `up`. The rule is the same: a missing item would otherwise silently never exist, and the component would run looking healthy with one call path that never works.

**Cost.** The declarations are not a guarantee. A user can fill a `minimum: 1` key with `0` and nothing at `up` stops them. What writing them buys is that a person or an AI *sees* the intended range; whether to error, clamp or fall back is the component's call.

**Refused.** Value validation of any kind.

### One component, one repository

No monorepo sub-directory components. A component is an independent unit of publishing (it has its own version — how would you even tag a monorepo?), of moving (`brickkit sync` moves the whole directory, `.git` and all), and of permission (its own visibility and publisher).

**Why.** Each of those three units breaks if two components share a repository (AGENTS.md §9.16).

**Cost.** None for the usual case: several parts of one piece of business — the proto, the backend code, the migration scripts — belong to the *same* component and don't need splitting. The rule is about components, not files.

**Refused.** Sub-directory components.

### `brickkit.yaml` is the declaration

Config is intent. Write it and it executes; the CLI never asks "are you sure?".

**Why.** Desired state lives in that file and actual state lives in Docker or Kubernetes; the CLI holds nothing between them. Each environment gets its own complete, self-contained file (`brickkit.prod.yaml`, selected with `--config`), with no overlay, inheritance or merge rules. Open one file and you see the whole picture, a Git diff is legible, and a change to a base layer can never silently affect production (AGENTS.md §9.9).

**Cost.** A few repeated lines across environment files. And what you wrote is what runs: narrowing the scope with `enabled: false` takes effect on the next `up`, and `git checkout brickkit.yaml` is how you widen it again.

**Refused.** Overlays and inheritance; confirmation prompts.

## Read further

- [Architecture overview](overview.md) — the pipeline these principles produce, and the table of what the platform deliberately doesn't do, with what you'd see if you built it anyway.
- [Core concepts](../concepts.md) — the service-naming rule everything is derived from.
- [Comparison with existing tools](../comparison.md) — where BrickKit overlaps with Compose, Helm, Kustomize and others, and where it solves a different problem.
- [AGENTS.md](../../../AGENTS.md) — the compressed form of this page (§4), the full refusal list (§4.1), and the twenty-three "why" justifications (§9).
