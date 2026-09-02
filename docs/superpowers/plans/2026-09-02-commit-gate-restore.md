# 提交前结构自洽闸门与 brickkit restore 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让「组件源码提交在归档目录里、而 brickkit.yaml 说它该启动」这个失误在 `git commit` 时被拦住，并提供 `brickkit restore` 一条命令把 yaml 的 `enabled` 与源码结构还原到最后一次提交。

**Architecture:** 新增只读的 `internal/gitrepo` 包封装全部 git 查询；判据做成不碰 git 也不碰磁盘的纯函数（`layoutFromIndex` + `judgeCommit`），由 `brickkit restore --check` 接线；`brickkit restore` 在内存里把 `enabled` 还原到 HEAD 值、先算判定再落盘，然后**原样复用** `sync` 的 `planSync` / `applySync` 移动目录；`pre-commit` hook 由 `brickkit init --hooks` 写入，只调 `restore --check`。

**Tech Stack:** Go 1.x、cobra、gopkg.in/yaml.v3（节点级编辑）、testify、POSIX sh（hook 脚本）

**Spec:** `docs/superpowers/specs/2026-09-02-commit-gate-restore-design.md`

## Global Constraints

- **判据读 index，不读 HEAD。** 即将提交的 yaml = `git show :<rel>`；即将提交的结构 = `git ls-files --cached --stage`。读 HEAD 会造出死锁：yaml 的改动永远比结构晚一拍。
- **`components/` 的 index 记录只查一次**，一份结果同时供判定、短路、gitlink 提醒三处用。
- **只拦一个方向**：判定说该跑、而源码提交在 `.archived/` 下。反方向（源码活跃、yaml 说不跑）一律放行——那只是「没跑过 sync」。
- **另加一条双向、优先的「两处都有」判据**，只管 yaml 里声明过的组件。
- **`yaml` 里没声明的组件一律不管**（与 `planSync` 同一个边界）。
- **判据按组件 ID 归集**，不按 `(id, version)` 条目——同 ID 多版本时按条目判会自相矛盾。
- **四种情形放行 + 警告，绝不拦**：正在冲突中、配置未被 git 跟踪、全图解析失败、找不到 `brickkit` 可执行文件。
- **`restore` 的执行顺序是硬约束**：解析工作区 yaml → 内存里还原 `enabled` → 算判定 → **判定成功才落盘 yaml** → 移动目录。且 `restore` 必须幂等。
- **`restore` 绝不 `git checkout` 整个配置文件**，只逐条改 `enabled` 字段；工作区新增的条目一个字不动。
- **hooks 目录一律用 `git rev-parse --git-path hooks`**，绝不手拼 `.git/hooks`。
- **hook 脚本必须是 `#!/bin/sh` 且不用任何 bash 特性**（Windows 上 Git for Windows 用自带 sh 跑 hook）。
- **`init` 只在「项目根 == 仓库根」时自动装 hook**，其余情况只提示 `brickkit init --hooks`。
- 用户可见文字一律中文；错误一律走 `clierr`（`WithDetail` / `WithHint`）。

---

## File Structure

| 文件 | 责任 |
| --- | --- |
| `internal/gitrepo/gitrepo.go`（新） | git 只读查询的唯一入口：定位仓库、相对路径、index / HEAD 内容、index 记录、已暂存判断、hooks 目录 |
| `internal/gitrepo/gitrepo_test.go`（新） | 用真 git 仓库验上面每一条 |
| `internal/config/edit.go`（改） | 加 `SetComponentEnabled` / `ClearComponentEnabled` |
| `internal/workspace/workspace.go`（改） | 加 `InBothPlaces`（`Locate` 活跃优先，答不出这一种） |
| `internal/cli/restore_check.go`（新） | 纯判据（`layoutFromIndex` / `judgeCommit` / `under`）+ `runRestoreCheck` 接线与输出 |
| `internal/cli/restore.go`（新） | `newRestoreCommand`、`restorePlan`、`runRestore` |
| `internal/cli/hooks.go`（新） | hook 脚本模板、标记行识别、项目列表幂等合并、安装 |
| `internal/cli/sync.go`（改） | 抽出 `declaredIDs` 与 `applyWorkspacePlan` 供 `restore` 复用（行为不变） |
| `internal/cli/init.go`（改） | `--hooks` 旗标；`init` 尾部按「项目根 == 仓库根」决定装还是提示 |
| `internal/cli/root.go`（改） | 注册 `restore` |
| `internal/cli/restore_check_test.go`（新） | 判据 3 × 4 状态表逐格 + 短路 / 放行分支 |
| `internal/cli/restore_test.go`（新） | `restorePlan` 每条规则 + 前置条件 + 顺序 + 幂等 |
| `internal/cli/hooks_test.go`（新） | 安装、不覆盖别人的 hook、多项目幂等、找不到二进制 |

---

### Task 1: `internal/gitrepo` —— git 查询的唯一入口

**Files:**
- Create: `internal/gitrepo/gitrepo.go`
- Test: `internal/gitrepo/gitrepo_test.go`

**Interfaces:**
- Consumes: 无（本任务是最底层）
- Produces:
  - `func Open(dir string) (*Repo, error)`，失败返回 `ErrNotRepo`
  - `func (r *Repo) Root() string`
  - `func (r *Repo) Rel(path string) (string, bool)`
  - `func (r *Repo) HasHEAD() bool`
  - `func (r *Repo) Unmerged() bool`
  - `func (r *Repo) Tracked(rel string) bool`
  - `func (r *Repo) IndexBlob(rel string) ([]byte, error)`
  - `func (r *Repo) HeadBlob(rel string) ([]byte, error)`
  - `func (r *Repo) IndexEntries(relPrefix string) ([]IndexEntry, error)`
  - `func (r *Repo) StagedUnder(relPrefix string) bool`
  - `func (r *Repo) HooksDir() (string, error)`
  - `type IndexEntry struct { Mode string; Path string }` 与 `func (e IndexEntry) IsGitlink() bool`

- [ ] **Step 1: 写测试夹具与失败的测试**

新建 `internal/gitrepo/gitrepo_test.go`：

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

// newRepo 建一个真的 git 仓库（不含提交）。
//
// 用真仓库而不是打桩：这个包全部行为都是 git 的行为，桩只会证明桩自己对。
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "--quiet")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "t")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v：%s", args, out)
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestOpenFindsRootFromSubdir(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "components/people/basic/main.go", "package main\n")

	r, err := Open(filepath.Join(dir, "components", "people"))
	require.NoError(t, err)

	rel, ok := r.Rel(filepath.Join(dir, "components", "people", "basic"))
	assert.True(t, ok)
	assert.Equal(t, "components/people/basic", rel, "必须是 / 分隔的相对路径")
}

func TestOpenRejectsNonRepo(t *testing.T) {
	_, err := Open(t.TempDir())
	assert.ErrorIs(t, err, ErrNotRepo)
}

func TestRelRejectsPathOutsideRepo(t *testing.T) {
	r, err := Open(newRepo(t))
	require.NoError(t, err)

	_, ok := r.Rel(filepath.Join(t.TempDir(), "brickkit.yaml"))
	assert.False(t, ok, "仓库外的路径必须报 false，而不是一串 ../..")
}

