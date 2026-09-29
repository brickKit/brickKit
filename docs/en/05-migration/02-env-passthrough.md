# Environment passed through

## The platform doesn't build connection strings

A migration connects to a database. The platform doesn't know whether you use PostgreSQL or MySQL, what the connection
string looks like, whether it needs SSL — and doesn't need to. **The migration container gets exactly the main service's
environment variables**: the same config, the same dependency addresses, the same secret file. The component takes the
host, port, database name, user and password from environment variables and builds the connection string itself.

## A complete example

The component declares which config it needs:

```yaml
# component.yaml
configSchema:
  type: object
  properties:
    DB_HOST:
      type: string
      description: PostgreSQL host name
    DB_PORT:
      type: integer
      default: 5432
      description: PostgreSQL port
    DB_NAME:
      type: string
      description: Database name
    DB_USER:
      type: string
      description: User to connect as
    DB_PASSWORD:
      type: string
      secret: true
      description: Password to connect with
  required: [DB_HOST, DB_NAME, DB_USER, DB_PASSWORD]

migration:
  command: ["/app/orders", "migrate"]
```

The project fills in values — when several components share one database, the address is written as a shared variable:

```yaml
# config/vars.yaml
PG_HOST: pg.internal
PG_PASSWORD: ${PG_PASSWORD}
```

```yaml
# config/shop-orders.yaml
DB_HOST: $var:PG_HOST
DB_NAME: orders
DB_USER: orders
DB_PASSWORD: $var:PG_PASSWORD
```

The migration container and the main service both get the same environment:

```yaml
  shop-orders-0-1-0-migration:
    command:
      - migrate
    entrypoint:
      - /app/orders
    env_file:
      - path: .brickkit/generated/env/shop-orders-0-1-0.env
    environment:
      - COMPONENT_ID=shop/orders
      - COMPONENT_VERSION=0.1.0
      - DB_HOST=pg.internal
      - DB_NAME=orders
      - DB_PORT=5432
      - DB_USER=orders
```

`DB_PASSWORD` is a secret: on Docker both reference the same env file with mode 0600 (it holds
`DB_PASSWORD="${PG_PASSWORD}"`, which `docker compose` fills in from the environment when it starts); on Kubernetes both
reference the same generated Secret. In the component's code:

```go
dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
	os.Getenv("DB_USER"), url.QueryEscape(os.Getenv("DB_PASSWORD")),
	os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_NAME"))
```

## Why it's split this way

- **The connection string's format is the component's business.** For the same PostgreSQL, Go's driver, JDBC and
  SQLAlchemy each want a different form; a platform building it for you could only build one of them.
- **One config, used in two places.** The migration and the main service read the same set of variables, so "the
  migration connected to database A and the service to database B" can't happen.
- **The database is prepared by the project.** The platform doesn't create databases: the database `DB_NAME` names has to
  exist first (created once by operations), and the tables are created by the migration.

Dependency addresses are passed through the same way: when a migration needs to call another component (registering with
some service before migrating, say), it gets the `*_ENDPOINT` variables too.
