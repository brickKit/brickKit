# Design principles and trade-offs

The [overview](overview.md) explains *what* the platform does. This page explains *why it is shaped this way*: the one idea that ties the design together; the engineering ideas BrickKit borrows from — and, just as deliberately, the ones it leaves alone — each explained from scratch, with no assumption that you've met them; and the twelve principles every proposal is checked against.

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

BrickKit doesn't invent a new paradigm. Everything it uses is an idea software engineering has had for a long time. You may know all of them, or none — this section is written for the second case. Each idea starts with what it is in a sentence or two, then what it buys you and what it costs, and ends with how BrickKit treats it: what it took, what it left, and why.

BrickKit's attitude to these ideas comes in three kinds:

- **Adopted:** the platform implements it itself.
- **Half borrowed:** it takes one part and deliberately leaves the other.
- **Hands off:** it's a matter inside a component, and the platform stays out of it.

Here is the whole picture first; the sections below follow the same order.

| Idea | In one line | BrickKit's attitude |
| --- | --- | --- |
| Bounded contexts (DDD) | Draw boundaries around business areas; each piece owns its own model | Adopted |
| Declarative and GitOps | Write what you want, not the steps; keep the declaration in Git | Adopted, minus continuous correction |
| Twelve-factor configuration | Keep config out of the code; pass it in at startup as environment variables | Adopted |
| Convention over configuration | The tool sets the rules up front; follow them and you configure little | Half borrowed |
| Exact versions and lockfiles | Pin one specific version instead of "any 1.x" | Adopted |
| Contract-first | Write the interface down as a file, then build both sides | Half borrowed |
| Design by contract | State what you require and what you guarantee, and fail when it's broken | Half borrowed |
| Loud failure | When something is wrong, fail at once and visibly instead of carrying on | Adopted |
| Least privilege | Grant only the access the job needs; everything else is closed | Adopted (parts are opt-in) |
| Supply-chain security | Check who published third-party code and that it wasn't altered | Half borrowed |
| Microservices and service mesh | Split into many small services; a mesh manages the traffic between them | Half borrowed |
| Hexagonal and layered architecture | Keep the business core apart from external systems | Hands off |
| Type-driven development | Use types so wrong code won't compile | Hands off |
| TDD and BDD | Write tests or behavior descriptions first, then the code | Not enforced, advice offered |
| Property-based testing | State a rule that must hold for any input; let a tool hunt for counterexamples | Not enforced; the platform uses it on itself |

### Bounded contexts (DDD)

**What it is.** DDD stands for domain-driven design. One of its central ideas: in a big system the same word often means different things in different parts of the business — a "customer" in ordering is the person placing the order, a "customer" in support is the person waiting for a reply. Rather than force one all-purpose "customer", you draw boundaries around business areas, and each one (a "bounded context") gets its own vocabulary, rules and data.

**Upside.** Different people can understand and change different pieces independently, and a change in one piece doesn't ripple into the rest. When something breaks, it's easier to tell which piece it belongs to.

**Cost.** A wrong boundary is hard to repair: too fine, and the pieces spend their time talking to each other; too coarse, and you're back to a tangle. Drawing the boundaries also requires really understanding the business first, which takes time.

