# P4: Git Distribution, `add` / `remove` / `fetch`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Components are distributed through git repositories and tags, with no market needed. `add`, `remove` and `fetch` work again on the three-layer model, and they are complete:
- `add` writes brickkit.yaml, the deploy files and `config/` in one go. This covers dependencies, multiple versions (`requiredBy`) and shells (A24).
- `remove` cleans up after itself: configs are archived, deploy entries are removed, versions nobody needs any more are removed, and shell members are released.
- `up` refuses to run code from a local repo whose version is not the one this run expects (A22/A24).

**Architecture:**
- **`internal/source` gains a git fetcher.**
  - The repo address comes from `baseUrl + <scope>-<name>` or from a component's own `source.repo`.
  - Bare repos live in a user-level cache shared by all projects (A12). The cache directory is named after the full repo address.
  - Manifests are read in four steps: the project's permanent manifest cache (`.brickkit/manifests/<scope>/<name>/<version>/`), then `git show` from the bare repo, then `git fetch --tags`, then `git clone --bare`.
  - Tags are `<version>`, or `<scope>-<name>/<version>` when `source.path` puts the component in a subdirectory (A9).
  - Git is the system `git`, and its errors are passed through (§9.9).
- **A new package `internal/install` holds the planning logic.**
  - It turns "this project + this resolved graph + this request" into a list of edits for the three files. It is pure logic with no I/O, so every multi-version and shell rule can be tested on its own.
  - The commands in `internal/cli` only resolve, prompt, apply the plan, and report.
- **Writes go through `yamlfile.Edit`, which gains operations for multiple versions and nested members.**
  - After writing, the command loads the project once (`project.Load`). If the result does not load, every file is restored to its original bytes and the command fails loudly. `add`/`remove` never leave a project `up` cannot read.

**Tech Stack:** Go 1.22, system `git` (tests use real `git` against local bare repos over `file://`; no network), testify.

**Spec:**
- `new_plan/提案.md`, Appendix A: A4, A5, A8, A9, A12, A14, A20–A24.
- `new_plan/提案.md` body: §5.4–5.5, §6.3, §6.7, §7.2.5, §7.6, §7.7, §8.5, §8.7, §9.1–9.9, §10.1.
- `new_plan/命令表.md`: commands 6, 7 and 8 (the flag surface is fixed there; P4 adds no command or flag).

## User rulings for this phase (2026-09-27)

- **`remove` of a default version while other versions remain:** if exactly one other version remains, it becomes the default.
  - Its `requiredBy` is dropped.
  - `config/<base>@<v>.yaml` is renamed to `config/<base>.yaml`.
  - Its deploy entry `id@v` becomes the bare `id`, in every deploy file.

  If several remain, this is an error listing them and their `requiredBy`.
- **`add id@v` when `id` is already in the project at another version and no dependent asked for `v`:** this is an error.
  - The hint points to `brickkit upgrade id@v` (P6), which changes the default version.
  - `add` only adds new components, plus the versions that dependencies or shells bring in.

## Decisions stated to the user (not questioned)

- **Where things are stored:**
  - Bare repos: `<UserCacheDir>/brickkit/repos/<host>/<path>.git`. A `file://` repo maps to `local/<path>`; an scp-style `git@host:org/repo` maps to `host/org/repo`.
  - Manifest cache: `.brickkit/manifests/<scope>/<name>/<version>/component.yaml`, plus `BRICKKIT.md` when the tag has one, plus the signature envelope `signature.json`. Market and local sources use the same layout.
- **How versions are found:**
  - "Latest" is the highest tag in exact semver form (major.minor.patch, compared numerically). Tags in any other form are ignored.
  - A namespaced tag is used if and only if `source.path` is set.
- **What `add` writes:**
  - `deploy.yaml`, plus `deploy.local.yaml` when that file exists. Files given with `-f` are not touched; a note names them.
  - The `$var:` prompt asks for each key in a TTY. With `--yes` it references the variable. Without stdin it declines, matching the existing `Options.Stdin` convention (no input = Enter = no).
- **`add` and shells:**
  - `add` of a shell brings in the members at the versions the shell declares, nested under it.
  - `add` of a member whose shell is already in the project, at exactly the version that shell declares, nests it under the shell. Any other version stays at the top level, and a note says why.
