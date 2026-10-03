# The migration container

## Declaring it

```yaml
# component.yaml
migration:
  command: ["/app/stock", "migrate"]
```

The command is an array: the very command to run in the container. It uses the component's **own image**: the migration
scripts and the component's code live in the same repository and the same image, and are always the same version — the
table structure the code needs is exactly what this version's migration builds.

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
times at once).

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

A component running as a process on this machine (`mode: debug`, `mode: local`) has no container, so its migration
container is skipped too, and `up` reminds you:

```text
⚠️ Note: a mode: debug component's database migration won't run automatically
   Component: shop/stock@0.2.0
   Reason: A mode: debug component generates no container, so its migration container is skipped too
   Suggestions:
   1. Run the component's migration command by hand on this machine: /app/stock migrate
   2. Use the environment variables from local-debug.shop-stock-0-2-0.env
```

Run the migration yourself once then: with the environment variables from `local-debug.<service name>.env`, run the
component's migration command on this machine. A `mode: local` component gets that file too: `up` starts the process
itself and doesn't read the file, but it holds the same variables, ready for a command like this.
