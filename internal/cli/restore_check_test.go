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