- **Other `add` rules:**
  - A dependency that needs another version is added with `requiredBy: [<dependent>]` and gets a versioned config skeleton. If a non-default line for that version already exists, the dependent is appended to its `requiredBy`.
  - `--repo` / `--repo-all` clone the source and check out the version's tag, so the local repo equals the default version (A22).
  - A per-component `source` in brickkit.yaml is honoured for components already declared there. `add` of a new component searches `sources`.
- **`remove`:**
  - Required dependents block the removal.
  - Config goes to `config/.archive/`. The unversioned file is archived as `<base>@<version>.yaml`, so the version survives.
  - Entries are removed from both deploy files.
  - A removed shell's members are promoted to the top level.
  - Every `requiredBy` that named the removed component drops it. A version whose list becomes empty is removed too, and the rule repeats until nothing changes.

## Global Constraints

- Messages go through `internal/msgid` and both catalogs. The i18n guard and the message guard test stay green, and unused msgids are removed.
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
- Git runs with `GIT_TERMINAL_PROMPT=0`, so CI never hangs on a password prompt, and it gets the context's cancellation.
- No new CLI commands, flags or environment variables. Tests redirect the repo cache through `cli.Options.RepoCacheDir` and `source.Options.RepoCacheDir`, which are not user-facing.
- Out of scope, per the roadmap:
  - `upgrade` and its `requiredBy` maintenance (P6).
  - `add --local --init`, `release`, `new --shell`, and the project `BRICKKIT.md` (P7).
  - The image label (P5).
  - Docs (P9).
  - `market-server/` (A2).

## Review Focus

1. **Offline after first fetch:** with the remote bare repo deleted, a second project on the same machine can still `add` a version the cache has seen, and `up --dry-run` works. The user-level cache is shared; the manifest cache is per project.
2. **Git failures:**
   - An auth failure shows git's own stderr and the three §9.9 hints, and never hangs.
   - A missing tag lists the versions that exist.
   - A missing repo names the URL that was tried.
3. **Interrupted clone:** a clone that stops halfway leaves no half-populated bare repo, because it clones into a temp directory and renames. The next run clones cleanly.
4. **`add` of a shell whose member's default is a different version:**
   - brickkit.yaml gets the declared version with `requiredBy: [<shell>]`.
   - The nested entry is written as `id@version`.
   - The default stays at the top level.
   - The P3f version check passes immediately, with no hint needed.
5. **`remove` in a multi-version project:**
   - Removing the dependent drops the version kept only for it, including its config (archived) and its deploy entries.
   - Removing the default with one version left promotes that version.
   - After every scenario, `project.Load` and `up --dry-run` succeed.

---

### Task 1: The editor handles versions, lists and nested members

**Files:**
- Modify: `internal/yamlfile/edit.go`.
- Test: `internal/yamlfile/edit_test.go`.

**Interfaces (Produces):**
- `type Selector struct{ ID, Version string }`. When `Version` is set, the entry must also have `version: <Version>`; this is how brickkit.yaml tells versions apart. Deploy files keep `ID` only (`id@v` is the whole id there).
- `func (e *Edit) AppendMapping(seqKey string, fields []Field) bool`, with `type Field struct{ Key string; Value any }`. `Value` is a string, or `[]string`, which is written as a flow list (`requiredBy: [erp/shell]`).
- `func (e *Edit) SetList(seqKey string, sel Selector, field string, values []string) bool`: an empty `values` deletes the field.
- `func (e *Edit) RemoveWhere(seqKey string, sel Selector) bool`.
- `func (e *Edit) RenameID(seqKey, oldID, newID string) bool`: renames `erp/api@1.0.0` ↔ `erp/api`, wherever the entry is nested.
- `func (e *Edit) Nest(seqKey, shellID, entryID string) bool`: moves an existing top-level entry, with its comments and fields, under the shell's `members`, creating `members`. A missing entry is appended bare.
- `func (e *Edit) Unnest(seqKey, shellID string) []string`: moves every member of the shell to the top level, right after the shell, keeping their fields, and returns their ids.
- The existing `SetField` / `DeleteField` / `AppendEntry` / `RemoveEntry` keep their signatures and build on `Selector{ID: id}`.

