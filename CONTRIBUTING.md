# Contributing to BrickKit

[中文](CONTRIBUTING.zh.md)

## Before you start

Skim [AGENTS.md](AGENTS.md) first — it compresses the platform's three layers, its ten design principles and the
command set into one file, including an explicit list of what the platform deliberately doesn't do (AGENTS.md §6).
Each principle is argued in [Design principles and trade-offs](docs/en/06-architecture/05-design-principles.md).
Many "why doesn't it just…" questions already have an answer there. If your change would add something to that list,
open an issue and make the case before writing code — it will most likely have to overturn one of those arguments,
not just add a feature.

## Building and running the tests

```bash
make build            # bin/brickkit + bin/market-server
make test             # unit tests
make test-all         # every suite, including the checklist / regression gates
make lint             # vet + every documentation-consistency check
make hooks            # once per clone: enable the repository's commit hook (.githooks/)
```

**Enable the commit hook once per clone** with `make hooks` (it sets `git config core.hooksPath .githooks`; git never
enables a repository's hooks by itself). When a commit touches `docs/en/`, `docs/zh/`, `AGENTS*.md`, `llms*.txt` or the
generator itself, the hook runs `make generate-llms` — rebuilding the documentation bundles in `llms/` that web AIs
read — and adds them to the same commit (planning documents under `docs/superpowers/` aren't bundled and don't trigger
it). It refuses when one of those files still has unstaged changes, or when `docs/en/` or `docs/zh/` has an untracked
new page: bundles built from the working tree would not match the commit. It also refuses `git commit <paths>`: that
mode runs the hook against a temporary index, so the regenerated bundles couldn't stay staged — `git add` the files and
commit without paths. Stage the whole file (or stash it) and commit again;
without the hook, run `make generate-llms` yourself — `make check-llms` in `make lint` fails on stale bundles.

**No CI runs on pull requests.** The repository's only GitHub Actions workflow (`.github/workflows/release.yml`)
triggers on a `v*` tag push and builds, signs and publishes a release — never on a branch or a PR. So `make lint`
and `make test-all` passing on your machine before you open a PR is the only gate there is. What is red on your
machine is red for every reviewer too.

`make lint` runs `golangci-lint` when it is installed (`make tools-lint` puts it in `.tools/bin`; there is no
repo-specific `.golangci.yml`, so it runs golangci-lint v2's defaults) and falls back to `go vet` otherwise. Install it
before opening a PR — `go vet` alone misses what golangci-lint catches.

`make lint` also runs a set of documentation-consistency scripts (`scripts/check-*.py`): references into the archive,
dangling or unnamed section references, broken links and anchors, commands and flags a doc mentions that don't
exist, a command reference that misses something, YAML field names that don't match the real structs, the
docs/en↔docs/zh mirror, and a few more (the full list, with what each guards, is in the README's
["Build and test"](README.md#build-and-test) section). They aren't decoration: several exist because a past change
broke something no test suite could notice — a renamed flag, a stale example, a link that once pointed somewhere real.

`make lint` also guards the JSON Schemas in `schemas/` (`schemas/component.schema.json`,
`schemas/brickkit.schema.json`, `schemas/deploy.schema.json` — what editors use to complete and check
`component.yaml`, `brickkit.yaml` and the deploy files). They are generated from the Go structs in
`internal/manifest`, `internal/projfile` and `internal/deployfile`, never edited by hand. **Whenever you add, remove
or retype a field there, or change its `omitempty` or a `jsonschema` tag, run `make generate-schemas` and commit the
regenerated files with your change** — otherwise `make check-schemas` (part of `make lint`) fails. That check also
holds the schemas' required fields, closed values, patterns and ranges to what the real validators accept, so a
`jsonschema` tag can't quietly drift away from `Validate`.

## Where tests live

Unit tests live **next to the code they test** (`internal/**/*_test.go`, `market-server/internal/**/*_test.go`);
there is no parallel test tree to keep in sync. `tests/` holds only what genuinely can't sit next to the code:
`tests/checklist/` and `tests/regression/` are acceptance checklists, each line paired with the test that proves it
(both enforced by `make lint`, as the README's "Build and test" table describes), and `tests/components/` holds the
real fixture components that several tests and documentation examples run.

If you're adding behaviour worth a checklist line (a boundary condition, an error case, a compatibility or security
guarantee), add the line to `tests/checklist/清单.tsv` or `tests/regression/清单.tsv` and wire a real test to it — a
line with no test, or a test that no longer exists, fails the build **on purpose** (the README's table says why).

## Documentation conventions

- **`docs/en/` and `docs/zh/` are two independently written, symmetric trees** — not a source plus a translation.
  When you add or change a page in one, the other needs the matching page at the same relative path
  (`make check-docs-bilingual` enforces this), written naturally in that language rather than translated mechanically.
- **CLI output shown in a doc must be real output**, not what you expect the CLI to print. Run the command and paste
  what came back. `make check-doc-fields` checks that every output line in the docs is one the CLI can really print.
- **A YAML field shown in a doc must exist on the real struct** — `make check-doc-fields` checks the docs against the
  real Go types of `component.yaml`, `brickkit.yaml` and the deploy files, not the other way round; the field
  references (`docs/*/11-reference/`) must also cover every field.
- If you change `AGENTS.md` / `AGENTS.zh.md`, keep them genuinely equivalent: each is written independently for its
  language, not translated from the other, but both must cover the same ground.

## Messages and languages

Everything the CLI prints to a person — errors, suggestions, progress lines, `--help` — comes from a **message
catalog**: one file per language in `internal/i18n/locales/` (`en.yaml`, `zh.yaml`). Each line pairs a key with that
language's text:

```yaml
# internal/i18n/locales/en.yaml
cli.release.done: "✅ Released %[1]s: tag %[2]s pushed"
```

```yaml
# internal/i18n/locales/zh.yaml
cli.release.done: "✅ 已发布 %[1]s：tag %[2]s 已推送"
```

The code never holds the text; it names the key through a Go constant and passes the arguments:

```go
opts.Printf("%s\n", i18n.T(msgid.CliReleaseDone, target.Ref(), target.Tag))
```

English is the **source** catalog: every key is declared in `en.yaml` first, and every other catalog has exactly the
same keys, in the same order. The constants in `internal/msgid/messages_gen.go` are generated from `en.yaml`, never
written by hand.

- **Changing a message**: edit its line in `en.yaml` and in every other `locales/*.yaml`. Nothing else changes.
- **Adding a message**: add `key: "text"` to `en.yaml` and, at the same position, to every other catalog; run
  `make generate-msgid`; use `msgid.<Name>` in the code (`cli.release.done` becomes `msgid.CliReleaseDone`). A key reads
  `<scope>.<what it is>`, in lower snake case, where the scope is the package or command (`cli.release.`,
  `configdir.`), and the rest says what the message is (`hint_`, `label_`, `reason_` for those roles). A comment above
  a key in `en.yaml` says when to use it.
- **Adding a language**: copy `en.yaml` to `locales/<code>.yaml`, translate every value, and add `{Code: "<code>"}` to
  `registry` in `internal/i18n/languages.go` — no other Go changes. `BRICKKIT_LANG=<code>` and `brickkit lang set` accept
  it from then on. The AI assistant skills that `brickkit init` installs are translated separately
  (`internal/skills/assets/<code>/`); until they are, a project in that language gets the English skills.
- **Writing the text**: put it in double quotes, or write a multi-line text as a `|` block; other YAML forms are
  refused with the file and line, because YAML would quietly change them (`yes`, a leading space, a `#`). Arguments are
  written by position — `%[1]s`, `%[2]d` — so a translation can reorder them, and each language uses exactly the
  arguments the English entry uses. A few `cobra.*` keys are empty in English: that keeps cobra's own English text.

`make lint` checks all of this: a missing or extra key, a different key order, a placeholder that doesn't match the
English one, a value in the wrong YAML form, and constants out of date with `en.yaml` (`make check-msgid`).
Renaming a key goes through `go run ./tools/i18n/rekey`, which renames it in the code and every catalog at once (see
[tools/i18n/README.md](tools/i18n/README.md)).

## Commit messages and branches

Commit subjects follow a `<type>: <what changed>` shape — `fix`, `feat`, `docs`, `refactor` and `test` are the
common types, optionally with a scope (`docs(zh): …`). `git log` shows the pattern; matching it isn't mandatory, but
it keeps the history easy to scan.

Work on a feature branch and open a PR against `main`. There is no branch protection or required review yet (the
project is young), so in practice a PR is where a change is discussed before it lands, not a technical gate.

## Reporting a bug or proposing a feature

Open a GitHub issue. For a bug, the CLI's own output is usually most of what's needed — `brickkit` writes structured
JSON logs to stderr (`--log-level debug` for more detail) and human-readable output to stdout. Include both, together
with your `brickkit.yaml`, the deploy file and the relevant components' `component.yaml` (blank out secrets in
`config/` first); it saves a round trip. For a feature request that isn't on the
["won't do" list](AGENTS.md#6-what-the-platform-wont-do), explain the problem you're solving, not only the mechanism you
have in mind — the bar a new mechanism has to clear is the
[ten design principles](AGENTS.md#2-design-principles-ten).