func TestHasHEADAndUnmerged(t *testing.T) {
	dir := newRepo(t)
	r, err := Open(dir)
	require.NoError(t, err)
	assert.False(t, r.HasHEAD(), "空仓库没有 HEAD")
	assert.False(t, r.Unmerged())

	write(t, dir, "a.txt", "a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "init")
	assert.True(t, r.HasHEAD())
}

func TestIndexBlobReadsStagedVersionNotHead(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "brickkit.yaml", "project: old\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "init")

	write(t, dir, "brickkit.yaml", "project: staged\n")
	git(t, dir, "add", "brickkit.yaml")
	write(t, dir, "brickkit.yaml", "project: worktree\n")

	r, err := Open(dir)
	require.NoError(t, err)

	staged, err := r.IndexBlob("brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, "project: staged\n", string(staged), "index 版 = 即将提交的那一份")

	head, err := r.HeadBlob("brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, "project: old\n", string(head))
}

func TestIndexEntriesSeesGitlinkWithoutTrailingSlash(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "components/people/basic/main.go", "package main\n")

	nested := filepath.Join(dir, "components", "erp", "backend")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	git(t, nested, "init", "--quiet")
	git(t, nested, "config", "user.email", "t@example.com")
	git(t, nested, "config", "user.name", "t")
	write(t, nested, "m.go", "package main\n")
	git(t, nested, "add", "-A")
	git(t, nested, "commit", "--quiet", "-m", "c1")

	git(t, dir, "add", "components")

	r, err := Open(dir)
	require.NoError(t, err)
	entries, err := r.IndexEntries("components")
	require.NoError(t, err)

	var gitlinks, files []string
	for _, e := range entries {
		if e.IsGitlink() {
			gitlinks = append(gitlinks, e.Path)
			continue
		}
		files = append(files, e.Path)
	}
	assert.Equal(t, []string{"components/erp/backend"}, gitlinks,
		"嵌套仓库是一条 160000 记录，路径没有尾斜杠")
	assert.Equal(t, []string{"components/people/basic/main.go"}, files)
}

func TestStagedUnderSeesStagedChangeOnly(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "components/people/basic/main.go", "package main\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "init")

	r, err := Open(dir)
	require.NoError(t, err)
	assert.False(t, r.StagedUnder("components"), "干净时不该有已暂存改动")

	write(t, dir, "components/people/basic/main.go", "package main // 改了\n")
	assert.False(t, r.StagedUnder("components"), "只改了工作区、没 add，不算已暂存")

	git(t, dir, "add", "components")
	assert.True(t, r.StagedUnder("components"))
}

func TestHooksDirFollowsCoreHooksPath(t *testing.T) {
	dir := newRepo(t)
	r, err := Open(dir)
	require.NoError(t, err)

	got, err := r.HooksDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(r.Root(), ".git", "hooks"), got)

	git(t, dir, "config", "core.hooksPath", ".githooks")
	got, err = r.HooksDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(r.Root(), ".githooks"), got,
		"husky / lefthook 会设 core.hooksPath，装错地方等于 hook 永不运行")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gitrepo/ -run . -v`
Expected: FAIL，编译不过（`undefined: Open`、`undefined: ErrNotRepo` 等）

- [ ] **Step 3: 写实现**

新建 `internal/gitrepo/gitrepo.go`：

```go
// Package gitrepo 是 BrickKit 对 git 仓库做只读查询的唯一入口。
//
// # 为什么要收成一个包
//
// 提交前的判据要读 index、restore 要读 HEAD、hook 安装要找 hooks 目录——三处都得
// 处理同样几件麻烦事：worktree（.git 是文件）、submodule、core.hooksPath、
// 路径分隔符（git 只认 /）、以及"别读使用者的全局配置"。分散在三处写，
// 迟早有一处写错，而写错的那一处不会报错，只会静默地判错或装错地方。
package gitrepo

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotRepo 表示给定目录不在任何 git 仓库里。
var ErrNotRepo = errors.New("不在 git 仓库里")

// Repo 是一个已定位的 git 仓库。
type Repo struct{ root string }

// Open 定位 dir 所在的 git 仓库。
func Open(dir string) (*Repo, error) {
	out, err := query(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNotRepo
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return nil, ErrNotRepo
	}
	// macOS 上 /tmp 是符号链接，git 报的是物理路径而 t.TempDir() 给的是链接路径；
	// 两边不统一，Rel 会算出一串 ../..，判据就会把整个仓库当成"在仓库外"。
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return &Repo{root: root}, nil
}

// Root 返回仓库根目录（绝对路径）。
func (r *Repo) Root() string { return r.root }

// Rel 把路径转成"相对仓库根、以 / 分隔"的形式。路径在仓库之外时 ok 为 false。
//
// git 只认 /，Windows 上必须转；而 .. 开头意味着它根本不在这个仓库里——
// 那种情况要让调用方看得出来，不能交给 git 去报一句莫名的错。
func (r *Repo) Rel(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	// 目标文件可能还不存在（配置文件路径），所以解析它的父目录而不是它自己。
	if dir, base := filepath.Split(abs); dir != "" {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			abs = filepath.Join(resolved, base)
		}
	}
	rel, err := filepath.Rel(r.root, abs)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// HasHEAD 报告仓库有没有任何提交。没有提交就没有"最后一次提交"这个基准。
func (r *Repo) HasHEAD() bool {
	_, err := query(r.root, "rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}

// Unmerged 报告是不是正处在冲突中（index 里有未合并条目）。
//
// 冲突中 `git show :<path>` 会直接 fatal（不存在 stage 0），所以判据必须先问这一句。
func (r *Repo) Unmerged() bool {
	out, err := query(r.root, "ls-files", "--unmerged")
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// Tracked 报告某个相对路径是否在 index 里。
func (r *Repo) Tracked(rel string) bool {
	out, err := query(r.root, "ls-files", "--cached", "--", rel)
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// IndexBlob 读 index 里某个路径的内容，也就是**即将提交的那一份**。
func (r *Repo) IndexBlob(rel string) ([]byte, error) {
	return query(r.root, "show", ":"+rel)
}

// HeadBlob 读 HEAD 里某个路径的内容，也就是**最后一次提交的那一份**。
func (r *Repo) HeadBlob(rel string) ([]byte, error) {
	return query(r.root, "show", "HEAD:"+rel)
}

// IndexEntry 是 index 里的一条记录。
type IndexEntry struct {
	// Mode 是 git 的文件模式：100644 普通文件、100755 可执行、120000 符号链接、
	// 160000 嵌套仓库指针。
	Mode string
	// Path 相对仓库根，以 / 分隔。
	Path string
}

// IsGitlink 报告这条记录是不是嵌套仓库指针——提交进去的只是一个指针，没有内容。
func (e IndexEntry) IsGitlink() bool { return e.Mode == "160000" }

// IndexEntries 列出 index 里某个前缀下的全部记录。
//
// 用 -z：带非 ASCII 字符的路径会被 git 加引号转义，-z 直接绕开这件事。
func (r *Repo) IndexEntries(relPrefix string) ([]IndexEntry, error) {
	out, err := query(r.root, "ls-files", "--cached", "--stage", "-z", "--", relPrefix)
	if err != nil {
		return nil, err
	}
	var entries []IndexEntry
	for _, rec := range strings.Split(string(out), "\x00") {
		// 一条记录的格式是：<mode> SP <object> SP <stage> TAB <path>
		tab := strings.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		fields := strings.Fields(rec[:tab])
		if len(fields) == 0 {
			continue
		}
		entries = append(entries, IndexEntry{Mode: fields[0], Path: rec[tab+1:]})
	}
	return entries, nil
}

// StagedUnder 报告某个前缀下有没有**已暂存**的改动。
func (r *Repo) StagedUnder(relPrefix string) bool {
	if !r.HasHEAD() {
		// 还没有任何提交时，index 里的每一条都算"已暂存"
		return r.Tracked(relPrefix)
	}
	out, err := query(r.root, "diff", "--cached", "--name-only", "--", relPrefix)
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// HooksDir 返回 hook 该装到哪。
//
// 用 git rev-parse --git-path hooks 而不是手拼 .git/hooks：实测它同时正确处理
// worktree（返回主仓库共享的那个）、submodule（.git 是文件）、以及
// core.hooksPath 被 husky / lefthook 设过的情形。手拼那三种全错。
//
// 它是本包**唯一**要读使用者真实配置的查询：core.hooksPath 可能设在全局配置里，
// 把全局配置屏蔽掉就会装到 .git/hooks，而 git 实际上根本不看那儿——
// hook 装了却永不运行，且没有任何迹象。
func (r *Repo) HooksDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-path", "hooks")
	cmd.Dir = r.root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("定位 hooks 目录失败：%w", err)
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(r.root, dir)
	}
	return dir, nil
}

// query 在仓库里跑一条只读的 git 命令。
//
// 不读使用者的全局 / 系统配置：别人的 core.* 设置不该影响这些查询
// （与 workspace.gitOut 同一个立场）。唯一的例外是 HooksDir，理由写在它上面。
func query(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0")
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s：%w（%s）",
			strings.Join(args, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return out.Bytes(), nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gitrepo/ -v`
Expected: PASS（8 个测试全绿）

- [ ] **Step 5: 提交**

```bash
git add internal/gitrepo/
git commit -m "gitrepo：git 只读查询收成一个包

判据要读 index、restore 要读 HEAD、hook 安装要找 hooks 目录，三处都得处理
worktree、submodule、core.hooksPath、路径分隔符、以及别读使用者全局配置。
分散写迟早有一处错，而错了不会报错，只会静默判错或装错地方。

HooksDir 是唯一读使用者真实配置的查询：core.hooksPath 可能设在全局配置里，
屏蔽掉就会装到 git 根本不看的 .git/hooks。"
```

---

### Task 2: `config.Edit` 的 enabled 读写

**Files:**
- Modify: `internal/config/edit.go`
- Test: `internal/config/edit_test.go`

**Interfaces:**
- Consumes: 已有的 `findComponent(id, version) int`、`componentsNode(create bool)`、`mappingValue(node, key)`
- Produces:
  - `func (e *Edit) SetComponentEnabled(id, version string, enabled bool) bool`
  - `func (e *Edit) ClearComponentEnabled(id, version string) bool`
  （都在条目不存在时返回 `false`；`Clear` 在本来就没写 `enabled` 时也返回 `false`）

- [ ] **Step 1: 写失败的测试**

追加到 `internal/config/edit_test.go`：

```go
// enabled 的读写必须保住注释与排版：restore 会在使用者的配置上动这个字段，
// 把注释吃掉一次，人就再也不敢用它了。
const editEnabledFixture = `# 顶部注释必须活着
project: my-erp

components:
  - id: demo/hello
    version: 1.0.0
    enabled: false          # 这条行尾注释也必须活着
  - id: demo/caller
    version: 1.0.0
resources: []
`

func TestSetComponentEnabledFlipsValueAndKeepsComments(t *testing.T) {
	path := writeTemp(t, editEnabledFixture)

	e, err := OpenEdit(path)
	require.NoError(t, err)
	require.True(t, e.SetComponentEnabled("demo/hello", "1.0.0", true))
	require.NoError(t, e.Save())

	got := read(t, path)
	assert.Contains(t, got, "enabled: true")
	assert.NotContains(t, got, "enabled: false")
	assert.Contains(t, got, "# 顶部注释必须活着")
	assert.Contains(t, got, "# 这条行尾注释也必须活着")

	cfg, err := ParseConfigFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.Components[0].Enabled)
	assert.True(t, *cfg.Components[0].Enabled)
}

func TestSetComponentEnabledAddsFieldWhenAbsent(t *testing.T) {
	path := writeTemp(t, editEnabledFixture)

	e, err := OpenEdit(path)
	require.NoError(t, err)
	require.True(t, e.SetComponentEnabled("demo/caller", "1.0.0", false))
	require.NoError(t, e.Save())

	cfg, err := ParseConfigFile(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.Components[1].Enabled)
	assert.False(t, *cfg.Components[1].Enabled)
}

func TestClearComponentEnabledRemovesField(t *testing.T) {
	path := writeTemp(t, editEnabledFixture)

	e, err := OpenEdit(path)
	require.NoError(t, err)
	require.True(t, e.ClearComponentEnabled("demo/hello", "1.0.0"))
	require.NoError(t, e.Save())

	got := read(t, path)
	assert.NotContains(t, got, "enabled:")

	cfg, err := ParseConfigFile(path)
	require.NoError(t, err)
	assert.Nil(t, cfg.Components[0].Enabled, "删掉字段 = 回到默认，不是 false")
}

func TestEnabledEditsReportMissingTargets(t *testing.T) {
	path := writeTemp(t, editEnabledFixture)

	e, err := OpenEdit(path)
	require.NoError(t, err)
	assert.False(t, e.SetComponentEnabled("demo/hello", "9.9.9", true), "版本对不上就不是同一个条目")
	assert.False(t, e.ClearComponentEnabled("nope/thing", "1.0.0"))
	assert.False(t, e.ClearComponentEnabled("demo/caller", "1.0.0"), "本来就没写 enabled")
}
```

`writeTemp` / `read` 是 `edit_test.go` 里已有的夹具（第 15 / 22 行），直接用，别再造一份。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run "Enabled" -v`
Expected: FAIL，`e.SetComponentEnabled undefined`

- [ ] **Step 3: 写实现**

在 `internal/config/edit.go` 里 `RemoveComponent` 之后插入：

```go
// keyEnabled 是组件条目里那个启停字段的键名。
const keyEnabled = "enabled"

// SetComponentEnabled 把某个组件条目的 enabled 设成给定值。条目不存在时返回 false。
//
// 在节点层改而不是重新序列化整个结构体：注释、字段顺序、`${ENV_VAR}` 全部原样
// （与 AddComponent / RemoveComponent 同一个理由）。
func (e *Edit) SetComponentEnabled(id, version string, enabled bool) bool {
	item := e.componentItem(id, version)
	if item == nil {
		return false
	}
	if node := mappingValue(item, keyEnabled); node != nil {
		// 就地改标量：这样行尾注释（yaml.Node 挂在键或值上的 LineComment）留得住
		node.Kind = yaml.ScalarNode
		node.Tag = "!!bool"
		node.Value = strconv.FormatBool(enabled)
		node.Style = 0
		node.Content = nil
		node.Alias = nil
		return true
	}
	item.Content = append(item.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: keyEnabled},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(enabled)})
	return true
}

// ClearComponentEnabled 删掉某个组件条目的 enabled 字段。
//
// 条目不存在、或本来就没写 enabled 时返回 false。
//
// 删字段与写 enabled: false **不是一回事**：不写才是默认（跟着上层走），
// 写了才是"钉住"的显式意图（004 §3.3）。还原时必须能表达"回到不写"。
func (e *Edit) ClearComponentEnabled(id, version string) bool {
	item := e.componentItem(id, version)
	if item == nil {
		return false
	}
	for i := 0; i+1 < len(item.Content); i += 2 {
		if item.Content[i].Value == keyEnabled {
			item.Content = append(item.Content[:i], item.Content[i+2:]...)
			return true
		}
	}
	return false
}

// componentItem 找到某个组件条目的映射节点，不存在时返回 nil。
func (e *Edit) componentItem(id, version string) *yaml.Node {
	i := e.findComponent(id, version)
	if i < 0 {
		return nil
	}
	return e.componentsNode(false).Content[i]
}
```

把 `strconv` 加进 `internal/config/edit.go` 的 import 块。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: PASS（新增 4 个测试绿，原有测试不变）

- [ ] **Step 5: 提交**

```bash
git add internal/config/edit.go internal/config/edit_test.go
git commit -m "config.Edit：加 enabled 的节点级读写

restore 要在使用者的配置上改 enabled，而删字段与写 enabled: false 不是
一回事——不写是默认（跟着上层走），写了才是钉住的显式意图（004 §3.3），
所以两个方法都得有。

在节点层改而不是重序列化：注释、字段顺序、\${ENV_VAR} 全部原样。"
```

---

### Task 3: 纯判据 —— `layoutFromIndex` 与 `judgeCommit`

**Files:**
- Create: `internal/cli/restore_check.go`
- Test: `internal/cli/restore_check_test.go`

**Interfaces:**
- Consumes: `gitrepo.IndexEntry`（Task 1）、`config.DirArchived`
- Produces:
  - `type commitLayout struct { active, archived map[string]bool; gitlinks []string }`
  - `func layoutFromIndex(entries []gitrepo.IndexEntry, componentsRel string, ids []string) commitLayout`
  - `type violationKind string` 与常量 `violationArchived` / `violationBoth`
  - `type violation struct { componentID string; kind violationKind }`
  - `func judgeCommit(ids []string, running map[string]bool, l commitLayout) []violation`
  - `func under(path, prefix string) bool`

- [ ] **Step 1: 写失败的测试（3 × 4 状态表逐格）**

新建 `internal/cli/restore_check_test.go`：

```go
package cli

// 本文件测的是**提交前判据的纯逻辑**：不碰 git，也不碰磁盘。
//
// 判据是要硬拦人的东西，漏一格就是有人提交不了、或者提交错了。所以
// 「判定结果 × index 里源码在哪」这张 3 × 4 的表逐格都有一个测试。

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/gitrepo"
)

// idxFile 造一条普通文件的 index 记录。
func idxFile(path string) gitrepo.IndexEntry {
	return gitrepo.IndexEntry{Mode: "100644", Path: path}
}

// idxGitlink 造一条嵌套仓库指针的 index 记录（路径没有尾斜杠）。
func idxGitlink(path string) gitrepo.IndexEntry {
	return gitrepo.IndexEntry{Mode: "160000", Path: path}
}

func TestLayoutFromIndexFoldsByComponentID(t *testing.T) {
	entries := []gitrepo.IndexEntry{
		idxFile("components/people/basic/main.go"),
		idxFile("components/.archived/erp/backend/main.go"),
		idxGitlink("components/portal/web"),
		idxFile("components/stranger/thing/main.go"), // 没声明的组件
	}
	l := layoutFromIndex(entries, "components",
		[]string{"people/basic", "erp/backend", "portal/web"})

	assert.True(t, l.active["people/basic"])
	assert.True(t, l.archived["erp/backend"])
	assert.False(t, l.active["erp/backend"])
	assert.True(t, l.active["portal/web"], "gitlink 路径没有尾斜杠，也得认出来")
	assert.False(t, l.active["stranger/thing"], "没声明的组件不该出现在结果里")
	assert.Equal(t, []string{"components/portal/web"}, l.gitlinks)
}

func TestLayoutFromIndexHandlesNestedProjectPrefix(t *testing.T) {
	entries := []gitrepo.IndexEntry{
		idxFile("apps/erp/components/.archived/people/basic/main.go"),
	}
	l := layoutFromIndex(entries, "apps/erp/components", []string{"people/basic"})
	assert.True(t, l.archived["people/basic"], "项目在仓库子目录里时前缀要跟着走")
}

// judgeCase 是 3 × 4 表里的一格。
type judgeCase struct {
	name     string
	running  bool
	active   bool
	archived bool
	want     []violation
}

func TestJudgeCommitFullStateTable(t *testing.T) {
	const id = "people/basic"
	cases := []judgeCase{
		{"该跑-活跃：自洽", true, true, false, nil},
		{"该跑-归档：唯一的主判据", true, false, true,
			[]violation{{id, violationArchived}}},
		{"该跑-两处都有：违反一个 ID 一份源码", true, true, true,
			[]violation{{id, violationBoth}}},
		{"该跑-都没有：源码没进仓库，管不着", true, false, false, nil},
		{"不该跑-活跃：只是没跑过 sync，允许", false, true, false, nil},
		{"不该跑-归档：意图声明，放行", false, false, true, nil},
		{"不该跑-两处都有：与 yaml 说什么无关", false, true, true,
			[]violation{{id, violationBoth}}},
		{"不该跑-都没有", false, false, false, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := commitLayout{active: map[string]bool{}, archived: map[string]bool{}}
			l.active[id] = c.active
			l.archived[id] = c.archived
			running := map[string]bool{id: c.running}

			assert.Equal(t, c.want, judgeCommit([]string{id}, running, l))
		})
	}
}

func TestJudgeCommitIgnoresUndeclaredComponents(t *testing.T) {
	l := commitLayout{
		active:   map[string]bool{"stranger/thing": true},
		archived: map[string]bool{"stranger/thing": true},
	}
	// ids 为空 = brickkit.yaml 里一个都没声明
	assert.Empty(t, judgeCommit(nil, map[string]bool{}, l),
		"没声明的组件平台一律不管，两处都有也不管")
}

func TestJudgeCommitIsDeterministicAcrossComponents(t *testing.T) {
	l := commitLayout{
		active:   map[string]bool{"b/two": true},
		archived: map[string]bool{"a/one": true, "b/two": true},
	}
	running := map[string]bool{"a/one": true, "b/two": true}

	got := judgeCommit([]string{"a/one", "b/two"}, running, l)
	assert.Equal(t, []violation{
		{"a/one", violationArchived},
		{"b/two", violationBoth},
	}, got, "输出顺序必须跟着入参的 ID 顺序，否则错误信息每次不一样")
}

func TestUnderMatchesPrefixItselfAndChildren(t *testing.T) {
	assert.True(t, under("components/erp/backend", "components/erp/backend"),
		"gitlink 的路径就是目录本身")
	assert.True(t, under("components/erp/backend/m.go", "components/erp/backend"))
	assert.False(t, under("components/erp/backend2/m.go", "components/erp/backend"),
		"前缀匹配不能把 backend2 也算进来")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run "TestLayoutFromIndex|TestJudgeCommit|TestUnder" -v`
Expected: FAIL，`undefined: layoutFromIndex`

- [ ] **Step 3: 写实现**

新建 `internal/cli/restore_check.go`（本任务只写纯逻辑部分，接线在 Task 4）：

```go
package cli

// 本文件实现提交前的结构自洽判据（brickkit restore --check）。
//
// 它回答一个问题：**这次提交里，brickkit.yaml 与组件源码结构自洽吗。**
//
// 判据分成两半，本文件上半是**纯函数**（不碰 git、不碰磁盘），下半才接线。
// 分开是因为这个判据要硬拦人：3 × 4 的状态表必须逐格测到，而带上 git 与
// Manifest 解析之后，写全那 12 格的代价会高到没人愿意写。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/gitrepo"
)

// commitLayout 是"即将提交的那份目录结构"按**组件 ID** 折出来的结果。
//
// 按 ID 而不是按 (id, version) 条目：一个组件 ID 只有一份源码目录（004 §8.1），
// 同 ID 的多个版本共用它。按条目判会让"同 ID 一个版本跑、一个不跑"自相矛盾。
type commitLayout struct {
	// active[id] 为真表示 index 里 <components>/<id>/ 下有东西。
	active map[string]bool
	// archived[id] 为真表示 index 里 <components>/.archived/<id>/ 下有东西。
	archived map[string]bool
	// gitlinks 是 index 里的嵌套仓库指针路径（相对仓库根，已排序）。
	gitlinks []string
}

// layoutFromIndex 把 index 记录折成 commitLayout。
//
// ids 是 brickkit.yaml 里声明过的组件 ID。**没声明的一律不管**：判定算不到它，
// 那是使用者自己在开发、还没 add 的源码（与 planSync 同一个边界）。
func layoutFromIndex(entries []gitrepo.IndexEntry, componentsRel string, ids []string) commitLayout {
	l := commitLayout{active: map[string]bool{}, archived: map[string]bool{}}
	archivedRoot := componentsRel + "/" + config.DirArchived
	for _, e := range entries {
		if e.IsGitlink() {
			l.gitlinks = append(l.gitlinks, e.Path)
		}
		for _, id := range ids {
			switch {
			case under(e.Path, archivedRoot+"/"+id):
				l.archived[id] = true
			case under(e.Path, componentsRel+"/"+id):
				l.active[id] = true
			}
		}
	}
	sort.Strings(l.gitlinks)
	return l
}

// under 报告 path 是不是 prefix 本身、或 prefix 下的东西。
//
// "prefix 本身"这一支是给嵌套仓库用的：它在 index 里是一条 160000 记录，
// 路径就是那个目录本身，**没有尾斜杠**（实测 components/erp/backend）。
// 而拼上 "/" 再比是为了不把 backend2 当成 backend 的孩子。
func under(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// violationKind 是一次提交里两种自洽性问题。
type violationKind string

const (
	// violationArchived 是"判定说它该跑，源码却提交在归档目录里"——那个反复发生的失误。
	violationArchived violationKind = "archived"
	// violationBoth 是"同一个组件的源码在提交里出现了两处"。
	violationBoth violationKind = "both"
)

// violation 是一条违规。
type violation struct {
	componentID string
	kind        violationKind
}

// judgeCommit 得出违规清单。纯函数：不碰 git，也不碰磁盘。
//
// # 为什么只拦一个方向
//
// 反方向——源码在活跃目录、而 yaml 说它不跑——只是"没跑过 sync"。004 §3.9
// 明说 sync 是可选的（"用户忘记执行就自己发现、自己处理"），拦它等于强迫
// 全员跑 sync，影响面大得多。
//
// 而"归档结构 + enabled: false 一起进了提交"同样放行：那是使用者的**意图声明**。
// 他要这个结构，平台没有立场替他改主意。
//
// # 为什么"两处都有"是独立的一条，且不看判定结果
//
// 它不是主判据的特例，它是一个**死循环的解药**。这种状态下 workspace.Locate
// 活跃优先，planSync 判它"已经在该在的位置"、什么都不做——restore 跑完什么都没变，
// 而闸门还在拦，且没有任何出路。所以它必须先判、单独报、并给出手工出路。
func judgeCommit(ids []string, running map[string]bool, l commitLayout) []violation {
	var vs []violation
	for _, id := range ids {
		switch {
		case l.active[id] && l.archived[id]:
			vs = append(vs, violation{id, violationBoth})
		case running[id] && l.archived[id]:
			vs = append(vs, violation{id, violationArchived})
		}
	}
	return vs
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -run "TestLayoutFromIndex|TestJudgeCommit|TestUnder" -v`
Expected: PASS（含 3 × 4 表的 8 个子测试）

- [ ] **Step 5: 提交**

```bash
git add internal/cli/restore_check.go internal/cli/restore_check_test.go
git commit -m "提交前判据的纯逻辑：3 × 4 状态表逐格覆盖

判据要硬拦人，漏一格就是有人提交不了或者提交错了。所以先把它写成不碰
git、不碰磁盘的纯函数，把「判定结果 × index 里源码在哪」12 格全测掉。

只拦一个方向：该跑却提交在 .archived/ 下。反方向只是没跑过 sync，§3.9
明说允许。另加一条双向的「两处都有」——那种状态下 planSync 什么都不做，
不单独报就与闸门死循环。"
```

---

### Task 4: `brickkit restore --check` 接线

**Files:**
- Modify: `internal/cli/restore_check.go`
- Create: `internal/cli/restore.go`（只放命令定义，`runRestore` 在 Task 5）
- Modify: `internal/cli/sync.go`（抽出 `declaredIDs`，行为不变）
- Modify: `internal/cli/root.go`（注册 `restore`）
- Modify: `internal/cli/cli_test.go`（`allCommands` 加 `restore`；`TestSubcommandFlags` 加 `restore: {check}`）
- Test: `internal/cli/restore_check_test.go`

**Interfaces:**
- Consumes: `layoutFromIndex` / `judgeCommit`（Task 3）、`gitrepo`（Task 1）、已有的 `syncFocus(ctx, opts, layout, cfg) (*focus, error)` 与 `focus.keep map[string]bool`
- Produces:
  - `func newRestoreCommand(opts *Options) *cobra.Command`
  - `func runRestoreCheck(ctx context.Context, opts *Options) error`
  - `func declaredIDs(cfg *config.Config) []string`（排序去重，`planSync` 也改用它）

- [ ] **Step 1: 写失败的测试**

追加到 `internal/cli/restore_check_test.go`。夹具复用 `sync_test.go` 的 `newSyncFixture`（它已经会造带真 Git 仓库的源码目录）：

```go
// gitProject 把测试项目本身变成一个 git 仓库，并把 components/ 一起提交进去。
//
// 这正是本设计要服务的那种项目：使用者把 components/ 从 .gitignore 去掉了。
func gitProject(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v：%s", args, out)
	}
	// 让 components/ 进得去：init 生成的 .gitignore 里有它
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("/.brickkit/\n"), 0o644))
}

func gitDo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v：%s", args, out)
}

func TestCheckPassesWhenComponentsNotTracked(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	// 只提交配置，components/ 不进仓库（默认情形）
	gitDo(t, f.Dir, "add", "brickkit.yaml", ".gitignore")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	r := runIn(t, f.Dir, "restore", "--check")
	assert.Equal(t, clierr.ExitOK, r.code, "components/ 没进仓库时必须零成本放行：%s%s", r.stdout, r.stderr)
}

func TestCheckBlocksArchivedStructureWithoutTheYAML(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	// 本地关掉 hello 并 sync（caller 跟着级联归档）
	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	// 只暂存结构变动，**不**暂存 yaml —— 就是那个反复发生的失误
	gitDo(t, f.Dir, "add", "-A", "components")

	r := runIn(t, f.Dir, "restore", "--check")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "提交被拦下")
	assert.Contains(t, r.stderr, "demo/hello")
	assert.Contains(t, r.stderr, "brickkit restore")
	assert.Contains(t, r.stderr, "git add brickkit.yaml")
}

func TestCheckPassesWhenYAMLGoesInWithTheStructure(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	gitDo(t, f.Dir, "add", "-A") // yaml 一起进提交 = 意图声明

	r := runIn(t, f.Dir, "restore", "--check")
	assert.Equal(t, clierr.ExitOK, r.code,
		"enabled: false 一起提交了就是他要这个结构：%s%s", r.stdout, r.stderr)
}

func TestCheckBlocksSourceInBothPlaces(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	// 窄 pathspec：只暂存新增的归档路径，旧活跃路径的删除没进 index
	gitDo(t, f.Dir, "add", filepath.Join("components", ".archived"))

	r := runIn(t, f.Dir, "restore", "--check")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "出现了两处")
	assert.Contains(t, r.stderr, "git add -A")
}

func TestCheckSkipsDuringMergeConflict(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	gitDo(t, f.Dir, "checkout", "--quiet", "-b", "other")
	require.NoError(t, os.WriteFile(filepath.Join(f.Dir, "k.txt"), []byte("v1\n"), 0o644))
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "v1")
	gitDo(t, f.Dir, "checkout", "--quiet", "-")
	require.NoError(t, os.WriteFile(filepath.Join(f.Dir, "k.txt"), []byte("v2\n"), 0o644))
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "v2")
	// 制造冲突（合并会失败，这里刻意忽略返回值）
	cmd := exec.Command("git", "merge", "other")
	cmd.Dir = f.Dir
	_ = cmd.Run()

	r := runIn(t, f.Dir, "restore", "--check")
	assert.Equal(t, clierr.ExitOK, r.code, "冲突中必须放行：git show :<path> 在那时会 fatal")
	assert.Contains(t, r.stdout, "跳过")
}

func TestCheckOutsideGitRepoPasses(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello")

	r := runIn(t, f.Dir, "restore", "--check")
	assert.Equal(t, clierr.ExitOK, r.code, "不在 git 仓库里就没有「即将提交的东西」可判")
}
```

需要的 import：`os`、`os/exec`、`path/filepath`、`github.com/stretchr/testify/require`、`github.com/brickkit/brickkit/internal/clierr`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run "TestCheck" -v`
Expected: FAIL，`未知命令 restore`

- [ ] **Step 3: 抽出 `declaredIDs`（sync.go，行为不变）**

把 `internal/cli/sync.go` 里 `planSync` 开头那段 ID 去重换成调用新函数，并把函数放在 `sync.go` 里：

```go
// declaredIDs 返回配置里声明过的组件 ID，排序去重。
//
// 一个组件 ID 只有一份源码目录（004 §8.1），与版本无关，所以按 ID 去重。
// 排序是为了输出与判据结果稳定——错误信息每次顺序不同会让人以为在变。
func declaredIDs(cfg *config.Config) []string {
	var ids []string
	seen := map[string]bool{}
	for _, c := range cfg.Components {
		if !seen[c.ID] {
			seen[c.ID] = true
			ids = append(ids, c.ID)
		}
	}
	sort.Strings(ids)
	return ids
}
```

`planSync` 里原来那 9 行（`var ids []string` 到 `sort.Strings(ids)`）替换成：

```go
	ids := declaredIDs(cfg)
```

- [ ] **Step 4: 写命令定义**

新建 `internal/cli/restore.go`：

```go
package cli

// 本文件实现 brickkit restore（004 §3.14）：把 brickkit.yaml 的 enabled 与组件
// 源码结构还原到最后一次提交，以及供 pre-commit hook 调用的 --check。

import (
	"context"

	"github.com/spf13/cobra"
)

// newRestoreCommand 实现 brickkit restore（004 §3.14）。
func newRestoreCommand(opts *Options) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:     "restore",
		Short:   "把 brickkit.yaml 的 enabled 与组件源码结构还原到最后一次提交",
		GroupID: groupProject,
		Long: `把 brickkit.yaml 里各组件的 enabled 还原成最后一次提交的值，再让源码结构跟着走（004 §3.14）。

给谁用：把 components/ 从 .gitignore 去掉、让组件源码跟项目一起进版本库的项目。
那种项目里 brickkit sync 移动目录会进项目的 diff，而"本地关掉几个顶层、
sync 归档、干完活忘了还原就提交"这件事会反复发生。

它只动 enabled 这一个字段，逐条动：

  - 工作区与最后一次提交都有的条目 → enabled 回到提交里的值（提交里没写就删掉字段）
  - 工作区新增的条目（刚 add 的、或改了版本号）→ 一个字不动
  - 提交里有而工作区没有的 → 绝不加回来（它不是 git revert）

其余改动一律不碰，所以刚 add 的组件不会被它吃掉。被覆盖的旧值会在动手前印出来。

--check 只检查、不改任何东西：即将提交的 yaml 与即将提交的目录结构自洽吗。
不自洽就非零退出——pre-commit hook 调的就是它（brickkit init --hooks 装）。`,
		Example: `  brickkit restore           还原 enabled 与源码结构
  brickkit restore --check   只检查这次提交自洽不自洽（退出码非零表示不自洽）`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if check {
				return runRestoreCheck(cmd.Context(), opts)
			}
			return runRestore(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false,
		"只检查即将提交的 yaml 与目录结构是否自洽，不改任何东西（供 pre-commit hook 调用）")
	return cmd
}

// runRestore 在 Task 5 实现；先给一个占位以便 --check 这一半能独立测。
func runRestore(ctx context.Context, opts *Options) error {
	return nil
}
```

在 `internal/cli/root.go` 的 `root.AddCommand(...)` 里，`newSyncCommand(opts)` 之后加一行 `newRestoreCommand(opts),`。

- [ ] **Step 5: 写 `runRestoreCheck` 接线**

追加到 `internal/cli/restore_check.go`：

```go
// runRestoreCheck 执行 brickkit restore --check。
//
// # 它读哪两份东西，以及为什么不能读别处
//
//	即将提交的 yaml    → index（git show :<rel>）
//	即将提交的结构      → index（git ls-files --cached --stage）
//
// 读 HEAD 的 yaml 会造出一个真死锁：yaml 的改动永远比结构晚一拍，于是
// "改 yaml + 归档结构一起提交"这件事**永远做不成**。读工作区的结构则会管到
// 没 git add 的东西——那些不进这次提交，不该拦。
//
// # 四种情形放行，绝不拦
//
// 冲突中、配置没交给 git、全图算不出来、找不到自己——闸门是抓一个特定失误，
// 不是质量门。把提交堵死在一次网络错误上，代价远大于漏掉一次。
func runRestoreCheck(ctx context.Context, opts *Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)

	repo, err := gitrepo.Open(layout.Root)
	if err != nil {
		return nil // 不在 git 仓库里：没有"即将提交的东西"可判
	}
	if repo.Unmerged() {
		opts.Printf("⚠️  正在解决冲突，跳过组件结构检查\n")
		return nil
	}
	cfgRel, ok := repo.Rel(layout.ConfigPath())
	if !ok || !repo.Tracked(cfgRel) {
		return nil // 配置在仓库外、或没交给 git：管不着
	}
	compRel, ok := repo.Rel(layout.ComponentsDir())
	if !ok {
		return nil
	}

	// 只查这一次：判定、短路、gitlink 提醒三处共用同一份结果。
	// 分成多次查会让短路把 gitlink 提醒一起短路掉。
	entries, err := repo.IndexEntries(compRel)
	if err != nil {
		return skipCheck(opts, "读不到即将提交的组件目录结构", err)
	}
	if !hasArchivedEntry(entries, compRel) {
		// components/ 还在 .gitignore 里的默认情形走的就是这一条：零成本
		warnGitlinks(opts, gitlinkPaths(entries))
		return nil
	}

	data, err := repo.IndexBlob(cfgRel)
	if err != nil {
		return skipCheck(opts, "读不到即将提交的 "+layout.ConfigName(), err)
	}
	cfg, err := config.ParseConfig(data, "index:"+cfgRel)
	if err != nil {
		return skipCheck(opts, "即将提交的 "+layout.ConfigName()+" 解析不了", err)
	}
	f, err := syncFocus(ctx, opts, layout, cfg)
	if err != nil {
		// 算不出来 ≠ 判据不通过。Manifest 缺失或要联网时会走到这里。
		return skipCheck(opts, "算不出这次会启动哪些组件", err)
	}

	ids := declaredIDs(cfg)
	l := layoutFromIndex(entries, compRel, ids)
	warnGitlinks(opts, l.gitlinks)

	vs := judgeCommit(ids, f.keep, l)
	if len(vs) == 0 {
		return nil
	}
	return violationError(vs, compRel, layout.ConfigName())
}

// hasArchivedEntry 报告即将提交的东西里有没有归档目录下的路径。
func hasArchivedEntry(entries []gitrepo.IndexEntry, componentsRel string) bool {
	root := componentsRel + "/" + config.DirArchived
	for _, e := range entries {
		if under(e.Path, root) {
			return true
		}
	}
	return false
}

// gitlinkPaths 挑出 index 里的嵌套仓库指针路径。
func gitlinkPaths(entries []gitrepo.IndexEntry) []string {
	var paths []string
	for _, e := range entries {
		if e.IsGitlink() {
			paths = append(paths, e.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

// skipCheck 说明为什么这次没检查，然后放行。
//
// 放行而不是拦：闸门守的是一个特定失误，不是"什么都得对"。堵死一次提交的
// 代价，远大于漏掉一次——尤其当原因是网络或缓存，与使用者正在做的事毫无关系。
func skipCheck(opts *Options, reason string, cause error) error {
	opts.Printf("%s", clierr.Warn(clierr.CodeConfigInvalid, "跳过组件结构检查："+reason).
		WithDetail("原因", cause.Error()).
		WithHint("这次提交照常进行；想手工确认就跑 brickkit restore --check").
		Format())
	return nil
}

// warnGitlinks 提醒嵌套的 Git 仓库进了提交。**只提醒，不改退出码。**
//
// 它超出"结构还原"的职责，但和"把 components/ 从 .gitignore 去掉"是同一个决定
// 引出来的坑：没有 .gitmodules 的 gitlink 不是指针，是个死记录。
// 004 §8.2 早就点过"会出现 Git 嵌套仓库的问题"，这里只是让它在真发生时说话。
func warnGitlinks(opts *Options, paths []string) {
	for _, p := range paths {
		opts.Printf("%s", clierr.Warn(clierr.CodeConfigInvalid,
			p+" 是一个嵌套的 Git 仓库（提交进去的只是一个指针）").
			WithHint(
				"仓库里没有 .gitmodules，队友 clone 下来只会得到一个空目录",
				"git submodule update 也拉不回来——没有地方记着它的 URL",
			).Format())
	}
}

// violationError 把违规清单变成那句该说的话。
//
// "两处都有"优先：它比"提交在归档目录里"更准，而且出路完全不同。两种同时存在时
// 先报它——修完再跑一次就看到另一种。
func violationError(vs []violation, componentsRel, configName string) error {
	archivedRoot := componentsRel + "/" + config.DirArchived

	var both, archived []string
	for _, v := range vs {
		if v.kind == violationBoth {
			both = append(both, v.componentID)
			continue
		}
		archived = append(archived, v.componentID)
	}

	if len(both) > 0 {
		e := clierr.New(clierr.CodeConfigConflict,
			"提交被拦下：同一个组件的源码在提交里出现了两处")
		for _, id := range both {
			e = e.WithDetail(id, componentsRel+"/"+id+"  与  "+archivedRoot+"/"+id)
		}
		return e.WithHint(
			"一个组件 ID 只能有一个源码目录（004 §8.1）",
			"多半是 git add 的路径太窄，漏掉了旧路径的删除：git add -A "+componentsRel+"/",
			"两处都有源码时，平台不替你决定保留哪一份",
		)
	}

	e := clierr.New(clierr.CodeConfigConflict,
		"提交被拦下：组件源码提交在归档目录里，但 "+configName+" 说它该启动")
	for _, id := range archived {
		e = e.WithDetail(id, "即将提交的位置："+archivedRoot+"/"+id)
	}
	return e.WithHint(
		"想保留这个归档结构 → git add "+configName+
			"（yaml 里的 enabled: false 进了提交，就是你的意图声明）",
		"不想 → brickkit restore，然后重新 git add",
	)
}
```

把 `context`、`github.com/brickkit/brickkit/internal/clierr` 加进 `restore_check.go` 的 import 块。

- [ ] **Step 6: 更新命令清单测试**

`internal/cli/cli_test.go`：

```go
var allCommands = []string{
	"init", "add", "remove", "up", "down", "status",
	"fetch", "sync", "restore", "login", "publish", "version",
}
```

`TestSubcommandFlags` 的 `want` 里加一行：

```go
		"restore": {"check"},
```

- [ ] **Step 7: 跑测试确认通过**

Run: `go test ./internal/cli/ -run "TestCheck|TestSync|TestRootHelp|TestSubcommandFlags|TestEachSubcommandHelp" -v`
Expected: PASS（`TestCheck*` 6 个全绿，`sync` 原有测试不变）

- [ ] **Step 8: 提交**

```bash
git add internal/cli/restore.go internal/cli/restore_check.go internal/cli/restore_check_test.go internal/cli/sync.go internal/cli/root.go internal/cli/cli_test.go
git commit -m "brickkit restore --check：提交前的结构自洽闸门

读 index 而不是 HEAD 是硬约束：读 HEAD 会让 yaml 的改动永远比结构晚一拍，
「改 yaml + 归档结构一起提交」就永远做不成。

components/ 的 index 记录只查一次，一份结果同时供判定、短路、gitlink
提醒三处用——分开查会让短路把 gitlink 提醒一起短路掉。

四种情形放行加警告，绝不拦：冲突中、配置没交给 git、全图算不出来、
读不到结构。闸门抓的是一个特定失误，不是质量门。

planSync 里的 ID 去重抽成 declaredIDs 两处共用，行为不变。"
```

---

### Task 5: `brickkit restore` 主流程

**Files:**
- Modify: `internal/cli/restore.go`
- Modify: `internal/cli/sync.go`（抽出 `applyWorkspacePlan`，行为不变）
- Modify: `internal/workspace/workspace.go`（加 `InBothPlaces`）
- Test: `internal/cli/restore_test.go`
- Test: `internal/workspace/workspace_test.go`

**Interfaces:**
- Consumes: `config.OpenEdit` / `SetComponentEnabled` / `ClearComponentEnabled`（Task 2）、`gitrepo`（Task 1）、`declaredIDs`（Task 4）、已有的 `syncFocus` / `planSync` / `applySync`
- Produces:
  - `type enabledChange struct { id, version string; from, to *bool }`
  - `func restorePlan(work, head *config.Config) (changes []enabledChange, untouched []string)`
  - `func runRestore(ctx context.Context, opts *Options) error`（替换 Task 4 的占位）
  - `func workspace.InBothPlaces(l config.Layout, componentID string) bool`
  - `func applyWorkspacePlan(opts *Options, layout config.Layout, actions []syncAction) error`

- [ ] **Step 1: 写失败的测试**

新建 `internal/cli/restore_test.go`：

```go
package cli

// 本文件测 brickkit restore：yaml 的 enabled 逐条还原 + 结构跟着走。
//
// 最要紧的两条断言是"它不该做什么"：不吃掉未提交的 add，判定失败时不留下
// "yaml 改了、结构没动"的半成品。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
)

func boolp(b bool) *bool { return &b }

func TestRestorePlanOnlyTouchesEntriesPresentInBothVersions(t *testing.T) {
	head := &config.Config{Components: []config.Component{
		{ID: "demo/hello", Version: "1.0.0"},                     // 提交里没写 enabled
		{ID: "demo/caller", Version: "1.0.0", Enabled: boolp(true)},
		{ID: "gone/thing", Version: "1.0.0"},                     // 本地已 remove
	}}
	work := &config.Config{Components: []config.Component{
		{ID: "demo/hello", Version: "1.0.0", Enabled: boolp(false)}, // 本地关掉了
		{ID: "demo/caller", Version: "1.0.0", Enabled: boolp(true)}, // 没变
		{ID: "brand/new", Version: "0.1.0", Enabled: boolp(false)},  // 本地新 add 的
		{ID: "demo/bumped", Version: "2.0.0", Enabled: boolp(false)},// 本地改了版本号
	}}

	changes, untouched := restorePlan(work, head)

	require.Len(t, changes, 1, "只有 hello 需要还原")
	assert.Equal(t, "demo/hello", changes[0].id)
	assert.Nil(t, changes[0].to, "提交里没写 enabled → 删掉这个字段，不是写 false")
	require.NotNil(t, changes[0].from)
	assert.False(t, *changes[0].from)

	assert.ElementsMatch(t, []string{"brand/new@0.1.0", "demo/bumped@2.0.0"}, untouched,
		"提交里没有的条目一个字不动——这是不吃掉未提交 add 的解药")
}

func TestRestoreRestoresEnabledAndMovesSourceBack(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	f.assertArchived(t, "demo/hello")

	r := runIn(t, f.Dir, "restore")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "enabled: false")
	assert.Contains(t, r.stdout, "删除该字段")

	f.assertActive(t, "demo/hello")
	f.assertActive(t, "demo/caller")

	cfg := f.parsed(t)
	assert.Nil(t, cfg.Components[0].Enabled, "enabled 回到「不写」")
}

func TestRestoreKeepsUncommittedAddInTheConfig(t *testing.T) {
	comps := []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		{ID: "solo/thing", Version: "1.0.0"},
	}
	f := addedProject(t, comps)
	f.writeConfig(t, allEnabled)
	initGitRepo(t, filepath.Join(f.Dir, "components", "demo", "hello"))
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	// 提交之后才 add 的组件
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "solo/thing@1.0.0", "--yes").code)

	r := runIn(t, f.Dir, "restore")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	cfg := f.parsed(t)
	var ids []string
	for _, c := range cfg.Components {
		ids = append(ids, c.ID)
	}
	assert.Contains(t, ids, "solo/thing",
		"restore 只动 enabled，绝不像 git checkout 那样把未提交的 add 一起吃掉")
}

func TestRestoreRejectsStagedComponentChanges(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	// 在归档目录里改了代码并暂存（004 §3.9.3 明说允许在那儿改）
	require.NoError(t, os.WriteFile(
		filepath.Join(f.archived("demo/hello"), "component.yaml"), []byte("# 改了\n"), 0o644))
	gitDo(t, f.Dir, "add", "-A", "components")

	r := runIn(t, f.Dir, "restore")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "已暂存")
	f.assertArchived(t, "demo/hello")
}

func TestRestoreRejectsSourceInBothPlaces(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	// 手工造出"两处都有"
	require.NoError(t, os.MkdirAll(f.archived("demo/hello"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(f.archived("demo/hello"), "component.yaml"), []byte("# 另一份\n"), 0o644))

	r := runIn(t, f.Dir, "restore")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "两处")
	assert.Contains(t, r.stderr, "不替你决定")
}

func TestRestoreRequiresGitBaseline(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello")

	r := runIn(t, f.Dir, "restore")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "git")

	gitProject(t, f.Dir) // 有仓库了，但还没有任何提交
	r = runIn(t, f.Dir, "restore")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "提交")
}

