# P7: the remaining commands — init, new --shell, local, deps, release, add --local --init, lint, skills

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** every command in `new_plan/命令表.md` exists with the documented flags and behaves as the spec says on
the three-layer model. New: `local`, `deps`, `release`. Reworked: `init`, `new`, `add --local`, `lint`, and the
content of the skill assets `init` installs.

**Architecture:**
- **`project` owns the file primitives.** Project init is one *complete* primitive (a plan of create / skip /
  warn, then apply) shared by creation mode, complement mode and `add --local --init`. The project doc
  (`BRICKKIT.md`) is a CLI-managed block, rendered from the loaded project and rewritten by every command that
  changes `brickkit.yaml`, so it never goes stale.
- **`deployfile` owns the local-file diff.** `local refresh` compares two deploy files as data, not as text.
- **`cli` stays thin:** it prints plans, asks for confirmation, runs git for `release`, and reuses
  `resolveTopology` for `deps` and the P4 applier for `add --local --init`.

**Tech Stack:** Go 1.22, cobra, testify, real git fixtures (`internal/source/gittest`, bare remotes for `release`).

**Spec:**
- `new_plan/命令表.md`: commands 1, 2, 4, 5, 6 (`--init`), 14, 16, 18.
- `new_plan/提案.md` §6.2–6.6, §9.6.1, §9.6.2, the shell directory convention, §10.2, §11.1, §11.3, §11.5,
  §16.1.1, §16.2, and Appendix A: A8, A9, A11, A14, A16, A24.

## Decisions (rulings on what the spec leaves open or contradicts)

- **`init <name>` creates `./<name>/`** (命令表 1 mode A). It refuses if `<name>` exists and is not empty.
  `init` with no argument is complement mode in the current directory. `--name` sets the project name in
  complement mode. `--hooks` (install only the pre-commit hook) stays.
- **The project name in complement mode** comes from `--name`, else the existing `brickkit.yaml`'s `project`,
  else the directory name. The directory name is normalised to a valid project name (lowercase, invalid runs
  become `-`); if nothing valid is left, the command fails and asks for `--name`.
- **Complement mode file rules** (§11.5):
  - `brickkit.yaml`, `deploy.yaml`, `config/vars.yaml`, `config/.gitkeep`: created if missing, otherwise
    skipped with "exists, skipped".
  - `.gitignore`: created in full if missing. If it exists, it is never edited: a missing required entry is a
    loud warning that lists each one.
  - A directory is non-empty when it holds anything except `.git/`. Then the plan is printed and
    confirmed (`[y/N]`). `--yes` skips the prompt; without stdin the prompt counts as "no" (the existing
    `confirm` convention).
  - Finally a closing check runs: the same cross-file load `lint` uses. On failure it prints the problems and
    the command fails (exit 1): the project was completed but is not yet runnable.
- **Shell directory convention:** a new project's `brickkit.yaml` declares two local sources,
  `local-dev: ./components` and `local-shells: ./shell`. `init` creates both directories; `shell/` gets a
  `.gitkeep` and is *not* gitignored, because shells are project code. `new --shell` defaults to
  `shell/<scope>/<name>/`. Local sources keep their `<root>/<scope>/<name>/` layout, so the spec's example
  path `shell/erp-shell/` is written as `shell/erp/shell/`.
- **Project `BRICKKIT.md`** (§16.2.1):
  - `init` writes a static head plus a CLI-managed block between
    `<!-- brickkit:managed:begin -->` / `<!-- brickkit:managed:end -->` markers.
  - The block holds two tables. **Components:** id, version, doc path, contract paths; doc and contract paths
    are shown only when those files exist. **Local-source components:** id, source dir, doc path.
  - The block is regenerated after every successful write by `add` / `remove` / `upgrade` (in the shared
    applier), and by `init` complement mode.
  - A `BRICKKIT.md` without the markers is left alone; complement mode reports this once.
  - When the directory holds a `component.yaml` (a component repo used as a workbench, §16.1.1),
    `BRICKKIT.md` is the component's own doc: `init` does not create a project doc there, and the applier
    never touches a file without markers.
- **`new --shell`:**
  - `component.yaml` has no `shell.kind`: per A11, `kind: shell` lives in `brickkit.yaml`, and the presence of
    `shell.members` makes a component a shell.
  - The validator rejects an empty members list, and the skeleton must pass validation (命令表 5). So
    `new --shell` writes one placeholder member, `example/member@0.1.0`, with a TODO comment. `add` of the
    unedited skeleton then fails loudly, naming that member.
  - Every `new` (not only `--shell`) also writes the component `BRICKKIT.md` skeleton (§16.2 structure). Its
    "shell declaration" section is filled only for `--shell`.
