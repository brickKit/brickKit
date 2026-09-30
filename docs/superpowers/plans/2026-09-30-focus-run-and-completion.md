# Focus run, one `components/`, and shell completion — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Work on one component from inside its directory in a large project (a *focus run* written in
`deploy.local.yaml`), keep every component's source in exactly one place, say when submodule code is missing,
and complete component IDs in the shell — installed automatically — with "did you mean" on typos.

**Architecture:** Project commands resolve the project root by walking up to the nearest `brickkit.yaml`
(`project.FindRoot`), keeping the user's directory separately (`Options.CallDir`) for display and for
"which component am I in". The focus is a personal-only top-level field of the deploy file; it takes effect
only through `deployfile.File.EntryAt` (the focused default version reads as `mode: local`) and through
`cascade.Compute` (the starting points become the focus plus explicit pins, and only what they reach runs).
Completion uses cobra's `ValidArgsFunction`s reading local files only; `install.sh` puts the generated
scripts where each shell loads them.

**Tech Stack:** Go 1.22, cobra v1.10.2 (`ValidArgsFunction`, `__complete`), `gopkg.in/yaml.v3`, POSIX sh.

**Spec:** `docs/superpowers/specs/2026-09-30-focus-run-and-completion-design.md` (read it first; this plan
argues from it and cites its sections as §N).

## Global Constraints

- The focus is written **only** in `deploy.local.yaml` (§4.2); `deploy.yaml` and any other `-f` file with
  `focus:` is rejected. Nothing about "what starts" is decided outside the deploy file (§2, the removed
  `--only`).
- Flag names: `--focus <id>`, `--all` (§4.3). Error codes: existing ones only (§4.7).
- Writing `focus:` changes only that line of `deploy.local.yaml`; every other byte stays (§4.3).
- `sync` ignores the focus; `graph` never reads local mode (§4.6).
- Nested directories are never moved or deleted by the CLI (§5); submodules are never fetched (§6).
- Completion reads only local files — no network, no market — and prints nothing outside a project (§7.1).
- `install.sh` never edits `~/.zshrc` or any rc file; `BRICKKIT_NO_COMPLETION=1` skips completion (§7.3).
- Every user-visible string goes through `internal/msgid` + both catalogs (`internal/i18n/locales/{en,zh}.yaml`,
  then `make generate-msgid`); placeholders positional (`%[1]s`); every call passes exactly the arguments
  its English text uses (guarded).
- Every doc example and output comes from a real run (`BRICKKIT_LANG=en` for English pages); en and zh are
  written independently.
- Code comments are in Chinese, like the rest of the repository.
- Each code task ends with `go build ./... && go vet ./...`, `.tools/bin/golangci-lint run ./...`, and
  `systemd-run --user --scope -q -p MemoryMax=8G -p MemorySwapMax=0 go test ./internal/... ./tests/... ./tools/...`
  with the real exit code checked (never through `| grep`). Full `make -k lint` is required green from
  Task 12 on (doc guards need the pages that Tasks 12–16 write); `make -k test-all` at Task 17.
