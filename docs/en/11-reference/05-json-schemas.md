# JSON Schemas

## The `schemas/` directory

The repository's `schemas/` holds three JSON Schemas (draft-07), describing the structure of the three kinds of file:

| File | Describes |
| --- | --- |
| `schemas/component.schema.json` | A component's `component.yaml` |
| `schemas/brickkit.schema.json` | A project's `brickkit.yaml` |
| `schemas/deploy.schema.json` | Deploy files: `deploy.yaml`, `deploy.local.yaml`, `deploy.<environment>.yaml` |

They're generated from the CLI's Go structs, not written by hand: when a struct changes, they're regenerated and committed
together; a check in CI stops "the struct changed but nobody regenerated". So the fields in the schemas match the fields
the CLI really accepts, one for one.

## Wiring them into an editor

Once an editor with a YAML language service (the Red Hat YAML extension in VS Code, the JetBrains family,
yaml-language-server in Neovim…) has the schemas: field names get completion, and wrong types, missing required fields and
misspelled field names are marked red at once — the same set of structural problems `brickkit lint` reports, only seen
while you type.

**Method one: a comment at the top of the file.** It applies to one file, the same for whoever opens it:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/brickKit/brickKit/main/schemas/deploy.schema.json
target: docker
components:
  - id: demo/hello
```

`component.yaml` uses `component.schema.json`, `brickkit.yaml` uses `brickkit.schema.json`.

**Method two: map by file name in the editor's settings.** VS Code's `settings.json`:

```json
{
  "yaml.schemas": {
    "https://raw.githubusercontent.com/brickKit/brickKit/main/schemas/component.schema.json": "component.yaml",
    "https://raw.githubusercontent.com/brickKit/brickKit/main/schemas/brickkit.schema.json": "brickkit.yaml",
    "https://raw.githubusercontent.com/brickKit/brickKit/main/schemas/deploy.schema.json": ["deploy.yaml", "deploy.*.yaml"]
  }
}
```

Put it in the project's `.vscode/settings.json`, and the whole team has it on opening the project. To pin a CLI version,
replace `main` in the URLs with that version's tag, or point at a local copy.

## What they cover, and what they don't

A JSON Schema describes only **one file's structure**: field names, types, required fields, fixed values (`target` can
only be `docker` / `podman` / `k8s`, and so on).

What they don't cover belongs to other checks:

| Rule | Checked by |
| --- | --- |
| Combinations of fields (`localPort` needs `mode`, `exposePort` needs `expose`, `mode: debug` only in `deploy.local.yaml`) | `brickkit lint` |
| Config items colliding with names the platform reserves, typos inside a `configSchema` item | `brickkit lint` |
| Whether the three layers agree (one deploy entry per component version, `$var:` defined, keys in `config/` present in `configSchema`) | `brickkit lint` |
| Whether dependencies resolve, whether the member versions a shell hosts are right | `brickkit up --dry-run` |
| Config values in `config/*.yaml` | No schema: each component's config items differ, set by its own `configSchema`, and `lint` checks key names against it |

Nothing red in the editor doesn't mean `lint` passes; `lint` passing doesn't mean `up --dry-run` passes — each of the three
checks more than the last.
