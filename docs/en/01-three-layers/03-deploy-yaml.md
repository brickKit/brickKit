# deploy.yaml

A deploy file answers **how things run**: where to deploy, whether each component runs, how ports are opened, which
members a shell hosts. The team's shared one is `deploy.yaml`; the personal one is `deploy.local.yaml` (exactly the same
structure — see [the next page](04-deploy-local-yaml.md)); other environments can each have their own, such as
`deploy.prod.yaml`, chosen with `-f`.

## A complete example

```yaml
target: k8s                       # docker | podman | k8s

k8s:                              # project-wide settings that only apply with target: k8s
  namespace: shop-prod
  ingressClass: nginx
  networkPolicy:
    enabled: true

vars:                             # overrides shared variables of the same name in config/vars.yaml
  PG_HOST: pg.prod.internal

components:
  - id: erp/shell                 # a shell: its members nest under it
    members:
      - id: erp/api
      - id: people/basic
  - id: people/basic@0.9.0        # another version of the same component: this entry covers only that version, deployed on its own
  - id: portal/web
    expose: true
    hostname: shop.example.com    # on Kubernetes, expose generates an Ingress, which needs a host name
    replicas: 3
  - id: report/batch
    mode: disable                 # doesn't run in this environment
```

## Top-level fields

| Field | Meaning |
| --- | --- |
| `target` | Required. `docker`, `podman` or `k8s`: decides whether a `compose.yaml` or Kubernetes manifests are generated, and which engine is called |
| `k8s` | Kubernetes-only project settings: `context`, `namespace`, `createNamespace`, `podSecurity`, `imagePullSecrets`, `ingressClass`, `ingressAnnotations`, `networkPolicy`, `serviceAccount`. Written under another `target`, it only warns |
| `network` | Docker / Podman only. A network the project provides: when set, the project joins it instead of `brickkit-<project>-net`, which is then not created. For when things outside the project (a database you run, an IdP, a gateway) must share a network with the components; see [Generating the deploy files](../06-architecture/04-deploy-file-generation.md) |
| `vars` | Overrides shared variables of the same name in `config/vars.yaml`; affects only `$var:` references (see [Shared variables](06-vars-and-var-ref.md)) |
| `components` | The components' deployment entries |

## Component deployment entries

Each entry's `id` is either the bare ID (`erp/api`, meaning the component's **default version**) or `id@version`
(meaning only that version). Every component version in `brickkit.yaml` has exactly one entry in the deploy file —
both `lint` and `up` check this.

