# Local debugging

You're changing `demo/hello` and want to run it in your IDE with breakpoints, while the project's other components keep
running in containers and can still call your process. This page walks through it from the start.

Background: what your personal deploy file `deploy.local.yaml` is, and why it "replaces the whole file rather than
merging", is in [deploy.local.yaml](../01-three-layers/04-deploy-local-yaml.md).

In a large project where you only need this component and what it depends on, start with a focus run instead:
`brickkit up` in the component's directory runs just those, and `mode: debug` on the focus still gives you breakpoints —
see [Developing inside the project](04-focus-run.md).

## The whole flow

### 1. Turn local mode on

```bash
brickkit local on
```

```text
✅ Local mode is on: commands now read deploy.local.yaml
   deploy.local.yaml was copied from deploy.yaml — change it as you like, it is not committed
```

From then on the commands that run or check the deployment (`up`, `down`, `status`, `sync`, `lint`, `build`) read
`deploy.local.yaml` (`graph` and `deps` keep reading `deploy.yaml`), and `up` starts each run with a reminder:

```text
Local mode is on: using deploy.local.yaml (brickkit local off switches back to deploy.yaml)
```

### 2. Mark the component you're debugging `mode: debug`

```yaml
# deploy.local.yaml
components:
  - id: demo/hello
    mode: debug
    localPort: 8080
  - id: demo/caller
    expose: true
    exposePort: 18080
```

