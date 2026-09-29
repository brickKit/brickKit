# Offline checks

## `brickkit lint`

No network, no Docker or Kubernetes, no file changed: it checks the project's YAML in seconds:

```bash
brickkit lint
```

```text
✅ brickkit.yaml
✅ deploy.yaml
✅ deploy.local.yaml
✅ cross-file: brickkit.yaml ↔ deploy.yaml ↔ config/
✅ cross-file: brickkit.yaml ↔ deploy.local.yaml ↔ config/
✅ components/demo/hello/component.yaml

📋 Checked 6 files: 0 with errors, 0 warnings
```

In a project directory (with a `brickkit.yaml`), it checks in turn:

| What | Examples |
| --- | --- |
| The structure of `brickkit.yaml` | Required fields, types, versions only exact |
| The structure of the deploy files | Unknown fields (typos), `mode: debug` in `deploy.yaml`, `localPort` without `mode`, host port conflicts |
| Whether the three layers agree | Exactly one deploy entry per component version; shell members nested under their shell; `config/` files matching components |
| References in `config/` | Every `$var:` defined; no conflict blocks (duplicate keys) left by an upgrade |
| Config against `configSchema` | Required items have values; written keys are in the schema (a mistyped key has no effect); shell members' values can go into the shell |
| Shell declarations | `kind: shell` in `brickkit.yaml` agrees with the component's `shell` block; members nested under a shell are really compiled into it |
| `component.yaml` files in local sources | Every one, added or not (`.archived/` excluded) |

When `deploy.local.yaml` exists it's checked too — whether local mode is on or not, it must agree with `brickkit.yaml`.
With `-f`, only that deploy file is checked. When `brickkit.yaml` itself fails, nothing after it can be trusted; the rest
is skipped, and it says why.

In a component repository (a `component.yaml`, no `brickkit.yaml`), only that `component.yaml` is checked.

A typical error:

```text
✅ brickkit.yaml
❌ Error: deploy.yaml failed validation
   File: deploy.yaml
   components[1].exposed: unknown field (line 8), did you mean expose?
   Suggestion: Full field reference: docs/en/11-reference/03-deploy-yaml-schema.md (swap en for zh for the Chinese version)
✅ deploy.local.yaml
✅ components/demo/hello/component.yaml

📋 Checked 4 files: 1 with errors, 0 warnings
❌ Error: the structure check did not pass
   Checked: 4 files
   With errors: 1 file
   Suggestion: Fix them at the locations listed above, then run brickkit lint again
```

An unknown field is an error, not a warning: with `exposed: true` mistyped, the component simply isn't opened to the
outside, and nothing else would ever tell you.

## `--strict`

`--strict` checks one more kind of thing — whether referenced **values** exist: a `${VAR}` found in neither the process
environment nor `.env`, a `file://` whose file doesn't exist. And warnings count as failures:

```bash
brickkit lint --strict
```

```text
⚠️ Warning: demo/caller@1.0.0's DATABASE_NAME points at a file:// that does not exist
   Path: .secrets/dbname
   Suggestion: file:// paths are relative to the project root; keep such files out of Git (e.g. under .secrets/)
⚠️ Warning: demo/caller@1.0.0's DATABASE_USER references an environment variable that is not set here
   Variable: DB_USER
   Suggestion: It is looked up in the process environment first, then .env in the project root — set it in one of them
```

```text
📋 Checked 6 files: 0 with errors, 2 warnings
❌ Error: the structure check did not pass
   Checked: 6 files
   Warnings: 2 (--strict: warnings count as failures)
   Suggestion: Fix them at the locations listed above, then run brickkit lint again
```

Without `--strict` these two aren't checked: on a development machine, the production passwords aren't in the
environment anyway. (`up` on Docker does check that a `${VAR}` the deployment will use is defined — see
[Secrets](../01-three-layers/07-sensitive-values.md).)

## Exit codes

| Result | Exit code |
| --- | --- |
| No problems, or only warnings | 0 |
| Errors | 1 |
| Warnings under `--strict` | 1 |

## What `lint` doesn't check

- **Whether dependencies resolve, and whether the member versions a shell hosts this time are right.** That needs every
  component's `component.yaml` and the complete dependency graph, plus working out what runs this time — for Git or
  market components, that means the network. `lint` promises to be offline, read-only and immediate, so it doesn't build
  that graph. Those are left to `brickkit up --dry-run`: it has to resolve the graph anyway, and names a missing required
  dependency or a mismatch between hosted and compiled-in versions.
- **Whether config values satisfy the `configSchema` constraints** (`enum`, `minimum`, `pattern`…). The platform checks
  key names, not values: a wrong value makes the component complain loudly on its own, while a wrong key name does
  nothing at all — your config just quietly doesn't apply — so only the latter is checked. See the
  [configSchema specification](../11-reference/04-config-schema-spec.md).

## In CI

```bash
brickkit lint --strict
brickkit lint --strict -f deploy.prod.yaml
brickkit up --dry-run -f deploy.prod.yaml
```

- `lint --strict` as a gate: structural errors and undefined references are stopped before merging. CI needs the values of
  those `${VAR}` (or drop `--strict` for a structure-only step).
- Check each environment's deploy file with `-f`: by default only `deploy.yaml` is checked.
- Add an `up --dry-run` step to check dependency resolution too — it needs access to the install sources, but not to the
  cluster.