- [ ] **Step 1: Failing tests.**
  - `TestSelectorTellsVersionsApart`: two `erp/api` lines in brickkit.yaml. `SetList` and `RemoveWhere` touch only the selected version.
  - `TestAppendMappingWritesFlowList`: the output contains `requiredBy: [erp/shell]` and keeps comments and blank lines elsewhere.
  - `TestNestMovesEntryWithFields`: the entry's `expose`/`exposePort` and head comment move with it.
  - `TestUnnestPromotesMembersAfterShell`: members keep their fields and land right after the shell. `members` is removed.
  - `TestRenameIDNestedAndTopLevel`.
- [ ] **Step 2:** Run the tests: FAIL. **Step 3:** Implement. **Step 4:** The suite passes.
- [ ] **Step 5:** Commit `feat(yamlfile): edit multi-version lines, lists and nested members`.

### Task 2: Permanent per-version manifest cache

**Files:**
- Modify: `internal/source/source.go` (`ManifestCachePath`, `SignatureCachePath`, the new `DocCachePath`, and `readCachedManifest`/`writeFileAll` callers), `internal/source/fetcher.go` (optional `docBytes`).
- Update: tests that write `.brickkit/manifests/<id>-<v>.yaml` (`internal/cli/up_upgrade_test.go`, `internal/cli/shell_e2e_test.go`, the source tests) to use a new test helper `source.CacheManifestForTest(layout, id, version, raw)`. It is exported from a `_test`-free file `internal/source/testing.go` so the cli tests can use it.

