# brickkit.yaml field reference

Which components the project uses, each at which exact version, and where they come from. It's the project's lock file.
How to use it: [brickkit.yaml](../01-three-layers/02-brickkit-yaml.md).

An unknown field is an error on the spot. How things are deployed (`mode`, `expose`…) isn't written here but in the
deploy file.

## Top level

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `project` | string | ✅ | Lowercase letters, digits and hyphens, starting and ending with a letter or digit (`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`); at most 54 characters (a K8s namespace is at most 63, minus the `brickkit-` prefix the CLI adds). It becomes the name of the Docker network, the Compose project and the K8s namespace |

## sources

Install sources: where to look for components. Tried in the order declared; the first source that has the component wins.

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `sources[].name` | string | ✅ | The install source's name, unique in this file |
| `sources[].type` | string | ✅ | `git` / `local` / `market` |
| `sources[].baseUrl` | string | Required for `git` | The repository of component `<scope>/<name>` is `<baseUrl>/<scope>-<name>` (a trailing `/` on `baseUrl` is optional; the CLI adds exactly one); mustn't start with `-` |
| `sources[].path` | string | Required for `local` | A directory on this machine, relative to the project root, laid out as `<scope>/<name>/component.yaml` |
| `sources[].url` | string | Required for `market` | The component market's API address |
| `sources[].authToken` | string | | A market token, usually written `${VAR}`; after `brickkit login`, the login credentials take precedence |
| `sources[].enabled` | boolean | | `true` by default; with `false` the source takes no part in lookups |

## components

One entry per component version.

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `components[].id` | string | ✅ | The component ID, with the same rules as `metadata.id` in `component.yaml` |
| `components[].version` | string | ✅ | An exact version. Components from local sources also write the real version from their `component.yaml` |
| `components[].kind` | string | | Only `shell`, or left out; written by the CLI when it `add`s a shell, and must agree with the component's `shell` block; the same for every version of one component ID |
| `components[].requiredBy` | list of strings | | Which components this version is kept for (a compatibility version); each must be a component ID in this file, and not itself |
| `components[].source.type` | string | ✅ (when `source` is written) | `git` / `local` |
| `components[].source.repo` | string | Required for `git` | This component's repository address (when it can't be derived, or is in another organisation) |
| `components[].source.path` | string | Required for `local` | `local`: the component directory, relative to the project root; `git`: the component's subdirectory in the repository (a monorepo), never absolute, never leaving the repository |

Rules across versions:

- A component version appears only once.
- When a component ID has several versions, **exactly one** has no `requiredBy` — it's the default version; the rest all
  have `requiredBy`.
- A shell has only one version in a project, so it can't have `requiredBy` either.
- The lines of one component must write `source` the same way (a component has one repository).

## installer

Verification settings for component-market signatures. They apply only to components from a market.

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `installer.requireSignature` | boolean | | `true` by default: unsigned market components aren't installed |
| `installer.publicKeys` | map of strings | | Trusted publishers' public keys: a key name (the `publicKeyRef` in the signature) → a public key file path (relative to the project root). **With none configured, signature verification is switched off entirely** |

See [Security and signing](../06-architecture/08-security-and-signing.md).
