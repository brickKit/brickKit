# Choosing a deployment shape

> Prerequisite: read [Architecture overview](../architecture/overview.md) first
> if you haven't — this guide assumes you already understand versioned
> service names and environment-variable injection.
>
> **This document answers one question: given a real project with N
> components, what should actually be running, and how?** It doesn't tell
> you how to build a shell (that's [Building a Qualified Shell](shell-implementers-guide.md))
> or walk through committing one specific component to one (that's
> [Declaring servedBy: a deployment checklist](servedby-deployment-checklist.md)).
> Read this one first if you haven't yet settled on your project's overall
> shape; the other two are the "how" once you have.

## Two decisions and one toggle

Running a brickKit project is two independent decisions, plus one
per-component switch that only makes sense once you've made the first two:

**Decision 1 — topology: how many containers do your components collapse into?**

- **Independent components** — every component gets its own container/Pod.
  This is what you get by writing nothing extra; it's the default in the
  literal sense that not writing `servedBy` on a component is what keeps it
  here.
- **Shell-merged** (`servedBy`, AGENTS.md §5.7) — as many components as
  reasonably possible are absorbed into a small number of shell containers.
- **Mixed** — some components merged, others independent. In practice this
  is usually the common case, not a compromise: some components are
  *structurally* incapable of merging (a pure frontend served by nginx has
  no backend process to absorb into anything; a stateless gateway-shaped
  component is often kept independent on purpose so it can scale on its own
  curve) regardless of how tight or loose your resource budget is.

**Decision 2 — deploy target: `docker` or `k8s`** (`deploy.target` in
`brickkit.yaml`, AGENTS.md §5.5). This is almost entirely an infrastructure
question, not a performance one: do you have a Kubernetes cluster to deploy
to, and do you need elastic scaling and self-healing across machines? If
not, `docker` generates and runs everything on one machine with `docker
compose` under the hood — simpler operationally, and the right choice for
local development, demos, and small-scale or single-site production. If you
do have a cluster, `k8s` generates Deployments/Services/Ingresses instead,
and everything about the topology decision above still applies unchanged —
switching targets is one field, not a rewrite (AGENTS.md §5.5/§9.4).

**The toggle — `local: true` (AGENTS.md §5.6): pull one or a few components
out to run as debuggable processes on your own machine, while everything
else keeps running for real.** This only exists under `deploy.target:
docker` — the platform rejects it outright, at generation time, under
`deploy.target: k8s` (see the aside under the matrix below). It composes
with either topology: a `local: true` component can depend on an ordinary
independent component, a shell, or a `servedBy` member (in which case its
`*_ENDPOINT` resolves to `localhost:<port>` through the shell's own compose
service — servedby-deployment-checklist.md's "What happens automatically
once you declare it" covers the mechanics). It cannot be combined with
`servedBy` on the *same* component entry — `local: true` and `servedBy` are
mutually exclusive fields, because they claim contradictory things about
where that component's code physically runs.

## A real option that isn't governed by any of this: running components by hand

There's a fourth way projects actually get run day to day, and it deserves
a direct answer rather than silence: nothing stops you from starting every
component as an ordinary process on your own machine (`go run`, `uvicorn`,
whatever your language's equivalent is) and never touching `brickkit.yaml`
or `brickkit up` at all.

**Be clear-eyed about what this is:** it is not a deployment shape the
platform manages, checks, or even knows occurred. `brickkit up`'s entire
contract — dependency resolution, environment-variable injection, health
checks, migration ordering, service discovery — simply doesn't run, because
you never invoked it. Every one of those has to be reconstructed by hand:
you decide the port each process listens on, you decide (and supply) every
environment variable a real deployment would have injected, and you're
responsible for starting things in an order that doesn't crash on a missing
dependency. Nothing here is `brickkit`-specific caution — it's the direct
consequence of AGENTS.md §2.2's "there is no long-running main system
service": the CLI's contract only exists for the duration of a command you
actually run.

This is still a completely legitimate, common way to work — it's usually
the fastest development loop that exists (no build-an-image step, IDE
breakpoints attach directly), and for a component with no shell yet, it's
often the everyday default before any merged topology exists at all. Don't
read the platform's silence about it as the platform saying it doesn't
happen; it's just the one shape that lives entirely outside anything
`brickkit.yaml` describes, so this guide is the right place to name it and
move on, rather than the matrix below.

**One concrete trick worth knowing, precisely because the platform doesn't
help here otherwise:** you can use `brickkit up --dry-run` as a cheat sheet
for what a real deployment would have injected, without actually deploying
that way. Temporarily flip the one component you're about to run by hand to
`local: true`, run `brickkit up --dry-run`, and read the
`local-debug.<versioned-service-name>.env` file it generates — that's the
exact set of environment variables (dependency addresses, resource
connections, your own config) a real deployment would hand this component.
Copy what you need, then revert the temporary edit (`git checkout --
brickkit.yaml`) — you're using the CLI to look up the answer, not to run
anything. Two things this trick doesn't cover: a config value that's a
hand-written literal rather than something the platform computes (an
`http://host.docker.internal:...`-shaped address, say) still assumes a
container network and needs manual correction to `localhost` for a bare
process exactly as it would under `local: true` itself (AGENTS.md §5.6); and
if the component you're inspecting is itself a `servedBy` member, it has no
independent port or environment of its own to preview this way — look at
its shell's entry instead.

## The matrix

|  | Bare process | Docker | K8s | Docker + local debug |
| --- | --- | --- | --- | --- |
| **Independent components** | [The everyday starting point, before any shell exists](#1-independent-components--bare-process) | [The standard shape while everything comfortably fits on one machine](#2-independent-components--docker) | [The resource-rich end of the cloud-native spectrum](#3-independent-components--k8s) | [Debugging one component while the rest stays real](#4-independent-components--docker--local-debug) |
| **Shell-merged (`servedBy`)** | [Testing a shell's own internal wiring in isolation](#5-shell-merged--bare-process) | [Solving the memory-floor problem once independent no longer fits](#6-shell-merged--docker) | [The tightest end of the cloud-native spectrum](#7-shell-merged--k8s) | [Debugging one component while the merged rest stays real](#8-shell-merged--docker--local-debug) |
| **Mixed** | [The everyday default once your project already has a shell](#9-mixed--bare-process) | [The structural shape most real projects actually settle into](#10-mixed--docker) | [The middle of the cloud-native spectrum](#11-mixed--k8s) | [The most realistic development-time combination there is](#12-mixed--docker--local-debug) |

There is deliberately no thirteenth cell for "K8s + local debug." `local:
true` is rejected outright, at generation time, the moment `deploy.target:
k8s` — not a warning, not something that generates a broken manifest and
fails later. The CLI's own error names the component and states the reason
in physical terms: a cluster Pod cannot reach a process running on a
developer's own machine. This isn't a gap this guide works around; it's a
combination the platform never lets exist as a deployable configuration in
the first place.

### 1. Independent components × bare process

**What it is:** every component you're working with is a plain host
process — `go run`, `uvicorn`, whatever your language's own tooling is —
with no Docker container, no K8s Pod, and no `brickkit.yaml` entry
describing this deployment at all. You decide each process's listening
port, and you supply every environment variable a real deployment would
have injected (dependency addresses, resource connections, your own
config) by hand.

**When to use it:** the everyday starting point for working on a single
component, especially before any shell exists in the project at all. It's
also the right choice whenever you want the fastest possible edit-run loop
and don't need to exercise the platform's own generation/injection
machinery — just the component's own code.

**Advantages:**
- Fastest iteration there is — no image-build step between an edit and
  seeing it run; an IDE debugger attaches directly to the real process.
- Lowest cognitive load when troubleshooting — no container layer and no
  orchestration layer sit between "this process" and "the address it's
  calling." When something's wrong, there are only two things to check: the
  process itself, and whatever address you gave it.
- Doesn't require Docker or a K8s cluster to be installed at all.

**Costs / limitations:**
- Not a platform-managed shape in any sense: no dependency injection, no
  health check, no service discovery, no migration ordering. Every one of
  those either doesn't happen, or you reconstruct it by hand, for exactly
  the handful of components you're running this way.
- A config value that's a hand-written literal assuming a container network
  (an `http://host.docker.internal:...`-shaped address, say) won't resolve
  from a bare host process; you correct it to `localhost` yourself — the
  same correction the local-debug overlay below needs, for the same reason.
- Doesn't scale past a handful of components at a time — reconstructing
  every environment variable by hand for a dozen components is real,
  tedious, error-prone work.

**Things worth knowing:**
- You can still borrow the platform to look up what it *would* have
  injected, without deploying anything: temporarily flip the component to
  `local: true`, run `brickkit up --dry-run`, and read the generated
  `local-debug.<versioned-service-name>.env` — that's the exact variable
  set a real deployment hands this component. Copy what you need, then
  revert the edit (`git checkout -- brickkit.yaml`).
- If the component you're inspecting this way is itself a `servedBy`
  member, it has no independent port or environment of its own to preview
  — you'd be looking at its shell's entry instead, which mixes in every
  other absorbed member's data too.

### 2. Independent components × Docker

**What it is:** every component gets its own Docker Compose service,
generated and driven end to end by `brickkit up` — network aliasing (DNS
resolves each versioned service name), environment-variable injection
(dependency addresses, resource connections, your own config), health
checks, topological start order, and any declared migration, all with zero
manual steps.

**When to use it:** the default shape for a project whose components
comfortably fit on one machine. "Comfortably fit" is a real number, not a
feeling: each running process has a memory floor set almost entirely by its
language runtime — Go/Rust 8–20MB, Python/Node 40–90MB, a JVM 200–450MB.
Twenty idle Go components is well under half a gigabyte; twenty idle Spring
Boot components is 4–9GB before any of them do a single unit of work. Stay
here — this covers project early-stage, most demos, and most small on-prem
deliveries — until that arithmetic genuinely stops fitting the machine
you're targeting; only then does merging some or all components into a
shell start to pay off.

**Advantages:**
- Matches the platform's core design invariant most directly: every
  component can `brickkit up` on its own, with a blast radius of exactly
  one — restarting or upgrading one component never touches another.
- Address injection, health checking, and start order need no manual
  attention at all — the platform's generation logic derives all of it from
  the Manifest alone.
- Debugging is at its simplest here: one container, one set of logs, one
  health status, per component — nothing shared to reason about.

**Costs / limitations:**
- Every component pays its own runtime's memory floor, independently — this
  is the direct cost merging components into a shell exists to avoid once
  component count grows.
- A single-machine shape by construction — it doesn't need a Kubernetes
  cluster, but also doesn't get any of K8s's cross-machine scheduling or
  self-healing.

**Things worth knowing:**
- Before assuming you've outgrown this shape, measure the real footprint
  (`docker stats` against what's actually running) rather than reasoning
  from a table — a 50-component project might only run a handful of
  containers locally if most components aren't `enabled` for this
  deployment.
- `expose: true` maps a component's port straight to the host under Docker,
  customizable via `exposePort`; the CLI errors on a port collision rather
  than silently picking a different one.

### 3. Independent components × K8s

**What it is:** each component becomes its own Kubernetes Deployment +
Service; `brickkit up --context <cluster>` targets a real cluster, and
every dependency address resolves through native K8s Service + CoreDNS — no
separate registry, no polling health checker, just the cluster's own
primitives.

**When to use it:** the resource-rich end of a three-point spectrum this
guide's shell-merged-on-K8s and mixed-on-K8s cells also sit on. Pick it once
your cluster has enough headroom that you're no longer trading per-component
control for a lower aggregate memory floor — every component keeps its own
independently tunable replica count and its own `resources.requests`/
`limits`, without another component's load ever competing for the same
container's quota.

**Advantages:**
- True independent horizontal scaling — a component under heavy load gets
  more replicas without touching any other component's resourcing at all.
- Cross-machine scheduling and failure recovery are native K8s behavior,
  and they apply individually, per component.
- The same blast-radius-of-one property independent components get on
  Docker, now backed by a whole cluster instead of a single machine.

**Costs / limitations:**
- The highest operational complexity of any cell in this row — you need
  real cluster-management capability, not just "install Docker."
- `kubectl` has to be installed, separately, on whatever machine runs
  `brickkit up` — the CLI shells out to a real `kubectl` binary on `PATH`
  and errors out by name if it can't find one; it never substitutes
  something like `minikube kubectl --` for it.
- Component count still means Deployment/Service object count — merging
  into shells is the lever for reducing that, not this cell.

**Things worth knowing:**
- `expose: true` requires `hostname` under `deploy.target: k8s` — the
  Docker-only `exposePort` field does nothing here, because K8s exposes
  things through an Ingress + domain routing, not a host port mapping.
  `brickkit up --dry-run` catches a missing `hostname` at generation time,
  before anything is actually deployed.
- A resource address literal written assuming single-machine Docker
  networking (say, `host.docker.internal`) needs its own K8s-appropriate
  replacement (for example `host.minikube.internal` under minikube) —
  switching orchestrators never rewrites a hand-written literal for you.

### 4. Independent components × Docker + local debug

**What it is:** the whole topology runs as real Docker containers except
the one (or few) component(s) you mark `local: true` — those run as
ordinary processes on your own machine instead. The platform doesn't do a
literal address substitution for this: it rewrites `extra_hosts` on every
*other* container so that component's versioned service name resolves to
`host-gateway` (your machine) inside their network — the address string a
caller receives is exactly the same shape it would be otherwise; only where
that name actually resolves to has changed.

**When to use it:** whenever you want two things that otherwise trade off
against each other, at the same time — "the rest of the system is real"
(real containers, real health checks, real address injection) and "I don't
want to rebuild an image every time I change this one component's code."
It's purely a development-time overlay; it never itself decides whether
your production topology is independent, shell-merged, or mixed, and
switching it on or off doesn't change any of that.

**Advantages:**
- Zero component-code changes required — a component reads its environment
  variables exactly the same way whether it's `local: true` or not.
- Several components can be debugged locally at once, each with its own
  `localPort`, without disturbing the rest of the topology.
- Everything a `local: true` component depends on still gets real,
  generated addresses — you're not hand-simulating anything, unlike running
  everything as bare processes.

**Costs / limitations:**
- Needs you to actually understand the `extra_hosts` mechanism — when an
  address "won't connect," the usual question is "is this name resolving to
  my machine or to a container," not an ordinary networking problem.
- A hand-written config literal that assumes a container network (again, an
  `http://host.docker.internal:...`-shaped value) isn't rewritten by this
  mechanism either — brickKit never parses the contents of a config string,
  so flipping `local: true` doesn't retarget it; you edit it yourself,
  usually to `localhost`.
- A `local: true` component gets no migration container of its own — if its
  schema isn't already up to date, you run its migration by hand once.

**Things worth knowing:**
- The CLI generates a `local-debug.<versioned-service-name>.env` file per
  `local: true` component for your IDE to load (`set -a && source ... &&
  set +a`); multi-line and special-character values inside it are
  shell-quoted so a PEM key or a `|`-delimited list survives intact.
- `local: true` and `servedBy` can never be written on the same component
  entry — they claim opposite things about where that component's code
  physically runs, so the platform rejects declaring both.

### 5. Shell-merged × bare process

**What it is:** instead of any orchestration layer, the shell's own binary
is started directly as a host process — you're testing the shell's own
multi-module wiring, not deploying anything. There's no
`BRICKKIT_SERVED_MEMBERS`/`BRICKKIT_SERVED_MEMBERS_CONFIG` injected by any
platform here either; whoever runs this constructs (or hard-codes, for a
test) that data by hand, the same way running an ordinary component as a
bare process means hand-constructing its environment.

**When to use it:** almost never something an end user of a shell needs —
it's aimed squarely at whoever is *building or testing* the shell itself:
verifying the shell's own startup sequencing (which absorbed modules
initialize, in what order, whether migrations run correctly relative to
each other), its health-check behavior, and its fault isolation between
modules, in complete isolation from any container or K8s layer, before
trusting any of that in a real Docker or Kubernetes deployment.

**Advantages:**
- Fastest possible iteration loop for shell-internal development — no image
  build, direct debugger access to the shell's own process.
- Isolates "is this a shell-wiring bug" from "is this a container or
  orchestration-layer problem," because there's no orchestration layer
  present at all to blame.

**Costs / limitations:**
- None of the platform's own guarantees are present — nothing here proves
  the shell will actually receive correct data once deployed for real; it
  only proves the shell's own internal logic behaves correctly given data
  you constructed yourself.
- The shell author is entirely on their own for reconstructing what a real
  `BRICKKIT_SERVED_MEMBERS`/`BRICKKIT_SERVED_MEMBERS_CONFIG` payload would
  look like — [Building a Qualified Shell](shell-implementers-guide.md) has
  the exact shape of both.

**Things worth knowing:**
- A shell's compiled module registry has to be keyed by the full versioned
  service name (`mdm-customer-1-0-7`, not the bare `mdm-customer`) if it's
  ever going to absorb two versions of the same component once it's really
  merging inside a real Docker or K8s deployment — a registry bug here
  stays invisible until it's tested with
  two real versions present, so it's worth deliberately exercising even in
  this isolated cell.

### 6. Shell-merged × Docker

**What it is:** as many components as make sense are absorbed into a small
number of shell containers; `brickkit up` generates one Compose service per
shell and none for the members it absorbs — a caller's `*_ENDPOINT` for an
absorbed member resolves straight to the shell's own address and port, with
no code change on the caller's side.

**When to use it:** once the arithmetic behind running everything as
independent Docker containers (each paying their own runtime's memory
floor) genuinely stops fitting the
machine you're targeting — not before. Before reaching for this: try a
lighter runtime for the expensive components first (a JVM component
rebuilt as a GraalVM native image drops its floor from 200–450MB to tens of
MB, with zero deployment-shape change at all), and check whether
`enabled: false` already excludes whatever's driving the number up for this
particular deployment. Merging is the right call only once those cheaper
options are exhausted and the memory economics still justify it for this
specific set of components.

**Advantages:**
- The core value: one shell container carries one Web-framework runtime
  baseline and one connection pool, instead of N of each — this is what
  actually collapses the aggregate memory floor.
- Fewer containers also means faster overall startup and a smaller
  day-to-day operational surface — fewer things to check logs on, fewer
  things to restart.
- `expose`/`exposePort`/`resources`/etc. written on an absorbed member's own
  entry are simply ignored, with a warning, rather than a hard error —
  nothing quietly breaks if you forget to strip them while migrating a
  component in.

**Costs / limitations:**
- Blast radius stops being "one component": every component sharing a shell
  shares its fault domain (one panic can take the others down if the shell
  isn't built to isolate that) and its scaling unit (you can't
  independently bump one absorbed member's replica count).
- The shell generates no migration container of its own for any absorbed
  member — someone's shell-startup code has to run every member's
  migration, in the right order, by hand.
- Whether the shell's declared version actually matches what's compiled
  inside its image is something no platform mechanism can verify from the
  outside — it's a discipline problem for whoever builds and publishes the
  shell image, permanently.

**Things worth knowing:**
- The shell itself is an ordinary `components:` entry, with its own `id`,
  `version`, image, `deployment.port`, and `healthCheck` — `servedBy`
  points *at* that entry; it isn't a special container type of its own.
- Two members absorbed into the same shell can never depend on two
  different exact versions of a third, shared component through that
  shell — the merge would need one `*_ENDPOINT` variable to mean two
  different addresses at once, and the platform refuses to generate rather
  than pick a winner silently.

### 7. Shell-merged × K8s

**What it is:** the same merging as shell-merging on Docker above, aimed
at a real cluster instead:
each shell is one Deployment, and every absorbed member gets its own
Service whose `selector` targets the shell's Pods and whose `targetPort` is
that member's own declared port — no Deployment or migration Job generated
for the member itself.

**When to use it:** the tightest end of the cloud-native resource spectrum
that also includes running independent components on K8s (resource-rich,
don't merge) and the mixed topology on K8s (moderate, merge most). Pick it
when cluster resources are genuinely scarce and minimizing
the aggregate footprint matters more than being able to independently scale
any one absorbed component.

**Advantages:**
- Gets both "merged for a low memory floor" and "K8s's own
  self-healing/cross-machine scheduling" in the same deployment — you don't
  trade one for the other.
- NetworkPolicy ingress/egress rules (where configured) automatically follow
  an absorbed member's own dependents and dependencies into the shell's
  rules — you never hand-write a separate rule just because the shell now
  also hosts one more component.

**Costs / limitations:**
- Every cost from merging on Docker still applies (shared fault domain,
  shared scaling unit, hand-rolled migration ordering) — merging doesn't
  get cheaper just because the target changed.
- Every K8s-specific cost from running independent components on K8s also
  still applies unchanged: real cluster-management skill required,
  `kubectl` installed separately,
  `hostname` (not `exposePort`) for exposure — merging components doesn't
  reduce what K8s itself demands operationally.

**Things worth knowing:**
- A Compose-style `depends_on` doesn't exist under K8s in the same form,
  but the equivalent substitution still happens: a dependency edge that
  would have pointed at a `servedBy` member automatically resolves to the
  shell instead, on both targets.
- Resource-binding checks treat a member's own `dependencies.resources` as
  satisfied once the *shell's* componentId is bound to the same
  `kind`+`engine` resource — you don't duplicate a binding under the
  member's own componentId just to satisfy the check.

### 8. Shell-merged × Docker + local debug

**What it is:** the same `local: true` overlay described for independent
components above, but the "rest of the topology" running for real is one
or more shells rather than independent
containers. A component you pull out this way can depend on a `servedBy`
member exactly like it would depend on any ordinary component: its
`*_ENDPOINT` resolves to a real, host-reachable `localhost:<port>` — the CLI
opens that mapping on the *shell's own* Compose service (the member has none
of its own to open it on), using the port the member itself declares, which
is also the exact port the shell is already required to listen on.

**When to use it:** you're actively changing one component that either sits
outside any shell, or that you've temporarily pulled back out of one for
the duration — and you want it talking to the real, currently-merged rest of
your topology (including anything living inside a shell) without rebuilding
any image.

**Advantages:**
- The address-resolution direction that's easy to assume doesn't work — a
  `local: true` component reaching *into* a shell to hit an absorbed member
  — is fully supported; you don't need to know anything special about which
  shell the member happens to live in.
- Everything else in the topology (other shells, other independent
  containers) keeps its full realism exactly as it does for independent
  components.

**Costs / limitations:**
- You cannot pull a shell-absorbed member itself out this way — there's no
  independent container for that one piece to become; debugging code that
  lives inside a shell means debugging it inside the shell's own process, or
  temporarily removing its `servedBy` field and giving it back an
  independent container for the session.
- Every limitation already described for the independent-components version
  of this overlay (hand-written literals not auto-rewritten, no migration
  container for the `local: true` component) still applies unchanged.

**Things worth knowing:**
- This mapping depends on the shell already being required to listen on the
  absorbed member's declared port — a requirement the platform checks for
  you at generation time (a port collision here fails `brickkit up`, it
  doesn't silently misroute).
- The reverse direction — an ordinary container depending on something
  you've made `local: true` — works too, through the same `extra_hosts`
  mechanism described above; neither direction is the "special case."

### 9. Mixed × bare process

**What it is:** whichever components you actually need for what you're
working on — some independent, some living inside a shell in the real
topology — all run as plain host processes, with the same caveats as
running independent components by hand (nothing platform-managed,
everything hand-supplied) or running a shell by hand (hand-built member
data), whichever shape each one really is.

**When to use it:** the everyday local-development default *once your
project's real production topology already includes at least one shell*.
Rather than standing up the full topology to change one component, run just
the handful you need (in whichever shape they'll really deploy as) as bare
processes — your local results for address resolution and cross-process
calls still reflect what the real deployment will actually do, because
you're not simulating a different topology than production uses.

**Advantages:**
- Keeps the fast bare-process iteration loop available even after your
  project has grown past "everything's independent."
- Because you're deliberately mirroring each component's *real* shape
  (independent vs. shell-shaped) rather than always treating everything as
  independent, you catch shape-specific bugs (a shell's module-registry
  keying, say) locally instead of only discovering them in a real
  deployment.

**Costs / limitations:**
- The most manual-setup-intensive cell in this guide — you're
  hand-reconstructing environment data for a topology with two different
  shapes mixed together, not just one.
- Nothing checks that your hand-built "which components go where" actually
  mirrors the real `brickkit.yaml` — that consistency is entirely on you.

**Things worth knowing:**
- The `local: true` + `--dry-run` cheat-sheet trick described above works
  the same way here for an independent component; for a component that's really
  shell-shaped in production, there's no equivalent shortcut — you either
  construct its `BRICKKIT_SERVED_MEMBERS`-shaped data by hand, or skip
  modeling that part and just run the bare business logic you need to
  exercise.

### 10. Mixed × Docker

**What it is:** some components are independent Docker containers, some are
absorbed into one or more shells — `brickkit up` generates and drives the
entire hybrid topology in a single pass, with no extra flags: which
components go where is read straight out of each component's `servedBy`
declaration (or absence of one) in `brickkit.yaml`.

**When to use it:** this is usually where a real, mature project actually
settles — and it's worth being clear that it's typically a *structural*
shape, not a resource-tuning compromise between running everything
independently and merging everything into shells. Some components
genuinely can't or shouldn't merge regardless of how much memory headroom
the rest of the topology has: a pure frontend served by something like
nginx has no backend process to absorb into anything, and a stateless
gateway-shaped component is often kept independent on purpose specifically
so it can scale on its own curve, decoupled from whatever's happening
inside any shell. If you're trying to pick one combination for a typical
on-prem delivery and aren't sure, this is usually the right default.

**Advantages:**
- Gets independent components' per-component independence exactly where
  it's structurally needed, and shell-merging's memory savings everywhere
  else, in one deployment — you're not forced to choose one trade-off for
  the whole project.
- Nothing about mixing the two shapes needs special-casing anywhere in
  `brickkit.yaml` — an independent component's entry looks exactly like an
  ordinary one, a merged one's looks exactly like a `servedBy` one, side by
  side, in the same file.

**Costs / limitations:**
- The heaviest cognitive load of the three Docker-target cells:
  troubleshooting means holding two mental models at once — "which shell is
  this member actually in" for merged components, and ordinary per-container
  reasoning for independent ones.
- Every cost specific to shells still applies to the merged half, and every
  cost specific to independence still applies to the independent half —
  mixing doesn't average the costs away, it just lets you place them
  deliberately.

**Things worth knowing:**
- An old, still-independent version of a component and a new, shell-merged
  version of the same component ID can coexist with zero coordination
  between their callers — this isn't special mixed-topology machinery, it
  falls straight out of versioned service names (see "One thing that holds
  across every cell," below).
- Migrating a previously-independent component into a shell is the moment
  its `resources:` block (and `expose`/`exposePort`/`replicas`/etc.)
  silently stops doing anything — those fields have to move to the shell's
  own entry, or the shell just runs without the quota you thought you'd
  carried over.

### 11. Mixed × K8s

**What it is:** the same hybrid topology as the mixed-on-Docker cell above,
targeting a real cluster:
independent components get their own Deployment + Service, absorbed ones
get a Service routed to their shell's Pods with no Deployment or migration
Job of their own — generated in one pass from the same `servedBy`
declarations, no extra configuration needed for the mix itself.

**When to use it:** the middle point of the cloud-native resource spectrum,
between shell-merging on K8s (resources tight, merge as much as possible)
and running independent components on K8s (resources plentiful, merge
nothing) — reach for this when cluster resources are moderate: keep most
components inside shells to hold down the aggregate footprint, and
deliberately pull out just the few that genuinely need their own replica
count or resource quota, independent of everything else. Unlike the
structural mixing described for the Docker version of this cell, the
mixing decision here is often a resource-tuning one — a component that
could be merged might be kept independent here
specifically because it needs to scale on its own, even though nothing
structural prevents merging it.

**Advantages:**
- The most resourcing flexibility of any single cell — any given component
  can sit on whichever end of the spectrum actually fits its own load
  pattern, rather than forcing the whole project to one extreme.
- Every mechanism (address resolution, NetworkPolicy, resource-binding
  checks) already handles the mix natively — there's no separate "hybrid
  mode" to configure beyond ordinary `servedBy` declarations.

**Costs / limitations:**
- Combines the full K8s operational cost of running components
  independently with the full merging cost of merging them into shells —
  nothing about mixing reduces either.
- The most moving parts to reason about when something goes wrong: is this
  component independent or merged, is the problem in K8s itself, in the
  shell's internal wiring, or in an ordinary container — mixed topology
  means checking all three possibilities, not assuming one.

**Things worth knowing:**
- Every caveat already described for independent components on K8s
  (hostname required for `expose`, `kubectl` installed separately) applies
  to whichever components in this deployment are independent; every caveat
  already described for shell-merging on K8s (shared fault domain,
  hand-rolled migration order inside the shell) applies to whichever ones
  are merged — check both if you're troubleshooting a mixed deployment and
  don't yet know which half the problem is in.

### 12. Mixed × Docker + local debug

**What it is:** a real hybrid topology — independent components and shells
side by side — running in Docker, with one
component pulled out via `local: true` for active debugging — the most
realistic development-time combination in this guide, because "the rest of
the system" is whatever your project's actual production shape already is,
not a simplified stand-in for it.

**When to use it:** you're changing and verifying one component against a
topology that's already the real mix your project runs in production —
typically the component you're pulling out is one of the
structurally-independent ones (a frontend, a gateway), since a
shell-absorbed member has the same "can't be pulled out individually"
limitation already described for the shell-merged version of this overlay.

**Advantages:**
- The closest thing to "debug against production" available without
  actually touching production — every other component, independent or
  shell-merged, behaves exactly as it would for real.
- Both address-resolution directions work regardless of which kind of
  component sits on the other end: your `local: true` component can depend
  on an independent container or a shell member, and an independent
  container or shell can depend right back on your `local: true` component,
  all through the same `extra_hosts` mechanism described above.

**Costs / limitations:**
- Requires understanding every mechanism this guide covers at once: shell
  merging, the `local: true` `extra_hosts` trick, and ordinary
  independent-container behavior — there's no simpler mental model to fall
  back on here.
- Same hard boundary already described for the shell-merged version of
  this overlay: you cannot pull a shell-absorbed member itself out to
  debug it this way.

**Things worth knowing:**
- Nothing about this cell changes based on how many components are merged
  versus independent — one component pulled out, everything else real, is
  the entire mechanism, regardless of how the "everything else" is
  internally shaped.

## One thing that holds across every cell: multiple versions coexist for free

None of the twelve cells above change how multiple versions of the same
component behave — it's not a variable this guide's matrix has a second
axis for, because versioned service names (AGENTS.md §5.1) already make
version coexistence topology- and target-independent: an old caller and a
new caller, an independent version and a shell-merged version, or two
different exact versions absorbed into the same shell can all coexist in
one deployment with zero coordination on the platform's side — the platform
never checks whether an old caller and a new dependency (or vice versa)
actually behave correctly together; keeping that contract compatible across
versions is entirely the component authors' own responsibility, by design
(AGENTS.md §9.2/§9.3). shell-implementers-guide.md's ["Multiple
versions, mixed deployment shapes"](shell-implementers-guide.md#multiple-versions-mixed-deployment-shapes)
works through the concrete scenarios and the one real per-language trap
(a Go shell's build system can't statically link two versions of the same
module path without semantic import versioning) in detail — that content
doesn't repeat here because it's identical regardless of which cell above
you're actually deploying under.

## Once you've decided

- Some components will be shell-merged: read
  [Declaring servedBy: a deployment checklist](servedby-deployment-checklist.md)
  per component you're routing into a shell, and
  [Building a Qualified Shell](shell-implementers-guide.md) if you're the
  one building that shell.
- Targeting K8s: [`brickkit up --context <cluster>`](../architecture/cli-reference.md#brickkit-up),
  and [Real generated Docker Compose and Kubernetes files, side by side](../architecture/deployment-generation.md)
  if you want to see exactly what gets produced from the same Manifest under
  each target.
- Multiple environments (say, a `brickkit.prod.yaml` alongside your default
  config): each one is fully self-contained — `brickkit up --config
  brickkit.prod.yaml` — with no overlay or inheritance between them
  (AGENTS.md §9.9).