**Rules:**
- The layout is `.brickkit/manifests/<scope>/<name>/<version>/component.yaml`, plus `signature.json` (same content as today's envelope) and `BRICKKIT.md` (only when the source provides it).
- It is permanent: nothing deletes it. `Options.Refresh` still bypasses the read.
- A `docFetcher` interface (`docBytes(ctx, id, version) ([]byte, error)`) is optional. Local reads `<dir>/BRICKKIT.md`; git reads `git show <tag>:<path>/BRICKKIT.md`; market does not implement it. Its absence is not an error.
- `(*Client).Doc(id, version) (path string, ok bool)` returns the cached `BRICKKIT.md` path for later phases (P7).

- [ ] **Step 1: Failing tests.**
  - `TestManifestCacheLayoutPerVersion`: after `Manifest()` from a local source, the file is at `manifests/erp/api/1.0.0/component.yaml`, and `BRICKKIT.md` is next to it when the source has one.
  - `TestCachedVersionSurvivesSourceChange`: the local dir moves to 1.1.0, and 1.0.0 is still served from the cache (today's behaviour, new path).
- [ ] **Steps 2–4.** Commit `feat(source): permanent per-version manifest cache with BRICKKIT.md`.

### Task 3: The git source

**Files:**
- Create: `internal/source/git.go` (fetcher), `internal/source/gitcache.go` (repo address → cache dir, clone/fetch/show, tag listing), `internal/source/gittest/gittest.go` (a non-test helper package that builds real bare-repo fixtures: `gittest.NewRemote(t, dir) *Remote`, `(*Remote).Tag(version string, files map[string]string)`, `(*Remote).TagAt(subpath, version, files)` for monorepo namespaces, `(*Remote).URL() string`; it skips the test when `git` is missing).
- Modify: `internal/source/source.go` (`newFetcher` for `SourceTypeGit`; per-component `source` honoured in `Manifest`/`LatestVersion`/`DownloadArtifacts`/`Origin`), `internal/projfile/validate.go` (a git source needs `baseUrl`; a component `source.type: git` needs `repo`; `path` is relative with no `..`), msgids.
- Test: `internal/source/git_test.go`.

**Interfaces (Produces):**
- `source.Options.RepoCacheDir string`: empty means `os.UserCacheDir()/brickkit/repos`.
- `func RepoURL(src projfile.Source, override *projfile.ComponentSource, id string) (url, subpath string)`.
- `func repoCacheDir(root, url string) string`: this is where the address normalisation lives.

**Rules:**
- **Read order** for `manifestBytes(id, v)` (the permanent cache has already been checked in `Client.Manifest`):
  1. With a cached bare repo, `git show <tag>:<subpath>/component.yaml`.
  2. If the tag is missing, `git fetch origin --tags --prune-tags --force`, then retry.
  3. Without a cached repo, `git clone --bare <url> <tmp>` in the cache root's `.tmp/`, then `os.Rename`. If the rename finds that another process got there first, drop the temp clone and use theirs.
- **Missing tag after fetching:** return `errNotFound` recorded as a version mismatch. The message lists the exact-version tags that exist (so the user sees "1.0.0, 1.1.0" instead of a generic "not found").
- **Latest version:** fetch the tags and pick the highest exact-version tag under the prefix.
- **Artifacts:** `git show <tag>:<subpath>/<file>`.
- **Origin:** `OriginGit` with the repo URL. For a subpath, `Origin` carries `Subpath` and `Tag` too, so the `--repo` clone (Task 5) checks out the tag and uses the directory.
- **Git failures** (clone, fetch, ls): `clierr` `CodeNetworkUnreachable` "拉取组件失败：<id>".
  - Details: the repo URL, and git's stderr trimmed to its last 10 lines.
  - Hints: SSH / HTTPS credential helper / CI token.
  - These are real failures, not `errNotFound`, so `firstRealError` reports them.
- **Git env:** `GIT_TERMINAL_PROMPT=0` and `GIT_ASKPASS=` unset only when it is not set by the user. Use `exec.CommandContext`.

- [ ] **Step 1: Failing tests,** all against `gittest` remotes:
  - `TestGitManifestAtTag`
  - `TestGitReadsCachedRepoWithoutNetwork`: delete the remote after the first read; a new project reading the same version works, and so does another version fetched earlier.
  - `TestGitFetchesNewTag`
  - `TestGitLatestIsHighestExactTag`: tags `1.2.0`, `1.10.0`, `v2.0.0` and `latest` give `1.10.0`.
  - `TestGitMonorepoNamespacedTag`: the component under `packages/erp-api` is read from tag `erp-api/1.0.0`, and a plain `1.0.0` tag is ignored.
  - `TestGitMissingTagListsExisting`
  - `TestGitAuthFailurePassesStderr`: the URL points at a nonexistent path. Assert the git stderr and the hints are in the error, and that the call returns (no hang).
  - `TestGitInterruptedCloneLeavesNoRepo`: a context cancelled mid-clone leaves no `<name>.git` in the cache.
  - `TestRepoCacheDirNormalisesAddresses`: a table of https, `.git` suffix, scp-style and `file://` addresses.
  - `TestComponentSourceOverride`: `source.repo` beats `baseUrl`.
  - `TestProjfileGitSourceValidation`.
- [ ] **Steps 2–4.** Commit `feat(source): git source with a shared bare-repo cache and tag namespaces`.

### Task 4: `internal/install` plans the edits

**Files:**
- Create: `internal/install/plan.go`, `internal/install/add.go`, `internal/install/remove.go`, `internal/install/install_test.go`.
- Messages: `internal/msgid/install.go` and the catalogs.

**Interfaces (Produces):**
```go
// Line 是 brickkit.yaml 的一行组件声明。
type Line struct{ ID, Version string; Shell bool; RequiredBy []string }
// Entry 是部署文件的一条（ID 可能带 @version）；Under 非空时嵌在那个外壳条目下面。
type Entry struct{ ID, Under string }
// ConfigFile 是要生成的配置骨架。
type ConfigFile struct{ ID, Version string; Versioned bool; Schema *manifest.ConfigSchema }
// Plan 是一次 add / remove 要对三份文件做的全部改动，顺序即应用顺序。
type Plan struct {
    AddLines      []Line
    SetRequiredBy []Line          // 改已有行的 requiredBy（空表示去掉字段）
    RemoveLines   []resolver.Ref
    AddEntries    []Entry
    NestEntries   []Entry         // 已有的顶层条目挪到外壳下面
    RenameEntries [][2]string     // old → new（默认版本转正时 id@v → id）
    RemoveEntries []string
    UnnestShells  []string
    AddConfigs    []ConfigFile
    ArchiveConfigs []ConfigArchive // {From, To}
    RenameConfigs  [][2]string
    Notes         []string        // 给使用者的说明（成员没进外壳的原因等）
    Removed       []resolver.Ref  // remove 连带移除的版本（给输出用）
}
func PlanAdd(p *project.Project, graph *resolver.Graph, target resolver.Ref) (*Plan, error)
func PlanRemove(p *project.Project, graph *resolver.Graph, target resolver.Ref) (*Plan, error)
```
`graph` for `PlanAdd` is resolved from the target plus every ref already in brickkit.yaml. For a shell target it also includes the declared members (Task 5 adds them as roots).

**Rules, `PlanAdd`:**
- **Refs added:** the target and every graph ref not yet in brickkit.yaml, in topological order (dependencies first).
- **For each new ref `id@v`:**
  - If `id` is new, `v` becomes the default: a line without `requiredBy`, a bare deploy entry, and an unversioned config file.
  - If `id` already has another version, `v` is non-default: `requiredBy` lists the components in the graph that require `id@v` (direct dependents that are in the project after the add, plus the shell when the shell declares `id@v`), sorted. The deploy entry is `id@v`, and the config file is `<base>@v.yaml`.
  - If `id@v` is the target itself and `id` already has another version, the ruling applies: `InstallAddOtherVersion`, with a hint `brickkit upgrade id@v`.
  - If the ref exists as a non-default line and a new dependent needs it, the dependent is appended through `SetRequiredBy`. A default line is left alone.
- **Shells (A24):**
  - For a shell `S`, `Line.Shell=true`.
  - For each `m@v` in `S`'s `shell.members`, the ref is in the graph (Task 5 guarantees this). Its deploy entry goes under `S`: bare when `v` is the default, `m@v` otherwise.
  - An existing top-level entry for `m@v` is nested (`NestEntries`), not duplicated.
  - A member already nested under another shell stays there, with a note.
- **Adding a member:** when a project shell `S` declares exactly `id@v`, the entry is nested under `S`. When `S` declares another version, a note names both versions.
- **Components without `configSchema` get no config file** (§7.6).
- **Adding a ref already present** with every deploy entry and config file in place is a no-op, and the plan is empty. A missing deploy entry or config file is filled in.

**Rules, `PlanRemove`:**
- **Target resolution happens in the CLI** (a bare id with several versions is an error there).
- **Required dependents:** components remaining after the removal whose Manifest requires `target` (ID and version). If there are any, `InstallRemoveHasDependents` names them.
  - A `requiredBy` that lists only components being removed in the same plan does not block.
  - Optional dependents only produce a note.
- **What is removed:** the line, the deploy entry (nested or top-level), and the config (archived).
  - If `target` is a shell, `UnnestShells` promotes the members, and any `requiredBy` that names the shell is cleaned by the next rule.
- **Cascade:** every line whose `requiredBy` names a removed component drops it. A non-default line whose list becomes empty is removed too, with its entry and config, and the cascade repeats.
- **Default removed while other versions remain (ruling):**
  - One remains: it is promoted. Its `requiredBy` is dropped, the config is renamed `<base>@v.yaml` → `<base>.yaml`, and the entry `id@v` → `id`. The promoted version's entry keeps its position, including nesting.
  - Several remain: `InstallRemoveDefaultAmbiguous` lists them with their `requiredBy`.
- **Archive name:** `config/.archive/<base>@<version>.yaml`. An existing archive file is replaced; the archive keeps the latest copy per version.

- [ ] **Step 1: Failing tests (table-driven, on `projecttest` projects and stub graphs):**
  - `TestPlanAddNewComponentIsDefault`
  - `TestPlanAddDependencyAtOtherVersionGetsRequiredBy`
  - `TestPlanAddExistingNonDefaultAppendsDependent`
  - `TestPlanAddOtherVersionDirectlyIsAnError`
  - `TestPlanAddShellNestsDeclaredMembers`: one member new, one member whose default differs (gets `requiredBy: [erp/shell]` and `@v`), and one member already at the top level at the declared version (nested).
  - `TestPlanAddMemberJoinsProjectShell`, and a variant where the version differs and only a note is added.
  - `TestPlanAddIdempotent`
  - `TestPlanAddNoSchemaNoConfig`
  - `TestPlanRemoveBlockedByDependents`
  - `TestPlanRemoveCascadesRequiredBy`
  - `TestPlanRemoveShellPromotesMembers`
  - `TestPlanRemovePromotesSoleRemainingVersion`
  - `TestPlanRemoveDefaultAmbiguous`
- [ ] **Steps 2–4.** Commit `feat(install): plan add and remove edits for the three files`.

### Task 5: `brickkit add`

**Files:**
- Create: `internal/cli/add.go`, `internal/cli/add_local.go`, `internal/cli/install_apply.go` (applies a `install.Plan` to the files and rolls back on failure).
- Test: `internal/cli/add_test.go`, `internal/cli/add_git_test.go`.
- Modify: `internal/cli/rebuilding.go` (drop the add stub), `internal/cli/root.go` (`Options.RepoCacheDir`), msgids.

**Rules:**
- **Flags,** per 命令表 6: `--yes/-y`, `--repo`, `--repo-all`, `--local`. `--local` cannot be combined with an argument, `--repo` or `--repo-all`. `--init` is P7 and is not registered.
- **Flow:**
  1. Load the project (`project.Load`). A missing brickkit.yaml gives the existing "run `brickkit init`" error.
  2. Parse the ref.
  3. With no version, resolve the latest version (`LatestVersion`) and print "latest is X".
  4. Build a graph from the target plus all declared refs. If the target Manifest is a shell, add its declared members as roots.
  5. `install.PlanAdd`.
  6. `$var:` prompt: for each new config file key whose name exists in `config/vars.yaml` (and in the deploy file's `vars`), ask `检测到 config/vars.yaml 中已有 DB_HOST，是否引用？[y/N]`. `--yes` means yes. No stdin means no.
  7. Apply the plan: back up bytes → edit brickkit.yaml → edit deploy.yaml and deploy.local.yaml if present → write the skeletons with `configdir.Skeleton` → `project.Load` as a check. If the check fails, restore every file and delete the created files, then return the load error wrapped in `InstallApplyFailed`.
  8. Download artifacts, where a failure is a warning (existing behaviour).
  9. Clone for `--repo`/`--repo-all` (`workspace.Clone`, then check out the tag; a subpath is cloned to a temp dir and its subdirectory moved in).
  10. Print the tree, the notes, the signatures, and the "other deploy files not touched" note.
- **`--local`:** every component the local sources provide, at its real version (A8). Components already declared are skipped. Components are added in dependency order within one plan, applied once.
- **Output keeps the old shape** (✅ lines per ref with "(default)" / "(requiredBy X)" / "(in erp/shell)").

- [ ] **Step 1: Failing tests.**
  - `TestAddFromGitWritesThreeLayers` (gittest remote with erp/api 1.0.0 having a `configSchema`, and erp/portal depending on it): brickkit.yaml, deploy.yaml, the config skeleton, and `up --dry-run` succeeds afterwards.
  - `TestAddLatestFromTags`
  - `TestAddDependencyOtherVersionCoexists`
  - `TestAddShellWithMembers` (Review Focus 4): P3f's check passes in `up --dry-run`.
  - `TestAddWritesDeployLocalToo`
  - `TestAddVarPrompt`: stdin `y`, `--yes`, and no stdin.
  - `TestAddRollsBackWhenProjectWouldNotLoad`: inject a pre-existing inconsistency the plan cannot fix, so the files are unchanged afterwards.
  - `TestAddOfflineAfterFirstFetch` (Review Focus 1)
  - `TestAddRepoChecksOutTag`
  - `TestAddLocalAddsAllAtRealVersions`
  - `TestAddOtherVersionPointsToUpgrade`
- [ ] **Steps 2–4.** Commit `feat(cli): add writes brickkit.yaml, deploy files and config skeletons from git or local sources`.

### Task 6: `brickkit remove`

**Files:**
- Create: `internal/cli/remove.go`.
- Test: `internal/cli/remove_test.go`.
- Modify: `internal/cli/rebuilding.go` (delete the file once add and remove both exist), msgids.

**Rules:**
- **Flags:** `--force` (命令表 7).
- **Target:** a bare id resolves to the only version, or errors listing the versions when there are several (`InstallRemoveNeedsVersion`).
- **Graph:** the declared refs, so dependents can be checked.
- **Apply:** `install.PlanRemove`, applied with the same backup, rollback and load check as `add`. Then the source directory is deleted with the existing workspace guard (`--force` overrides uncommitted/unpushed changes). The source directory is deleted only when no version of the id remains.
- **Output:** what was removed (including cascaded versions), what was archived, members released, and the version promoted.

- [ ] **Step 1: Failing tests.**
  - `TestRemoveArchivesConfigAndEntries`
  - `TestRemoveBlockedByDependent`
  - `TestRemoveCascadesVersionKeptForDependent` (Review Focus 5)
  - `TestRemoveShellReleasesMembers`
  - `TestRemovePromotesSoleRemainingVersion`
  - `TestRemoveNeedsVersionWhenSeveral`
  - `TestRemoveDeletesSourceDirUnlessDirty` (`--force`)

  Each test ends with `up --dry-run` succeeding.
- [ ] **Steps 2–4.** Commit `feat(cli): remove archives config, syncs deploy files and releases shell members`.

### Task 7: `brickkit fetch` on git sources

**Files:**
- Modify: `internal/cli/fetch.go` (only if needed), `internal/cli/fetch_test.go`.

**Rules:**
- `fetch id[@v]` resolves the latest tag when there is no version, writes the Manifest cache and the artifacts, and never touches the three files. It also works when the project has no deploy file (fetch only needs `sources`).
- Artifact failures are errors (existing behaviour).

- [ ] **Step 1: Failing tests.**
  - `TestFetchFromGitDoesNotTouchProjectFiles`: byte-compare brickkit.yaml, deploy.yaml and config/.
  - `TestFetchLatestTag`
- [ ] **Steps 2–4.** Commit `feat(cli): fetch reads git sources`.

### Task 8: `up` checks local repo versions (A22, A24)

**Files:**
- Create: `internal/project/localrepo.go`: `func (p *Project) LocalRepo(id string) (dir string, ok bool)`. It looks at the component's own `source.type: local` path first, then each local source's `<root>/<scope>/<name>` (as `source.localSource.componentDir`), then `components/<scope>/<name>` (the `--repo` clone). It is used by `up_local.go` (replacing `workspace.SourceDir`) and by the new check.
- Modify: `internal/cli/up_local.go` (`checkLocalSources` → `checkLocalRepos`), msgids.
- Test: `internal/cli/localrepo_test.go`, `internal/project/localrepo_test.go`.

**Rules:**
- **Which refs are checked:** every ref this run runs from a local repo:
  - `mode: local`/`debug` components that are running;
  - members of a running bare-process shell (`shell.Members` hosted by a shell whose own entry is bare).
- **For each ref, read `<dir>/component.yaml` `metadata.version`:**
  - Missing dir: the existing "no local source" error.
  - Repo version ≠ `ref.Version`: `LocalRepoVersionMismatch`, with hints `brickkit upgrade <id>@<repo version>` and `git -C <dir> checkout <tag of ref.Version>`.
  - `ref` is not the default version: `LocalRepoNotDefault` (A22: a bare-process shell hosting a non-default version is blocked). The hint is to make it the default first (`brickkit upgrade`).
- **Container deployments are not checked** (A22).

- [ ] **Step 1: Failing tests.**
  - `TestLocalRepoVersionMustMatchDefault`
  - `TestBareShellMemberRepoMustMatchDeclared`: the shell runs `mode: local`, a member repo is at 1.1.0, and the shell declares 1.0.0, so there is an error.
  - `TestBareShellHostingNonDefaultBlocked`
  - `TestContainerDeploymentNotChecked`
  - `TestLocalRepoFindsShellDirectory`: a `mode: local` shell under `shell/erp/shell` is found, where before this it looked in `components/`.
- [ ] **Steps 2–4.** Commit `feat(cli): up checks that local repos hold the version this run expects`.

### Task 9: Final review

- [ ] Run `review-package` over the plan's range. Dispatch the final reviewer (opus) with the Review Focus above, the ledger's `Ruling:` lines, and the user rulings above. Do one fix pass, verified by TDD. Report to the user in Chinese. Update the roadmap row and project memory.