func TestRestoreIsIdempotent(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)

	first := runIn(t, f.Dir, "restore")
	require.Equal(t, clierr.ExitOK, first.code, first.stdout+first.stderr)
	after := f.config(t)

	second := runIn(t, f.Dir, "restore")
	require.Equal(t, clierr.ExitOK, second.code, second.stdout+second.stderr)
	assert.Equal(t, after, f.config(t), "连跑两次的结果必须一样")
	f.assertActive(t, "demo/hello")
}
```

`internal/workspace/workspace_test.go` 追加：

```go
func TestInBothPlacesAnswersWhatLocateCannot(t *testing.T) {
	dir := t.TempDir()
	l := config.NewLayout(dir, "")
	const id = "demo/hello"

	require.NoError(t, os.MkdirAll(SourceDir(l, id), 0o755))
	assert.False(t, InBothPlaces(l, id))

	require.NoError(t, os.MkdirAll(ArchivedDir(l, id), 0o755))
	assert.True(t, InBothPlaces(l, id))
	assert.Equal(t, StateActive, Locate(l, id), "Locate 活跃优先，答不出这一种")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run TestRestore -v` 与 `go test ./internal/workspace/ -run TestInBothPlaces -v`
Expected: FAIL，`undefined: restorePlan`、`undefined: InBothPlaces`

- [ ] **Step 3: 加 `workspace.InBothPlaces`**

追加到 `internal/workspace/workspace.go` 的归档小节：

```go
// InBothPlaces 报告一个组件的源码是不是活跃目录与归档目录**都有**。
//
// # 为什么 Locate 答不出这一问
//
// Locate 回答"在哪"时活跃优先——两处都有时它答 StateActive，于是 planSync 判它
// "已经在该在的位置"、什么都不做。而这种状态是**错的**：004 §8.1 立过
// "一个组件 ID 只有一个源码目录"。
//
// 不单独回答这一问，它就会和提交前的闸门形成死循环：闸门拦下提交、
// restore 说没事可做，两边都没错，人却没有任何出路。
func InBothPlaces(l config.Layout, componentID string) bool {
	return isDir(SourceDir(l, componentID)) && isDir(ArchivedDir(l, componentID))
}
```

- [ ] **Step 4: 抽出 `applyWorkspacePlan`（sync.go，行为不变）**

`internal/cli/sync.go` 里 `runSync` 结尾那段（`if len(actions) == 0 { ... }` 到 `return applySync(...)`）抽成函数，`runSync` 与 `runRestore` 共用：

```go
// applyWorkspacePlan 执行工作区整理计划，并如实汇报。
//
// sync 与 restore 共用它：同一件事只有一处渲染代码，两个命令的输出也就不可能
// 各说一套。
func applyWorkspacePlan(opts *Options, layout config.Layout, actions []syncAction) error {
	if len(actions) == 0 {
		opts.Printf("📂 工作区无需整理\n")
		opts.Printf("   %s 下没有需要归档或激活的组件源码\n", config.DirComponents)
		return nil
	}
	return applySync(opts, layout, actions)
}
```

`runSync` 结尾改为：

```go
	return applyWorkspacePlan(opts, layout, planSync(layout, cfg, keep))
```

- [ ] **Step 5: 写 `restorePlan` 与 `runRestore`**

替换 `internal/cli/restore.go` 里 Task 4 的占位实现：

```go
// enabledChange 是一处 enabled 还原。
type enabledChange struct {
	id, version string
	// from 是工作区当前的值（nil = 没写），只用于如实汇报被覆盖的旧值。
	from *bool
	// to 是要设成的值；nil 表示**删掉这个字段**（最后一次提交里没写）。
	to *bool
}

// ref 返回 id@version，用于输出。
func (c enabledChange) ref() string { return c.id + "@" + c.version }

// restorePlan 算出要改哪些 enabled。纯函数。
//
// 只动"工作区与 HEAD 都有的同一个 (id, version) 条目"，另外两种刻意不动：
//
//	工作区新增的条目     本地刚 add 的、或本地改了版本号。一个字不动——
//	                    这是"不吃掉未提交的 add"的解药。004 §3.10 批评
//	                    brickkit reset 的正是"救配置的命令自己救不回来"
//	HEAD 有而工作区没有   本地 remove 掉的。绝不加回来——restore 不是 revert
//
// 返回的 untouched 是那些"工作区有、提交里没有"的条目引用，要在输出里点名说
// "未动"：使用者得知道为什么它没变，否则会以为命令漏了它。
func restorePlan(work, head *config.Config) ([]enabledChange, []string) {
	headEnabled := make(map[string]*bool, len(head.Components))
	for _, c := range head.Components {
		headEnabled[c.Ref()] = c.Enabled
	}

	var changes []enabledChange
	var untouched []string
	for _, c := range work.Components {
		want, ok := headEnabled[c.Ref()]
		if !ok {
			untouched = append(untouched, c.Ref())
			continue
		}
		if sameEnabled(c.Enabled, want) {
			continue
		}
		changes = append(changes,
			enabledChange{id: c.ID, version: c.Version, from: c.Enabled, to: want})
	}
	return changes, untouched
}

// sameEnabled 比较两个 enabled 值。nil（没写）与 false 不是一回事。
func sameEnabled(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// applyEnabled 把还原结果写进**内存里**的配置。
func applyEnabled(cfg *config.Config, changes []enabledChange) {
	for _, ch := range changes {
		for i := range cfg.Components {
			if cfg.Components[i].ID == ch.id && cfg.Components[i].Version == ch.version {
				cfg.Components[i].Enabled = ch.to
			}
		}
	}
}

// runRestore 执行 brickkit restore。
//
// # 顺序是硬约束
//
//	解析工作区 yaml → 内存里还原 enabled → 算判定 → 落盘 yaml → 移动目录
//
// 反过来（先落盘再算判定）就会在 Manifest 缺失或需要联网时留下一个
// "yaml 改了、结构没动"的半成品——而那正是使用者最不希望在提交前撞上的状态。
// 按这个顺序，判定失败时 yaml 一个字没改，重跑即可。
func runRestore(ctx context.Context, opts *Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)

	repo, cfgRel, err := restoreBaseline(layout)
	if err != nil {
		return err
	}

	work, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}
	if err := restorePreflight(repo, layout, work); err != nil {
		return err
	}

	headData, err := repo.HeadBlob(cfgRel)
	if err != nil {
		return restoreErr("读不到最后一次提交里的 "+layout.ConfigName(), err).
			WithHint("确认它在最后一次提交里存在：git show HEAD:" + cfgRel)
	}
	head, err := config.ParseConfig(headData, "HEAD:"+cfgRel)
	if err != nil {
		return restoreErr("最后一次提交里的 "+layout.ConfigName()+" 不是合法配置——基准坏了", err).
			WithHint(
				"还原的基准就是它，它坏了就没有可还原的目标",
				"先修一个能解析的版本提交上去，再跑 brickkit restore",
			)
	}

	changes, untouched := restorePlan(work, head)

	// 先算判定，算成功了才落盘（见上面那段"顺序是硬约束"）
	applyEnabled(work, changes)
	f, err := syncFocus(ctx, opts, layout, work)
	if err != nil {
		return err
	}
	if err := writeEnabled(layout, changes); err != nil {
		return err
	}

	printEnabledChanges(opts, layout, changes, untouched)
	return applyWorkspacePlan(opts, layout, planSync(layout, work, f))
}

