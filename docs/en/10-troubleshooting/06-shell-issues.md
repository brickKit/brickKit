# Shell problems

A shell is a component that compiles several components (members) into one process. Background in
[Shells](../04-shell/README.md).

## The shell fails to parse `BRICKKIT_SERVED_MEMBERS_CONFIG` at start-up

**Symptom**

The shell's container keeps restarting, with a JSON parse error in its logs, like `BRICKKIT_SERVED_MEMBERS_CONFIG: invalid
character …`.

**Cause**

The JSON the platform generates is always valid: members' config values are worked out at generation time and encoded by
JSON's rules, and on Docker `$` is additionally written as `$$`, so Compose leaves it alone (see
[Special characters](../04-shell/06-special-characters.md)). So a parse failure almost always means the JSON **isn't the
one the platform generated**:

- The shell runs as a process started by hand, with JSON you assembled yourself, and its quotes, newlines or `$` weren't
  escaped properly.
- Someone edited the files under `.brickkit/generated/` by hand, or copied the JSON value onto a shell command line, where
  the shell expanded it once more.
- The shell's code doesn't read this variable, or reads another variable of the same name.

**Fix**

Use the one the platform generates: `brickkit up --dry-run`, and the shell's JSON is in
`.brickkit/generated/env/<shell service name>.env` (in the generated Secret on K8s). When starting the shell by hand, load
it with `set -a && source <that env file> && set +a`; don't paste the JSON onto a command line.

## A member's config value can't be carried in JSON

**Symptom**

```text
❌ Error: config item STOCK_WAREHOUSE of shell member shop/stock@0.1.0 is not valid UTF-8 text
   Reason: JSON can only carry text; binary bytes would be replaced silently
   Suggestion: Store binary content base64-encoded and decode it in the component
```

**Cause**

Members' config goes into one JSON string, and JSON carries only text. When a `file://` points at a binary file (a
certificate in DER format, an archive), encoding would silently replace the invalid bytes, and the member would get an
altered value. The platform stops it at generation time, rather than letting it run with a broken value.

**Fix**

base64-encode the binary content into a text file first, and decode it in the member's code. Certificates in PEM (text)
format don't need this step.

## A member can't read its own config

**Symptom**

A module inside the shell behaves as if it had no config: it uses defaults, or reports "missing such-and-such config".
The same component running on its own works fine.

**Cause**

Running on its own, a member's config is environment variables in its container; inside a shell, every member shares the
shell's one process, and a member's config **isn't in the process environment** — it's under `config` in the member's own
item of `BRICKKIT_SERVED_MEMBERS_CONFIG`. Module code that reads `os.Getenv("STOCK_WAREHOUSE")` directly finds nothing
inside a shell.

**Fix**

When the shell initialises each module, it hands it that item's `config`, and the module reads its config from that map,
not from the process environment (see [Writing a shell](../04-shell/05-shell-development.md)). For a component that must
both run on its own and be compiled into a shell, the least work is putting config reading in one function: pass it the
process environment when running on its own, and the item's `config` inside the shell.

When a member's **required** config has no value, `up` stops it before generating anything (`Error: a required component
config item has no value`), just as when it runs on its own — that case doesn't belong to this section.

## Two components on a shell want the same port

**Symptom**

`up` (or `up --dry-run`) stops before generating anything:

```text
❌ Error: two components on shell shop/shell@0.1.0 both want port 8081
   Held by: Component shop/cart@0.1.0
   Held by: Component shop/stock@0.1.0
   Suggestion: These components all end up running in the same shell container/Pod, so ports must not collide
