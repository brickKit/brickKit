# component.yaml fields

`component.yaml` is how a component describes itself. The CLI installs the component, generates its deployment files and
injects its environment variables from this one file, and never reads the component's source. This page goes block by
block: what each one is for and how to write it well. Each field's exact type and validation rules are in the
[component.yaml schema](../11-reference/01-component-yaml-schema.md).

`demo/quote`'s complete `component.yaml`:

```yaml
apiVersion: brickkit/v1
kind: Component

metadata:
  id: demo/quote
  name: Quote
  version: 0.1.0
  description: Returns a quotation each time, prefixed with demo/hello's greeting

artifacts:
  - type: api-contract
    format: openapi
    files:
      - api/openapi.yaml

dependencies:
  components:
    - demo/hello@1.1.0

configSchema:
  type: object
  properties:
    QUOTE_PREFIX:
      type: string
      default: "Quote of the day: "
      description: The prefix put in front of the quotation

deployment:
  type: container
  build:
    context: .
    dockerfile: Dockerfile
  port: 8080

healthCheck:
  type: http
  path: /healthz
```

Only known fields may appear: a misspelled field name (`dependancies`) is rejected on the spot, not quietly ignored.

## metadata: who I am

| Field | Meaning |
| --- | --- |
| `id` | `<scope>/<name>`, globally unique. It decides the repository name (`demo-quote`), the service name (`demo-quote-0-1-0`) and the address variable others get (`DEMO_QUOTE_ENDPOINT`) |
| `version` | An exact `major.minor.patch`. **The single source of truth for the version**: the tag `release` creates and the image tag `build` uses both come from it |
| `name`, `description` | A name for people, and a one-sentence description. `description` is also the "What it does" column of the component table at the end of each user project's `AGENTS.md`, so say what the component does, not what it is called |
| `vendor`, `license`, `apiDocs` | Optional: publisher, licence, API docs address |
| `repository` | Optional: the address of the component's repository or page. It becomes the "Home" column of that same table, so someone reading the project on the web (where there is no `.brickkit/`) can still reach each component |

`tags` are optional search tags.

## dependencies: what I depend on

```yaml
dependencies:
  components:
    - demo/hello@1.1.0             # required
    - id: demo/bus@1.0.0           # optional
      optional: true
```

- **Exact versions only.** `^1.0.0`, `~1.0` and `latest` are all rejected: a dependency's version is fixed the moment you
  release, and users always get the one you tested.
- A missing **required** dependency makes the user's `up` fail without starting. A missing **optional** one only warns, and
  its address variable is **not injected at all** — not injected as an empty string. So code reading an optional
  dependency's address must handle "no such variable":

  ```go
  bus, ok := os.LookupEnv("DEMO_BUS_ENDPOINT")
  if !ok {
      // degrade: send no events, keep serving
  }
  ```

  Why not an empty string: `http.Get(endpoint + "/healthz")` with an empty string becomes a request to `/healthz` — it hits
  the component itself and returns 200, and you'd believe the dependency is fine. Better to fail loudly at start than to
  go wrong quietly at runtime.
- **A component ID can appear only once.** The address variable's name is derived from the component ID, without the
  version, so two versions of one component would collide on the same variable.

For each dependency, the platform injects an address variable: `<component ID uppercased, / and - turned into _>_ENDPOINT`,
whose value carries the version — `DEMO_HELLO_ENDPOINT=http://demo-hello-1-1-0:8080`. A dependency with extra ports also
gets `DEMO_HELLO_<port name>_ENDPOINT`.

## configSchema: the config I need

```yaml
configSchema:
  type: object
  properties:
    QUOTE_PREFIX:
      type: string
      default: "Quote of the day: "
      description: The prefix put in front of the quotation
  required: []
```

**The key is the environment variable name**, injected as-is, so write it in upper case with underscores, like `DB_HOST`.
Each item can have `type`, `default` and `description`; a secret gets `secret: true`, and required items go into
`required`. `enum`, `minimum`, `maximum`, `pattern` and `items` can be written too, but they're documentation — the
platform doesn't check values against them.

How to design config well — how finely to split it, how to name it, what should be required — is in
[Designing a configSchema](03-config-schema-design.md).

## deployment: how I run

| Field | Meaning |
| --- | --- |
| `type` | Always `container` |
| `image` | A prebuilt image that users pull |
| `build` | Built locally: `context` (the build context) and `dockerfile`, both relative to the repository root, `.` and `Dockerfile` by default |
| `port` | The main port: the health check hits it, and others' `*_ENDPOINT` points at it |
| `extraPorts` | Extra ports (gRPC, say): `- name: grpc` / `port: 9090`; others get `…_GRPC_ENDPOINT` |
| `resources` | Recommended CPU / memory; users can override them in the deploy file |
| `stopGracePeriodSeconds` | How long after a stop signal it needs to finish what it holds (a consumer acknowledging in-flight messages, an outbox sending its batch, HTTP finishing in-flight requests). Unset: the engine's default, 10 s for compose, 30 s for K8s. Your code's own shutdown timeout should be shorter than it |
| `labels` | Labels passed through as-is (Docker service labels, Kubernetes Pod annotations); the platform doesn't interpret them |