- Commit after each task, message ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`;
  work on `main`, no worktree.

## Review Focus

1. **Auto-focus from a shell's directory** — `shell/<scope>/<name>/` is a local source too (`local-shells`);
   `up` there must focus that shell, exactly like a component under `components/`. (Task 4,
   `TestUpFromShellDirectoryFocusesTheShell`.)
2. **A `deploy.local.yaml` with personal edits and comments** — writing the focus keeps every other byte;
   a comment line containing `focus:` and a file without a final newline must not confuse the edit. (Task 2,
   `TestSetFocusKeepsEveryOtherByte`.)
3. **A workbench inside the project** — `up` in a component directory that has its own `brickkit.yaml` runs
   that workbench (nearest wins) and writes nothing into the outer project. (Task 4,
   `TestUpInsideAWorkbenchRunsTheWorkbench`.)
4. **A weak-dependency cycle unrelated to the focus** — neither member is top-level, so the "top-level runs"
   rule never stops them; under a focus they must not start. (Task 3,
   `TestFocusDoesNotStartAnUnrelatedCycle`.)
5. **Completion outside a project, or in a broken project** — no error block, no output, fast exit; an error
   printed there would corrupt the user's shell prompt. (Task 10,
   `TestCompletionOutsideAProjectIsSilent`, `TestCompletionInABrokenProjectIsSilent`.)

---

### Task 1: Find the project from a subdirectory

**Files:**
- Create: `internal/project/findroot.go`, `internal/project/findroot_test.go`
- Modify: `internal/cli/root.go` (`Options.CallDir`, `PersistentPreRunE`, `Options.display`)
- Modify: every `displayPath(opts.WorkDir, …)` call in `internal/cli/*.go` → `opts.display(…)`
- Modify: `internal/cli/new.go` (`--path` relative to `CallDir`)
- Modify: the command constructors listed in §3 (annotation)
- Modify: `internal/i18n/locales/{en,zh}.yaml`, then `make generate-msgid`
- Test: `internal/cli/findroot_test.go`

**Interfaces:**
- Produces:
  - `func project.FindRoot(dir string) (root string, found bool, err error)` — `dir` made absolute; walks
    `dir`, its parent, … to `/`; returns the first directory containing `brickkit.yaml`. Never stops at `.git`.
  - `Options.CallDir string` — the directory the command was run from (absolute); `Options.WorkDir` becomes
    the project root for project commands.
  - `func (o *Options) display(path string) string` — `displayPath(o.CallDir, path)`.
  - `const annotFindsProject = "brickkit/finds-project"`; `func findsProject(cmd *cobra.Command) bool`
    (checks the command and its ancestors).

- [ ] **Step 1: Write `FindRoot`'s tests**

```go
package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 向上找最近的 brickkit.yaml：像 git 找 .git，但不在嵌套的 .git 处停下——components/ 下的组件
// 往往自己就是一个 git 仓库。
func TestFindRootWalksUpToTheNearestProject(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, FileDecl), []byte("project: shop\n"), 0o644))
	deep := filepath.Join(root, "components", "erp", "api", "internal")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "components", "erp", "api", ".git"), 0o755))
	require.NoError(t, os.MkdirAll(deep, 0o755))

	got, found, err := FindRoot(deep)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, root, got)
}

// 就近原则：工作台（组件目录里自己的 brickkit.yaml）就是项目，不再往上找。
func TestFindRootStopsAtAWorkbench(t *testing.T) {
	root := t.TempDir()
	bench := filepath.Join(root, "components", "erp", "api")
	require.NoError(t, os.MkdirAll(bench, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, FileDecl), []byte("project: shop\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(bench, FileDecl), []byte("project: erp-api\n"), 0o644))

	got, found, err := FindRoot(bench)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, bench, got)
}

func TestFindRootReportsNothingOutsideAProject(t *testing.T) {
	_, found, err := FindRoot(t.TempDir())
	require.NoError(t, err)
	assert.False(t, found)
}
```

(`t.TempDir()` lives under `/tmp`; a stray `/tmp/brickkit.yaml` on the machine would break the last test —
the test is written to fail loudly in that case rather than be skipped.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/project -run TestFindRoot`
Expected: FAIL to compile — `undefined: FindRoot`.

- [ ] **Step 3: Implement**

```go
package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// FindRoot 从 dir 往上找最近的一个含 brickkit.yaml 的目录，就像 git 找 .git。
//
// 不在嵌套的 .git 处停下：大项目 components/ 下的组件多半各自是 git 仓库，
// 在里面干活时要找的正是外面那个项目。离 dir 最近的 brickkit.yaml 胜出——组件目录里
// 自己有一份（工作台）时，它就是项目。
func FindRoot(dir string) (root string, found bool, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", false, err
	}
	for {
		_, statErr := os.Stat(filepath.Join(dir, FileDecl))
		switch {
		case statErr == nil:
			return dir, true, nil
		case !errors.Is(statErr, fs.ErrNotExist):
			return "", false, statErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}
```

- [ ] **Step 4: Run the project tests**

Run: `go test ./internal/project -run TestFindRoot -count=1`
Expected: PASS.

- [ ] **Step 5: Write the CLI-level tests** (`internal/cli/findroot_test.go`; the fixture `three-layer-shell`
  is a complete project named `shop` with local sources — `copyFixture` copies it into a temp dir)

```go
package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 在项目的子目录里跑项目命令：命令作用于项目，第一行说明用的是哪个项目。
func TestProjectCommandFromSubdirectoryUsesTheProject(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	r := runWithEngine(t, newFakeEngine(), filepath.Join(dir, "config"), "status")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, i18n.T(msgid.CliProjectFoundAbove, "..", "shop"))
}

// 在项目根目录跑：不打印那一行（没有向上找）。
func TestProjectCommandAtTheRootSaysNothingExtra(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	r := runWithEngine(t, newFakeEngine(), dir, "status")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "📁")
}

// 只作用于当前目录的命令不向上找：release 读的是当前目录的 component.yaml。
func TestReleaseDoesNotLookUpward(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	r := runIn(t, filepath.Join(dir, "config"), "release")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.NotContains(t, r.stdout, "📁")
}

// new --path 的相对路径相对使用者所在的目录，不是项目根。
func TestNewPathIsRelativeToWhereYouAre(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	sub := filepath.Join(dir, "config")
	r := runIn(t, sub, "new", "demo/x", "--path", "x")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.FileExists(t, filepath.Join(sub, "x", "component.yaml"))
}

// 子目录里显示的路径相对使用者所在的目录，像 git 一样。
func TestPathsAreShownRelativeToWhereYouAre(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	r := runWithEngine(t, newFakeEngine(), filepath.Join(dir, "config"), "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, filepath.Join("..", ".brickkit", "generated"))
}
```

Add the message: `cli.project_found_above` — en `"📁 Project: %[1]s (%[2]s)"`, zh `"📁 项目：%[1]s（%[2]s）"`
(`%[1]s` the root relative to where you are, `%[2]s` the project name), next to `cli.root.*`; run
`make generate-msgid`.

- [ ] **Step 6: Run to verify they fail**

Run: `go test ./internal/cli -run 'TestProjectCommand|TestReleaseDoesNotLookUpward|TestNewPathIsRelative|TestPathsAreShown' -count=1`
Expected: FAIL — `status` in `config/` reports `PROJECT_MISSING`.

- [ ] **Step 7: Implement in the CLI**

In `root.go`:

```go
	// CallDir 是使用者敲命令时所在的目录（绝对路径）。项目命令向上找到项目后，WorkDir 换成
	// 项目根，而显示给人看的路径、"我在哪个组件里"都按 CallDir 算——像 git 一样。
	CallDir string
```

```go
// annotFindsProject 标出"作用于项目"的命令：在项目的子目录里运行时向上找项目根（设计 §3）。
// 只作用于当前目录的命令（init、release、publish、skills）不带它。
const annotFindsProject = "brickkit/finds-project"

func findsProject(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[annotFindsProject] == "true" {
			return true
		}
	}
	return false
}

// display 把路径显示成相对使用者所在目录的样子。
func (o *Options) display(path string) string { return displayPath(o.CallDir, path) }

// enterProject 在项目命令开始前定位项目根：当前目录没有 brickkit.yaml 就往上找；找到了就
// 换过去并说一句用的是哪个项目。找不到时什么都不改，命令照旧报它自己的 PROJECT_MISSING。
func (o *Options) enterProject() error {
	root, found, err := project.FindRoot(o.WorkDir)
	if err != nil || !found {
		return err
	}
	if filepath.Clean(root) == filepath.Clean(o.CallDir) {
		return nil
	}
	o.WorkDir = root
	name := filepath.Base(root)
	if decl, err := projfile.ParseFile(project.NewLayout(root).DeclPath()); err == nil && decl.Project != "" {
		name = decl.Project
	}
	o.Printf("%s\n", i18n.T(msgid.CliProjectFoundAbove, o.display(root), name))
	return nil
}
```

In `PersistentPreRunE`, before the log line:

```go
		if opts.CallDir == "" {
			abs, err := filepath.Abs(opts.WorkDir)
			if err != nil {
				return err
			}
			opts.WorkDir, opts.CallDir = abs, abs
		}
		if findsProject(cmd) {
			if err := opts.enterProject(); err != nil {
				return err
			}
		}
```

Set `Annotations: map[string]string{annotFindsProject: "true"}` on the constructors of `up`, `down`,
`status`, `sync`, `lint`, `graph`, `restore`, `local`, `add`, `remove`, `upgrade`, `fetch`, `build`, `deps`,
`new`, `login`, `logout` (§3 table). Replace every `displayPath(opts.WorkDir, x)` with `opts.display(x)`
(`git grep -n "displayPath(opts.WorkDir"` must be empty afterwards). In `new.go`, resolve a relative
`--path` against `opts.CallDir` instead of `opts.WorkDir`; the default `components/<id>` / `shell/<id>`
stays relative to `opts.WorkDir` (the project root). `restore --check` run from the hook sets its own
directory — leave `restore_check.go` reading `opts.WorkDir` (now the root).

- [ ] **Step 8: Run the tests, then the suite**

Run: `go test ./internal/project ./internal/cli -count=1`
Expected: PASS (existing tests call `runIn(t, root, …)`, where `CallDir == WorkDir`).

Run the Global Constraints gates. Expected: all exit 0.

- [ ] **Step 9: Commit**

```bash
git add internal/project/findroot.go internal/project/findroot_test.go internal/cli internal/i18n/locales internal/msgid
git commit -m "feat: project commands find the project from a subdirectory, like git"
```

---

### Task 2: The `focus:` field in the deploy file

**Files:**
- Modify: `internal/deployfile/deployfile.go` (`File.Focus`, `EntryAt`)
- Create: `internal/deployfile/focus.go` (`SetFocus`), `internal/deployfile/focus_test.go`
- Modify: `internal/deployfile/validate.go`
- Modify: `internal/i18n/locales/{en,zh}.yaml`, `make generate-msgid`
- Modify: `schemas/deploy.schema.json` (`make generate-schemas`)
- Modify: `docs/{en,zh}/11-reference/03-deploy-yaml-schema.md`, `docs/{en,zh}/01-three-layers/09-field-reference.md`
  (one row each for `focus`: the field-coverage guard in `tests/docfields` requires it)

**Interfaces:**
- Produces:
  - `File.Focus string` (`yaml:"focus,omitempty"`) — a bare component ID.
  - `func (f *File) EntryAt(id, version string, isDefault bool) (Located, bool)` — unchanged signature; when
    `id == f.Focus && isDefault` and the entry's mode is `""`, `enabled` or `local`, the returned entry's
    `Mode` is `local`; `debug` and `disable` are returned as written.
  - `func SetFocus(data []byte, id string) ([]byte, error)` — `id == ""` removes the line.
  - Messages: `deployfile.focus_only_local`, `config.focus_k8s_unsupported`, `deployfile.focus_invalid`.

- [ ] **Step 1: Write the tests**

```go
package deployfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const personal = "# my notes about this file\n" +
	"target: docker          # docker | podman | k8s\n" +
	"\n" +
	"components:\n" +
	"  # focus: this comment mentions the word and must stay\n" +
	"  - id: erp/api\n" +
	"    localPort: 8081\n" +
	"  - id: erp/db"

// 写焦点只动 focus: 这一行：注释、对齐用的空格、没有结尾换行，一个字节都不变（设计 §4.3）。
func TestSetFocusKeepsEveryOtherByte(t *testing.T) {
	out, err := SetFocus([]byte(personal), "erp/api")
	require.NoError(t, err)
	want := "# my notes about this file\n" +
		"target: docker          # docker | podman | k8s\n" +
		"focus: erp/api\n" +
		personal[len("# my notes about this file\ntarget: docker          # docker | podman | k8s\n"):]
	assert.Equal(t, want, string(out))

	again, err := SetFocus(out, "erp/db")
	require.NoError(t, err)
	assert.Equal(t, strings.Replace(want, "focus: erp/api", "focus: erp/db", 1), string(again))

	cleared, err := SetFocus(again, "")
	require.NoError(t, err)
	assert.Equal(t, personal, string(cleared))
}

// 没有 target: 行时（手改过的文件），焦点写在文件头注释之后的第一行。
func TestSetFocusWithoutATargetLine(t *testing.T) {
	out, err := SetFocus([]byte("# header\ncomponents:\n  - id: erp/api\n"), "erp/api")
	require.NoError(t, err)
	assert.Equal(t, "# header\nfocus: erp/api\ncomponents:\n  - id: erp/api\n", string(out))
}

// 焦点对准的默认版本读出来是 mode: local；debug / disable 按写的来；别的组件不受影响。
func TestEntryAtAppliesTheFocus(t *testing.T) {
	f := &File{Target: TargetDocker, Focus: "erp/api", Components: []Component{
		{Entry: Entry{ID: "erp/api"}}, {Entry: Entry{ID: "erp/db"}},
	}}
	e, ok := f.Entry("erp/api", "1.0.0", true)
	require.True(t, ok)
	assert.Equal(t, ModeLocal, e.Mode)
	e, _ = f.Entry("erp/db", "1.0.0", true)
	assert.Equal(t, "", e.Mode)

	f.Components[0].Mode = ModeDebug
	e, _ = f.Entry("erp/api", "1.0.0", true)
	assert.Equal(t, ModeDebug, e.Mode)

	e, _ = f.Entry("erp/api", "0.9.0", false)
	assert.Equal(t, ModeDebug, e.Mode, "只有默认版本从源码跑")
}

// 焦点是个人的事：团队文件里写了就拒绝；k8s 上拒绝（集群够不着你的机器）。
func TestFocusValidation(t *testing.T) {
	f := &File{Target: TargetDocker, Focus: "erp/api", Components: []Component{{Entry: Entry{ID: "erp/api"}}}}
	_, err := f.Validate(RoleTeam)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "focus")

	_, err = f.Validate(RoleLocal)
	require.NoError(t, err)

	f.Target = TargetK8s
	_, err = f.Validate(RoleLocal)
	require.Error(t, err)

	f.Target, f.Focus = TargetDocker, "not a component id"
	_, err = f.Validate(RoleLocal)
	require.Error(t, err)
}

// 焦点组件本身写了 localPort（没写 mode）：它按 local 跑，localPort 合法。
func TestFocusEntryMayCarryALocalPort(t *testing.T) {
	f := &File{Target: TargetDocker, Focus: "erp/api", Components: []Component{{Entry: Entry{ID: "erp/api", LocalPort: 8081}}}}
	_, err := f.Validate(RoleLocal)
	require.NoError(t, err)
}
```

(The test file imports `strings`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/deployfile -run 'TestSetFocus|TestEntryAtAppliesTheFocus|TestFocus' -count=1`
Expected: FAIL to compile — `Focus` unknown field, `undefined: SetFocus`.

- [ ] **Step 3: Implement**

`deployfile.go` — add after `Target`:

```go
	// Focus 是焦点组件（裸 ID）：只有它和它需要的组件启动，它从源码跑（设计 §4）。
	// 只允许写在个人文件 deploy.local.yaml 里，与 mode: debug 同一条规则。
	Focus string `yaml:"focus,omitempty"`
```

In `EntryAt`, wrap both returns:

```go
	if entryVersion == version { return f.focused(l, isDefault... ) }
```

concretely:

```go
func (f *File) EntryAt(id, version string, isDefault bool) (Located, bool) {
	var bare *Located
	for _, l := range f.All() {
		entryID, entryVersion := l.Key()
		if entryID != id {
			continue
		}
		if entryVersion == version {
			return f.withFocus(l, isDefault), true
		}
		if entryVersion == "" && isDefault {
			found := l
			bare = &found
		}
	}
	if bare != nil {
		return f.withFocus(*bare, isDefault), true
	}
	return Located{}, false
}

// withFocus：焦点组件的默认版本从源码跑——没写 mode、写了 enabled 或 local 都按 local；
// debug 按写的来（交给 IDE 启动）；disable 原样返回，由项目校验报两个意图矛盾。
// 条目是值拷贝，文件本身一个字段都不改。
func (f *File) withFocus(l Located, isDefault bool) Located {
	id, _ := l.Key()
	if f.Focus == "" || id != f.Focus || !isDefault {
		return l
	}
	switch l.Mode {
	case "", ModeEnabled, ModeLocal:
		l.Mode = ModeLocal
	}
	return l
}
```

`validate.go` — in `Validate`, call `f.validateFocus(p, role)` after `validateTarget`:

```go
// validateFocus：焦点只写在个人文件里；k8s 上不行（集群里的 Pod 够不着你的机器）；值是组件 ID。
func (f *File) validateFocus(p *clierr.ProblemSet, role Role) {
	if f.Focus == "" {
		return
	}
	switch {
	case role != RoleLocal:
		p.Add("focus", i18n.T(msgid.DeployfileFocusOnlyLocal))
	case manifest.ComponentIDProblem(f.Focus) != "":
		p.Add("focus", i18n.T(msgid.DeployfileFocusInvalid, f.Focus))
	case f.Target == TargetK8s:
		p.Add("focus", i18n.T(msgid.ConfigFocusK8sUnsupported))
	}
}
```

In `validatePorts`, the `localPort` check uses "is a bare process"; pass the entry through `f.withFocus`
for the bare ID entry of the focus, i.e. replace `!c.IsBareProcess()` with
`!c.IsBareProcess() && !(f.Focus != "" && c.ID == f.Focus)`. (`validatePorts` is a function; give it the
file as its first argument.)

Messages (en / zh), next to `deployfile.*`:
- `deployfile.focus_only_local`: `"focus is personal: write it in deploy.local.yaml (brickkit up --focus <id> writes it for you)"` /
  `"focus 是个人的事：写在 deploy.local.yaml 里（brickkit up --focus <id> 会替你写）"`
- `deployfile.focus_invalid`: `"%[1]q is not a component ID (<scope>/<name>)"` / `"%[1]q 不是组件 ID（<scope>/<name>）"`
- `config.focus_k8s_unsupported`: `"a focus run starts a process on your machine, which a cluster cannot reach; use target docker or podman"` /
  `"焦点运行要在你的机器上起进程，集群够不着；改用 target docker 或 podman"`

`focus.go`:

```go
package deployfile

import (
	"bytes"
	"regexp"
)

var (
	focusLine  = regexp.MustCompile(`(?m)^focus:.*(\n|$)`)
	targetLine = regexp.MustCompile(`(?m)^target:.*\n`)
	commentRun = regexp.MustCompile(`\A(?:#.*\n|\n)*`)
)

// SetFocus 把个人部署文件的 focus: 设成 id（id 为空时删掉这一行），只动这一行：
// 注释、对齐用的空格、其余字段一个字节都不变（设计 §4.3）。按行编辑而不是经过 YAML
// 重新编码——重新编码会改掉 `target: docker          # …` 这种对齐。
// 写在 target: 下面；没有 target: 行时写在文件头注释之后。
func SetFocus(data []byte, id string) ([]byte, error) {
	line := []byte("focus: " + id + "\n")
	if loc := focusLine.FindIndex(data); loc != nil {
		if id == "" {
			return append(append([]byte{}, data[:loc[0]]...), data[loc[1]:]...), nil
		}
		return append(append(append([]byte{}, data[:loc[0]]...), line...), data[loc[1]:]...), nil
	}
	if id == "" {
		return data, nil
	}
	at := len(commentRun.Find(data))
	if loc := targetLine.FindIndex(data); loc != nil {
		at = loc[1]
	}
	return bytes.Join([][]byte{data[:at], line, data[at:]}, nil), nil
}
```

(A `focus:` line whose value would be replaced keeps no trailing comment; that is fine — it is the CLI's own
line.)

Regenerate: `make generate-msgid && make generate-schemas`. Add the `focus` row to both field references
(en: "`focus` | string | Personal only (`deploy.local.yaml`): the component a focus run centres on — it and
what it needs start, it runs from source. See [Developing inside the project](../02-project-guide/04-focus-run.md)."
— the link target is written in Task 13; until then link to `04-deploy-local-yaml.md` and let Task 13 retarget it).

- [ ] **Step 4: Run the tests and the guards**

Run: `go test ./internal/deployfile ./tests/docfields ./internal/schemagen -count=1`
Expected: PASS.

Run the Global Constraints gates. Expected: all exit 0.

- [ ] **Step 5: Commit**

```bash
git add internal/deployfile internal/i18n/locales internal/msgid schemas docs/en/11-reference docs/zh/11-reference docs/en/01-three-layers docs/zh/01-three-layers
git commit -m "feat: focus, a personal-only field of the deploy file"
```

---

### Task 3: What starts under a focus

**Files:**
- Modify: `internal/project/check.go` (focus checks), `internal/project/load.go` (`IgnoreFocus`, `FocusRef`)
- Modify: `internal/cascade/cascade.go`
- Modify: `internal/i18n/locales/{en,zh}.yaml`, `make generate-msgid`
- Test: `internal/cascade/focus_test.go`, `internal/project/focus_test.go`

**Interfaces:**
- Consumes: `File.Focus`, `File.EntryAt` (Task 2).
- Produces:
  - `func (p *Project) FocusRef() (id, version string, ok bool)` — the focused default version.
  - `func (p *Project) IgnoreFocus()` — clears `p.Deploy.Focus` in memory (used by `sync`).
  - `cascade.Compute` honours the focus; reasons `cascade.reason.focus` ("starting (focus)") and
    `cascade.reason.outside_focus` ("not starting (outside the focus)").

- [ ] **Step 1: Write the cascade tests** (`internal/cascade/focus_test.go`, package `cascade_test`, reusing
  `newGraph`, `spec`, `cfgOf`, `entry`, `runningIDs`, `reasonOf` from `cascade_test.go`)

```go
package cascade_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
)

func focusOn(p *project.Project, id string) *project.Project {
	p.Deploy.Focus = id
	return p
}

// 焦点：只有焦点组件和它需要的组件跑（强弱依赖都算），别的顶层不跑。
func TestFocusStartsOnlyTheFocusAndWhatItNeeds(t *testing.T) {
	graph := newGraph(t,
		spec{id: "shop/web", requires: []string{"erp/api"}},
		spec{id: "erp/api", requires: []string{"erp/db"}, optional: []string{"infra/cache"}},
		spec{id: "erp/db"},
		spec{id: "infra/cache"},
		spec{id: "shop/report"},
	)
	p := focusOn(cfgOf(entry("shop/web", ""), entry("erp/api", ""), entry("erp/db", ""),
		entry("infra/cache", ""), entry("shop/report", "")), "erp/api")

	result, err := cascade.Compute(p, graph)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"erp/api", "erp/db", "infra/cache"}, runningIDs(result))

	_, why := reasonOf(t, result, "erp/api")
	assert.Equal(t, i18n.T(msgid.CascadeReasonFocus), why)
	_, why = reasonOf(t, result, "erp/db")
	assert.Equal(t, i18n.T(msgid.CascadeReasonNeededBy, "erp/api"), why)
	for _, id := range []string{"shop/web", "shop/report"} {
		state, why := reasonOf(t, result, id)
		assert.Equal(t, cascade.StateSkipped, state, id)
		assert.Equal(t, i18n.T(msgid.CascadeReasonOutsideFocus), why, id)
	}
}

// 显式钉住的组件照样跑：mode: enabled 是写下来的意图。
func TestFocusKeepsExplicitPins(t *testing.T) {
	graph := newGraph(t, spec{id: "erp/api"}, spec{id: "shop/report"})
	p := focusOn(cfgOf(entry("erp/api", ""), entry("shop/report", "enabled")), "erp/api")

	result, err := cascade.Compute(p, graph)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"erp/api", "shop/report"}, runningIDs(result))
}

// 与焦点无关的弱依赖环不跑：环上谁都不是顶层，"顶层才跑"那条规则停不下它们（Review Focus 4）。
func TestFocusDoesNotStartAnUnrelatedCycle(t *testing.T) {
	graph := newGraph(t,
		spec{id: "x/a", optional: []string{"x/b"}},
		spec{id: "x/b", optional: []string{"x/a"}},
		spec{id: "erp/api"},
	)
	p := focusOn(cfgOf(entry("x/a", ""), entry("x/b", ""), entry("erp/api", "")), "erp/api")

	result, err := cascade.Compute(p, graph)
	require.NoError(t, err)
	assert.Equal(t, []string{"erp/api"}, runningIDs(result))
}

// 焦点需要的强依赖被关掉：两个意图矛盾，报 COMPONENT_DISABLED（与钉住的组件同一条错误）。
func TestFocusWithADisabledRequirementIsAnError(t *testing.T) {
	graph := newGraph(t, spec{id: "erp/api", requires: []string{"erp/db"}}, spec{id: "erp/db"})
	p := focusOn(cfgOf(entry("erp/api", ""), entry("erp/db", "disable")), "erp/api")

	_, err := cascade.Compute(p, graph)
	require.Error(t, err)
	assert.Equal(t, clierr.CodeComponentDisabled, clierr.As(err).Code)
}

// 没写焦点时一切照旧：同一张图，顶层照跑。
func TestNoFocusKeepsTheTopLevelRule(t *testing.T) {
	graph := newGraph(t, spec{id: "erp/api"}, spec{id: "shop/report"})
	result, err := cascade.Compute(cfgOf(entry("erp/api", ""), entry("shop/report", "")), graph)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"erp/api", "shop/report"}, runningIDs(result))
}
```

- [ ] **Step 2: Write the project tests** (`internal/project/focus_test.go`)

```go
package project_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/project/projecttest"
)

// loadFocused 写一个只有 erp/api 的项目，个人文件里写 focus 与 erp/api 条目的额外行，按个人文件装载。
func loadFocused(t *testing.T, focus, apiExtra string) (*project.Project, error) {
	t.Helper()
	root := t.TempDir()
	projecttest.Write(t, root, projecttest.Files{
		"brickkit.yaml":     "project: shop\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n",
		"deploy.yaml":       "target: docker\ncomponents:\n  - id: erp/api\n",
		"deploy.local.yaml": "target: docker\nfocus: " + focus + "\ncomponents:\n  - id: erp/api\n" + apiExtra,
	})
	return project.LoadTopology(root, project.LoadOptions{DeployFile: filepath.Join(root, "deploy.local.yaml")})
}

// 焦点指向 brickkit.yaml 里没有的组件：COMPONENT_NOT_FOUND。
func TestFocusOnAnUnknownComponent(t *testing.T) {
	_, err := loadFocused(t, "erp/apj", "")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeComponentNotFound, clierr.As(err).Code)
}

// 焦点组件的条目写着 mode: disable：COMPONENT_DISABLED。
func TestFocusOnADisabledEntry(t *testing.T) {
	_, err := loadFocused(t, "erp/api", "    mode: disable\n")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeComponentDisabled, clierr.As(err).Code)
}

// 焦点组件的默认版本读出来是 mode: local；IgnoreFocus 之后照旧。
func TestFocusAndIgnoreFocus(t *testing.T) {
	p, err := loadFocused(t, "erp/api", "")
	require.NoError(t, err)
	id, version, ok := p.FocusRef()
	require.True(t, ok)
	assert.Equal(t, "erp/api@1.0.0", id+"@"+version)
	assert.Equal(t, "local", p.DeployEntry("erp/api", "1.0.0").Mode)

	p.IgnoreFocus()
	_, _, ok = p.FocusRef()
	assert.False(t, ok)
	assert.Equal(t, "", p.DeployEntry("erp/api", "1.0.0").Mode)
}
```

(`-f deploy.local.yaml` loads it with the personal-file rules — the existing behaviour of `LoadFiles`.)

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/cascade ./internal/project -run 'Focus' -count=1`
Expected: FAIL (compile errors on `FocusRef`, `IgnoreFocus`, `CascadeReasonFocus`).

- [ ] **Step 4: Implement**

`load.go`:

```go
// FocusRef 返回焦点组件的默认版本；没写焦点时 ok 为 false。
func (p *Project) FocusRef() (id, version string, ok bool) {
	if p.Deploy == nil || p.Deploy.Focus == "" {
		return "", "", false
	}
	version, ok = p.Decl.DefaultVersion(p.Deploy.Focus)
	return p.Deploy.Focus, version, ok
}

// IgnoreFocus 让这次运行不看焦点（sync 用：它保留的是不带焦点时项目要跑的全部源码，设计 §4.6）。
// 只改内存里的解析结果，文件不动。
func (p *Project) IgnoreFocus() {
	if p.Deploy != nil {
		p.Deploy.Focus = ""
	}
}
```

`check.go` — a `checkFocus` step appended to `checkTopology`'s list:

```go
// checkFocus：焦点指向的组件要在 brickkit.yaml 里；它的条目不能写 mode: disable（两个意图矛盾）。
func (p *Project) checkFocus() error {
	id := p.Deploy.Focus
	if id == "" {
		return nil
	}
	version, ok := p.Decl.DefaultVersion(id)
	if !ok {
		return clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.ProjectFocusUnknown, id)).
			WithDetail(i18n.T(msgid.LabelFile), p.DeployPath).
			WithHint(i18n.T(msgid.ProjectHintFocusClear))
	}
	if raw, found := p.Deploy.Entry(id, version, true); found && raw.Mode == deployfile.ModeDisable {
		return clierr.New(clierr.CodeComponentDisabled, i18n.T(msgid.ProjectFocusDisabled, id)).
			WithDetail(i18n.T(msgid.LabelFile), p.DeployPath).
			WithHint(i18n.T(msgid.ProjectHintFocusDisabled, id))
	}
	return nil
}
```

(`Entry` returns `disable` unchanged — Task 2's `withFocus` leaves it.)

`cascade.go` — in `computeStopped`, after the disabled seeding, add the focus rule; `Compute` passes the
focus in:

```go
// 焦点运行（设计 §4.4）：起点只有焦点组件与显式钉住的组件；从起点沿依赖（强弱都算）走不到的
// 一律不跑。按可达性算而不是"顶层才跑"——与焦点无关的弱依赖环上谁都不是顶层，
// 那条规则停不下它们。走到被关掉的组件就不再往下走：它下面的只为它而跑。
func seedOutsideFocus(graph *resolver.Graph, decl declSet, focus resolver.Ref, stopped map[resolver.Ref]bool) {
	reached := map[resolver.Ref]bool{}
	var walk func(resolver.Ref)
	walk = func(ref resolver.Ref) {
		if reached[ref] || decl.disabled(ref) {
			return
		}
		reached[ref] = true
		node := graph.Node(ref)
		if node == nil {
			return
		}
		for _, dep := range node.Requires {
			walk(dep)
		}
		for _, dep := range node.Optional {
			walk(dep)
		}
	}
	walk(focus)
	for _, node := range graph.Nodes {
		if decl.pinned(node.Ref) {
			walk(node.Ref)
		}
	}
	for _, node := range graph.Nodes {
		if !reached[node.Ref] {
			stopped[node.Ref] = true
		}
	}
}
```

`Compute` gets the focus ref from `p.FocusRef()`; when set, call `seedOutsideFocus` right after the
disabled seeding (the focused ref is pinned already through `DeployEntry` → `mode: local`, so the existing
propagation and the pinned-conflict check need no change). `classify` gets two extra cases, checked before
the existing ones:

```go
	case focus && ref == focusRef:
		c.State, c.Reason = StateRunning, i18n.T(msgid.CascadeReasonFocus)
	case focus && stopped[ref] && !decl.disabled(ref) && blocker[ref] == (resolver.Ref{}):
		c.State, c.Reason = StateSkipped, i18n.T(msgid.CascadeReasonOutsideFocus)
```

Messages: `cascade.reason.focus` — en `"starting (focus)"`, zh `"启动（焦点）"`;
`cascade.reason.outside_focus` — en `"not starting (outside the focus)"`, zh `"不启动（焦点之外）"`;
`project.focus_unknown` — en `"Error: the focus %[1]s is not a component of this project"`, zh `"错误：焦点 %[1]s 不是这个项目的组件"`;
`project.focus_disabled` — en `"Error: the focus %[1]s is written mode: disable"`, zh `"错误：焦点 %[1]s 的条目写着 mode: disable"`;
`project.hint_focus_clear` — en `"brickkit up --focus <id> sets another one; brickkit up --all runs every component"`, zh `"brickkit up --focus <id> 换一个焦点；brickkit up --all 运行全部组件"`;
`project.hint_focus_disabled` — en `"Remove mode: disable from %[1]s, or focus on another component"`, zh `"去掉 %[1]s 的 mode: disable，或换一个焦点"`.
Match the look of the existing `cascade.reason.*` values in each catalog (check them first and follow their
wording form exactly).

- [ ] **Step 5: Run the tests and gates**

Run: `go test ./internal/cascade ./internal/project -count=1`
Expected: PASS. Then the Global Constraints gates.

- [ ] **Step 6: Commit**

```bash
git add internal/cascade internal/project internal/i18n/locales internal/msgid
git commit -m "feat: under a focus, only the focus and what it needs start"
```

---

### Task 4: `up --focus`, `--all`, and `up` from a component's directory

**Files:**
- Create: `internal/cli/focus.go` (`componentAt`, `ensureFocus`, `clearFocus`, `renderFocus`)
- Modify: `internal/cli/up.go` (flags, `runUp`, `renderDeploySource` callers)
- Modify: `internal/cli/status.go`, `internal/cli/lint.go`, `internal/cli/build.go`, `internal/cli/lifecycle.go`
  (print the focus line where the local-mode reminder is printed — find every caller of `renderDeploySource`
  and every place `CliUpUsingLocalDeployFile` appears)
- Modify: `internal/i18n/locales/{en,zh}.yaml`
- Test: `internal/cli/focus_test.go`

**Interfaces:**
- Consumes: `project.FindRoot`, `Options.CallDir` (Task 1); `deployfile.SetFocus` (Task 2); project focus
  checks (Task 3).
- Produces:
  - `func componentAt(l project.Layout, decl *projfile.File, dir string) (id string, ok bool)` — `dir` is
    inside `<local source root>/<scope>/<name>/` of one of `decl.Sources` of type `local` and that directory
    holds a `component.yaml`.
  - `func ensureFocus(opts *Options, l project.Layout, id string) error` — turns local mode on like
    `local on` (copy or reuse), writes `focus: id` with `SetFocus` when different, prints the line.
  - `func clearFocus(opts *Options, l project.Layout) error`.
  - `func renderFocus(opts *Options, proj *project.Project)` — `🎯 Focus: <id>` when set.

- [ ] **Step 1: Write the tests** (`internal/cli/focus_test.go`)

The fixture `three-layer-shell` (`copyFixture`) has local sources `components/` and `shell/`: `erp/portal` and
`erp/worker` both require `erp/api`; the shell `erp/shell` hosts `erp/api` and `erp/worker`. Components that run
as local processes need a start command; the fixture's directories hold only `component.yaml`, so the tests give
them an explicit `local.runCommand`. `--dry-run` generates everything and starts nothing.

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// focusFixture：外壳夹具，每个组件（和外壳）都写上显式的启动命令，好让它们能从源码跑。
func focusFixture(t *testing.T) string {
	t.Helper()
	dir := copyFixture(t, "three-layer-shell")
	for _, rel := range []string{"components/erp/portal", "components/erp/api", "components/erp/worker", "shell/erp/shell"} {
		editFile(t, filepath.Join(dir, rel, "component.yaml"), "deployment:\n",
			"local:\n  runCommand: [\"sleep\", \"60\"]\ndeployment:\n")
	}
	return dir
}

func in(dir string, rel ...string) string { return filepath.Join(append([]string{dir}, rel...)...) }

// 在组件目录里 up：焦点设成这个组件，写进 deploy.local.yaml，本地模式随之打开；
// 只有它和它需要的组件启动。
func TestUpFromAComponentDirectoryFocusesIt(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), in(dir, "components", "erp", "portal"), "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Contains(t, readFile(t, in(dir, "deploy.local.yaml")), "\nfocus: erp/portal\n")
	assert.FileExists(t, in(dir, ".brickkit", "local-mode"))
	assert.Contains(t, r.stdout, i18n.T(msgid.CliFocusSet, "erp/portal", "deploy.local.yaml"))
	assert.Contains(t, r.stdout, i18n.T(msgid.CascadeReasonFocus))
	assert.Contains(t, r.stdout, i18n.T(msgid.CascadeReasonNeededBy, "erp/portal"))
	assert.Contains(t, r.stdout, i18n.T(msgid.CascadeReasonOutsideFocus), "erp/worker 不在焦点之内")
	assert.Equal(t, readFile(t, in(dir, "deploy.yaml")), readFile(t, filepath.Join("testdata", "three-layer-shell", "deploy.yaml")),
		"团队文件一个字节都不动")
}

// shell/ 下的外壳目录一样（Review Focus 1）。
func TestUpFromShellDirectoryFocusesTheShell(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), in(dir, "shell", "erp", "shell"), "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, readFile(t, in(dir, "deploy.local.yaml")), "\nfocus: erp/shell\n")
}

// 组件目录里有自己的 brickkit.yaml（工作台）：跑的是工作台，外面项目的文件一个字节都不动（Review Focus 3）。
func TestUpInsideAWorkbenchRunsTheWorkbench(t *testing.T) {
	dir := focusFixture(t)
	bench := in(dir, "components", "erp", "portal")
	writeTree(t, bench, map[string]string{
		"brickkit.yaml": "project: erp-portal\ncomponents: []\n",
		"deploy.yaml":   "target: docker\ncomponents: []\n",
	})
	r := runWithEngine(t, newFakeEngine(), bench, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NoFileExists(t, in(dir, "deploy.local.yaml"))
	assert.NoFileExists(t, in(dir, ".brickkit", "local-mode"))
	assert.NotContains(t, r.stdout, "📁")
}

// --focus 在项目根也能用；--all 清掉焦点、全部照常启动；两个一起写、或配 -f / --no-local 是用法错误。
func TestUpFocusFlagAndAll(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/worker")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, readFile(t, in(dir, "deploy.local.yaml")), "\nfocus: erp/worker\n")

	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--all")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, readFile(t, in(dir, "deploy.local.yaml")), "focus:")
	assert.Contains(t, r.stdout, i18n.T(msgid.CliFocusCleared))
	assert.NotContains(t, r.stdout, i18n.T(msgid.CascadeReasonOutsideFocus))

	for _, args := range [][]string{
		{"up", "--dry-run", "--focus", "erp/api", "--all"},
		{"up", "--dry-run", "--focus", "erp/api", "-f", "deploy.yaml"},
		{"up", "--dry-run", "--focus", "erp/api", "--no-local"},
	} {
		r = runWithEngine(t, newFakeEngine(), dir, args...)
		assert.Equal(t, clierr.ExitUsage, r.code, "%v", args)
	}
}

// 本地文件里的个人修改与注释在写焦点后原样都在（Review Focus 2 的命令层一侧）。
func TestUpFocusKeepsPersonalEdits(t *testing.T) {
	dir := focusFixture(t)
	mustLocal(t, dir, "on")
	path := in(dir, "deploy.local.yaml")
	edited := readFile(t, path) + "# my own note\n"
	edited = strings.Replace(edited, "  - id: erp/portal\n", "  - id: erp/portal\n    localPort: 18090\n", 1)
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/portal")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	got := readFile(t, path)
	assert.Equal(t, edited, strings.Replace(got, "focus: erp/portal\n", "", 1))
}

// --focus 写了不存在的组件：报 COMPONENT_NOT_FOUND，而且什么都没写。
func TestUpFocusOnAnUnknownComponentWritesNothing(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/nope")
	require.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "COMPONENT_NOT_FOUND")
	assert.NoFileExists(t, in(dir, "deploy.local.yaml"))
}

// 焦点设着时，status / lint 也说一句焦点（与本地模式的提醒放在一起）。
func TestFocusIsAnnouncedByEveryCommandThatReadsIt(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/portal")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	for _, args := range [][]string{{"status"}, {"lint"}} {
		r = runWithEngine(t, newFakeEngine(), dir, args...)
		assert.Contains(t, r.stdout, i18n.T(msgid.CliFocusLine, "erp/portal"), "%v", args)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/cli -run 'TestUpFrom|TestUpInsideAWorkbench|TestUpFocus|TestFocusIsAnnounced' -count=1`
Expected: FAIL — unknown flag `--focus`; `up` in a component directory runs the whole project.

- [ ] **Step 3: Implement**

`focus.go`:

```go
package cli

// 本文件是焦点运行在命令层的部分（设计 §4）：从使用者所在的目录认出"我在哪个组件里"，
// 把焦点写进 deploy.local.yaml，并在每次读它的命令里说一句。

// componentAt 返回 dir 所在的本地组件：dir 在某个本地安装源的 <scope>/<name>/ 下面，且那里有 component.yaml。
func componentAt(l project.Layout, decl *projfile.File, dir string) (string, bool) {
	for _, s := range decl.Sources {
		if s.Type != projfile.SourceTypeLocal || s.Path == "" {
			continue
		}
		root := l.Resolve(s.Path)
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 {
			continue
		}
		id := parts[0] + "/" + parts[1]
		if fileExists(filepath.Join(root, parts[0], parts[1], manifest.FileName)) {
			return id, true
		}
	}
	return "", false
}

// ensureFocus 把焦点设成 id：本地模式没开就先打开（与 local on 同样复制或沿用），
// 文件里的焦点不同才改，改了就说一句。
func ensureFocus(opts *Options, l project.Layout, id string) error {
	if !fileExists(l.DeployLocalPath()) {
		if err := copyDeployToLocal(l); err != nil {
			return err
		}
		opts.Printf("%s\n", i18n.T(msgid.CliLocalOn, project.FileDeployLocal))
		opts.Printf("   %s\n", i18n.T(msgid.CliLocalCopied, project.FileDeployLocal, project.FileDeploy))
	}
	on, err := project.LocalModeOn(l)
	if err != nil {
		return err
	}
	if !on {
		if err := project.SetLocalMode(l, true); err != nil {
			return localSwitchError(l, err)
		}
		opts.Printf("%s\n", i18n.T(msgid.CliLocalOn, project.FileDeployLocal))
	}
	data, err := os.ReadFile(l.DeployLocalPath())
	if err != nil {
		return localIOError(l.DeployLocalPath(), err)
	}
	out, err := deployfile.SetFocus(data, id)
	if err != nil {
		return err
	}
	if !bytes.Equal(out, data) {
		if err := os.WriteFile(l.DeployLocalPath(), out, 0o644); err != nil {
			return localIOError(l.DeployLocalPath(), err)
		}
		opts.Printf("%s\n", i18n.T(msgid.CliFocusSet, id, project.FileDeployLocal))
	}
	return nil
}
```

`clearFocus` reads `deploy.local.yaml` if it exists, `SetFocus(data, "")`, writes when changed and prints
`CliFocusCleared`. `renderFocus` prints `i18n.T(msgid.CliFocusLine, id)` when `proj.Deploy.Focus != ""`.

`up.go` — flags `--focus string`, `--all bool` into `upOptions{focus string; all bool}`; at the start of
`runUp`, before `buildUpPlan`:

```go
	if flags.focus != "" && flags.all {
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliUpFocusAndAll)).WithExit(clierr.ExitUsage)
	}
	if opts.DeployFile == "" && !opts.NoLocal {
		if err := applyFocusIntent(opts, flags); err != nil {
			return err
		}
	}
```

```go
// applyFocusIntent 把这次 up 的焦点意图落进文件（设计 §4.3）：--all 清掉；--focus 设成它；
// 在组件目录里、没写这两个就设成这个组件；否则不动。-f 与 --no-local 时不碰个人文件。
func applyFocusIntent(opts *Options, flags upOptions) error {
	l := project.NewLayout(opts.WorkDir)
	if flags.all {
		return clearFocus(opts, l)
	}
	decl, err := projfile.ParseFile(l.DeclPath())
	if err != nil {
		return err
	}
	id := flags.focus
	if id == "" {
		var ok bool
		if id, ok = componentAt(l, decl, opts.CallDir); !ok {
			return nil
		}
	}
	if !slices.Contains(decl.IDs(), id) {
		return clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.ProjectFocusUnknown, id)).
			WithHint(i18n.T(msgid.ProjectHintFocusClear))
	}
	return ensureFocus(opts, l, id)
}
```

`--focus` with `-f` or `--no-local` is `INVALID_ARGUMENT` (`CliUpFocusNeedsLocal`): the focus lives in the
personal file those flags skip. Call `renderFocus(opts, proj)` right after every `renderDeploySource` call,
and in `status`, `lint`, `build`, `down` where they report which deploy file they read.

Messages (en / zh):
- `cli.focus_set`: `"🎯 Focus: %[1]s (written to %[2]s; brickkit up --all runs every component again)"` /
  `"🎯 焦点：%[1]s（已写进 %[2]s；brickkit up --all 恢复运行全部组件）"`
- `cli.focus_line`: `"🎯 Focus: %[1]s"` / `"🎯 焦点：%[1]s"`
- `cli.focus_cleared`: `"🎯 Focus cleared: every component runs"` / `"🎯 已清除焦点：运行全部组件"`
- `cli.up.focus_and_all`: `"Error: --focus and --all contradict each other"` / `"错误：--focus 与 --all 互相矛盾"`
- `cli.up.focus_needs_local`: `"Error: --focus is written into deploy.local.yaml, which -f and --no-local skip"` /
  `"错误：--focus 写在 deploy.local.yaml 里，而 -f 与 --no-local 都不读它"`
- flag help `cli.up.flag_focus`: `"Run only this component and what it needs, the component from source (written into deploy.local.yaml)"` /
  `"只运行这个组件和它需要的组件，这个组件从源码跑（写进 deploy.local.yaml）"`
- flag help `cli.up.flag_all`: `"Clear the focus and run every component"` / `"清除焦点，运行全部组件"`
- Add to `cli.up.example` (both languages, keep the column alignment of the existing lines):
  `  brickkit up --focus erp/api        run one component and what it needs` /
  `  brickkit up --focus erp/api        只运行这个组件和它需要的`

- [ ] **Step 4: Run tests and gates**

Run: `go test ./internal/cli -count=1`, then the Global Constraints gates.
Expected: PASS / exit 0. (`make lint`'s `check-cli-docs` will ask for `--focus` / `--all` in the CLI
reference — Task 15 writes it; it is not part of the per-task gates.)

- [ ] **Step 5: Commit**

```bash
git add internal/cli internal/i18n/locales internal/msgid
git commit -m "feat: up --focus / --all, and up from a component's directory focuses on it"
```

---

### Task 5: `sync`, `local refresh`, `lint` and `graph` with a focus

**Files:**
- Modify: `internal/cli/sync.go` (rename the internal `focus` type to `activeSet`; call `proj.IgnoreFocus()`)
- Test: `internal/cli/focus_commands_test.go`

**Interfaces:**
- Consumes: `Project.IgnoreFocus` (Task 3), `ensureFocus` (Task 4).

- [ ] **Step 1: Rename the existing internal name**

In `sync.go`, the type `focus`, `newFocus`, `syncFocus`, `focusFrom` and the parameter `f *focus` mean "which
source stays active". Rename them to `activeSet`, `newActiveSet`, `syncActiveSet`, `activeSetFrom` and
`a *activeSet` (and their comments), so the word "focus" means only the focus run. Run
`go test ./internal/cli -run Sync -count=1` — PASS (pure rename).

- [ ] **Step 2: Write the tests** (`internal/cli/focus_commands_test.go`, reusing `focusFixture` and `in`
  from Task 4)

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

func focusOnPortal(t *testing.T, dir string) {
	t.Helper()
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run", "--focus", "erp/portal")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// sync 不看焦点：焦点之外、项目本来要跑的组件，源码照样留在活跃目录（设计 §4.6）。
func TestSyncIgnoresTheFocus(t *testing.T) {
	dir := focusFixture(t)
	focusOnPortal(t, dir)
	r := runWithEngine(t, newFakeEngine(), dir, "sync")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.DirExists(t, in(dir, "components", "erp", "worker"))
	assert.NoDirExists(t, in(dir, "components", ".archived", "erp", "worker"))
}

// local refresh 把焦点当成一处本地修改列出来。
func TestLocalRefreshListsTheFocus(t *testing.T) {
	dir := focusFixture(t)
	focusOnPortal(t, dir)
	r := runIn(t, dir, "local", "refresh")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, i18n.T(msgid.CliLocalChangeUnset, deployfile.ScopeDeploy, "focus", "erp/portal"))
}

// lint 检查焦点：个人文件改成 k8s 时报焦点不能用。
func TestLintChecksTheFocus(t *testing.T) {
	dir := focusFixture(t)
	focusOnPortal(t, dir)
	editFile(t, in(dir, "deploy.local.yaml"), "target: docker", "target: k8s")
	r := runIn(t, dir, "lint")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.ConfigFocusK8sUnsupported))
}

// graph 读 deploy.yaml，不受本地模式与焦点影响：设焦点前后输出完全一样。
func TestGraphIgnoresTheFocus(t *testing.T) {
	dir := focusFixture(t)
	before := runIn(t, dir, "graph")
	require.Equal(t, clierr.ExitOK, before.code, before.stdout+before.stderr)
	focusOnPortal(t, dir)
	after := runIn(t, dir, "graph")
	require.Equal(t, clierr.ExitOK, after.code, after.stdout+after.stderr)
	assert.Equal(t, before.stdout, after.stdout)
}
```

- [ ] **Step 3: Run to verify**

Run: `go test ./internal/cli -run 'TestSyncIgnoresTheFocus|TestLocalRefreshListsTheFocus|TestLintChecksTheFocus|TestGraphIgnoresTheFocus' -count=1`
Expected: `TestSyncIgnoresTheFocus` FAILS (sync archives the sources outside the focus); the other three
PASS already (they pin behaviour that comes for free — say so in the commit message).

- [ ] **Step 4: Implement**

In `runSync`, right after `project.Load`: `proj.IgnoreFocus()` with a comment citing §4.6. If `lint` does
not report the focus's missing local source (the check lives in `checkLocalRepos`, which `up` calls), call the
same check from `lint` for the focused ref only, reusing `checkLocalRepos`.

- [ ] **Step 5: Tests, gates, commit**

```bash
git add internal/cli
git commit -m "feat: sync ignores the focus; refresh, lint and graph behave as designed with one"
```

---

### Task 6: `build` and `deps` default to the component you are in

**Files:**
- Modify: `internal/cli/build.go`, `internal/cli/deps.go`
- Test: `internal/cli/focus_defaults_test.go`

**Interfaces:**
- Consumes: `componentAt`, `Options.CallDir`.

- [ ] **Step 1: Tests** (`internal/cli/focus_defaults_test.go`, reusing `focusFixture` and `in`)

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 在组件目录里不带参数 deps：打印这个组件的依赖树，不是整个项目。
func TestDepsWithoutArgumentsInAComponentDirectory(t *testing.T) {
	dir := focusFixture(t)
	r := runIn(t, in(dir, "components", "erp", "portal"), "deps")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "erp/portal")
	assert.Contains(t, r.stdout, "erp/api")
	assert.NotContains(t, r.stdout, "erp/worker")
}

// 在组件目录里不带参数 build：只构建这个组件。
func TestBuildWithoutArgumentsInAComponentDirectory(t *testing.T) {
	dir := focusFixture(t)
	for _, rel := range [][]string{{"components", "erp", "portal"}, {"components", "erp", "worker"}} {
		editFile(t, in(dir, append(rel, "component.yaml")...), "deployment:\n",
			"deployment:\n  build: { context: ., dockerfile: Dockerfile }\n")
		writeTree(t, in(dir, rel...), map[string]string{"Dockerfile": "FROM scratch\n"})
	}
	imgs := newFakeImages()
	r := runWith(t, func(o *Options) { o.Images = imgs }, in(dir, "components", "erp", "portal"), "build")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	require.Len(t, imgs.built(), 1)
	assert.Contains(t, imgs.built()[0], "erp-portal")
}

// 在项目根不带参数：与今天相同（deps 整个项目）。
func TestDepsAtTheRootIsUnchanged(t *testing.T) {
	dir := focusFixture(t)
	r := runIn(t, dir, "deps")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "erp/worker")
}
```

- [ ] **Step 2: Run to verify they fail**, **Step 3: Implement** — in both `RunE`s, when no argument is
given, `if id, ok := componentAt(layout, decl, opts.CallDir); ok { arg = id }` (parse the declaration with
`projfile.ParseFile` if the command hasn't loaded the project yet). **Step 4: Tests and gates.**

- [ ] **Step 5: Commit**

```bash
git add internal/cli
git commit -m "feat: build and deps without arguments default to the component you are in"
```

---

### Task 7: One `components/` directory

**Files:**
- Create: `internal/project/nested.go`, `internal/project/nested_test.go`
- Modify: `internal/cli/up.go`, `internal/cli/lint.go`, `internal/cli/sync.go` (run the check)
- Modify: `internal/cli/add.go` (refuse `--repo` in a nested workbench)
- Modify: `internal/i18n/locales/{en,zh}.yaml`
- Test: `internal/cli/nested_test.go`

**Interfaces:**
- Produces:
  - `type NestedCopy struct { ID, Dir, Inside string; TopHasIt bool; Risk string }` — `Dir` the nested copy,
    `Inside` the component whose directory holds it, `Risk` = `workspace.DeletionRisk(Dir)` ("" when clean
    and pushed).
  - `func (p *Project) NestedCopies() ([]NestedCopy, error)` — for every local source root, every
    `<root>/<scope>/<name>/components/<s>/<n>/component.yaml`.
  - `func NestedCopiesError(copies []NestedCopy, display func(string) string) error` — `CONFIG_CONFLICT`.

- [ ] **Step 1: Tests** (`internal/cli/nested_test.go`, reusing `focusFixture`, `in`, and `gitOrgProject` from
  `add_git_test.go`)

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
)

// nestCopy 在 erp/portal 的目录里放一份 erp/api 的副本（带 component.yaml）。
func nestCopy(t *testing.T, dir string) string {
	t.Helper()
	nested := in(dir, "components", "erp", "portal", "components", "erp", "api")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "component.yaml"),
		[]byte(readFile(t, in(dir, "components", "erp", "api", "component.yaml"))), 0o644))
	return nested
}

// 组件目录里又有一份组件：找出来，说明上层有没有同一个 ID、这份在别处有没有副本。
func TestNestedCopiesAreFound(t *testing.T) {
	dir := focusFixture(t)
	nested := nestCopy(t, dir)
	p, err := project.Load(dir, project.LoadOptions{})
	require.NoError(t, err)
	copies, err := p.NestedCopies()
	require.NoError(t, err)
	require.Len(t, copies, 1)
	assert.Equal(t, "erp/api", copies[0].ID)
	assert.Equal(t, nested, copies[0].Dir)
	assert.Equal(t, "erp/portal", copies[0].Inside)
	assert.True(t, copies[0].TopHasIt)
	assert.Equal(t, i18n.T(msgid.WorkspaceRiskNotGitRepo), copies[0].Risk)
}

// submodule 留下的空目录不算副本。
func TestEmptySubmoduleDirectoryIsNotACopy(t *testing.T) {
	dir := focusFixture(t)
	require.NoError(t, os.MkdirAll(in(dir, "components", "erp", "portal", "components", "demo", "lib"), 0o755))
	p, err := project.Load(dir, project.LoadOptions{})
	require.NoError(t, err)
	copies, err := p.NestedCopies()
	require.NoError(t, err)
	assert.Empty(t, copies)
}

// up / lint / sync 遇到嵌套副本时报 CONFIG_CONFLICT，并且一个目录都不挪。
func TestCommandsRefuseANestedCopyAndMoveNothing(t *testing.T) {
	for _, args := range [][]string{{"up", "--dry-run"}, {"lint"}, {"sync"}} {
		dir := focusFixture(t)
		nested := nestCopy(t, dir)
		r := runWithEngine(t, newFakeEngine(), dir, args...)
		assert.Equal(t, clierr.ExitError, r.code, "%v", args)
		assert.Contains(t, r.stdout+r.stderr, "CONFIG_CONFLICT", "%v", args)
		assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.ProjectNestedCopies), "%v", args)
		assert.FileExists(t, filepath.Join(nested, "component.yaml"), "%v", args)
	}
}

// 嵌在项目本地源里的工作台里 add --repo：拒绝（会克隆出第二份），点名外面的项目。
func TestAddRepoInANestedWorkbenchIsRefused(t *testing.T) {
	dir := focusFixture(t)
	bench := in(dir, "components", "erp", "portal")
	writeTree(t, bench, map[string]string{
		"brickkit.yaml": "project: erp-portal\ncomponents: []\n",
		"deploy.yaml":   "target: docker\ncomponents: []\n",
	})
	r := runIn(t, bench, "add", "erp/api@1.0.0", "--repo")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "CONFIG_CONFLICT")
	assert.NoDirExists(t, in(bench, "components"))
}

// 从组件目录（没有工作台）add --repo：克隆进顶层 components/，组件目录里不出现 components/。
func TestAddRepoFromAComponentDirectoryClonesIntoTheTop(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	g.release(comp{ID: "erp/web", Version: "1.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/web@1.0.0", "--repo")
	g.mustRun(in(dir, "components", "erp", "web"), "add", "erp/api@1.0.0", "--repo")
	assert.DirExists(t, in(dir, "components", "erp", "api"))
	assert.NoDirExists(t, in(dir, "components", "erp", "web", "components"))
}
```

- [ ] **Step 2: Run to verify they fail**

- [ ] **Step 3: Implement**

`nested.go` walks each local source root two levels (the same shape `internal/source/local.go` scans:
skip names starting with `.`), then looks for `components/*/*/component.yaml` under each component directory.
Only a directory holding a `component.yaml` counts (§5, Review Focus of the spec). The error lists, per copy,
`Dir` (displayed), `Inside`, whether the top has the ID, and `Risk` when non-empty; hints: move it with
`git mv`/`mv` or delete it after checking, with the exact paths filled in, and that the CLI does neither.

In `add.go`, before planning clones, when `f.repo || f.repoAll`: `project.FindRoot(filepath.Dir(opts.WorkDir))`;
if an outer project exists and `componentAt(outerLayout, outerDecl, opts.WorkDir)` is true, refuse with
`CONFIG_CONFLICT` (`CliAddRepoInNestedWorkbench`, details: the outer project path; hints: run it from the
outer project, or use a focus run there instead of this workbench).

Messages (en / zh):
- `project.nested_copies`: `"Error: component source is nested inside another component's directory"` /
  `"错误：组件源码嵌在另一个组件的目录里"`
- `project.nested_copy_detail`: `"%[1]s (inside %[2]s)"` / `"%[1]s（在 %[2]s 里面）"`
- `project.nested_top_has_it`: `"the project's components/ has %[1]s too"` / `"项目的 components/ 里也有 %[1]s"`
- `project.nested_hint_one_place`: `"A component's source lives in one place, the project's components/; move or delete the nested copy yourself — BrickKit moves nothing"` /
  `"组件源码只放一处：项目的 components/。嵌套的那份请自己挪走或删掉——BrickKit 不替你挪"`
- `cli.add.repo_in_nested_workbench`: `"Error: this workbench sits inside project %[1]s; --repo would clone a second copy here"` /
  `"错误：这个工作台在项目 %[1]s 里面；--repo 会在这里再克隆一份"`
- `cli.add.hint_repo_from_outer`: `"Run brickkit add --repo from %[1]s, or work on this component with a focus run there (brickkit up in its directory)"` /
  `"在 %[1]s 里跑 brickkit add --repo；或者在那里用焦点运行开发这个组件（在它的目录里 brickkit up）"`

- [ ] **Step 4: `init` inside a project's component says a focus run needs no workbench** (§3 table)

Test (append to `internal/cli/nested_test.go`):

```go
// 在项目里的组件目录 init：照常补全出工作台，另外说一句——焦点运行不需要工作台，建了之后这个目录不再向上找。
func TestInitInsideAProjectsComponentAddsANote(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	r := runIn(t, in(dir, "components", "erp", "portal"), "init", "--yes", "--no-skills")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, i18n.T(msgid.CliInitInsideProjectNote, "erp/portal"))
}
```

Run it — FAIL. Implement in `init.go`'s completion mode: after the plan is applied, if
`project.FindRoot(filepath.Dir(opts.WorkDir))` finds an outer project and `componentAt` says this directory is one
of its local components, print `💡 ` + `CliInitInsideProjectNote`. Message (en / zh):
`"%[1]s is a component of an enclosing project: brickkit up in this directory runs it as a focus run with no workbench; now that this directory has a brickkit.yaml, commands here use this workbench instead"` /
`"%[1]s 是外面那个项目的组件：在这个目录里 brickkit up 就能以焦点运行它，不需要工作台；这里有了 brickkit.yaml 之后，这个目录里的命令改用这个工作台"`.
Run it — PASS.

- [ ] **Step 5: Tests, gates.** **Step 6: Commit**

```bash
git add internal/project internal/cli internal/i18n/locales internal/msgid
git commit -m "feat: one components/ directory — nested copies are reported, never moved"
```

---

### Task 8: Notes about submodules

**Files:**
- Create: `internal/gitrepo/gitmodules.go` (`SubmodulePathsIn`), test
- Modify: `internal/cli/build.go` (after `ExportSource`), `internal/cli/add.go` (`runClones`)
- Modify: `internal/i18n/locales/{en,zh}.yaml`
- Test: `internal/cli/submodule_test.go`

**Interfaces:**
- Produces: `func gitrepo.SubmodulePathsIn(dir string) []string` — the `path` values of `dir/.gitmodules`
  read with `git config -f <file> --get-regexp '^submodule\..*\.path$'` (works in a directory that is not a
  repository, like an exported tree); nil when there is no file or it can't be read.

- [ ] **Step 1: Tests** — real repositories, built the way the spec's experiment did.

`internal/gitrepo/gitmodules_test.go`:

```go
package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitIn 在 dir 里跑 git，不读使用者的全局配置；允许 file:// 的 submodule。
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "protocol.file.allow=always"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// 导出的源码树（不是 git 仓库）里也读得出 .gitmodules 登记的路径。
func TestSubmodulePathsInAnExportedTree(t *testing.T) {
	root := t.TempDir()
	lib, app, out := filepath.Join(root, "lib"), filepath.Join(root, "app"), filepath.Join(root, "out")
	for _, d := range []string{lib, app, out} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	gitIn(t, lib, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(lib, "lib.go"), []byte("package lib\n"), 0o644))
	gitIn(t, lib, "add", ".")
	gitIn(t, lib, "commit", "-qm", "lib")
	gitIn(t, app, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(app, "component.yaml"), []byte("x: 1\n"), 0o644))
	gitIn(t, app, "submodule", "add", "-q", lib, "components/demo/lib")
	gitIn(t, app, "add", ".")
	gitIn(t, app, "commit", "-qm", "app")
	archive := exec.Command("sh", "-c", "git archive --format=tar HEAD | tar -x -C "+out)
	archive.Dir = app
	b, err := archive.CombinedOutput()
	require.NoError(t, err, string(b))

	assert.Equal(t, []string{"components/demo/lib"}, SubmodulePathsIn(out))
	assert.Nil(t, SubmodulePathsIn(t.TempDir()))
}
```

`internal/cli/submodule_test.go` — a component repository whose `components/demo/lib` is a submodule, released
through `gitOrgProject` (its remotes live in `g.org`, named `<scope>-<name>`):

```go
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// releaseWithSubmodule 发布 demo/app@1.0.0：它的仓库把另一个仓库挂成 components/demo/lib 这个 submodule。
func releaseWithSubmodule(t *testing.T, g *gitOrgProject) {
	t.Helper()
	git := func(dir string, args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "protocol.file.allow=always"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	lib := filepath.Join(g.org, "lib-src")
	app := filepath.Join(g.org, "demo-app")
	for _, d := range []string{lib, app} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	git(lib, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(lib, "lib.go"), []byte("package lib\n"), 0o644))
	git(lib, "add", ".")
	git(lib, "commit", "-qm", "lib")
	git(app, "init", "-q")
	c := comp{ID: "demo/app", Version: "1.0.0", Image: "-"}
	require.NoError(t, os.WriteFile(filepath.Join(app, "component.yaml"), []byte(c.yamlText()), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(app, "Dockerfile"), []byte("FROM scratch\n"), 0o644))
	git(app, "submodule", "add", "-q", lib, "components/demo/lib")
	git(app, "add", ".")
	git(app, "commit", "-qm", "app")
	git(app, "tag", "1.0.0")
}

