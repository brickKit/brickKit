# Quick field reference

Every field you can write in the three files (plus a component's `component.yaml`), on one page. Each field's exact
type and validation rules are in the [Reference](../11-reference/README.md); for editor completion, use the JSON Schemas
in `schemas/` — see [JSON Schemas](../11-reference/05-json-schemas.md).

## brickkit.yaml

| Field | Required | Notes |
| --- | --- | --- |
| `project` | ✅ | Project name: lowercase letters, digits, hyphens |
| `sources[].name` | ✅ | The install source's name |
| `sources[].type` | ✅ | `git` / `local` / `market` |
| `sources[].baseUrl` | for git | Component `a/b`'s repository is `<baseUrl>a-b` |
| `sources[].path` | for local | A directory on this machine |
| `sources[].url` | for market | The component market's API address |
| `sources[].authToken` | | A market token (usually `${VAR}`, or use `brickkit login`) |
| `sources[].enabled` | | Default `true` |
| `components[].id` | ✅ | `scope/name` |
| `components[].version` | ✅ | An exact version |
| `components[].kind` | | `shell` (maintained by the CLI) |
| `components[].requiredBy` | | Which components this version is here for |
| `components[].source` | | A source for this one component: `type` (`git` / `local`), `repo`, `path` |
| `installer.requireSignature` | | Market components must be signed; default `true` |
| `installer.publicKeys` | | Trusted publisher keys: name → public key file path |

## deploy.yaml / deploy.local.yaml

| Field | Required | Notes |
| --- | --- | --- |
| `target` | ✅ | `docker` / `podman` / `k8s` |
| `focus` | | Only in `deploy.local.yaml`: run just this component (from source) and what it needs — see [Developing inside the project](../02-project-guide/04-focus-run.md) |
| `vars` | | Overrides shared variables of the same name in `config/vars.yaml` |
| `network` | | Docker / Podman only: the name of a network the project provides; the generated compose joins it and never creates or removes it |
| `k8s.context` / `k8s.namespace` / `k8s.createNamespace` | | Which cluster and namespace to deploy to |
| `k8s.podSecurity` | | `restricted`: generate for the Pod Security "restricted" level |
| `k8s.imagePullSecrets` | | Names of the Secrets used to pull images |
| `k8s.ingressClass` / `k8s.ingressAnnotations` | | The Ingress class and annotations |
| `k8s.appProtocols` | | Replaces the port protocol a component declares with the `appProtocol` this cluster reads |
| `k8s.networkPolicy.*` | | NetworkPolicies generated from the dependency graph: `enabled`, `ingressController`, `allowFrom[]`, `egress` |
| `k8s.serviceAccount.enabled` | | One ServiceAccount per component, with no token mounted |
| `components[].id` | ✅ | The bare ID (default version) or `id@version` |
| `components[].mode` | | `enabled` / `disable` / `local` / `debug` (`debug` only in `deploy.local.yaml`) |
| `components[].localPort` | | With `local` / `debug` (or on the `focus` component's entry), the port of the process on your machine |
| `components[].expose` / `exposePort` | | Open it to the outside; the host port on Docker |
| `components[].hostname` / `tlsSecret` | | The Kubernetes Ingress host name and certificate |
| `components[].paths` | | On Kubernetes, the path prefixes this component takes under its `hostname` (when several components share a domain); without it, the whole domain |
| `components[].replicas` | | Kubernetes replicas |
| `components[].resources` | | `cpu` and `memory` for `requests` / `limits` |
| `components[].stopGracePeriodSeconds` | | Overrides the stop grace period the component recommends (seconds) |
| `components[].serviceAccountName` | | Use an existing ServiceAccount on Kubernetes |
| `components[].labels` | | Labels passed through as-is |
| `components[].skipWaitFor` | | Required dependencies not to wait for at start |
| `components[].members[]` | | The members a shell hosts, each a full entry (the fields above, but no nested `members`) |

## config/*.yaml

No fixed fields: the keys are the config items declared in the component's `configSchema`, which are also the
environment variable names in the container. A value can be a literal, `$var:NAME`, a string with `${VAR}` in it,
`file://path`, or `{ existingSecret: name, key: key }`. In `config/vars.yaml` you choose the keys, and a value can't be
`$var:`.

## component.yaml

| Field | Required | Notes |
| --- | --- | --- |
| `apiVersion`, `kind` | ✅ | `brickkit/v1`, `Component` |
| `metadata.id` / `name` / `version` / `description` | ✅ | Component ID, name, exact version, description |
| `metadata.vendor` / `license` / `apiDocs` / `repository` | | Publisher, licence, API docs address, the repository or home page (the "Home" column of a project's component table) |
| `tags` | | Tags for search |
| `artifacts[]` | | Contracts: `type`, `format`, `description`, `files` |
| `dependencies.components[]` | | Dependencies: `id@version`; an optional one says `optional: true` |
| `configSchema` | | The config spec sheet: `type`, `default`, `description`, `secret`, `mount`, `enum`, `minimum`, `maximum`, `pattern`, `items` under `properties.<key>`; `required` |
| `deployment.type` | ✅ | Always `container` |
| `deployment.image` / `deployment.build` | one or both | The image to pull, or the `context` and `dockerfile` to build locally |
| `deployment.port` | ✅ | The main port |
| `deployment.protocol` | | What the main port speaks (`http` / `grpc` / `tcp`); on K8s it becomes `appProtocol` on the Service port |
| `deployment.extraPorts[]` | | Extra ports: `name`, `port`, an optional `protocol`. Each gives callers `<ID>_<NAME>_ENDPOINT`, the name uppercased with `-` turned into `_` (port `admin-api` of `people/basic` → `PEOPLE_BASIC_ADMIN_API_ENDPOINT`) |
| `deployment.resources` | | Recommended resources |
| `deployment.stopGracePeriodSeconds` | | Seconds the component needs to finish its work after a stop signal (1–3600); a deploy entry may override it |
| `deployment.labels` | | Labels passed through |
| `migration.command` | | The database migration command (an array) |
| `healthCheck.type` | ✅ | `http` / `tcp` / `none` |
| `healthCheck.path` | for `http` | The HTTP path, starting with `/` |
| `healthCheck.startPeriodSeconds` | | The startup grace period in seconds (default 60) |
| `readinessCheck.type` / `path` | | The readiness check (`http` / `tcp`), for a component that is alive but can't take traffic yet; without it, readiness follows `healthCheck` |
| `events.publishes` / `events.subscribes` | | Names of the events published and subscribed to (a subscription may be a prefix ending in `*`); only shown and hinted at by `graph` / `deps` / `lint` |
| `shell.members` | | The members a shell compiles in, as exact `id@version` |
| `local.language` / `local.runCommand` | | For `mode: local`, the language, or the start command given outright |

How to write it and design choices: the [component.yaml field guide](../03-component-guide/02-component-yaml-reference.md).

## Common mistakes

| Wrong | Why it doesn't work | Right |
| --- | --- | --- |
| `version: ^1.2.0` / `latest` | Only exact versions are accepted | `version: 1.2.0` |
| `version: local` | A local source also states its real version, or exact dependency matching fails | The version from `component.yaml` |
| `expose` or `mode` in `brickkit.yaml` | That's how things are deployed | On this component's entry in the deploy file |
| `mode: debug` in `deploy.yaml` | It's a personal fact, not for Git | In `deploy.local.yaml` |
| `localPort` without `mode` | `localPort` only means something for a process on your machine | With `mode: local` or `mode: debug` (the `focus` component's entry needs neither: it runs as `local`) |
| A config key `dbHost` while the component reads `DB_HOST` | The key is the environment variable name, injected as-is | Exactly the key in `configSchema` |
| `DATABASE_URL: $var:PG_HOST:5432` | `$var:` must be the whole value | Put the whole value into `config/vars.yaml` as a variable of its own (`PG_URL: jdbc:postgresql://pg.internal:5432/people`) and write `$var:PG_URL`, or reference the pieces separately. `${PG_HOST}` inside a string reads the process environment and `.env`, not `config/vars.yaml` |
| A secret in plain text | `config/` goes into Git | `${VAR}` or `file://` |
| Two entries for the same component version in a deploy file | Each version has exactly one entry | Delete the extra one |
| Shell members at the top level plus a separate list of member IDs | Membership has one source | Member entries nested under the shell's entry, as `members` |
