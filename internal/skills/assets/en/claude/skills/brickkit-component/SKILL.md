---
name: brickkit-component
description: Use when writing a new BrickKit component from scratch, editing component.yaml, adding a database migration, or exposing a gRPC / HTTP API from a component. Covers the hard contract every component must satisfy, the reserved-variable no-go zone, the health-check prohibition and startup grace period, and the rules for declaring dependencies. Applies when the user says "write a component," "how do I declare a component that won't start," or is editing component.yaml.
---

# Writing a BrickKit component

## When to use this skill

- Writing a new component from scratch
- Editing `component.yaml`
- Adding a database migration to a component
- Giving a component an extra port (gRPC, say)
- A component won't start, and a declaration might be the cause

## From scratch: run brickkit new first

`brickkit new <scope>/<name>` generates a `component.yaml` skeleton that already passes validation,
written to `components/<scope>/<name>/` (the same layout a local install source scans). With
`--contract openapi` or `--contract proto` it also generates a placeholder contract file and
registers it under `artifacts`. The skeleton is full of TODOs — no Dockerfile, no source code, the
platform doesn't pick a language for you. After finishing the TODOs, run `brickkit lint` from the
next section, then check it against every "where you'll guess wrong" item below.

## After writing or editing: run brickkit lint first

Whenever `component.yaml` has changed — whether you just finished the skeleton's TODOs or hand-edited
it later — run `brickkit lint` before anything else. It's offline, read-only, needs no Docker, and
answers instantly, reporting every structural problem at once: missing required fields, wrong
types, unrecognized keys (typos), version format, port ranges, plus two kinds of warning — a
misspelled key inside a `configSchema` property (like `defualt`, which silently has no effect), and
a config item name that collides with a reserved variable (point 2 below). It runs directly inside a
standalone component repository (just `component.yaml`, no `brickkit.yaml`); inside a BrickKit
project it also checks every `component.yaml` under the local install sources.

It **doesn't check** whether dependencies can actually be resolved, or whether a `servedBy` target
exists — that needs the network, and is left to `brickkit up --dry-run` in the BrickKit project that
uses this component (a standalone component repository has no `brickkit.yaml`, so `up` doesn't run
there); nor does it check the values behind `configSchema`'s `enum` / `minimum` — the platform never
validates config values.

## Where you'll guess wrong

**1. `component.yaml` has no extension-field mechanism.**

`apiVersion` is `brickkit/v1`. An unrecognized key is **rejected on the spot**, not silently
ignored. So don't add custom fields — there used to be two "reserved" fields, `observability` and
`compatibility.minCliVersion`; both were removed: neither was ever read anywhere, and the second
was worse — it looked like a safety gate, but a component declaring `minCliVersion: 2.0.0` would
install just fine on an old CLI.

Two exceptions, use them instead of inventing a field: **to carry metadata for the underlying
engine** (gateway routing, monitoring scrape config) use `deployment.labels` (point 8); **to ship a
file for your own tooling** (extra metadata, a contract, an SDK) use `artifacts` — it's registered
alongside the Manifest, closed-source components have theirs stored by the Market, `add` / `fetch`
both download it to `.brickkit/artifacts/<versioned-service-name>/<type>/`, and the CLI **only
downloads it, never parses it** — exactly the semantics this kind of thing needs.

**2. Never touch a reserved variable.** This is the easiest one to trip over.

A `configSchema` item's name, once uppercased, must not collide with any of these:

| Pattern | How it matches |
| --- | --- |
| `COMPONENT_ID`, `COMPONENT_VERSION` | Exact |
| `*_ENDPOINT` | Suffix |
| `DATABASE_*`, `REDIS_*`, `MQ_*`, `STORAGE_*`, `SEARCH_*`, `SMTP_*` | Prefix |
| `{envPrefix}_*` | Prefix, `envPrefix` set by whoever uses it in `brickkit.yaml` |

What happens on a collision: the Market **refuses** it at publish time; the CLI **warns and skips
that config item** at injection time, and the platform-injected value wins. In other words, your
config item silently has no effect. The naming rule is `defaultPageSize` → `DEFAULT_PAGE_SIZE`, so
`databaseTimeout` collides with `DATABASE_*` — rename it to something like `dbTimeout`.

**3. Health-check prohibition: `/healthz` only checks that this process itself is alive.**

**Never** check a database, a dependency component, or any external system inside it. The reason is
cascading failure: one downstream hiccup gets every upstream marked unhealthy and restarted at the
same time, turning a local problem into a platform-wide one.

**4. A component with a cold start longer than the default 60 seconds must raise `startPeriodSeconds`.**

