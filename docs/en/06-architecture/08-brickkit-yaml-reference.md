# brickkit.yaml Field Reference

AGENTS.md §7 is the skeleton. This document is the dictionary behind it — every field's type, whether it's required, its default, and the exact constraint the validator applies, verified line by line against `internal/config/config.go` and `internal/config/validate.go`. Unlike `component.yaml`, `brickkit.yaml` genuinely is project-specific free-form state in places (`config` overrides, resource credentials), so not every field below has a fixed enum of legal values — where that's true, this document says so explicitly rather than implying a constraint that doesn't exist.

This document owns the *fields*. What a field actually does once it's set correctly — how `enabled` cascades, how a resource binding turns into environment variables, how `servedBy` merges a member's config onto its shell — is covered by [04-environment-variables.md](04-environment-variables.md), [05-resource-binding.md](05-resource-binding.md), [02-dependency-resolution.md](02-dependency-resolution.md), and AGENTS.md §5; this document cross-references them rather than repeating them.

The fields on this page also exist as a JSON Schema, [`schemas/brickkit.schema.json`](../../../schemas/brickkit.schema.json), generated from the same Go structs. An editor can use it to complete field names and underline unknown keys as you type; [Wire up your editor](../00-quick-start.md#wire-up-your-editor) shows how to attach it, and what it deliberately doesn't cover.

## Top level

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `project` | string | yes | matches `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$` — lowercase letters, digits, `-`, starting and ending with a letter or digit. Used directly as the Docker network name suffix and the default K8s namespace suffix. |
| `deploy` | object | yes | see below |
| `sources` | `[]Source` | no | see below |
| `components` | `[]Component` | no | see below |
| `resources` | `[]Resource` | no | see below |
| `installer` | object | no | see below |

## `deploy`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `deploy.target` | string | yes | `docker` or `k8s` |
| `deploy.context` | string | no | free string (a kubeconfig context name); **K8s only** — meaningless under `docker` but not rejected if present, simply unused |
| `deploy.namespace` | string | no (default `brickkit-<project>`) | same character rule as `project`; **K8s only** |
| `deploy.podSecurity` | string | no | only `""` (unset) or `"restricted"` are legal — there's no `baseline`/`privileged` value to opt into, on purpose (AGENTS.md's "secure by default" doesn't extend to auto-enabling a setting that can stop an already-working component from starting) |
| `deploy.imagePullSecrets` | `[]string` | no | free-form Secret names; **K8s only** |
| `deploy.ingressClass` | string | no | free string, written verbatim into the generated Ingress's `spec.ingressClassName`; **K8s only** |
| `deploy.ingressAnnotations` | `map[string]string` | no | passthrough, unvalidated, same posture as `deployment.labels` on the component side; **K8s only** |
| `deploy.createNamespace` | `*bool` | no (default `true`) | **K8s only** — `false` means the CLI neither generates nor applies a `Namespace` object at all, for projects with only namespace-scoped RBAC permissions |
| `deploy.networkPolicy` | object | no | see below; **K8s only** |
| `deploy.serviceAccount.enabled` | bool | no (default `false`; the `serviceAccount` block as a whole is optional) | **K8s only** — `true` generates one dedicated, tokenless ServiceAccount per component; unset, every Pod runs under the namespace's `default` ServiceAccount with its token auto-mounted as normal (AGENTS.md §7's warning) |

### `deploy.networkPolicy`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `deploy.networkPolicy.enabled` | bool | yes (to turn the block on) | |
| `deploy.networkPolicy.ingressController.namespace` | string | required whenever `ingressController` is present at all | an empty value would generate a Kubernetes label selector matching nothing, silently applying successfully while blocking the ingress controller itself — so it's rejected outright rather than producing that |
| `deploy.networkPolicy.ingressController.podSelector` | `map[string]string` | no | omitted = the whole namespace named above, not narrowed further |
| `deploy.networkPolicy.allowFrom[].name` | string | yes (per entry) | free text, used only in error messages and the generated policy's own annotation — this is what lets `kubectl get networkpolicy -o yaml` be self-explanatory six months later |
| `deploy.networkPolicy.allowFrom[].namespace` | string | yes (per entry) | same "empty = matches nothing, applies clean, blocks silently" reasoning as `ingressController.namespace` |
| `deploy.networkPolicy.allowFrom[].podSelector` | `map[string]string` | no | omitted = the whole named namespace |
| `deploy.networkPolicy.allowFrom[].ports` | `[]int` | no | each `1`–`65535`; omitted = every port the target component itself declares (`deployment.port` + `extraPorts`) |

