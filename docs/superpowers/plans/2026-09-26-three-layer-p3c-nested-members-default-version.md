# P3c: Nested Shell Members and the Default Version

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Three user decisions after P3b:
- (A) A component version has one "default" line in `brickkit.yaml`. A version that exists only because another component depends on it carries `requiredBy: [<ids>]`.
- (B) A shell's members are written as full deploy entries nested under the shell entry. The file then holds each component version once, and a member's own fields are what it uses when the shell doesn't run.
- (C) An unversioned name — bare deploy entry id, unversioned config file — always means the default version.

**Architecture:** `projfile` learns `requiredBy` and `DefaultVersion`. `deployfile.Component.Members` becomes `[]Component`, one level deep, and every lookup goes through a flattened view. `project` resolves a bare entry to the default version only; that single rule drives coverage, hosting (`ShellOf`), config file choice and entry lookup. P3b's "highest version" rule and the `members: [id@version]` string form disappear. The "fields ignored while hosted" warning disappears: nested fields are the member's standalone configuration by construction.

**Tech Stack:** Go 1.22, testify; packages `internal/{projfile,deployfile,project,shell,compose,k8s,cli,schemagen}`.

**Spec:** `new_plan/提案.md` §5.4, §6.1, §8.4, Appendix A (A5 revised here). The user's decisions from the conversation of 2026-09-26 are recorded as A20–A22 in Task 3.

## Global Constraints

- Messages go through `internal/msgid` plus both catalogs. `go test ./tests/i18nguard/...` stays green, and unused msgids are removed.
- Comments in Chinese, matching the surrounding style.
- Commit on main. The message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- After each task: `go build ./...`, the task's tests, the i18n guard, and `.tools/bin/golangci-lint run` on touched packages.
- Out of scope, later phases:
  - `add`/`upgrade` writing and maintaining `requiredBy` (P4/P6);
  - the local repo version check (P4);
  - user docs (P9).

## Review Focus

1. **Two versions, second one `requiredBy`, bare deploy entry `erp/api` only:** the second version is reported as a missing entry. It must not silently inherit the default version's `expose` or ports.
2. **Nested member `erp/api@2.0.0` (a `requiredBy` version) under the shell, default `erp/api` at top level:** the shell hosts 2.0.0 and 1.2.0 runs standalone. Both entries count toward coverage.
3. **Nested member with `mode: disable` / `mode: debug`, or with its own `localPort` / `exposePort`:** the single-file checks cover nested entries (port conflicts across top level and members, debug only in the local file). Error field paths read `components[0].members[1].…`.
4. **Config:**
   - `config/erp-api.yaml` applies to the default version only.
   - A `requiredBy` version without `config/erp-api@<v>.yaml` gets no config and no ambiguity error.
   - An unversioned file plus `config/erp-api@<default>.yaml` for the same default is an error, because two files claim one version.
5. **Validation of `requiredBy`:**
   - several default lines for one ID → error;
   - no default line → error;
   - a `requiredBy` naming an undeclared ID or itself → error;
   - `requiredBy` on a shell → error.

---

### Task 1: `requiredBy` and the default version (`projfile`)

**Files:** `internal/projfile/{projfile.go,validate.go}` (+tests), msgids/catalogs, `schemas/brickkit.schema.json` (regenerate).

**Rules:**
- `Component.RequiredBy []string` (`yaml:"requiredBy,omitempty"`): IDs of the components that pinned this version.
- Validation per ID:
  - exactly one line without `requiredBy` (`ProjfileDefaultMissing` / `ProjfileDefaultTwice`);
  - each `requiredBy` entry is a declared ID other than the component itself (`ProjfileRequiredByUnknown` / `ProjfileRequiredBySelf`);
  - a shell never carries it (`ProjfileRequiredByShell`).
- `(*File).DefaultVersion(id) (string, bool)` and `(*File).IsDefault(id, version) bool`.

- [ ] Step 1: write the failing tests: `TestDefaultVersion`, `TestValidateRequiredBy` (table covering the five errors plus one valid case).
- [ ] Step 2: run them and confirm FAIL. Step 3: implement. Step 4: run `go test ./internal/projfile/` and confirm PASS.
- [ ] Step 5: commit `feat(projfile): requiredBy marks a version kept only for a dependent`.

