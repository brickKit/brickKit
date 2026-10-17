# Migrations and shells

A shell member's code runs in the shell's process, but its **migration doesn't run in the shell**. The platform generates
the member's own migration container as usual:

- With **the member's own image**: every member must have its own image (`deployment.image` or `deployment.build`), even
  one that always runs inside a shell — unless the shell runs as a process on this machine with the member's code from
  source ([below](#when-the-shell-runs-as-a-process-on-this-machine)).
- With **the member's own config**: exactly the environment variables it would get running on its own.
- **The shell starts only once it finished successfully**: the moment the member's code is loaded, the table structure
  has to be in place.

`shop/stock@0.2.0` is in the shell `shop/shell@0.2.0`; what's generated is:

```yaml
  shop-shell-0-2-0:
    depends_on:
      shop-stock-0-1-0:
        condition: service_healthy
      shop-stock-0-2-0-migration:
        condition: service_completed_successfully
```

(The first entry is there because `shop/cart` in the shell depends on `shop/stock@0.1.0`, which runs on its own outside.)

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

The member's migration logs come before the shell's:

```text
shop-stock-0-1-0-migration-1  | shop/stock: migrations applied
shop-stock-0-2-0-migration-1  | shop/stock: migrations applied
shop-shell-0-2-0-1            | 2026/09/29 23:09:58 serving shop/cart@0.1.0 on :8081
shop-shell-0-2-0-1            | 2026/09/29 23:09:58 serving shop/stock@0.2.0 on :8082
```

What this split buys:

- **The shell needn't understand how each member migrates.** A member's migration command and the environment it
  migrates with are the member's own.
- **When one member's migration fails, the shell doesn't start, and the error names that member** — rather than the shell
  starting and some module then failing against a table that doesn't exist.
- **Moving a member out of the shell changes nothing.** Its migration already runs on its own.

When another version of the same member runs on its own outside the shell — as `shop/stock@0.1.0` does here — the two
versions' migrations are chained by version number as usual (see
[Migration order across versions](03-multi-version-chain.md)).

## When the shell runs as a process on this machine

With the shell as `mode: local` or `mode: debug` (or the focus of a focus run), there is no shell container to wait for,
and where a member's migration comes from follows where its code comes from:

| The member | The shell process runs | The member's migration |
| --- | --- | --- |
| Has a local repository (under `components/`, or in a local source) | The code in that working copy | From source: the member's `local.migrationCommand`. No image needed |
| Comes from Git or a market, with no local repository | The released version's code | Its migration container, with its own image, run once by `up` after the other containers |

The member's image isn't used for a member from source: it was built from that version at some point, and the working
copy compiled into the shell may have changed since — another image of the same name and version may even be on the
machine. A migration that doesn't match the code is worse than none, because nothing reports it.

A member from source migrates in its own directory, with the environment it gets inside the shell — the values of
`BRICKKIT_SERVED_MEMBERS_CONFIG`, addresses reachable from this machine — which `up` also writes to
`.brickkit/generated/local-debug.<member's service name>.env`:

- **Shell `mode: local`**: before starting the shell, `up` runs its members' migrations in start order, then the shell's
  own; each one's output is prefixed with `<service name>-migration`. One that fails ends the session with
  `MIGRATION_FAILED`, and the shell doesn't start.
- **Shell `mode: debug`**: you start the shell, so `up` runs nothing; its note names each member's command and env file.
- **A member without `local.migrationCommand`**: its migration doesn't run, and `up` says so, naming the shell.

See [When no migration container runs](01-migration-service.md#when-no-migration-container-runs) for the field and the
notes.
