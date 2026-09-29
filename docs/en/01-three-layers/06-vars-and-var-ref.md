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
```

A shared variable can't reference another shared variable (`$var:` doesn't chain):

```text
❌ Error: vars.yaml failed validation
   File: config/vars.yaml
   B: a shared variable cannot reference another one with $var: — only ${ENV_VAR} and file:// are allowed here
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

`$var:NAME` must be the **whole value**: `$var:PG_HOST:5432` is not valid. To build a value out of several pieces, use a
`${VAR}` template (`jdbc:postgresql://${PG_HOST}:5432/people`), which can sit inside a string — note that `${…}` looks
up the process environment and `.env`, not shared variables.

## Three ways to reference a value

| Written | Where the value comes from | When it's evaluated |
| --- | --- | --- |
| `$var:NAME` | The deploy file's `vars:`, then `config/vars.yaml` | When the CLI loads the project |
| `${NAME}`, `${NAME:-default}` | The process environment, then `.env` at the project root | On Docker, expanded by `docker compose` at start (the CLI checks at generation time that it's defined); on Kubernetes, by the CLI when it generates manifests |
| `file://path` | The file's content (the path is relative to the project root) | When the CLI generates deployment files |

The last two are mainly for secrets; see [Secrets](07-sensitive-values.md).

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
