# Local debugging problems

Local debugging pulls one component onto your machine with `mode: debug` and runs it in your IDE, while the other
components run in containers as usual. The whole flow is in [Local debugging](../02-project-guide/03-local-debug-workflow.md).

## `deploy.local.yaml` is out of date

**Symptom**

After `git pull`, `up` (and other commands) refuse to run:

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

**Cause**

A teammate added (or removed) a component. `deploy.local.yaml` replaces `deploy.yaml` as a whole, and every component
version must have exactly one entry; the CLI doesn't know how the new component should run on your machine, so it stops
rather than guessing for you.

**Fix**

Most of the time, option A:

```bash
brickkit local refresh
```

```text
✅ Generated a fresh deploy.local.yaml from deploy.yaml. The old one is backed up at deploy.local.yaml.bak.
ℹ️ The old file had 2 local changes; merge the ones you still need into the new deploy.local.yaml by hand:
   - [demo/hello] mode: debug (now unset)
   - [demo/hello] localPort: 8080 (now unset)
```

Copy the lines you still want back into the new file, going down the list. When only one or two entries are missing,
adding them by hand (option B) is quicker.

## `mode: debug` doesn't take effect

**Symptom one: written in `deploy.yaml`, and refused.**

```text
❌ Error: deploy.yaml failed validation
   File: deploy.yaml
   components[0].mode: mode: debug can only be written in deploy.local.yaml: it records that you are debugging this component on your machine right now, which is not a team decision. Run brickkit local on and set it there
   Suggestion: Full field reference: docs/en/11-reference/03-deploy-yaml-schema.md (swap en for zh for the Chinese version)
```

**Cause**: `mode: debug` is your personal fact of the moment, and can only be written in the personal file. **Fix**:
`brickkit local on`, and write it in `deploy.local.yaml`.

**Symptom two: written in `deploy.local.yaml`, yet the component still runs in a container.**

**Cause**: local mode is off, so commands read `deploy.yaml` and never look at `deploy.local.yaml`. Or this run used `-f` /
`--no-local`, which skip local mode.

**Fix**: confirm which file is read:

```bash
brickkit local status
```

```text
Local mode: off
deploy.local.yaml: present (not read while local mode is off)
```

`brickkit local on` turns it on. With local mode on, the first line of `up`'s output says `Local mode is on: using
deploy.local.yaml (brickkit local off switches back to deploy.yaml)`; no such line means `up` didn't read it. The other
commands (`down`, `status`, `sync`, `lint`) print no such line — for them, `brickkit local status` says which file is read.

**Symptom three: the deploy target is `k8s`, and it's refused.** A Pod in the cluster can't reach your laptop, so
`mode: debug` and `mode: local` only work under `docker` / `podman`. In the personal file you can change `target` to
`docker` and debug on your machine.

## Containers can't reach your process

**Symptom**

The caller (in a container) requests your process on this machine and gets connection refused, a timeout, or a 502/503.

**Causes and fixes**, by how often they come up:

1. **The port doesn't match.** `localPort` must be the port your process **actually listens on**. A process hard-coded to
   listen on 8080 needs `localPort` to be 8080. The platform replaces the port in the address callers get with
   `localPort`; when the two differ, requests land on a port nobody listens on.
2. **The process listens only on `127.0.0.1`.** Containers come in through the host's bridge address (`host-gateway`),
   not through the loopback address, so a process listening only on `127.0.0.1` never receives them. The caller sees:

   ```text
   wget: can't connect to remote host (172.17.0.1): Connection refused
   ```

   while `curl localhost:<port>` on your machine works — which is exactly what makes it hard to track down. Make the
   process listen on all interfaces (`0.0.0.0`): in Go write `:8080`, not `127.0.0.1:8080`; `uvicorn` and Flask's
   development server listen only on `127.0.0.1` by default and need `--host 0.0.0.0`; Node's `listen(port)` without a host
   name listens on all interfaces by default.
3. **The process didn't start, or started in another terminal, on another machine.** On your machine, first
   `curl http://localhost:<localPort>/healthz`; once that works, look at the container side.

