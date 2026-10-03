# up / down problems

`up` works in a fixed order: load and check the three layers → decide who starts this time → resolve dependencies →
generate the deployment files → check images → run migrations → call the engine. Which step an error appears in roughly
tells you which layer the problem is in. The whole flow is in [Starting and stopping](../02-project-guide/06-up-and-down.md).

## Images don't exist

**Symptom**

```text
❌ Error: these images are built locally and have not been built yet
   demo/hello@1.0.0: demo-hello:1.0.0
   demo/caller@1.0.0: demo-caller:1.0.0
```

Or `Error: image pull not authorized`, `Error: image not found`.

**Cause and fix**

`up` never builds images. For images built on this machine, run `brickkit build` first; for pulled ones, make sure you're
logged in to the image registry and the address and tag are right. More in [Build problems](05-build-issues.md).

## A required config item is missing

**Symptom**

```text
❌ Error: a required component config item has no value
   Missing config: demo/widget@0.1.0 → API_URL
   Reason: The component declares it in configSchema.required without a default — the platform can't derive this one, so the project has to supply it
   Suggestions:
   1. Give it a value in config/demo-widget.yaml:
    API_URL: <value>
   2. The value may be ${ENV_VAR} (real value in .env), $var:NAME (shared, from config/vars.yaml) or file://path
```

**Cause**

The component declares a required config item with no default — usually an address the platform can't work out (a
service deployed by another project) or a credential. With it missing, the component would still start and look healthy,
yet have a call path that never works; so the platform stops it before starting, instead of letting it slip through quietly.

**Fix**

Fill it in in `config/<component>.yaml`, as the suggestion says. For a component just `upgrade`d, required items the new
version added appear in the config file as `KEY: ""`, and need filling in too.

## Port conflicts

**Symptom one: two components want the same host port.**

```text
❌ Error: deploy.yaml failed validation
   File: deploy.yaml
   components[2].exposePort: conflicts with components[0].exposePort (host port 18080 is already taken)
   Suggestion: Full field reference: brickkit docs 11-reference/03-deploy-yaml-schema (online: https://github.com/brickKit/brickKit/blob/v1.1.0/docs/en/11-reference/03-deploy-yaml-schema.md)
```

**Fix**: give one of them another `exposePort` (or `localPort`).

**Symptom two: another program on this machine holds the port.** Validation passes, then `docker compose` fails to start
the container, with `port is already allocated` or `address already in use` in the engine's output.

**Fix**: find the program holding it (`ss -ltnp | grep <port>`) and stop it, or pick another `exposePort`.