- **`local on`:**
  - When `deploy.local.yaml` is missing, it is copied byte for byte from `deploy.yaml` (comments kept), then
    switched on.
  - When the file exists (A16: `off` keeps it), it is switched on and reused, never overwritten. The command
    says so, and points to `local refresh` for a fresh copy. The 命令表 "exists → error" wording is overridden
    by A16, because an error would make `off` → `on` impossible.
  - `local on` / `off` on a state that is already set just reports the state.
- **`local off`** only removes the `.brickkit/local-mode` switch (A16).
- **`local status`** prints the switch, whether the file exists, and, when the switch is on, the
  consistency result (the same check `up` runs). It also notes when `deploy.local.yaml` exists while the
  switch is off.
- **`local refresh`:**
  - Requires `deploy.local.yaml` to exist; otherwise it fails with a hint to run `local on`.
  - It keeps the switch as it is.
  - It overwrites `deploy.local.yaml.bak` (the one backup the spec names). The previous `.bak` is lost, and
    the output says it was replaced.
  - **The diff summary compares data, not text.** Top-level fields are shown as `[target]`, `[k8s.namespace]`;
    vars as `[vars] KEY`; component-entry fields as `[<entry id>] field`, with members keyed by their own entry
    id.
  - One line per value that the old file has and the new one differs from or lacks:
    `- [erp/backend] mode: debug (now `enabled`)` / `(now unset)`. A whole entry the team file no longer has
    is reported as one line.
  - Values are rendered one-line (the `configdir.ScalarYAML` / flow form used for conflicts).
- **`deps`:**
  - `deps` prints one tree per top-level default version (nothing in the project depends on it), in
    `brickkit.yaml` order, using `├──` / `└──` / `│`.
  - Each node shows `id@version`, marked `(optional)` for a weak edge, `(optional, not installed)` when it is
    missing, and `(shown above)` when a node repeats. A repeated node's subtree is printed once per command,
    so diamonds stay small.
  - `deps <id>[@version]` prints that component's tree (every version of the id when no version is given),
    then a `Required by:` line listing its direct dependents, or `(top-level)`.
  - It resolves exactly like `graph`: same source client, network for uncached manifests. Warnings go to
    stderr.
- **`release`** (§10.2; A14 keeps `publish` for the market):
  - `release [--path <dir>]`; `--path` defaults to `.`. It reads only `component.yaml` (§16.1.1 ignores
    `brickkit.yaml`).
  - The tag is `<version>` when the component directory is the repo root. Otherwise it is
    `<scope>-<name>/<version>` (A9), the same names the git source reads.
  - **Checks, all before tagging:**
    - the manifest parses and validates;
    - `git status --porcelain -- <dir>` is empty (scoped to the component directory, so a monorepo sibling's
      edits do not block it);
    - the branch has an upstream, and `git rev-list @{u}..HEAD` is empty;
    - the tag does not exist locally or on `origin` (`git ls-remote --tags`).
  - Then `git tag <tag>` and `git push origin <tag>`. A push failure deletes the local tag (`git tag -d`) and
    reports git's output.
- **`release --local`** covers every local-source component of the project. A component whose tag already
  exists and points at the current `HEAD` is skipped as "already released"; a tag that exists at a different
  commit is an error ("bump `metadata.version`"). All checks for all components run before the first tag is
  written. Tag-and-push then runs in order and stops at the first push failure: earlier tags stay, that one is
  rolled back, later ones are untouched (§9.6.2).
- **`add --local --init`** (§9.6.1):
  - It patrols every enabled local source. For each component directory without a `brickkit.yaml`, it runs the
    complement primitive in that directory, without skills and without the confirmation prompt (`--init` is the
    confirmation).
  - The child's project name is the component id with `/` → `-`. Its `brickkit.yaml` inherits the parent's
    sources. A local source's path is rewritten relative to the child, so the child sees its siblings.
  - The child's own dependencies are then added in the child through the normal install path
    (`installAdd` on the child project).
  - Finally the parent's `add --local` runs as before. It stops at the first failure; a child workbench already
    written stays, and the error names it.
  - Children that already have a `brickkit.yaml` are left alone.