**Generation-time-only check, not caught here**: a component with `expose: true` requires `ingressController` to actually be set, but that can't be validated from `brickkit.yaml` alone — it depends on which components end up running, which needs the dependency graph. That check lives in the K8s renderer, not this parser.

### `deploy.networkPolicy.egress`

⚠️ Turning this on flips the default posture for every component from "can reach anything" to "can only reach what's explicitly allowed" — AGENTS.md §7's warning box, restated because it's the one field on this page capable of taking down a working deployment on its own if a target is missed, and the failure is silent until the next Pod restart.

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `deploy.networkPolicy.egress.enabled` | bool | yes (to turn the block on) | |
| `deploy.networkPolicy.egress.allowTo[].name` | string | yes (per entry) | same annotation purpose as `allowFrom[].name` |
| `deploy.networkPolicy.egress.allowTo[].resource` | string | no | must name a `resources[].id` — mutually exclusive with writing `ports` directly (the port is read from that resource's own `port` field instead, so the two can't drift apart) |
| `deploy.networkPolicy.egress.allowTo[].namespace` | string | one of `namespace`/`cidr`, not both | a cluster-internal target |
| `deploy.networkPolicy.egress.allowTo[].podSelector` | `map[string]string` | no | narrows `namespace` further |
| `deploy.networkPolicy.egress.allowTo[].cidr` | string | one of `namespace`/`cidr`, not both | an out-of-cluster target (a managed database, a third-party API) |
| `deploy.networkPolicy.egress.allowTo[].ports` | `[]int` | no, and rejected together with `resource` | each `1`–`65535` |

Writing neither `namespace` nor `cidr` is rejected; writing both is also rejected — exactly one target-location field per entry. DNS itself and every dependency-graph edge between components are handled automatically and need no entry here at all; only resources and genuinely out-of-band destinations do.

## `sources[]`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `sources[].id` | string | yes | unique among all sources |
| `sources[].type` | string | yes | `market` / `git` / `local` |
| `sources[].url` | string | required for `market` and `git`, ignored for `local` | |
| `sources[].path` | string | required for `local`, ignored for `market`/`git` | |
| `sources[].ref` | string | no | git only; unset = the repository's default branch |
| `sources[].authToken` | string | no | typically an `${ENV_VAR}` reference; prefers stored `.brickkit/credentials` from `brickkit login` if already authenticated |
| `sources[].enabled` | `*bool` | no (default `true`) | a disabled source is skipped entirely during resolution |

