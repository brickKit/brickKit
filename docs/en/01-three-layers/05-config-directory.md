# The config/ directory

`config/` answers **which environment variables each component gets**. There is one flat YAML file per component, and
every key becomes an environment variable in the container, as-is.

## Layout

```text
config/
├── vars.yaml                  shared variables: values several components use, referenced with $var:NAME
├── department-tree.yaml       config for the default version of department/tree
├── people-basic.yaml          config for the default version of people/basic
├── people-basic@0.9.0.yaml    config for people/basic 0.9.0 (here only because another component depends on it)
└── .archive/                  config archived by remove (not in Git by default)
```

## File names

The component ID with `/` turned into `-`: `department/tree` → `department-tree.yaml`.

When a component is present in several versions (see [the default version in brickkit.yaml](02-brickkit-yaml.md#the-default-version-and-requiredby)):

| File | Belongs to |
| --- | --- |
| `config/people-basic.yaml` | The default version (the line in `brickkit.yaml` without `requiredBy`) |
| `config/people-basic@0.9.0.yaml` | The version with `requiredBy`; `add` generates its skeleton when it brings that version in |

Having two files for the default version (one without a version and one `@<default version>`) is an error: which one
applies can't be told, so the CLI refuses outright. A file that belongs to no declared version is reported too — it's
most likely an orphan left behind by a version change.

## The config skeleton

`add` generates a skeleton from the component's `configSchema`, in two parts:

```yaml
# Component: department/tree@1.0.0
# Environment variables for this component; every key is injected as-is.
# Shared variable: $var:NAME (config/vars.yaml) · environment variable: ${NAME} · local file: file://path

# === Required: startup is blocked until these have a value ===
DATABASE_HOST: ""  # string | PostgreSQL host name
DATABASE_NAME: ""  # string | Database name
DATABASE_USER: ""  # string | User to connect as

# === Optional: commented keys use the component's default; uncomment to override ===
# DATABASE_PASSWORD:  # string | secret | Password to connect with. Write it as a ${VAR} or file:// reference, never as plain text in config/
# DATABASE_PORT: 5432  # integer | PostgreSQL port (default)
# LOG_LEVEL: info  # string | Log level (debug | info | warn | error) (default)
```

- **Required items without a default** get an empty value for you to fill in. Until you do, `up` stops and names the
  missing ones:

  ```text
  ❌ Error: a required component config item has no value
     Missing config: department/tree@1.0.0 → DATABASE_HOST
     Missing config: department/tree@1.0.0 → DATABASE_NAME
     Missing config: department/tree@1.0.0 → DATABASE_USER
     Reason: The component declares it in configSchema.required without a default — the platform can't derive this one, so the project has to supply it
  ```

- **Optional items are written as comments**, carrying their default. An item you haven't uncommented always follows
  the default of the component's current version — when an upgrade changes a default, you get the new one
  automatically; only the values you actually write are yours, and only they can conflict on an upgrade (see
  [Upgrading and config migration](../02-project-guide/07-upgrade-and-migration.md)).
- **Items declared `secret: true`** are marked `secret` in the comment, to remind you to use a reference rather than
  plain text (see [Secrets](07-sensitive-values.md)).

When `config/vars.yaml` (or the deploy file's `vars:`) already has a shared variable of the same name, `add` asks
whether to reference it directly; with `--yes` it always does:

```text
🔗 DATABASE_HOST in config/department-tree.yaml references the variable of the same name in config/vars.yaml (--yes)
```

```yaml
DATABASE_HOST: $var:DATABASE_HOST  # string | PostgreSQL host name
```

`add` never touches a byte of a config file that already exists.

## `.archive/`: archived on remove, restored on add

When you `remove` a component, its config isn't deleted but moved into `config/.archive/`, with the version in its name:

```text
➖ Removed department/tree@1.0.0
🗄️  Config archived: config/department-tree.yaml → config/.archive/department-tree@1.0.0.yaml
```

When you later `add` the same component again, its config comes back from the archive through the same migration an
upgrade uses (an archived older version is migrated to the new version's `configSchema`):

```text
♻️  config/department-tree.yaml restored from the archive (config/.archive/department-tree@1.0.0.yaml, migrated to this version)
📝 config/department-tree.yaml
   Kept as written: DATABASE_HOST, DATABASE_NAME, DATABASE_USER
```

When restoring from the archive, what's in the archive is the config you wrote back then; it wins, and `add` doesn't
ask again about referencing shared variables. `.archive/` is in `.gitignore` by default: it's "might still need it
later" material on this machine, not part of the project.

## How it relates to `configSchema`

A component's `configSchema` is its **spec sheet**: which config items it recognises, of what type, with what default,
which are required and which are secrets. The files in `config/` are the **actual values**.

The platform checks **key names**, not **values**:

- A key that isn't in `configSchema` gets a warning from both `up` and `lint` — a key with one letter wrong raises no
  error, it just silently does nothing, so someone has to say so:

  ```text
  ⚠️ demo/hello@1.0.0: GREETNG is not declared in the component's configSchema, so it has no effect
     File: config/demo-hello.yaml
     Declared keys: GREETING
     Suggestion: Did you mean GREETING?
  ```

- Types, enums and ranges of values aren't checked: what a component does with an invalid value (fail and exit, fall
  back to a default) is its own business. See the [configSchema specification](../11-reference/04-config-schema-spec.md).
