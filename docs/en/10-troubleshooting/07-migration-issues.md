# Migration problems

When a component declares `migration.command`, the platform runs the migration once with the component's own image before
the main service starts: a one-shot container on Docker, a Job on Kubernetes. When the migration fails, the main service
doesn't start. Background in [The migration container](../05-migration/01-migration-service.md).

## A migration fails

**Symptom**

```text
❌ Error: database migration failed
   Component: shop/stock@0.2.0
   View logs: docker compose -p brickkit-shop logs shop-stock-0-2-0-migration
   Suggestions:
   1. When a migration fails the main service does not start: it waits for the migration to finish successfully
   2. Fix it and run brickkit up again: the migration container runs once more
```

**Cause**

The migration command ended with a non-zero exit code. Why is in the migration's own logs: it can't connect to the
database, the SQL is wrong, the database doesn't exist, the user has no permission to create tables.

**Fix**

1. Run the "View logs" line from the error, and read what the migration itself says.
2. Fix it (code, config, database permissions) and `brickkit up` again. The migration container runs again on every `up`.

So **a migration must be idempotent**: running a change that was already made again does nothing. The usual way is a
migration-record table noting which steps have run.

## A migration never finishes

**Symptom**

- Docker: `up` never returns; the migration container is `running`, the main service sits at `Created`.
- Kubernetes: after waiting ten minutes, `up` reports `Error: database migration failed`, listing the two commands
  `kubectl describe job/…` and `kubectl logs job/…` below it.

**Cause**

- **The entry program took the migration argument for "start the service".** The migration container and the main
  service are the same image, differing only in the argument; when the entry program meets an argument it doesn't know
  and starts the service instead of exiting with an error, the migration container never finishes. Its logs even say "the
  service is ready". See [up / down problems](01-up-down-issues.md#up-hangs-and-the-migration-container-keeps-running).
- **The migration is waiting for a lock.** Another process (an earlier migration that didn't exit cleanly, a database
  session left open by hand) holds a table lock or the migration tool's own lock.
- **The migration really is slow.** Adding an index to a big table, backfilling data. On Docker `up` keeps waiting; on
  Kubernetes `up` waits at most ten minutes and treats anything longer as a failure.
- **On Kubernetes the Pod was never created.** The Job was submitted, but admission control (PodSecurity, ResourceQuota,
  LimitRange) refused its Pod, and the logs are empty. The error says so on purpose: look at the events first, then the
  logs.

**Fix**

- Read the migration's logs first, to see whether it's working, waiting for a lock, or actually started as a service.
- Waiting for a lock: find the session holding it and end it.
- Too slow: take the time-consuming data changes off the start-up path — keep structural changes in the migration, and
  move large backfills to the background after the component starts, or into a separate operations step.
- Empty logs on Kubernetes: `kubectl describe job/<name> -n <namespace>`, and see in the events who refused the Pod.

## The component being debugged reports "table doesn't exist"

**Symptom**

A component running on this machine with `mode: debug` or `mode: local` reports a missing table as soon as it starts.
`up`'s output has:

```text
⚠️ Note: a mode: debug component's database migration won't run automatically
```

**Cause and fix**

