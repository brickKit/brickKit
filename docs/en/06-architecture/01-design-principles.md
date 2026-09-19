# Design principles and trade-offs

The [overview](00-overview.md) explains *what* the platform does. This page explains *why it is shaped this way*: the one idea that ties the design together; the engineering ideas BrickKit uses — and, just as deliberately, the ones it leaves alone — each explained from scratch, with no assumption that you've met them, and tied to the AI-development pain it answers, what BrickKit does, what it deliberately doesn't do, and how an AI copes; and the twelve principles every proposal is checked against.

[AGENTS.md](../../../AGENTS.md) carries the same twelve principles in compressed form, one line each, written so an AI assistant can judge a proposal against them. This page is the argument behind those lines: what each one means, what it buys, what it costs, and what it refused. Ignoring the 1–12 numbering, the twelve headings below match AGENTS.md §4 word for word, and `make lint` fails if they drift apart or the numbering slips.

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

## Meet the ideas

Everything BrickKit uses — and everything it deliberately doesn't — is an idea software engineering has had for a long time. You don't need to know any of them beforehand. Each idea below starts from what it is, then gives its upside and cost, the pain it maps to when you write code with an AI, what BrickKit does about it, what BrickKit deliberately does **not** do, and finally how an AI should cope.

**"What BrickKit doesn't do"** means what the platform deliberately leaves alone, for the component's author (and an AI) to decide. It is a trade-off made on purpose, not a gap waiting to be filled — every item has a reason.

**How to read this:** start with the five overview tables below, grouped by theme. Click the name in the first column to jump to that idea's numbered write-up.

### A. How the system is split

