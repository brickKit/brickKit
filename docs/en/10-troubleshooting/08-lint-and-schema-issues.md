# lint and schema problems

Three checks, each seeing more than the last: the JSON Schema in your editor looks only at one file's structure;
`brickkit lint` also checks field combinations and whether the three layers agree; `brickkit up --dry-run` also resolves the
whole dependency graph. When their conclusions differ, it's usually because they check different scopes, not because one of
them is wrong.

## `lint` reports an error, yet `up` works

**Symptom**

`brickkit up` runs fine, but `brickkit lint` reports an error, like:

```text
❌ Error: deploy.local.yaml is out of date: it does not match the components in brickkit.yaml
   File: deploy.local.yaml
   No entry for: demo/bus
   Reason: brickkit.yaml changed, but your deploy.local.yaml was not updated. Local mode is off, so commands don't read it now — but the next brickkit local on will
   Suggestions:
   1. Option A (recommended): brickkit local refresh regenerates deploy.local.yaml from deploy.yaml and keeps the old one as deploy.local.yaml.bak
   2. Option B: add or remove the listed entries in deploy.local.yaml by hand
   3. Option C: if you no longer need it, delete deploy.local.yaml (it is never committed)
```

**Cause**

`lint` checks more than this run of `up` uses:

| What `lint` checks and this `up` doesn't necessarily read | Why |
| --- | --- |
| `deploy.local.yaml` — **whether local mode is on or not** | With local mode off, `up` doesn't read it; but you may `local on` at any moment, and then it must match `brickkit.yaml`. The team added a component and your personal file didn't keep up: it's reported here |
| **Every** `component.yaml` in the local sources, whether `add`ed or not | `up` doesn't read components not yet added to the project; `lint` makes them right before they're used |
| Under `--strict`: whether `${VAR}` has a value in the process environment or `.env`, whether the file `file://` points at exists | On Docker, `${VAR}` is expanded by `docker compose` only at start-up, and `up` itself doesn't check it |

**Fix**

Fix what the error says. A stale personal file: `brickkit local refresh` (it works with local mode off too; it backs up the
old file and lists your earlier local changes); if you no longer need it, just delete it. To check only one deploy file:
`brickkit lint -f deploy.prod.yaml`.

## `lint` passes, yet `up --dry-run` reports an error

**Symptom**

`lint` is all green, and `up --dry-run` reports `required dependency missing`, a mismatched member version hosted by a
shell, or a component that can't be found.

**Cause**

This is a deliberate division of work. `lint` promises to be offline, read-only and instant, so it **doesn't resolve the
dependency graph**: resolving it means getting every component's `component.yaml`, which for components on Git or a market
means the network. Whether dependencies exist, and whether the member versions a shell hosts this time are right, can only
be judged on the dependency graph.

**Fix**

Run both steps in CI: `lint --strict` stops structural problems, `up --dry-run` stops dependency problems (it needs access
to the install sources, but not to a cluster). See [Offline checks](../02-project-guide/10-lint-and-checks.md#in-ci).

## The editor shows nothing red, yet `lint` reports an error

**Cause**

A JSON Schema describes only **one file's structure**: field names, types, required fields, fixed values. It can't express
these, which `lint` checks:

- Combinations of fields: `localPort` goes together with `mode: local` / `mode: debug`; `mode: debug` can only be written
  in `deploy.local.yaml`.
- Across the three layers: every component version has exactly one entry in the deploy file; `$var:` is defined; keys in
  `config/` are in the component's `configSchema`.
- Config items colliding with names the platform reserves.

**Fix**

Fix what `lint` reports. The editor showing nothing red only says that one file's structure is right.

## The editor shows red, yet `lint` passes

**Symptoms and causes**, the two common ones:

- **A conflict block in `config/` is marked as a duplicate key.** That's expected: BrickKit deliberately uses duplicate keys
  for conflicts left by an upgrade. In this case `lint` reports an error too (an unresolved conflict), though; see
  [Config conflict problems](03-config-conflict-issues.md).
- **The schema your editor uses doesn't match your CLI's version.** Wired up as the docs describe, it's the schema on the
  `main` branch, which may be newer (more fields) or older (fewer fields) than the CLI you installed. The CLI is what
  counts: if `lint` passes, the CLI accepts it.

**Fix**

Change the schema URL from `main` to the tag of the CLI version you use, or point it at a local copy under `schemas/` in the
repository at the same version as the CLI. How to wire it up: [JSON Schemas](../11-reference/05-json-schemas.md).

## The editor has no completion or validation at all

**Cause**

- No YAML language service is installed (the Red Hat YAML extension in VS Code, JetBrains' built-in one,
  yaml-language-server in Neovim).
- The file name doesn't match the mapping: the settings map `deploy.yaml`, while you opened `deploy.prod.yaml`.
- `config/*.yaml` has no schema to begin with: each component's config items differ, set by its own `configSchema`, and only
  `lint` checks key names against it.

**Fix**

Following method two in [JSON Schemas](../11-reference/05-json-schemas.md), map every deploy file with a glob like
`["deploy.yaml", "deploy.*.yaml"]`.
