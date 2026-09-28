# P9: documentation for the three-layer model

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** every page of the new documentation tree exists in both languages, describes the code as it is, and
`make lint && make test-all` are green — the red list P8 handed over (roadmap row P9) is empty.

**Architecture:** documents are written module by module, Chinese first (`AGENTS.zh.md` → `llms.zh.txt` →
`README.zh.md` → `docs/zh/`), then English written independently from the finished Chinese tree. The P8 doc
guards are the tests: each task ends with the guards scoped to the files it wrote showing no finding. Three guards
change shape with the new AGENTS structure (principles, AGENTS § anchors, raw links) and one gains a check this
plan needs (link `#anchors`).

**Tech Stack:** Markdown; Python guards in `scripts/`; Go guards in `tests/docfields`, `tests/regression`;
the `brickkit` binary (`make build-cli`) for every example and every piece of output.

**Spec:** `new_plan/文档修改-布局.md` (page list and per-page outline), `new_plan/文档修改-本地文件.md` (structure
of AGENTS / llms / README), `new_plan/提案.md` (facts; **Appendix A overrides the body**), `new_plan/命令表.md`
(command surface), roadmap row P9 (`docs/superpowers/plans/2026-09-26-three-layer-refactor-roadmap.md`).

## Global Constraints

- **Facts come from the code, then Appendix A, then the proposal body, then the two doc specs.** The doc specs
  give structure, page names and outlines; where a fact in them disagrees with the code or Appendix A, the code
  wins and the disagreement is recorded as a ruling (the known ones are in Decisions).
- Every example is run for real with `bin/brickkit` in a scratch project; every output block is pasted from a
  real run in the page's language (`BRICKKIT_LANG=zh` / `en`). Nothing is typed from memory.
- Examples use the minimal fixtures in `tests/components/` (`demo/hello`, `demo/caller`, …), not new realistic
  components.
- Assume the reader knows none of the ideas: each concept is first said plainly, then what it buys and what it
  costs, then what BrickKit does and why not more. README stays short; overview tables are readable at a glance
  and link to numbered sections.
- `docs/en` and `docs/zh` are symmetric mirrors, not translations: same files, same facts, each written naturally
  in its language. When one side is better organised, raise the other to it.
- No references to archived material (`archive/`), no "旧设计书/开发计划/决策记录" pointers; every `§` names its
  document (`提案 §x`, `附录 Ax`, `AGENTS.md §x`, `RFC n §x`).
- Tutorials stay empty directories (Appendix A13); no page links a tutorial file that does not exist.
- Commit after every task on `main`, message ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Tests that walk types or can loop run under `systemd-run --user --scope -q -p MemoryMax=2G -p MemorySwapMax=0`.

## Review Focus

1. **A fact copied from a doc spec instead of the code** (command count, `publish`, cache location, per-command
   flags) — a reader following it gets an error. Guarded by `check-cli-docs` (commands, flags, counts) and by the
   Decisions list; the final review checks every Decision against the pages.
2. **A link to a section anchor that does not exist** (`page.md#some-heading`) — `check-docs` ③ only checks the
   file today, so a renamed heading silently breaks the link. Task 1 adds anchor checking (GitHub slug rules,
   CJK headings included) with a self-check.
3. **A YAML example the CLI rejects** — covered for `component.yaml` / `brickkit.yaml` / `deploy.yaml` blocks in
   AGENTS/README by `TestDocSkeletonsUseOnlyKnownFields`; Task 1 widens its scan to every live markdown page.
4. **Output that no longer matches the CLI** — `TestDocOutputLinesConformToCatalog` (≥200 checked lines per
   language) plus the rule that output is pasted from real runs.
5. **English and Chinese pages drifting in facts** — mirror existence is checked; facts are checked by the
   language-specific guards on both sides (CLI reference, error codes, field references, principles, outputs).

## Decisions

- **Command count is 21 business commands** (`publish` stays next to the new `release`, Appendix A14; `login` /
  `logout` work, they are not "reserved"). The local-files spec's "20 个命令" and "`publish` → `release`" are
  corrected in AGENTS, README, llms and the CLI reference.