- **`lint` new checks** (§11.3). Everything stays offline; manifests are read only from disk (the permanent
  cache `.brickkit/manifests/`, or a local source whose directory holds that exact version).
  - **Required config values:** a required key without a default and without a value is an error. Components
    whose manifest is not on disk are listed in one note as "not checked".
  - **Duplicate keys, orphan configs, deploy consistency, members, dangling `$var:`:** already enforced by the
    shared load, unchanged.
  - **The local deploy file is checked for consistency even when the switch is off.** It is loaded a second
    time with that file when it exists and is not the one in use.
  - **`--strict` reference checks:** a `${VAR}` not found in the process environment or `.env`, and a
    `file://` whose file is missing, are warnings. They are reported only under `--strict`, where warnings
    fail.
  - **Special characters:** a member's value, as it will be JSON-injected into its shell, must be valid UTF-8.
    Literal and `file://` values are checked always, `${VAR}` values under `--strict`. This reuses the check
    `up` already does in `shell.memberConfig`, exported as `shell.CheckMemberValue`.
- **Skills assets:** the four skills and `AGENTS.md`, in both languages, are rewritten for the three-layer
  model: the file roles, `local on/off/refresh`, `upgrade`, `build`, `release`, `deps`, `requiredBy`, shells
  and `members`. `servedBy`, `override.yaml`, `resources:` and `mode: debug` in `brickkit.yaml` are gone.
  `brickkit skills update` delivers the new content through the existing five-state check.

## Global Constraints

- Messages go through `internal/msgid` and both catalogs. The i18n guard and the message guard test stay
  green, and there are no unused msgids.
- Chinese comments matching the surrounding style.
- Commit on main after each task. The message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- After each task, run the P6 verify set (the script exits non-zero on the first failure, and its exit status
  is checked, never piped away):
  - `go build ./...`
  - `go vet ./...`
  - `go test -count=1 ./internal/... ./cmd/... ./tests/i18nguard/... ./tests/archguard/...`
  - `.tools/bin/golangci-lint run ./...`
  - `GOOS=windows` / `GOOS=darwin` builds
  - `make check-schemas check-i18n cover-check`

  The doc checks in `make lint` belong to P9.
- New CLI surface is exactly 命令表's. No extra flags beyond `--yes` on `init` and the kept `--hooks`.
- Out of scope: market alignment (P7b), `tests/` fixtures and green `make test-all` (P8), docs (P9).

## Review Focus

1. **Complement `init` on a real component repo** (a `component.yaml`, a `.git/`, a hand-written
   `.gitignore` missing `.brickkit/`):
   - it creates the three files;
   - it never edits `.gitignore` (the warning lists `.brickkit/`);
   - it does not create a project `BRICKKIT.md` over the component doc;
   - the closing check passes.
2. **`local refresh` with a member entry changed under a shell,** plus a local `vars` override: both appear in
   the summary with their entry ids, and nothing else does.
3. **`release` in a monorepo subdirectory whose sibling has uncommitted edits:** it tags
   `<scope>-<name>/<ver>`, and `add` from that repo reads the tag back.
4. **`release --local` where the second component's push fails:** the first tag stays on the remote, the
   second is rolled back locally, the third was never written.
5. **The project doc after `upgrade` and `remove`:** its table shows the new version and drops the removed one.
   A user-written `BRICKKIT.md` without markers is byte-identical after both.

---

### Task 1: `project.Complete` — the init primitive, creation and complement modes

**Files:** `internal/project/init.go` (rewrite to `Complete`), `internal/project/projectdoc.go` (new),
`internal/project/init_test.go`, `internal/cli/init.go`, `internal/cli/init_test.go`, CLI test helpers that
call `init <name>` (switch them to `init --name <name>` in their empty work directory),
`internal/msgid/project.go`, `internal/msgid/cli_init.go`, both catalogs.

**Interfaces:**
- Produces:
  - `project.CompletePlan{Name string; Create, Skip []string; GitignoreMissing []string; GitignoreCreate bool; ProjectDoc bool}`
  - `project.PlanComplete(l Layout, name string) (*CompletePlan, error)`
  - `(*CompletePlan).Apply(l Layout) error`
  - `project.ProjectNameFor(l Layout, flag string) (string, error)`
  - `project.DirIsEmpty(dir string) (bool, error)` (ignores `.git`)
  - `project.WriteProjectDoc(l Layout, p *Project) (written bool, err error)`: writes the managed block, creating
    the file when it is missing and the directory has no `component.yaml`
  - `project.RenderProjectDoc(p *Project) string`
