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
✅ cross-file: brickkit.yaml ↔ deploy.local.yaml ↔ config/
✅ cross-file: brickkit.yaml ↔ deploy.yaml ↔ config/
✅ ./ (docs)
✅ components/demo/hello/component.yaml
✅ components/demo/hello/ (docs)

📋 Checked 8 files: 0 with errors, 0 warnings
```

In a project directory (with a `brickkit.yaml`), it checks in turn:

| What | Examples |
| --- | --- |
| The structure of `brickkit.yaml` | Required fields, types, versions only exact |
| The structure of the deploy files | Unknown fields (typos), `mode: debug` in `deploy.yaml`, `localPort` without `mode`, host port conflicts |
| Whether the three layers agree | Exactly one deploy entry per component version; shell members nested under their shell; `config/` files matching components |
| References in `config/` | Every `$var:` defined; no conflict blocks (duplicate keys) left by an upgrade |
| Config against `configSchema` | Required items have values; written keys are in the schema (a mistyped key has no effect); shell members' values can go into the shell. For every component version whose `component.yaml` is on disk (in `.brickkit/manifests/` or a local source holding that version); the rest are listed in a note as not checked |
| Shell declarations | `kind: shell` in `brickkit.yaml` agrees with the component's `shell` block; members nested under a shell are really compiled into it (likewise only where the `component.yaml` is on disk) |
| `component.yaml` files in local sources | Every one, added or not (`.archived/` excluded) |
| Documents | The project's own documents (`./ (docs)`) and the docs of every component in a local source; see [Documentation checks](#documentation-checks) |

When `deploy.local.yaml` exists it's checked too — whether local mode is on or not, it must agree with `brickkit.yaml`.
With `-f`, only that deploy file is checked. When `brickkit.yaml` itself fails, nothing after it can be trusted; the rest
is skipped, and it says why.

In a component repository (a `component.yaml`, no `brickkit.yaml`), that `component.yaml` and the component's documents
are checked.

In a workbench (a component repository that also has a `brickkit.yaml`), the three layers are checked as a project, and
the component's `component.yaml` and documents with them.

### One component of a project

In a component's directory inside a project, `brickkit lint` with no argument checks **only that component** — the same
rule `build` and `deps` follow. `brickkit lint <id>` does the same from anywhere:

```text
📁 Project: ../../.. (shop)
🔎 Only demo/hello is checked (brickkit lint --all checks the whole project)
✅ component.yaml
✅ ./ (docs)
✅ demo/hello: configuration (config/ ↔ configSchema)

