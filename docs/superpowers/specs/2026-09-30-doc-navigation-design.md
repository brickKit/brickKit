# Documentation navigation: one entry per reader, a code map, and fetchable bundles

Date: 2026-09-30. Status: design, approved in conversation; this is the written spec.

## 1. Goal

Three standards, set by the maintainer and binding on every later documentation change (in priority order):

1. **Local development (most important).** An AI working in this repository finds any feature's code and any document
   quickly through `AGENTS.md`.
2. **Web / API readers.** When asked to read the whole project, an AI reads all documentation in the fewest fetches;
   when asked a question, it routes to the right page quickly.
3. **GitHub readers.** `README.md` makes every kind of documentation easy to reach.

Component documentation (the per-component spec, the project map, `brickkit new`'s skeleton, the skills installed
into user projects) follows the same standards but is a **separate task**, out of scope here.

## 2. What is wrong today

- `llms.txt` / `llms.zh.txt` carry 97 absolute `https://raw.githubusercontent.com/brickKit/brickKit/main/…` links
  each, `AGENTS.md` / `AGENTS.zh.md` 19 each: ~55 characters of the same prefix repeated ~230 times, and a local AI
  gets URLs where it wants paths it can open.
- `AGENTS.md` has no code map: not one `internal/` path. Finding where a feature lives means searching 37 packages.
- No way to read everything in a few fetches: 94 pages per language (~725 KB English, ~650 KB Chinese), one fetch each.
- The README's AI box points only at `AGENTS.md` and `llms.txt`.

## 3. Entry points: one reader, one first file

| Reader | First file | How it locates things |
| --- | --- | --- |
| Local development AI | `AGENTS.md` (Claude Code loads it every session via `CLAUDE.md`) | Its **doc map** and **code map**, all repository-relative paths it can open directly |
| Web / API AI | `llms.txt` (`llms.zh.txt` for Chinese) | Two routes at the top: read everything → bundles 00…NN in order; answer a question → pick a page by its one-line description |
| GitHub reader | `README.md` | A navigation line under the introduction, and "Where to go next" |

## 4. Link policy

- **Hand-written files** (`AGENTS*.md`, `llms*.txt`, `README*.md`, `docs/`) use repository-relative links.
  `AGENTS*.md` and `llms*.txt` state the web prefix **once**, near the top: "On the web, prefix every path with
  `https://raw.githubusercontent.com/brickKit/brickKit/main/`."
- **Exceptions that stay absolute:** the README's AI box (an AI reading GitHub's HTML page can't know the raw prefix),
  the `curl … install.sh` commands, and JSON Schema `$schema` URLs (editors fetch them).
- **Generated bundles** use absolute URLs for navigation (the next part, and where the manifest lives): they are read
  raw on the web, and being generated they cost nothing to keep right.
- **Guard (check-docs):** in `AGENTS.md`, `AGENTS.zh.md`, `llms.txt`, `llms.zh.txt` the raw prefix appears exactly
  once. A second occurrence fails lint.

## 5. `AGENTS.md` / `AGENTS.zh.md`

Written independently in each language, same structure.

