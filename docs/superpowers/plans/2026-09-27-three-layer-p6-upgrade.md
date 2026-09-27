# P6: `brickkit upgrade` — config migration, conflicts, requiredBy, shell upgrade

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `upgrade` moves a component's default version, and each of the three files follows:
- **brickkit.yaml:** the new default version, and `requiredBy` kept right (A20).
- **Deploy files:** the entries follow.
- **config/:** the file is migrated to the new `configSchema` (§12.2). A conflict (§12.3, A4) is either chosen interactively or written as duplicate keys, which makes the project refuse to start until it is resolved.

Upgrading a shell switches its members to the versions the new shell compiles in (A24, overrides §8.6). A config found in `config/.archive/` is migrated back the same way when a component is re-added (§7.7).

**Architecture:**
- **`configdir.Migrate`** is a pure function: old config file plus old and new schema produce the new file text and a report.
- **`install.PlanUpgrade`** is a pure planner: project plus old graph plus new graph plus the set of version moves produce an `install.Plan` with new `MigrateConfigs` actions.
- **The CLI** resolves the targets, asks about conflicts, applies the plan through the P4 applier, and checks topology afterwards. Config conflicts are expected output here, not a failure.

**Tech Stack:** Go 1.22, testify, real git fixtures (`internal/source/gittest`).

**Spec:**
- `new_plan/提案.md` §12.1–12.4, §7.7, §8.6 (as overridden by A24), and Appendix A: A4, A5, A20, A24.
- `new_plan/命令表.md` command 9.

## Decisions stated to the user (not questioned)

- **Command surface:** `upgrade [<id>[@<version>]] [--dry-run] [--yes]`.
  - `--dry-run` is in 命令表 9.
  - `--yes` is A4's non-interactive switch: every conflict is written as a duplicate key. Without stdin the effect is the same (the existing `Options.Stdin` convention).
- **Which version upgrading goes to:**
  - `upgrade <id>` goes to the latest tag. For a component served by a local source, "latest" is the version in its directory (A8). This is the `brickkit upgrade <id>@<repo version>` that A22's local-repo check suggests.
  - `upgrade` with no argument upgrades every default version that has a newer one.
- **All or nothing:** the whole upgrade is planned first and applied in one go through the applier (rollback on any failure). §12.1's "stop, never leave a half-finished state" is met without half-upgraded projects.
- **What happens to the old version** when a default moves `v1 → v2`:
  - If anything still depends on v1 (in the new graph), or a shell in the project compiles v1 in, v1 stays with `requiredBy: [<those>]`, a `id@v1` entry and a versioned config file `<base>@v1.yaml` (the old unversioned file is renamed).
  - Otherwise v1 is removed, and its config is archived (§7.7).
  - If v2 was already a compatibility line, its `requiredBy` is dropped and it becomes the default. Its `id@v2` entry is removed, because the bare entry now means v2.