- Local sources in the skeleton: `local-dev: ./components`, `local-shells: ./shell`.

- [ ] Tests first (`internal/project/init_test.go`):
  - `TestPlanCompleteEmptyDirCreatesEverything`
  - `TestPlanCompleteSkipsExistingFiles`
  - `TestPlanCompleteNeverEditsExistingGitignore` (the missing entries are listed, the file is byte-identical
    after Apply)
  - `TestPlanCompleteInComponentRepoLeavesBrickkitMd`
  - `TestProjectNameForFallbacks` (flag > brickkit.yaml > normalised dir name > error)
  - `TestRenderProjectDocTables` (cached doc path shown only when the file exists; a local-source row)
  - `TestWriteProjectDocLeavesFileWithoutMarkers`
- [ ] CLI tests (`internal/cli/init_test.go`):
  - `TestInitCreatesNamedDirectory`
  - `TestInitRefusesNonEmptyNamedDirectory`
  - `TestInitCompleteAsksInNonEmptyDir` (stdin `n` → nothing written; `y` → written)
  - `TestInitCompleteYesSkipsPrompt`
  - `TestInitCompleteWarnsMissingGitignoreEntries`
  - `TestInitCompleteClosingCheckFails` (an existing `brickkit.yaml` lists a component with no deploy entry →
    exit 1 with the inconsistency block)
- [ ] Watch them fail, implement, watch them pass, run verify, commit
  `feat(init): create and complement modes on one primitive, project BRICKKIT.md`.

### Task 2: the applier keeps the project doc current

**Files:** `internal/cli/install_apply.go`, `internal/cli/add_test.go` / `remove_test.go` / `upgrade_test.go`.

- [ ] Tests:
  - `TestAddUpdatesProjectDoc`
  - `TestRemoveUpdatesProjectDoc`
  - `TestUpgradeUpdatesProjectDoc`
  - `TestApplierLeavesUnmanagedBrickkitMd` (byte-identical)
- [ ] After the post-write load check succeeds, call `project.WriteProjectDoc(reloaded)`. A doc write error
  does not roll back the three files, since they are already correct. It is reported as a warning naming the
  file.
- [ ] Verify, commit `feat(cli): add, remove and upgrade keep the project BRICKKIT.md current`.

### Task 3: `new --shell` and the component `BRICKKIT.md` skeleton

**Files:** `internal/manifest/scaffold.go`, `internal/manifest/scaffold_test.go`, `internal/cli/new.go`,
`internal/cli/new_test.go`, msgid + catalogs.

- [ ] Tests:
  - `TestScaffoldShellPassesValidation` (Parse succeeds, `IsShell()`, the placeholder member)
  - `TestScaffoldWritesComponentDoc` (all five §16.2 headings; the shell section filled only for `--shell`)
  - `TestNewShellDefaultsToShellDir`
  - `TestNewWritesBrickkitMd`
- [ ] `ScaffoldOptions.Shell bool`; add the `--shell` flag.
- [ ] Verify, commit `feat(new): --shell skeleton and a BRICKKIT.md for every new component`.

### Task 4: `brickkit local on|off|status|refresh`

**Files:** `internal/deployfile/diff.go` (new), `internal/deployfile/diff_test.go`, `internal/cli/local.go`
(new), `internal/cli/local_test.go`, `internal/cli/root.go`, msgid (`internal/msgid/cli_local.go`) + catalogs.

**Interfaces:**
- Produces:
  - `deployfile.LocalChange{Scope, Field, Old string; New *string}` (`New == nil` means unset in the new file;
    `Field == ""` means a whole entry)
  - `deployfile.DiffLocal(old, fresh []byte) ([]LocalChange, error)`

- [ ] Tests:
  - `TestDiffLocalReportsEntryFieldTopLevelAndVars`
  - `TestDiffLocalKeysMembersByEntryID`
  - `TestDiffLocalWholeEntryGone`
  - `TestDiffLocalIgnoresCommentsAndOrder`
- [ ] CLI tests:
  - `TestLocalOnCopiesDeployBytes`
  - `TestLocalOnReusesExistingFile`
  - `TestLocalOffKeepsFile`
  - `TestLocalStatusReportsStale`
  - `TestLocalRefreshBacksUpAndSummarises`
  - `TestLocalRefreshWithoutFileFails`
  - `TestLocalRefreshKeepsSwitch`
