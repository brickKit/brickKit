# component.yaml field reference

Every field of a component's Manifest: type, whether it's required, its default, the rules the validator really applies.
How to write each part well: [The component.yaml guide](../03-component-guide/02-component-yaml-reference.md).

Only the fields listed here may be written: an unknown field is an error on the spot, with a suggestion of what it might be
a misspelling of. There's no extension-field mechanism.

## Top level

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `apiVersion` | string | ✅ | Only `brickkit/v1` |
| `kind` | string | ✅ | Only `Component` |
| `tags` | list of strings | | Tags for searching; the platform doesn't interpret them |

## metadata

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `metadata.id` | string | ✅ | `<scope>/<name>`; each part lowercase letters, digits and hyphens, starting and ending with a letter or digit (`^[a-z0-9]([a-z0-9-]*[a-z0-9])?/[a-z0-9]([a-z0-9-]*[a-z0-9])?$`); at most 63 characters (the versioned service name must follow DNS label rules) |
| `metadata.name` | string | ✅ | A name for people |
| `metadata.version` | string | ✅ | An exact version `major.minor.patch` (`^\d+\.\d+\.\d+$`); no `v` prefix, ranges or `latest` |
| `metadata.description` | string | ✅ | A one-sentence description |
| `metadata.vendor` | string | | The publisher |
| `metadata.license` | string | | The licence |
| `metadata.apiDocs` | string | | The API documentation's address |
| `metadata.repository` | string | | The address of the component's repository or page; the component table in a project's `AGENTS.md` links to it |

## artifacts

Contracts and other files released with the component. Each item:

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `artifacts[].type` | string | ✅ | A free string the platform doesn't interpret (e.g. `api-contract`) |
| `artifacts[].files` | list of strings | ✅ | At least one; paths relative to the repository root, never absolute, never leaving the repository with `..` |
| `artifacts[].format` | string | | A free string (e.g. `openapi`, `protobuf`) |
| `artifacts[].description` | string | | A description for people |

## dependencies

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `dependencies.components[].id` | string | ✅ | `<component ID>@<exact version>`. The whole item can also be just this string (shorthand for a required dependency) |
| `dependencies.components[].optional` | boolean | | `true` makes it optional; required by default |

The two forms:

```yaml
dependencies:
  components:
    - demo/hello@1.1.0
    - id: demo/bus@1.0.0
      optional: true
```

Rules: versions must be exact; a component can't depend on itself; a component ID can appear only once (the address
variable's name carries no version, so two versions would collide on the same variable).

## configSchema

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `configSchema.type` | string | | If written, `object` |
| `configSchema.required` | list of strings | | Each must be declared under `properties` |
| `configSchema.properties.<key>.type` | string | ✅ | `string` / `integer` / `number` / `boolean` / `array` / `object` |
| `configSchema.properties.<key>.default` | any | | The default; lists and maps are encoded as one line of JSON when injected |
| `configSchema.properties.<key>.description` | string | | Appears at the end of the line in the project's config skeleton |
| `configSchema.properties.<key>.secret` | boolean | | `true` treats the item as a secret (a 0600 env file on Docker, a Secret on K8s) |
| `configSchema.properties.<key>.enum` | list | | Allowed values — **documentation only; the platform doesn't check values** |
| `configSchema.properties.<key>.minimum` | number | | As above |
| `configSchema.properties.<key>.maximum` | number | | As above |
| `configSchema.properties.<key>.pattern` | string | | As above |
| `configSchema.properties.<key>.items.type` | string | | The element type of an `array`; as above |

`<key>` is the environment variable name injected: it must be a valid environment variable name (letters, digits and
underscores, not starting with a digit), injected as it is, with no case conversion. When it collides with a name the
platform reserves (`COMPONENT_ID`, `COMPONENT_VERSION`, `PORT`, `BRICKKIT_SERVED_MEMBERS`,
`BRICKKIT_SERVED_MEMBERS_CONFIG`, `*_ENDPOINT`): `up` / `lint` warn and ignore the item, and a market refuses it. The
platform checks key names only, not values; details in [The configSchema spec](04-config-schema-spec.md).

## deployment

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `deployment.type` | string | ✅ | Only `container` |
| `deployment.image` | string | One of the two | A prebuilt image; without a tag, `metadata.version` is added |
| `deployment.build.context` | string | One of the two | The build context, relative to the repository root, `.` by default |
| `deployment.build.dockerfile` | string | | The Dockerfile path, relative to the repository root, `Dockerfile` by default |
| `deployment.port` | integer | ✅ | The main port, 1–65535 |
| `deployment.extraPorts[].name` | string | ✅ | Lowercase letters, digits and hyphens, at most 15 characters (the K8s Service port-name rule); no duplicates |
| `deployment.extraPorts[].port` | integer | ✅ | 1–65535, not the same as the main port |
| `deployment.resources.requests.cpu` | string | | The suggested CPU request, like `"100m"` |
| `deployment.resources.requests.memory` | string | | The suggested memory request, like `"128Mi"` |
| `deployment.resources.limits.cpu` | string | | The suggested CPU limit (better left out) |
| `deployment.resources.limits.memory` | string | | The suggested memory limit |
| `deployment.labels` | map of strings | | Passed through verbatim: service labels on Docker, Pod annotations on K8s; values must be strings |

At least one of `image` and `build`; the paths of `build` must be inside the repository. When `resources` is written,
`requests` or `limits` must hold at least `cpu` or `memory`; these values are the author's suggestions, and the project
overrides them field by field on its deploy-file entry. Only `requests` has a platform default (`100m` / `128Mi`); `limits`
has none.

## migration

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `migration.command` | list of strings | ✅ (when `migration` is written) | The migration command, as an array; run with the same image |

## healthCheck

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `healthCheck.type` | string | ✅ | `http` / `tcp` / `none` |
| `healthCheck.path` | string | Required for `http` | Starts with `/` |
| `healthCheck.startPeriodSeconds` | integer | | The start-up grace period in seconds, 60 by default, at most 3600; not allowed with `type: none` |

The check interval of 10 seconds, a 3-second timeout and 3 failures in a row meaning unhealthy are fixed by the platform and
can't be configured.

## shell

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `shell.members` | list of strings | ✅ (when `shell` is written) | The members compiled into the shell, each `<component ID>@<exact version>`; at least one; not itself; one version per component |

Writing `shell` makes the component a shell; its entry in the project's `brickkit.yaml` must carry `kind: shell`. See
[Declaring a shell](../04-shell/03-shell-declaration.md).

## local

How BrickKit starts the component when it runs as `mode: local`. Usually not needed: it's recognised automatically from the
marker files in the source directory.

| Field | Type | Required | Rules |
| --- | --- | --- | --- |
| `local.language` | string | | `go` / `rust` / `dotnet` / `node` / `java` / `python` / `ruby`; names the language when one directory holds marker files for several |
| `local.runCommand` | list of strings | | Gives the start command directly; when the first item contains a path separator, it's relative to the component directory |
