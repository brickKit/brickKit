# Design principles and trade-offs

The [overview](overview.md) explains *what* the platform does. This page explains *why it is shaped this way*: the one idea that ties the design together; the engineering ideas BrickKit uses — and, just as deliberately, the ones it leaves alone — each explained from scratch, with no assumption that you've met them, and tied to the AI-development pain it answers, how BrickKit provides it, and why it deliberately leaves a blank; and the twelve principles every proposal is checked against.

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

## Meet the ideas

Everything BrickKit uses — and everything it deliberately doesn't — is an idea software engineering has had for a long time. You don't need to know any of them beforehand. Each idea below starts from what it is, then says what problem it maps to when you write code with an AI, how BrickKit provides it, why BrickKit deliberately does **not** provide the rest, and how an AI should adapt to that gap.

**"The blank"** means what the platform deliberately leaves alone, for the component's author (and an AI) to decide.

**How to read this:** start with the five overview tables below, grouped by theme. Click the name in the first column to jump to that idea's numbered write-up.

### A. How the system is split

| Idea | The AI-development pain | How BrickKit provides it | Why BrickKit doesn't (the blank) | How an AI adapts to the blank |
| --- | --- | --- | --- | --- |
| [1. Bounded contexts (DDD)](#1-bounded-contexts-ddd)<br>Draw a boundary per business area | The system is too big for an AI to read at once, so it guesses | One component = one business boundary, its interface in `component.yaml`; 200–3,500 lines, readable in one pass | Doesn't dictate how a component models its inside, or whether components share a database. Inside the boundary is the author's | People draw the boundaries (an AI can't make that domain call); the AI works inside one, one component at a time |
| [2. Microservices and service mesh](#2-microservices-and-service-mesh)<br>Many small services; a mesh manages their traffic | Service discovery, config centers, registration SDKs — all of it lands on the AI | Components are built and deployed on their own; addresses come from DNS + an env var, so the AI reads one variable | No registry, gateway, mesh, circuit breaking or rate limiting: each is something that must run permanently and be operated | Ask the AI explicitly to write retries, timeouts and fallbacks into the component's own code |
| [3. Hexagonal and layered architecture](#3-hexagonal-and-layered-architecture)<br>Keep the business core apart from the database and interfaces | An AI tends to pile layers of abstraction onto a simple feature | Not enforced; the [Go component template](../go-component-template.md) is a layered reference | How a component is organized inside is the author's. Once the platform manages the inside, it turns from an orchestrator into a framework | Ask for "just enough, no extra abstraction"; for layers, show it the Go template |

### B. How to keep an AI from guessing

| Idea | The AI-development pain | How BrickKit provides it | Why BrickKit doesn't (the blank) | How an AI adapts to the blank |
| --- | --- | --- | --- | --- |
| [4. Declarative and GitOps](#4-declarative-and-gitops)<br>Write what you want; keep it in Git | Having an AI write deployment scripts: many steps, hard to review | `brickkit.yaml` says only what you want; order, addresses, variables and deployment files are computed by the CLI, and the diff is the review | No continuous correction: `up` is a one-shot apply, the CLI exits, nothing keeps watching | Have it change only `brickkit.yaml`; run `up --dry-run` to see the plan, then `up`; never hand-edit running containers |
| [5. Convention over configuration](#5-convention-over-configuration)<br>The tool sets the rules; follow them and configure little | An AI has to guess variable names, ports and addresses — and a wrong guess often raises no error | Names come from fixed rules: `people/basic` → `PEOPLE_BASIC_ENDPOINT`, no guessing | Doesn't decide for you: version, exposure and enabling must all be written down, never guessed | Derive variable names by the rule instead of letting the AI invent them; make it write versions and exposure explicitly |
| [6. Twelve-factor configuration](#6-twelve-factor-configuration)<br>Keep config out of the code; pass it in as environment variables | The AI has to cope with config differences between environments | Addresses, database connections and config all arrive as env vars; same address format on Docker and Kubernetes, so nothing changes between them | No config center, no hot reload: change config, then `brickkit up` to restart | Read only with `os.environ.get()`; read `configSchema` first to see which keys exist |
| [7. Exact versions and lockfiles](#7-exact-versions-and-lockfiles)<br>Pin one specific version | An AI's breaking change drags down whatever depends on it | Exact versions only; the version is in the service name, so v1 and v2 run side by side without touching v1's callers | Whether data stays compatible between side-by-side versions is not the platform's business | For v2, add a new version entry instead of overwriting v1; data compatibility is on you and the AI |

### C. How the boundary is stated, and what happens on error

| Idea | The AI-development pain | How BrickKit provides it | Why BrickKit doesn't (the blank) | How an AI adapts to the blank |
| --- | --- | --- | --- | --- |
| [8. Contract-first](#8-contract-first)<br>Write the interface down as a file, then build | To call another component an AI can only dig through its source and guess | `artifacts` carry OpenAPI / protobuf contracts with the component; a closed-source component must still publish its contract | Doesn't dictate a format or check that the implementation matches the contract | Have it read the contract, not the source; whether they agree is up to contract tests you write |
| [9. Design by contract](#9-design-by-contract)<br>State what you require and guarantee; fail when broken | An AI doesn't know which settings a component accepts, or in what range | `configSchema` states the settings, types and ranges; a misspelled key in the project's config gets a CLI warning | Doesn't check values: a wrong value makes the component fail on its own, a wrong name is the one nobody would notice | Have it read `configSchema` before writing; put value checks in the component's startup |
| [10. Loud failure](#10-loud-failure)<br>On error, fail at once and visibly | An AI's error handling often covers only the happy path — a missing weak dependency, a failed migration | Unknown Manifest keys rejected; a missing weak dependency injects no variable at all; a missing required setting stops `up` | How to degrade after an error isn't the platform's call: every component's business differs | Ask explicitly for handling of a missing weak dependency and a failed migration; the entrypoint exits at once on an argument it doesn't know |

### D. Security

| Idea | The AI-development pain | How BrickKit provides it | Why BrickKit doesn't (the blank) | How an AI adapts to the blank |
| --- | --- | --- | --- | --- |
| [11. Least privilege](#11-least-privilege)<br>Grant only the access needed; the rest is closed | With a bug or a break-in, the more a component can reach, the bigger the damage | No port mapping means no outside access; no `expose` means no entry point; network policies can be generated from the dependency graph | Network policies and ServiceAccount isolation are opt-in: turning them on can stop a working component from starting, and that cost is yours to weigh | The AI declares only the dependencies it really needs; a person decides whether to turn those two on |
| [12. Supply-chain security](#12-supply-chain-security)<br>Check who published third-party code and that it wasn't altered | When an AI pulls in a third-party component, the source is hard to verify | Publishers sign with cosign; the installer verifies with the Go standard library; the trusted keys live in your own project | No upfront review (scanning, sandboxing): installing implies trust, and a bad component is marked `blocked` afterwards | A person confirms any new component and any new trusted key — don't leave those to the AI |

### E. How a component is written and tested inside

| Idea | The AI-development pain | How BrickKit provides it | Why BrickKit doesn't (the blank) | How an AI adapts to the blank |
| --- | --- | --- | --- | --- |
| [13. Type-driven development](#13-type-driven-development)<br>Use types so wrong code won't compile | Whether AI-written code is right can't be seen just by reading it | Nothing (the CLI is written in Go, but components aren't required to be) | No language restriction: anything that builds a container image is a component | Pick a language with type checking for the AI, and let the compiler be the first gate |
| [14. TDD and BDD](#14-tdd-and-bdd)<br>Write tests or behavior descriptions first, then the code | Code first and tests after, so the tests merely restate the code | Advice: [layered testing](../patterns/testing.md) and a spec-first order; `up --dry-run` checks the assembly without starting a container | How to test is the component's; the platform checks nothing. `--dry-run` checks the assembly, not the code | Have it write the spec and tests first and watch them fail, then the implementation; add the edge cases yourself |
| [15. Property-based testing](#15-property-based-testing)<br>State a rule that holds for any input; let a tool hunt for counterexamples | AI-written tests often cover only the cases it thought of | The platform uses it on itself (start decisions, start order, quota merging); for components it only gets a mention in the [data construction guide](../patterns/data-construction.md) | Neither required nor recommended for components: it applies narrowly | For a core invariant (a balance never goes negative) let the AI write one; otherwise don't bother |

---

### 1. Bounded contexts (DDD)

**What it is:** In a big system the same word often means different things in different parts of the business — a "customer" in ordering is the person placing the order, a "customer" in support is the person waiting for a reply. DDD (domain-driven design) draws boundaries around business areas, and each one (a "bounded context") gets its own vocabulary, rules and data.

- **Upside:** Different people can understand and change different pieces independently; when something breaks, it's easier to tell which piece it belongs to.
- **Cost:** A wrong boundary is hard to repair — too fine, and the pieces spend their time talking to each other; too coarse, and you're back to a tangle. Drawing the boundaries also takes real understanding of the business first.
- **The AI-development pain:** Once a system is big, an AI can't read it all and has to guess.
- **How BrickKit provides it:** A component is one business boundary, with its own repository, Manifest, version and interface contract. The 10 real components the project ships range from 200 to 3,500 lines, so an AI can read one in a single pass. A component shouldn't straddle two contexts; a context can be one component or several.
- **Why BrickKit doesn't provide the rest:** It doesn't dictate how a component models its inside, or whether components share a database or get merged into one process (`servedBy`). The platform manages the boundary; inside it belongs to the author (see [component autonomy](#component-autonomy)).
- **How an AI adapts:** Where the boundaries go is not something an AI can decide — it's a domain judgment for you and the domain experts. The AI works inside the boundaries you drew, one component at a time. The term-by-term mapping is in the [component design guidelines](../patterns/component-design.md#if-your-team-thinks-in-ddd).

---

### 2. Microservices and service mesh

**What it is:** Microservices means splitting one big program into many small services, each developed and deployed on its own and calling the others over the network. A service mesh is a layer of infrastructure dedicated to how those services talk: finding one another, spreading load, rate limiting, retrying, and circuit breaking (pausing calls to something that keeps failing). Like turning one large restaurant into a street of small shops.

- **Upside:** Each shop can open and refit on its own; one shop having trouble doesn't shut the whole street.
- **Cost:** With more shops, someone has to look after the signposts (a registry), the entrances (a gateway), and the queues and limits (a mesh) — operating it gets far harder, and tracking a fault means visiting several shops.
- **The AI-development pain:** Service discovery, config centers, registration SDKs — all of it lands on the AI to handle.
- **How BrickKit provides it:** Components are built, deployed and called independently. Addresses come from DNS plus an environment variable, so an AI reads a single variable (say `PEOPLE_BASIC_ENDPOINT`) and never has to understand service discovery.
- **Why BrickKit doesn't provide the rest:** No registry (the DNS that Docker Compose and Kubernetes already provide is the service discovery), no gateway, no mesh, no circuit breaking or rate limiting, no config center. Each is something that has to run permanently and be operated, while BrickKit's CLI runs and exits; thresholds and backoff strategies also differ by business, so one platform-wide setting can't be right. If you do need a gateway (Traefik, say), `labels` are passed through verbatim. See [what the platform deliberately doesn't do, and why](overview.md#what-the-platform-deliberately-doesnt-do-and-why).
- **How an AI adapts:** Retries, timeouts and fallbacks are the component's own code, so ask for them explicitly — an AI's error handling often covers only the happy path.

---

### 3. Hexagonal and layered architecture

**What it is:** Both are ways of organizing the inside of a program. Layering cuts it into tiers (one that receives requests, one that handles the business, one that reads and writes data), each tier calling only the one below. Hexagonal architecture goes further: the business logic sits at the center, and everything external — the database, the web interface, the message queue — plugs in through interfaces agreed in advance. Like a standard interface between the kitchen and the delivery apps, the till and the suppliers, so changing supplier doesn't mean rewriting the recipes.

- **Upside:** The business logic doesn't depend on a particular framework or database, so it's easier to test on its own and easier to swap an external system out.
- **Cost:** You write a fair amount of extra interface and conversion code; in a small program it's like fitting a gearbox to a bicycle.
- **The AI-development pain:** An AI tends to pile many layers of abstraction onto a simple feature (over-engineering).
- **How BrickKit provides it:** It doesn't enforce it. There is a real layered reference: [writing a Go component](../go-component-template.md) (an HTTP layer, a business layer and a data-access layer).
- **Why BrickKit doesn't provide the rest:** How a component is organized inside is the author's. The platform specifies only how a component looks *at its boundary* (see [component autonomy](#component-autonomy)), and once it starts managing the inside it stops being an orchestrator and becomes a framework.
- **How an AI adapts:** Ask for "just enough, no extra abstraction" — you have to say so yourself; if you want layers, show it the Go template.

---

### 4. Declarative and GitOps

**What it is:** There are two ways to tell a tool what to do. Imperative means giving it the steps — first this, then that — like teaching a cook the recipe. Declarative means writing only what you want at the end and letting the tool work out how, like ordering from a menu. GitOps keeps that declaration in Git as the single source of truth, with a program that is always running to keep pulling the real system toward it.

- **Upside:** The declaration is the documentation; once it's in Git, every change can be reviewed and rolled back.
- **Cost:** Anything the declaration language can't express can't be done, and when something goes wrong you have to understand how a declaration turns into actual actions. The "keep pulling it back" half also needs a resident program watching all the time.
- **The AI-development pain:** Having an AI write deployment scripts means many steps and changes that are hard to review.
- **How BrickKit provides it:** `brickkit.yaml` is the only input, it lives in Git, and each environment gets its own complete file. Start order, service addresses, environment variables and deployment files are all computed by the CLI — what you review is a diff, not a pile of scripts.
- **Why BrickKit doesn't provide the rest:** No "keep pulling it back". `brickkit up` is a one-shot apply and the CLI exits, so nothing keeps watching; if someone hand-edits a running container, no program changes it back, and it lines up again at the next `up`. Continuous correction would need a resident program that somebody has to operate, which contradicts a CLI that runs and exits (see [platform minimalism](#platform-minimalism)).
- **How an AI adapts:** Have it change only `brickkit.yaml`, run `brickkit up --dry-run` to see the plan, then `up`; never hand-edit running containers.

---

### 5. Convention over configuration

**What it is:** The tool decides a set of rules up front — what things are called, where they go, what happens by default — and if you follow them you write almost no configuration. Rails is the best-known example.

- **Upside:** Less to write and less to read, and anyone picking up your project knows where things are at a glance.
- **Cost:** When the rules don't fit your situation there's nothing to negotiate. Worse are conventions that work by guessing: the tool quietly decided for you, and when it goes wrong you can't tell what it guessed.
- **The AI-development pain:** An AI has to guess variable names, ports and service addresses, and a wrong guess often raises no error at all.
- **How BrickKit provides it:** Fixed naming: service names, variable names and address formats are computed by fixed rules — `people/basic` at 1.0.0 is always `people-basic-1-0-0`, and its variable is always `PEOPLE_BASIC_ENDPOINT`. Nothing to guess, nothing to configure.
- **Why BrickKit doesn't provide the rest:** It doesn't decide for you. Which version, which port to expose, whether to enable a component — the platform never guesses these, and you have to write them down (see [explicit over implicit](#explicit-over-implicit)). That is why BrickKit's own phrase is "derivation over configuration": what can be *computed* from what you wrote is derived; what can't, you write.
- **How an AI adapts:** Derive variable names by the rule rather than letting the AI invent them; make it write down versions and whether something is exposed.

---

### 6. Twelve-factor configuration

**What it is:** "Twelve-factor" is a checklist of lessons for building apps that run in the cloud. One of its most quoted rules: don't hard-code configuration (the database address, a password, where another service lives) in the code; read it from environment variables when the program starts.

- **Upside:** One image runs in test and in production, and the only difference is which environment variables it was started with. Passwords stay out of the repository.
- **Cost:** Environment variables are flat strings, so anything structured has to be encoded and decoded by hand. Past a certain count they're hard to keep track of, a missing one often shows up only when the program needs it, and a config change needs a restart.
- **The AI-development pain:** It has to cope with config differences between environments.
- **How BrickKit provides it:** Dependency addresses, connection details for resources such as databases, and a component's own configuration all reach it as environment variables, in the same address format on Docker and Kubernetes, so moving between them changes no component code. `up` checks: a misspelled config key warns, and a required setting with no value stops it.
- **Why BrickKit doesn't provide the rest:** No config center and no hot reload — change `brickkit.yaml` and run `brickkit up` to restart, because most configuration can only safely take effect on a restart anyway.
- **How an AI adapts:** Read only with `os.environ.get()`, and read `configSchema` first to see which keys exist; a weak dependency's variable may not exist at all, and `os.environ["X"]` crashes at startup when it doesn't. See the [environment variable contract](environment-variables.md).

---

### 7. Exact versions and lockfiles

**What it is:** When you depend on someone else's software, you can write the version as a range (say `^1.0.0`, meaning "the latest 1.x is fine") or pin one specific version (`1.0.0`). A lockfile (npm's `package-lock.json`, for example) records the specific versions the first install produced, so later installs reproduce them.

- **Upside:** What you install today is the same as what you install three months from now — production doesn't quietly change at 3 a.m. because someone published a release. When something breaks you also know which version was running.
- **Cost:** Patches and security fixes don't arrive on their own; you edit a line to upgrade. Left alone for a long time, you fall a long way behind.
- **The AI-development pain:** An AI's breaking change (a new version incompatible with the old) drags down whatever depends on it.
- **How BrickKit provides it:** Only exact versions are accepted — writing `^1.0.0` is an error — and when you run `brickkit add` with no version, the CLI looks up the latest once and pins it (which amounts to generating a lockfile). The version is part of the service name, so v1 and v2 are two different DNS names that run side by side without colliding. See [core concepts](../concepts.md).
- **Why BrickKit doesn't provide the rest:** Whether data stays compatible between two versions running side by side is not the platform's business.
- **How an AI adapts:** For v2, have it add a new version entry instead of overwriting v1; whether a database migration stays backward compatible is up to you and the AI.

---

### 8. Contract-first

**What it is:** When two teams have to work together, first write down what the interface looks like as a file (OpenAPI or protobuf, say), and let both sides build against it — rather than finishing one side and making the other guess. Like agreeing the shape of the plug first.

- **Upside:** Both sides can work in parallel, and you can tell what a component promises without reading its implementation.
- **Cost:** Someone has to keep the contract up to date. A contract that no longer matches the implementation is worse than none — readers trust a description that has gone stale.
- **The AI-development pain:** To call another component, an AI can only dig through its source and guess.
- **How BrickKit provides it:** A component ships its contract files with the Manifest through `artifacts`, and `add`/`fetch` download them; the Market also requires a closed-source component that provides an API to include an `api-contract` — the code may be closed, the contract may not.
- **Why BrickKit doesn't provide the rest:** It doesn't dictate a format (`type` and `format` are free-form strings the CLI never parses) and never checks that an implementation actually matches its contract.
- **How an AI adapts:** Have it read the contract, not the dependency's source (so the dependency can be refactored freely without breaking it); whether the two agree is up to contract tests you write. See the [AI-assisted development guide](../ai-development.md).

---

### 9. Design by contract

**What it is:** Every function or module states "what I require of my input and what I guarantee about my output", and fails immediately when either is broken. Think of the rating plate on an appliance — "input 220V, output 12V" — and a breaker that trips if you wire it wrong.

- **Upside:** The error is caught closest to its cause, and the rules double as the most precise documentation there is.
- **Cost:** Rules that are too strict shut out legitimate uses nobody anticipated; rules that are too loose might as well not exist. Checking on every run has a price, and the rules need maintaining.
- **The AI-development pain:** An AI doesn't know which settings a component accepts or in what range, so it goes on impression.
- **How BrickKit provides it:** Only the "write it down" half: `configSchema` states which settings a component has, their types and their allowed ranges (`enum`, `minimum`, `maximum`, `pattern`), where both people and AI can read them. The CLI checks that a key's *name* exists in the project's config, and warns on a typo. Strictly it is only a relative of design by contract, since it describes and doesn't enforce.
- **Why BrickKit doesn't provide the rest:** It doesn't check a setting's *value*. The line falls where a runtime safety net exists or doesn't: a wrong value makes the component fail on its own, so you'll certainly notice; a wrong name just means the variable quietly doesn't exist and the component takes its default branch and runs normally, with nothing to tell you. And once you start validating values you can't stop — enums? ranges? regexes? — so the CLI keeps growing. See [configSchema is a spec sheet, not a security gate](#configschema-is-a-spec-sheet-not-a-security-gate).
- **How an AI adapts:** Have it read `configSchema` before writing code; have it put value checks in the component's startup, with a clear error message.

---

### 10. Loud failure

**What it is:** When a program finds something wrong, it stops at once and says so, instead of pretending nothing happened and carrying on. Often called fail fast.

- **Upside:** Errors surface early and close to their cause. The worst bug isn't a crash, it's a quietly wrong answer that looks fine.
- **Cost:** Stricter means more rejections: a file with one unknown field, or a component that wasn't written defensively, might have limped along before and now simply won't start.
- **The AI-development pain:** An AI's error handling often covers only the happy path, and edge cases such as a missing weak dependency or a failed migration get left out.
- **How BrickKit provides it:** An unrecognized key in a Manifest is rejected; a misspelled config key warns; a config key that is required, has no default and isn't set in the project stops `up`. The clearest case is a weak dependency: when one is missing, the platform injects *no variable at all* rather than an empty string — an empty string glued into an address sends the request to the component's own port and gets back a healthy-looking 200, an extremely hard bug to find (AGENTS.md §9.13).
- **Why BrickKit doesn't provide the rest:** How to degrade after an error isn't the platform's call — it's business logic: if Redis goes down, one component queries the database, another returns an empty list, another writes to a local file and retries later.
- **How an AI adapts:** Ask explicitly for handling of a missing weak dependency and a failed migration; read a weak dependency's variable with `.get()` and say what happens when it's absent; make the entrypoint exit at once on an argument it doesn't recognize.

---

### 11. Least privilege

**What it is:** Every program and every component gets only the access its job requires, and everything else is closed by default. Like a hotel key card that opens only your own room. In network security it is often mentioned together with "zero trust".

- **Upside:** If a component is compromised — or just has a bug — what it can reach is limited, and the damage stays small.
- **Cost:** Every permitted connection has to be written down. Leave one out and you're in for a "why can't it connect?" investigation.
- **The AI-development pain:** With a bug or a break-in, the more a component can reach, the bigger the damage.
- **How BrickKit provides it:** With no port mapping a component can't be reached from outside, and with no `expose` it has no external entry point; on Kubernetes the CLI can generate network policies from the dependency graph, allowing traffic only along the dependencies you declared; and the CLI itself never mounts the Docker socket.
- **Why BrickKit doesn't provide the rest:** Network policies and ServiceAccount isolation are both **opt-in** — the platform doesn't turn them on for you, because either can stop a component that already works from starting, and that cost is yours to weigh per project. A network policy also only holds if the cluster's network plugin (CNI) enforces it; otherwise it's a piece of paper, and the CLI warns about that. See [network policy and least privilege](../guide/11-network-policy.md).
- **How an AI adapts:** Have it declare only the dependencies it really needs (fewer dependencies, fewer open paths); whether to turn network policies on is a person's decision.

---

### 12. Supply-chain security

**What it is:** Is the third-party component you're installing really from the author it claims? Was it swapped on the way? Like a seal and a sender's signature on a parcel.

- **Upside:** It stops impersonation and tampering in transit.
- **Cost:** A signature proves who sent something and that it wasn't changed, not that what was sent is harmless. You also have to manage which public keys you trust.
- **The AI-development pain:** When an AI pulls in a third-party component, the source is hard to verify.
- **How BrickKit provides it:** Publishers sign with cosign, and the installer verifies with the Go standard library alone, so cosign needn't be installed. The public keys you trust go in your own project's `installer.publicKeys`, not fetched from the Market — otherwise a compromised Market could swap the component and the key together. Note: with no public key configured at all, verification is **switched off entirely** (the CLI warns once).
- **Why BrickKit doesn't provide the rest:** No upfront review — no scanning, no sandboxing, no static analysis. Installing implies trust, the same model as npm and the VS Code extension marketplace; if a component later turns out to be malicious, the Market marks it `blocked`. Upfront review either flags a lot of legitimate components or misses cleverly disguised malicious ones: expensive and unreliable.
- **How an AI adapts:** Have a person confirm any new component and any key added to `publicKeys` — don't leave those to the AI. See [signing and the trust model](signing-and-trust.md).

---

### 13. Type-driven development

**What it is:** Use the language's type system to write the rules into the shape of the code, so a wrong way of writing it won't compile. For example, an order amount isn't just any number but a dedicated "amount" type, so you can't hand over a "quantity" where an "amount" is expected.

- **Upside:** A whole class of mistakes is stopped at compile time instead of being found at runtime.
- **Cost:** It depends on the language — not every type system is strong enough — and designing the types takes effort of its own; done badly, they become a burden.
- **The AI-development pain:** Whether AI-written code is right can't be seen just by reading it.
- **How BrickKit provides it:** It doesn't. BrickKit's own CLI is written in Go and benefits from type checking, but components aren't required to do the same.
- **Why BrickKit doesn't provide the rest:** It doesn't limit the language, and as completely as anywhere: anything that builds a container image will do.
- **How an AI adapts:** Pick a language with type checking for the AI, and let the compiler be the first gate. See [build your first component](../guide/10-build-your-own.md).

---

### 14. TDD and BDD

**What it is:** TDD (test-driven development) means writing the test first and then the code that makes it pass; BDD (behavior-driven development) means describing the expected behavior first in near-everyday language — "given … when … then …" — and turning that into tests. Like writing the acceptance criteria before you start cooking.

- **Upside:** The tests constrain what the code *should* do instead of restating what it *already* does, and requirements become something you can check.
- **Cost:** It takes time up front, and if the acceptance criteria are themselves wrong, you'll build the wrong thing very diligently.
- **The AI-development pain:** If an AI writes the implementation first and the tests after, the tests merely restate the code — they test only that "the code does what it does".
- **How BrickKit provides it:** It doesn't enforce it, but it gives advice: the [layered testing guide](../patterns/testing.md) covers how to test a component in layers and suggests a spec-first order. What BrickKit adds is `brickkit up --dry-run`: it starts no container, yet checks whether `component.yaml` and `brickkit.yaml` agree (a misspelled config key, for instance).
- **Why BrickKit doesn't provide the rest:** How to test is the component's own matter, and the platform checks nothing. `--dry-run` checks whether the **assembly** is right, not whether the **component's code** is.
- **How an AI adapts:** Have it write `component.yaml` and the tests first and watch the tests fail, then write the implementation; it often tests only the normal cases, so add the edge cases yourself.

---

### 15. Property-based testing

**What it is:** In an ordinary test you write a few examples by hand: input A should give B. Property-based testing takes a different approach: you state a rule that must hold *whatever the input* ("a sorted list is always in order"), and a tool generates thousands of random inputs looking for one that breaks it.

- **Upside:** It finds edge cases you wouldn't have thought of.
- **Cost:** Working out the right property isn't easy, a counterexample often has to be shrunk before you can read it, and it fits poorly with code that leans heavily on external systems.
- **The AI-development pain:** AI-generated tests often cover only the normal cases it thought of.
- **How BrickKit provides it:** The platform uses it on itself: the three densest rule sets in the CLI — deciding what starts, start order, and resource-quota merging — are each tested against thousands of random inputs. For components it only gets a mention in the [data construction guide](../patterns/data-construction.md).
- **Why BrickKit doesn't provide the rest:** Neither required nor recommended for components — it applies to a fairly narrow set of cases, mainly core invariants such as a balance never going negative.
- **How an AI adapts:** For a core invariant, let the AI write a property test; otherwise don't bother.

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