- Top: one line on paths ("relative to the repository root; on the web, prefix …; to read everything at once start
  from `llms/en/00-core.md`").
- Keep §1–§8 (what BrickKit is, principles, the three layers, shells, commands, what it won't do, migration,
  distribution), tightened where they repeat the docs.
- **New: Code map.**
  - *Packages*: every directory under `internal/` (37 today) and `cmd/`, one row each: what it owns, in one line.
  - *Features → code*: each command and each cross-cutting feature → entry file(s) → core packages. Examples:
    `up` → `internal/cli/up.go` → `cascade`, `compose`, `k8s`, `inject`, `procsup`; focus run →
    `internal/cli/focus.go`, `internal/cascade`, `internal/deployfile/focus.go`; completion →
    `internal/cli/complete.go`, `internal/source/cached.go`.
  - *Tests and checks*: where unit tests, fixtures (`internal/cli/testdata/`, `tests/components/`), the regression
    checklist (`tests/checklist/`) and the lint scripts (`scripts/check-*.py`) live; how to run them (`make lint`,
    `make test-all`).
- **§9 becomes the Doc map**: "to find out X → read `docs/en/…`", relative paths, grouped by task. The page-by-page
  list is `llms.txt`'s job; `AGENTS.md` does not repeat it.
- **Guards (Go test in `tests/docfields`):**
  - every package directory under `internal/` and `cmd/` appears in both code maps, and no row names a package that
    no longer exists;
  - every path written in either code map exists.

## 6. `llms.txt` / `llms.zh.txt`

- Keep the llmstxt.org shape: title, one-paragraph summary, sections of `- [path](path): one-line description`.
- Top, before the sections: the web prefix line, then
  - **Read everything:** the bundle list — `llms/en/00-core.md` (core), then `01.md` … `NN.md` in order;
  - **Answer a question:** "find the page by its description below, fetch just that page".
- All page links relative. Descriptions stay: they are the router.
- The bundle list is generated into `llms.txt` between markers (`<!-- llms:bundles:begin -->` … `end`) by the same
  generator, so the part count never drifts.

## 7. Bundles

### Layout

`llms/en/00-core.md`, `llms/en/01.md` … `llms/en/NN.md`; the same under `llms/zh/`. Committed to the repository
so raw URLs work; generated, never edited by hand.

### Content and order

- **00-core** — to understand and evaluate BrickKit from one fetch: `AGENTS.md` (or `AGENTS.zh.md`), then
  `00-intro/01-what-is-brickkit.md`, `00-intro/02-quick-start.md`, `00-intro/04-core-concepts.md`,
  `00-intro/06-fractal-architecture.md`, `01-three-layers/01-overview.md`, `01-three-layers/08-resolution-priority.md`,
  `01-three-layers/09-field-reference.md` (~71 KB English today). The list lives in the generator.
- **01…NN** — every other page of `docs/<lang>/` in reading order (folder number, then `README.md` first, then file
  number), packed greedily: add pages to the current part until the next one would exceed the budget, then start a
  new part. A page is never split.
- Every page appears in exactly one file across 00…NN: reading them all is reading everything, once.

### Budget

100,000 bytes per file, including headers. The generator **fails loudly** (non-zero exit, names the file and size)
when 00-core exceeds it, or when a single page alone exceeds it — it never truncates.

### Format of each file

```
# BrickKit documentation — part 2 of 8 (English)

Contains: docs/en/02-project-guide/README.md, docs/en/02-project-guide/01-init-and-project-creation.md, …
Paths below are relative to the repository root: https://raw.githubusercontent.com/brickKit/brickKit/main/
Next: https://raw.githubusercontent.com/brickKit/brickKit/main/llms/en/03.md      (the last part says it is the last)

---

> File: docs/en/02-project-guide/README.md

<the page, with relative links rewritten>

---

> File: docs/en/02-project-guide/01-init-and-project-creation.md
…
```

- Part 00's title says "core"; its header adds "for the rest, read 01 … NN".
- **Link rewriting:** a relative link in a page (`../01-three-layers/04-deploy-local-yaml.md#x`) becomes
  repository-relative (`docs/en/01-three-layers/04-deploy-local-yaml.md#x`), so inside a bundle every link still
  names its file. External links are untouched.
- Output is deterministic: same input, byte-identical output.

### Generator

- `cmd/gen-llms` (Go, like `cmd/gen-msgid` / `cmd/gen-schemas`): writes `llms/` and the marker block in
  `llms*.txt`; `-check` compares instead of writing and fails with the file that differs.
- `make generate-llms`; `make check-llms` runs in `make lint`.

## 8. Keeping bundles current: a versioned pre-commit hook

- `.githooks/pre-commit` (POSIX sh, in the repository): when the staged files include `docs/`, `AGENTS.md`,
  `AGENTS.zh.md`, `llms.txt` or `llms.zh.txt`, it runs the generator and `git add llms/ llms.txt llms.zh.txt`, so
  every commit carries matching bundles.
- It refuses (exit 1, says why) when one of those files has unstaged changes: the generator reads the working tree,
  and bundles built from half-staged files would not match the commit. Stage the whole file, or commit with
  `--no-verify` and regenerate afterwards.
- Without Go on `PATH` it prints a warning and lets the commit through: `make lint` (`check-llms`) is the backstop.
- `make hooks` enables it once per clone (`git config core.hooksPath .githooks`); git never enables hooks by itself.
  `CONTRIBUTING.md` / `CONTRIBUTING.zh.md` say so.
- The stray `.git/hooks/pre-commit` in the maintainer's clone (left by an accidental `brickkit init --hooks` in this
  repository, pointing at a deleted temporary binary) is removed; `core.hooksPath` would bypass it anyway.
- Why pre-commit and not pre-push: at push time the commits already exist; regenerating then would leave the new
  bundles outside every pushed commit.

## 9. README (English and Chinese)

- Under the introduction, one navigation line: Quick start · The three layers · Running a project · Writing
  components · CLI reference · Troubleshooting · All docs · For AI.
- The AI box becomes three routes, absolute URLs kept: developing locally → `AGENTS.md`; read everything → the
  bundles, starting at `llms/en/00-core.md`; answer a question → `llms.txt`.
- "Where to go next" stays; its AI sentence points at the bundles too.

## 10. Other pages that change

- `CONTRIBUTING*.md`: `make hooks`, `make generate-llms`, what the hook does and its refusal.
- `docs/{en,zh}/08-ai-guide/`: how an AI reads this repository (local: `AGENTS.md` maps; web: bundles or
  `llms.txt` routing).
- `llms*.txt`: entries for the bundles' directory and anything new above.

## 11. Testing

- Generator unit tests: greedy packing respects the budget; a page is never split; every page appears exactly once
  across 00…NN; core over budget fails; a single page over budget fails; link rewriting (parent directories, anchors,
  external links untouched); byte-identical output on a second run; `-check` reports a stale file.
- Code-map guard: a package missing from either map fails; a row for a deleted package fails; a non-existent path
  fails (each proven by a mutation).
- Raw-prefix guard: a second occurrence in `AGENTS.md` or `llms.txt` fails (mutation).
- Hook: a shell test with a temporary repository — docs change regenerates and stages `llms/`; a half-staged doc
  file refuses; no docs change does nothing.
- `make -k lint` and `make -k test-all` stay green.

## 12. Out of scope

- Component documentation (the separate task): the project map's "what each component does", a component README for
  GitHub, a component's own code map, `brickkit new`'s skeleton, the skills and `AGENTS.md` installed into user
  projects.
- A single full-content file: the parts already let a long-context model read everything in a few fetches.