- **Which deploy entry follows the upgrade:**
  - The bare entry (with the user's fields) always follows the default, so it now means v2.
  - If that bare entry sits under a shell that compiles v1 in, and v1 is kept, it becomes `id@v1` in place. v2 gets a new top-level bare entry, so the P3f check stays true.
- **Dependencies:**
  - New dependencies of v2 are added exactly as `add` would add them (default, or `requiredBy`).
  - Versions only v1 needed are removed by the P4 cascade.
- **Shell upgrade (A24):** upgrading shell `S` to v2 applies S2's `shell.members`:
  - a member whose default is the old compiled-in version moves to the new one (with config migration);
  - a compatibility version pinned only for S is replaced;
  - a member no longer in S2 is moved out to the top level (§8.7);
  - new members are added and nested.
- **Migration** (§12.2, A4), key by key:

  | Old file | New schema | Result |
  | --- | --- | --- |
  | key written by the user | key still there | value copied verbatim (`$var:`, `${…}`, `file://` included) |
  | key written by the user | key gone | dropped from the new file, kept in the archive, listed in the report |
  | key not written (commented skeleton line) | key still there | skeleton line of the new version (follows the new default) |
  | — | new key | skeleton line (required without default: `KEY: ""`) |

  Conflict: the user wrote the key, the default changed, and the user's value differs from the old default. The conflict is chosen interactively in a TTY (keep mine / take the new default), or written as a duplicate-key block (`configdir.ConflictBlock`). When the user's value equals the old default, the user did not really change it, so it follows the new default (§12.3 row 1).
- **Archive restore on `add`:** when `config/.archive/` holds a file for that id, the newest archived version is migrated into the new skeleton with the same algorithm (old schema from the permanent manifest cache; if it is missing, every archived key is treated as user-written). This replaces P4's "an archived config exists" note.

## Global Constraints

- Messages go through `internal/msgid` and both catalogs. The i18n guard and the message guard test stay green, and there are no unused msgids.
- Chinese comments matching the surrounding style.
- Commit on main after each task. The message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- After each task, run all of the following:
  - `go build ./...`
  - `go vet ./...`
  - `go test -count=1 ./internal/... ./cmd/... ./tests/i18nguard/... ./tests/archguard/...`
  - `.tools/bin/golangci-lint run ./...`
  - `GOOS=windows` / `GOOS=darwin` builds
  - `make check-schemas check-i18n cover-check`

  The doc checks in `make lint` fail on the base too; they belong to P9.
- Out of scope:
  - Upgrading a compatibility (`requiredBy`) version on its own. Its dependents pin it, so it moves only when they do.
  - Docs (P9).

## Review Focus

1. **Round trip, the plan's "done when":**
   - The user changed `LOG_LEVEL`, and the new version changed its default.
   - `upgrade --yes` writes the duplicate-key block. `up` refuses with the friendly conflict error.
   - The user deletes one line and the comment, and `up --dry-run` succeeds.
2. **Upgrade of a default that a dependent still needs at the old version:**
   - v1 stays with `requiredBy`, an `id@v1` entry and `config/<base>@v1.yaml` (the user's old values).
   - `config/<base>.yaml` is migrated for v2.
   - `up --dry-run` succeeds.
3. **Upgrade of a shell whose new version compiles a member in at a newer version:** the member's default moves (config migrated), the P3f check passes, and a member dropped from S2 runs on its own.
4. **`upgrade` with no argument where the second upgrade cannot be resolved:** nothing at all is written. The first one is not applied either.
5. **`--dry-run`:** it prints every version move, dependency change, config key copied / added / dropped / conflicting, and version kept or removed. No file changes, which is checked by comparing bytes.

---

### Task 1: `configdir.Migrate`

**Files:**
- Create: `internal/configdir/migrate.go`.
- Test: `internal/configdir/migrate_test.go`.

**Interfaces (Produces):**
```go
// Conflict 是一处"使用者写过、默认值也变了"的冲突；Choose 决定怎么处理。
type Conflict struct{ Key, UserYAML, OldDefault, NewDefault string }
type Choice int
const (ChooseDuplicate Choice = iota; ChooseMine; ChooseNew)
type MigrateInput struct {
    ID, FromVersion, ToVersion string
    Old       []byte                 // 旧配置文件原文（可以为空：没有旧文件）
    OldSchema, NewSchema *manifest.ConfigSchema // OldSchema 为 nil 表示不知道（归档恢复时缓存里没有）
    VarRefs   map[string]string       // 新增键要写成 $var: 引用的（同 add 的提示）
    Choose    func(Conflict) Choice
}
type MigrateReport struct{ Copied, Added, Dropped, Followed []string; Conflicts []Conflict; Resolved map[string]Choice }
func Migrate(in MigrateInput) ([]byte, MigrateReport, error)
```

**Rules:** as in the Decisions table.
- **Output:** the P1 skeleton layout (header `# Component: id@to`, required section, optional section).
  - Copied keys are written uncommented, in their section.
  - Conflict blocks use `ConflictBlock`.
  - `ChooseMine` writes the user's line; `ChooseNew` writes the skeleton line.
- **Old values** are read with the YAML node API, so the value text is copied exactly (quoting, `$var:` and `${}` untouched).
- **Unparseable old file:** error. A file that still holds conflict markers is `configdir`'s existing `ConflictError`, meaning "resolve the previous conflict first".

- [ ] **Step 1: Failing tests** (one per rule plus the fixed cases):
  - `TestMigrateCopiesWrittenKeys`: `$var:` / `${}` / quoted values stay byte-identical.
  - `TestMigrateNewKeysFromSkeleton`
  - `TestMigrateDroppedKeysReported`
  - `TestMigrateUnwrittenKeyFollowsNewDefault`
  - `TestMigrateUserValueEqualsOldDefaultFollowsNew`
  - `TestMigrateConflictDuplicate`, `TestMigrateConflictChooseMineAndNew`
  - `TestMigrateUnknownOldSchemaTreatsKeysAsWritten`
  - `TestMigrateRefusesUnresolvedConflictMarkers`
  - `TestMigrateOutputReparsesWithConflictDetection`: the duplicate block is found by the existing loader as a `ConflictError`.
- [ ] **Steps 2–4.** Commit `feat(configdir): migrate a config file to a new configSchema with conflict blocks`.

### Task 2: `install.PlanUpgrade`

**Files:**
- Create: `internal/install/upgrade.go`.
- Modify: `internal/install/plan.go` (`MigrateConfigs []ConfigMigration`, with `ConfigMigration{ID, From, To string; FromFile, ToFile ConfigRef; OldSchema, NewSchema *manifest.ConfigSchema}`; `Moves []Move` for output).
- Test: `internal/install/upgrade_test.go`.

**Interfaces (Produces):**
```go
type Move struct{ ID, From, To string }
// PlanUpgrade：graph 以"升级后的项目"为根解析（移走的默认版本换成新版本，外壳换成新外壳编进的成员）。
func PlanUpgrade(p *project.Project, oldGraph, newGraph *resolver.Graph, moves []Move) (*Plan, error)
// ShellMoves 把外壳的升级展开成成员的版本移动（附录 A24）。
func ShellMoves(p *project.Project, oldShell, newShell *manifest.Manifest) []Move
```

**Rules:** as in the Decisions section.
- **Errors:**
  - `InstallUpgradeNotDefault`: the target is a compatibility version.
  - `InstallUpgradeSameVersion`: already at that version, which the CLI reports as "up to date", not an error.
  - A move whose new version is below the current one is allowed (downgrade), but noted.

- [ ] **Step 1: Failing tests:**
  - `TestPlanUpgradeMovesDefault`
  - `TestPlanUpgradeKeepsOldVersionForDependent`
  - `TestPlanUpgradeRemovesOldVersionNobodyNeeds`
  - `TestPlanUpgradePromotesExistingCompatibilityLine`
  - `TestPlanUpgradeAddsNewDependencies`
  - `TestPlanUpgradeCascadesDroppedDependencies`
  - `TestPlanUpgradeNestedEntryUnderShellCompilingOld`
  - `TestShellMovesFollowNewMembers` (member moved, member dropped → unnested, new member nested)
  - `TestPlanUpgradeRejectsCompatibilityTarget`
- [ ] **Steps 2–4.** Commit `feat(install): plan upgrades — default moves, requiredBy upkeep, shell member sets`.

### Task 3: `brickkit upgrade`

**Files:**
- Create: `internal/cli/upgrade.go`.
- Test: `internal/cli/upgrade_test.go`.
- Modify: `internal/cli/install_apply.go` (apply `MigrateConfigs` with `configdir.Migrate`; `checkInstalled` gets a topology-only mode for upgrades, because config conflicts are expected), `internal/cli/root.go`, msgids.

**Rules:**
- **Flow:**
  1. Load the project (`loadForInstall`).
  2. Resolve the targets:
     - `id@v`;
     - `id` → `LatestVersion`;
     - no argument → every default whose `LatestVersion` is higher;
     - a local-source default → its directory's version.
  3. Build the old graph (`ResolveProject`) and the new graph (plain `Resolve` over the upgraded roots, with shell members added as roots).
  4. Expand shell moves, then `PlanUpgrade`.
- **`--dry-run`:** render the plan (moves, versions kept or removed, dependencies added or removed, and per-config the copied / added / dropped / conflict keys, computed by running `Migrate` in memory with `ChooseDuplicate`) and exit 0 without writing.
- **Otherwise:**
  - Conflict choices: in a TTY, one prompt per conflict (`CliUpgradeConflictPrompt`, answer `m`/`n`/Enter = keep both as a duplicate). With `--yes`, or without stdin, a duplicate.
  - Apply, then the topology check.
  - Download the new versions' artifacts.
  - Print a summary. If there are conflict blocks, list the files and say `up` will refuse until they are resolved (exit 0: the upgrade itself succeeded).
- **Nothing to upgrade:** "everything is up to date".

- [ ] **Step 1: Failing tests** (git fixtures):
  - `TestUpgradeSingleToLatest`
  - `TestUpgradeToExplicitVersion`
  - `TestUpgradeRoundTripConflict` (Review Focus 1)
  - `TestUpgradeKeepsOldVersionForDependent` (Review Focus 2)
  - `TestUpgradeShellMovesMembers` (Review Focus 3)
  - `TestUpgradeAllIsAllOrNothing` (Review Focus 4)
  - `TestUpgradeDryRunWritesNothing` (Review Focus 5)
  - `TestUpgradeInteractiveChoice`
  - `TestUpgradeLocalSourceToRepoVersion`
  - `TestUpgradeNothingToDo`
- [ ] **Steps 2–4.** Commit `feat(cli): brickkit upgrade moves default versions and migrates their config`.

### Task 4: Archive restore on `add`

**Files:**
- Modify: `internal/cli/install_apply.go` (in `editConfigs`, `AddConfigs` with an archive → `Migrate` from it instead of the bare skeleton), `internal/cli/add.go` (report restored keys; remove the "archived config exists" note and its msgid).
- Test: `internal/cli/add_git_test.go`.

- [ ] **Step 1: Failing tests:**
  - `TestAddRestoresArchivedConfig`: remove, then add again: the user's values are back.
  - `TestAddRestoresFromOlderArchivedVersion`: the archive holds 1.0.0 and 2.0.0 is added, so values are migrated and dropped keys reported.
  - `TestAddArchiveRestoreConflictUsesYes`
- [ ] **Steps 2–4.** Commit `feat(cli): add restores an archived config through the migration algorithm`.

### Task 5: Final review

- [ ] Run `review-package` over the plan's range. Dispatch the final reviewer (opus) with the Review Focus above, the ledger's `Ruling:` lines and the Decisions section. Do one fix pass, verified by TDD. Report to the user in Chinese, then update the roadmap and project memory.