- **Bare-repo cache is user-level** (`<UserCacheDir>/brickkit/repos`, Appendix A12), not `.brickkit/repos/`.
- **Flags are documented where the CLI has them.** `-f/--file` and `--no-local` are per-command flags of the
  commands that read deploy files; the CLI reference lists them under each such command and explains them once
  in a shared section. `--log-level` is the only global flag.
- **Repository structure blocks list the real packages** (`internal/projfile`, `deployfile`, `configdir`,
  `project`, `install`, `shell`, …), not the spec's `internal/config`.
- **AGENTS follows the local-files spec structure** (`## §1`…`## §9`, ten principles in §2). Consequences, all in
  Task 1: `tests/docfields/principles_test.go` reads §2's numbered bold list (`1. **大声失败**：…`), count 10,
  mirrored by `06-architecture/05-design-principles.md` section "十条原则" / "The ten principles"; `check-docs`
  `heading_numbers` accepts `## §N` headings; the code's `AGENTS.md §9.12 / §9.9 / §9.2` references (lint help
  text included) are re-pointed to the live page that now carries that argument.
- **AGENTS §9 and llms carry raw links; both are link-checked.** `check-docs-bilingual`'s raw-link check covers
  `AGENTS.md` and `AGENTS.zh.md` as well as the llms files.
- **Write order:** AGENTS/llms/README first (they fix structure), docs module by module in the spec order
  00→01→07→02→03→04→05→06→08→09→10→11, English after Chinese. The closing task revisits AGENTS/llms/README so the
  routing matches the pages actually written (the layout spec's "AGENTS after the docs" concern).
- **`09-patterns/05-deployment-selection.md`** is written from behaviour the tests prove (topologies, targets,
  local modes); no "to be completed" markers.
- **CLI reference layout** (`07-cli-reference/README.md`): one `## \`brickkit <cmd>\`` per command, one
  `### \`brickkit <cmd> <sub>\`` per subcommand, a flag table under each; a shared-flags section; a "what
  changed" section written as tombstones (removed commands named with deletion wording, `check-cli-docs`
  TOMBSTONE). Numbering like `### 6. \`brickkit add …\`` is allowed — the parser accepts it.
- **Error-code reference** (`06-architecture/09-error-codes.md`): one `### CODE` per `clierr.Code*` (including
  `NOT_IMPLEMENTED`, said to be produced by no command today), tables whose first column holds real error titles
  in backticks in the page's language (≥40 titles).
- **Field references** (`11-reference/01–03`): tables whose first column is the full field path in backticks,
  every field of `manifest.Manifest` / `projfile.File` / `deployfile.File`, nothing else.

## Tasks

Each task: write the pages from the outline in `new_plan/文档修改-布局.md` (line ranges given), check every fact
against the code, run every example, then run the task's verification and commit. "No finding" below means: the
guard's output lists no file this task wrote (other files may still be red until their task).

### Task 1: Chinese root files and the guards they reshape

**Files:** `AGENTS.zh.md`, `llms.zh.txt`, `README.zh.md`, `CONTRIBUTING.zh.md`; `tests/docfields/principles_test.go`,
`tests/docfields/docfields_test.go` (`docs()` scans every live markdown page), `scripts/check-docs.py`
(`## §N` anchors, `#anchor` link checking), `scripts/check-docs-bilingual.py` (AGENTS raw links); code that cites
`AGENTS.md §9.x` (`internal/cli/lint.go`, `internal/i18n/catalog_{en,zh}.go` `CliLintLong`,
`internal/manifest/types.go`, `internal/manifest/edge_test.go`, `internal/schemagen/*.go`, `internal/cli/up_test.go`).

- [ ] Guards first (TDD): add self-check samples to `check-docs.py` for `## §2 核心设计原则` → anchor `2`, and for
  `#anchor` links — a link to an existing CJK heading passes, a misspelled anchor fails; run it, watch the
  self-check fail; implement GitHub's slug rule (lowercase, drop punctuation except `-`, spaces → `-`, keep CJK,
  numbered duplicates `-1`); watch it pass.