| Field | Meaning |
| --- | --- |
| `mode` | Whether it runs and how; see the table below. Without it, the component follows the ones above it |
| `localPort` | With `mode: local` / `mode: debug`, the port of the process on your machine; only allowed together with those two modes, or on the entry of the `focus` component in `deploy.local.yaml`, which runs as `mode: local` (see [Focus run](../02-project-guide/04-focus-run.md)) |
| `expose` | Open it to the outside: on Docker it maps a host port, on Kubernetes it generates an Ingress. Without it, the component isn't reachable from outside |
| `exposePort` | On Docker, which host port to map (defaults to the component's port); not used on Kubernetes |
| `hostname`, `tlsSecret` | On Kubernetes, the Ingress host name and the TLS certificate Secret |
| `replicas` | On Kubernetes, the number of replicas (default 1); above 1, a PodDisruptionBudget is generated |
| `resources` | Resource `requests` / `limits`, overriding the component's recommended values |
| `serviceAccountName` | On Kubernetes, use a ServiceAccount your operators already created |
| `labels` | Passed through as-is: container labels on Docker, Pod annotations on Kubernetes (for tools like Traefik or Prometheus to read) |
| `stopGracePeriodSeconds` | The stop grace period in seconds, overriding the one the component recommends in `component.yaml`: how long after the stop signal before it is killed |
| `skipWaitFor` | Don't wait for these required dependencies at start (only the wait goes; the connection stays). Docker / Podman only: Kubernetes has no start order. See [start cycles created by merging into a shell](../04-shell/04-members-management.md) |
| `members` | Only on a shell's entry: the members it hosts, each a full entry in its own right |

Fields that only mean something on Kubernetes (`hostname`, `tlsSecret`, `replicas`, `serviceAccountName`) only warn
under another `target` and the command goes ahead — the same entry can move between the two targets. The other way
round, the Docker / Podman-only fields `exposePort` and `skipWaitFor` only warn under `target: k8s`.

## `mode`: whether it runs, and how

| Value | Meaning | Written in |
| --- | --- | --- |
| nothing | Follow the ones above: a top-level component (nothing depends on it) runs by default; a component others depend on runs as long as one of them does | anywhere |
| `enabled` | Always runs; an error if one of its required dependencies is turned off | the team file or the personal file |
| `disable` | Never runs; components that **require** it stop too (an error if one of those is pinned to run), while components that depend on it optionally keep running without its address | the team file or the personal file |
| `local` | Always runs, but as a process on your machine: BrickKit works out the start command, launches it and watches it | the team file or the personal file |
| `debug` | Always runs, as a process you start yourself in your IDE; the platform makes sure other components find it | **only** the personal `deploy.local.yaml` |

`debug` can only go in the personal file because what it records is "I'm debugging this component on my machine right
now" — a fact about you and about this moment. It has no place in a file the team reviews, and nobody else should get
it from a `git pull`. In `deploy.yaml` it is rejected:

```text
❌ Error: deploy.yaml failed validation
   File: deploy.yaml
   components[0].mode: mode: debug can only be written in deploy.local.yaml: it records that you are debugging this component on your machine right now, which is not a team decision. Run brickkit local on and set it there
   Suggestion: Full field reference: brickkit docs 11-reference/03-deploy-yaml-schema (online: https://github.com/brickKit/brickKit/blob/v1.1.0/docs/en/11-reference/03-deploy-yaml-schema.md)
```

`local` and `debug` processes run on your machine, so they only make sense on the `docker` / `podman` targets; a Pod in
a cluster can't reach your laptop. How to debug with them: [Local debugging](../02-project-guide/03-local-debug-workflow.md).

## `members`: which components a shell hosts

A shell is a component that runs several components compiled into one process. **Which ones it hosts is written here
and only here**: members are full entries nested under the shell's entry, as `members`, one level deep. The
`kind: shell` on the shell's line in `brickkit.yaml` is only a marker, maintained by the CLI.

- While the shell runs: the members under `members` are all inside the shell's process and get no container of their
  own; fields like `replicas` and `resources` on a member's entry don't apply then. `expose` is the exception: the shell
  opens it for the member (see [Managing members](../04-shell/04-members-management.md)).
- When the shell doesn't run (`mode: disable`, or nothing needs it): members are deployed on their own, using the fields
  on their own entries.
- A member entry with a bare ID means the shell hosts the default version; to host another version, move that
  version's `id@version` entry under the shell.

How a shell declares which member versions it can host, and how members join and leave: [Shells](../04-shell/README.md).

## `vars`: shared variables per environment

A deploy file's `vars:` is written like `config/vars.yaml`, and wins when a name appears in both. The typical use is each
environment's deploy file carrying its own database address, while the component config always says `$var:PG_HOST`.
Values in `vars:` may be `${VAR}`. It only affects `$var:` lookups and never overrides a value written directly in a
component's config file — see [Where a value comes from](08-resolution-priority.md).

## `add` / `remove` / `upgrade` maintain it for you

| Operation | What changes in the deploy file |
| --- | --- |
| `brickkit add` | An entry for the new component (the bare ID; `id@version` for a non-default version brought in for a dependency); a shell's members are nested under it automatically |
| `brickkit remove` | The component's entry goes; when it's a shell, its members move back to the top level and run on their own (their fields kept as they were) |
| `brickkit upgrade` | When the default version changes, the entry follows; when a shell is upgraded, the member entries follow the versions the new shell compiles in |

`deploy.yaml` and `deploy.local.yaml` (when it exists) are updated together: `add` never leaves your personal file behind.

After each of these, `components` is sorted by component ID, the same order as `brickkit.yaml`: the bare-ID entry first, the
same component's `id@version` entries after it by version; the `members` under a shell are sorted among themselves. An
entry's fields and comments move with it, and the order changes nothing about what runs or how.