📋 Checked 3 files: 0 with errors, 0 warnings
```

That is its `component.yaml` and documents when a local source holds it (added or not), and its configuration against
its `configSchema` once it is in `brickkit.yaml`. Problems elsewhere in the project aren't reported: when you rebuild
components one at a time, the one you just finished shouldn't be red because another isn't done yet. The project still
has to load the way `up` loads it — if it doesn't, this component can't run either, so that error stays. For a component
from git or a market, its `component.yaml` and documents are its author's to check; only its configuration is checked.

`brickkit lint --all` checks the whole project, wherever you are. CI runs it at the project root, where there's no
difference.

A typical error:

```text
✅ brickkit.yaml
❌ [CONFIG_INVALID] Error: deploy.yaml failed validation
   File: deploy.yaml
   components[0].exposed: unknown field (line 7), did you mean expose?
   Suggestion: Full field reference: brickkit docs 11-reference/03-deploy-yaml-schema (online: https://github.com/brickKit/brickKit/blob/v1.1.0/docs/en/11-reference/03-deploy-yaml-schema.md)
✅ deploy.local.yaml
✅ ./ (docs)
✅ components/demo/hello/component.yaml
✅ components/demo/hello/ (docs)

📋 Checked 6 files: 1 with errors, 0 warnings
❌ Error: the structure check did not pass
   Checked: 6 files
   With errors: 1 file
   Suggestion: Fix them at the locations listed above, then run brickkit lint again
```

An unknown field is an error, not a warning: with `exposed: true` mistyped, the component simply isn't opened to the
outside, and nothing else would ever tell you.

## Documentation checks

`lint` also reads the documents, mechanically: are the required files there, do they have their fixed sections, do the
paths and links in them lead somewhere, does a document still say what `component.yaml` says. What it reads depends on
where you run it:

| Where | Documents checked |
| --- | --- |
| A component repository | The component's `BRICKKIT.md` (and its translations), `AGENTS.md`, `CLAUDE.md`, `README.md`, and `docs/` when there is one |
| A project | The project's `AGENTS.md`, `CLAUDE.md`, `README.md`, their translations and every file under `docs/`; an old project map `BRICKKIT.md` left at the root; and the documents of every component in a local source |
| A workbench | The component's documents, as in a component repository |

Each set of documents is one line in the report — `✅ ./ (docs)` for the project, `✅ components/demo/hello/ (docs)` for
a component — or, when something is off, the warnings in its place:

```text
⚠️ [DOC_LINK_NOT_PORTABLE] BRICKKIT.md is read alone in other projects' caches, so the relative link openapi.json is dead there: name the file as inline code, or use an absolute URL
   File: components/demo/hello/BRICKKIT.md
   Line: 46
```

**Every documentation finding is a warning.** It never stops `lint`, `up` or `release`: a document that is a little
behind is a problem to fix, not a reason to stop a deployment. `--strict` turns warnings into a failure, for a team that
wants CI to hold the line.

Every finding in the report — error or warning, about the documents or about the three layers — starts with its
[error code](../06-architecture/09-error-codes.md) in brackets. The code is the same in every language and every version,
so it is what a script or a CI rule should match, and what you look up; the sentence after it is for reading. The
documentation checks use these:

| Code | What it means |
| --- | --- |
| `DOC_FILE_MISSING` | A required document is absent |
| `DOC_SECTION_MISSING` | A document lacks one of its fixed sections (headings are recognised in English and Chinese) |
| `DOC_PATH_MISSING` | A path in the code map of a component's `AGENTS.md` doesn't exist |
| `DOC_LINK_BROKEN` | A relative link points at a file that doesn't exist |
| `DOC_LINK_NOT_PORTABLE` | `BRICKKIT.md` has a relative link (it is read alone, where the link is dead), or a document links out of the component directory |
| `DOC_OUT_OF_STEP` | `component.yaml` has a dependency, required config key, contract file or shell member the document doesn't mention |
| `DOC_PLACEHOLDER` | A `TODO` (or `TBD`, `FIXME`) is still in the text — the skeletons leave one in every section on purpose |
| `DOC_TRANSLATION_DRIFT` | A translation (`BRICKKIT.zh.md`, `README.zh.md`, a page under `docs/zh/`) is out of step with its primary file, or a page is missing from one of the `docs/<lang>/` trees |
| `AGENTS_BLOCK_MISSING` | `AGENTS.md` has no usable block maintained by the CLI, so its component table isn't kept up to date |
| `CLAUDE_IMPORT_MISSING` | `CLAUDE.md` doesn't contain `@AGENTS.md` |
| `PROJECT_MAP_OBSOLETE` | The project root still has the old project map `BRICKKIT.md` |

What each one asks you to do: [Error codes](../06-architecture/09-error-codes.md#documentation-checks). How the
documents themselves are written: [A component's documentation](../03-component-guide/08-component-doc-spec.md) and
[The project's `AGENTS.md`](01-init-and-project-creation.md#the-projects-agentsmd) and
[The project's other documents](01-init-and-project-creation.md#the-projects-other-documents).

## `--strict`

`--strict` checks one more kind of thing — whether referenced **values** exist: a `${VAR}` found in neither the process
environment nor `.env`, a `file://` whose file doesn't exist. And warnings count as failures:

```bash
brickkit lint --strict
```

```text
⚠️ [CONFIG_INVALID] Warning: demo/caller@1.0.0's DATABASE_NAME points at a file:// that does not exist
   Path: .secrets/dbname
   Suggestion: file:// paths are relative to the project root; keep such files out of Git (e.g. under .secrets/)
⚠️ [CONFIG_INVALID] Warning: demo/caller@1.0.0's DATABASE_USER references an environment variable that is not set here
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

- `lint --strict` as a gate: structural errors, undefined references and documentation warnings are stopped before
  merging. CI needs the values of those `${VAR}` (or drop `--strict` for a structure-only step).
- Check each environment's deploy file with `-f`: by default only `deploy.yaml` (and `deploy.local.yaml`, if present)
  is checked, never `deploy.prod.yaml`.
- Add an `up --dry-run` step to check dependency resolution too — it needs access to the install sources, but not to the
  cluster.