- [ ] `principles_test.go`: `agentsPrinciples` reads `## §2`'s numbered list `N. **名字**：…`; `principleCount = 10`;
  pairs `{"en","AGENTS.md","The ten principles"}`, `{"zh","AGENTS.zh.md","十条原则"}`; update its detector
  self-test to the new shape; run it against a synthetic markdown in the test (red → green).
- [ ] `docfields_test.go` `docs()`: every live `.md` (git-listed plus untracked, same exclusions as `check-docs`),
  not only AGENTS/README; `TestDocSkeletonsUseOnlyKnownFields` still ≥4 checked.
- [ ] `check-docs-bilingual.py`: raw-link check also over `AGENTS.md` / `AGENTS.zh.md`; self-check unchanged.
- [ ] Write `AGENTS.zh.md` (structure: local-files spec §二; facts: Decisions), `llms.zh.txt` (spec §三, minus
  tutorial links), `README.zh.md` (spec §一, facts corrected), fix `CONTRIBUTING.zh.md` structure references.
- [ ] Re-point the `AGENTS.md §9.x` references in code to the page that will carry the argument
  (`docs/{en,zh}/03-component-guide/03-config-schema-design.md` for "configSchema is a spec sheet",
  `docs/{en,zh}/06-architecture/05-design-principles.md` for the others); rebuild; `go test ./internal/...`.
- [ ] Verify: `python3 scripts/check-docs.py` — no finding in the four root files (links into `docs/zh/` pages not
  yet written are expected); `python3 scripts/check-cli-docs.py bin/brickkit` — no finding in them;
  `go test ./tests/docfields -run 'Principles|Skeleton'` — the principle test fails only on the missing
  `05-design-principles.md`. Commit.

### Task 2: `docs/zh/00-intro/` (layout lines 69–250)

