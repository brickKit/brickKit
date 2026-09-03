package cli

// 本文件测的是**提交前判据的纯逻辑**：不碰 git，也不碰磁盘。
//
// 判据是要硬拦人的东西，漏一格就是有人提交不了、或者提交错了。所以
// 「判定结果 × index 里源码在哪」这张 3 × 4 的表逐格都有一个测试。

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
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

// ============================================================
// 以下是 brickkit restore --check 的接线测试：真的 git 仓库、真的 sync。
// ============================================================

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

// Fix 1（复审第二条）：index 里源码还是归档的，暂存的 yaml 说该启动，
// 但磁盘上源码已经被 brickkit sync 激活——只是这次目录移动没有暂存。
//
// 这是「两处都有」那个死锁的同类：judgeCommit 只看 index，判成 violationArchived，
// 而 restore 的三道前置检查都通不过（StagedUnder 看不到已暂存的 components/
// 改动，InBothPlaces 看磁盘也只找到一处），restorePlan 也算不出 enabled 差异，
// planSync 一看磁盘已经是对的、什么都不做——「brickkit restore」与
// 「git add brickkit.yaml」两条路都已经走过，唯一没人说出口的是
// 「git add -A components/」。
func TestCheckArchivedInIndexActiveOnDiskNamesGitAddDashA(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	// 归档 hello（caller 级联跟着走），把这份归档结构连同 enabled: false
	// 一起提交下来——这是「归档在 git 里」的合法起点（004 §3.9.3）。
	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "archive")

	// 使用者决定重新启用：改 yaml，跑 sync 把磁盘上的源码激活……
	f.writeConfig(t, allEnabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	f.assertActive(t, "demo/hello")
	f.assertActive(t, "demo/caller")

	// ……但只暂存了 yaml，没暂存这次目录移动：index 里源码仍是归档的。
	gitDo(t, f.Dir, "add", "brickkit.yaml")

	r := runIn(t, f.Dir, "restore", "--check")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "demo/hello")
	assert.Contains(t, r.stderr, "git add -A components/",
		"出路是暂存已经发生的那次目录移动，不是 git add brickkit.yaml 或 brickkit restore——"+
			"这两条路使用者都已经走过、也走不通")
}

func TestCheckSkipsDuringMergeConflict(t *testing.T) {
	f := newSyncFixture(t, helloDisabled, "demo/hello")
	// 先把 demo/hello 归档好，再进 git：短路排到了冲突判断前面之后，
	// 「即将提交的结构里有没有归档路径」必须先为真，测试才走得到冲突那一支——
	// 否则重排后的短路会在 Unmerged 之前就把它放行掉。
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
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

// 审查发现：「配置未被 git 跟踪」原来静默 return nil，与「四种情形放行+警告」
// 的约束矛盾——一个 brickkit.yaml 还没 git add 过的人，根本不知道闸门没跑。
func TestCheckWarnsWhenConfigNotTracked(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)

	// 归档结构已暂存，但 brickkit.yaml 从来没有 git add 过——闸门从这里开始
	// 才算真的启动，短路挡不住它，必须走到"配置未跟踪"这一支并出声。
	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	gitDo(t, f.Dir, "add", filepath.Join("components", ".archived"))

	r := runIn(t, f.Dir, "restore", "--check")
	assert.Equal(t, clierr.ExitOK, r.code, "配置没交给 git，管不着，但必须放行：%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stdout, "跳过")
	assert.Contains(t, r.stdout, "brickkit.yaml")
	assert.Contains(t, r.stdout, "未被 git 跟踪", "必须是走到了配置未跟踪这一支，不是撞在别的放行分支上")
}

// helloDisabledWithUnresolvable 语法上是合法配置（能过 config.ParseConfig），
// 但 solo/thing@9.9.9 在本地安装源里根本不存在——用来在不碰网络的前提下，
// 制造一次"全图解不出来"的失败（对应真实场景里的网络错误 / Manifest 缺失）。
const helloDisabledWithUnresolvable = `components:
  - id: demo/hello
    version: 1.0.0
    enabled: false
  - id: demo/caller
    version: 1.0.0
  - id: solo/thing
    version: 9.9.9
resources: []
`

// 审查要求补的第二条：四条放行路径里最要命的一条——「全图算不出来」——
// 之前零测试。一次网络错误或 Manifest 缺失如果能堵死提交，这道闸门就是灾难。
func TestCheckWarnsWhenGraphResolutionFails(t *testing.T) {
	f := newSyncFixture(t, allEnabled, "demo/hello", "demo/caller")
	gitProject(t, f.Dir)
	gitDo(t, f.Dir, "add", "-A")
	gitDo(t, f.Dir, "commit", "--quiet", "-m", "init")

	// 先用能正常解析的配置真的 sync 一次，产出真实的归档结构，并把 yaml
	// 一起暂存——不然会先撞在"配置未跟踪"那一支，测不到这里要测的东西。
	f.writeConfig(t, helloDisabled)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "sync").code)
	gitDo(t, f.Dir, "add", "-A")

	// 再把暂存的 yaml 换成引用不存在版本的配置：语法仍合法，但 resolver
	// 解不出图。
	f.writeConfig(t, helloDisabledWithUnresolvable)
	gitDo(t, f.Dir, "add", "brickkit.yaml")

	r := runIn(t, f.Dir, "restore", "--check")
	assert.Equal(t, clierr.ExitOK, r.code, "算不出来 ≠ 判据不通过，必须放行：%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stdout, "跳过")
	assert.Contains(t, r.stdout, "算不出这次会启动哪些组件",
		"必须是走到了 syncFocus 失败这一支，不是撞在别的放行分支上")
}