```

One of the `Held by` lines can also read `the shell itself`.

**Cause**

The shell and every member it hosts this run listen in one container (one Pod on K8s), so their main ports
(`deployment.port` in each `component.yaml`) must all differ. Two components that each ran fine on their own, both on
`8080` say, collide the moment they go into one shell. `up` checks only the main ports; when two `extraPorts` collide,
the shell process fails to bind one of them at start and exits, which you see in the shell's logs.

**Fix**

- Give one of them another port: change `deployment.port` in that component's `component.yaml`, as a new version of it
  (raise `metadata.version`, then `brickkit upgrade`), since the port is part of its contract.
- Or take one of them out of the shell, so it runs in its own container again: see
  [Moving out of a shell](../04-shell/04-members-management.md#moving-out-of-a-shell).

## Calling a member in a shell: connection refused or 404

**Symptom**

Another component calls a member inside the shell. The network is fine (DNS resolves, the shell's container is running),
but the request is refused (connection refused), or returns 404.

**Cause**

The platform only makes sure a request **reaches the right port of the shell**: the address callers get is
`http://<shell service name>:<the member's port>`, and the member's own service name resolves to the shell's container
too. Which module a request goes to once it reaches the shell is the shell's code's business. The common mistakes:

- The shell doesn't listen on this member's `httpPort` (connection refused). The shell opens only its own port, say,
  meaning to route requests to modules by path, but never listens on the members' ports.
- The shell listens on the member's port but hands requests to the wrong module, or the module's route prefix differs from
  what callers use (404).
- The shell quietly skipped a member it doesn't compile in, and the address callers got has nothing behind it.

**Fix**

Check the shell's start-up code: for every item of `BRICKKIT_SERVED_MEMBERS_CONFIG`, listen on that item's `httpPort` (and
`extraPorts`) and hand it to the matching module; on a member ID it doesn't compile in, fail and exit at once rather than
skipping it. A reference implementation is in [Writing a shell](../04-shell/05-shell-development.md). Sending one request
from inside the shell's container (`docker compose -p brickkit-<project> exec <shell service name> wget -qO-
http://localhost:<member port>/<path>`) tells "the request never reached the shell" apart from "the shell didn't catch it".

## The member version a shell hosts doesn't match the one compiled in

**Symptom**

```text
❌ Error: the member versions this run hosts in a shell differ from the ones its component.yaml says are compiled in
   File: deploy.yaml
   components[0].members[0]: shell shop/shell@0.1.0 contains shop/stock@0.1.0, but this run hosts shop/stock@0.2.0
```

**Cause**

A shell's image has the code of one specific version of each member compiled in, declared in `shell.members` of the
shell's `component.yaml`. When the deploy file has it host another version, what runs in the shell isn't the code you
think. It's usually caused by editing the deploy file by hand, or upgrading only the member and not the shell.

**Fix**

Under the error are three ways out: upgrade the shell to the version that compiles in the new one; move the member out of
the shell to run on its own; or keep hosting the old version in the shell while the new one runs on its own. How to change
each: [Upgrading a shell](../04-shell/07-shell-upgrade.md#three-ways-out-when-versions-dont-match).

## The shell image is old

**Symptom**

```text
❌ Error: the local image of shell shop/shell@0.2.0 contains other member versions than its component.yaml declares
   Image: shop-shell:0.2.0
   Members in the image: shop/cart@0.1.0,shop/stock@0.2.0
   Members declared in component.yaml: shop/cart@0.1.0,shop/stock@0.1.0
   Suggestion: Rebuild the shell image: brickkit build shop/shell@0.2.0 --force
```

**Cause**

The shell's `shell.members` changed without rebuilding the image. The image tag follows only the shell's version; the
version didn't change, so `build` takes the image to be up to date by default.

**Fix**

`brickkit build <shell>@<version> --force`. Better: when the members a shell compiles in change, raise the shell's
version — for a released version, `component.yaml`, tag and image agree by construction.

## Won't start once in a shell: a start wait cycle

**Symptom**

```text
❌ Error: running the members inside shell shop/shell@0.1.0 makes it wait for a component that in turn waits for the shell
```

**Cause**

There's no cycle between the components, but the shell starts as one unit: it inherits every member's dependencies, and
whatever depends on a member depends on the shell. So the shell waits for a component outside, and that component waits for
the shell.

**Fix**

The error gives three ways out: put the component in the middle into the shell too, or move a member out; make one of the
dependencies optional; or remove one wait with `skipWaitFor`, with the member retrying on its own. See
[Managing members](../04-shell/04-members-management.md#start-cycles-caused-by-merging-and-skipwaitfor).