`interval` / `timeout` / `failureThreshold` are fixed by the platform (10s / 3s / 3); their product
is only 30 seconds, so the platform gives every component a **60-second** startup grace period by
default, and most components never need to touch it. Exceed 60 seconds: under Docker it's marked
`unhealthy` and `up` fails, stalling any dependent waiting on `service_healthy`; under K8s the Pod
gets killed and restarted, runs through the same 60 seconds again → **permanent
CrashLoopBackOff, while the container's own logs look completely normal**.

A heavy Spring Boot app, a Django project that preloads a lot, or .NET's first JIT pass can reach
that line. The grace period only delays "declaring it dead," never "declaring it alive" (a
component ready in two seconds still turns healthy in two seconds), **so setting it generously
costs nothing**. It's the only overridable timing parameter under `healthCheck`.

**5. One component ID can only appear once in `dependencies`.**

You can't depend on both `people/basic@1.0.0` and `@2.0.0`. Because the dependency address's
**variable name is based on the component ID and carries no version**, writing two versions would
collide on the same `PEOPLE_BASIC_ENDPOINT`, with the latter silently overwriting the former. The
CLI errors on this when it parses the Manifest.

Diamond dependencies are unaffected (A depends on X@1, B depends on X@2, each gets its own).
Coexisting versions is a **project-level** capability.

**6. A missing optional dependency injects nothing at all, not an empty string.**

This is the most commonly misunderstood part of BrickKit's design. Component code must read env
vars defensively: Python's `os.environ.get()`, Java's `System.getenv()`. Using `os.environ["X"]`
throws a `KeyError` and crashes the component immediately — **this is deliberate**, so "the
dependency isn't there" surfaces at startup instead of becoming a runtime mystery pointed at an
empty address.

Degradation logic (query the database, return an empty list, or write to a local file for later
retry when Redis is down) is your own business code — the platform doesn't manage it.

**7. `resources` should generally only set `requests`, not `limits`.**

The quota priority chain is `brickkit.yaml` > `component.yaml` > the CLI's own default, and it
**merges field by field** — a component that writes `limits.cpu` can never have it removed by the
project config. A ceiling is a business judgment call, left to the deployer.

**8. `deployment.labels` is the one deployment-metadata passthrough, and values must be strings.**

The platform **never interprets keys or values**, it just carries them across: Docker writes them
into the service's `labels`, K8s into the Deployment's and Pod's `annotations`. A component author
should write facts only they know (`prometheus.io/port: "9090"` — which port I expose metrics on);
gateway routing belongs to the deployer, who overrides it **key by key** in `brickkit.yaml`.

Two pitfalls: **a value without quotes fails validation on the spot** (`prometheus.io/scrape: true`
gets parsed by YAML as a boolean); **a platform-reserved key is rejected outright** (`app`,
`brickkit.io/*`, `com.docker.compose.*`).

## How the mechanism works

**Address injection.** The platform injects each dependency's address for you, with a variable name
based on the component ID (`/` and `-` become `_`, uppercased) and a value pointing at the
versioned service name:

```
PEOPLE_BASIC_ENDPOINT=http://people-basic-1-0-0:8080
PEOPLE_BASIC_GRPC_ENDPOINT=http://people-basic-1-0-0:9090
```

An extra port's variable name is `{component-ID-prefix}_{NAME-uppercase}_ENDPOINT`, where `NAME`
comes from `deployment.extraPorts[].name`.

**The address format is identical locally and in production** (always
`http://<versioned-service-name>:<port>`), so component code needs zero changes between
environments.

**Service name = transformed component ID + exact version**: `/` → `-`, `.` → `-`, all lowercase.
`people/basic` + `1.0.0` → `people-basic-1-0-0`.

**The main port serves two purposes**: the health check, and the `_ENDPOINT` variable.
`deployment.port` is required.

**Migrations** are `migration.command`, array form. Migration state is your own business (a
migrations table); the platform's only job is to run it as one pre-startup step during `up`.

**`deployment.type` is always `container`**, frontend components included — a frontend equally
needs to listen on a port, provide a health check, be exposed via Ingress, and get its backend
address through environment variables.

## Where to dig deeper

(The `docs/...` and `AGENTS.md` paths below all live in the BrickKit repository
<https://github.com/brickKit/brickKit>; every article under `docs/` has an `en/` and a `zh/`
version, content-equivalent.)

- Every `component.yaml` field's rules and the full reference: `docs/en/06-architecture/07-component-yaml-reference.md`
- The complete dictionary of environment-variable naming and reserved variables: `docs/en/06-architecture/04-environment-variables.md`
- A hands-on tutorial (writing a component from scratch): `docs/en/03-guide/11-build-your-own.md`;
  a complete Go component example (database, migrations included): `docs/en/04-go-component-template.md`