**Files:** `docs/zh/README.md` (the tree's index: every module, reading order), `README.md`,
`01-what-is-brickkit.md`, `02-quick-start.md`, `03-core-concepts.md`, `04-comparison.md`,
`05-fractal-architecture.md`.

- [ ] Quick start run end to end in a scratch directory with `demo/hello` (init → add → up → curl → change
  config → up → down); outputs pasted from that run. Docker is available on this machine; if `up` cannot run,
  stop and report rather than inventing output.
- [ ] Verify: `check-docs.py` no finding under `docs/zh/00-intro/`; `check-cli-docs.py` no finding there. Commit.

### Task 3: `docs/zh/01-three-layers/` (layout lines 251–524)

**Files:** `README.md`, `01-overview.md` (draws the `.brickkit/` tree), `02-brickkit-yaml.md`, `03-deploy-yaml.md`,
`04-deploy-local-yaml.md`, `05-config-directory.md`, `06-vars-and-var-ref.md`, `07-sensitive-values.md`,
`08-resolution-priority.md`, `09-field-reference.md`.

- [ ] `02-brickkit-yaml.md` states the lock-file rule from the roadmap row: an undeclared required dependency is
  an error with an `add` hint, an undeclared optional one is simply absent.
- [ ] Verify: `python3 scripts/check-doc-tree.py bin/brickkit` passes (it only needs this page);
  `TestDocSkeletonsUseOnlyKnownFields` no finding in this module; `check-docs.py` no finding here. Commit.

### Task 4: `docs/zh/07-cli-reference/README.md` (layout lines 1533–1550, `new_plan/命令表.md`)

- [ ] Every command, subcommand and flag from `bin/brickkit … --help` (not from the command table where they
  differ); per-command sections as in Decisions; examples run for real.
- [ ] Verify: `python3 scripts/check-cli-docs.py bin/brickkit` — no "命令参考没写全" finding for
  `docs/zh/07-cli-reference/README.md`, no nonexistent command/flag in it. Commit.

### Task 5: `docs/zh/02-project-guide/` (layout lines 525–824)

**Files:** `README.md` and `01-…` to `11-build-and-images.md` (twelve files).

- [ ] `06-upgrade-and-migration.md` states the two known limits of config migration: comments at the head of an
  old config file are not carried over, and a `|+` block loses its trailing blank lines.
- [ ] Verify: `check-docs.py`, `check-cli-docs.py`, skeleton test — no finding in this module. Commit.

### Task 6: `docs/zh/03-component-guide/` (layout lines 825–1081)

**Files:** `README.md` and `01-…` to `10-git-distribution.md` (eleven files).

- [ ] Verify as Task 5. Commit.

### Task 7: `docs/zh/04-shell/` and `docs/zh/05-migration/` (layout lines 1082–1337)

**Files:** 04-shell: `README.md`, `01-…` to `08-shell-config.md`; 05-migration: `README.md`, `01-…` to
`04-shell-interaction.md`.

- [ ] Must cover the P9 items from the roadmap row: shell member versions (A24, the three ways out of a
  mismatch), merge cycles and `skipWaitFor` with its cost (A23), checking member compatibility after upgrades
  (A22), hosted members' labels / health checks not applied, why `add` does not offer a shell for a member.
- [ ] Verify as Task 5. Commit.

### Task 8: `docs/zh/06-architecture/` (layout lines 1338–1532)

**Files:** `README.md`, `01-pipeline-overview.md`, `02-dependency-resolution.md`, `03-env-injection-contract.md`,
`04-deploy-file-generation.md`, `05-design-principles.md`, `06-bare-repo-mechanism.md`, `07-cache-design.md`,
`08-security-and-signing.md` (incl. the NetworkPolicy enforcement caveat), `09-error-codes.md` (incl.
`RELEASE_BLOCKED` / `RELEASE_PUSH_FAILED` from P7).

- [ ] Verify: `go test ./tests/docfields -run 'ErrorCodes|Principles'` passes for zh; `check-docs.py` no finding
  here. Commit.

### Task 9: `docs/zh/08-ai-guide/`, `09-patterns/`, `10-troubleshooting/`, `11-reference/` (layout lines 1551–1937)

**Files:** 08: 5 files; 09: 6; 10: 9 (01 includes the Podman AppArmor `down` failure the CLI hint points at);
11: `README.md`, `01-component-yaml-schema.md`, `02-brickkit-yaml-schema.md`, `03-deploy-yaml-schema.md`,
`04-config-schema-spec.md`, `05-json-schemas.md` (`06-market-api.md` exists; bring it in line if needed).

- [ ] Also covers from the roadmap row: SSH host-key / passphrase prompts bypass `GIT_TERMINAL_PROMPT`; offline
  `add <id>` without a version fails because "latest" needs the remote's tags.
- [ ] `06-market-api.md` already exists: keep `make check-market-api` green if it is touched.
- [ ] Verify: `go test ./tests/docfields -run 'YAMLReferences'` passes for zh; `check-docs.py` finds nothing under
  `docs/zh/`; `TestDocOutputLinesConformToCatalog` zh side ≥200 checked lines. Commit.

### Task 10: English root files

**Files:** `AGENTS.md`, `llms.txt`, `README.md`, `CONTRIBUTING.md` — written from the finished Chinese tree,
independently phrased.

- [ ] Verify: principles test en side fails only on the missing English principles page; `check-cli-docs.py` no
  finding in these files. Commit.

### Tasks 11–14: `docs/en/` modules

11: `docs/en/README.md`, `00-intro`, `01-three-layers`, `07-cli-reference`; 12: `02-project-guide`, `03-component-guide`;
13: `04-shell`, `05-migration`, `06-architecture`; 14: `08-ai-guide`, `09-patterns`, `10-troubleshooting`,
`11-reference`. Same verification per module as the Chinese tasks, English side; outputs captured with
`BRICKKIT_LANG=en`. Commit after each.

### Task 15: Close

- [ ] Re-read AGENTS / llms / README in both languages against the pages written; fix routing and summaries.
- [ ] `systemd-run … make -k lint` and `make -k test-all` — both fully green (R41 included).
- [ ] Roadmap: P9 row marked done with the outcome; memory updated.
- [ ] opus whole-phase review (factual accuracy against code is the main axis); one fix pass; commit.

Verify after each task as well: `go build ./... && go vet ./...`, and for any task that touched Go code
`go test ./internal/... ./tests/...` under the memory cap.