// restoreBaseline 找出"最后一次提交"这个基准，没有基准就说清楚。
func restoreBaseline(layout config.Layout) (*gitrepo.Repo, string, error) {
	repo, err := gitrepo.Open(layout.Root)
	if err != nil {
		return nil, "", restoreErr("这里不是一个 git 仓库", err).
			WithHint(
				"brickkit restore 把配置还原到**最后一次提交**，没有 git 就没有这个基准",
				"想收窄范围又不想提交，就手工改回 enabled 再跑 brickkit sync",
			)
	}
	if repo.Unmerged() {
		return nil, "", clierr.New(clierr.CodeConfigConflict, "错误：正在解决冲突，先把冲突处理完").
			WithHint("brickkit restore 要读最后一次提交，冲突中的 index 读不了")
	}
	if !repo.HasHEAD() {
		return nil, "", clierr.New(clierr.CodeConfigInvalid, "错误：这个仓库还没有任何提交").
			WithHint("还原的基准是最后一次提交，一次都没有就没有可还原的目标")
	}
	cfgRel, ok := repo.Rel(layout.ConfigPath())
	if !ok {
		return nil, "", clierr.New(clierr.CodeConfigInvalid,
			"错误："+layout.ConfigName()+" 不在这个 git 仓库里").
			WithDetail("配置", layout.ConfigPath()).
			WithDetail("仓库", repo.Root())
	}
	if !repo.Tracked(cfgRel) {
		return nil, "", clierr.New(clierr.CodeConfigInvalid,
			"错误："+layout.ConfigName()+" 没有被 git 跟踪").
			WithHint("先 git add "+cfgRel+" 并提交一次，它才有可还原的基准")
	}
	return repo, cfgRel, nil
}

