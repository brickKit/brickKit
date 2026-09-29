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

Why the lower version goes first: the old version's migration builds the structure it needs, and the new version's
migration carries on from there. The other way round, the old version's migration might fail against a structure the new
version has already changed.

## Data compatibility is the component author's business

The platform only guarantees that two versions' migrations never change the structure at the same time. Whether the two
versions are compatible while both read and write the same tables — does the old version accept the columns the new one
added, can the new version read what the old one wrote — the platform has no way to know; only the component's author can
design for it. It usually means migrations only "add" (add columns, add tables), never delete or change structure the old
version still uses, and clean up once the old version has retired.