## `components[]`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `components[].id` | string | yes | same `scope/name` rule as a Manifest's `metadata.id` |
| `components[].version` | string | yes | exact version, same rule as a Manifest's `metadata.version` |
| `components[].enabled` | `*bool` | no | three-state: unset follows the top (AGENTS.md §5.4), `true` pins it on, `false` pins it off |
| `components[].local` | bool | no (default `false`) | see the mutual-exclusion notes below |
| `components[].localPort` | int | required *if* `local: true` and you want a fixed host port; otherwise omit | only legal when `local: true` is also set — present without it is rejected, not ignored; `1`–`65535`; must be unique across every component's `localPort` in the project |
| `components[].servedBy` | string (`id@version`) | no | see the mutual-exclusion notes below |
| `components[].expose` | bool | no (default `false`) | |
| `components[].hostname` | string | required when `expose: true` **and** `deploy.target: k8s` | not required under `docker` — Compose exposure is a host port, not a domain |
| `components[].exposePort` | int | only legal when `expose: true` | `1`–`65535`, unique across every component's `exposePort`. Meaningful only under `deploy.target: docker` (it becomes the host port mapping); **the validator does not check `deploy.target` here** — writing it alongside `deploy.target: k8s` is accepted at parse time and then silently unused, since the K8s path exposes through `hostname` and a generated Ingress instead. This is the one exception to this document's "written but inert is always rejected" pattern; nothing currently catches it. |
| `components[].tlsSecret` | string | only legal when `expose: true` | **K8s only**, names a pre-existing Secret holding the Ingress TLS cert |
| `components[].serviceAccountName` | string | no | **K8s only**; references an SA the operator already created — the platform never generates one for a component that sets this |
| `components[].config` | `map[string]any` | no | keys checked against the component's `configSchema.properties` (warns, doesn't block, if a key doesn't match — [04-environment-variables.md](04-environment-variables.md) §5.2); values never type-checked |
| `components[].resources.requests.cpu` / `.memory` | string | no | same shape as a Manifest's `deployment.resources.requests` (free-form strings, Kubernetes quantity syntax). See [05-resource-binding.md](05-resource-binding.md) for how this merges against the Manifest's own recommendation and the CLI default |
| `components[].resources.limits.cpu` / `.memory` | string | no | same as above, matching a Manifest's `deployment.resources.limits` |
| `components[].replicas` | `*int` | no (default `1`, **K8s only**) | must be `≥ 1` if set — `0` is rejected rather than treated as "off"; use `enabled: false` to actually stop a component, since that goes through the cascade and warns dependents, while `replicas: 0` would leave dependents starting normally and connecting to nothing |
| `components[].labels` | `map[string]string` | no | same reserved-key rule as a Manifest's `deployment.labels` (can't start with `brickkit.io/` or `com.docker.compose.`, can't be exactly `app`); merges key-by-key over the Manifest's own `deployment.labels`, this side winning on conflicts |

### Mutual exclusions among `components[].local` / `.servedBy` / `.replicas`

These three fields describe three different, incompatible ideas about where a component's process actually runs, and the validator rejects every pairwise combination:

- **`local: true` + `servedBy`** — rejected outright: `local` means "this component runs on your machine, outside any container, for IDE debugging"; `servedBy` means "this component's code is already compiled into another component's image." A component can't simultaneously not be containerized and be merged into someone else's container.
- **`servedBy` chains** — a component can't declare `servedBy` pointing at a target that *itself* declares `servedBy` ("a shell can't be served by another shell"), and a component can't be the target of a `servedBy` from someone else while also being `local: true` itself — a shell has to be reachable from the container/cluster network, which a process running on a developer's own machine structurally can't provide.
- **`servedBy` self-reference** — `components[].servedBy` can't equal that same entry's own `id@version`.
- **`replicas` + `local`** — rejected: `local: true` means the component runs as a single process in your IDE; a replica count describes multiple Pods, which has no meaning for a process that isn't a Pod at all.

`localPort` without `local: true`, and `exposePort`/`tlsSecret` without `expose: true`, follow the same "written but inert" philosophy as `healthCheck.startPeriodSeconds` under `type: none` on the component side ([07-component-yaml-reference.md](07-component-yaml-reference.md)) — rejected at parse time rather than silently doing nothing, because a config value that's simply ignored is exactly the failure mode this whole platform tries hardest to avoid.