| Idea | The AI-development pain | What BrickKit does | What BrickKit doesn't do | How an AI copes |
| --- | --- | --- | --- | --- |
| [1. Bounded contexts (DDD)](#1-bounded-contexts-ddd)<br>Draw a boundary per business area | The system is too big for an AI to read at once, so it guesses | One component = one business boundary, its interface in `component.yaml`; 200–3,500 lines, readable in one pass | Doesn't dictate how a component models its inside, or whether components share a database. Inside the boundary is the author's | People draw the boundaries (an AI can't make that domain call); the AI works inside one, one component at a time |
| [2. Microservices and service mesh](#2-microservices-and-service-mesh)<br>Many small services; a mesh manages their traffic | Service discovery, config centers, registration SDKs — all of it lands on the AI | Components are built and deployed on their own; addresses come from DNS + an env var, so the AI reads one variable | No registry, gateway, mesh, circuit breaking or rate limiting: each is something that must run permanently and be operated | Ask the AI explicitly to write retries, timeouts and fallbacks into the component's own code |
| [3. Hexagonal and layered architecture](#3-hexagonal-and-layered-architecture)<br>Keep the business core apart from the database and interfaces | An AI tends to pile layers of abstraction onto a simple feature | Not enforced; the [Go component template](../04-go-component-template.md) is a layered reference | How a component is organized inside is the author's. Once the platform manages the inside, it turns from an orchestrator into a framework | Ask for "just enough, no extra abstraction"; for layers, show it the Go template |

### B. How to keep an AI from guessing

| Idea | The AI-development pain | What BrickKit does | What BrickKit doesn't do | How an AI copes |
| --- | --- | --- | --- | --- |
| [4. Declarative and GitOps](#4-declarative-and-gitops)<br>Write what you want; keep it in Git | Having an AI write deployment scripts: many steps, hard to review | `brickkit.yaml` says only what you want; order, addresses, variables and deployment files are computed by the CLI, and the diff is the review | No continuous correction: `up` is a one-shot apply, the CLI exits, nothing keeps watching | Have it change only `brickkit.yaml`; run `up --dry-run` to see the plan, then `up`; never hand-edit running containers |
| [5. Convention over configuration](#5-convention-over-configuration)<br>The tool sets the rules; follow them and configure little | An AI has to guess variable names, ports and addresses — and a wrong guess often raises no error | Names come from fixed rules: `people/basic` → `PEOPLE_BASIC_ENDPOINT`, no guessing | Doesn't decide for you: version, exposure and enabling must all be written down, never guessed | Derive variable names by the rule instead of letting the AI invent them; make it write versions and exposure explicitly |
| [6. Twelve-factor configuration](#6-twelve-factor-configuration)<br>Keep config out of the code; pass it in as environment variables | The AI has to cope with config differences between environments | Addresses, database connections and config all arrive as env vars; same address format on Docker and Kubernetes, so nothing changes between them | No config center, no hot reload: change config, then `brickkit up` to restart | Read only with `os.environ.get()`; read `configSchema` first to see which keys exist |
| [7. Exact versions and lockfiles](#7-exact-versions-and-lockfiles)<br>Pin one specific version | An AI's breaking change drags down whatever depends on it | Exact versions only; the version is in the service name, so v1 and v2 run side by side without touching v1's callers | Whether data stays compatible between side-by-side versions is not the platform's business | For v2, add a new version entry instead of overwriting v1; data compatibility is on you and the AI |

### C. How the boundary is stated, and what happens on error

| Idea | The AI-development pain | What BrickKit does | What BrickKit doesn't do | How an AI copes |
| --- | --- | --- | --- | --- |
| [8. Contract-first](#8-contract-first)<br>Write the interface down as a file, then build | To call another component an AI can only dig through its source and guess | `artifacts` carry OpenAPI / protobuf contracts with the component; a closed-source component must still publish its contract | Doesn't dictate a format or check that the implementation matches the contract | Have it read the contract, not the source; whether they agree is up to contract tests you write |
| [9. Design by contract](#9-design-by-contract)<br>State what you require and guarantee; fail when broken | An AI doesn't know which settings a component accepts, or in what range | `configSchema` states the settings, types and ranges; a misspelled key in the project's config gets a CLI warning | Doesn't check values: a wrong value makes the component fail on its own, a wrong name is the one nobody would notice | Have it read `configSchema` before writing; put value checks in the component's startup |
| [10. Loud failure](#10-loud-failure)<br>On error, fail at once and visibly | An AI's error handling often covers only the happy path — a missing weak dependency, a failed migration | Unknown Manifest keys rejected; a missing weak dependency injects no variable at all; a missing required setting stops `up` | How to degrade after an error isn't the platform's call: every component's business differs | Ask explicitly for handling of a missing weak dependency and a failed migration; the entrypoint exits at once on an argument it doesn't know |

### D. Security

| Idea | The AI-development pain | What BrickKit does | What BrickKit doesn't do | How an AI copes |
| --- | --- | --- | --- | --- |
| [11. Least privilege](#11-least-privilege)<br>Grant only the access needed; the rest is closed | With a bug or a break-in, the more a component can reach, the bigger the damage | No port mapping means no outside access; no `expose` means no entry point; network policies can be generated from the dependency graph | Network policies and ServiceAccount isolation are opt-in: turning them on can stop a working component from starting, and that cost is yours to weigh | The AI declares only the dependencies it really needs; a person decides whether to turn those two on |
| [12. Supply-chain security](#12-supply-chain-security)<br>Check who published third-party code and that it wasn't altered | When an AI pulls in a third-party component, the source is hard to verify | Publishers sign with cosign; the installer verifies with the Go standard library; the trusted keys live in your own project | No upfront review (scanning, sandboxing): installing implies trust, and a bad component is marked `blocked` afterwards | A person confirms any new component and any new trusted key — don't leave those to the AI |

### E. How a component is written and tested inside

| Idea | The AI-development pain | What BrickKit does | What BrickKit doesn't do | How an AI copes |
| --- | --- | --- | --- | --- |
| [13. Type-driven development](#13-type-driven-development)<br>Use types so wrong code won't compile | Whether AI-written code is right can't be seen just by reading it | Nothing (the CLI is written in Go, but components aren't required to be) | No language restriction: anything that builds a container image is a component | Pick a language with type checking for the AI, and let the compiler be the first gate |
| [14. TDD and BDD](#14-tdd-and-bdd)<br>Write tests or behavior descriptions first, then the code | Code first and tests after, so the tests merely restate the code | Advice: [layered testing](../07-patterns/01-testing.md) and a spec-first order; `up --dry-run` checks the assembly without starting a container | How to test is the component's; the platform checks nothing. `--dry-run` checks the assembly, not the code | Have it write the spec and tests first and watch them fail, then the implementation; add the edge cases yourself |
| [15. Property-based testing](#15-property-based-testing)<br>State a rule that holds for any input; let a tool hunt for counterexamples | AI-written tests often cover only the cases it thought of | The platform uses it on itself (start decisions, start order, quota merging); for components it only gets a mention in the [data construction guide](../07-patterns/02-data-construction.md) | Neither required nor recommended for components: it applies narrowly | For a core invariant (a balance never goes negative) let the AI write one; otherwise don't bother |

---

### 1. Bounded contexts (DDD)

**What it is:** In a big system the same word often means different things in different parts of the business — a "customer" in ordering is the person placing the order, a "customer" in support is the person waiting for a reply. DDD (domain-driven design) draws boundaries around business areas; each one (a "bounded context") gets its own vocabulary, rules and data, and the contexts exchange information only through explicit interfaces. In an online shop, say, ordering, inventory and support could each be a context.

- **Upside:**
  - Different people can understand, change and release different pieces independently, and a change in one piece doesn't ripple into the rest.
  - Vocabulary doesn't contaminate: the same word can mean something precise in each context.
  - When something breaks, it's easier to tell which piece it belongs to.
- **Cost:**
  - A wrong boundary is hard to repair — too fine, and the pieces spend their time talking to each other; too coarse, and you're back to a tangle.
  - Drawing the boundaries takes real understanding of the business first.
  - Consistency across contexts is harder: when one change has to touch two of them, no single transaction covers both.
- **The AI-development pain:** Once a system is big, an AI can't read it all, doesn't know what a change will touch, and has to guess.
- **What BrickKit does:**
  - A component is one business boundary, with its own repository, Manifest (`component.yaml`), version and interface contract — an independent unit of publishing, versioning and permission.
  - The boundary is written in `component.yaml`: what it depends on, which ports it offers, which configuration and resources it needs. An AI reads the current component's `component.yaml` plus the contracts of what it depends on, and can start — no need to understand the whole system.
  - Components are small: the 10 real components the project ships range from 200 to 3,500 lines, readable in one pass.
  - A component shouldn't straddle two contexts; a context can be one component or several. Data that must hold together (say "a balance never goes negative", which constrains both accounts and ledger entries) belongs in the **same component**, because there is no transaction across components.
  - Whether to split can be settled with three questions (recommended criteria, not platform rules): is there an invariant between the two pieces of data that must hold at every moment? Do they need to be released independently, or belong to different people? After the split, can a caller write its calling code from the contract alone?
- **What BrickKit doesn't do:**
  - It doesn't dictate how a component models its inside (how aggregates and entities are drawn). That is component autonomy: the platform manages the boundary; inside it belongs to the author.
  - It doesn't enforce isolation: whether components share a database (isolated by schema or by a primary-key prefix, say) or get merged into one process (a `servedBy` shell) is your call, made for the business at hand. If they share one, mind this: the migration state table's primary key must include a component identifier, or the components' migrations will clobber each other.
  - The reason: once the platform starts prescribing the inside, it stops being an orchestrator and becomes a framework, and a framework can only produce components of one shape.
- **How an AI copes:**
  - Where the boundaries go is not something an AI can decide: how many components a feature should become, which operations need a transaction — those are domain judgments for you and the domain experts.
  - The AI works inside the boundaries you drew, one component at a time, and only moves on once that one runs.
  - Have it read a dependency's contract, not its source (see [8. Contract-first](#8-contract-first)).
  - The term-by-term mapping between DDD and BrickKit is in the [component design guidelines](../07-patterns/00-component-design.md#if-your-team-thinks-in-ddd).

---

### 2. Microservices and service mesh

**What it is:** Microservices means splitting one big program into many small services, each developed and deployed on its own and calling the others over the network. A service mesh is a layer of infrastructure dedicated to how those services talk: finding one another, spreading load, rate limiting, retrying, and circuit breaking (pausing calls to something that keeps failing). Like turning one large restaurant into a street of small shops.

- **Upside:**
  - Each shop can open and refit on its own, use different techniques, and be run by a different team.
  - One shop having trouble doesn't shut the whole street.
- **Cost:**
  - With more shops, someone has to look after the signposts (a registry: who is where), the entrances (a gateway: where outside requests go first) and the queues and limits (a mesh) — operating it gets far harder.
  - Calls now cross a network: jitter, timeouts and a callee that half-succeeded all become cases to handle.
  - Tracking a fault means visiting several shops.
- **The AI-development pain:** Service discovery, config centers, registration SDKs — all of it lands on the AI; and when a call between components fails, an AI often has written only the happy path.
- **What BrickKit does:**
  - Components are built, deployed and called independently; every component is a container (frontends too).
  - Addresses come from DNS plus an environment variable: a caller reads one variable (say `PEOPLE_BASIC_ENDPOINT`, whose value is `http://people-basic-1-0-0:8080`) and never has to understand service discovery. A component needs no registration SDK at all — zero intrusion.
  - What is needed is left to the underlying engine, which already provides it: load balancing is Kubernetes Service's own; health checks and restarts are Compose's `healthcheck` and Kubernetes Probes; if you need a gateway, `labels` are passed through verbatim to a tool like Traefik — the platform doesn't understand what a label means, it just passes it on.
- **What BrickKit doesn't do:**
  - **A registry:** the DNS that Docker Compose and Kubernetes already provide is service discovery, for free. Building one means keeping it highly available; and once a component adopts a home-made discovery SDK it is no longer "zero intrusion".
  - **A gateway, a mesh, load balancing:** routing rules differ by business, and forcing one shape on them fits nobody. If you bypass `labels` and hand-write a routing config full of versioned service names, it silently goes stale the moment a component bumps a version, and the platform won't tell you.
  - **Circuit breaking, rate limiting, retries:** thresholds and backoff strategies differ by business — a platform default would rarely suit every case and would limit the flexibility some component really needs. Wait for the platform to retry for you and nothing happens: one transient network blip surfaces as an unhandled exception.
  - **A config center, hot reload:** that is a whole heavy mechanism of long-lived connections, pushes and version comparison, while most basic configuration can only safely take effect on a restart anyway.
  - **Health-check polling:** Compose and Kubernetes already solve it; doing it in the platform would need either a background process or a client library embedded in every component.
  - The common reason: each is something that has to run permanently and be operated by someone, while BrickKit's CLI runs and exits. See [what the platform deliberately doesn't do, and why](00-overview.md#what-the-platform-deliberately-doesnt-do-and-why).
- **How an AI copes:**
  - Retries, timeouts, fallbacks and rate limits are the component's own code, so ask for them explicitly — an AI's error handling often covers only the happy path.
  - Have `/healthz` check only the process itself, not probe the database or another dependency: otherwise one dependency wobbling gets every upstream marked unhealthy and restarted together.
  - If you need a gateway, use `labels` passthrough rather than hand-writing routes with version numbers in them.
  - To call another component, read the address from the environment variable; don't let the AI build discovery itself.

---

### 3. Hexagonal and layered architecture

**What it is:** Both are ways of organizing the inside of a program. Layering cuts it into tiers (one that receives requests, one that handles the business, one that reads and writes data), each tier calling only the one below. Hexagonal architecture goes further: the business logic sits at the center, and everything external — the database, the web interface, the message queue — plugs in through interfaces agreed in advance. Like a standard interface between the kitchen and the delivery apps, the till and the suppliers, so changing supplier doesn't mean rewriting the recipes.

- **Upside:**
  - The business logic doesn't depend on a particular framework or database, so it's easier to test on its own (with a fake database, say).
  - It's easier to swap an external system out.
  - Concerns are separated, so you know which layer to look in.
- **Cost:**
  - You write a fair amount of extra interface and conversion code.
  - With more indirection, following one request end to end takes more hops.
  - In a small program it's like fitting a gearbox to a bicycle.
- **The AI-development pain:** An AI tends to pile many layers of abstraction onto a simple feature (over-engineering), and you have to ask for it to be simplified.
- **What BrickKit does:**
  - It enforces no internal structure. The platform specifies only how a component looks *at its boundary*: a Manifest, a health check, and reading environment variables (see the principle "[Component autonomy](#2-component-autonomy)").
  - There is a real layered reference: [writing a Go component](../04-go-component-template.md). It is a real component that depends on PostgreSQL and has migrations; `server.go` / `service.go` / `store.go` are the HTTP layer, the business layer and the data-access layer, alongside `config.go` (configuration read only from the environment) and `migrate.go`.
- **What BrickKit doesn't do:**
  - It doesn't dictate how to layer, or whether to use hexagonal. `brickkit new` generates only a `component.yaml` that passes validation — no code structure, and it doesn't pick a language for you.
  - The reason: once the platform manages the inside, it stops being an orchestrator and becomes a framework, and a framework can only produce components of one shape.
- **How an AI copes:**
  - Say "just enough" yourself: for example, "three layers — HTTP, business, data access — are enough; don't add an interface for something with a single implementation".
  - If you want layers, show it the Go template; that's more reliable than describing it in words.
  - If you want hexagonal, it has to be in what you ask of the AI — the platform won't choose it for you.

---

### 4. Declarative and GitOps

**What it is:** There are two ways to tell a tool what to do. Imperative means giving it the steps — first this, then that — like teaching a cook the recipe. Declarative means writing only what you want at the end and letting the tool work out how, like ordering from a menu. GitOps keeps that declaration in Git as the single source of truth, with a program that is always running to keep pulling the real system toward it — when the real state and the declaration disagree that is called "drift", and that program's job is to notice drift and correct it.

- **Upside:**
  - The declaration is the documentation: open the file and you know what the system should look like.
  - Once it's in Git, every change can be reviewed and rolled back.
  - Applying the same declaration again gives the same result.
- **Cost:**
  - Anything the declaration language can't express can't be done.
  - When something goes wrong you have to understand how a declaration turns into actual actions.
  - The "keep pulling it back" half needs a resident program watching all the time.
- **The AI-development pain:** Having an AI write deployment scripts means many steps, an order that matters, a copy per environment, and changes that are hard to review.
- **What BrickKit does:**
  - You write two kinds of declaration: each component's `component.yaml` says what it is and what it depends on; the project's `brickkit.yaml` says which versions you want, what is enabled and what is exposed. Together they form a graph, and everything else is *derived* from it by the CLI (see [the one idea underneath](#the-one-idea-underneath-declare-a-graph-derive-the-rest) at the top of this page): which components run, start order, service addresses, environment variables, `docker-compose.yaml` or Kubernetes manifests, network policies.
  - Each environment gets its own complete, self-contained file, chosen with `brickkit up --config brickkit.prod.yaml`.
  - `brickkit up --dry-run` starts nothing and prints the plan first, so you (and an AI) look before acting.
  - The only way to narrow what starts is to change `enabled`; there is no `--only`-style flag.
- **What BrickKit doesn't do:**
  - **Continuous correction:** `brickkit up` is a one-shot apply and the CLI exits; nothing keeps watching. If someone hand-edits a running container, no program changes it back, and it lines up again at the next `up`. Continuous correction needs a resident program that somebody has to operate. Build a daemon to fix "nobody remembers to run `up`" and you have re-created the single point of failure and attack surface BrickKit deliberately avoids (see the principle "[Platform minimalism](#1-platform-minimalism)").
  - **Multi-environment overlays (a base layer plus override layers):** an overlay makes you assemble "base, override, merge rules" in your head to understand the final config, and a Git diff can't tell you whether a change to the base ripples into another environment. BrickKit chooses one complete file per environment: what you see is what you get, and the cost is some repetition between the files.
  - **An "are you sure?" prompt:** `brickkit.yaml` is the declaration — write it and it executes.
- **How an AI copes:**
  - Have it change only `brickkit.yaml`, run `brickkit up --dry-run` to see the plan, then `up`.
  - Never hand-edit running containers: nothing changes them back, and they line up again only at the next `up`.
  - For several environments, have it edit each complete file separately — don't expect inheritance.

**Example:** the plan `brickkit up --dry-run` prints (an excerpt from [guide 2](../03-guide/02-what-runs.md)) — why each component starts and in what order, at a glance:

```
📋 组件状态计算：
   ✅ demo/hello@1.0.0   启动（demo/caller 需要）
   ✅ demo/caller@1.0.0  启动（顶层）
...
📋 启动顺序（拓扑排序）：
   1. demo-hello-1-0-0   无依赖
   2. demo-caller-1-0-0  ← 依赖 1
```

---

### 5. Convention over configuration

**What it is:** The tool decides a set of rules up front — what things are called, where they go, what happens by default — and if you follow them you write almost no configuration. Rails is the best-known example: a model called `User` maps to a `users` table by default, and if you go along with that you never write the mapping.

- **Upside:**
  - Less to write and less to read.
  - Anyone picking up your project knows where things are at a glance.
- **Cost:**
  - When the rules don't fit your situation there's nothing to negotiate.
  - Worse are conventions that work by guessing: the tool quietly decided for you, and when it goes wrong you can't tell what it guessed.
- **The AI-development pain:** An AI has to guess variable names, ports and service addresses; a wrong guess just means the variable quietly doesn't exist, often with no error at all.
- **What BrickKit does:** Fixed naming, all of it computed by rule, so there is nothing to guess and nothing to configure:

  | What | The rule | Example |
  | --- | --- | --- |
  | Service name | Component ID + exact version; `/` and `.` become `-`, all lowercase | `people/basic` 1.0.0 → `people-basic-1-0-0` |
  | Service address | `http://<service name>:<port>`, the same on Docker and Kubernetes | `http://people-basic-1-0-0:8080` |
  | Dependency address variable | `/` and `-` in the component ID become `_`, uppercase, plus `_ENDPOINT` | `PEOPLE_BASIC_ENDPOINT` |
  | Variable for an extra port | Add the port's name | `PEOPLE_BASIC_GRPC_ENDPOINT` |
  | A component's own config | A `configSchema` camelCase key becomes upper snake case | `defaultPageSize` → `DEFAULT_PAGE_SIZE` |
  | Resource connection variables | The resource's `kind` is the prefix | `DATABASE_*`, `REDIS_*` … |

  The rule runs **both ways**: from `people/basic` you can compute the variable name, and from `PEOPLE_BASIC_ENDPOINT` you know exactly which component it points at. Defaults are conventions too: no `expose` means not reachable from outside, and no `enabled` means follow whatever is above.
- **What BrickKit doesn't do:**
  - **Decide for you:** which version, which port to expose, whether to enable a component — the platform never guesses these, and you have to write them down (see the principle "[Explicit over implicit](#5-explicit-over-implicit)"). That is why BrickKit's own phrase is "derivation over configuration": what can be *computed* from what you wrote is derived; what can't, you write.
  - **Dependency aliases:** someone proposed letting a dependency have an alias (`as: iam`). An alias keeps only half of that two-way relationship — `IAM_ENDPOINT` can't be traced back to the component it points at, and that is exactly where an investigation into "why is this address wrong" starts.
  - The cost is real too: a component ID becomes, unchanged, the variable name in every caller's code, so name it with the domain's own words, not implementation words.
- **How an AI copes:**
  - Derive variable names by the rule; don't let the AI go on impression. Have it read `dependencies` in `component.yaml` first, then derive the names.
  - Make it write down decisions such as the version, whether something is exposed, and whether it is enabled.
  - When naming a component ID, use business words.

---

### 6. Twelve-factor configuration

**What it is:** "Twelve-factor" is a checklist of lessons for building apps that run in the cloud. Three of its rules concern configuration: config lives in the environment (factor III), backing services such as a database or cache are treated as attached resources you can swap (IV), and development and production stay as alike as possible (X). The most quoted is the first: don't hard-code configuration (the database address, a password, where another service lives) in the code; read it from environment variables when the program starts.

- **Upside:**
  - One image runs in test and in production, and the only difference is which environment variables it was started with.
  - Passwords stay out of the repository.
  - Every language can read environment variables, with no library.
- **Cost:**
  - Environment variables are flat strings, so anything structured has to be encoded and decoded by hand.
  - Past a certain count they're hard to keep track of, and a missing one often shows up only when the program needs it.
  - A config change needs a restart to take effect.
- **The AI-development pain:** The AI has to cope with config differences between environments, and can easily hard-code an address or a password.
- **What BrickKit does:**
  - Everything a component needs to know reaches it as environment variables: dependency addresses (`*_ENDPOINT`), platform-wide variables (`COMPONENT_ID`, `COMPONENT_VERSION`), connection details for resources such as databases (`DATABASE_*`, `REDIS_*` and so on), and the component's own configuration (`configSchema` keys turned into upper snake case).
  - The address format is identical on Docker and Kubernetes; moving between them changes one field (`deploy.target`) and no component code.
  - `up` checks, moving the "found out too late" cost up to deploy time: a misspelled config key warns and guesses which one you meant; a setting that is required, has no default and isn't set in the project stops `up`; a config key that collides with a reserved platform variable warns and is skipped (the platform's value wins).
  - The full list of variables is in the [environment variable contract](04-environment-variables.md).
- **What BrickKit doesn't do:**
  - **A config center, hot reload:** that is a whole heavy mechanism of long-lived connections, pushes and version comparison, and settings such as connection-pool size and timeouts only take effect safely on a restart anyway. Change `brickkit.yaml`, then `brickkit up`. If a business toggle truly needs millisecond-level updates, the component can poll Redis itself.
  - **Validating a config value:** `configSchema` is only a spec sheet — see [9. Design by contract](#9-design-by-contract).
- **How an AI copes:**
  - Read only with `os.environ.get()` (`System.getenv()` in Java), and read `configSchema` first to see which keys exist and what the defaults are.
  - A weak dependency's variable may not exist at all; never write `os.environ["X"]`, or a missing one crashes the component at startup (see [10. Loud failure](#10-loud-failure)).
  - **A pitfall that has been checked:** a setting declared as `array` or `object` arrives in the container **not as JSON** — `["cn-east", "cn-north"]` becomes `[cn-east cn-north]`, which `json.loads` can't parse. When you need structured config, declare it `type: string`, make the value a JSON string, and parse it in the component's own code.
  - After a config change, `brickkit up` restarts the component so it takes effect.

**Example:** how a component reads configuration — a required dependency, a weak dependency, its own setting:

```python
import os

people = os.environ.get("PEOPLE_BASIC_ENDPOINT")             # required dependency: always there
bus = os.environ.get("INFRA_REDIS_EVENT_BUS_ENDPOINT")       # weak dependency: absent when it isn't running
page_size = int(os.environ.get("DEFAULT_PAGE_SIZE", "20"))   # its own setting: a default as backstop

if not bus:
    ...  # degrade: how to degrade is the component's own business logic
```

---

### 7. Exact versions and lockfiles

**What it is:** When you depend on someone else's software, you can write the version as a range (say `^1.0.0`, meaning "the latest 1.x is fine") or pin one specific version (`1.0.0`, in the form `major.minor.patch`). A lockfile (npm's `package-lock.json`, for example) records the specific versions the first install produced, so later installs reproduce them.

- **Upside:**
  - What you install today is the same as what you install three months from now — production doesn't quietly change at 3 a.m. because someone published a release.
  - When something breaks you know exactly which version was running, and a rollback has a clear target.
- **Cost:**
  - Patches and security fixes don't arrive on their own; you edit a line to upgrade.
  - Left alone for a long time, you fall a long way behind.
- **The AI-development pain:** An AI's breaking change (a new version incompatible with the old) drags down everything that depends on it; it may also casually write `^1.0.0` or `latest`.
- **What BrickKit does:**
  - Only exact versions are accepted: `^1.0.0`, `~1.0.0` and `latest` are all errors at resolve time, not surprises at runtime.
  - When you run `brickkit add people/basic` with no version, the CLI looks up the latest installable version once, at that moment, and writes it into the config as an **exact version** — which amounts to generating a lockfile. What differs from writing a range in the config is *when* it resolves: once, not on every run.
  - The version becomes part of the service name: `people-basic-1-0-0` and `people-basic-2-0-0` are two different DNS names that run side by side without colliding.
  - The variable *name* carries no version, but the variable *value* does: `DEPARTMENT_TREE_ENDPOINT=http://department-tree-1-0-0:8080`. A caller always knows which version it is talking to; there is no implicit upgrade.
  - See [core concepts](../01-concepts.md).
- **What BrickKit doesn't do:**
  - **Resolving version ranges:** a range is a classic cause of "works on my machine, breaks in production".
  - **Two versions of one component inside another component:** within a single `component.yaml`'s `dependencies`, one component ID can appear only once — the variable name carries no version, so two would collide on the same `*_ENDPOINT` and the later would silently overwrite the earlier. Side-by-side coexistence is therefore a **project-level** capability, not a component-level one: `brickkit.yaml` can list two version entries so different callers each use their own.
  - **Data compatibility between side-by-side versions:** the platform offers only a physical buffer; a breaking change is an organizational coordination problem, and whether the data layer stays compatible is your responsibility.
  - It won't ask "are you sure?" either: if `brickkit.yaml` lists two versions, that is your intent.
- **How an AI copes:**
  - When it writes v2, have it add a new version entry and run it beside v1, then move callers over gradually; don't overwrite v1.
  - Have it always write exact versions and reject `^` or `latest`.
  - When two versions share a database, whether a migration stays backward compatible is up to you and the AI; the migration state table's primary key must include a component identifier.

**Example:** two versions side by side need only one entry each in `brickkit.yaml` (the full walkthrough is in [guide 5](../03-guide/05-upgrades-and-versions.md)):

```yaml
components:
  - id: demo/hello
    version: 2.0.0
  - id: demo/hello
    version: 1.0.0
```

---

### 8. Contract-first

**What it is:** When two teams have to work together, first write down what the interface looks like as a file (OpenAPI is common for HTTP interfaces, protobuf for gRPC), and let both sides build against it — rather than finishing one side and making the other guess. Like agreeing the shape of the plug first.

- **Upside:**
  - Both sides can work in parallel.
  - You can tell what a component promises without reading its implementation.
- **Cost:**
  - Someone has to keep the contract up to date.
  - A contract that no longer matches the implementation is worse than none — readers trust a description that has gone stale.
- **The AI-development pain:** To call another component, an AI can only dig through its source and guess. And what's in the source isn't a promise to the outside: the moment the dependency is refactored, the AI's calling code breaks.
- **What BrickKit does:**
  - A component ships its contract files with the Manifest through `artifacts`, and `brickkit add` and `brickkit fetch` download them. `fetch` downloads only the artifacts — it doesn't touch `brickkit.yaml` and deploys nothing — for calling a service in another project.
  - The Market requires a closed-source component that provides an API to include an `api-contract` artifact: the code may be closed, the contract may not.
  - The platform's one job is to make sure what's listed under `files` really reaches whoever wants it.
- **What BrickKit doesn't do:**
  - It doesn't dictate a format: `type` and `format` are free-form strings the CLI never parses (OpenAPI, protobuf, anything of your own). Parsing formats would mean the platform first had to understand OpenAPI and protobuf.
  - It doesn't check that an implementation actually matches its contract. Whether they agree is the component's own matter.
- **How an AI copes:**
  - Have it read the dependency's contract file, not its source — so the dependency can be refactored freely without breaking it.
  - Write the contract first and the implementation after; whether the two agree is verified by contract tests you write yourself (L1 in [layered testing](../07-patterns/01-testing.md)).
  - See the [AI-assisted development guide](../05-ai-development.md).

**Example:** an artifact declaration (from `demo/hello`; see [guide 7](../03-guide/07-consuming-artifacts.md)):

```yaml
artifacts:
  - type: api-docs
    format: openapi
    description: HTTP API 文档
    files:
      - openapi.json
```

---

### 9. Design by contract

**What it is:** Every function or module states "what I require of my input and what I guarantee about my output", and fails immediately when either is broken. Think of the rating plate on an appliance — "input 220V, output 12V" — and a breaker that trips if you wire it wrong.

- **Upside:**
  - The error is caught closest to its cause.
  - The rules double as the most precise documentation there is.
- **Cost:**
  - Rules that are too strict shut out legitimate uses nobody anticipated; rules that are too loose might as well not exist.
  - Checking on every run has a price.
  - The rules themselves need maintaining.
- **The AI-development pain:** An AI doesn't know which settings a component accepts or in what range, so it goes on impression.
- **What BrickKit does:** Only the "write it down" half. The nearest thing BrickKit has is `configSchema` — strictly a relative, since it describes and doesn't enforce.
  - A component uses it to state which settings it has, their types, defaults and allowed ranges (`enum`, `minimum`, `maximum`, `pattern`), where both people and AI can read them.
  - The CLI checks that a key's *name* in the project's config exists in `configSchema`: a typo warns and guesses which key you meant. If a component declares no `configSchema` at all but the project writes `config` anyway, the whole block has no effect, and that warns too.
  - A setting that is required (`required`), has no default and isn't set in the project stops `up`, naming exactly which component and which item.
- **What BrickKit doesn't do:**
  - **It doesn't check a setting's *value*.** A setting declared `type: integer` that is filled in with a string isn't stopped by the CLI; the component fails when it calls `int()` on it — the discovery happens at runtime, not at `brickkit up`.
  - The line falls where a runtime safety net exists or doesn't: a wrong value makes the component fail on its own, so you'll certainly notice; a wrong name just means the variable quietly doesn't exist and the component takes its default branch and runs normally, with nothing to tell you. So names are checked and values are not.
  - And once you start validating values you can't stop — enums? ranges? regexes? `required`? JSON Schema's power is enormous, and the CLI would keep growing. What to do about a bad value (fail, degrade, fall back to a default) is also the component's call.
  - See the principle "[configSchema is a spec sheet, not a security gate](#10-configschema-is-a-spec-sheet-not-a-security-gate)".
- **How an AI copes:**
  - Have it read `configSchema` before writing the code that reads configuration; names and defaults follow it.
  - Have it put value checks (ranges, enums) in the component's startup, exit at once on failure, and give a clear error.
  - Don't let it treat `minimum` / `maximum` in `configSchema` as something the platform will enforce.

**Example:** a `configSchema`, and the CLI's real warning when a key is misspelled in the project (from the demo in [layered testing](../07-patterns/01-testing.md), where `greeting` was written `greetting`):

```yaml
configSchema:
  type: object
  properties:
    defaultPageSize:
      type: integer
      default: 20
      minimum: 1
      maximum: 100
      description: 列表接口默认每页条数
```

```
⚠️ config 里有配置项不会生效：组件 demo/hello 的 greetting
   配置项：greetting
   原因：组件的 configSchema 里没有这一项，是不是想写 greeting？
   影响：这一项不会被注入任何环境变量；组件会使用它自己的默认值
```

---

### 10. Loud failure

**What it is:** When a program finds something wrong, it stops at once and says so, instead of pretending nothing happened and carrying on. Often called fail fast.

- **Upside:**
  - Errors surface early and close to their cause.
  - The worst bug isn't a crash, it's a quietly wrong answer that looks fine: a crash you're sure to notice, a wrong answer you may not.
- **Cost:**
  - Stricter means more rejections: a file with one unknown field, or a component that wasn't written defensively, might have limped along before and now simply won't start.
- **The AI-development pain:** An AI's error handling often covers only the happy path, and edge cases such as a missing weak dependency or a failed migration get left out.
- **What BrickKit does:** It moves errors up to the moment of `up`, and says where and why:
  - An unrecognized key in a Manifest is rejected, not silently ignored (a Manifest has no extension-field mechanism).
  - A version range in a dependency is an error at resolve time.
  - A missing required dependency makes `up` refuse to generate anything; a misspelled config key warns; a setting that is required, has no default and isn't set in the project stops `up`.
  - A failed migration keeps the main service from starting; a port conflict is an error.
  - **The clearest case is a weak dependency:** when one is missing, the platform injects *no variable at all* rather than an empty string.
- **What BrickKit doesn't do:**
  - **Decide how a component degrades.** If Redis goes down, one component queries the database, another returns an empty list, another writes to a local file and retries later — that is pure business judgment, and the platform has no universal definition of "degrade". Its only action is to not inject the variable.
  - The cost is real too: a component that reads that variable with `os.environ["X"]` crashes at startup when it's absent. That is deliberate: better to crash loudly at startup than to be quietly wrong at runtime.
- **How an AI copes:**
  - Ask explicitly for handling of the two edges it most often leaves out: a missing weak dependency and a failed migration.
  - Read a weak dependency's variable with `.get()` and say what happens when it's absent.
  - Make the entrypoint exit at once on an argument it doesn't recognize. The migration container and the main service run from the same image, told apart only by a command-line argument; if the entrypoint "just starts the service" on an unknown argument, a mistyped migration command turns the migration container into a second service that never exits, and the whole deployment hangs at `Created` while its own logs say everything is ready.

**Example:** why a missing weak dependency must not become an empty string (AGENTS.md §9.13). A developer forgets to check for empty and writes `requests.get(f"{ENDPOINT}/healthz")`. If the injected value were an empty string, that becomes `/healthz` — the request hits the container's **own** port, and its own `/healthz` returns 200, so the developer wrongly concludes the weak dependency is healthy. That kind of bug is extremely hard to track down. So the platform makes the variable not exist, and lets the component crash loudly at startup.

---

### 11. Least privilege

**What it is:** Every program and every component gets only the access its job requires, and everything else is closed by default. Like a hotel key card that opens only your own room. In network security it is often mentioned together with "zero trust".

- **Upside:**
  - If a component is compromised — or just has a bug — what it can reach is limited, and the damage stays small.
- **Cost:**
  - Every permitted connection has to be written down.
  - Leave one out and you're in for a "why can't it connect?" investigation.
- **The AI-development pain:** With a bug or a break-in, the more a component can reach, the bigger the damage.
- **What BrickKit does:**
  - **Closed by default:** with no port mapping a component can't be reached from outside, and with no `expose` it has no external entry point (on Kubernetes, no Ingress is generated).
  - **Network policies:** on Kubernetes, `deploy.networkPolicy.enabled: true` computes network policies straight from the dependency graph — traffic is allowed only along the dependencies you declared, with no access-control list to maintain by hand. For callers outside the dependency graph (an Ingress controller, another team's namespace) you write the allowance with `allowFrom`. You can also turn on the outbound direction (`egress`) and say where the resources are in `allowTo`.
  - **ServiceAccount:** `serviceAccount: { enabled: true }` gives each component its own ServiceAccount, with no token mounted.
  - The CLI itself never mounts the Docker socket.
  - See [network policy and least privilege](../03-guide/11-network-policy.md).
- **What BrickKit doesn't do:**
  - **Turn them on by default:** network policies, ServiceAccount isolation and `podSecurity: restricted` can each stop a component that already works from starting — a real, project-specific cost the platform isn't in a position to weigh for you.
  - **Verify that a network policy takes effect:** many clusters accept these objects and enforce none of them — `kubectl apply` succeeds and `kubectl get networkpolicy` shows them, yet traffic flows freely, with no error. The default network plugin (CNI) of minikube and kind is one of these. Kubernetes has no API to ask, so the platform can't tell, and `brickkit up` warns you every time to check it yourself once.
- **How an AI copes:**
  - Have it declare only the dependencies it really needs: fewer dependencies, fewer open paths.
  - Write `expose` only when something must be served to the outside.
  - Whether to turn on network policies and ServiceAccount isolation is a person's decision; once on, verify as the guide does that "the authorized path still works and the unauthorized path really is blocked" — don't stop at a generated YAML that looks plausible.

**Example:** the network policy generated for `demo/hello` — `demo/caller` is its only caller, so it is the only source allowed, and the port is the one `demo/hello` itself listens on:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
spec:
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: demo-caller-1-0-0
      ports:
        - port: 8080
          protocol: TCP
  podSelector:
    matchLabels:
      app: demo-hello-1-0-0
  policyTypes:
    - Ingress
```

---

### 12. Supply-chain security

**What it is:** Is the third-party component you're installing really from the author it claims? Was it swapped on the way? Like a seal and a sender's signature on a parcel.

- **Upside:**
  - It stops impersonation.
  - It stops tampering in transit.
- **Cost:**
  - A signature proves who sent something and that it wasn't changed, not that what was sent is harmless.
  - You also have to manage which public keys you trust, and publishers have to keep their private keys safe.
- **The AI-development pain:** When an AI pulls in a third-party component, the source is hard to verify.
- **What BrickKit does:**
  - **Publishing:** publishers sign with cosign (`brickkit publish --sign --key ...`). What is actually signed is a "canonical payload" — the Manifest parsed and re-encoded by a fixed rule — so a changed comment, indentation or key order never invalidates the signature; a Manifest with duplicate keys is rejected outright, so two parsers can't read it two ways.
  - **Image digest:** `brickkit publish` by default resolves a mutable image tag to its digest before signing, so the signature covers a reference nobody can quietly repoint later; skipping that takes an explicit `--no-pin-digest` and prints a warning that names the risk.
  - **Installing:** the installer verifies with the Go standard library alone, so cosign needn't be installed. The public keys you trust go in your own project's `installer.publicKeys`, and `requireSignature` defaults to `true`.
  - **States and visibility:** a component version is `draft`, `stable`, `deprecated` or `blocked`, and its visibility is `public` or `private`.
  - See [signing and the trust model](06-signing-and-trust.md).
- **What BrickKit doesn't do:**
  - **Upfront review:** no scanning, no sandboxing, no static analysis. Installing implies trust — the same model as npm, the VS Code extension marketplace and GitHub. Upfront review either flags too many legitimate components or misses cleverly disguised malicious ones: expensive and unreliable. When a component is later confirmed malicious, the only tool is to mark it `blocked`, which stops new installs.
  - **Take public keys from the Market:** otherwise a compromised Market could swap the component and the key together, and verification would still pass — the Market issuing its own certificates to itself.
  - **Note:** with no public key configured at all, signature verification is **switched off entirely**, and `requireSignature: true` does nothing either (the CLI warns once).
- **How an AI copes:**
  - Have a person confirm any new component and any key added to `publicKeys` — don't leave those to the AI.
  - Don't let the AI turn off `requireSignature` or delete `publicKeys` just to "make the install pass".

---

### 13. Type-driven development

**What it is:** Use the language's type system to write the rules into the shape of the code, so a wrong way of writing it won't compile. For example, an order amount isn't just any number but a dedicated "amount" type, so you can't hand over a "quantity" where an "amount" is expected.

- **Upside:**
  - A whole class of mistakes is stopped at compile time instead of being found at runtime.
  - The types are documentation that can't go stale.
- **Cost:**
  - It depends on the language — not every type system is strong enough (a dynamic language can get part of the benefit from type annotations plus a checker, such as Python's annotations with mypy).
  - Designing the types takes effort of its own; done badly, they become a burden.
- **The AI-development pain:** AI-written code reads smoothly, but whether it is right can't be seen just by reading it — a machine has to check.
- **What BrickKit does:** Nothing, and it doesn't need to. BrickKit's own CLI is written in Go and benefits from type checking, but components aren't required to do the same.
- **What BrickKit doesn't do:**
  - **Restrict the language, as completely as anywhere:** anything that builds a container image will do. The fixtures in the repository already use three: `people/basic` is Python, `auth/password-login` is Go, and `portal/user-frontend` is plain HTML served by nginx. Being language-agnostic is one of the platform's core values.
  - **Provide an SDK:** the only trace of the platform in a component's code is reading environment variables. So everything the platform hands a component is a **string** (numbers and booleans included), and turning it into types is the component's own job.
- **How an AI copes:**
  - Pick a language with type checking for the AI, and let the compiler (or type checker) be the first gate: it must compile before anything else is discussed.
  - At startup, read the environment once into a typed config structure, and fail to start at once when a required item is missing rather than quietly falling back to a default. `config.go` in [writing a Go component](../04-go-component-template.md) does exactly this: a missing `DATABASE_HOST` or similar fails startup, lists everything missing in one go, and never falls back to `localhost` (see [10. Loud failure](#10-loud-failure)).
  - See [build your first component](../03-guide/10-build-your-own.md).

**Example:** how types make a wrong way of writing it fail to compile (Go):

```go
type Cents int64   // an amount, in cents
type Quantity int  // a quantity

func charge(amount Cents) { /* ... */ }

var qty Quantity = 3
charge(qty) // won't compile: a Quantity can't be used as Cents
```

---

### 14. TDD and BDD

**What it is:** TDD (test-driven development) means writing the test first and then the code that makes it pass: first the test goes red, then you write the implementation to turn it green. BDD (behavior-driven development) means describing the expected behavior first in near-everyday language — "given … when … then …" — and turning that into tests. Like writing the acceptance criteria before you start cooking.

- **Upside:**
  - The tests constrain what the code *should* do instead of restating what it *already* does.
  - Requirements become something you can check.
- **Cost:**
  - It takes time up front.
  - If the acceptance criteria are themselves wrong, you'll build the wrong thing very diligently.
- **The AI-development pain:** If an AI writes the implementation first and the tests after, the tests merely restate the code — they test only that "the code does what it does". AI-generated tests also tend to cover only the normal cases, and edge cases slip through.
- **What BrickKit does:** It doesn't enforce it, but it gives advice:
  - [Layered testing](../07-patterns/01-testing.md) covers how to test a component in layers: L1 contract tests (the shape of the interface), L2 business-rule tests (invariants, idempotency, legal state transitions), L3 unit tests (the branches of this implementation), L4 integration tests (real dependencies, end to end). One criterion separates L2 from L3: delete the implementation entirely and rewrite it in another language — should this test still hold? If so, it's L2.
  - It also gives a "spec first, implementation after" order (the table below). A BrickKit component happens to have three specs that don't depend on the implementation, all in `component.yaml`: `configSchema`, `dependencies` and the contract files.
  - What BrickKit adds is `brickkit up --dry-run`: it starts no container, yet checks whether `component.yaml` and `brickkit.yaml` agree (a misspelled config key, for instance).
- **What BrickKit doesn't do:**
  - It neither requires nor checks how you test: testing strategy has never been within the platform's remit — it's part of component autonomy.
  - `--dry-run` checks whether the **assembly** is right, not whether the **component's code** is.
  - And a word on the evidence: the layered-testing method comes from the retrospective of a real production deployment (14 components); the "spec first, implementation after" order is a recommendation that doesn't yet have the same field backing, so use it as you judge.
- **How an AI copes:**
  - Keep the order: spec first, then tests you watch fail, and only then let the AI write the implementation. The red has to be for a reason: a test that is green on its first run tested nothing.
  - Write each business rule first as one "given … when … then …" sentence, then turn it into a test. For example, "given an idempotency key not yet processed; when two requests carrying the same key arrive at the same time; then exactly one really executes and the other gets the same result" — that sentence is itself the most precise prompt you can give an AI.
  - An AI often tests only the normal cases; add the edge cases yourself, or ask for them explicitly.
  - Run L4 against real dependencies; don't trust only mocks.

**Example:** the recommended order, with real feedback at every step:

| Step | What to do | How you know it's right |
| --- | --- | --- |
| 1 | Write `component.yaml` in full: `dependencies`, `configSchema`, the contract files | `brickkit up --dry-run` (no container) |
| 2 | Write L1 contract tests from the contract file | They must all be red — and red for a reason |
| 3 | Write each L2 business rule as one sentence, then as a test | Again, red first |
| 4 | Have the AI write the implementation until L1 and L2 are green | The tests are the acceptance; nobody needs to read the implementation line by line |
| 5 | Add L3: the branches of this implementation | Every branch is reached |
| 6 | Run L4: `brickkit up` with real dependencies, walk the real path | End to end works |

---

### 15. Property-based testing

**What it is:** In an ordinary test you write a few examples by hand: input A should give B. Property-based testing takes a different approach: you state a rule that must hold *whatever the input* ("a sorted list is always in order and the same length"), and a tool generates thousands of random inputs looking for one that breaks it.

- **Upside:**
  - It finds edge cases you wouldn't have thought of.
  - When it finds a counterexample, the tool usually shrinks it to the smallest one that still reproduces.
- **Cost:**
  - Working out the right property isn't easy.
  - A random failure has to be reproducible, so you must record the random seed.
  - It fits poorly with code that leans heavily on external systems.
- **The AI-development pain:** AI-generated tests often cover only the normal cases it thought of.
- **What BrickKit does:** The platform uses it on itself. The three densest rule sets in the CLI each have tests driven by random input:
  - **Start decisions (`cascade`):** thousands of random small graphs (up to 6 components, covering the three states of `enabled`, strong and weak dependencies, weak cycles, and several upstream components sharing one) are checked against an independently written brute-force reference. The reference doesn't copy the platform's propagation algorithm; it enumerates every candidate set and takes the largest one that satisfies the documented rules one by one — the two sides share the rules, not the algorithm.
  - **Start order (`resolver`):** checks properties such as "a dependency comes before what depends on it", "each component exactly once", "removing every weak-dependency edge leaves the order unchanged", and "shuffling the input gives the same result".
  - **Resource-quota merging (`inject`):** checks field-by-field precedence, that `limits` has no default layer, and that merging the result in again changes nothing.
  - Everything uses a fixed random seed, and failure messages carry the seed so a failure reproduces exactly; no new dependency. To check that the tests really bite, two bugs were planted in each of the three implementations, and all six were caught; all three implementations also matched the documented rules.
  - For components it only gets a mention in the [data construction guide](../07-patterns/02-data-construction.md): property-based testing suits a "core invariant" (a balance never going negative, a state machine never reaching an invalid transition). It needs volume, but for a different reason than seed data — to maximize the chance that random inputs land on a boundary, not to give a complete experience.
- **What BrickKit doesn't do:**
  - It's neither required nor recommended for components: it applies fairly narrowly, mainly to core invariants; for most business code, ordinary example-based tests are enough.
  - It doesn't provide a property-testing framework or library: the platform itself uses the standard library's random numbers.
- **How an AI copes:**
  - When you have a core invariant, write it down first as one sentence ("an account balance is never negative"), then have the AI write the property test from it.
  - Otherwise, don't: don't use it for its own sake.

## The twelve principles

### 1. Platform minimalism

The CLI does nothing it doesn't have to. Every feature is maintenance forever, and the platform's whole job is to connect components and translate their declarations for Docker or Kubernetes.

**Why.** What the underlying engines already do well — DNS, health probes, restart policies, load balancing — is better done by them than by a platform-built copy. A platform that grows a runtime becomes one more thing you must keep alive.

**Cost.** The things people reach for first aren't here: no gateway, no config center, no registry. [The refusal table](00-overview.md#what-the-platform-deliberately-doesnt-do-and-why) says what to do instead, and what you'd see if you built one anyway.

**Refused.** A resident control plane, a registry, health-check polling, a config center, a service mesh.

---

### 2. Component autonomy

Language, framework, API style, transactions and log format belong to the component. The platform asks for exactly three things: a Manifest, a health check, and that the component reads its environment variables.

**Where the boundary is.** What affects how components work *with each other* — how they are declared, addressed, configured and depended on — the platform manages. What affects how a component is written *inside* — architecture, modeling, layering, testing — the platform doesn't enforce ([patterns/](../07-patterns/README.md) offers advice, but nothing checks it). That is not the same as asking nothing of the container: what the platform specifies is how a container looks *at the boundary*. The entrypoint must fail fast on an argument it doesn't recognize (the migration container and the main service run from the same image); a weak dependency's variable must be read with `.get()` (when the dependency is missing, the platform doesn't inject it at all); `/healthz` checks only the process itself; a cold start longer than 30 seconds needs `startPeriodSeconds`. Those are contracts at the boundary, not interference with how the code inside is structured. Once the platform starts managing what is inside, it stops being an orchestrator and becomes a framework — and a framework can only produce components of one shape.

**Why.** That is what makes "any language that can build a Docker image" true. It is also why a frontend component is just another container listening on a port: there is no "static" component type, and so no `if type == static` branches spreading through the CLI (AGENTS.md §9.10).

**Cost.** The platform can't help with anything inside the boundary — no shared SDK, no validated business config, no opinion on how you structure code.

**Refused.** SDKs, sidecars and agents; special component types; platform-defined degradation behavior.

---

### 3. Incremental

You never have to design the whole system first. Add one component and run it; add another and run that; stop and resume at any point.

**Why.** `brickkit add` pulls a component's whole dependency subtree in one command; `enabled` follows the top, so what nobody needs isn't running; `brickkit sync` keeps the source directory down to what you're actually looking at. This is also why fifty components doesn't mean fifty things to hold in your head: you only need the contract of your direct dependencies (usually one to three), a fifty-component project may run four containers locally, and the transitive dependencies are the CLI's problem (AGENTS.md §9.15).

**Cost.** The platform bounds what you must know *per component*, not how large the whole gets. Nothing stops you from growing something you can't picture in one sitting.

**Refused.** A design phase that must be complete before anything runs.

---

### 4. Environment-agnostic

Component code never learns whether it runs under Docker or Kubernetes. The address it reads is `http://<versioned-service-name>:<port>` in both, byte for byte.

**Why.** One Manifest, two environments. A component describes what it is and what it needs, never where it runs, so it ships no `docker-compose.yaml` and no Kubernetes manifests (AGENTS.md §9.4). Switching environments is one field, `deploy.target`.

**Cost.** The platform has to generate correct files for both engines, and anything only one engine can do isn't expressible from the component side. Engine-specific hints go through the `labels` passthrough, uninterpreted.

**Refused.** Per-environment component files; engine-specific fields in the Manifest.

---

### 5. Explicit over implicit

Exact versions, explicit exposure, explicit enabling. Anything the platform would have to guess, it makes you say.

**Why.** Version ranges are the classic cause of "works on my machine, breaks in production" (AGENTS.md §9.2). Pinning still happens once, at `add`, the way a lockfile does it: leave the version off and the CLI asks the source for the latest installable one and writes the exact result to disk. Exposure is opt-in because a guess about it would be a security decision made silently.

**Cost.** More to write down. Even an unwritten `enabled` is deliberate: it means "follow the top", not "unset".

**Refused.** Version ranges (an error, not a warning), implicit exposure, dependency aliases, anything auto-detected.

---

### 6. Secure by default

Nothing is reachable until you say so. No port mapping means no outside access; no `expose` means no Ingress; `private` means invisible without authorization.

**Why.** The safe state should be the one you get by writing nothing. The CLI runs and exits with no listening port and never mounts the Docker socket, so the platform adds no attack surface of its own.

**Where it stops.** This principle covers *reachability*, not every Kubernetes default. `serviceAccount.enabled` and `podSecurity: restricted` are opt-in, because either can stop an already-working component from starting, and that is a project-specific cost the platform can't weigh for you. Skip `serviceAccount: { enabled: true }` and every Pod runs under the namespace's `default` ServiceAccount, its token mounted as usual.

**Refused.** Anything that opens a path by default.

---

### 7. Open source first

The CLI and the Market are open source. Closed-source components are distributed through the Market in a controlled way.

**Why.** A platform you hand your deployments to should be one you can read. Closed source is a commercial fact, not something to forbid, so the Market stores the Manifest and the image reference in place of a Git repository. The one thing it won't let stay closed is the contract: a closed-source component that provides an API must declare an `api-contract` artifact ([the marketplace guide](../03-guide/08-marketplace.md) shows the rejection).

**Cost.** Keeping a closed-source component closed is the publisher's job, and pulling an image hands over every byte it will run — see [Protecting closed-source components](../07-patterns/04-closed-source-image-hardening.md).

**Refused.** A proprietary CLI or Market.

---

### 8. The platform provides tools, it doesn't make decisions for you

Mechanism, not policy. Multiple versions coexist by default; degradation logic belongs to the component; a breaking change is an organizational coordination problem, and version coexistence is only a physical buffer for it.

**Why.** `people-basic-1-0-0` and `people-basic-2-0-0` are two different DNS names, so coexistence costs the platform nothing, and there is nothing for it to ask "are you sure?" about — listing both in `brickkit.yaml` *is* the intent (AGENTS.md §9.3). What a component does when Redis is down — query the database, return an empty list, write to a file and retry — is business logic, and the right answer differs per component (AGENTS.md §9.7).

**Cost.** The platform won't save you from a data-layer incompatibility between v1 and v2, or from a missing fallback. Both are yours.

**Refused.** Weak-dependency degradation, communication governance (circuit breaking, rate limiting, retries), multi-tenancy.

---

### 9. Install implies trust

The same model as npm, the VS Code Marketplace and GitHub: installing a component means trusting it. The platform does no upfront review — no scanning, no sandboxing, no static analysis — and steps in only afterwards: a component confirmed malicious is marked `blocked`, which stops new installs.

**Why.** An upfront review has either too many false positives, blocking legitimate components, or too many false negatives, missing carefully disguised malicious code. `blocked` is cheap and effective as a last line (AGENTS.md §9.11). The Market is a marketplace, not a vault.

**What it does provide.** Signing. The publisher signs with cosign; the installer verifies with nothing but the Go standard library. The public key has to live in *your* project (`installer.publicKeys`), not come from the Market — otherwise a compromised Market could swap the component and the key together and verification would still pass. With zero keys configured, verification is off entirely, and the CLI says so once. [Signing and the trust model](06-signing-and-trust.md) has the details.

**Cost.** You are responsible for who you install from.

**Refused.** Third-party security review as a platform service.

---

### 10. configSchema is a spec sheet, not a security gate

`configSchema` documents a component's own configuration: types, defaults, and `enum`, `minimum`, `maximum` and `pattern` (parsed and stored, never enforced). The CLI does not validate the *values* you put in `config`. It does check the *key names*: a key that isn't in `properties` triggers a warning, with a guess at which one you meant.

**Why the line falls there.** It is drawn at whether there is a runtime safety net. A wrong *type* fails loudly — the component gets `"abc"`, calls `int()`, crashes, and you notice. A wrong *key name* fails silently: the variable never appears, the component falls into its default branch and runs perfectly normally, just not the way you configured it. So the platform catches the failure that would otherwise be invisible and leaves the visible one alone. It is also a single existence check with no follow-up questions, unlike value validation, where opening the door means answering whether to check `enum`, `minimum`, `pattern`, and eventually all of JSON Schema inside the CLI (AGENTS.md §9.12).

**One narrow exception.** A key listed in `required` that has no `default` blocks `up`. The rule is the same: a missing item would otherwise silently never exist, and the component would run looking healthy with one call path that never works.

**Cost.** The declarations are not a guarantee. A user can fill a `minimum: 1` key with `0` and nothing at `up` stops them. What writing them buys is that a person or an AI *sees* the intended range; whether to error, clamp or fall back is the component's call.

**Refused.** Value validation of any kind.

---

### 11. One component, one repository

No monorepo sub-directory components. A component is an independent unit of publishing (it has its own version — how would you even tag a monorepo?), of moving (`brickkit sync` moves the whole directory, `.git` and all), and of permission (its own visibility and publisher).

**Why.** Each of those three units breaks if two components share a repository (AGENTS.md §9.16).

**Cost.** None for the usual case: several parts of one piece of business — the proto, the backend code, the migration scripts — belong to the *same* component and don't need splitting. The rule is about components, not files.

**Refused.** Sub-directory components.

---

### 12. `brickkit.yaml` is the declaration

Config is intent. Write it and it executes; the CLI never asks "are you sure?".

**Why.** Desired state lives in that file and actual state lives in Docker or Kubernetes; the CLI holds nothing between them. Each environment gets its own complete, self-contained file (`brickkit.prod.yaml`, selected with `--config`), with no overlay, inheritance or merge rules. Open one file and you see the whole picture, a Git diff is legible, and a change to a base layer can never silently affect production (AGENTS.md §9.9).

**Cost.** A few repeated lines across environment files. And what you wrote is what runs: narrowing the scope with `enabled: false` takes effect on the next `up`, and `git checkout brickkit.yaml` is how you widen it again.

**Refused.** Overlays and inheritance; confirmation prompts.

## Read further

- [Architecture overview](00-overview.md) — the pipeline these principles produce, and the table of what the platform deliberately doesn't do, with what you'd see if you built it anyway.
- [Core concepts](../01-concepts.md) — the service-naming rule everything is derived from.
- [Comparison with existing tools](../02-comparison.md) — where BrickKit overlaps with Compose, Helm, Kustomize and others, and where it solves a different problem.
- [AGENTS.md](../../../AGENTS.md) — the compressed form of this page (§4), the full refusal list (§4.1), and the twenty-three "why" justifications (§9).