`mode: debug` means "I start this component on my machine myself": the platform generates no container for it, but
still counts it as running, and the other components still get its address. `localPort` is the port your process
**actually listens on**. `demo/hello` listens on 8080 in its code, so 8080 goes here; if they differ, callers only get
connection refused. The process must also listen on **all interfaces** (`0.0.0.0`): containers come in through the host's
bridge address, and a process listening only on `127.0.0.1` never receives them (see
[Local debugging problems](../10-troubleshooting/02-local-debug-issues.md#containers-cant-reach-your-process)).

`mode: debug` can only be written in `deploy.local.yaml` — "I'm debugging this on my machine" is a personal fact, and
has no place in a file the team reviews.

### 3. Start the other components

```bash
brickkit up
```

```text
📋 Component state calculation:
   ✅ demo/hello@1.0.0   starting (mode: debug)
   ✅ demo/caller@1.0.0  starting (top-level)
```

```text
🔧 Local debugging (mode: debug):
   demo/hello@1.0.0
      No container is generated; start it in your IDE, listening on port 8080 on all interfaces (0.0.0.0) — containers cannot reach a process that listens on 127.0.0.1 only
      Environment variables: .brickkit/generated/local-debug.demo-hello-1-0-0.env
      VS Code: set "envFile": "${workspaceFolder}/.brickkit/generated/local-debug.demo-hello-1-0-0.env" in launch.json
```

```text
🐳 Starting (docker)...
   demo-caller-1-0-0            running (healthy)
✅ All components started (1)
```

`up` generated an environment file for `demo/hello`: every variable it would have got in its container, none missing.

```text
COMPONENT_ID=demo/hello
COMPONENT_VERSION=1.0.0
GREETING='Hi from the host process'
```

(`GREETING` comes from `config/demo-hello.yaml` — to try another value while debugging, change the config file and `up`
again as usual.)

### 4. Start it in your IDE

In VS Code, put that `envFile` line into `launch.json`; from a shell:

```bash
cd components/demo/hello
set -a && source ../../../.brickkit/generated/local-debug.demo-hello-1-0-0.env && set +a
go run .
```

Where the source comes from: `brickkit add demo/hello@1.0.0 --repo` clones it into `components/` (see
[Adding components](02-add-and-component-install.md#cloning-source---repo---repo-all)).

### 5. Check the container reaches you

```bash
curl http://localhost:18080/api/v1/call
```

```json
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:8080","upstream":{"component":"demo/hello","greeting":"Hi from the host process","message":"Hi from the host process, I'm demo/hello@1.0.0","version":"1.0.0"},"version":"1.0.0"}
```

`demo/caller` in its container still calls `http://demo-hello-1-0-0:8080` — the address didn't change, and not a line of
component code did. Inside its container the platform resolves that service name to the host (Docker's
`extra_hosts: demo-hello-1-0-0:host-gateway`) and swaps the port for your `localPort`.

`status` lists it separately:

```text
🔧 Local debugging (mode: debug, not started by the platform)
 ┌────────────┬─────────┬─────────────────────────────────┐
 │ Component  │ Version │ Local address                   │
 ├────────────┼─────────┼─────────────────────────────────┤
 │ demo/hello │ 1.0.0   │ localhost:8080 (IDE debug mode) │
 └────────────┴─────────┴─────────────────────────────────┘
```

A few limits to know:

- **Docker / Podman only.** A Pod in a Kubernetes cluster can't reach your laptop; with the `k8s` target, `mode: debug`
  is rejected.
- **The debugged component runs no database migration.** The migration step went away with the container; if the
  component has migrations, run them by hand the first time.
- **Strings hard-coded in config aren't rewritten, with one name excepted.** `host.docker.internal` is what a container
  calls the host machine (it is what `config/` usually says when the database or the message queue runs on your machine).
  In the values a process on your machine receives, it becomes `localhost` — the same machine, under the name that
  resolves there, which on Linux the first one doesn't; containers still get the value as written. So one `config/` works
  on both sides, and you don't change it in the `vars:` of `deploy.local.yaml` (`vars:` reaches the containers too, and
  they would lose the connection). Anything else you wrote by hand reaches the process as is: an address that names
  another container's service needs a value that works on your machine.

## Ports on your machine

A process on your machine (`mode: debug`, `mode: local`, or the focus component of a focus run) and the containers it
talks to meet on host ports. `up` assigns them in one pass, the ones you wrote first:

| What | Host port |
| --- | --- |
| A container with `expose: true` | Its `exposePort` |
| The process itself | Its `localPort`. Without one: the component's own `deployment.port`, or the first free port from 8081 when that's taken. A `mode: local` process gets the number as `PORT` |
| The process's extra ports | As declared in `deployment.extraPorts` |
| A container the process depends on | 10000 + its container port (postgres's 5432 → 15432, a component's 8080 → 18080), or the first free port from 18080 when that's taken; for a dependency hosted in a shell, published on the shell's container. The env file holds `localhost:<that port>` |

Containers that call the process don't use a host port: they keep its service name, which `extra_hosts` resolves to the
host, at its `localPort`.

`up` only knows the ports it assigns itself. A port another program on your machine already holds fails when the engine
binds it, not before. If the project keeps a port registry (a different `deployment.port` for each component), register
ports as usual — 8080, 8081 and their neighbours are all fine — and mind two things:

- **The one time a process gives up its own port.** Only when its `deployment.port` is already assigned in this `up` (two
  components running on the machine declare the same port, or it collides with an `exposePort`) does the process move to
  the first free port from 8081 — which may be the port another component registered. It doesn't happen when the
  components' ports are all different; to fix the port, write a `localPort` for it in `deploy.local.yaml`.
- **10000 + container port belongs to the mappings.** With container ports in 8080–8499, 18080–18499 are used for the host
  mappings of the containers a process depends on; don't register them for components.

## After the team changes files: the strict consistency check

You have local mode on, and a teammate adds a component and pushes. You pull:

```bash
git pull
brickkit up
```

```text
❌ Error: deploy.local.yaml is out of date: it does not match the components in brickkit.yaml
   File: deploy.local.yaml
   No entry for: demo/bus
   Reason: Local mode is on and brickkit.yaml changed, but your deploy.local.yaml was not updated
   Suggestions:
   1. Option A (recommended): brickkit local refresh regenerates deploy.local.yaml from deploy.yaml and keeps the old one as deploy.local.yaml.bak
   2. Option B: add or remove the listed entries in deploy.local.yaml by hand
   3. Option C: brickkit local off switches back to the team's deploy.yaml
```

Why an error rather than quietly adding the new component: the local file replaces `deploy.yaml` as a whole, and the CLI
doesn't know how the new component should run on your machine — the team's way, or do you have other plans? A wrong
guess means running something you didn't mean to, without knowing. So it stops and lists the three ways forward.

**The team added or removed nothing, only changed values in `deploy.yaml`** (another port, `expose` switched on, a
different `vars:`): that isn't out of date, and `up` runs your personal file as usual. Those changes are not in effect on
your machine — the local file replaces the team file as a whole, which is what it is for, and is easy to forget. So `up`
and `brickkit local status` say so:

```text
ℹ️ deploy.yaml has changed since deploy.local.yaml was copied from it, and those changes are not in effect here: brickkit local refresh brings them in (and lists your local changes, to put back)
```

What is compared is the **data** of the `deploy.yaml` saved at the last copy (`local on` / `refresh`) and of the one there
now: a changed comment doesn't count, nor entries reordered by `add`; and `add` / `remove`, which change the team file and
your personal file together, don't count either. To keep working on the older copy, ignore it.

## `brickkit local refresh`

```bash
brickkit local refresh
```

```text
✅ Generated a fresh deploy.local.yaml from deploy.yaml. The old one is backed up at deploy.local.yaml.bak.
ℹ️ The old file had 2 local changes; merge the ones you still need into the new deploy.local.yaml by hand:
   - [demo/hello] mode: debug (now unset)
   - [demo/hello] localPort: 8080 (now unset)
```

`refresh` does three things:

1. keeps the old file as is in `deploy.local.yaml.bak` (overwriting the previous backup);
2. copies a complete new file from `deploy.yaml` — **no merging**: the new file is a copy of the team file;
3. compares the old file with the copy it was made from, and lists every local change you had that differs from the team
   file now: `mode`, `localPort`, `vars` values, a changed `target`, entries you moved, fields you deleted.

You just go down that list and copy back the lines you still want, instead of comparing two YAML files end to end. Once
copied back:

```yaml
components:
  - id: demo/hello
    mode: debug
    localPort: 8080
  - id: demo/caller
    expose: true
    exposePort: 18080
  - id: demo/bus
```

Why no automatic merge: however clever the merge rules, sometimes they guess wrong, and a wrong guess means "what the
file says isn't what runs". A whole-file replacement plus a list keeps the result visible at a glance.

## Debugging a shell member

A member hosted by a shell can be debugged on its own too: in `deploy.local.yaml`, write `mode: debug` and `localPort` on
that member under the shell's `members`. This time you start it on your machine, the shell **no longer hosts it** (it
isn't in the member list the shell receives), the other members stay in the shell, and other components get an address
pointing at your process. How shells work: [Shells](../04-shell/README.md).

## Turning local mode off

```bash
brickkit local off
```

```text
✅ Local mode is off: commands now read deploy.yaml
   deploy.local.yaml is kept; brickkit local on uses it again
```

The file stays, and the next `local on` picks it up as it is. To have a single run ignore it, you don't need to turn it
off: `brickkit up --no-local`.
