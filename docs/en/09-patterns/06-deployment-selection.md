# Choosing a deployment shape

This page answers one question only: **what should a project with N components actually run as?** It doesn't teach you
how to build a shell (see [Writing a shell](../04-shell/05-shell-development.md)), nor the steps to put a component into a
shell (see [Managing members](../04-shell/04-members-management.md)). Read this page first, before the project's overall
shape is decided.

## The short answer

| You want to | Run it as |
| --- | --- |
| Keep it running on one machine: a delivery, a small production | Containers on **rootless Podman**; Docker is equally supported, for a machine that already runs Docker or tools that only speak Docker |
| Keep it running with self-healing and scaling across machines, or on a cluster you already have | Containers on Kubernetes |
| Change one or two components against the real rest | Containers, with those components as processes on this machine (`mode: debug` / `mode: local`) |
| Show it or try it on a machine without a container engine | Every component `mode: local` — a demo or a trial, never a delivery |

Whichever of these, the topology is decided separately: start with every component standalone, move components into
shells when the memory really doesn't add up, and expect to end up mixed.

**Containers are the main line.** The engine restarts a crashed container and starts it again at boot, and the image
pins the runtime the component was tested with. **Processes on this machine are the convenient alternative**: they need
no container engine and no image, but they live only as long as the `up` that started them, and they run on whatever
toolchain the machine has. More and more deployments are run by scripts and AI assistants, for which installing a
container engine is no burden; so the cases left for processes are development, demos and trials.

## Three decisions

