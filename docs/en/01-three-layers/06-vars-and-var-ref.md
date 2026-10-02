# Shared variables and explicit $var: references

When several components connect to the same database or the same message queue, the address shouldn't be copied into
each component's config — when it changes, one copy always gets missed. BrickKit's way: write the value once in
`config/vars.yaml`, and have every component that uses it reference it **explicitly** with `$var:`.

## config/vars.yaml

A flat key-value file; you choose the variable names (letters, digits, underscores, not starting with a digit):

```yaml
# config/vars.yaml
PG_HOST: pg.internal
PG_PORT: 5432
PG_PASSWORD: ${PG_PASSWORD}     # a value may be ${VAR}: the real value is in .env or the process environment
TLS_CA: file://.secrets/ca.pem   # or file://
IAM_URL: $endpoint:infra/iam     # or another component's address (see below)
```

A shared variable can't reference another shared variable (`$var:` doesn't chain):

```text
❌ Error: vars.yaml failed validation
   File: config/vars.yaml
   B: a shared variable cannot reference another one with $var: — only ${ENV_VAR}, file:// and $endpoint: are allowed here
```

One level is enough: with chains, finding out what a value actually is takes several hops — exactly the problem shared
variables exist to solve.

## Referencing it from a component's config

```yaml
# config/people-basic.yaml
DATABASE_HOST: $var:PG_HOST
DATABASE_PORT: $var:PG_PORT
DATABASE_PASSWORD: $var:PG_PASSWORD
DATABASE_NAME: people            # a value only this component uses: write it directly
```

`$var:NAME` must be the **whole value**: `$var:PG_HOST:5432` is not valid. When the whole string is shared, make it a
shared variable of its own (`PG_URL: jdbc:postgresql://pg.internal:5432/people` in `config/vars.yaml`) and write
`$var:PG_URL`. A `${VAR}` template (`jdbc:postgresql://${PG_HOST}:5432/people`) can sit inside a string — but note that
`${…}` looks up the process environment and `.env`, not shared variables, so `PG_HOST` has to be defined there.

## Four ways to reference a value

| Written | Where the value comes from | When it's evaluated |
| --- | --- | --- |
| `$var:NAME` | The deploy file's `vars:`, then `config/vars.yaml` | When the CLI loads the project |
| `${NAME}`, `${NAME:-default}` | The process environment, then `.env` at the project root | On Docker, expanded by `docker compose` at start (the CLI checks at generation time that it's defined); on Kubernetes, by the CLI when it generates manifests |
| `file://path` | The file's content (the path is relative to the project root) | When the CLI generates deployment files |
| `$endpoint:<scope>/<name>` | The address of another component of the project (see the next section) | When the CLI generates deployment files |

`${NAME:-default}` takes the default when the variable can't be found, so it never counts as undefined. The default is
plain text, and can't contain `$`, `{` or `}`. Something that starts like a reference but isn't one (`${A:-${B}}`,
`${1X}`, a missing `}`) is an error when the project is loaded, rather than reaching the container as literal text.

`${NAME}` and `file://` are mainly for secrets; see [Secrets](07-sensitive-values.md).

## Another component's address: `$endpoint:`

When a component depends on another, the platform works out the address and injects it as `*_ENDPOINT`. Sometimes,
though, a component **must not** declare a dependency on one particular component: it wants "the address of an identity
service", and which implementation is installed is the project's decision (see [slot
families](../09-patterns/01-component-design.md)). The component then declares an address item in its `configSchema`,
and the project fills it in. `$endpoint:` lets the project fill it in by component ID instead of writing an address:

```yaml
# config/vars.yaml — who fills the slot is written here and nowhere else
IAM_URL: $endpoint:infra/iam-casdoor
IAM_JWKS_URL: $endpoint:infra/iam-casdoor/.well-known/jwks.json
AUTHZ_URL: $endpoint:infra/authz
```

```yaml
# config/erp-sales.yaml
IAM_URL: $var:IAM_URL
AUTHZ_URL: $var:AUTHZ_URL
```

