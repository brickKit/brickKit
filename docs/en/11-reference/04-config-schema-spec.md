# The configSchema spec

`configSchema` is a block in a component's `component.yaml` declaring which config items the component recognises. Its
form borrows a subset of JSON Schema (draft-07): one `type: object`, with `properties` listing each item and `required`
listing the required ones. How to split and name the items: [Designing configSchema](../03-component-guide/03-config-schema-design.md).

## Structure

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
      minimum: 1
      maximum: 65535
      description: PostgreSQL port
    DB_PASSWORD:
      type: string
      secret: true
      description: Password to connect with
    REGIONS:
      type: array
      items:
        type: string
      default: [eu-west, us-east]
      description: Regions served
  required: [DB_HOST, DB_PASSWORD]
```

| Written | Means |
| --- | --- |
| The keys of `properties` | Config item names, which are the environment variable names injected, as they are: they must be valid environment variable names (letters, digits and underscores, not starting with a digit) |
| `type` | `string` / `integer` / `number` / `boolean` / `array` / `object` |
| `default` | Used when no value is written; scalars are injected as strings, `array` / `object` as one line of JSON |
| `description` | Appears at the end of the line in the project's config skeleton |
| `secret: true` | The item is a secret: at deployment it takes the secret channel, never written into deployment files in plain text |
| `required` | The required items; each must be declared under `properties` |
| `enum`, `minimum`, `maximum`, `pattern`, `items` | Allowed values — **documentation only** |

The exact rules of each field: [component.yaml field reference](01-component-yaml-schema.md#configschema).

## The platform checks key names, not values

This is the most important rule of `configSchema`.

**Key names** the platform checks:

| Situation | Result |
| --- | --- |
| `config/` writes a key that isn't in `configSchema` | A warning that it "has no effect", with a guess at what you meant |
| The component has no `configSchema`, yet `config/` holds config for it | A warning: none of that config takes effect |
| An item in `required` has no `default` and nothing was filled in under `config/` | `up` refuses to start, naming the missing item |
| A key name collides with a name the platform reserves | `up` / `lint` warn and ignore the item; a market refuses it |

**Values** the platform doesn't check: `"abc"` in a `type: integer` item, a value outside `enum`, a number above `maximum` —
the platform injects them all as usual.

The line is whether there's a safety net at run time. A wrong value fails when the component reads it (it can't be
parsed, it fails validation), so you're sure to notice, and how to handle a wrong value is the component's own decision. A
wrong key name means the variable doesn't exist at all, the component takes its default branch and runs normally —
nothing fails, the config just quietly doesn't take effect. Only the platform can catch errors of that kind, so that's the
only kind it checks.

And once value checking starts, it doesn't stop: types, enums, ranges, regular expressions, nested structures… JSON Schema
can do almost anything, and every extra check is one more decision the platform makes for the component.

So a component validates values itself at start-up:

```go
port, err := strconv.Atoi(os.Getenv("DB_PORT"))
if err != nil || port < 1 || port > 65535 {
	log.Fatalf("DB_PORT must be a port number, got %q", os.Getenv("DB_PORT"))
}
```

## How it relates to JSON Schema

The form is compatible with JSON Schema, but it isn't **executed** as JSON Schema: the CLI reads only the keys of
`properties`, `type`, `default`, `required`, `secret` and `description`; the other keywords are kept as they are, for people
to read. A keyword not listed here written in an item (a misspelled `defualt`, or `format`, `examples`, `oneOf`… out of
JSON Schema habit) is dropped when parsing — it has no effect. The author gets a warning where they can hear it and fix it
(`lint`, local source scans, `publish`); installing someone else's component doesn't fail because of it, since the project
using it can't change someone else's Manifest.

## Editors

`schemas/component.schema.json` describes the structure of the `configSchema` block; wired into an editor, writing
`configSchema` gets completion, and a misspelled keyword (`defualt`) is marked red at once. See
[JSON Schemas](05-json-schemas.md). `lint` also reports misspelled keys in a `configSchema` item:

```text
Warning: some keys declared on configSchema items won't take effect
```