func TestAddRepoNotesUnfetchedSubmodules(t *testing.T) {
	g := newGitOrgProject(t)
	releaseWithSubmodule(t, g)
	dir := g.project()
	r := g.mustRun(dir, "add", "demo/app@1.0.0", "--repo")
	assert.Contains(t, r.stdout, i18n.T(msgid.CliAddSubmodulesNotFetched, "demo/app@1.0.0", "components/demo/lib"))
}

func TestBuildWarnsAboutEmptySubmoduleDirectories(t *testing.T) {
	g := newGitOrgProject(t)
	releaseWithSubmodule(t, g)
	dir := g.project()
	g.mustRun(dir, "add", "demo/app@1.0.0")
	r := g.mustRun(dir, "build", "demo/app", "--force")
	assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.CliBuildSubmodulesEmpty, "demo/app@1.0.0"))
	assert.Contains(t, r.stdout+r.stderr, "components/demo/lib")
}
```

- [ ] **Step 2: Run to verify they fail.** **Step 3: Implement** — `build`: after `client.ExportSource`,
  `if paths := gitrepo.SubmodulePathsIn(tmp); len(paths) > 0 { renderWarnings(opts, []*clierr.Error{clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliBuildSubmodulesEmpty, ref.String())).WithDetail(i18n.T(msgid.LabelDir), strings.Join(paths, ", ")).WithHint(i18n.T(msgid.CliBuildHintSubmodules))}) }`;
  `add --repo`: after each successful clone, the same lookup on the clone directory prints
  `ℹ️  ` + `CliAddSubmodulesNotFetched` with the paths.

Messages (en / zh):
- `cli.build.submodules_empty`: `"Warning: the source of %[1]s has git submodules, and they are empty directories here"` /
  `"警告：%[1]s 的源码里有 git submodule，这里它们是空目录"`
- `cli.build.hint_submodules`: `"BrickKit never fetches submodules; if the build needs them, the component should publish an image (deployment.image) or build without them"` /
  `"BrickKit 从不拉取 submodule；构建要是需要它们，这个组件应当发布现成的镜像（deployment.image），或者让构建不依赖它们"`
- `cli.add.submodules_not_fetched`: `"%[1]s has git submodules, not fetched: %[2]s (git submodule update --init fetches them if you need them)"` /
  `"%[1]s 里有 git submodule，没有拉取：%[2]s（确实需要时 git submodule update --init）"`

- [ ] **Step 4: Tests, gates.** **Step 5: Commit**

```bash
git add internal/gitrepo internal/cli internal/i18n/locales internal/msgid
git commit -m "feat: build and add --repo say when a component's git submodules were not fetched"
```

---

### Task 9: "Did you mean"

**Files:**
- Create: `internal/suggest/suggest.go`, `internal/suggest/suggest_test.go`
- Modify: `internal/yamlcheck/unknown.go` (use `suggest.EditDistance`; delete its private copy)
- Modify: the "not found" error sites for `remove`, `upgrade`, `deps`, `build`, `add`, and the focus
  (`applyFocusIntent`, `Project.checkFocus`)
- Modify: `internal/i18n/locales/{en,zh}.yaml`

**Interfaces:**
- Produces:
  - `func suggest.Similar(typed string, candidates []string) []string` — at most 3, in this order of
    preference: candidates containing `typed`; candidates whose name part (after `/`) equals `typed`'s name
    part; candidates within edit distance ≤ 2 of `typed`; ties sorted alphabetically; never `typed` itself.
  - `func suggest.EditDistance(a, b string) int` (moved from `yamlcheck`).
  - Message `hint.did_you_mean`: en `"Did you mean: %[1]s?"`, zh `"你是不是想写：%[1]s？"` (list joined with
    `msgid.ListSeparator`).

- [ ] **Step 1: Tests**

```go
func TestSimilar(t *testing.T) {
	ids := []string{"erp/api", "erp/auth", "crm/api", "infra/cache"}
	assert.Equal(t, []string{"crm/api", "erp/api"}, Similar("api", ids))
	assert.Equal(t, []string{"erp/api"}, Similar("erp/apj", ids))
	assert.Equal(t, []string{"infra/cache"}, Similar("cache", ids))
	assert.Empty(t, Similar("zzz", ids))
	assert.Empty(t, Similar("erp/api", ids), "完全一样的不算建议")
}
```

and the CLI side (`internal/cli/suggest_test.go`, reusing `focusFixture`):

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 组件 ID 写错：报错不变，多一条"你是不是想写"。一个都不猜着执行。
func TestMistypedIDsGetSuggestions(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"remove":  {[]string{"remove", "erp/apj"}, "erp/api"},
		"upgrade": {[]string{"upgrade", "erp/portl"}, "erp/portal"},
		"deps":    {[]string{"deps", "erp/workr"}, "erp/worker"},
		"focus":   {[]string{"up", "--dry-run", "--focus", "portal"}, "erp/portal"},
		"add":     {[]string{"add", "erp/workr"}, "erp/worker"},
	}
	for name, c := range cases {
		dir := focusFixture(t)
		r := runWithEngine(t, newFakeEngine(), dir, c.args...)
		require.NotEqual(t, clierr.ExitOK, r.code, name)
		assert.Contains(t, r.stderr, i18n.T(msgid.HintDidYouMean, c.want), name)
	}
}
```

