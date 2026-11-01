# Migration order across versions

## The problem

When two versions of the same component run at once (see
[Several versions side by side](../03-component-guide/09-multi-version-coexistence.md)), they usually connect to the same
database. If both versions' migrations run at the same time, they change the structure of the same tables concurrently — at
best one side fails, at worst the schema ends up broken.

## Chained by version

The platform chains the migrations of the versions of one component **by version number**: the lower version's migration
finishes before the higher one's starts. When `shop/stock`'s 0.1.0 and 0.2.0 both declare a migration, what's generated is:

```yaml
  shop-stock-0-2-0-migration:
    command:
      - migrate
    depends_on:
      shop-stock-0-1-0-migration:
        condition: service_completed_successfully
    entrypoint:
      - /app/stock
    image: shop-stock:0.2.0
```

The two migration containers' logs, in order:

```text
shop-stock-0-1-0-migration-1  | shop/stock: migrations applied
shop-stock-0-2-0-migration-1  | shop/stock: migrations applied
```

The rules:

- Versions are compared as versions, not as names (`1.10.0` comes after `1.9.0`).
- A version that declares no migration is skipped, and the chain joins up across it.
- **Different components' migrations aren't chained**: their chains run in parallel — even two components sharing one
  database each look after their own tables.
- **A migration from source (`local.migrationCommand`) is outside the chain**: it runs after the containers. When a newer
  version of the same component migrates in a container in the same `up`, the older version's migration from source runs
  after it — no different from every later `up` (next section).

Why the lower version goes first: on an empty database, the old version's migration builds the structure it needs, and the
new version's migration carries on from there. That decides what the database looks like on the first `up` only.

## The older version's migration meets a newer database

Every version's migration runs again on every `up` (which is why a migration must be idempotent, see
[When a migration fails](01-migration-service.md#when-a-migration-fails)). So from the second `up` on, the chain runs the
older version's migration against a database the newer version has already migrated. The same happens on the first `up`
when an older version joins a project whose database a newer one already migrated (a component that depends on the older
version is added), and when the older version migrates from source after a newer one's container. No order prevents it.

So, **when versions run side by side, a component's migration must treat a database already migrated by a newer version of
the same component as done**: change nothing and exit 0. Otherwise `up` stops at the older version's migration
(`MIGRATION_FAILED`), and the migrations after it in the chain, and the services waiting for them, don't start.

What that takes depends on how the migration records its state:

- **It records each step it has applied, and applies only its own steps not yet recorded**: the steps a newer version
  added are rows it simply doesn't look at. Nothing more to do.
- **It records one current version**, as many migration tools do: some of them refuse a database at a version they have no
  file for (golang-migrate: `no migration found for version N`). Before migrating, compare the database's version with
  the newest one the component ships; when the database's is newer, say so in the log and exit 0.

## Data compatibility is the component author's business

The platform only guarantees that two versions' migrations never change the structure at the same time. Whether the two
versions are compatible while both read and write the same tables — does the old version accept the columns the new one
added, can the new version read what the old one wrote — the platform has no way to know; only the component's author can
design for it. It usually means migrations only "add" (add columns, add tables), never delete or change structure the old
version still uses, and clean up once the old version has retired.