What the platform does: in the other containers it resolves your component's versioned service name to the host
(`extra_hosts: <service name>:host-gateway`), and replaces the port with `localPort`. The address string callers get
doesn't change at all — so when troubleshooting, first ask "does this name resolve to my machine, and is someone listening
on that port", rather than checking the network in general.

## The process on this machine reports `relation does not exist`

**Symptom**

The component being debugged reports a missing table as soon as it starts (PostgreSQL's `relation "…" does not exist`, or
the same kind of error from another database). `up`'s output has a note:

```text
⚠️ Note: a mode: debug component's database migration won't run automatically
```

**Cause**

Migrations run in the component's migration container; a component running as a process on this machine has no container,
so the migration is skipped along with it.

**Fix**

Run the migration once yourself: load the variables from `local-debug.<service name>.env` and run the component's migration
command (`migration.command` in `component.yaml`) on this machine.

```bash
set -a && source .brickkit/generated/local-debug.demo-caller-1-0-0.env && set +a
go run . migrate
```

## The process on this machine can't reach an address written in config

**Symptom**

The process on this machine reaches its dependencies fine, but fails to reach one "outside" service, whose address you
wrote by hand in `config/` — `http://host.docker.internal:8000`, say.

**Cause**

The platform rewrites only the dependency addresses it works out itself (`*_ENDPOINT`). Strings you wrote into config by
hand aren't parsed or rewritten, and reach the process as they are — and that address assumes the container network,
which doesn't resolve on the host.

**Fix**

Give it a value that works on this machine in the `vars:` of `deploy.local.yaml` (with `$var:NAME` referencing it in
config): while debugging it's `localhost`, and the team's deploy files are unaffected:

```yaml
# config/demo-caller.yaml
REPORT_URL: $var:REPORT_URL
```

```yaml
# deploy.local.yaml
vars:
  REPORT_URL: http://localhost:8000
```

## A focus run won't start

**Symptom**

`up` in a component's directory, or `up --focus`, stops with one of these:

```text
❌ Error: the focus demo/lb is not a component of this project
   File: deploy.local.yaml
   Suggestions:
   1. brickkit up --focus <id> sets another one; brickkit up --all runs every component
   2. Did you mean: demo/lib?
```

```text
❌ Error: the focus demo/lib is written mode: disable
   File: deploy.local.yaml
   Suggestion: Remove mode: disable from demo/lib, or focus on another component
```

**Cause and fix**

- **Not a component of this project**: `focus:` in `deploy.local.yaml` names a component that isn't in `brickkit.yaml`
  — a typo, or the team removed it. Pick another with `--focus`, or drop the focus with `brickkit up --all`.
- **`mode: disable`**: "run this" and "never run this" at once. Remove the `mode: disable` from the entry, or focus on
  another component.
- **`failed validation` on the `focus` field**: the focus is in `deploy.yaml` (it belongs in your personal file), or
  your personal file says `target: k8s` — a cluster can't reach a process on your machine.
- **`Code that runs from a local repository does not match this run`**: the focus runs from its source, and that source
  either isn't there (`brickkit add <id> --repo`) or holds another version than `brickkit.yaml` (run the `upgrade` the
  error suggests). See [Developing inside the project](../02-project-guide/04-focus-run.md#moving-versions-forward).

## Under a focus, a component didn't start

**Symptom**

A component you expected is listed as `not starting (outside the focus)`.

**Cause**

With a focus, only the focus and the components whose `mode` says they always run (`enabled`, `local`, `debug`) are
starting points; everything else starts only if one of them needs it. A component the focus doesn't depend on — one
that calls the focus, for instance — stays off.

**Fix**

Write `mode: enabled` on that component in `deploy.local.yaml` to pin it for your runs, or run everything with
`brickkit up --all`.

## Debugging a member inside a shell on its own

In `deploy.local.yaml`, write `mode: debug` and `localPort` on that member under the shell entry's `members`. This time
the shell no longer hosts it, it runs on your machine, and the address other components get for it points at your
process. See [Local debugging](../02-project-guide/03-local-debug-workflow.md#debugging-a-shell-member).