- [ ] **Step 2–4:** fail, implement, pass — at each site, when the ID isn't found, build the candidate list
  (project IDs from `decl.IDs()`; for `add`, the local sources' IDs plus the manifest cache's
  `.brickkit/manifests/<scope>/<name>/`), and if `suggest.Similar` returns anything, append
  `.WithHint(i18n.T(msgid.HintDidYouMean, strings.Join(s, i18n.T(msgid.ListSeparator))))`. Code and title stay.

- [ ] **Step 5: Commit**

```bash
git add internal/suggest internal/yamlcheck internal/cli internal/project internal/i18n/locales internal/msgid
git commit -m "feat: a mistyped component ID gets 'did you mean' suggestions"
```

---

### Task 10: Completion candidates

**Files:**
- Create: `internal/cli/complete.go`, `internal/cli/complete_test.go`
- Modify: `remove`, `upgrade`, `deps`, `build`, `add` constructors (`ValidArgsFunction`); `up` (`--focus`),
  every command with `-f` (via `addDeployFileFlags`), `lang set`, `skills update` (`--lang`)
  (`RegisterFlagCompletionFunc`)
- Modify: `internal/source` — `func (c *Client) CachedVersions(id string) []string` (offline: local source
  version, git bare-cache tags via the existing `gitRepo.tags` when the cache directory exists, stripped of
  the `<scope>-<name>/` prefix for subpath components)

