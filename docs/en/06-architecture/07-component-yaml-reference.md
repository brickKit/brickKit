# component.yaml Field Reference

AGENTS.md §6 is the skeleton you copy-paste. This document is what's behind it: every field's type, whether it's required, its default, and — this is the part the skeleton can't show — the exact constraint that fires when you get it wrong, verified line by line against `internal/manifest/types.go` and `internal/manifest/validate.go`. The Manifest has no extension-field mechanism (AGENTS.md §6's closing note): every field below is the complete set, and a key not on this list is rejected at parse time, not silently ignored.

This document owns the *fields*. What actually happens once they're correct — how a dependency turns into an address, how a resource quota merges across three layers, how a healthcheck becomes three different K8s probes — is covered by [04-environment-variables.md](04-environment-variables.md), [05-resource-binding.md](05-resource-binding.md), and [03-deployment-generation.md](03-deployment-generation.md); this document cross-references them rather than repeating them.

## `apiVersion` / `kind`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `apiVersion` | string | yes | must be exactly `brickkit/v1` |
| `kind` | string | yes | must be exactly `Component` |

## `metadata`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `metadata.id` | string | yes | format `scope/name`; all-lowercase; each half matches `[a-z0-9]([a-z0-9-]*[a-z0-9])?` (starts and ends with a letter/digit, `-` allowed in the middle); ≤63 characters total — the versioned service name derived from it has to fit a DNS label |
| `metadata.name` | string | yes | free text, no format constraint |
| `metadata.version` | string | yes | must match `major.minor.patch` (`^\d+\.\d+\.\d+$`) — no `v` prefix, no pre-release suffix, no `^`/`~` range syntax |
| `metadata.description` | string | yes | free text |
| `metadata.vendor` | string | no | free text |
| `metadata.license` | string | no | free text |
| `metadata.apiDocs` | string | no | free text (a URL by convention, not validated as one) |

## `tags`

`[]string`, optional. Free-form; used only for marketplace search filtering — the CLI itself never reads it.

## `artifacts[]`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `artifacts[].type` | string | yes | free-form (`api-contract`, `api-docs`, … — not an enum, the platform doesn't interpret it) |
| `artifacts[].format` | string | no | free-form (`protobuf`, `openapi`, …) |
| `artifacts[].description` | string | no | free text |
| `artifacts[].files` | `[]string` | yes | at least one entry; each must be a **relative** path (an absolute path is rejected) and can't use `..` to escape the component repo root |

## `dependencies.components[]`

Two YAML shapes for the same underlying `ComponentDep`:

```yaml
- department/tree@1.0.0                 # required — a bare "id@version" string
- id: infra/redis-event-bus@1.0.0       # optional — a mapping with optional: true
  optional: true
```

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `dependencies.components[].id` (the part before `@`) | string | yes | same `scope/name` rule as `metadata.id` |
| version (the part after `@` — not a separate YAML key) | string | yes | exact version, same rule as `metadata.version` — a range (`^1.0.0`) is rejected here, not silently accepted |
| `dependencies.components[].optional` | bool | no (default `false`) | only meaningful on the mapping form |

Three things the validator catches that aren't obvious from the shape alone:

- **A component can't depend on itself** — `id == metadata.id` is a hard error.
- **The same component ID can't appear twice** in `dependencies.components`, required or optional, same version or different — the *variable name* a dependency resolves to (`{EnvPrefix}_ENDPOINT`) has no version slot, so a second entry can only ever silently overwrite the first at injection time, not add anything. AGENTS.md §5.1 has the full "why," and the validator's own error message spells out both ways out: depend on only one version (version coexistence is project-level, AGENTS.md §5.1), or route the second one through a `configSchema` property instead (AGENTS.md §9.23).
- Real, verbatim error text for the second case:

  ```
  同一个组件声明了两个版本
       demo/hello@1.0.0（dependencies.components[0]）与 demo/hello@2.0.0
       两者都注入 DEMO_HELLO_ENDPOINT —— 依赖地址的环境变量名基于组件 ID、不带版本号，
       后者覆盖前者，而组件不会察觉自己只连上了其中一个
       出路 1：只依赖其中一个版本。多版本共存是**项目级**的——
               brickkit.yaml 里可以同时跑两个版本，供不同调用方各用各的
       出路 2：确实要同时调两个，把第二个声明成 configSchema 里的一个配置项，
               由项目填地址
  ```

## `dependencies.resources[]`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `dependencies.resources[].kind` | string | yes | one of `database` / `cache` / `mq` / `storage` / `search` / `smtp` — an unrecognized value is rejected, not passed through |
| `dependencies.resources[].engine` | string | yes | non-empty, but **never validated against a known-engines list** — `postgresql`, `mongodb`, `made-up-string` are all equally acceptable to the parser. This declaration is purely descriptive for a human reading the Manifest; it has no effect on what variables get injected (that's driven entirely by `kind` — see [04-environment-variables.md](04-environment-variables.md) §3) and it isn't cross-checked against whatever `engine` the project's `brickkit.yaml` binding later declares for the same resource. |

This block is optional self-documentation, not a functional requirement — nothing stops a project from binding a resource to a component that never declared `dependencies.resources` at all (`brickkit.yaml`'s own `resources[].bindings[].componentId` is what actually wires a resource to a component; see [08-brickkit-yaml-reference.md](08-brickkit-yaml-reference.md)).

## `configSchema`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `configSchema.type` | string | no | if present, must be `object` — there is no other legal value, since `configSchema` always describes a flat map of named properties |
| `configSchema.properties.<key>.type` | string | yes (per property) | one of `string` / `integer` / `number` / `boolean` / `array` / `object` |
| `configSchema.properties.<key>.default` | any | no | not type-checked against the declared `type` (AGENTS.md §9.12) — see [04-environment-variables.md](04-environment-variables.md) §4 for exactly how each type renders into an environment variable, including the array/object footgun (it becomes Go's `fmt.Sprint` form, not JSON) |
| `configSchema.properties.<key>.description` | string | no | free text |
| `configSchema.properties.<key>.enum` | `[]any` | no | **parsed and stored, never read anywhere else in the codebase.** Not by the CLI (no value is ever checked against it), not by the marketplace, not by any renderer. It's pure documentation today — useful for a human or an AI reading the schema, enforced by nothing. |
| `configSchema.properties.<key>.items.type` | string | no (the `items` block as a whole is optional; `type` is the only key in it) | **parsed and stored, never read anywhere else in the codebase either** — grep the whole repo and the only other `.Items` hits are an unrelated Kubernetes list type. Meaningful only by convention on a `type: array` property; today it has exactly the same effect as not writing it at all. |
| `configSchema.properties.<key>.minimum` | number | no | **parsed and stored, and nothing enforces it.** Meaningful only by convention on a `type: integer`/`number` property; writing it or not has exactly the same effect. It must be a number — a string is rejected at parse time, but that checks the spec sheet's own structure, not a value a user filled in: a default outside the bounds, or a `minimum` greater than `maximum`, still passes with no warning. |
| `configSchema.properties.<key>.maximum` | number | no | same as `minimum`. |
| `configSchema.properties.<key>.pattern` | string | no | **parsed and stored, and nothing enforces it.** Meaningful only by convention on a `type: string` property. It is never compiled as a regex, so an invalid one passes too — nothing ever runs it. |
| `configSchema.required` | `[]string` | no | each entry must name a key that's also declared in `properties` — a `required` entry with no matching property is rejected at Manifest-validation time, before the project ever gets a chance to supply a value for it |

`required` is where the one real teeth in this whole block live: a property listed in `required` with no `default` and no `brickkit.yaml` override blocks `brickkit up` outright, for the whole project, not just this component — [04-environment-variables.md](04-environment-variables.md) §5.3 has the full mechanism and the real error text.

**A misspelled key inside a property declaration** (say `defualt` for `default`) doesn't fail Manifest validation; the parser silently drops it — and the component ends up with no default. `Parse` stays quiet about it because a Manifest already published with extra keys (`format`, `examples`, … written out of JSON Schema habit) has to keep installing, and a consumer can do nothing about someone else's Manifest. So this is a **warning** raised only where the author can hear it and fix it, and it guesses the key you meant: `brickkit publish`, and `brickkit add --local` when it scans a local source. It never blocks, and the property names themselves under `configSchema.properties` (names the author chose) are never checked.

## `deployment`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `deployment.type` | string | yes | must be exactly `container` — the only value, on purpose (AGENTS.md §9.10: no "static" type, a frontend is a container too) |
| `deployment.image` | string | yes | non-empty; not otherwise validated (no registry reachability check at Manifest-validation time) |
| `deployment.port` | int | yes | `1`–`65535` |
| `deployment.extraPorts[].name` | string | yes (per entry) | ≤15 characters, matches `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$` — this is the Kubernetes Service port name rule (`IANA_SVC_NAME`) applied at the Manifest level, because an extra port's `name` becomes a literal K8s Service port name at generation time; must be unique among a component's own `extraPorts` |
| `deployment.extraPorts[].port` | int | yes (per entry) | `1`–`65535`; can't equal `deployment.port` |
| `deployment.resources.requests.cpu` / `.memory` | string | no | free-form (Kubernetes quantity syntax, e.g. `"100m"`, `"128Mi"`); not validated for parseability by the CLI itself |
| `deployment.resources.limits.cpu` / `.memory` | string | no | same |
| `deployment.labels` | `map[string]string` | no | see the reserved-key rule below |

**`deployment.resources`, if present at all, must declare at least one of `requests`/`limits`, and each of those, if present, must declare at least one of `cpu`/`memory`** — an empty `resources: {}` or an empty `requests: {}` is rejected rather than silently doing nothing. The full three-layer merge with `brickkit.yaml`'s own override and the CLI's built-in default is [05-resource-binding.md](05-resource-binding.md)'s subject, not this document's — including why only `requests` gets a platform default and `limits` never does.

**`deployment.labels` reserved-key rule**: a key can't start with `brickkit.io/` (the platform's own namespace — component ID, version, project name all live under it), can't start with `com.docker.compose.` (Compose's own bookkeeping labels), and can't be exactly `app` (Kubernetes' own Deployment→Pod selector key, also what `NetworkPolicy` matches on). Hitting any of these is a hard parse-time error, not a silent drop — a value is never checked at all, only the key. The whole point of this field existing is described in AGENTS.md §9.22: it's a passthrough specifically so the platform can keep not understanding what any label means, so nothing about a label's *value* is ever validated, only whether its *key* collides with one the platform needs for its own bookkeeping.

## `migration`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `migration.command` | `[]string` | yes, if `migration` is present at all | at least one element; every element must be non-empty after trimming whitespace |

`migration` itself is optional — omit the whole block for a component with nothing to migrate. There's no separate `migration.image`: the migration container always reuses `deployment.image`, distinguished only by which command gets passed — which is exactly why AGENTS.md's callout about entrypoints failing fast on an unrecognized argument exists (§6's block quote).

## `healthCheck`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `healthCheck.type` | string | yes | one of `http` / `tcp` / `none` |
| `healthCheck.path` | string | required only when `type: http` | must start with `/` |
| `healthCheck.startPeriodSeconds` | int | no (default `60`, `DefaultStartPeriodSeconds`) | must be a positive integer, `≤ 3600` (one hour) — **and setting it at all under `type: none` is itself rejected**, not silently ignored: `type: none` generates no probe whatsoever, so there is nothing for a grace period to apply to, and the validator says so by name rather than letting the field sit there unused |

The 3600-second ceiling isn't about long grace periods being harmful — a component that genuinely takes ten minutes to warm up is allowed to say so. It exists because the single most common way to get this field wrong is writing milliseconds by habit: `startPeriodSeconds: 60000` reads like "60 seconds" and is actually 16 hours, and unlike almost every other mistake in this document, this one **produces no error at all** — the component just sits in `starting` for most of a day while nothing looks obviously broken. The full mechanics of what `startPeriodSeconds` actually changes downstream — the K8s `startupProbe`'s `failureThreshold` computed as `ceil(startPeriodSeconds ÷ 5)`, the 60-second default you get by not setting it at all — are in [03-deployment-generation.md](03-deployment-generation.md), not repeated here.

**⚠️ AGENTS.md §6's health-check prohibition still applies and isn't re-validated by the CLI**: `healthCheck.path` must only check this process's own liveness. Nothing in `internal/manifest/validate.go` can detect "this handler also pings a database" — that's a code-review-time rule, not a parseable constraint, which is exactly why AGENTS.md states it as a design principle rather than this document listing it as a field constraint.

## What's deliberately not here

Two fields that used to exist — `observability` (a pair of `metrics`/`tracing` booleans) and `compatibility.minCliVersion` — were removed rather than left unused, and there is no extension mechanism to bring back something equivalent without the platform explicitly adding a new field. AGENTS.md §6's closing note has the reasoning for both; the short version is that neither was ever read anywhere, and `minCliVersion` was actively worse than simply absent — a component declaring `minCliVersion: 2.0.0` would install without complaint on a CLI that predates that version entirely, which looks like a safety gate while providing none.

## Read further

- AGENTS.md §6 — the copy-pasteable skeleton this document expands
- [04-environment-variables.md](04-environment-variables.md) — exactly what each `configSchema` property and each declared dependency actually turns into inside the container
- [05-resource-binding.md](05-resource-binding.md) — the quota merge chain, and what happens when a `brickkit.yaml` binding collides with this Manifest's expectations
- [03-deployment-generation.md](03-deployment-generation.md) — how `deployment`/`healthCheck`/`migration` become real Compose services or K8s resources, probe by probe
- [08-brickkit-yaml-reference.md](08-brickkit-yaml-reference.md) — the project-side counterpart: what a project writes to actually run, override, expose, and bind resources to a component described here
