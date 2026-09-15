# Declaring `servedBy`: A Deployment Checklist

> Prerequisite: read [Architecture overview](../architecture/overview.md) first if you
> haven't — this guide assumes you already understand versioned service
> names and environment-variable injection.

This is a different document from [Building a Qualified Shell](shell-implementers-guide.md).
That one is for whoever *builds* the shell component — the container that
absorbs another component's workload. This one is for whoever *declares*
`servedBy` on a component in their own `brickkit.yaml` — deciding to route
a component into an existing shell rather than let it run in its own
container. If you're doing both jobs yourself, read both; the split exists
because on a real team they're often different people, and each needs to
know things the other doesn't.

## When `servedBy` is the right call — and when it isn't

The problem `servedBy` actually solves is a memory-floor problem, not a
general-purpose "fewer containers is better" one. Each running process has
a floor cost regardless of load, and that floor is set almost entirely by
language runtime (AGENTS.md §6): Go/Rust sit at 8–20MB, Python/Node at
40–90MB, a JVM at 200–450MB. Twenty idle Spring Boot components is 4–9GB
before any of them do a single unit of work — a real constraint during a
private, resource-capped on-prem delivery. Twenty idle Go components is
well under half a gigabyte, which is rarely worth the trade below.

Before reaching for `servedBy`, measure the real footprint first — `docker
stats` against what's actually running, not a number from a table someone
else published — then check the options below, roughly in this order:

| Option | Verdict |
| --- | --- |
| **A lighter runtime** (a JVM component rebuilt as a GraalVM native image, a smaller heap) | **Try this first.** The memory floor drops from 200–450MB to tens of MB, and the deployment shape doesn't change at all — still one component, one container, one process; signing, health checks, migrations, and independent scaling all keep working exactly as before. This is the component author's problem to solve, not something `servedBy` or this checklist has anything to do with. If the motivation is "the JVM is expensive," this option's cost-to-benefit ratio beats merging by an order of magnitude. |
| **On-demand activation** (`enabled: false` for whatever this deployment doesn't need) | A 50-component project might only run 4 containers locally — check whether the components driving the memory number are even needed in this deployment before assuming they all have to run. |
| **Scale-to-zero** (KEDA, Knative scaling to 0 replicas) | **Doesn't work on this platform.** Components call each other over direct DNS — nothing on that call path can wake a scaled-to-zero component back up, so the request just fails. Making it work needs an activator or proxy inserted into the call path, which is exactly the API-gateway/service-mesh shape the platform deliberately doesn't build (AGENTS.md §4.1) — and once something sits between a caller and its `*_ENDPOINT`, the very property that makes `servedBy` safe to build on (a caller never needs to know what's on the other end) stops holding. |
| **Memory overselling** (`requests` set low, `limits` set high) | **Actively harmful, not just unhelpful.** Memory isn't compressible: `requests` far below `limits` produces Burstable QoS, and when a node runs short, eviction is ordered by how far a Pod's real usage exceeds its own `requests` — which puts your heaviest, most important component first in line to be killed. AGENTS.md §6 already recommends the opposite: `requests == limits` for memory (Guaranteed QoS, evicted last), CPU `requests` with no `limits` at all. |

Only once the cheaper options above are exhausted does merging make sense.

And merging has a real cost, paid by the shell's author, not by the
platform: every component absorbed into one shell shares its fault domain
(one panic can take the others down with it, if the shell isn't built
carefully — see the implementers guide's nine properties), shares its
scaling unit (you can no longer scale one absorbed component's replica
count independently of the others in the same shell), and loses its own
migration container (someone's startup code has to run migrations in the
right order by hand). `servedBy` is the right call when the memory
economics justify paying that cost for *this specific set* of components,
against a shell you already have — or are willing to build and verify —
that satisfies the implementers guide's requirements. (If the components
you're merging talk to PostgreSQL or Oracle, there's a second resource
question beyond memory worth reading before you commit to this — see
[Sharing a database connection pool inside a shell](shared-connection-pools.md).)
It's the wrong call as a default way to tidy up a project, and the platform
doesn't nudge you toward it: the ordinary path (each component in its own
container) remains
what you get by simply not writing `servedBy` at all.

## Before you write `servedBy:`

Four things need to already be true:

1. **The shell is an ordinary `components:` entry in your config**, with
   its own `id`, `version`, image, `deployment.port`, and `healthCheck` —
   `servedBy` points *at* a component, not at a container you configure
   some other way. If the shell doesn't exist as a component yet, add it
   first.
2. **The shell has actually been checked against the nine properties** in
   [Building a Qualified Shell](shell-implementers-guide.md#nine-properties-a-merged-process-must-satisfy).
   The platform cannot verify any of them — it only wires addresses
   correctly assuming they hold. Declaring `servedBy` against a shell that
   silently violates property 4 (identity leaking across a shared
   connection pool) or property 8 (a duplicate-registration panic) doesn't
   fail at `brickkit up` time; it fails later, in production, in a way
   that looks nothing like a `servedBy` problem.
3. **You've picked the port this component will present**, and confirmed
   it doesn't collide with the shell's own port or any other member
   already routed into that same shell. The platform checks this for you
   at generation time (see below) and refuses to generate if it doesn't
   hold — but deciding it up front avoids a failed `up` you have to
   untangle after the fact.
4. **The shell is actually going to be running in this deployment** — not
   `enabled: false`, and not `local: true`. A `servedBy` component whose
   shell isn't running has no container anywhere for its code to execute
   in, which the platform treats as an error, not a warning.

## Writing the field, and what gets rejected

```yaml
components:
  - id: mdm/customer
    version: 2.0.0
    servedBy: infra/shell-go-core@1.0.0   # <component-id>@<exact-version>
    deployment: { port: 8081 }
```

The value has to be an exact version — the same `major.minor.patch`
restriction as every other version reference on this platform (AGENTS.md
§9.2); `^1.0.0` or a bare `infra/shell-go-core` is rejected at parse time,
before anything about the dependency graph is even considered. Four more
things are rejected the same way, all without needing to resolve the
dependency graph first:

- **Pointing at itself.** `mdm/customer@2.0.0` declaring
  `servedBy: mdm/customer@2.0.0` is caught immediately.
- **Chaining.** If the shell you name has itself declared a `servedBy` (it
  is, in turn, absorbed into some other shell), the platform rejects this
  — a shell can't be nested inside another shell.
- **Combining with `local: true`.** These express contradictory intents on
  the same entry: `local: true` means "this code runs on my machine for
  debugging," `servedBy` means "this code is already compiled into that
  other image." The platform rejects declaring both on the same
  component — and, in the other direction, rejects marking a component
  `local: true` if some other component already names it as a
  `servedBy` target, for the same reason: a shell has to be reachable on
  the cluster or container network, and a developer's own machine isn't.

Two more checks need the full dependency graph, so they surface at
generation time (`brickkit up`) rather than parse time:

- **The named shell doesn't exist** in this project's `components:` list —
  usually a typo in the component ID or version.
- **The named shell exists but isn't running** — most often because it's
  been turned off with `enabled: false`. There's genuinely no container
  anywhere for this component's code to run in until that's fixed.

## What moves to the shell's own entry instead

`expose`, `exposePort`, `hostname`, `replicas`, `resources`,
`serviceAccountName`, and `labels` on a `servedBy` component's own entry do
nothing — the platform warns about this rather than silently ignoring it or
erroring, because these fields all describe "how my own
container/Pod gets deployed," and a `servedBy` component doesn't have
one. If you need the merged deployment exposed, scaled, resourced, or
labeled a particular way, write those fields on **the shell's own entry** —
it's the one that actually has a container. This is easy to miss the first
time: you migrate a previously-standalone component into a shell, forget
its old `resources:` block is now inert, and the shell silently runs
without the quota you thought you'd carried over.

`labels` used to be the one field on this list that actually did merge from
every member (same collision rule as `*_ENDPOINT` variables below), until
real multi-component migrations showed that rule was unworkable: a label
whose value is legitimately supposed to differ per component —
`prometheus.io/port` is the everyday case, its value is each component's own
port number — triggered a "same key, different value" collision on
essentially every real merge, not on a genuine mistake. The platform doesn't
special-case that one key (or start a list of "keys known to vary"), because
that would mean interpreting specific label semantics — exactly what
`labels` as a plain passthrough is supposed to avoid. So it moved into this
bucket instead: a member's `labels` are ignored (with a warning), and only
the shell's own entry's `labels` land on the merged container.

## What happens automatically once you declare it

None of the following needs any extra configuration beyond the `servedBy`
field itself — these are the parts of the platform's own contract, and
they were specifically hardened (a servedBy member's traffic used to be
invisible to both of the mechanisms below) to make sure `servedBy`
composes correctly with the rest of a project's config rather than being
a special case that opts out of it:

- **Every caller's `*_ENDPOINT` for this component** resolves to the
  shell's address, using this component's own declared port — a caller's
  dependency declaration and the address it computes never change based
  on where the dependency physically lives.
- **NetworkPolicy** (if `deploy.networkPolicy.enabled`): the shell's
  generated ingress rules automatically include every absorbed member's
  own dependents, using that member's own port — you don't write a
  separate `allowFrom` entry for the shell just because it now also hosts
  an absorbed component.
- **Egress** (if `deploy.networkPolicy.egress.enabled`): a caller's
  egress rule for a `servedBy` dependency automatically targets the
  shell instead of a container that was never generated.
- **Docker's `depends_on`**: a Compose service that depends on a
  `servedBy` component automatically depends on the shell's service and
  its readiness condition instead.
- **Resource-binding checks**: a member's own `dependencies.resources` (in
  its `component.yaml`) count as satisfied once the shell's own componentId
  is bound to a resource of the same `kind`+`engine` — you don't need a
  second, functionally-inert `bindings` entry under the member's own
  componentId just to satisfy `brickkit up`'s check. The real connection env
  vars only ever land in the shell's container regardless of which
  componentId the binding names.

## What does not happen automatically

**Migrations.** The platform generates no migration container or Job for
a `servedBy` component — it emits a warning at generation time (naming
the migration command that's being skipped) precisely so this isn't a
surprise discovered at runtime. Running that migration, in the correct
order relative to every other absorbed module's own migrations, is the
shell's own startup code's job (property 7 in the implementers guide) —
not something you configure from the deployment side at all. If you're
migrating a previously-standalone component into a shell, confirm the
shell's startup sequence has actually been extended to cover it before
you rely on it in production.

**Anything that assumes one container per component.** Tooling that lists
"one component, one container" (some observability or debugging setups
build this assumption in) needs to be told to look inside the shell
instead for a `servedBy` component's logs, metrics, or shell access —
the platform has no mechanism to paper over this, because there
genuinely isn't a separate container to point at.

**External tools/scripts hitting a component's port directly — you compute
the address yourself.** A local dev script (a seed-data script, say) or an
ops tool that needs to bypass a component's business API and hit its
gRPC/HTTP port directly gets no dedicated platform support for this — and
doesn't need any, because the formula is exactly the same as for a
standalone-deployed component: `http://<versioned-service-name>:<the
component's own declared port>`, detailed in [the overview's "External
tools connecting to a component's port directly" section](../architecture/overview.md#external-tools-connecting-to-a-components-port-directly).
The easy trap is carrying over pre-migration habits — locating a container
by name prefix (a `servedBy` component has no container of its own), or
assuming the port is published to the host (`expose: true` has no effect
at all on a `servedBy` member).

## Conflicts the platform catches for you

All three of these fail `brickkit up` outright, at generation time, rather
than generating something that quietly does the wrong thing:

- **Port conflict.** The shell itself and every member routed into it draw
  from one shared port space. Two owners claiming the same port — the
  shell's own port and a member's, or two members' — fails generation,
  naming both owners.
- **Endpoint collision.** Two members absorbed into the same shell each
  depend on a different exact version of the same component ID. Merging
  their environments would need one variable name (`*_ENDPOINT`, derived
  from the component ID with no version in it) to hold two different
  values at once — a genuine conflict, not something the platform can
  silently resolve by picking one, so it refuses to generate.

Members' `deployment.labels` used to be merged the same way, with the same
collision check — it no longer is. See "What moves to the shell's own entry
instead," above: a member's `labels` are ignored (with a warning) rather
than merged or checked for collisions at all.

## Multiple versions and mixed deployment shapes

Migrating a component into a shell is rarely an instant, all-at-once
cutover — some callers stay on an old, still-standalone version while
others move to a new, shell-hosted one; two versions might even share the
same shell for a while. None of this is `servedBy`-specific machinery —
it all falls out of the platform's existing exact-version-pinning rules
applied to one more ordinary field. The implementers guide already works
through the concrete scenarios and the one real per-language trap (Go's
build system can't statically link two versions of the same module path
into one binary without semantic import versioning, which most components
don't use) in detail:
[Multiple versions, mixed deployment shapes](shell-implementers-guide.md#multiple-versions-mixed-deployment-shapes).

## A complete example

```yaml
components:
  - id: infra/shell-go-core
    version: 1.0.0
    image: brickenterprise/be-shell-go:1.0.0
    deployment: { port: 9000 }
    healthCheck: { path: /healthz }
    resources:
      requests: { cpu: "500m", memory: "512Mi" }

  - id: mdm/customer
    version: 2.0.0
    servedBy: infra/shell-go-core@1.0.0
    deployment: { port: 8081 }        # unique among this shell + its members

  - id: mdm/orders
    version: 1.3.0
    servedBy: infra/shell-go-core@1.0.0
    deployment: { port: 8082 }        # unique among this shell + its members
```

Both `mdm/customer` and `mdm/orders` get correct `*_ENDPOINT` values for
their own callers, both route through `infra/shell-go-core`'s address, and
neither generates a container, a migration Job, or a NetworkPolicy rule of
its own — all of that is the shell's, computed once and shared. The
`resources` block that governs the actual footprint you're trying to save
lives on the shell's entry, not on either absorbed component's.
