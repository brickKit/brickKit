# brickkit.yaml

`brickkit.yaml` answers one question: **what is this project made of**. It holds install sources and the exact versions
of components, and nothing else — how things are deployed is in the deploy file, configuration is in `config/`.

## A complete example

```yaml
project: my-shop                  # project name: also the name of the Docker network and the Kubernetes namespace

sources:                          # where to look for components, tried in order
  - name: org
    type: git
    baseUrl: https://git.example.com/components/   # erp/backend → https://git.example.com/components/erp-backend
  - name: local-dev
    type: local
    path: ./components            # a directory on this machine: <scope>/<name>/component.yaml

components:
  - id: erp/backend
    version: 1.2.0
  - id: people/basic
    version: 1.0.0
  - id: people/basic              # another version of the same component: here only because erp/legacy needs it
    version: 0.9.0
    requiredBy: [erp/legacy]
  - id: erp/shell
    version: 1.0.0
    kind: shell                   # a shell, maintained by the CLI
  - id: vendor/billing
    version: 3.1.0
    source:                       # this one component names its repository instead of following the sources
      type: git
      repo: https://github.com/vendor/billing-component.git

installer:
  requireSignature: true          # market components must be signed (true is the default)
```

## The project name, `project`

Required. Lowercase letters, digits and hyphens only, starting and ending with a letter or digit — it becomes the name
of the Docker network (`brickkit-<project>-net`) and of the Kubernetes namespace.

## Component entries, `components[]`

| Field | Required | Meaning |
| --- | --- | --- |
| `id` | ✅ | The component ID, `scope/name` |
| `version` | ✅ | An exact version `major.minor.patch`; a local-source component also states the real version from its `component.yaml` |
| `kind` | | Only one value, `shell`, marking a shell; written by `add` from the component's `component.yaml`, and `lint` checks it agrees with the manifest |
| `requiredBy` | | This version is in the project only because these components depend on it (see "The default version" below) |
| `source` | | A source for this one component instead of the install sources: `type` (`git` / `local`) plus `repo` (a Git address) or `path` (a local directory, relative to the project root) |

### Exact versions: it is the lock file

Only exact versions are accepted; `^1.0.0`, `~1.2` and `latest` are all rejected. The reason is the same as for
`package-lock.json`: a range lets "it runs here" and "it runs in production" install different things.

Beyond that, `brickkit.yaml` **is** the lock: every component version the project uses must be written here. When a
component depends on one that isn't, `up` stops and tells you to add it with `add`; an **optional** dependency that
isn't written here simply doesn't exist — its address variable is not injected and the component takes its own
degraded path. The platform never quietly adds a component for you.

### The default version and `requiredBy`

A component can be present in several versions at once (two versions are two different service names, with no clash).
**Exactly one line has no `requiredBy`**; that line is the component's **default version**:

- an entry in a deploy file that uses the bare ID (`- id: people/basic`) means the default version;
- the config file without a version, `config/people-basic.yaml`, belongs to the default version;
- the source in a local source is the default version's code.

Every other version carries `requiredBy: [<the IDs of the components that depend on it>]`, so you can see at a glance
why it's still there. The CLI maintains all of this: `add` writes `requiredBy` when it brings in another version to
satisfy a dependency; `upgrade` adjusts it when the default version changes; versions nobody depends on any more are
cleared by `upgrade` / `remove`.

## Install sources, `sources[]`

| Type | What you write | Where components come from |
| --- | --- | --- |
| `git` | `baseUrl` | The repository of `erp/backend` is `<baseUrl>erp-backend`; a version is a Git tag in that repository |
| `local` | `path` | A directory on this machine, laid out as `<scope>/<name>/component.yaml`; images are built from here by `brickkit build` |
| `market` | `url` (optionally `authToken`) | A component market's API address; optional infrastructure |

Every source has a required `name` and can be switched off for a while with `enabled: false`. To find a component the
sources are tried in the order they're declared, and the first one that has it wins. A component whose repository
name can't be derived (another organisation, a name that doesn't match) names its own source with `source` on its
entry. The details of Git sources (the cache, tag rules, authentication) are in
[Distributing through Git](../03-component-guide/10-git-distribution.md).

## `installer`

Only about signatures on market components: `requireSignature` (default `true`) and `publicKeys` (the publisher keys
you trust, name → public key file path). With no public key configured, signature checking doesn't actually happen,
and the CLI says so once. See [Security and signing](../06-architecture/08-security-and-signing.md).

## How `brickkit add` settles on a version

| You type | Result |
| --- | --- |
| `brickkit add erp/backend` | The latest version in the install source (for a Git source the highest version tag, for a local source the version in its `component.yaml`), written as an exact version |
| `brickkit add erp/backend@1.2.0` | Exactly that version |
| Its dependencies | Added at the exact versions its `component.yaml` names; when a dependency wants a different version than the project's current default, it gets a line of its own with `requiredBy` |

For a component that already has a default version, `add` of another version is refused with a pointer to
`brickkit upgrade`: changing the default version means migrating its config too, and that is `upgrade`'s job.

## Fields that moved out of the old single file

The old `brickkit.yaml` held everything. With three layers, these fields moved:

| Old field | Where it lives now |
| --- | --- |
| `deploy.target`, `deploy.namespace` and the like | The deploy file's `target` and its `k8s:` block |
| A component's `mode`, `localPort`, `expose`, `replicas`, `resources`, `labels` | That component's entry in the deploy file |
| A component's `config` | `config/<component>.yaml` |
| `resources` (database and Redis connections and bindings) | Gone: connection details are ordinary config items of the component, written in `config/` |
| `servedBy` | Gone: shell members are listed under the shell's entry in the deploy file, as `members` |
| `override.yaml` | `deploy.local.yaml` + `brickkit local` |