// restorePreflight 拦下两种"动手就会出事"的现场。
func restorePreflight(repo *gitrepo.Repo, layout config.Layout, cfg *config.Config) error {
	// ① components/ 下有已暂存的改动
	//
	// 004 §3.9.3 明说允许直接在 components/.archived/<id>/ 下改代码。如果那些改动
	// 已经 git add 过，restore 一 rename 目录，index 里那些路径就变成"删除"——
	// 提交出去等于删文件。
	if compRel, ok := repo.Rel(layout.ComponentsDir()); ok && repo.StagedUnder(compRel) {
		return clierr.New(clierr.CodeConfigConflict,
			"错误："+compRel+"/ 下有已暂存的改动，restore 会把它们悬空").
			WithHint(
				"restore 要移动源码目录，而已暂存的路径会跟着变成「删除」",
				"先把暂存区处理掉：git commit，或者 git reset "+compRel+"/",
			)
	}

	// ② 某个组件两处都有源码
	//
	// planSync 会判它"已经在该在的位置"、什么都不做。不报出来就会与提交前的闸门
	// 形成死循环：闸门拦下提交、restore 说没事可做，人却没有任何出路。
	var both []string
	for _, id := range declaredIDs(cfg) {
		if workspace.InBothPlaces(layout, id) {
			both = append(both, id)
		}
	}
	if len(both) > 0 {
		e := clierr.New(clierr.CodeConfigConflict, "错误：有组件的源码在两处都存在")
		for _, id := range both {
			e = e.WithDetail(id,
				workspace.DisplayDir(id)+"  与  "+workspace.DisplayArchivedDir(id))
		}
		return e.WithHint(
			"一个组件 ID 只能有一个源码目录（004 §8.1），restore 不知道该保留哪一份",
			"先检查两个目录里各是什么，确认无用后删除或重命名其中一份",
			"两处都有源码时，平台不替你决定保留哪一份",
		)
	}
	return nil
}

