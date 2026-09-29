# Designing a configSchema

`configSchema` is the component's **config spec sheet** for its users: which environment variables the component
recognises, what each means, what the default is, and which ones the project has to supply. Users fill in values in
`config/<component>.yaml` following it (`add` generates the skeleton from it), and the platform injects the values as
environment variables following it.

## How the platform treats it

**The platform doesn't understand what config means.** Whether `DB_HOST` is a database or a cache address, whether
`TIMEOUT` is seconds or milliseconds — the platform doesn't know and doesn't guess; that's the component's business. It
does two things only: inject each key as an environment variable, as-is; and check the **key names** users write against
the spec sheet.

**Key names are checked, values aren't.** That's a line drawn on purpose:

- When a user misspells a key (`QUOTE_PREFX`), **nothing at all happens at runtime** — the variable simply doesn't exist,
  the component takes its own default path, everything looks fine, and the setting just doesn't apply. Only the platform
  can notice this, so the platform has to say it:

  ```text
  ⚠️ demo/quote@0.1.0: QUOTE_PREFX is not declared in the component's configSchema, so it has no effect
     File: config/demo-quote.yaml
     Declared keys: QUOTE_PREFIX
     Suggestion: Did you mean QUOTE_PREFIX?
  ```

- When a user writes a wrong value (a port as `"abc"`), the component **fails loudly** when it parses it at start — the
  component can notice by itself, and what to do with a bad value (fail, degrade, fall back to a default) is its decision.
  Once the platform starts checking values it has to go all the way: types, `enum`, `minimum`, `pattern`… JSON Schema's
  capabilities have no end, and every extra check is one more decision the platform makes for the component.

So `enum`, `minimum`, `maximum`, `pattern` and `items` can and should be written, but they're **documentation for people**,
not a security gate.

The one exception is **required items**: a key in `required` with no `default` makes `up` refuse to start when users
leave it out, naming the missing item. That kind of mistake is also "invisible at runtime" — an unconfigured upstream
address doesn't crash the component; it just leaves one call path that never works.

## Naming

- **The key is the environment variable name, injected as-is.** Use upper case with underscores (`DB_HOST`), no camelCase
  conversion — the name the component's code reads is the one you see here.
- **Don't collide with the platform's reserved names.** The platform injects `COMPONENT_ID`, `COMPONENT_VERSION`, `PORT`,
  `BRICKKIT_SERVED_MEMBERS`, `BRICKKIT_SERVED_MEMBERS_CONFIG`, and every dependency address ending in `_ENDPOINT`. On a
  collision your config item is ignored, the platform's value wins, and `lint` warns:

  ```text
  ⚠️ Config conflict: the config item of component demo/widget was ignored
     Component: demo/widget
     Config item: UPSTREAM_ENDPOINT
     Conflicting reserved pattern: *_ENDPOINT
     Handling: This config item is ignored; the platform-injected value takes precedence
     Origin: components/demo/widget/component.yaml
     Suggestions:
     1. Rename the config item in configSchema to avoid the platform's reserved variables
     2. For example, rename it to UPSTREAM_BASE_URL
  ```

- **A component's variable names are its own.** No component prefix is needed (`QUOTE_` and the like): each component reads
  its own environment in its own container. Several components needing the same value (the same database address) is
  something users solve with `$var:` in `config/vars.yaml`, not a reason for components to rename things.

## How finely to split

One config item per **value a user decides on its own**.

**Too coarse:** one `CONFIG` holding a whole block of JSON.

```yaml
    CONFIG:
      type: string
      description: all the configuration, as JSON
```

Users can't see what's in it or what's required; changing one value means rewriting the whole block; the key-name check
can't help; and when a default changes on an upgrade, the whole block conflicts.

**Too fine:** every fragment of a connection string split out — `DB_SSL_MODE`, `DB_POOL_MIN`, `DB_POOL_MAX`,
`DB_POOL_IDLE_TIMEOUT`… dozens of items, while users really change two or three. What nobody needs to change stays in the
code, or gets a sensible `default`.

**Just right:** what users change because environments differ gets an item each; what almost nobody changes gets a default.

## Good designs

**A database connection**: address and credentials apart, the password marked secret.

```yaml
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
      description: Database name. The user creates the database once; the component's migration creates the tables
    DB_USER:
      type: string
      description: User to connect as
    DB_PASSWORD:
      type: string
      secret: true
      description: Password to connect with
  required: [DB_HOST, DB_NAME, DB_USER, DB_PASSWORD]
```

A `secret: true` value takes the secret path when deployed (a 0600 env file on Docker, a generated Secret on Kubernetes)
and is never written in plain text into a deployment file; users write `${DB_PASSWORD}` in `config/` to reference it (see
[Secrets](../01-three-layers/07-sensitive-values.md)). Only you decide which item is a secret — the platform doesn't guess
from names.

**Read/write split**: whether to split reads and writes is the component's decision; the platform knows nothing about
primaries and replicas. To support it, declare two addresses, with reads falling back to the write database:

```yaml
    DB_WRITE_HOST:
      type: string
      description: The write database host
    DB_READ_HOST:
      type: string
      description: The read database host; when unset, reads and writes both go to DB_WRITE_HOST
```

**Several instances of one kind**: two caches get a set of items each, named by business meaning rather than `CACHE_1`,
`CACHE_2`:

```yaml
    SESSION_REDIS_URL:
      type: string
      description: Session cache
    RATE_LIMIT_REDIS_URL:
      type: string
      description: Rate-limit counters
```

**A feature switch**: a boolean with a default, saying what switching it on adds.

```yaml
    EXPORT_ENABLED:
      type: boolean
      default: false
      description: When on, serves /api/v1/export; exporting uses a fair amount of memory
```

**The address of a service in another project**: a service deployed by another project isn't among your dependencies,
and the platform injects no `*_ENDPOINT` for it. Declare the address as a required config item without a default — the
platform can't derive it, so users have to fill it in:

```yaml
    NOTIFIER_URL:
      type: string
      description: The notification service's address (deployed by the operations platform project)
  required: [NOTIFIER_URL]
```

## Writing `description` and `default` well

- `description` appears at the end of each line in users' config skeletons. Write its business meaning and units, not a
  restatement of the key.
- What the default can't say (how to choose between values, what changing it does) goes into the "Configuration" section
  of the component's [BRICKKIT.md](08-component-doc-spec.md).
- **Changing a default is something to do carefully.** Keys users never wrote follow the new default automatically; a key
  a user wrote, whose default you then change, becomes a conflict on upgrade that they must decide one by one (see
  [Upgrading and config migration](../02-project-guide/06-upgrade-and-migration.md)).

The complete field rules are in the [configSchema specification](../11-reference/04-config-schema-spec.md).