**Interfaces:**
- Produces:
  - `func completionProject(opts *Options) (*projfile.File, project.Layout, bool)` — finds the root from
    `opts.WorkDir` with `project.FindRoot`, parses `brickkit.yaml`; any error → `ok=false` (silent).
  - `func completeProjectComponents(opts *Options) cobra.CompletionFunc`, `completeAddCandidates`,
    `completeDeployFiles`, `completeLanguages` — all return `cobra.ShellCompDirectiveNoFileComp`.

- [ ] **Step 1: Tests** (`internal/cli/complete_test.go`) — drive cobra's hidden `__complete` command
  through `runIn`, which is exactly what the shell scripts call. It prints one candidate per line, then
  `:<directive>`; cobra writes `Completion ended with directive: …` to stderr, which is normal.

```go
package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
)

// candidates 取出 __complete 的候选（去掉最后那行 :<directive>）。
func candidates(t *testing.T, r result) []string {
	t.Helper()
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	lines := strings.Split(strings.TrimRight(r.stdout, "\n"), "\n")
	require.NotEmpty(t, lines)
	require.True(t, strings.HasPrefix(lines[len(lines)-1], ":"), r.stdout)
	return lines[:len(lines)-1]
}

func TestCompleteRemoveOffersProjectComponents(t *testing.T) {
	dir := focusFixture(t)
	got := candidates(t, runIn(t, dir, "__complete", "remove", ""))
	assert.Subset(t, got, []string{"erp/api", "erp/portal", "erp/worker", "erp/shell"})
}

// 从子目录也能补全，而且补全输出里没有"📁 项目"那一行。
func TestCompleteFromASubdirectory(t *testing.T) {
	dir := focusFixture(t)
	r := runIn(t, in(dir, "components", "erp", "portal"), "__complete", "remove", "")
	assert.Subset(t, candidates(t, r), []string{"erp/api"})
	assert.NotContains(t, r.stdout, "📁")
}

func TestCompleteAddOffersLocalSourcesAndVersionsAfterAt(t *testing.T) {
	dir := focusFixture(t)
	assert.Subset(t, candidates(t, runIn(t, dir, "__complete", "add", "")), []string{"erp/api", "erp/worker"})
	assert.Contains(t, candidates(t, runIn(t, dir, "__complete", "add", "erp/api@")), "erp/api@1.0.0")
}

func TestCompleteDeployFile(t *testing.T) {
	dir := focusFixture(t)
	writeTree(t, dir, map[string]string{"deploy.prod.yaml": "target: docker\ncomponents: []\n"})
	got := candidates(t, runIn(t, dir, "__complete", "up", "-f", ""))
	assert.Subset(t, got, []string{"deploy.yaml", "deploy.prod.yaml", "deploy.k8s.yaml"})
	assert.NotContains(t, got, "brickkit.yaml")
}

func TestCompleteLanguages(t *testing.T) {
	assert.ElementsMatch(t, i18n.LangNames(), candidates(t, runIn(t, t.TempDir(), "__complete", "lang", "set", "")))
}

// 项目外：没有候选，也没有任何报错块（Review Focus 5）。
func TestCompletionOutsideAProjectIsSilent(t *testing.T) {
	r := runIn(t, t.TempDir(), "__complete", "remove", "")
	assert.Empty(t, candidates(t, r))
	assert.NotContains(t, r.stdout+r.stderr, "❌")
}

// 项目文件写坏了：同样安静。
func TestCompletionInABrokenProjectIsSilent(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"brickkit.yaml": "components: [\n"})
	r := runIn(t, dir, "__complete", "remove", "")
	assert.Empty(t, candidates(t, r))
	assert.NotContains(t, r.stdout+r.stderr, "❌")
}

// 补全从不联网：git 源指向一个连不上的地址，补全照样很快返回，仓库缓存里什么都没建。
func TestCompletionNeverUsesTheNetwork(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml": "project: shop\nsources:\n  - name: org\n    type: git\n    baseUrl: http://127.0.0.1:1/\ncomponents: []\n",
		"deploy.yaml":   "target: docker\ncomponents: []\n",
	})
	cache := t.TempDir()
	r := runWith(t, func(o *Options) { o.RepoCacheDir = cache }, dir, "__complete", "add", "erp/api@")
	candidates(t, r)
	entries, err := os.ReadDir(cache)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
```

