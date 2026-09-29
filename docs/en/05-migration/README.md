# Database migrations

A component is responsible for its own table structure: it declares a migration command, and before starting the
component the platform runs that command once, with the same image and the same config, starting the component only when
it succeeded.

The platform only handles **when it runs, in what order, and whom a failure stops**; what the migration does, which
database it connects to, how it builds the connection string — all of that is the component's own business. The platform
doesn't deploy databases and doesn't know about them: a database's address, user and password are ordinary config items of
the component (declared in `configSchema`, filled in under `config/`), injected like any other config. The database itself
is prepared by whoever uses the component (created once); the tables are created by the component's migration.

| Page | Covers |
| --- | --- |
| [01 The migration container](01-migration-service.md) | The one-shot migration container, how the main service waits for it, what happens when it fails |
| [02 Environment passed through](02-env-passthrough.md) | The migration gets exactly the main service's config; the component builds its own connection string |
| [03 Migration order across versions](03-multi-version-chain.md) | When two versions of one component share a database, their migrations run one after the other, by version |
| [04 Migrations and shells](04-shell-interaction.md) | A shell member's migration runs on its own, with the member's own image |