// writeEnabled 把还原结果落盘。走节点级编辑器：注释与排版原样。
func writeEnabled(layout config.Layout, changes []enabledChange) error {
	if len(changes) == 0 {
		return nil
	}
	edit, err := config.OpenEdit(layout.ConfigPath())
	if err != nil {
		return err
	}
	for _, ch := range changes {
		if ch.to == nil {
			edit.ClearComponentEnabled(ch.id, ch.version)
			continue
		}
		edit.SetComponentEnabled(ch.id, ch.version, *ch.to)
	}
	return edit.Save()
}

// printEnabledChanges 汇报 yaml 那一半改了什么。
//
// **被覆盖的旧值必须印出来。** restore 不可逆：被覆盖的 enabled 没有第二份副本，
// sync 那句"搞错了再执行一次就回来了"在这里不成立。处理办法不是加 --yes 确认
// （那两三行本来就是 004 §3.9.2 教人 git checkout 掉的东西），而是如实汇报——
// 使用者从终端 scrollback 里就能读回来。
func printEnabledChanges(
	opts *Options, layout config.Layout, changes []enabledChange, untouched []string,
) {
	if len(changes) == 0 && len(untouched) == 0 {
		opts.Printf("📄 %s 与最后一次提交一致\n", layout.ConfigName())
		return
	}
	opts.Printf("📄 %s：按最后一次提交还原 enabled（其余改动未动）\n", layout.ConfigName())
	for _, ch := range changes {
		opts.Printf("   %-26s enabled: %s → %s\n", ch.ref(), showEnabled(ch.from), toEnabled(ch.to))
	}
	for _, ref := range untouched {
		opts.Printf("   %-26s 未动（这个条目在最后一次提交里不存在）\n", ref)
	}
}

func showEnabled(v *bool) string {
	if v == nil {
		return "（没写）"
	}
	if *v {
		return "true"
	}
	return "false"
}

func toEnabled(v *bool) string {
	if v == nil {
		return "删除该字段（提交里没写）"
	}
	return showEnabled(v)
}

// restoreErr 是 restore 前置检查的统一错误壳子。
func restoreErr(message string, cause error) *clierr.Error {
	return clierr.New(clierr.CodeConfigInvalid, "错误："+message).
		WithDetail("原因", cause.Error()).
		WithCause(cause)
}
```

`internal/cli/restore.go` 的 import 补上：`github.com/brickkit/brickkit/internal/clierr`、`github.com/brickkit/brickkit/internal/config`、`github.com/brickkit/brickkit/internal/gitrepo`、`github.com/brickkit/brickkit/internal/workspace`。

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/cli/ -run TestRestore -v` 与 `go test ./internal/workspace/ -v`
Expected: PASS（`TestRestore*` 7 个 + `TestInBothPlaces` 全绿）

- [ ] **Step 7: 跑一遍全量单测确认没打坏 sync**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/cli/restore.go internal/cli/restore_test.go internal/cli/sync.go internal/workspace/workspace.go internal/workspace/workspace_test.go
git commit -m "brickkit restore：enabled 逐条还原 + 结构跟着走

顺序是硬约束：先在内存里还原 enabled、算出判定，判定成功才落盘 yaml，
最后移动目录。反过来会在 Manifest 缺失或要联网时留下「yaml 改了、结构
没动」的半成品——那正是提交前最不该撞上的状态。

只动 enabled，工作区新增的条目一个字不动：刚 add 完还没提交就跑 restore，
不能像 git checkout 那样把那次 add 一起吃掉（§3.10 批评 reset 的正是
「救配置的命令自己救不回来」）。

拦两种动手就出事的现场：components/ 下有已暂存改动（rename 会让它们悬空）、
组件两处都有源码（planSync 什么都不做，不报就与闸门死循环）。

workspace 加 InBothPlaces：Locate 活跃优先，答不出这一种。"
```

---

### Task 6: pre-commit hook 的脚本与安装

**Files:**
- Create: `internal/cli/hooks.go`
- Test: `internal/cli/hooks_test.go`

**Interfaces:**
- Consumes: `gitrepo.Repo.HooksDir()`（Task 1）
- Produces:
  - `type hookProject struct { Dir, Config string }`
  - `func renderHook(binPath, ver string, projects []hookProject) string`
  - `func parseHookProjects(script string) []hookProject`
  - `func installHook(repo *gitrepo.Repo, p hookProject, binPath, ver string) (path string, added bool, err error)`
  - `const hookMarker = "# brickkit-managed-hook"`

- [ ] **Step 1: 写失败的测试**

新建 `internal/cli/hooks_test.go`：

```go
package cli

// 本文件测 pre-commit hook 的脚本与安装。
//
// hook 是唯一能在 git commit 那个时点上真的拦住人的东西，所以它自己绝不能
// 变成新的故障源：找不到 brickkit 要放行、别人的 hook 绝不覆盖、
// 多个项目要幂等追加。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/gitrepo"
)

func TestRenderHookIsPosixShAndListsProjects(t *testing.T) {
	script := renderHook("/opt/bin/brickkit", "v1.2.3", []hookProject{
		{Dir: ".", Config: "brickkit.yaml"},
		{Dir: "apps/erp", Config: "brickkit.prod.yaml"},
	})

	assert.True(t, strings.HasPrefix(script, "#!/bin/sh\n"), "必须是 sh，Windows 上 git 用自带 sh 跑 hook")
	assert.Contains(t, script, hookMarker+" v1.2.3")
	assert.Contains(t, script, "/opt/bin/brickkit")
	assert.Contains(t, script, ".|brickkit.yaml")
	assert.Contains(t, script, "apps/erp|brickkit.prod.yaml")
	assert.NotContains(t, script, "[[", "不用任何 bash 特性")
	assert.NotContains(t, script, "function ")
}

func TestRenderedHookRunsAndBlocks(t *testing.T) {
	dir := t.TempDir()
	// 假的 brickkit：--check 一律非零退出
	fake := filepath.Join(dir, "fake-brickkit")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\nexit 1\n"), 0o755))

	script := filepath.Join(dir, "pre-commit")
	require.NoError(t, os.WriteFile(script,
		[]byte(renderHook(fake, "v0", []hookProject{{Dir: ".", Config: "brickkit.yaml"}})), 0o755))

	cmd := exec.Command("/bin/sh", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	assert.Error(t, err, "brickkit restore --check 非零时 hook 必须非零：%s", out)
}

func TestRenderedHookPassesWhenBinaryMissing(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "pre-commit")
	require.NoError(t, os.WriteFile(script,
		[]byte(renderHook(filepath.Join(dir, "does-not-exist"), "v0",
			[]hookProject{{Dir: ".", Config: "brickkit.yaml"}})), 0o755))

	cmd := exec.Command("/bin/sh", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH=/nonexistent")
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err, "找不到 brickkit 必须放行，否则新人 clone 下来就提交不了：%s", out)
	assert.Contains(t, string(out), "找不到 brickkit")
}

func TestRenderedHookSkipsMissingProjectDir(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-brickkit")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	script := filepath.Join(dir, "pre-commit")
	require.NoError(t, os.WriteFile(script,
		[]byte(renderHook(fake, "v0", []hookProject{{Dir: "gone", Config: "brickkit.yaml"}})), 0o755))

	cmd := exec.Command("/bin/sh", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err, "项目目录已经不在了，不该因此堵死提交：%s", out)
}

func TestParseHookProjectsRoundTrips(t *testing.T) {
	want := []hookProject{
		{Dir: ".", Config: "brickkit.yaml"},
		{Dir: "apps/erp", Config: "brickkit.prod.yaml"},
	}
	assert.Equal(t, want, parseHookProjects(renderHook("/x/brickkit", "v1", want)))
}

func TestInstallHookWritesExecutableAndIsIdempotent(t *testing.T) {
	dir := newTestRepo(t)
	repo, err := gitrepo.Open(dir)
	require.NoError(t, err)

	path, added, err := installHook(repo, hookProject{".", "brickkit.yaml"}, "/x/brickkit", "v1")
	require.NoError(t, err)
	assert.True(t, added)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o100, "hook 必须可执行")

	_, added, err = installHook(repo, hookProject{".", "brickkit.yaml"}, "/x/brickkit", "v1")
	require.NoError(t, err)
	assert.False(t, added, "同一个项目重复安装不该重复加")

	_, added, err = installHook(repo, hookProject{"apps/erp", "brickkit.yaml"}, "/x/brickkit", "v1")
	require.NoError(t, err)
	assert.True(t, added)

	script, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Len(t, parseHookProjects(string(script)), 2, "第二个项目要追加，不是覆盖")
}

func TestInstallHookNeverOverwritesForeignHook(t *testing.T) {
	dir := newTestRepo(t)
	repo, err := gitrepo.Open(dir)
	require.NoError(t, err)
	hooks, err := repo.HooksDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(hooks, 0o755))
	foreign := filepath.Join(hooks, "pre-commit")
	require.NoError(t, os.WriteFile(foreign, []byte("#!/bin/sh\n# husky\nexit 0\n"), 0o755))

	_, _, err = installHook(repo, hookProject{".", "brickkit.yaml"}, "/x/brickkit", "v1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "已存在")
	assert.Contains(t, err.Error(), "restore --check", "要把该插进去的那一行告诉他")

	got, readErr := os.ReadFile(foreign)
	require.NoError(t, readErr)
	assert.Contains(t, string(got), "# husky", "别人的 hook 一个字都不能改")
}

// newTestRepo 建一个空的 git 仓库。
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v：%s", args, out)
	}
	return dir
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run "Hook" -v`
Expected: FAIL，`undefined: renderHook`

- [ ] **Step 3: 写实现**

新建 `internal/cli/hooks.go`：

```go
package cli

// 本文件写 pre-commit hook：它是唯一能在 git commit 那个时点上真的拦住人的
// 东西（004 §3.14）。
//
// 因此它自己绝不能变成新的故障源。三条底线：
//
//	找不到 brickkit        放行。否则新人 clone 下来第一件事就是提交不了，
//	                      而原因跟他要做的事毫无关系
//	别人的 pre-commit      绝不覆盖。报错，并把该插进去的那一行告诉他
//	项目目录已经不在了      跳过。一条过期的路径不该堵死整个仓库的提交

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/gitrepo"
)