### Task 2: Nested members and bare-means-default (`deployfile`, `project`, consumers)

**Files:**
- `internal/deployfile/{deployfile.go,validate.go}` (+tests)
- `internal/project/{check.go,load.go,configload.go}` (+tests)
- `internal/shell/check.go`
- `internal/compose/servedby.go`, `internal/k8s/servedby.go` (drop the ignored-fields warning and its msgids)
- `internal/cli/status.go` (team entry lookup)
- `internal/inject/inject.go` (`configFileFor`)
- `internal/project/projecttest/entries.go`, `internal/cli/testsupport_threelayer_test.go`
- fixtures under `internal/cli/testdata/`
- schema regeneration

**Rules:**
- `Component.Members []Component`. A member entry may not have `members` (`DeployfileMemberNested`).
- `(*File).All() []Located` returns every entry with its field path and its shell entry id (`""` at top level). All single-file checks run over `All()`: id format, duplicates, mode, ports, replicas, labels.
- `(*File).Entry(id, version string, isDefault bool)`: an exact `id@version` entry wins. A bare entry covers the version only when `isDefault`. Both nested and top-level entries are searched.
- `project`:
  - `DeployEntry` passes `Decl.IsDefault`.
  - Coverage: every declared version is covered exactly once, by its `id@version` entry or, for the default, by the bare entry. A bare entry and `id@<default>` together are a duplicate. An entry that covers nothing is extra.
- Members:
  - nested entries are allowed only under a `kind: shell` entry;
  - a member may not be a shell;
  - one ID is hosted in only one shell, and one shell hosts only one version of an ID.
  - `shellOf` is keyed by `id@version`.
- Config:
  - the unversioned file belongs to the default version;
  - `id@v` files belong to v;
  - an unversioned file plus a versioned file for the default version → `ProjectConfigTwoFilesForDefault`;
  - the old ambiguity error disappears;
  - `configFileFor` names the unversioned file for the default version and the versioned file otherwise.
- Remove `highestVersion`, `DeployfileMemberBadVersion`, `ProjectMemberVersionUndeclared` and the `ServedFieldsIgnored` warning paths.

- [ ] Step 1: write the failing tests:
  - `TestNestedMemberEntries` (deployfile: parse, `All()` paths, nested members rejected, port conflict across a member and a top-level entry, debug in a member of the team file rejected);
  - `TestEntryBareMeansDefault`;
  - `TestLoadRequiredByVersionNeedsOwnEntry` (Review Focus 1);
  - `TestLoadShellHostsRequiredByVersion` (Review Focus 2);
  - `TestLoadConfigDefaultVersion` (Review Focus 4);
  - `TestComposeHostedMemberFieldsNotWarned`.
- [ ] Step 2: FAIL. Step 3: implement, and update the helpers, the translator and the fixtures to the nested form. Step 4: run `go test ./internal/... ./tests/i18nguard/... ./tests/archguard/...` and confirm PASS.
- [ ] Step 5: commit `feat(deploy): shell members are nested entries; unversioned means the default version`.

### Task 3: Spec rulings and end to end

**Files:**
- `new_plan/提案.md` Appendix A: add A20 (`requiredBy` / default version, and the maintenance rules for `add` and `upgrade`), A21 (nested members), A22 (the local repo version must equal the default version: an error in `up` for code run from a local repo, P4). A5 is revised.
- `internal/cli/testdata/three-layer-shell/deploy*.yaml` (nested form).
- `internal/cli/shell_e2e_test.go` (a member with its own `exposePort`; the shell disabled → the member runs standalone and publishes it).

- [ ] Step 1: write the failing e2e test `TestShellDisabledMembersRunStandalone`. Step 2: FAIL. Step 3: fixture changes. Step 4: `go build ./... && go vet ./... && go test ./internal/... ./cmd/... ./tests/i18nguard/... ./tests/archguard/...` PASS, golangci-lint reports 0 issues.
- [ ] Step 5: commit `docs(spec): A20–A22 default version, nested members, local version check`.
