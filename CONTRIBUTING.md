# Contributing to BrickKit

[中文](CONTRIBUTING.zh.md)

## Before you start

Skim [AGENTS.md](AGENTS.md) first — it compresses the platform's whole
philosophy, terminology, and twelve design principles into one file,
including an explicit list of things the platform deliberately doesn't do
(§4.1) and why (§9). A lot of "why doesn't it just—" questions are already
answered there. If a change would add something on that rejection list,
open an issue and make the case before writing code — it'll very likely need
to overturn one of those twenty-three arguments, not just add a feature.

## Building and running the test suite

```bash
make build            # bin/brickkit + bin/market-server
make test             # unit tests
make test-all         # the full suite, including checklist/regression gates
make lint             # vet + all the doc-consistency checks
```

**There is no CI that runs on pull requests.** The only GitHub Actions
workflow (`.github/workflows/release.yml`) triggers on a `v*` tag push and
builds/signs/publishes a release — it never runs on a branch or a PR. That
means `make lint` and `make test-all` passing locally, before you open a PR,
is the only gate there is. A red `make lint` on your machine will be red for
every reviewer too.

`make lint` runs `golangci-lint` if it's installed (`make tools-lint` installs
it into `.tools/bin`; there's no repo-specific `.golangci.yml`, so it's
whatever golangci-lint v2 ships as defaults) and falls back to `go vet` if
it isn't. Install it before you open a PR — `go vet` alone won't catch
everything golangci-lint does.

`make lint` also runs a set of documentation-consistency scripts
(`scripts/check-*.py`) — dangling section references, broken links, commands
and flags claimed in docs that don't actually exist, doc snippets whose YAML
field names don't match the real struct, the docs/en↔docs/zh mirror, and a
few others (the full list, with what each one guards, is in the README's
["Build & test"](README.md#build--test) section). These aren't decoration:
several of them exist specifically because a past change broke something a
test suite has no way to notice — a renamed flag, a stale example, a link
that used to point somewhere real.

`make lint` also guards the JSON Schemas in `schemas/`
(`schemas/component.schema.json` and `schemas/brickkit.schema.json` — what
editors use to complete and check `component.yaml` and `brickkit.yaml`). They
are generated from the Go structs in `internal/manifest` and
`internal/config`, never hand-edited. **If you add, remove or retype a field
there, or change its `omitempty` or a `jsonschema` tag, run `make
generate-schemas` and commit the regenerated files along with the change** —
otherwise `make check-schemas` (part of `make lint`) fails. That check also
holds the schema's required fields and its closed values, patterns and ranges
to what the real validators accept, so a `jsonschema` tag can't quietly drift
away from `Validate`.

## Where tests live

Unit tests live **next to the code they test** (`internal/**/*_test.go`,
`market-server/internal/**/*_test.go`) — there's no parallel test tree to
keep in sync. `tests/` only holds what genuinely can't live next to the
code it's testing: `tests/checklist/` and `tests/regression/` are
acceptance checklists paired with the tests that prove each line item (both
enforced by `make lint`, per the table in the README), and
`tests/components/` holds the real fixture components several tests and
guide articles run against.

If you're adding a checklist-worthy behavior (a boundary condition, an
error case, a compatibility or security guarantee), add the line to the
relevant `tests/checklist/*/清单.tsv`/`tests/regression/清单.tsv` and wire a
real test to it — a checklist entry with no test, or a test that's stopped
existing, fails the build on purpose (see the "Build & test" table in the
README for why).

## Documentation conventions

- **`docs/en/` and `docs/zh/` are two independently-written, symmetric
  trees** — not a source language plus translations. If you add or change a
  document in one, the other needs the equivalent file at the same relative
  path (`make check-docs-bilingual` enforces this), written naturally in
  that language rather than mechanically translated.
- **A guide or troubleshooting example that shows CLI output must be real
  output**, not what you expect the CLI to print. Run the command, paste
  what actually came back. `make check-guide-output` verifies this for the
  hands-on guide series.
- **A YAML field shown in a doc must exist on the real struct** — `make
  check-doc-fields` checks this against `component.yaml`/`brickkit.yaml`'s
  actual Go types, not the other way around.
- If you touch `AGENTS.md`/`AGENTS.zh.md`, keep them genuinely equivalent
  in content — they're each independently written for their language, not a
  translation pair, but they should still cover the same ground.

## Commit messages and branches

Commit subjects in this repo follow a `<动词>：<what changed>` /
`<verb>: <what changed>` convention — `新增`/`修复`/`改进`/`更新` (add/fix/
improve/update) are the ones you'll see most. Look at `git log` for the
shape; matching it isn't mandatory but keeps history scannable.

Work in a feature branch and open a PR against `main`. There's no branch
protection or required review configured yet (this is a young project), so
in practice a PR is how a change gets discussed before landing, not a
technical gate.

## Reporting a bug or proposing a feature

Open a GitHub issue. For a bug, the CLI's own output is usually most of
what's needed — `brickkit` writes structured JSON logs to stderr
(`--log-level debug` for more detail) and human-readable output to stdout;
including both, plus your `brickkit.yaml` and the component's
`component.yaml`, saves a round trip. For a feature request that isn't
already on the [rejection list](AGENTS.md#41-what-the-platform-explicitly-refuses-to-do-the-rejection-list),
explain the problem you're solving, not just the mechanism you have in mind
— see the ["twelve design principles"](AGENTS.md#4-twelve-design-principles-the-philosophical-core)
for the bar a new mechanism needs to clear.