## `resources[]`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `resources[].kind` | string | yes | one of `database` / `cache` / `mq` / `storage` / `search` / `smtp` |
| `resources[].engine` | string | yes | non-empty, free-form — same "descriptive only, not cross-checked against anything" status as the `engine` field on a Manifest's `dependencies.resources` |
| `resources[].id` | string | yes | unique among all resources in the project |
| `resources[].host` | string | yes | free string |
| `resources[].port` | int | yes | `1`–`65535` |
| `resources[].username` | string | no | |
| `resources[].password` | string | no | not required to be an `${ENV_VAR}` reference by the parser, but writing a literal secret here triggers a plaintext-password warning at `up` time (AGENTS.md §7's "must be referenced via an env var" is enforced as a warning, not a hard parse error) |
| `resources[].existingSecret` | string | no | K8s only. References a Secret an external system (Vault Secrets Operator, External Secrets Operator, Sealed Secrets, …) already put in the cluster, instead of the platform generating one from `password`. Mutually exclusive with `password` — writing both errors. The platform never reads or writes the value; it only points the `secretKeyRef` at this name, using the same key (`password` or `secret-key`) it would use for a generated Secret. Ignored (with a warning) under `docker`. A `configSchema` property declared `secret: true` can use the same idea for a component's own config value: write `{ existingSecret: <name>, key: <key-in-secret> }` in place of a scalar. See [Secrets](../07-patterns/10-secrets.md) for how this fits with the other way to hand a secret to `brickkit up`. |
| `resources[].bindings[]` | `[]Binding` | no | see below |

### `resources[].bindings[]`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `resources[].bindings[].componentId` | string | yes | **not cross-checked against `components[]` at validation time** — a binding naming a component that isn't declared anywhere is a *warning* at `up` time (a "dangling binding," harmless by construction: nothing reads it), never a parse-time error. This is deliberate: it lets a project declare resources and bindings before running `brickkit add` for the component itself, and it means `brickkit remove` leaving a stray binding behind doesn't break every subsequent command. |
| `resources[].bindings[].database` / `.vhost` / `.bucket` / `.index` | string | no | **mutually exclusive with each other** — at most one may be set per binding, and which one (if any) is legal depends on `kind`: `database`→`database`, `mq`→`vhost`, `storage`→`bucket`, `search`→`index`; `cache` and `smtp` accept none of the four. Full detail, including the real generated validation error, is in [05-resource-binding.md](05-resource-binding.md)'s "Getting the binding slot wrong" section. |
| `resources[].bindings[].envPrefix` | string | no | must match `^[A-Z][A-Z0-9_]*$` — an uppercase letter, then uppercase letters/digits/underscores, because this string gets concatenated directly into an environment variable name (`{envPrefix}_DATABASE_HOST`) |

**A second, distinct collision the validator catches**: binding the same component to two resources of the same `kind` with no `envPrefix` (or the same `envPrefix`) on either produces the same set of environment variable names twice, the second binding silently overwriting the first's values at injection time. This is a hard error, not a warning — unlike a dangling binding, the failure mode here is "the component connects to the wrong database with no indication anything is wrong." [05-resource-binding.md](05-resource-binding.md)'s "Two resources of the same kind" section has the real generated error text and the fix.

## `installer`

| Field | Type | Required | Constraint |
| --- | --- | --- | --- |
| `installer.requireSignature` | `*bool` | no (default `true`) | |
| `installer.publicKeys` | `map[string]string` | no | `publicKeyRef` (as named in a component's signature) → local file path holding that public key |

⚠️ **`publicKeys` is the field that actually makes signature verification take effect** — with zero keys configured, verification is disabled entirely regardless of `requireSignature`, and the CLI only warns about this once, not on every run. AGENTS.md §7 and [06-signing-and-trust.md](06-signing-and-trust.md) have the full reasoning for why a public key has to live in the project rather than travel alongside the signature from the marketplace.

## Read further

- AGENTS.md §7 — the copy-pasteable skeleton this document expands
- AGENTS.md §5.4 — the full `enabled` cascade rule this document only states the field's shape for
- [04-environment-variables.md](04-environment-variables.md) — exactly what `config`, resource bindings, and `servedBy` turn into inside a container's environment
- [05-resource-binding.md](05-resource-binding.md) — binding-slot and same-kind-collision errors in full, plus the quota merge chain
- [02-dependency-resolution.md](02-dependency-resolution.md) — what the cascade and resolve stages actually do with `enabled`, required/optional dependencies, and diamond deduplication
- [07-component-yaml-reference.md](07-component-yaml-reference.md) — the component-side counterpart this file's `components[]`/`resources[]` entries bind against
- [06-signing-and-trust.md](06-signing-and-trust.md) — what `installer.publicKeys` actually gates and why