// hookMarker 是"这个 hook 是 brickkit 写的"的标记。认它才敢覆盖。
const hookMarker = "# brickkit-managed-hook"

// hookListStart / hookListEnd 圈出脚本里的项目清单，升级时按它们把清单读回来。
const (
	hookListStart = "BRICKKIT_PROJECTS"
	hookListEnd   = "BRICKKIT_PROJECTS"
)

// hookProject 是 hook 要检查的一个项目。
type hookProject struct {
	// Dir 是项目根相对仓库根的路径（以 / 分隔；仓库根本身是 "."）。
	Dir string
	// Config 是该项目的配置文件名（--config 可以改名）。
	Config string
}

// renderHook 生成 pre-commit 脚本。
//
// 全程 POSIX sh，不用任何 bash 特性：Windows 上 Git for Windows 用自带的 sh
// 跑 hook，一句 [[ ]] 就会让它在那儿直接崩掉。
//
// 清单用 quoted here-doc 喂给 while 循环，而不是 for + 变量展开：
// 后者会按空格切词，带空格的路径就散了。here-doc 喂给当前 shell 的循环，
// 循环里设的 rc 也就带得出来（管道会把循环丢进子 shell，rc 出不来）。
func renderHook(binPath, ver string, projects []hookProject) string {
	var list strings.Builder
	for _, p := range projects {
		list.WriteString(p.Dir + "|" + p.Config + "\n")
	}
	return `#!/bin/sh
` + hookMarker + ` ` + ver + `
# 由 brickkit init --hooks 写入。可安全覆盖升级；想卸载就删掉这个文件。
#
# 它拦一件事：组件源码提交在 components/.archived/ 里，而 brickkit.yaml 说它该启动。
# 判据与出路见 brickkit restore --check。
BRICKKIT_BIN='` + binPath + `'
[ -x "$BRICKKIT_BIN" ] || BRICKKIT_BIN=$(command -v brickkit 2>/dev/null)
if [ -z "$BRICKKIT_BIN" ]; then
	echo "⚠️  找不到 brickkit，跳过组件结构检查（brickkit init --hooks 可重装本 hook）" >&2
	exit 0
fi
rc=0
while IFS='|' read -r dir cfg; do
	[ -n "$dir" ] || continue
	[ -d "$dir" ] || continue
	( cd "$dir" && "$BRICKKIT_BIN" restore --check --config "$cfg" ) || rc=1
done <<'` + hookListStart + `'
` + list.String() + hookListEnd + `
exit $rc
`
}

// parseHookProjects 从脚本里把项目清单读回来（升级时要保住别的项目）。
func parseHookProjects(script string) []hookProject {
	_, rest, ok := strings.Cut(script, "<<'"+hookListStart+"'\n")
	if !ok {
		return nil
	}
	body, _, ok := strings.Cut(rest, "\n"+hookListEnd+"\n")
	if !ok {
		return nil
	}
	var projects []hookProject
	for _, line := range strings.Split(body, "\n") {
		dir, cfg, ok := strings.Cut(line, "|")
		if !ok || dir == "" {
			continue
		}
		projects = append(projects, hookProject{Dir: dir, Config: cfg})
	}
	return projects
}

// installHook 把 pre-commit hook 装进仓库，幂等追加项目。
//
// added 报告这次是不是真加了一个新项目，用于输出措辞（"已装上" / "已经装过了"）。
func installHook(
	repo *gitrepo.Repo, p hookProject, binPath, ver string,
) (string, bool, error) {
	hooks, err := repo.HooksDir()
	if err != nil {
		return "", false, clierr.New(clierr.CodeInternal, "错误：定位 hooks 目录失败").
			WithDetail("原因", err.Error()).WithCause(err)
	}
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		return "", false, clierr.New(clierr.CodeInternal, "错误：创建 hooks 目录失败").
			WithDetail("目录", hooks).
			WithDetail("原因", err.Error()).WithCause(err)
	}
	path := filepath.Join(hooks, "pre-commit")

	// 清单必须在写之前读回来：写完再读就只能读到自己刚写的那一份
	before := len(parseHookProjectsFile(path))
	projects := []hookProject{p}
	if existing, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(existing), hookMarker) {
			return "", false, foreignHookError(path, p)
		}
		projects = mergeHookProjects(parseHookProjects(string(existing)), p)
	}
	added := len(projects) != before

	if err := os.WriteFile(path, []byte(renderHook(binPath, ver, projects)), 0o755); err != nil {
		return "", false, clierr.New(clierr.CodeInternal, "错误：写入 pre-commit hook 失败").
			WithDetail("文件", path).
			WithDetail("原因", err.Error()).WithCause(err)
	}
	return path, added, nil
}

// parseHookProjectsFile 读文件里的项目清单；读不到就当空的。
func parseHookProjectsFile(path string) []hookProject {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseHookProjects(string(data))
}

// mergeHookProjects 把一个项目并进清单，按 Dir 去重（重复安装不重复加）。
func mergeHookProjects(existing []hookProject, p hookProject) []hookProject {
	for i, e := range existing {
		if e.Dir == p.Dir {
			// 同一个项目重装：配置文件名可能变了，跟最新的走
			existing[i] = p
			return existing
		}
	}
	return append(existing, p)
}

// foreignHookError 是"这儿已经有别人的 hook 了"该说的那句话。
//
// 绝不覆盖：那可能是 husky、lefthook，或者他自己写的十几行。平台没有立场
// 替他决定丢掉它——所以把该插进去的那一行给他，让他自己放。
func foreignHookError(path string, p hookProject) error {
	line := "brickkit restore --check --config " + p.Config + " || exit 1"
	if p.Dir != "." {
		line = "( cd " + p.Dir + " && brickkit restore --check --config " + p.Config + " ) || exit 1"
	}
	return clierr.New(clierr.CodeConfigConflict, "错误：pre-commit hook 已存在，不是 brickkit 写的").
		WithDetail("文件", path).
		WithDetail("要插进去的那一行", line).
		WithHint(
			"平台绝不覆盖你自己的 hook——它可能是 husky / lefthook，也可能是你写的",
			"把上面那一行加进你的 pre-commit 即可",
			"确认那个文件没用了，也可以删掉它再跑 brickkit init --hooks",
		)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -run "Hook" -v`
Expected: PASS（7 个测试全绿，含真的用 /bin/sh 跑一遍生成的脚本）

- [ ] **Step 5: 提交**

```bash
git add internal/cli/hooks.go internal/cli/hooks_test.go
git commit -m "pre-commit hook 的脚本与安装

hook 是唯一能在 git commit 那个时点上真的拦住人的东西，所以它自己绝不能
变成新的故障源。三条底线都有测试：找不到 brickkit 就放行（否则新人 clone
下来第一件事就是提交不了）、别人的 pre-commit 绝不覆盖（把该插进去的那一行
告诉他）、项目目录不在了就跳过。

全程 POSIX sh：Windows 上 git 用自带 sh 跑 hook，一句 [[ ]] 就崩。
清单用 quoted here-doc 喂给 while 循环——for + 变量展开会按空格切词，
带空格的路径就散了；管道会把循环丢进子 shell，rc 出不来。"
```

---

### Task 7: `brickkit init --hooks` 与 init 集成

**Files:**
- Modify: `internal/cli/init.go`
- Test: `internal/cli/init_test.go`

**Interfaces:**
- Consumes: `installHook` / `hookProject`（Task 6）、`gitrepo.Open`（Task 1）、`version.Version`
- Produces: `func installCommitHook(opts *Options, layout config.Layout, explicit bool) error`

- [ ] **Step 1: 写失败的测试**

追加到 `internal/cli/init_test.go`：

```go
func TestInitInstallsHookWhenProjectIsRepoRoot(t *testing.T) {
	dir := newTestRepo(t)

	r := runIn(t, dir, "init", "my-erp")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	assert.FileExists(t, hook)
	assert.Contains(t, r.stdout, "pre-commit")

	script, err := os.ReadFile(hook)
	require.NoError(t, err)
	assert.Contains(t, string(script), hookMarker)
}

func TestInitSkipsHookOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()

	r := runIn(t, dir, "init", "my-erp")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "brickkit init --hooks",
		"装不上要提示怎么补装——init 常常跑在 git init 之前")
}

// init 完全可能跑在一个跟本项目无关的仓库的子目录里（本仓库的
// 试用指南/playground/ 就是）。那时自动装等于往别人的 .git/hooks 里写东西。
func TestInitDoesNotWriteHookIntoAnUnrelatedRepo(t *testing.T) {
	repo := newTestRepo(t)
	nested := filepath.Join(repo, "sub", "project")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	r := runIn(t, nested, "init", "my-erp")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.NoFileExists(t, filepath.Join(repo, ".git", "hooks", "pre-commit"),
		"嵌套项目要装 hook，得自己显式说一句")
	assert.Contains(t, r.stdout, "brickkit init --hooks")
}

func TestInitHooksOnlyInstallsIntoExistingProject(t *testing.T) {
	repo := newTestRepo(t)
	nested := filepath.Join(repo, "sub", "project")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.Equal(t, clierr.ExitOK, runIn(t, nested, "init", "my-erp").code)

	r := runIn(t, nested, "init", "--hooks")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	require.FileExists(t, hook)
	script, err := os.ReadFile(hook)
	require.NoError(t, err)
	assert.Contains(t, string(script), "sub/project|brickkit.yaml",
		"清单里记的是项目根相对仓库根的路径")
}

func TestInitHooksOnlyRejectsProjectName(t *testing.T) {
	r := runIn(t, newTestRepo(t), "init", "--hooks", "my-erp")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "不需要项目名称")
}

func TestInitHooksOnlyFailsLoudlyOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, clierr.ExitOK, runIn(t, dir, "init", "my-erp").code)

	r := runIn(t, dir, "init", "--hooks")
	assert.Equal(t, clierr.ExitError, r.code, "显式要求装就得装上，装不上是错误")
	assert.Contains(t, r.stderr, "git")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run "TestInit" -v`
Expected: FAIL（`unknown flag: --hooks`；hook 文件不存在）

- [ ] **Step 3: 写实现**

`internal/cli/init.go`：

命令定义里加旗标与分支——

```go
func newInitCommand(opts *Options) *cobra.Command {
	var noSkills bool
	var hooksOnly bool
	cmd := &cobra.Command{
		// ...（Use / Short / GroupID / Long 保持原样，Long 末尾加一段见下）
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if hooksOnly {
				if len(args) > 0 {
					return clierr.New(clierr.CodeInvalidArgument,
						"brickkit init --hooks 只安装 pre-commit hook，不需要项目名称").
						WithExit(clierr.ExitUsage)
				}
				return installCommitHook(opts, config.NewLayout(opts.WorkDir, opts.ConfigPath), true)
			}
			if len(args) == 0 {
				return clierr.New(clierr.CodeInvalidArgument, "请指定项目名称：brickkit init <项目名称>").
					WithExit(clierr.ExitUsage)
			}
			return runInit(opts, args[0], noSkills)
		},
	}
	cmd.Flags().BoolVar(&noSkills, "no-skills", false,
		"不装 AI 助手技能（.claude/skills/、AGENTS.md）")
	cmd.Flags().BoolVar(&hooksOnly, "hooks", false,
		"只安装提交前检查用的 pre-commit hook（在已有项目里补装）")
	return cmd
}
```

`Long` 末尾追加：

```
组件源码要跟项目一起进 Git 的话，还会装一个 pre-commit hook，
拦住"源码归档了、而 brickkit.yaml 没跟着提交"这个失误（004 §3.14）。
它只在项目根就是仓库根时自动装——嵌套在别人仓库里的项目，
用 brickkit init --hooks 显式补装。
```

`Example` 追加一行：

```
  brickkit init --hooks                                  只补装 pre-commit hook
```

`runInit` 里，`installSkills` 之后、`opts.Printf("\n")` 之前插入：

```go
	if err := installCommitHook(opts, layout, false); err != nil {
		return err
	}