- [ ] Verify, commit `feat(cli): brickkit local on/off/status/refresh with a data diff summary`.

### Task 5: `brickkit deps`

**Files:** `internal/cli/deps.go` (new), `internal/cli/deps_test.go`, `root.go`, msgid (`cli_deps.go`) + catalogs.

- [ ] Tests:
  - `TestDepsProjectTrees` (two top-level roots, a shared node shown once with `(shown above)`)
  - `TestDepsMarksOptionalAndMissing`
  - `TestDepsSingleComponentWithRequiredBy`
  - `TestDepsAllVersionsOfID`
  - `TestDepsUnknownComponent` (error naming the id with the `add` hint)
- [ ] Verify, commit `feat(cli): brickkit deps prints the dependency tree`.

### Task 6: `brickkit release` and `release --local`

**Files:** `internal/gitrepo/release.go` (new: status scoped to a directory, upstream ahead count, tag
exists locally / remotely / points at HEAD, tag, push, delete tag), `internal/gitrepo/release_test.go`,
`internal/cli/release.go` (new), `internal/cli/release_test.go`, `root.go`, msgid (`cli_release.go`) + catalogs.

- [ ] Tests (real repos with a bare `origin`):
  - `TestReleaseTagsAndPushes`
  - `TestReleaseSubdirUsesNamespacedTag` (and `source` git reads it back via `add`)
  - `TestReleaseRefusesDirtyComponentDir`
  - `TestReleaseIgnoresSiblingEdits`
  - `TestReleaseRefusesUnpushedCommits`
  - `TestReleaseRefusesNoUpstream`
  - `TestReleaseRefusesExistingTag`
  - `TestReleaseRollsBackOnPushFailure` (a pre-receive hook on the bare remote rejects the push)
  - `TestReleaseInvalidManifestStopsBeforeTag`
  - `TestReleaseLocalChecksAllBeforeTagging`
  - `TestReleaseLocalSkipsAlreadyReleased`
  - `TestReleaseLocalStopsAtFirstPushFailure`
- [ ] Verify, commit `feat(cli): brickkit release tags and pushes a component version atomically`.

### Task 7: `add --local --init`

**Files:** `internal/cli/add_local.go`, `internal/cli/add.go` (flag), `internal/cli/add_local_test.go`, msgid +
catalogs.

- [ ] Tests:
  - `TestAddLocalInitCreatesChildWorkbench` (child `brickkit.yaml` with the rewritten local paths, deploy file,
    `config/`, the child's dependency added in the child)
  - `TestAddLocalInitLeavesInitializedChild`
  - `TestAddLocalInitCoversShellDir`
  - `TestAddLocalInitStopsAtFirstFailure`
  - `TestAddInitWithoutLocalIsUsageError`
- [ ] Verify, commit `feat(add): --local --init gives each local component its own workbench`.

### Task 8: `lint` — the §11.3 checks

**Files:** `internal/cli/lint.go`, `internal/cli/lint_config.go` (new: on-disk manifests, required values,
reference checks, member values), `internal/cli/lint_test.go`, `internal/shell/shell.go` (export
`CheckMemberValue`), msgid + catalogs.

- [ ] Tests:
  - `TestLintRequiredValueMissing`
  - `TestLintNotesUncachedManifests`
  - `TestLintChecksLocalFileWhileSwitchOff`
  - `TestLintStrictEnvAndFileReferences` (no warning without `--strict`; failure with it)
  - `TestLintMemberInvalidUTF8`
  - `TestLintDuplicateKeysStillReported`
- [ ] Verify, commit `feat(lint): required values, local file consistency, references, member values`.

### Task 9: skill assets for the three-layer model

**Files:** `internal/skills/assets/{en,zh}/AGENTS.md`, `internal/skills/assets/{en,zh}/claude/skills/*/SKILL.md`,
`internal/skills/*_test.go` (only if they pin content).

- [ ] Test first: `TestSkillAssetsHaveNoRemovedConcepts` fails on any asset containing `servedBy`,
  `override.yaml`, `brickkit override`, `publish --path`, `resources:` or a `mode: debug` example under
  `brickkit.yaml`, and requires each project-scope asset to name `deploy.yaml`, `config/` and `local on`.
- [ ] Rewrite both languages independently (align up, not down).
- [ ] Verify, commit `docs(skills): skill assets describe the three-layer model`.
