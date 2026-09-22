---
name: brickkit-deploy
description: Use when deploying a BrickKit project to Docker or Kubernetes, binding resources like a database/cache/message queue, configuring secrets, or exposing a service externally. Covers the difference between the two deploy paths, the address-injection format, how resource bindings are declared, and secret handling in production. Applies when the user mentions deploy / target / k8s / compose / ingress / resource binding, or asks "how do I go live."
---

# Deployment and resource binding

## When to use this skill

- Running the project on Docker or Kubernetes
- A component reports "resource dependencies not satisfied"
- Wiring up a database, cache, message queue, object storage, search, or SMTP
- Exposing a component outside the cluster
- Handling secrets like passwords and tokens
- Configuring multiple environments (dev / prod)

## Where you'll guess wrong

**1. There are exactly two deploy targets: `docker` and `k8s`.**

`deploy.target` is required. There's no Podman (it was supported once, and has been removed).

**2. Resources are deployed by ops; the platform never installs a database.**

The `resources` block in `brickkit.yaml` is **declaration and binding**, not "have the platform
start a postgres." What the platform manages is the connection identity (how the address, account,
and password get injected into the component); product-specific knobs go through the component's
own `configSchema`. The user has to create the database itself first.

**3. `kind` is a closed enum of six values:** `database` / `cache` / `mq` / `storage` / `search` / `smtp`.

The binding slot for "which spot this component occupies" is named differently per `kind`, and only
one is allowed:

| kind | The slot is called | Injected as |
| --- | --- | --- |
| `database` | `database` | `DATABASE_NAME` |
| `mq` | `vhost` | `MQ_VHOST` |
| `storage` | `bucket` | `STORAGE_BUCKET` |
| `search` | `index` | `SEARCH_INDEX` |
| `cache` / `smtp` | **no such slot** | writing one errors |

The wrong name errors and names the right one.

**4. `kind` and `engine` must exactly match what the component declares.**

A component declares `kind: database` + `engine: postgresql` in `component.yaml`; the resource the
project supplies must match both exactly, or it reports "resource dependencies not satisfied."

**5. Passwords must be referenced through an environment variable, never written in plaintext.**

```yaml
password: ${DB_PASSWORD}
```

`.env` is already in `.gitignore`.

**6. `publicKeys` is the only field that actually makes signature verification take effect.**

With zero public keys configured, signature verification **is disabled entirely**, and
`requireSignature: true` does nothing either — there's no trust anchor to check against. The CLI
warns once about this, but by then it hasn't verified anything.

Public keys have to be configured in the project, not pulled alongside the signature from the
Market — otherwise the Market would be issuing its own certificates to itself, and if it were ever
compromised, an attacker could swap out both the component and the public key together, and
verification would still pass.

**7. There's no overlay / inheritance / merge mechanism.**

Multiple environments means **each environment gets its own complete, self-contained config file**,
e.g. `brickkit.prod.yaml`, selected with `brickkit up --config brickkit.prod.yaml`. Don't look for
a way to "override just the differences" — that design was explicitly rejected.

**8. `limits` has no default; if neither is written, none is generated at all.**

Only `requests` has a default (`100m` / `128Mi`). The platform never guesses `limits` — guessing a
number risks OOMKilling a perfectly healthy component. The recommended pattern is the **opposite**:
set a CPU `requests` and no ceiling (a CPU limit runs through the CFS quota, which throttles into
p99 spikes even when the node is idle); for memory, requests = limits (earns Guaranteed QoS, so
it's the last thing evicted when memory runs short).

**9. Don't merge components just to save memory.**

The only hard constraint is "the sum of every Pod's `requests` on a node ≤ that node's allocatable
capacity" — the sum of `limits` can far exceed capacity, and overcommitting is normal usage. The
real cost is each process's memory floor, almost entirely decided by language: Go 8–20MB,
Python/Node 40–90MB, JVM 200–450MB. 20 idle Spring Boot instances alone eat 4–9G. At that point,
switch runtime or use `mode: disable` to run fewer of them — merging components is solving the
wrong problem.

**10. The platform doesn't do gateways, but `labels` is the passthrough a gateway needs.**

When the user wants to wire up Traefik / Caddy / Prometheus, **don't talk them out of it, and don't
suggest the platform add a gateway** — write `labels` on the component entry, and the platform
copies them verbatim into Docker's service `labels` / K8s's Deployment and Pod `annotations`; the
gateway is deployed out of band and discovers them once it's attached to the
`brickkit-<project>-net` network. The platform never interprets keys or values.

```yaml
  - id: erp/sales
    version: 1.0.0
    labels:
      traefik.enable: "true"
      traefik.http.routers.erp-sales.rule: "PathPrefix(`/erp/sales`)"
```

Three pitfalls: **values must be quoted** (`traefik.enable: true` fails validation on the spot);
**a platform-reserved key is rejected** (`app`, `brickkit.io/*`, `com.docker.compose.*`); **writing
one on a `mode: debug` component warns** — it generates no container, so there's nothing to attach
labels to.

Don't fall back to hand-writing a file-provider config: that file has to be full of **versioned
service names** (`erp-sales-1-0-0`), and it silently goes stale every time the component bumps a
version, with the platform never saying a word about it.

## How the mechanism works

**The address format is identical in both environments**: `http://<versioned-service-name>:<port>`.
Locally that's `http://people-basic-1-0-0:8080`; on K8s it's the exact same string. So component
code needs zero changes. This is also why multiple versions naturally coexist — they're two
non-conflicting DNS names.

**Exposing outside the cluster** is `expose: true` on a component entry. K8s also requires
`hostname`; `exposePort` only applies under Docker; `tlsSecret` only under K8s + expose.

**What `up` does, in order**: decide who starts → generate the deployment files → generate
`local-debug.env` → check image pull permissions → run migrations → invoke the engine. To see the
generated result without actually starting anything, use `--dry-run`.

**Local debugging** means marking a component `mode: debug`: it **generates no container**, and
instead runs in an IDE on your own machine, with `extra_hosts` mapping its versioned service name
into the container network. Multiple components can be debugged locally at once, each with its own
`localPort`. The CLI generates `local-debug.env` for the IDE to load. It's Docker only:
`mode: debug` together with `deploy.target: k8s` is rejected when `brickkit.yaml` is parsed.

**K8s-specific settings** (`context`, `namespace`, `podSecurity`, `ingressClass`,
`serviceAccount`, `networkPolicy`, `replicas`) all live under `deploy` or on a component entry, and
have no effect under Docker. `replicas > 1` auto-generates a PDB.

## Where to dig deeper

(The `docs/...` and `AGENTS.md` paths below all live in the BrickKit repository
<https://github.com/brickKit/brickKit>; every article under `docs/` has an `en/` and a `zh/`
version, content-equivalent.)

- Flags: `brickkit up --help`, `brickkit down --help`
- Generation detail for both deploy paths, Ingress, migration Jobs: `docs/en/06-architecture/03-deployment-generation.md`
- Network policy: `docs/en/03-guide/13-network-policy.md`
- How the six resource kinds are declared, bound, and injected, and secret handling: `docs/en/06-architecture/05-resource-binding.md`
- The full field reference for `brickkit.yaml`: `docs/en/06-architecture/08-brickkit-yaml-reference.md`