Write at least one of `image` and `build`. Only `build`: users build from your tag with `brickkit build`. Both: users of
your Git or market release pull the image, while people who clone the source to develop build from source.

**Recommend `requests` only.** You know what you need at steady state; "how far it may grow" is the deployer's call, and
if you write `limits.cpu`, users can't take it away. The platform invents no default `limits`: invent 512Mi, and a
perfectly healthy component that needs 600Mi gets OOM-killed.

## migration: database migrations

```yaml
migration:
  command: ["/app/quote", "migrate"]
```

Before the main service starts, `up` runs this command once in **the same image**; if the migration fails, the main
service doesn't start. On Kubernetes it's a Job — run once, however many replicas.

**The entry point must fail and exit immediately on an argument it doesn't recognise.** The migration container and the
main service are the same image, told apart only by arguments; if the entry point treats a misspelled argument as "then
start the service", the migration container becomes a second service that runs forever, the main service waits for the
migration to finish forever, and the logs look perfectly fine. Check the argument before reading environment variables or
connecting to the database.

## healthCheck: am I alive

```yaml
healthCheck:
  type: http          # http | tcp | none
  path: /healthz
  startPeriodSeconds: 90
```

- **Only check that this process is alive.** Don't connect to the database or call dependencies in a health check: one
  downstream hiccup gets every upstream marked unhealthy and restarted at once — a cascading failure.
- The rhythm is fixed by the platform (every 10 seconds, 3-second timeout, 3 failures in a row mean unhealthy). The
  **startup grace period** defaults to 60 seconds; a component with a longer cold start (a heavy Spring Boot, Django
  preloading a lot, .NET's first JIT) has to set `startPeriodSeconds`, or it CrashLoopBackOffs forever on Kubernetes. The
  grace period only delays "declaring it dead", never "declaring it alive", so a generous value costs nothing.
- On Docker, the HTTP check runs `wget` or `curl` inside the container: the image needs at least one of them (Alpine ships
  `wget`; distroless images have neither — use `type: tcp` there).

## readinessCheck: can I take traffic

```yaml
readinessCheck:       # optional
  type: http          # http | tcp
  path: /readyz
```

Alive isn't the same as ready for traffic: while a cache warms up, a first sync hasn't finished or permission data hasn't
arrived, the process is fine but can only answer 503. The two have different consequences — a failed liveness check gets
the container killed and restarted on Kubernetes; a failed readiness check only keeps traffic away. So a component with such
a period declares a readiness check of its own:

| Target | Where it is used |
| --- | --- |
| Kubernetes | The readinessProbe checks it (during a rolling update, a new Pod that isn't ready gets no traffic); the startupProbe and livenessProbe keep checking `healthCheck` |
| Docker / Podman | The compose healthcheck checks it: dependents start only once it can really take traffic, and `up` waits until then. A compose healthcheck never kills the container, so the question it asks was "ready?" all along |

Without it, readiness follows `healthCheck`, as before. The rule is the health check's: **only whether this process is
ready, never the downstream** — failing readiness whenever a downstream is briefly down takes every replica out of
service at once. The start-up grace period and the check rhythm are shared with `healthCheck`.

## shell: I'm a shell

```yaml
shell:
  members:
    - erp/api@1.2.0
    - erp/auth@2.0.0
```

With `shell.members`, the component is a **shell**: at build time it compiles the listed components (at exact versions)
into its own process. Only shells write this block. See [Shells](../04-shell/README.md).

## artifacts: my contract for callers

```yaml
artifacts:
  - type: api-contract
    format: openapi
    description: HTTP interface
    files:
      - api/openapi.yaml
```

`type` and `format` are free strings the platform doesn't interpret; it only delivers `files` into the user's
`.brickkit/artifacts/`. See [Contracts and artifacts](06-artifacts-and-contracts.md).

## local: how to start me as a process on the machine

When a user sets the component to `mode: local`, BrickKit recognises the language from the source directory and starts it
itself (`go.mod` → `go run .`, `package.json` → its `start` script…). When it can't tell, or finds more than one, write
this block:

```yaml
local:
  language: go                  # which language, when the directory has marker files for several
  runCommand: ["go", "run", "./cmd/server"]   # or give the start command outright
```

Under `mode: local` the platform also injects `PORT` (the port on the machine it chose for this process, preferring
`deployment.port`): code that reads `PORT`, falling back to its own default, never fights another process for a port.