- [ ] **Step 2–4:** fail, implement, pass. Completion functions must not call `enterProject` (no
  "📁 Project" line in completion output) and must not load catalogs of other languages or the source
  client's network paths — `CachedVersions` only reads directories that already exist.

- [ ] **Step 5: Commit**

```bash
git add internal/cli internal/source
git commit -m "feat: TAB completes component IDs, versions, deploy files and languages — offline"
```

---

### Task 11: `install.sh` installs the completion

**Files:**
- Modify: `install.sh` (step 7), `scripts/check-install-sh.sh`

- [ ] **Step 1: Extend the check first** — in `check-install-sh.sh`, make `run_install` pass
  `HOME="$tmp/home"` and `XDG_DATA_HOME`, `XDG_CONFIG_HOME` unset, and `PATH` containing fake `bash`, `zsh`,
  `fish` shims only where a case needs them (create them in `$tmp/shells`). New cases:
  1. with all three shells present: the three files exist under `$tmp/home`
     (`.local/share/bash-completion/completions/brickkit`, zsh fallback `.zsh/completions/_brickkit`,
     `.config/fish/completions/brickkit.fish`), each non-empty and starting with the header cobra writes for
     that shell; output names each path; output contains the two `~/.zshrc` lines; `$tmp/home/.zshrc` does
     not exist afterwards.
  2. `BRICKKIT_NO_COMPLETION=1`: none of the files exist; output says it was skipped.
  3. only fish present: only the fish file.
  Run `make check-install-sh` — the new cases FAIL.