1. **Where the containers run** — `target: podman`, `docker` or `k8s` in the deploy file.
2. **The topology** — how many containers the components end up in: all standalone, all in shells, or mixed.
3. **Whether some components run as processes on this machine this time** — a development overlay, or, with every
   component that way, a demo without containers. It is described last, in
   [Off the main line](#off-the-main-line-processes-on-this-machine).

The first two are independent axes: any topology runs on any target, and switching either is switching one deploy
file, without a word of `config/` changing. [The matrix](#the-matrix-topology--where-it-runs) lists their six cells.

## Decision one: where the containers run

This is almost entirely an infrastructure question, not a performance one: do you have a Kubernetes cluster, do you need
scheduling across machines and self-healing? If not, one machine runs everything with `podman compose` or
`docker compose`, the simplest to operate. If so, `k8s` generates Deployments / Services / Ingresses.

### One machine: rootless Podman, or Docker

BrickKit generates the same `compose.yaml` for `target: podman` and `target: docker` — the only difference is the
restart policy (see [Coming back after a reboot](#coming-back-after-a-reboot)) — and every `brickkit` command works the
same on both. What differs is the engine on the host:

| Engine | What runs on the host | Upside | Cost | For |
| --- | --- | --- | --- | --- |
| **Rootless Podman** (recommended) | No daemon: each container has a small monitor process (`conmon`); a user-space helper (`pasta`) carries the containers' network | The engine never runs as root; the least resident memory | The user-space networking costs CPU; the host needs a few settings ([below](#setting-up-a-host-for-rootless-podman)); ports below 1024 can't be published as they are | Single-machine delivery; small machines; hosts that don't allow a root daemon |
| **Docker** | The Docker daemon (`dockerd`, `containerd`), as root | The most widespread, the most surrounding tooling; setting it up is installing it | The daemon's resident memory | Machines that already run Docker; tools that only speak Docker |
| **Rootful Podman** | Podman as root; the kernel carries the network | No user-space networking | The engine runs as root; next to Docker on one machine, containers can't reach each other until Podman's bridges are let through (see [the troubleshooting section](../10-troubleshooting/01-up-down-issues.md#all-healthy-on-podman-yet-calls-between-containers-time-out)) | When the networking CPU matters and root is acceptable |

**What one project measured.** One machine (Intel i5-12450H, 14 GiB, Linux); 12 components (11 Go, 1 Python) and 3
shells, with PostgreSQL, NATS, object storage and an identity provider; a mixed load of 7 requests a second; Podman
5.7.0, Docker 29.9.2, a single-node minikube v1.38.1.

| | Rootless Podman | Docker | Kubernetes, single node |
| --- | --- | --- | --- |
| The platform's own resident memory | about 0.14 GiB | about 0.55–0.7 GiB (the daemon) | about 2 GiB (control plane and kubelet) |
| CPU of the whole stack | no difference from Docker (6% more, within the noise) | — | about 8% more (within the noise) |
| Latency | no difference | — | no difference |

- The engine's or the cluster's own cost is paid once per machine, whatever the components do, so it weighs most on a
  small machine: on this stack (about 1.4 GiB in all on Podman) the Docker daemon's half GiB was about a third of the
  total.
- Rootful Podman used 9–10% less CPU across the stack than rootless: that is the user-space networking.
- These are one project's numbers on one machine; the components' own memory and CPU were the same on every engine.
  Measure your own stack before sizing a machine.

### Setting up a host for rootless Podman

As the user who will own the deployment:

1. Install Podman and Docker's Compose V2 (`docker-compose`). `podman compose` has no compose implementation of its own:
   it forwards to that program, which talks to Podman; the Docker Engine itself isn't needed. `podman compose version`
   should print `Executing external compose provider ".../docker-compose"`; the separate `podman-compose` project hasn't
   been verified with BrickKit.
2. `systemctl --user enable --now podman.socket` — `brickkit up`, `down` and `status` reach Podman through it.
3. `sudo loginctl enable-linger <user>` — the user's services keep running after logout, and start at boot without
   anyone logging in.
4. `systemctl --user enable podman-restart.service` — starts the containers again at boot.
5. A port below 1024 (`exposePort: 443`, say) can't be published by a rootless engine as is: publish a port from 1024
   up and let the gateway in front take 443, or lower `net.ipv4.ip_unprivileged_port_start` on the host.
6. Run the [environment check](../10-troubleshooting/01-up-down-issues.md#podman-run-the-environment-check-first) once;
   it tests that containers can reach each other and be removed cleanly.

What has been verified with BrickKit is rootless Podman on a machine that also has Docker; a machine with only Podman
and the Compose binary hasn't been verified end to end.

### Kubernetes

For a cluster you already have, or when you need self-healing and scaling across machines. What it costs:

- **The cluster's own resources.** On one node, the control plane and kubelet are a fixed cost (about 2 GiB in the
  measurement above): a single-node cluster for a small project mostly buys that cost.
- **Operations.** It needs real cluster administration, and the machine running `up` needs `kubectl` and a context that
  reaches the cluster (`k8s.context`).
- **A crash.** The Pod is restarted and ready again within a few seconds; requests sent to it meanwhile wait or fail.

Processes on this machine don't combine with `k8s`: `mode: debug` / `mode: local` meeting `target: k8s` is refused while
loading, the error pointing at the offending entry (`components[N].mode`) — a Pod in the cluster can't reach your
machine.

### Coming back after a reboot

Containers are the shape that keeps running: the engine restarts a crashed container and starts them again at boot. What
has to be in place for each:

| Engine | After a crash | At boot |
| --- | --- | --- |
| Docker | `restart: unless-stopped`, generated | The Docker service enabled at boot (most packages do it; check with `systemctl is-enabled docker`) |
| Rootless Podman | `restart: always`, generated | Linger for the user and the user unit `podman-restart.service` enabled ([steps 3 and 4](#setting-up-a-host-for-rootless-podman)) |
| Rootful Podman | `restart: always`, generated | The system unit enabled: `sudo systemctl enable podman-restart.service` |
| Kubernetes | The kubelet | The cluster |

- Podman gets `always` because its boot unit starts only containers whose policy is `always`; otherwise the two
  policies behave the same in Podman. On Docker, `unless-stopped` keeps a container you stopped by hand stopped.
- What comes back is what the last `up` created. `down` removes the containers, so nothing comes back after it.
- At boot the engine starts the containers by itself: not in the order `up` waits through (`depends_on`), and the
  migrations don't run again — they ran at the last `up`. A component that starts before its dependency is reachable
  must keep retrying rather than give up; it needs that anyway, for a dependency that restarts.
- Processes on this machine never come back: nothing restarts them or starts them at boot.

## Decision two: topology — how many containers do the components end up in?

- **All standalone**: each component its own container (or Pod). This is the shape you get by writing nothing extra.
- **All in shells**: as many components as can be merged are compiled into a few shells, one container per shell.
- **Mixed**: some in shells, some standalone. This is usually the norm, not a compromise: some components **structurally**
  can't go into a shell — a pure frontend served by nginx has no backend process to compile in; a stateless gateway is
  often kept standalone on purpose, so it can scale on its own load.

A shell is itself an ordinary component (marked `kind: shell` in `brickkit.yaml`); the members it can host are written in
`shell.members` of its `component.yaml`, and **which it hosts this time** is written in the deploy file — member entries
nested under the shell entry's `members`. So topology is decided **per deploy file**: the development machine's
`deploy.yaml` can be all standalone while production's `deploy.prod.yaml` gathers a few members into shells, without a word
of `config/` changing.

**When to merge.** Each process's memory floor is almost entirely set by its language runtime — Go / Rust 8–20 MB,
Python / Node 40–90 MB, JVM 200–450 MB (typical orders of magnitude; measure your own components). Twenty idle Go
components add up to under half a GB; twenty idle Spring Boot components already take 4–9 GB without doing anything. Stay
all standalone until that sum really doesn't work out on the machine you deploy to, and before merging try cheaper things
first: give the expensive components a lighter runtime (compiling JVM components into GraalVM native images drops the
memory floor from hundreds of MB to tens, with the deployment shape unchanged); check whether the components driving the
number up could be `mode: disable` this time anyway.

**What merging saved in one project** (the measurement above): with every component in shells, the components used
14–28% less CPU and 32–37% less memory on every engine, with no difference in latency, and the platform's cost fell with
the number of containers. The memory of the whole stack didn't necessarily fall: one database connection of a shell keeps
caches for each of its members, and PostgreSQL grew by about 210 MiB, cancelling what the components saved. Merging
reliably saves CPU and containers; measure the memory.

## The matrix: topology × where it runs

|  | One machine (Podman / Docker) | Kubernetes |
| --- | --- | --- |
| **All standalone** | [1. The standard shape while one machine is enough](#1-all-standalone--one-machine) | [2. The most generous end](#2-all-standalone--kubernetes) |
| **All in shells** | [3. Squeezing memory when one machine isn't enough](#3-all-in-shells--one-machine) | [4. The tightest end](#4-all-in-shells--kubernetes) |
| **Mixed** | [5. Where most real projects end up](#5-mixed--one-machine) | [6. The middle ground on resources](#6-mixed--kubernetes) |

### 1. All standalone × one machine

**What it is**: one Compose service per component, driven end to end by `brickkit up`: address injection, health checks,
starting in dependency order, migrations.

**When**: the default shape while one machine can hold the components (see [When to merge](#decision-two-topology--how-many-containers-do-the-components-end-up-in)).

**Upside**: every component can be started and stopped on its own, and trouble affects only itself; one component means
one container, one log, one health state — the simplest to troubleshoot.

**Cost**: every component pays its own runtime memory floor — the very reason shells exist; a single-machine shape, with no
scheduling across machines.

**Worth knowing**: measure before deciding "it doesn't fit" (`podman stats`, `docker stats`); don't take numbers off the
table — a 50-component project may run only a few, because most are `mode: disable` this time, or nothing needs them.

### 2. All standalone × Kubernetes

**What it is**: one Deployment + Service per component, with addresses resolved by the cluster's native Service DNS.

**When**: the cluster has resources to spare, and there's no need to trade merging for a lower memory floor. Every
component keeps its own `replicas` and its own `resources`.

**Upside**: truly independent horizontal scaling; scheduling across machines and failure recovery work per component.

**Cost**: the highest operational complexity; many components means many Deployment / Service objects.

**Worth knowing**: on K8s, `expose: true` needs `hostname` (Ingress routes by domain name), and `exposePort` does
nothing; a missing `hostname` is stopped by `up --dry-run`. Addresses in `config/` that assume a single-machine container
network have to be changed when moving to K8s — giving different values per environment with the deploy file's `vars:` is
the least work.

### 3. All in shells × one machine

**What it is**: as many components as possible compiled into a few shells; one Compose service per shell, and members
generate no service container of their own. A caller's `*_ENDPOINT` for a member is the member's own address,
`http://<member's service name>:<its own port>` — the same as when it runs on its own — and that name reaches the shell:
a network alias of the shell's container (on K8s, a Service selecting the shell's Pod). Caller code doesn't change, and
neither does the address when a member moves into or out of a shell.

**When**: when the memory sum of cell 1 really doesn't work out — not before.

**Upside**: a shell carries one runtime floor and one connection pool, not N; fewer containers, faster start-up, fewer logs
to watch and fewer things to restart.

**Cost**: members sharing a shell share a failure domain (one crash restarts the whole shell, unless the shell is built
very carefully) and a unit of scaling (one member can't get extra replicas on its own); upgrading one member means
rebuilding the shell.

**Worth knowing**:
- Members' migrations **are still run by the platform**: with the member's own image and config, and the shell starts only
  once they succeeded (see [Migrations and shells](../05-migration/04-shell-interaction.md)). So every member must have an
  image of its own — except, with the shell running on this machine, a member compiled in from its local repository: it
  migrates from source (`local.migrationCommand`).
- For a shell image built on this machine, `brickkit build` records in a label which member versions it compiled in, and
  `up` compares that label with `shell.members` in the shell's `component.yaml`: a mismatch stops it (`IMAGE_STALE`),
  rather than a shell missing a member quietly starting. An image without the label (built by hand) only gets a warning
  (`IMAGE_UNVERIFIED`); pulled images aren't checked — a released version's image matches its `component.yaml` by
  construction.
- Fields on a member entry describing "how its own container is deployed" (`replicas`, `resources` and so on) do
  nothing inside a shell; they're kept for when the member falls back to a standalone component. `expose` is the
  exception: the shell opens the member's port (a mapping on the shell's container on one machine, an Ingress to the
  member's Service on Kubernetes).
- Merging can create a start wait cycle; `up` stops it and gives three ways out (see
  [Managing members](../04-shell/04-members-management.md#start-cycles-caused-by-merging-and-skipwaitfor)).

### 4. All in shells × Kubernetes

**What it is**: the same merging against a cluster: one Deployment per shell; one Service per member, whose `selector`
picks the shell's Pod and whose port is the member's own declared port; members generate no Deployment of their own.

**When**: the cluster's resources really are tight, and pushing the total cost down matters more than scaling a member on
its own.

**Upside**: both "merging saves memory" and "K8s self-healing and scheduling across machines". With network policies on, a
member's callers and dependencies are counted into the shell's rules automatically.

**Cost**: every cost of cell 3 holds unchanged; so does every K8s cost of cell 2 — merging doesn't lower K8s' own demands.

**Worth knowing**: Pods have no start order on K8s, so wait cycles caused by merging aren't stopped here, and `skipWaitFor`
only warns and does nothing; members' migrations are still their own Jobs.

### 5. Mixed × one machine

**What it is**: some standalone containers, some in shells, all driven by one `brickkit up`. Who is where is decided
entirely by where entries sit in the deploy file (at the top level, or under a shell's `members`).

**When**: where a mature project usually ends up, and usually **structurally**, not as a resource compromise — components
like frontends and gateways don't belong in shells to begin with. It is also how a component that needs to be scaled,
upgraded or isolated on its own leaves a shell. When you're unsure which cell an on-premises delivery should use, this is
usually the answer.

**Upside**: standalone where independence is needed, memory saved everywhere else; both kinds of entry sit side by side in
one deploy file, with no special handling.

**Cost**: troubleshooting means carrying two mental models at once; the costs of the merged half and of the standalone half
each hold unchanged — mixing doesn't average them out.

**Worth knowing**: an old version of a component running standalone while a new version runs in a shell can coexist with
zero coordination (see [the end of this page](#versions-side-by-side-need-no-extra-care)).

### 6. Mixed × Kubernetes

**What it is**: cell 5's mixed topology against a cluster: standalone components each with a Deployment + Service,
members' Services pointing at the shell Pods.

**When**: resources neither tight nor generous: most components stay in shells to keep the total cost down, and the few
that really need their own replica count or resource quota are picked out and deployed on their own. Unlike cell 5, mixing
here is often a **resource-tuning** decision — a component that could structurally be merged perfectly well is split out
deliberately, to scale on its own.

**Upside**: the most flexible resource allocation; address resolution and network policies support mixing natively, with
no extra configuration.

**Cost**: the full K8s operational cost and the full merging cost at once; the most places to look when something goes
wrong: K8s itself, a shell's internal orchestration, ordinary containers.

## Off the main line: processes on this machine

Three ways run components as processes on this machine instead of in containers. All three are for development, demos
and trials; none is a delivery.

| Way | Who starts the processes | For |
| --- | --- | --- |
| [Containers plus a few processes](#containers-plus-a-few-processes): `mode: debug` / `mode: local` on some entries | You in your IDE (`debug`), or `up` (`local`) | Changing one or two components against the real rest |
| [Every component as a process](#every-component-as-a-process): `mode: local` on every entry | `up` | A demo or a trial on a machine without a container engine |
| [By hand, outside the platform](#by-hand-outside-the-platform) | You, without `brickkit up` | The fastest loop for one component; testing a shell's own orchestration |

What they have in common, against containers:

- **They don't keep running.** A `mode: local` process lives as long as the `up` session that started it: `Ctrl+C` or
  closing the terminal stops it, and one crash ends the session. Nothing restarts it or starts it at boot — BrickKit has
  no long-running process (see [`mode`](../01-three-layers/03-deploy-yaml.md#mode-whether-it-runs-and-how)).
- **The runtime is the machine's.** The process runs on the toolchain installed here, not one pinned by an image, so two
  machines can run a component on two versions of its language. Its source has to be on the machine.
- **What they cost.** In the project measured above, every component as a process used the same CPU as Docker. For a Go
  component the start command is `go run`, which stays resident beside the program it built (about 0.4 GiB for 12
  processes, 116 MiB once merged into 3 shells). Mixed with containers, the processes reach the containers through host
  ports, so the engine's port forwarding carries that traffic once more — a few percent of CPU.

### Containers plus a few processes

**What it is**: everything is a real container except the components you marked `mode: debug` or `mode: local`.

- `mode: debug`: you start the process in your IDE, and the platform only makes sure others can find it. It can only be
  written in the personal `deploy.local.yaml`.
- `mode: local`: the platform recognises the start command from the component's source, starts the process (after its
  migration from source, when the component declares `local.migrationCommand`) and supervises it for as long as that `up`
  runs.

The platform makes the other containers resolve that component's versioned service name to your machine (`extra_hosts`
pointing at `host-gateway`). Callers keep the same service name — `extra_hosts` only changes where it resolves — and the
port becomes your `localPort`, which by default is the component's declared port, so often nothing in the address changes.

**When**: you want "the rest is real" and also "no image rebuild for changing this one component". It's only a
development-time overlay; it doesn't decide the production topology. It works with every topology on one machine:

- **The rest standalone**: the simplest; everything the process depends on gets the real addresses the platform works out.
- **The rest in shells**: a process on this machine depending on a member gets a real `http://localhost:<port>` — the
  mapping is opened on the **shell's** Compose service (the member has no service of its own), on the port the member
  declares, which the shell must listen on anyway. A member itself can be pulled out: in `deploy.local.yaml`, write
  `mode: debug` and `localPort` on that member under the shell entry's `members`; the shell doesn't host it this time —
  the shell must handle one item fewer in `BRICKKIT_SERVED_MEMBERS` correctly.
- **The rest mixed**: the closest thing to "debugging against production" without touching production, because the rest
  is the project's real shape; it also means understanding shell merging, the addresses of processes on this machine
  and ordinary containers at once.

**Upside**: zero changes to component code; several components can be pulled out at once, each with its own `localPort`;
you don't need to know which shell a member is in.

**Cost**: when something can't connect, first ask "does this name resolve to my machine or to a container", rather than
checking the network in general; container-network addresses written by hand in `config/` aren't rewritten.

**Worth knowing**: a `mode: debug` component gets a `local-debug.<service name>.env` for the IDE to load, with multi-line
values and values holding special characters already escaped by shell rules. Its **database migration isn't run for you** —
the migration step went away together with its container; you run it from source, with the command the component
declares in `local.migrationCommand` (see [When no migration container runs](../05-migration/01-migration-service.md#when-no-migration-container-runs)).

### Every component as a process

**What it is**: every entry `mode: local`. `up` starts no container engine at all; it runs each component's migration
from source, starts the processes and watches them in the foreground. Infrastructure the components need (a database,
say) has to be reachable from this machine.

**When**: someone wants to see the project run once, or try it, on a machine where they don't want to install or set up a
container engine: one `up` to start it, `Ctrl+C` to stop it.

**Upside**: no engine, no images, no build step; the same CPU as containers.

**Cost**: every limit above, for the whole project: it stops with the session, the toolchain of every component's
language must be installed, and nothing about it survives a reboot. When it has to keep running, move it to containers —
the deploy file loses its `mode: local` lines and `config/` stays as it is.

### By hand, outside the platform

Nothing stops you from starting every component as an ordinary process (`go run`, `uvicorn`, or your language's
equivalent), without going through `brickkit up` at all.

**The boundary first: this isn't a shape the platform manages; the platform doesn't even know it exists.** Dependency
resolution, environment variable injection, health checks, migration order and service discovery all don't happen,
because you didn't call it. Which port each process listens on, every variable a real deployment would inject, the start
order — you reconstruct all of it yourself. That's a direct consequence of "the CLI exits when done, with no long-running
service": the platform exists only while a command runs.

It's still entirely legitimate and very common — usually the fastest development loop (no images to build, breakpoints
attach directly). It takes three forms, by topology:

- **All standalone**: the most common starting point for developing one component, especially while the project has no
  shells yet. No container layer between a process and the addresses it connects to, so a problem has only two variables
  to check.
- **A shell alone**: the shell's own program started straight on your machine, to test its multi-module orchestration —
  start order, health check, failure isolation between modules — without the container layer; needed almost only by
  whoever builds shells. `BRICKKIT_SERVED_MEMBERS` / `BRICKKIT_SERVED_MEMBERS_CONFIG` are yours to assemble (their exact
  shape is in [Config as JSON](../04-shell/02-json-injection.md)), so it proves only that the shell behaves right on data
  you assembled. The module registry inside a shell should be keyed by **the full component ID plus version**, so that a
  real deployment can host two versions of one component at once — a trap that only shows up when two versions really
  are fed in, worth practising on purpose here.
- **Mixed**: once the production topology uses shells, start the few components you're changing in their real shapes —
  standalone components alone, shells as above. Reconstructing the real shape lets you hit shape-related bugs locally;
  nothing checks that the "who is where" you set up by hand agrees with the deploy file.

**A trick worth knowing: have the CLI work out the environment variables for you.** In `deploy.local.yaml`, temporarily
mark the component you'll run by hand `mode: debug`, run `brickkit up --dry-run` once, and read the generated
`.brickkit/generated/local-debug.<versioned service name>.env` — exactly the full set of variables a real deployment would
give it. For instance, `shop/order` depending on the shell member `shop/stock`:

```text
COMPONENT_ID=shop/order
COMPONENT_VERSION=0.1.0
PORT=8083
SHOP_STOCK_ENDPOINT=http://localhost:18082
```

Copy what you need, then delete that line (or `brickkit local off`). `deploy.local.yaml` is never committed anyway, so it
affects no one. One thing the trick doesn't cover: addresses you wrote by hand in `config/` that name another container's
service — the platform doesn't parse config values and won't rewrite them for you, just as with `mode: debug` itself.
The one exception is `host.docker.internal`: a process on your machine receives `localhost` instead, unless your machine
resolves that name to a loopback address.

## Versions side by side need no extra care

No shape on this page changes how several versions of the same component coexist. Versioned service names make coexistence independent
of topology and of deploy target: old callers and new ones, a version running standalone and a version in a shell, can all
coexist in one deployment. The platform doesn't check whether old and new versions work together correctly — keeping
contracts compatible is the component author's responsibility (see
[Several versions side by side](../03-component-guide/09-multi-version-coexistence.md)).

## Comparing two environments

Each environment has one complete deploy file, so comparing two environments needs no dedicated command; two ordinary
`diff`s are enough:

1. `diff -u deploy.yaml deploy.prod.yaml` shows **what you wrote differently**.
2. A `diff` of two `up --dry-run` outputs shows **what those differences lead to** — which components don't start as a
   result, and why.

Real output below: `demo/caller` requires `demo/hello` and optionally depends on `demo/bus`; the production file switches
to `k8s` and turns `demo/hello` off:

```text
$ diff -u deploy.yaml deploy.prod.yaml
--- deploy.yaml
+++ deploy.prod.yaml
@@ -1,10 +1,8 @@
-# deploy.yaml — how this project is deployed: the target, and which components run and how.
-# The team file: commit it. Your personal copy is deploy.local.yaml (brickkit local on).
-target: docker # docker | podman | k8s
-
+target: k8s
 components:
   - id: demo/hello
+    mode: disable
   - id: demo/bus
   - id: demo/caller
     expose: true
-    exposePort: 18080
+    hostname: shop.example.com
```

```text
$ diff <(brickkit up --dry-run 2>&1 | grep -v '^{') \
       <(brickkit up --dry-run -f deploy.prod.yaml 2>&1 | grep -v '^{')
1c1,2
< 🚀 Starting project my-shop (target: docker)
---
> Using deploy.prod.yaml (--file)
> 🚀 Starting project my-shop (target: k8s)
3,5c4,6
<    ✅ demo/hello@1.0.0   starting (demo/caller needs it)
<    ✅ demo/bus@1.0.0     starting (demo/caller needs it)
<    ✅ demo/caller@1.0.0  starting (top-level)
---
>    ⬜ demo/hello@1.0.0   disabled explicitly (mode: disable)
>    ⬜ demo/bus@1.0.0     not starting (nothing above it is starting)
>    ⬜ demo/caller@1.0.0  not starting (required dependency demo/hello is not starting)
```

(The second `diff` goes on with differences in start order and generated files, cut here.)

The second one gives the consequences directly: not only does `demo/hello` not start, `demo/caller` doesn't either (it
requires `demo/hello`), and `demo/bus`, with nothing above needing it, doesn't start. The `mode: disable` line in the first
`diff` alone doesn't tell you any of that.

- `grep -v '^{'` filters out the JSON log lines the CLI writes on stderr; otherwise they mix into the human-readable output
  and the `diff` is full of noise.
- What's compared here is **the start decision**, not the generated deployment files themselves: one side is
  `compose.yaml`, the other a set of Kubernetes manifests, which can't be compared line by line anyway.