```

文件末尾追加：

```go
// installCommitHook 装提交前检查用的 pre-commit hook（004 §3.14）。
//
// # 为什么 init 顺带装时要多一条"项目根 == 仓库根"
//
// init 完全可能跑在一个**跟本项目无关的仓库**的子目录里——本仓库的
// 试用指南/playground/ 就是这样（它在 brickKit 自己的仓库里）。那时候
// 自动装等于往别人的 .git/hooks 里写东西，而那个人根本没要求过。
//
// 所以顺带装只服务最常见的那一种：项目根就是仓库根。嵌套的项目要装，
// 就自己显式说一句 brickkit init --hooks——那时 explicit 为真，装不上是错误。
func installCommitHook(opts *Options, layout config.Layout, explicit bool) error {
	repo, err := gitrepo.Open(layout.Root)
	if err != nil {
		if explicit {
			return clierr.New(clierr.CodeConfigInvalid, "错误：这里不是一个 git 仓库").
				WithHint(
					"pre-commit hook 只能装进 git 仓库",
					"先 git init，再执行 brickkit init --hooks",
				)
		}
		hookHint(opts)
		return nil
	}

	rel, ok := repo.Rel(layout.Root)
	if !ok {
		if explicit {
			return clierr.New(clierr.CodeConfigInvalid, "错误：项目根不在这个 git 仓库里").
				WithDetail("项目", layout.Root).
				WithDetail("仓库", repo.Root())
		}
		hookHint(opts)
		return nil
	}
	if !explicit && rel != "." {
		// 项目嵌在别人的仓库里：不替他决定往那个仓库写东西
		hookHint(opts)
		return nil
	}

	path, added, err := installHook(repo,
		hookProject{Dir: rel, Config: layout.ConfigName()}, brickkitBinPath(), version.Version)
	if err != nil {
		return err
	}

	display := path
	if p, ok := repo.Rel(path); ok {
		display = p
	}
	if !added && explicit {
		opts.Printf("✅ pre-commit hook 已经装过了：%s\n", display)
		return nil
	}
	opts.Printf("   🪝 %-21s%s\n", display, "提交前检查组件结构（004 §3.14）")
	return nil
}

// hookHint 在没自动装上时说清怎么补装。
//
// 装不上是**常态而不是错误**：init 常常跑在 git init 之前，项目也常常嵌在
// 别人的仓库里。但不说一声，使用者就永远不知道有这道闸门。
func hookHint(opts *Options) {
	opts.Printf("   💡 %s\n",
		"组件源码要跟项目一起进 Git 的话：brickkit init --hooks 装上提交前检查")
}

// brickkitBinPath 返回当前可执行文件的绝对路径，取不到就回落到裸名字。
//
// 把绝对路径写进 hook 是必要的：GUI 客户端（VS Code 的源代码管理面板、
// macOS 上从 Finder 启动的客户端）的 PATH 常常不含 ~/.local/bin，
// 只写 "brickkit" 会让 hook 在那些地方一律走"找不到就放行"那一支。
func brickkitBinPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "brickkit"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}
```

`internal/cli/init.go` 的 import 补上：`os`、`path/filepath`、`github.com/brickkit/brickkit/internal/gitrepo`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -run "TestInit" -v`
Expected: PASS（新增 6 个测试绿，原有 init 测试不变）

- [ ] **Step 5: 跑全量单测**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/cli/init.go internal/cli/init_test.go
git commit -m "brickkit init --hooks：装提交前检查的 hook

init 顺带装时多一条硬规则：只在「项目根 == 仓库根」时装。init 完全可能
跑在一个跟本项目无关的仓库的子目录里——本仓库的 试用指南/playground/
就是——那时自动装等于往别人的 .git/hooks 里写东西。嵌套项目要装就自己
显式说一句，那时装不上是错误。

hook 里写的是当前可执行文件的绝对路径：VS Code 源代码管理面板、macOS 上
从 Finder 启动的客户端，PATH 常常不含 ~/.local/bin。只写裸名字会让 hook
在那些地方一律走「找不到就放行」那一支，等于从不生效。"
```

---

### Task 8: 文档

**Files:**
- Modify: `design/004-CLI 设计.md`（§3.1 命令表、§3.9 补前提、§3.10 写清区别、新增 §3.14、§8.2 补一段）
- Modify: `AI-CONTEXT.md`、`llms.txt`、`README.md`
- Modify: `试用指南/01-初始化项目.md`

**Interfaces:**
- Consumes: Task 4–7 落地的命令与旗标（`scripts/check-cli-docs.py` 会验文档里写的命令真的存在）
- Produces: 无代码接口

- [ ] **Step 1: §3.1 命令表加一行**

在 `| `brickkit sync` |` 那一行之后插入：

```markdown
| `brickkit restore` | 还原配置与源码结构 | 把 `enabled` 与源码结构还原到最后一次提交。`--check` 供 pre-commit hook 判断这次提交自洽不自洽 |
```

- [ ] **Step 2: §3.9 补上那个前提**

在 §3.9 现有那两段「不包括 `git status`」的引用块之后，追加一段：

```markdown
> **但那个前提使用者会去掉。** 上面两段都建立在「`components/` 在 `.gitignore` 里」
> 之上。真实项目会把它去掉——组件源码要跟项目一起进版本库，或者想在项目里留一份
> 指针。那种项目里 `sync` 的整目录移动**会**进项目的 diff，于是「本地关掉几个顶层、
> `sync` 归档、干完活忘了还原就提交」这件事会反复发生。
> 提交时点上的闸门与还原命令见 §3.14。
```

- [ ] **Step 3: §3.10 写清与 `restore` 的区别**

在 §3.10 末尾（`配置改坏了：git checkout brickkit.yaml...` 那一段之后）追加：

```markdown
**这与 §3.14 的 `brickkit restore` 不冲突，两者差在哪：**

| | 已删除的 `reset` | `restore`（§3.14） |
| --- | --- | --- |
| 基准从哪来 | CLI 自建的 `.initial` / `.last` 备份 | **git 的最后一次提交**——不自建任何备份 |
| 覆盖多少 | 整个配置文件 | 只有 `enabled` 一个字段，逐条改 |
| 未提交的 `add` | 一起抹掉 | **一个字不动** |
| 自己可逆吗 | 不可逆，且覆盖前的状态没有任何地方留着 | 被覆盖的旧值动手前全部印出来 |

§3.10 反对的是「自建一套不如 git 的备份机制」。`restore` 恰恰是**用** git 当基准，
它做的是 git 做不到的那一半：把源码目录结构跟着配置一起还原。
```

- [ ] **Step 4: 新增 §3.14**

在 §3.13（`brickkit logout`）之后、`## 4. 依赖解析引擎` 之前插入一节。内容从
spec 的 §4、§5、§6 转写（判据的 3 × 4 状态表、放行清单、输出样例、hook 安装规则），
并保留这几处必须逐字一致的输出块（`scripts/check-guide-output.py` 会核对设计书里的
输出样例）：

```markdown
### 3.14 brickkit restore

**用途：** 把 `brickkit.yaml` 里各组件的 `enabled` 还原成最后一次提交的值，
再让组件源码结构跟着走；`--check` 供 pre-commit hook 判断这次提交自洽不自洽。

（正文照 spec 的 §4 / §5 / §6 转写：判据读 index 而不是 HEAD 的理由、
3 × 4 状态表、「两处都有」为什么独立且优先、放行清单、restore 的逐条规则、
执行顺序、hook 的安装规则与三条底线。）
```

**这一节必须写全，不能只留一句「见 spec」**：`design/` 是「CLI 开发者、高级用户」
的核心读物，spec 在 `docs/superpowers/` 下不是给使用者读的。

- [ ] **Step 5: §8.2 补「去掉之后会怎样」**

在 §8.2 的「为什么 `components/` 不提交到项目仓库？」列表之后追加：

```markdown
**去掉这一行之后会怎样。** 上面是建议，不是禁令——组件源码要跟项目一起进版本库
是真实需求。去掉之后有两件事跟着变：

1. `brickkit sync` 移动目录**会**进项目的 diff。「忘了还原结构就提交」由此反复
   发生，提交时点上的闸门与还原命令见 §3.14
2. `--repo` clone 下来的组件自带 `.git`，被项目仓库跟踪时是一条 `160000` 的
   **gitlink**。仓库里没有 `.gitmodules`，队友 clone 下来只会得到一个空目录，
   `git submodule update` 也拉不回来——没有地方记着它的 URL。
   `brickkit restore --check` 会在提交里出现 gitlink 时附带一句提醒（不影响退出码）
```

- [ ] **Step 6: 更新 AI-CONTEXT.md / llms.txt / README.md**

三份里的命令清单各加一行 `brickkit restore`，措辞与 §3.1 那一行一致。
`AI-CONTEXT.md` 里 `sync` 的段落后面补一句指向 `restore`。

- [ ] **Step 7: 试用指南 01 的预期输出跟上**

`试用指南/01-初始化项目.md` 的「✅ 预期」块里，在 `📁 AGENTS.md` 那一行之后加：

```
   💡 组件源码要跟项目一起进 Git 的话：brickkit init --hooks 装上提交前检查
```

（playground 在 brickKit 自己的仓库里、且不是仓库根，所以走的是提示那一支——
这正是 Task 7 那条「不往别人的仓库写 hook」规则在真实场景里的样子。）

- [ ] **Step 8: 跑文档检查与全量测试**

Run: `make lint && make test-unit`
Expected: PASS。特别确认 `check-cli-docs`（文档写的命令真的存在）、
`check-docs`（§3.14 这个引用指向真实存在的小节）、`check-guide-output`
（指南块的每一行都在真实输出里）三项全绿。

- [ ] **Step 9: 提交**

```bash
git add design/ AI-CONTEXT.md llms.txt README.md 试用指南/
git commit -m "文档：restore 与提交前闸门

004 §3.9 补上那个被去掉的前提——两段「git status 看不见它们」都建立在
components/ 在 .gitignore 里之上，而真实项目会去掉它。

§3.10 加一张对照表说清 restore 与被删掉的 reset 差在哪：基准来自 git 而不是
自建备份、只动 enabled 一个字段、未提交的 add 一个字不动、旧值动手前全印出来。
§3.10 反对的是自建一套不如 git 的备份，restore 恰恰是用 git 当基准。

§8.2 保留「不提交 components/」的建议，但写明去掉之后会怎样，包括没有
.gitmodules 的 gitlink 是个死记录。"
```

---

## Self-Review

**1. Spec coverage**

| spec 小节 | 落在哪个 Task |
| --- | --- |
| §4.1 读哪两份东西 | Task 1（`IndexBlob` / `IndexEntries`）、Task 4（接线） |
| §4.2 3 × 4 状态表 | Task 3（逐格测试）、Task 4（端到端） |
| §4.3 两处都有独立、双向、优先 | Task 3（`judgeCommit`）、Task 4（`violationError` 优先分支）、Task 5（`restorePreflight`） |
| §4.4 短路与放行清单 | Task 4（`hasArchivedEntry` / `skipCheck` / 冲突 / 未跟踪 / 仓库外）、Task 6（找不到二进制） |
| §4.5 被拦时的输出 | Task 4（`violationError`） |
| §4.6 gitlink 附带提醒 | Task 3（`gitlinks` 收集）、Task 4（`warnGitlinks`） |
| §5.1 yaml 逐条规则 | Task 2（Edit 方法）、Task 5（`restorePlan`） |
| §5.2 结构部分复用 sync | Task 5（`applyWorkspacePlan` + `planSync`） |
| §5.3 执行顺序与幂等 | Task 5（`runRestore` 顺序 + `TestRestoreIsIdempotent`） |
| §5.4 前置条件 | Task 5（`restoreBaseline` / `restorePreflight`） |
| §5.5 输出与旧值汇报 | Task 5（`printEnabledChanges`） |
| §6.1 hooks 目录定位 | Task 1（`HooksDir`，含 `core.hooksPath` 测试） |
| §6.2 标记行与不覆盖 | Task 6（`hookMarker` / `foreignHookError`） |
| §6.3 多项目幂等追加 | Task 6（`mergeHookProjects` / `parseHookProjects`） |
| §6.4 找不到 brickkit | Task 6（脚本回落 + 真跑 sh 的测试）、Task 7（写绝对路径） |
| §6.5 init 集成与仓库根规则 | Task 7 |
| §7 已知边界 | Task 8（文档写明） |
| §8 与既有口径的关系 | Task 8（§3.9 / §3.10 / §8.2） |
| §9 实现落点 | 与 File Structure 一致 |
| §10 测试要点 | Task 1/3/5/6/7 的测试步骤 |

无缺口。

**2. Placeholder scan**

Task 8 的 §3.14 那一步是唯一带「照 spec 转写」字样的地方。它不是占位符：转写的
源头（spec 的 §4/§5/§6）已经写全，且那一步明确写了「必须写全，不能只留一句见 spec」
以及为什么。其余步骤都带可直接落地的代码或原文。

**3. Type consistency**

- `focus.keep` 是 `map[string]bool`（sync.go 现有），`judgeCommit` 的 `running` 参数同类型 ✅
- `declaredIDs(cfg *config.Config) []string` 在 Task 4 定义，Task 5 的 `restorePreflight` 使用 ✅
- `hookProject{Dir, Config}` 在 Task 6 定义，Task 7 构造时用的是同样两个字段 ✅
- `installHook` 返回 `(string, bool, error)`，Task 7 按 `path, added, err` 接 ✅
- `enabledChange{id, version, from, to}` 只在 Task 5 内部使用，`restorePlan` 的返回类型与测试一致 ✅
- `workspace.InBothPlaces(l config.Layout, componentID string) bool` 在 Task 5 定义并在同一 Task 使用 ✅
- Task 4 先给 `runRestore` 一个 `return nil` 占位、Task 5 替换成真实现——签名 `func runRestore(ctx context.Context, opts *Options) error` 两处一致 ✅