**In BrickKit.** Adopted: a component is the engineering boundary of a bounded context — its own repository, Manifest, version and interface contract, with `component.yaml` as the boundary. A component shouldn't straddle two contexts; a context can be made of one component or several. Hands off: how a component models its insides, and whether several contexts share a database or get merged into one process (`servedBy`) — that's your call, made for the business at hand. The term-by-term mapping is in the [component design guidelines](../patterns/component-design.md#if-your-team-thinks-in-ddd).

### Declarative and GitOps

**What it is.** There are two ways to tell a tool what to do. Imperative means giving it the steps — first this, then that — like teaching a cook the recipe. Declarative means writing only what you want at the end and letting the tool work out how, like ordering from a menu. GitOps goes one step further: the declaration lives in Git as the single source of truth, and a program that is always running keeps pulling the real system toward it.

**Upside.** The declaration is the documentation: open the file and you know what the system should look like. Once it's in Git, every change can be reviewed and rolled back.

**Cost.** Anything the declaration language can't express can't be done, and when something goes wrong you have to understand how a declaration turns into actual actions. The "keep pulling it back" half of GitOps also needs a resident program watching all the time.

**In BrickKit.** Declarative is adopted: `brickkit.yaml` is the only input, it lives in Git, and each environment gets its own complete, self-contained file — the diff is the review. The continuous correction is not: `brickkit up` is a one-shot apply and the CLI exits, so nothing keeps watching. If someone edits a running container by hand, no program changes it back; it lines up again at the next `up`. Continuous correction would need a resident program that somebody has to operate, which contradicts a CLI that runs and exits (see [platform minimalism](#platform-minimalism)).

### Twelve-factor configuration

**What it is.** "Twelve-factor" is a checklist of lessons for building apps that run in the cloud. One of its most quoted rules: don't hard-code configuration (the database address, a password, where another service lives) in the code; read it from environment variables when the program starts.

**Upside.** One image runs in test and in production, and the only difference between the environments is which environment variables it was started with. Passwords stay out of the repository, and the code never has to know where it is running.

**Cost.** Environment variables are flat strings, so anything structured has to be encoded and decoded by hand. Past a certain count they're hard to keep track of, and a missing one often shows up only at the moment the program actually needs it. A config change also needs a restart to take effect.

**In BrickKit.** Adopted: dependency addresses, connection details for resources such as databases, and a component's own configuration all reach it as environment variables, and the address format is identical on Docker and Kubernetes, so moving between them changes no component code. Against the "found out too late" cost, the platform checks at `up`: a misspelled config key warns, and a key that is required, has no default and isn't set in the project blocks `up`. Left out: a config center and hot reload — you change `brickkit.yaml` and run `brickkit up` to restart, because most configuration can only safely take effect on a restart anyway. See the [environment variable contract](environment-variables.md).

### Convention over configuration

**What it is.** The tool decides a set of rules up front — what things are called, where they go, what happens by default — and if you follow them you write almost no configuration. Rails is the best-known example.

**Upside.** Less to write and less to read, and anyone picking up your project knows where things are at a glance.

**Cost.** When the rules don't fit your situation there's nothing to negotiate. Worse are conventions that work by guessing: the rule wasn't clear, the tool quietly decided for you, and when it goes wrong you can't tell what it guessed.

**In BrickKit.** Half borrowed. What it takes is fixed naming: service names, environment variable names and address formats are computed by fixed rules — `people/basic` at 1.0.0 is always `people-basic-1-0-0`, and its variable is always `PEOPLE_BASIC_ENDPOINT` — so you never configure them. What it doesn't take is deciding for you: which version, which port to expose, whether to enable a component — the platform never guesses these, and you have to write them down (see [explicit over implicit](#explicit-over-implicit)). That is why BrickKit's own phrase is "derivation over configuration": what can be *computed* from what you wrote is derived; what can't be computed, you write.

### Exact versions and lockfiles

**What it is.** When you depend on someone else's software, you can write the version as a range (say `^1.0.0`, meaning "the latest 1.x is fine") or pin one specific version (`1.0.0`). A lockfile (npm's `package-lock.json`, for example) records the specific versions the first install produced, so later installs reproduce them.

**Upside.** What you install today is the same as what you install three months from now — your production environment doesn't quietly change at 3 a.m. because someone published a new release. When something breaks you also know exactly which version was running.

**Cost.** Patches and security fixes don't arrive on their own; you edit a line to upgrade. Left alone for a long time, you fall a long way behind.

**In BrickKit.** Adopted, and stricter than npm: config accepts only exact versions, and writing `^1.0.0` is an error. When you run `brickkit add people/basic` with no version, the CLI looks up the latest once, at that moment, and writes it into the config as an exact version — which amounts to generating a lockfile: resolved once, not on every run. The version also becomes part of the service name (`people-basic-1-0-0`), so two versions are two different DNS names that run side by side without colliding. Left to you: whether data stays compatible between two versions running side by side. See [core concepts](../concepts.md).

### Contract-first

**What it is.** When two teams have to work together, first write down what the interface looks like as a file (an OpenAPI or protobuf file, say), and let both sides build against it — rather than finishing one side and making the other guess. It's like agreeing the shape of the plug first, so each side can build its half separately.

**Upside.** Both sides can work in parallel, and you can tell what a component promises without reading its implementation.

**Cost.** Someone has to keep the contract file up to date. A contract that no longer matches the implementation is worse than none — readers trust a description that has gone stale.

**In BrickKit.** Half borrowed. It takes the "carry it with you" part: a component ships its contract files with the Manifest through `artifacts`, and `add`/`fetch` download them; the Market also requires a closed-source component that provides an API to include an `api-contract` artifact — the code may be closed, the contract may not. It doesn't take "managing the contract itself": it doesn't dictate a format (`type` and `format` are free-form strings the CLI never parses) and never checks that an implementation actually matches its contract. See the [AI-assisted development guide](../ai-development.md).

### Design by contract

**What it is.** Every function or module states "what I require of my input and what I guarantee about my output", and fails immediately when either is broken. Think of the rating plate on an appliance — "input 220V, output 12V" — and a breaker that trips if you wire it wrong.

**Upside.** The error is caught closest to its cause, and the rules double as the most precise documentation there is.

**Cost.** Rules that are too strict shut out legitimate uses nobody anticipated; rules that are too loose might as well not exist. Checking on every run has a price, and the rules themselves need maintaining.

**In BrickKit.** Half borrowed: only the "write it down" half. The nearest thing BrickKit has is `configSchema` — strictly a relative, since it describes and doesn't enforce: a component uses it to state which config keys it has, their types and their allowed ranges (`enum`, `minimum`, `maximum`, `pattern`), where both people and AI can read them. It doesn't take the "enforce" half: the CLI checks that a key's *name* exists (a typo warns) and never checks its *value*. The line falls where a runtime safety net exists or doesn't: a wrong value makes the component fail on its own, so you'll certainly notice; a wrong name just means the variable quietly doesn't exist and the component takes its default branch and runs normally, with nothing to tell you. And once you start validating values you can't stop — enums? ranges? regexes? — and the CLI keeps growing. See [configSchema is a spec sheet, not a security gate](#configschema-is-a-spec-sheet-not-a-security-gate).

### Loud failure

**What it is.** When a program finds something wrong, it stops at once and says so, instead of pretending nothing happened and carrying on. It's often called fail fast.

**Upside.** Errors surface early and close to their cause. The worst bug isn't a crash, it's a quietly wrong answer that looks fine — a crash you're sure to notice, a wrong answer you may not.

**Cost.** Stricter means more rejections: a file with one unknown field, or a component that wasn't written defensively, might have limped along before and now simply won't start.

**In BrickKit.** Adopted, and on purpose. An unrecognized key in a Manifest is rejected, not silently ignored; a misspelled config key warns; a config key that is required, has no default and isn't set in the project blocks `up`. The clearest case is a weak dependency: when one is missing, the platform injects *no variable at all* rather than an empty string — an empty string glued into an address sends the request to the component's own port, gets back a healthy-looking 200, and that is an extremely hard bug to find (AGENTS.md §9.13). The cost is real too: a component that reads that variable with `os.environ["X"]` crashes at startup when it's absent, so its author has to read it defensively with `.get()`.

### Least privilege

**What it is.** Every program and every component gets only the access its job requires, and everything else is closed by default. Like a hotel key card that opens only your own room. In network security it is often mentioned together with "zero trust".

**Upside.** If a component is compromised — or just has a bug — what it can reach is limited, and the damage stays small.

**Cost.** Every permitted connection has to be written down explicitly. Leave one out and you're in for a "why can't it connect?" investigation.

**In BrickKit.** Adopted: with no port mapping a component can't be reached from outside, and with no `expose` it has no external entry point; on Kubernetes the CLI can generate network policies from the dependency graph, allowing traffic only along the dependencies you declared; and the CLI itself never mounts the Docker socket. Two things to keep in mind. First, network policies and ServiceAccount isolation are both **opt-in**: the platform doesn't turn them on for you, because either can stop a component that already works from starting, and that cost is yours to weigh per project. Second, whether a network policy actually holds depends on the cluster's network plugin (CNI) enforcing it — if it doesn't, the policy is just a piece of paper, and the CLI warns about that. See [network policy and least privilege](../guide/11-network-policy.md).

### Supply-chain security

**What it is.** Is the third-party component you're installing really from the author it claims? Was it swapped on the way? Like a seal and a sender's signature on a parcel.

**Upside.** It stops impersonation and tampering in transit.

**Cost.** A signature proves who sent something and that it wasn't changed, not that what was sent is harmless. You also have to manage which public keys you trust.

**In BrickKit.** Half borrowed. It takes signature verification: publishers sign with cosign, and the installer verifies with the Go standard library alone, so cosign needn't be installed. The public keys you trust go in your own project's `installer.publicKeys`, not fetched from the Market — otherwise a compromised Market could swap the component and the key together, and verification would still pass. Note: with no public key configured at all, signature verification is **switched off entirely** (the CLI warns once). What it doesn't take is upfront review: no scanning, no sandboxing, no static analysis — installing implies trust, the same model as npm and the VS Code extension marketplace; if a component later turns out to be malicious, the Market marks it `blocked`, which stops new installs. The reason is that upfront review either flags a lot of legitimate components or misses cleverly disguised malicious ones: expensive and unreliable. See [signing and the trust model](signing-and-trust.md).

### Microservices and service mesh

**What it is.** Microservices means splitting one big program into many small services, each developed and deployed on its own and calling the others over the network. A service mesh is a layer of infrastructure dedicated to how those services talk to each other: finding one another, spreading load, rate limiting, retrying failures, and circuit breaking (pausing calls to something that keeps failing). Like turning one large restaurant into a street of small shops.

**Upside.** Each shop can open and refit on its own, and one shop having trouble doesn't shut the whole street.

**Cost.** With more shops, someone has to look after the signposts (a registry), the entrances (a gateway) and the queues and limits (a mesh) — operating it gets far harder, and tracking a fault means visiting several shops.

**In BrickKit.** Half borrowed. It takes the splitting: components are built, deployed and called independently. It doesn't take the governance stack: no registry (the DNS that Docker Compose and Kubernetes already provide is the service discovery — finding each other by name), no gateway, no mesh, no circuit breaking or rate limiting (that is the component's own code, because thresholds and backoff strategies differ by business and one platform-wide setting can't be right), no config center. Each of those is something that has to run permanently and be operated by someone, while BrickKit's CLI runs and exits. If you do need a gateway (Traefik, say), `labels` are passed through to it verbatim — the platform doesn't understand what a label means, it just passes it on. See [what the platform deliberately doesn't do, and why](overview.md#what-the-platform-deliberately-doesnt-do-and-why).

### Hexagonal and layered architecture

**What it is.** Both are ways of organizing the inside of a program. Layering cuts it into tiers (say one that receives requests, one that handles the business, one that reads and writes data), where each tier only calls the one below. Hexagonal architecture goes further: the real business logic sits at the center, and everything external — the database, the web interface, the message queue — plugs into it through interfaces agreed in advance. Like putting a standard interface between the kitchen and the delivery apps, the till and the suppliers, so changing supplier doesn't mean rewriting the recipes.

**Upside.** The business logic doesn't depend on a particular framework or database, so it's easier to test on its own and easier to swap an external system out.

**Cost.** You write a fair amount of extra interface and conversion code; in a small program it's like fitting a gearbox to a bicycle.

**In BrickKit.** Hands off. How a component is layered or organized inside is the author's decision. The reason is that BrickKit only specifies how a component looks *at its boundary* (see [component autonomy](#component-autonomy)), and once the platform starts prescribing internal structure it stops being an orchestrator and becomes a framework. To see what a real layered component looks like, [writing a Go component](../go-component-template.md) walks through one: an HTTP layer, a business layer and a data-access layer.

### Type-driven development

**What it is.** Use the language's type system to write the rules into the shape of the code, so a wrong way of writing it won't compile. For example, an order amount isn't just any number but a dedicated "amount" type, so you can't hand over a "quantity" where an "amount" is expected.

**Upside.** A whole class of mistakes is stopped at compile time instead of being found at runtime.

**Cost.** It depends on the language — not every type system is strong enough — and designing the types takes effort of its own; done badly, they become a burden.

**In BrickKit.** Hands off, and as completely as anywhere: the platform doesn't limit what language a component uses, and anything that builds a container image will do. BrickKit's own CLI is written in Go and benefits from type checking, but it doesn't ask the same of components. See [build your first component](../guide/10-build-your-own.md).

### TDD and BDD

**What it is.** TDD (test-driven development) means writing the test first and then the code that makes it pass; BDD (behavior-driven development) means describing the expected behavior first in near-everyday language — "given … when … then …" — and turning that into tests. Like writing the acceptance criteria before you start cooking.

**Upside.** The tests constrain what the code *should* do instead of restating what it *already* does, and requirements become something you can check.

**Cost.** It takes time up front, and if the acceptance criteria are themselves wrong, you'll build the wrong thing very diligently.

**In BrickKit.** Not enforced: how to test is the component's own matter and the platform checks nothing. It does offer advice: the [layered testing guide](../patterns/testing.md) covers how to test a component in layers and suggests an order for having an AI write one — spec first, implementation after. What BrickKit adds is `brickkit up --dry-run`: it starts nothing, yet checks whether `component.yaml` and `brickkit.yaml` agree (a misspelled config key, for instance). Note that it checks whether the **assembly** is right, not whether the **component's code** is.

### Property-based testing

**What it is.** In an ordinary test you write a few examples by hand: input A should give B. Property-based testing takes a different approach: you state a rule that must hold *whatever the input* ("a sorted list is always in order"), and a tool generates thousands of random inputs looking for one that breaks it.

**Upside.** It finds edge cases you wouldn't have thought of.

**Cost.** Working out the right property isn't easy, a counterexample often has to be shrunk before you can read it, and it fits poorly with code that leans heavily on external systems.

**In BrickKit.** Neither required nor recommended for components — it applies to a fairly narrow set of cases, mainly core invariants such as a balance never going negative, which the [data construction guide](../patterns/data-construction.md) touches on. But the platform uses it on itself: the three densest rule sets in the CLI — deciding what starts, start order, and resource-quota merging — are each tested against thousands of random inputs.

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
