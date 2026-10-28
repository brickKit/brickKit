# The migration container

## Declaring it

```yaml
# component.yaml
migration:
  command: ["/app/stock", "migrate"]
```

The command is an array: the very command to run in the container. It uses the component's **own image**: the migration
scripts and the component's code live in the same repository and the same image, and are always the same version — the
table structure the code needs is exactly what this version's migration builds. Being a command inside the image, it
can't run as written when the component runs as a process from source; that is what `local.migrationCommand` is for (see
[When no migration container runs](#when-no-migration-container-runs)).

The entry program has to tell "run the migration" from "start the service" by its argument, and **must fail at once on an
argument it doesn't know**:

```go
if len(os.Args) > 1 {
	if os.Args[1] != "migrate" {
		log.Fatalf("unknown argument %q", os.Args[1]) // fail at once on an argument it does not know
	}
	fmt.Println("shop/stock: migrations applied")
	return
}
```

Otherwise a misspelled argument turns the migration container into a second service that keeps running: it never
finishes, the main service waits for it forever, and the logs look perfectly normal.

## What the platform generates

On Docker, every component that declares a migration gets one more, one-shot service:

```yaml
  shop-stock-0-2-0-migration:
    command:
      - migrate
    depends_on:
      shop-stock-0-1-0-migration:
        condition: service_completed_successfully
    entrypoint:
      - /app/stock
    environment:
      - COMPONENT_ID=shop/stock
      - COMPONENT_VERSION=0.2.0
      - PORT=8082
      - STOCK_WAREHOUSE=Main warehouse
    image: shop-stock:0.2.0
    networks:
      - brickkit-net
    restart: "no"
```

(Its `depends_on` is there because this project also runs `shop/stock@0.1.0`: see
[Migration order across versions](03-multi-version-chain.md).)

The main service **waits for it to finish successfully**, not for it to "be up":

```yaml
  shop-stock-0-1-0:
    depends_on:
      shop-stock-0-1-0-migration:
        condition: service_completed_successfully
```

On Kubernetes it's a Job: `up` first deletes the same-named Job left over from last time, then creates it and waits for
it to complete, and only then deploys the main service. The Job doesn't retry (`backoffLimit: 0`), and it runs once only,
however many replicas the main service has (with a Pod InitContainer, three replicas would run the same migration three
times at once). On Docker / Podman likewise: there's one migration container, and every replica of the main service waits
for it to succeed.

`up` tells you beforehand which ones it will run:

```text
🔧 Database migrations that run before startup (on failure that component won't start):
   shop/stock@0.1.0  /app/stock migrate
   shop/stock@0.2.0  /app/stock migrate
```

## When a migration fails

When the migration exits non-zero, the main service doesn't start:

```text
❌ Error: database migration failed
   Component: shop/stock@0.2.0
   View logs: docker compose -p brickkit-shop logs shop-stock-0-2-0-migration
   Suggestions:
   1. When a migration fails the main service does not start: it waits for the migration to finish successfully
   2. Fix it and run brickkit up again: the migration container runs once more
```

```text
shop-stock-0-2-0-migration-1  | shop/stock: migration 002 failed: column "warehouse" already exists
```

Fix it and `up` again: the migration container runs on every `up`. So **a migration must be idempotent** — running a
change that was already made again does nothing. The usual way is a migration-record table noting which steps have run.
When several components share one database, that table's primary key has to include a component identifier, or their
records overwrite each other.

## When no migration container runs

**The rule: a migration runs where the code runs.** Code from an image migrates with that image's
`migration.command`; code from source migrates from source. Two kinds of component run from source:

- a component running as a process on this machine: `mode: local`, `mode: debug`, or the focus component of a focus run;
- a shell member whose code a shell running as such a process compiles in from the member's local repository (see
  [Migrations and shells](04-shell-interaction.md#when-the-shell-runs-as-a-process-on-this-machine)).

Neither has a migration container. Nor can its `migration.command` simply run on your machine instead: it runs inside the
component's image (`/app/stock migrate` is a path in the image), and the source you run may hold newer migrations than
the image — the image may even hold other code of the same version. The migration has to run from source, and only the
component knows how; the platform doesn't guess it. So the component declares it in `local.migrationCommand`, written
like `local.runCommand` (an argv; a program with a path separator is relative to the component's directory). It requires
a `migration`, and `brickkit lint` warns when a component that ships its code (`deployment.build`) has a `migration`
without it:

```yaml
migration:
  command: ["/app/stock", "migrate"]               # inside the image
local:
  migrationCommand: ["go", "run", ".", "migrate"]  # from source
```

**`mode: local`** (for a member: its shell is `mode: local`). `up` runs it in the component's directory, after the
containers have started and before the process starts — a shell's members' first, in start order, then the shell's own.
It gets the same environment variables as the process; a member gets the ones it receives inside the shell, which `up`
also writes to `local-debug.<member's service name>.env`. Its output lines are prefixed with `<service name>-migration`.
If it fails, the process isn't started and the session ends with `MIGRATION_FAILED` — the same rule as a migration
container holding back its main service. `up --dry-run` lists it among the migrations that run before startup. It runs
on every `up`, like the container, so it has to be idempotent too.

**`mode: debug`.** You start the process yourself, so `up` runs nothing and reminds you instead, naming the command and
the environment file (for a member, the note also names its shell):

```text
⚠️ Note: a mode: debug component's database migration won't run automatically
   Component: shop/stock@0.2.0
   Reason: A mode: debug component generates no container, so its migration container is skipped too
   Suggestions:
   1. Run its migration from source by hand, in its source directory: go run . migrate
   2. Use the environment variables from local-debug.shop-stock-0-2-0.env
```

In the component's directory: `set -a && source <project>/.brickkit/generated/local-debug.<service name>.env && set +a`,
then the command.

**Without `local.migrationCommand`**, the migration doesn't run in either mode — the image isn't used instead — and `up`
says why it has no command to give you:

```text
⚠️ Note: a mode: local component's database migration won't run automatically
   Component: shop/stock@0.2.0
   Reason: A mode: local component generates no container, so its migration container is skipped too
   Suggestions:
   1. Its migration.command (/app/stock migrate) runs inside its image and can't be run on this machine as written
   2. To run it from source, the component declares local.migrationCommand in component.yaml (see its BRICKKIT.md); with mode: local, up then runs it before starting the process
```

How to run it from source is then in the component's `BRICKKIT.md`; if the component is yours, declare
`local.migrationCommand`.

**Its place in the order.** A migration from source runs after the engine has run the containers, so it isn't part of
the [version chain](03-multi-version-chain.md). When a **newer** version of the same component migrates in a container
this time, the two would run newest first; `up` stops before anything starts (`CONFIG_INVALID`), naming both versions —
don't run the newer one this time, or run this one from its image. An older version in a container is fine: it runs
first.