| Written | Gives |
| --- | --- |
| `$endpoint:infra/authz` | `http://infra-authz-2-0-1:8223` — the project's default version of it, main port |
| `$endpoint:infra/authz@2.0.0` | A given version (the project must have it) |
| `$endpoint:infra/authz:grpc` | The extra port named `grpc` |
| `$endpoint:infra/iam-casdoor/.well-known/jwks.json` | The address with a path after it. A component ID always has two parts, so the first `/` after the second part starts the path |

The value is worked out by the same rule as the dependency address `*_ENDPOINT`, so none of the trouble with hand-written
addresses comes with it:

- **It follows the version.** The address holds the versioned service name; after `brickkit upgrade` no address needs
  changing.
- **It follows how things run.** When the target is hosted by a shell, it points at the shell; when the referring
  component runs on this machine (`mode: local` / `debug`, a focus run), it becomes an address reachable from here; a
  component referring to itself gets its own address (to hand its callback URL to an outside system, say). The members
  of one shell all get exactly the same value.
- **The platform knows the edge.** The referenced component runs along with the components referring to it (it
  "follows the layer above", like an optional dependency), a focus run brings it along, K8s's `networkPolicy` lets the
  connection through, and `graph` / `deps` draw it.

It differs from a dependency in two ways, both on purpose:

- **No start order, and cycles are fine.** A reference creates no `depends_on`: two components referring to each other
  (an authorization service needs the identity service's keys, the identity service asks the authorization service at
  login) is a normal shape, and both sides should back off and retry while the other isn't there yet. Something that
  must be up first is a dependency, declared in `component.yaml`.
- **No `*_ENDPOINT` is injected.** The component gets only the config item it declared.

When the referenced component doesn't run this time (it says `mode: disable`, or nothing referring to it runs), the
value counts as not given: an optional item is left out and the component degrades on its own; a required item stops the
start with an error naming who doesn't run. A reference to a component the project doesn't have, or to a port name the
component doesn't declare, is an error in `up` and `lint` alike.

It isn't a dependency alias: the variable name is still the component's own, and what it points at is written in
`config/` or `vars.yaml`, where you can open the file and see.

## Fail loudly when it's not found

Referencing a variable that isn't defined anywhere stops the project from loading:

```text
❌ Error: config files reference shared variables that are defined nowhere
   Undefined reference: config/demo-hello.yaml: GREETING → $var:NOPE
   Suggestion: Define the variable in config/vars.yaml, or in the deploy file's vars:
```

`lint` reports the same.

## The deploy file's `vars:` per environment

`deploy.yaml`, `deploy.local.yaml` and `deploy.prod.yaml` can all have `vars:`; for the same name it wins over
`config/vars.yaml`:

```yaml
# deploy.prod.yaml
target: k8s
vars:
  PG_HOST: pg.prod.internal
components:
  - id: people/basic
```

Deploying with `-f deploy.prod.yaml`, every `$var:PG_HOST` gets `pg.prod.internal`; otherwise it gets the value in
`config/vars.yaml`. To use a local database on your machine, write `PG_HOST: localhost` under `vars:` in
`deploy.local.yaml`.

## Why not an implicit fallback

A common alternative is "a key missing from a component's config is looked up by name in a global file". BrickKit
deliberately doesn't do that:

- **What you see is what runs.** Open `config/people-basic.yaml` and you can tell at a glance where each value comes
  from; an implicit fallback makes you merge several files in your head.
- **Self-contained.** One component's config states every value it depends on; when a shared variable is deleted, every
  place referencing it fails loudly instead of quietly switching to some other value.
- **Friendly to AI.** An AI reading one config file knows where to look for each value, without guessing a set of
  lookup rules.

The cost is writing a few more `$var:`. When `add` generates a skeleton and finds a variable of the same name in
`config/vars.yaml`, it asks whether to reference it directly, which saves most of the typing.

## Resolution order

The full rule for which value a config item ends up with is in [Where a value comes from](08-resolution-priority.md).
Only one part concerns `$var:`: `$var:NAME` looks in the current deploy file's `vars:` first, then in
`config/vars.yaml`.
