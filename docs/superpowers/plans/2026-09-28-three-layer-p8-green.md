# P8: back to green (fixtures, checklists, guards against the new tree)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** everything that does not depend on P9's documents is green again, and every guard that does
depend on them points at the new documentation layout, so its remaining failures are exactly P9's to-do list.

**Spec:** `new_plan/提案.md` (Appendix A binding; A13 tutorials are empty directories for now), the new
layout `new_plan/文档修改-布局.md`, roadmap row P8.

## Survey (2026-09-28, before any change)

- `make test-all`: only `tests/checklist` and `tests/regression` are red — 32 rows point at tests that were
  deleted or renamed in P1–P7 (19 checklist, 13 regression) — plus one evidence file path under the moved
  archive.
- `make lint`:
  - `check-doc-tree` reads the deleted `internal/config/layout.go`.
  - `check-docs` looks for design books in `docs/archive/`; the archive now lives at `archive/v1/`.
  - `check-docs-bilingual` self-check assumes the old tree's document count.
  - `check-guide-output` + `tests/guides` + `scripts/check-guides.sh` are all bound to the old
    `docs/*/03-guide/` tutorials; A13 leaves tutorials empty.
  - `check-cli-docs` counts commands in AGENTS/README/llms (old content).
  - `check-doc-fields` needs the error-code, principles, field-reference and CLI-reference documents (P9).
- 6 of 10 `tests/components` still declare `dependencies.resources`; configSchema keys are camelCase
  (`logLevel`) although A10 makes the key the environment variable name the code reads (`LOG_LEVEL`).
- `.github/smoke.sh` runs `init smoke-shop` expecting the old in-place init; `init <name>` now creates `./<name>/`.
- ~1000 comment/test lines outside `archive/` cite archived design books (`005 §5.13.1`, `design/004`),
  archived decisions (`D47`) and the archived development plan (`开发计划 18.6`). None is user-visible.

## Decisions

- **Doc guards target the new layout; P9 turns them green.** Old path → new path:
  - error codes `06-architecture/10-error-codes.md` → `06-architecture/09-error-codes.md`
  - principles `06-architecture/01-design-principles.md` → `06-architecture/05-design-principles.md`
  - component.yaml reference → `11-reference/01-component-yaml-schema.md`; brickkit.yaml →
    `11-reference/02-brickkit-yaml-schema.md`; deploy files → `11-reference/03-deploy-yaml-schema.md` (new
    guard: every deploy-file field documented)
  - CLI reference `06-architecture/09-cli-reference.md` → `07-cli-reference/README.md`
  - CLI hints and scripts that print a doc path follow the same map; a guard checks that every doc path
    written in code exists (red until P9 writes the page).
  Cost if wrong: `make lint` stays red after P8 by design; the red list is written into the roadmap's P9 row.
- **Archived references are removed from current code, not re-pointed.** Current content either states the
  reason itself or cites the live spec (`提案 §x`, `附录 Ax`). `check-docs` ① now checks that every
  `提案 §` / `附录 A` reference exists in `new_plan/提案.md`, and fails on references to retired books,
  decisions or plan steps outside `archive/`. Cost if wrong: a reader loses a pointer into history; the
  archive is still in the repository.
- **Tutorial output checking is retired until tutorials exist.** `check-guide-output`, `check-guides`,
  `tests/guides` are deleted (history keeps them); the roadmap records that the tutorials phase re-creates
  a checker against `tutorials/`.
- **Checklist rows are re-proved, not dropped.** A row whose promise still holds is pointed at the test that
  proves it today; if none exists, the test is written (and the behaviour fixed if it regressed). A row
  whose concept was removed by the spec (resources, `servedBy`, `override.yaml`, `--config`) is deleted
  with the spec reference in the commit message.
- **Test components follow the three-layer model.** `dependencies.resources` goes; connection settings are
  ordinary configSchema keys named as the code reads them (`DATABASE_HOST`, password `secret: true`);
  camelCase keys become the env names the code already reads. `make lint` gains `check-components`: every
  `tests/components/*/component.yaml` passes `brickkit lint`. A dry-run test assembles all of them into one
  three-layer project (`add --local`, `up --dry-run` on docker and k8s).

## Tasks

1. **Test components.** Rewrite the ten `component.yaml` files, their `component_test.go` checks and the README
   sections about resource binding; `check-components` in lint; the assembled-project dry-run test in
   `internal/cli` (reads `tests/components`). `make test-components` stays green.
2. **Smoke and retired guide checks.** Fix `.github/smoke.sh` for `init <name>` and run it against a built
   binary; delete `scripts/check-guide-output.py`, `scripts/check-guides.sh`, `tests/guides/`, their Makefile
   targets and comments.
3. **Checklists.** Re-prove the 32 rows (see Decisions); fix the record's evidence path. `make test-boundary
   test-error test-compat test-security test-regression` green.
4. **Archived references.** Scripted removal of pure-reference parentheticals, then a manual pass over the
   rest; `check-docs` ① rewritten as above, its link check ② covering live markdown only (`docs/`, top-level
   docs, `deploy/`, `tests/components`), its old-guide checks ③④ removed with the old guides.
5. **Doc guards against the new tree.** `check-doc-tree` → `internal/project/layout.go`; `check-docs-bilingual`
   self-check uses a known live file instead of a count; `tests/docfields` and `check-cli-docs` re-pointed per
   the map, deploy-file reference guard added; code/script doc paths re-pointed; "doc paths in code exist"
   guard.
6. **Close.** `make test-all` green; `make lint` failures limited to P9-owned content; roadmap P8/P9 rows
   updated with the exact red list; opus review; one fix pass.

Verify after each task: `go build ./... && go vet ./...`, `go test ./internal/... ./tests/...`,
`.tools/bin/golangci-lint run ./...`, `make check-i18n cover-check test-market`, plus the task's own targets.
