# Deploy file field reference

`deploy.yaml`, `deploy.local.yaml` and `deploy.<environment>.yaml` share one structure. How to use them:
[deploy.yaml](../01-three-layers/03-deploy-yaml.md) and [deploy.local.yaml](../01-three-layers/04-deploy-local-yaml.md).

An unknown field is an error on the spot. Fields useful only for the other deploy target aren't errors but get a warning
that they have "no effect with target: … and are ignored" — they take effect when the target changes.

## Top level

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `target` | string | ✅ | `docker` / `podman` / `k8s` |
| `focus` | string | | Only in `deploy.local.yaml`: a component ID. Only that component and what it needs start, and it runs from its source (as `mode: local`, or as written `mode: debug`); not with `target: k8s`. See [Developing inside the project](../02-project-guide/04-focus-run.md) |
| `vars` | map | | Overrides same-named shared variables from `config/vars.yaml`, affecting only `$var:` lookups; keys must be valid environment variable names |

## k8s

Project-level Kubernetes settings. Written when `target` isn't `k8s`, they're warned about and ignored.

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `k8s.context` | string | | Which kubeconfig context to deploy to. When written, `up` (before really deploying), `down` and `status` first confirm that `kubectl` is currently connected to it; a current context that can't be read lets the command through |
| `k8s.namespace` | string | | `brickkit-<project name>` by default; the same rules as the project name |
| `k8s.createNamespace` | boolean | | `true` by default; write `false` when you only have namespace-level permissions |
| `k8s.podSecurity` | string | | Only `restricted`: generates a restricted-level `securityContext` for every container |
| `k8s.imagePullSecrets` | list of strings | | Names of Secrets for pulling images |
| `k8s.ingressClass` | string | | The Ingress class |
| `k8s.ingressAnnotations` | map of strings | | Written verbatim onto every Ingress |
| `k8s.serviceAccount.enabled` | boolean | | With `true`, one ServiceAccount per component, with no token mounted |
| `k8s.networkPolicy.enabled` | boolean | | With `true`, a NetworkPolicy is generated per component from the dependency graph |
| `k8s.networkPolicy.ingressController.namespace` | string | See the rules | The Ingress controller's namespace; required when network policies are on and some component has `expose: true` |
| `k8s.networkPolicy.ingressController.podSelector` | map of strings | | The labels of the Ingress controller's Pods |
| `k8s.networkPolicy.allowFrom[].name` | string | ✅ | Whom this is let through for (written into an annotation, so it's clear later why it was opened) |
| `k8s.networkPolicy.allowFrom[].namespace` | string | ✅ | Which namespace to let through |
| `k8s.networkPolicy.allowFrom[].podSelector` | map of strings | | Only let through Pods in that namespace carrying these labels |
| `k8s.networkPolicy.allowFrom[].ports` | list of integers | | Only let through these ports (1–65535) |
| `k8s.networkPolicy.egress.enabled` | boolean | | With `true`, outbound policies are generated too: dependencies in the dependency graph and DNS are let through automatically, everything else refused |
| `k8s.networkPolicy.egress.allowTo[].name` | string | ✅ | What this outbound target is (e.g. `main-db`) |
| `k8s.networkPolicy.egress.allowTo[].namespace` | string | One of the two | A target inside the cluster: its namespace |
| `k8s.networkPolicy.egress.allowTo[].cidr` | string | One of the two | A target outside the cluster: an address range; only one of this and `namespace` |
| `k8s.networkPolicy.egress.allowTo[].podSelector` | map of strings | | The labels of the target Pods inside the cluster |
| `k8s.networkPolicy.egress.allowTo[].ports` | list of integers | | Only let through these ports |

With outbound policies on, outside services like a database have to be written into `allowTo` yourself: the platform only
knows the components in the dependency graph, not a string of addresses in `config/`.

## components

Exactly one entry per component version, matching `brickkit.yaml` one to one (shell members' entries, nested under the
shell, count the same).

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `components[].id` | string | ✅ | A bare ID means the default version; `<component ID>@<version>` means that version (a compatibility version must be written this way) |
| `components[].mode` | string | | `enabled` / `disable` / `local` / `debug`. Left out, it follows the layer above: it runs when any component that depends on it runs (a top-level component runs by default). `debug` can only be written in `deploy.local.yaml`; `local` and `debug` aren't allowed with `target: k8s` |
| `components[].localPort` | integer | | The port the process on this machine listens on, 1–65535; only with `mode: local` / `mode: debug`, or on the entry of the `focus` component (which runs as `mode: local`); mustn't clash with another entry. Left out for `mode: local`, one is picked automatically |
| `components[].expose` | boolean | | Exposed to the outside: a host port mapped on Docker, an Ingress generated on K8s |
| `components[].exposePort` | integer | | The host port mapped on Docker, the component's port by default; only with `expose: true`; mustn't clash; ignored on K8s |
| `components[].hostname` | string | See the rules | The Ingress's domain name; required with `target: k8s` and `expose: true` |
| `components[].tlsSecret` | string | | The Ingress's TLS certificate Secret; only with `expose: true` |
| `components[].replicas` | integer | | The K8s replica count, 1 by default, at least 1; above 1 a PodDisruptionBudget is generated automatically; not together with `mode: local` / `debug`. To turn a component off, use `mode: disable` |
| `components[].serviceAccountName` | string | | On K8s, use an existing ServiceAccount (the platform only references it, never creates it) |
| `components[].resources.requests.cpu` | string | | Overrides the component's suggested quota, field by field |
| `components[].resources.requests.memory` | string | | As above |
| `components[].resources.limits.cpu` | string | | As above |
| `components[].resources.limits.memory` | string | | As above |
| `components[].labels` | map of strings | | Overrides the component's `deployment.labels` key by key, passed through verbatim |
| `components[].stopGracePeriodSeconds` | integer | | Overrides the stop grace period the component recommends in `deployment.stopGracePeriodSeconds`, 1–3600 seconds |
| `components[].skipWaitFor` | list of strings | | Required dependencies not waited for at start-up (component IDs without versions); must be real required dependencies of this component version; not itself, no duplicates. For Docker / Podman containers only; ignored on K8s and for processes on this machine |

Quota precedence: the deploy file's `resources` > the component's `deployment.resources` > the platform default (only
`requests`: `100m` / `128Mi`).

### Shell members: `components[].members`

A shell's entry can nest `members`; each member is a full deploy entry with the same fields as the table above, nested one
level only:

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `components[].members[].id` | string | ✅ | A bare ID or `id@version`; not the shell itself; one version per component in one shell |
| `components[].members[].mode` | string | | Like `components[].mode`; with `debug` / `local` the member runs as a process on this machine this time, and the shell doesn't host it |
| `components[].members[].localPort` | integer | | As above |
| `components[].members[].stopGracePeriodSeconds` | integer | | As above; no effect while a shell hosts the member (the shell uses its own), effective when the member falls back to a standalone component |
| `components[].members[].skipWaitFor` | list of strings | | As above |
| `components[].members[].expose` | boolean | | No effect while the shell hosts it; takes effect when the shell doesn't run |
| `components[].members[].exposePort` | integer | | As above |
| `components[].members[].hostname` | string | | As above |
| `components[].members[].tlsSecret` | string | | As above |
| `components[].members[].replicas` | integer | | As above |
| `components[].members[].serviceAccountName` | string | | As above |
| `components[].members[].labels` | map of strings | | As above |
| `components[].members[].resources.requests.cpu` | string | | As above |
| `components[].members[].resources.requests.memory` | string | | As above |
| `components[].members[].resources.limits.cpu` | string | | As above |
| `components[].members[].resources.limits.memory` | string | | As above |

Only a shell's entry (`kind: shell` in `brickkit.yaml`) can write `members`, and the members placed under it must be
components compiled into the shell's `component.yaml`. See [Managing members](../04-shell/04-members-management.md).

## Fields that apply to one target only

| Only on | Fields | On the other target |
| --- | --- | --- |
| k8s | The `k8s` block, `replicas`, `hostname`, `tlsSecret`, `serviceAccountName` | Warned about, ignored |
| docker, podman | `exposePort`, `skipWaitFor` | Warned about, ignored |
| docker, podman | `mode: local`, `mode: debug` | An error on `k8s` (a Pod in the cluster can't reach your machine) |