**Symptom three: two components on a shell use the same port** — see
[Shell problems](06-shell-issues.md#two-components-on-a-shell-want-the-same-port).

## A migration fails

**Symptom**

```text
❌ Error: database migration failed
   Component: shop/stock@0.2.0
   View logs: docker compose -p brickkit-shop logs shop-stock-0-2-0-migration
```

**Cause and fix**

The migration exited non-zero, so the main service won't start. Read the migration container's logs as the suggestion
says, fix it, and `up` again; the migration runs once more. More in [Migration problems](07-migration-issues.md).

## The component's logs look fine, yet the platform says it's unhealthy

**Symptom**

`up` waits to the end and reports `Error: some components did not start properly`, or the components depending on it never
start. Meanwhile the component's own logs say it's ready and listening on its port.

**Cause**

On Docker, the health check runs **inside** the container, through the image's `/bin/sh`. For `type: http` it's:

```text
wget -q --spider http://127.0.0.1:8080/healthz || curl -fsS http://127.0.0.1:8080/healthz || exit 1
```

and for `type: tcp` it's `nc -z 127.0.0.1 8080`. So `type: http` needs `/bin/sh` plus `wget` or `curl` in the image, and
`type: tcp` needs `/bin/sh` plus `nc`. When they're missing, the command always fails and the container is always judged
unhealthy — however healthy the component itself is. `scratch` and distroless images have no shell at all, so **neither
type** can pass there; images based on `busybox` or `alpine` have all of it. Another common cause: a wrong health check
path (`healthCheck.path` in `component.yaml` differs from what the component actually serves).

The check goes to `127.0.0.1`, not `localhost`: inside a container `localhost` points at both `127.0.0.1` and `::1`,
Alpine's `wget` and `nc` pick `::1`, and a server listening only on `0.0.0.0` (the usual way in Python and Node) has
nothing answering on IPv6. So the component has to listen on `0.0.0.0` or `::` — one listening only on `localhost`
couldn't be reached from other containers anyway.

On Kubernetes this problem doesn't exist: the probes (`httpGet`, `tcpSocket`) are sent by the kubelet from outside the
container and need nothing in the image.

**Fix**

- Look at the container's health-check record: `docker inspect --format '{{json .State.Health}}' <container name>`; the
  output of the last few entries says `wget: not found`, `nc: not found` or that `/bin/sh` doesn't exist.
- Check what the image has: `docker run --rm --entrypoint sh <image> -c 'command -v wget curl nc'` (when even this fails,
  the image has no shell).
- Give the image the tools: build the final stage on `busybox` or `alpine` (a few MB; they carry `sh`, `wget` and `nc`).
  Switching from `http` to `tcp` only helps when the image has `nc` but neither `wget` nor `curl`.
- If the image must stay without a shell, `healthCheck.type: none` is the remaining choice: components depending on it
  then wait only for its container to start, not for it to be ready, and on Kubernetes it gets no probes either — so they
  have to retry on their own while it comes up.
- When the path doesn't match, change `healthCheck.path`.
- When the output is `Connection refused` and the component is running: look at the address it listens on
  (`docker exec <container name> netstat -ltn`). It has to be `0.0.0.0:<port>` or `:::<port>`, not `::1` or one specific
  address other than `127.0.0.1`.

## A component with a cold start over a minute is judged failed

**Symptom**

A slow-starting component (a heavy Spring Boot app, a Django project that preloads a lot, .NET's first JIT pass): on
Docker, `up` reports it unhealthy; on Kubernetes the Pod keeps restarting, stuck in `CrashLoopBackOff`. And the container's
own logs look perfectly normal throughout — it just hasn't finished starting yet.

**Cause**

The health check's rhythm is fixed by the platform: every 10 seconds, a 3-second timeout, 3 failures in a row means
unhealthy. The platform gives every component a 60-second start-up grace period, during which failures don't count. With a
cold start over 60 seconds, it isn't ready when the grace period ends and is declared dead; on Kubernetes it's killed and
restarted, starts over from scratch, and never comes up.

**Fix**

Raise the grace period in the component's `component.yaml`, with room to spare:

```yaml
healthCheck:
  type: http
  path: /healthz
  startPeriodSeconds: 180
```

The grace period only delays "declaring it dead", never "declaring it alive": a component ready in two seconds still turns
healthy in two seconds, so setting it generously costs nothing.

## `up` hangs, and the migration container keeps running

**Symptom**

`up` never returns. In `docker compose -p brickkit-<project> ps` the main service sits at `Created` while the migration
container is `running`; the migration container's logs say the service is ready and listening on its port.

**Cause**

The migration container and the main service run the same image, differing only in the command argument. When the
component's entry program meets an argument it doesn't know (`migration.command` misspelled), it doesn't exit with an
error — it **starts the service as usual**. The migration container becomes a second service that keeps running and never
finishes; the main service waits for it to "finish successfully" before starting, and waits forever.

**Fix**

- The entry program must fail and exit at once (with a non-zero exit code) on an argument it doesn't know; see
  [The migration container](../05-migration/01-migration-service.md). Check this **before** reading environment variables
  or connecting to the database, or a misspelled argument shows up as a misleading "can't connect to the database".
- Check the spelling of `migration.command`.
- `Ctrl+C` first, then `brickkit down` to clear the stuck containers.

## `docker compose logs` shows nothing

**Symptom**

The component is clearly running, yet `docker compose logs` prints nothing, or says it can't find the service.

**Cause**

`docker compose` finds containers by **project name**. The Compose project name BrickKit generates is
`brickkit-<project name>`, while running it directly in the project directory, Compose uses the directory's name and looks
at another (empty) project.

**Fix**

Add `-p`:

```bash
docker compose -p brickkit-my-shop logs -f demo-caller-1-0-0
```

The "View the logs" line `up` prints at the end is the right command.

## Deployed to the wrong cluster (or nearly)

**Symptom**

```text
❌ Error: the cluster currently connected is not the one the configuration specifies
   Required by the deploy file (k8s.context): prod-cluster
   Current context: dev-cluster
   Suggestions:
   1. Switch to it: kubectl config use-context prod-cluster
   2. If dev-cluster is where you mean to deploy: use a deploy file whose k8s.context names it (brickkit up -f <file>)
   3. Don't continue until you are sure — deploying to the wrong cluster can't be undone
```

**Cause**

The deploy file's `k8s.context` names one cluster while `kubectl`'s current context is another. Deploying to the wrong
cluster can't be taken back, so `up` checks first and stops when they don't match.

**Fix**

`kubectl config use-context <the one in the deploy file>`, or use the deploy file naming the current cluster (`-f`).
There's no command-line flag to switch clusters for one run: one deploy file per cluster.

## `down` fails on Podman: `permission denied`

**Symptom**

The deploy target is `podman`. `up`, `status` and ordinary requests all work, yet `down` (or `up` when it cleans up
leftover containers) fails, with this in the engine's output:

```text
Error: removing container ...: 1 error occurred:
	* rootless netns: kill network process: permission denied
```

BrickKit recognises this signature and adds a line to the error's suggestions:

```text
This is a known Ubuntu/Debian AppArmor gap blocking rootless Podman's network teardown (containers/podman#27372), not a BrickKit or Podman bug — see docs/en/10-troubleshooting/01-up-down-issues.md for the fix
```

On an affected machine it reproduces **every** time; it isn't intermittent. The container itself is usually already gone
(Podman falls back to `SIGKILL`), but network resources may not be released, and the command exits with a failure — easy to
mistake for "it didn't stop at all this time".

**Cause**

Podman running without root connects containers to the network through a helper process called `pasta`. When stopping a
container, Podman sends `pasta` a `SIGTERM` so it tears the network down cleanly. Ubuntu's / Debian's default AppArmor
policy blocks that signal. It's a gap in how the distribution packages its security policy; Podman upstream has exactly the
same report ([containers/podman#27372](https://github.com/containers/podman/issues/27372)), and Debian has tracked the
same error ([Debian #1100135](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1100135)).

**First, check whether you're affected**

```bash
scripts/podman/check-environment.sh
```

It's essentially read-only; to get a real answer it starts a throwaway test container — you can't tell from configuration
files alone, since whether it's blocked depends on the policy currently loaded in the kernel. Exit code `0` is fine, `1`
means the problem reproduced, `2` means the check itself couldn't run (Podman not installed, say).

**The fix**

```bash
scripts/podman/fix-apparmor.sh
```

**Read the script before running it — it's destructive.** It resets Podman to a clean state: deleting your existing Podman
containers, images and volumes, reinstalling `podman` and `apparmor-utils` with apt, and regenerating the base
configuration. It needs `sudo`, is verified only on Ubuntu / Debian, and doesn't touch Docker. It takes two routes, verified
to fix the problem when used together:

1. **Delete the placeholder `podman` AppArmor profile** (Debian's conclusion): it restricts nothing itself, but gives the
   `podman` process a name, which triggers AppArmor's cross-profile signal mediation. The script first writes a minimal
   file of the same name, unloads it from the kernel, then deletes the file — in the reverse order, the unload command
   can't read the file, and the old profile quietly stays in the kernel.
2. **Put `pasta` into complain mode** (`aa-complain /usr/bin/pasta`): AppArmor only logs this one process, without blocking.
   The rest of the system is unaffected; the cost is that this one process is from then on only monitored, not confined.

The script also handles three things unrelated to this signal that likewise make `up` fail or hang: configuring default
image registries in `registries.conf` (otherwise pulling an image without a registry prefix, like `alpine:3`, fails, or pops
up an interactive choice that hangs a non-interactive `up`); setting `policy.json` (otherwise Podman refuses to pull any
image); enabling `podman.socket` (`podman compose` talks to Podman through it, and without it reports `failed to connect to
the docker API`). When only the socket is missing, run just:

```bash
systemctl --user enable --now podman.socket
```

**Two further prerequisites**

- **A terminal opened from a snap-packaged application** (VS Code installed as a snap, say) redirects `$XDG_DATA_HOME` to a
  versioned path, and after a Podman upgrade Podman reports `database configuration mismatch`. `check-environment.sh`
  flags it; the fix is setting `export XDG_DATA_HOME="$HOME/.local/share"` and `export XDG_CONFIG_HOME="$HOME/.config"` in
  your shell's start-up file.
- **`podman compose` should use Docker's own Compose V2 plugin.** `podman compose` has no compose implementation of its
  own; it only forwards to an external program it finds. What's verified is Docker and Podman installed on the same
  machine, with it forwarding to Docker's Compose plugin. Run `podman compose version`: `Executing external compose
  provider ".../docker-compose"` means that route; when it shows the separate `podman-compose` project, working with BrickKit
  hasn't been verified.

## `down` can't stop a `mode: local` component

**Symptom**

After `brickkit down`, a `mode: local` component is still running; `down` only printed a session's process ID.

**Cause**

A `mode: local` process is supervised in the foreground by the terminal that ran `up`; it isn't a container. `down` only
looks after containers, and can't reach into another terminal's process tree.

**Fix**

`Ctrl+C` in that terminal. A normal stop prints no crash summary.

## Config changed, no difference after `up`

**Causes and fixes**

- The key name is wrong: `up`'s output has a warning saying it "is not declared in the component's configSchema, so it has
  no effect"; see [Config conflict problems](03-config-conflict-issues.md#config-written-but-not-taking-effect).
- The file changed isn't the one read this time: with local mode on, `deploy.local.yaml` is read, while you changed `vars:`
  in `deploy.yaml`. `brickkit local status` shows which one is read this time.
- What changed is code, not config: after changing code, `brickkit build --force`; see [Build problems](05-build-issues.md).
- What changed is a secret delivered as a file (the component declares `mount: file` in its `configSchema`): `up` only
  gives the file its new content and **does not restart the component**. A component that reads the file once at start
  needs a restart to use the new value (`brickkit down`, then `brickkit up`); on Kubernetes the file changes once the
  kubelet syncs, usually within a minute. See
  [Delivered as a file](../01-three-layers/07-sensitive-values.md#delivered-as-a-file-mount-file).