A component running as a process on this machine has no container, so its migration container is skipped along with it.
Run it once yourself: load the variables from `local-debug.<service name>.env`, and run the component's migration command
on this machine. See [Local debugging problems](02-local-debug-issues.md#the-process-on-this-machine-reports-relation-does-not-exist).

## Two versions of one component, migrations in the wrong order

**Symptom**

Two versions of one component are in the project at once (the default version, and a compatibility version kept because
another component still depends on it), and one version's migration fails over the table structure: a column already
exists, a column doesn't exist, a constraint conflicts.

**Cause**

The two versions usually connect to the same database. The platform chains their migrations **by version number**: the
lower version's finishes before the higher version's starts, and they never change the structure at the same time (see
[Migration order across versions](../05-migration/03-multi-version-chain.md)). But the order only governs "who runs first
this time", not what the database already looks like:

- The new version's migration already ran on some earlier `up`, and this time the lower version's migration runs again —
  facing a structure the new version has changed.
- Both versions' migrations believe a table is theirs, and change each other's columns.

**Fix**

This is the component author's business; the platform has no way to judge whether two versions' structures are
compatible:

- Migrations only "add": add columns and tables, never delete or change structure the old version still uses; clean up
  once the old version has retired.
- Every migration step is idempotent: check whether it was already done before doing it, rather than assuming the database
  is empty or stopped at the previous version.
- The migration-record table records "steps", not "version numbers": when the lower version runs again, each of its steps
  is already in the table, and nothing happens.

## Several components share a database, and their migrations overwrite each other

**Symptom**

Two components connect to the same database. After one component's migration runs, the other's "thinks it already ran"
and skips, or the other way round re-runs steps already done.

**Cause**

Both components use the same migration-record table (many migration tools share a default table name), and the table's
primary key is only the step number, with no component identifier — the two sides' records overwrite each other. The
platform doesn't chain different components' migrations: their chains run in parallel, each looking after its own tables.

**Fix**

Include a component identifier in the migration-record table's primary key, or give each component a record table of its
own name, its own schema. More thorough still is one database per component (the database name being one of the
component's config items), the most direct guarantee that components stay within their own bounds.

## The connection string is put together wrong

**Symptom**

The migration (or the component itself) can't connect to the database, with an address or authentication error, while the
database itself is fine.

**Causes and fixes**, the common ones:

**`$var:` can only be the whole value.** Trying to splice a shared variable into a string:

```yaml
# config/demo-caller.yaml
DATABASE_HOST: $var:PG_HOST:5432
```

```text
❌ Error: demo-caller.yaml failed validation
   File: config/demo-caller.yaml
   DATABASE_HOST: "$var:PG_HOST:5432" is not a valid $var: reference; write $var:NAME, where NAME uses letters, digits and underscores
   Suggestion: How config/ files are written: docs/en/01-three-layers/05-config-directory.md (swap en for zh for the Chinese version)
```

`$var:` has to be the whole value; it can't be embedded in a string.

**`${…}` refers to an environment variable, not a shared variable.** Writing
`postgres://app@${PG_HOST}:5432/shop` while `PG_HOST` is defined only in `config/vars.yaml` — `${PG_HOST}` looks in the
process environment and `.env`, doesn't find it, and generation stops:

```text
❌ Error: environment variables referenced in config/ or the deploy file are not defined
   Missing variables: PG_HOST
   Reason: docker compose would replace them with an empty string at start, warning only in its own output: the component would get a broken value and no error at all
   Suggestions:
   1. Add these variables to .env in the project root, or export them in the current shell
   2. You can also write a default: ${POSTGRES_PASSWORD:-dev}
```

**The most robust way: don't make the project put together connection strings.** The component splits host, port,
database name, user and password into separate items in `configSchema`, and assembles them in its own code:

```yaml
# config/demo-caller.yaml
DB_HOST: $var:PG_HOST
DB_PORT: $var:PG_PORT
DB_NAME: shop
DB_USER: app
DB_PASSWORD: ${PG_PASSWORD}
```

Then each item can reference a shared variable or an environment variable on its own, and the password can be marked a
secret on its own. There's one more benefit: a password holding characters like `@`, `:` or `/` breaks a URL it's spliced
into and needs URL-encoding; passed separately to the database driver, it doesn't. How to design config items:
[Designing configSchema](../03-component-guide/03-config-schema-design.md).

**The migration and the main service get the same environment variables.** When the main service connects and the
migration doesn't, it's most likely that the migration code reads a different variable name from the main service, not that
the platform gave a different value. Compare the two services in `.brickkit/generated/compose.yaml`: the migration
service (`<service name>-migration`) has the same `environment` as the main service and loads the same `env_file`,
`.brickkit/generated/env/<service name>.env`, which holds the secret items (a `DB_PASSWORD` marked `secret: true`) and
`file://` contents (they never go into `compose.yaml`).