- [ ] **Step 2: Implement** — after step 6 in `install.sh`:

```sh
# ---- 7. Shell completion ----
#
# Completion is the shell's feature: it loads a function registered for
# `brickkit` from a known directory. A program can't change a shell that is
# already running, so the best an installer can do is put the file where the
# shell looks — what Homebrew and apt do. Never edit rc files: they are the
# user's own.
bk="${install_dir}/brickkit"
if [ -n "${BRICKKIT_NO_COMPLETION:-}" ]; then
	info ""
	info "Shell completion skipped (BRICKKIT_NO_COMPLETION is set)"
else
	info ""
	info "Shell completion:"
	if command -v bash >/dev/null 2>&1; then
		dir="${XDG_DATA_HOME:-${HOME}/.local/share}/bash-completion/completions"
		mkdir -p "$dir" && "$bk" completion bash >"${dir}/brickkit" 2>/dev/null &&
			info "   bash  ${dir}/brickkit (needs the bash-completion package)"
	fi
	if command -v zsh >/dev/null 2>&1; then
		zdir=""
		for d in "$(brew --prefix 2>/dev/null)/share/zsh/site-functions" /usr/local/share/zsh/site-functions; do
			case "$d" in /share/*) continue ;; esac
			if [ -d "$d" ] && [ -w "$d" ]; then zdir="$d"; break; fi
		done
		if [ -n "$zdir" ]; then
			"$bk" completion zsh >"${zdir}/_brickkit" 2>/dev/null && info "   zsh   ${zdir}/_brickkit"
		else
			zdir="${HOME}/.zsh/completions"
			mkdir -p "$zdir" && "$bk" completion zsh >"${zdir}/_brickkit" 2>/dev/null &&
				info "   zsh   ${zdir}/_brickkit — add these two lines to ~/.zshrc:" &&
				info "           fpath=(~/.zsh/completions \$fpath)" &&
				info "           autoload -Uz compinit && compinit"
		fi
	fi
	if command -v fish >/dev/null 2>&1; then
		dir="${XDG_CONFIG_HOME:-${HOME}/.config}/fish/completions"
		mkdir -p "$dir" && "$bk" completion fish >"${dir}/brickkit.fish" 2>/dev/null &&
			info "   fish  ${dir}/brickkit.fish"
	fi
	info "   PowerShell: add  brickkit completion powershell | Out-String | Invoke-Expression  to \$PROFILE"
	info "   Open a new terminal (or run: exec \$SHELL) for completion to take effect."
fi
```

Update the header comment's environment variable list with `BRICKKIT_NO_COMPLETION`. `shellcheck --shell=sh`
must pass (the check runs it when installed).

- [ ] **Step 3: Run** `make check-install-sh` — PASS. **Step 4: Commit**

```bash
git add install.sh scripts/check-install-sh.sh
git commit -m "feat: install.sh installs shell completion where each shell loads it, never touching rc files"
```

---

### Task 12: Renumber the documentation for the two new pages

**Files:**
- Create (scratchpad, not committed): `renumber-focus.py`
- Rename (both languages): the 8 pages of `02-project-guide/` and 3 pages of `00-intro/` in spec §8.1
- Modify: every file linking to them

- [ ] **Step 1: Write the renumbering script** (in the session scratchpad) with the map of §8.1 for `en` and
  `zh`, and these parts, each asserting it hit:
  1. **No chained renames:** assert no new path equals an old path.
  2. **Markdown links:** for every tracked `*.md` outside `docs/superpowers/` and `archive/`, and for
     `llms.txt`, `llms.zh.txt` and `internal/skills/assets/**`, parse each `[text](target)` whose target is a
     relative path; resolve it against the linking file's **old** location; if it points at a renamed file,
     recompute it relative to the linking file's **new** location (the linking file may itself be renamed);
     keep the `#anchor`. If the link text shows the old file name, replace it with the new name.
  3. **Absolute strings:** replace `docs/en/02-project-guide/05-up-and-down.md`-style full paths (and the
     `raw.githubusercontent.com/.../docs/...` URLs in `AGENTS*.md`, `llms*.txt`) by whole-string substitution.
  4. `git mv` each file.
  5. Index edits, asserted one by one: `02-project-guide/README.md` page table (rows 04–11 → 05–12 with their
     numbers in the link text `[04 Several environments]` → `[05 …]`), `00-intro/README.md` table, and the
     numbered entries in `llms.txt` / `llms.zh.txt`.

- [ ] **Step 2: Run it**, then:

Run: `git grep -n -E "0[4-9]-multi-env|05-up-and-down|06-upgrade-and|07-remove-and|08-status-and|09-lint-and|10-sync-and|11-build-and|03-core-concepts|04-comparison|05-fractal" -- . ':!docs/superpowers' ':!archive'`
Expected: only the new names (`05-multi-env-switch`, `06-up-and-down`, …, `04-core-concepts`,
`05-comparison`, `06-fractal-architecture`) appear.

Run: `git status --short | grep -c '^R'` — Expected: `22`.

- [ ] **Step 3: Search sentences that state an order** (`git grep -n -E "last page|最后一篇|next page|下一篇|eleven|十一"` in
  `docs/en docs/zh`) and fix any that the renumbering made wrong.

- [ ] **Step 4: Full lint**

Run: `systemd-run --user --scope -q -p MemoryMax=8G -p MemorySwapMax=0 make -k lint; echo $?`
Expected: `0` except `check-cli-docs` naming `--focus` / `--all` as undocumented (Task 15 fixes it). Record
that in the commit message. Any other failure is a renumbering mistake — fix it before committing.

- [ ] **Step 5: Commit**

```bash
git add -A docs README.md README.zh.md AGENTS.md AGENTS.zh.md llms.txt llms.zh.txt internal/skills/assets
git commit -m "docs: renumber project-guide 04–11 and intro 03–05 to make room for two new pages"
```

---

### Task 13: New page — "Developing inside the project" (`02-project-guide/04-focus-run.md`)

**Files:**
- Create: `docs/en/02-project-guide/04-focus-run.md`, `docs/zh/02-project-guide/04-focus-run.md`
- Modify: `docs/{en,zh}/02-project-guide/README.md` (row 04), `llms.txt`, `llms.zh.txt` (entry in order),
  `docs/{en,zh}/01-three-layers/09-field-reference.md` and `11-reference/03-deploy-yaml-schema.md` (retarget
  the `focus` row's link to this page)

- [ ] **Step 1: Run it for real.** Build the CLI; in the scratchpad, create the demo project the project-guide
  uses (`my-shop` with `demo/hello`, `demo/caller`, `demo/bus` — copy `tests/components/` fixtures as the other
  pages' runs do) with all three as local sources under `components/`; run, capturing every output with
  `BRICKKIT_LANG=en` and `BRICKKIT_LANG=zh`:
  `cd components/demo/caller && brickkit up`; `brickkit status`; `brickkit up --focus demo/hello` from the
  root; `brickkit sync`; `brickkit local refresh`; `brickkit up --all`; a nested copy created by hand and
  `brickkit lint`; `brickkit up --focus demo/bus` with `target: k8s` in `deploy.local.yaml`.

- [ ] **Step 2: Write the page (en), then independently (zh)** covering, in this order: who it is for (a
  large project where components are edited in place); focus run vs workbench — which to use when (a table);
  `up` from a component's directory, with its real output (the `📁 Project`, `🎯 Focus`, local-mode lines and
  the per-component reasons); how the focus sits in `deploy.local.yaml` (the real file excerpt); `--focus`
  and `--all`; what `status`, `down`, `sync`, `local refresh`, `graph` do with it; moving versions forward
  together (the local-repository version rule and the `upgrade` hint it prints); the one-`components/` rule
  and the nested-copy error (real output); submodules are never fetched. Link to `03-local-debug-workflow.md`
  (breakpoints with `mode: debug`), `../03-component-guide/05-local-dev-fractal.md` (workbenches) and
  `../07-cli-reference/README.md`.

- [ ] **Step 3: Index rows** — the project-guide README table gets
  `| [04 Developing inside the project](04-focus-run.md) | up from a component's directory, --focus / --all, what starts, one components/ |`
  (zh written in Chinese), and its "life of a project" sentence gains "work on components in place" after
  "debug locally". Add the `llms*.txt` entries in number order.

- [ ] **Step 4: Lint** — `make -k lint` (only the `--focus` / `--all` CLI-reference gap may remain).
  `make check-doc-fields` checks every output line on the page exists in the catalogs.

- [ ] **Step 5: Commit**

```bash
git add docs llms.txt llms.zh.txt
git commit -m "docs: developing inside the project — focus runs, from real runs"
```

---

### Task 14: New page — "Shell completion" (`00-intro/03-shell-completion.md`)

**Files:**
- Create: `docs/en/00-intro/03-shell-completion.md`, `docs/zh/00-intro/03-shell-completion.md`
- Modify: `docs/{en,zh}/00-intro/README.md`, `docs/{en,zh}/00-intro/02-quick-start.md`, `README.md`,
  `README.zh.md` (install section), `llms.txt`, `llms.zh.txt`

- [ ] **Step 1: Run it for real** — `install.sh` against a `file://` release into a scratch `$HOME` (the way
  `check-install-sh.sh` does) to capture its completion output; `brickkit __complete remove ""` in the demo
  project; for each of bash, zsh, fish present on this machine, source the generated script in a
  non-interactive test (`bash -c 'source <(brickkit completion bash); complete -p brickkit'`) and record what
  it prints.

- [ ] **Step 2: Write the page (en), then independently (zh)**, for readers who have never configured
  completion: what TAB completion does, in one paragraph; why the shell needs a file (the shell, not the
  program, decides the candidates; a program can't change a running shell); what `install.sh` already did and
  how to check (`ls` of the three paths, a new terminal, `brickkit rem<TAB>`); per shell — bash (the
  bash-completion package; how to install it on Debian/Ubuntu, Fedora, macOS), zsh (the `~/.zshrc` lines when
  the file landed in `~/.zsh/completions`; oh-my-zsh users usually need nothing), fish, PowerShell (the
  `$PROFILE` line); installs without `install.sh` (downloaded binary, `go install`): the one command per shell
  that writes the file; what gets completed (the §7.1 table) and that it works offline; "it doesn't complete"
  — a new terminal / `exec $SHELL`, `compinit` not run, bash-completion missing, the directory not on
  `$fpath`, an old file from a previous install. Link back to the quick start and the CLI reference.

- [ ] **Step 3: Pointers** — the quick start gains one line after installing ("TAB completes commands and
  component IDs — see [Shell completion](03-shell-completion.md)"); both READMEs' install sections gain the
  same pointer; the intro README table gains row 03; `llms*.txt` entries.

- [ ] **Step 4: Lint**, **Step 5: Commit**

```bash
git add docs README.md README.zh.md llms.txt llms.zh.txt
git commit -m "docs: shell completion — what install.sh did, and every shell by hand"
```

---

### Task 15: The pages that change

**Files (both languages each):** every row of spec §8.2 except `README*`, `00-intro/02-quick-start.md`,
`AGENTS*`, `llms*` and the skill assets (Tasks 14 and 16):
`01-three-layers/04-deploy-local-yaml.md`, `02-project-guide/02-add-and-component-install.md`,
`02-project-guide/03-local-debug-workflow.md`, `02-project-guide/11-sync-and-restore.md`,
`02-project-guide/12-build-and-images.md`, `03-component-guide/05-local-dev-fractal.md`,
`06-architecture/06-bare-repo-mechanism.md`, `06-architecture/09-error-codes.md`,
`07-cli-reference/README.md`, `08-ai-guide/02-ai-dev-workflow.md`,
`10-troubleshooting/02-local-debug-issues.md`, `10-troubleshooting/05-build-issues.md`.

- [ ] **Step 1: Capture real outputs** for every new message the pages quote (the demo project of Task 13 and
  the submodule repositories of Task 8), in both languages.

- [ ] **Step 2: Edit each page** with the content its §8.2 row names. The CLI reference gets:
  a "Running from a subdirectory" section with the §3 table; `up`'s `--focus` and `--all` rows and an example;
  `build` / `deps` defaults in a component directory; a `completion` section with the §7.1 table and a link to
  `../00-intro/03-shell-completion.md`. The error-codes page adds the new titles under `COMPONENT_NOT_FOUND`
  (focus unknown), `COMPONENT_DISABLED` (focus disabled), `CONFIG_INVALID` (focus in a team file, on k8s,
  invalid ID), `CONFIG_CONFLICT` (nested copies, `add --repo` in a nested workbench), `INVALID_ARGUMENT`
  (`--focus` with `--all`, with `-f` / `--no-local`), and the warnings (submodules) — titles exactly as in the
  catalogs (the doc-field guard checks them against source).

- [ ] **Step 3: Full lint** — `make -k lint; echo $?` → `0` (the CLI-reference gap is now closed).

- [ ] **Step 4: Commit**

```bash
git add docs
git commit -m "docs: focus runs, subdirectories, one components/, submodules and completion across the guides"
```

---

### Task 16: AGENTS, llms and the skill assets

**Files:**
- Modify: `AGENTS.md`, `AGENTS.zh.md` (§3.2, §3.3, §5), `llms.txt`, `llms.zh.txt` (summaries of the changed
  pages where they describe behaviour)
- Modify: `internal/skills/assets/{en,zh}/AGENTS.md`, `…/brickkit-assemble/SKILL.md`,
  `…/brickkit-deploy/SKILL.md`, `…/brickkit-troubleshoot/SKILL.md`

- [ ] **Step 1: Edit** — AGENTS §3.2: focus is personal-only; project commands find the project from a
  subdirectory; §3.3: a workbench for a standalone component, a focus run for a component inside a project;
  one `components/`; §5 flags: `--focus`, `--all`, running from a subdirectory. Skills: `assemble` gets a
  "focus run" point and the one-`components/` rule; `deploy` gets the `focus:` field (personal-only, what
  starts, `sync` ignores it); `troubleshoot` gets rows for the focus errors, the nested-copy error, the `add
  --repo` refusal and the submodule warning (their error codes are already covered by the troubleshoot guard;
  the rows name the new titles).

- [ ] **Step 2: Guards** — `go test ./internal/skills -count=1` (troubleshoot covers every code, no invented
  code), `make -k lint` (`check-cli-docs` scans the skill assets and AGENTS for every command and flag).

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md AGENTS.zh.md llms.txt llms.zh.txt internal/skills/assets
git commit -m "docs: AGENTS, llms and the AI skills describe focus runs, one components/ and completion"
```

---

### Task 17: Checklist rows, full gates, review

**Files:**
- Modify: `tests/checklist/清单.tsv`

- [ ] **Step 1: Checklist rows** (5-column format `id<TAB>description<TAB>module<TAB>package<TAB>test`),
  continuing after `boundary.25` and `error.20`:

```
boundary.26	在组件目录里 up 只启动这个组件和它需要的，焦点写进 deploy.local.yaml	.	./internal/cli	TestUpFromAComponentDirectoryFocusesIt
boundary.27	焦点下与它无关的弱依赖环不启动	.	./internal/cascade	TestFocusDoesNotStartAnUnrelatedCycle
boundary.28	组件目录里有自己的工作台时，up 跑工作台，不动外面的项目	.	./internal/cli	TestUpInsideAWorkbenchRunsTheWorkbench
boundary.29	补全在项目外、项目文件写坏时都不输出任何东西	.	./internal/cli	TestCompletionOutsideAProjectIsSilent
boundary.30	补全从不联网	.	./internal/cli	TestCompletionNeverUsesTheNetwork
error.21	焦点写在团队部署文件里被拒绝	.	./internal/deployfile	TestFocusValidation
error.22	组件源码嵌在另一个组件目录里时报错，且一个目录都不挪	.	./internal/cli	TestCommandsRefuseANestedCopyAndMoveNothing
error.23	组件 ID 写错时给出相近的候选	.	./internal/suggest	TestSimilar
```

- [ ] **Step 2: Full gates**

Run: `systemd-run --user --scope -q -p MemoryMax=8G -p MemorySwapMax=0 make -k lint; echo $?` → `0`
Run: `systemd-run --user --scope -q -p MemoryMax=10G -p MemorySwapMax=0 make -k test-all; echo $?` → `0`
(regression list all passing).

- [ ] **Step 3: Commit**

```bash
git add tests/checklist
git commit -m "test: checklist rows for focus runs, nested copies, completion and suggestions"
```

- [ ] **Step 4: Whole-plan review** — one fresh reviewer on the most capable model over the plan's commit range,
  with this plan's Review Focus verbatim; one fix pass, each fix with a failing test first; rulings and deferred
  minors reported. Remind the user that projects with the skills installed will see them as out of date
  (`brickkit skills update`).
