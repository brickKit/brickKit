package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
)

// depsProject：portal → api → db、portal → auth → db（db 是共享的菱形底），admin → auth，
// api 还弱依赖一个项目里没有的 infra/cache。
func depsProject(t *testing.T) (*gitOrgProject, string) {
	t.Helper()
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/db", Version: "1.0.0"})
	g.release(comp{ID: "erp/auth", Version: "1.0.0", Requires: []string{"erp/db@1.0.0"}})
	g.release(comp{ID: "erp/api", Version: "1.0.0", Requires: []string{"erp/db@1.0.0"}, Optional: []string{"infra/cache@1.0.0"}})
	g.release(comp{ID: "erp/portal", Version: "1.0.0", Requires: []string{"erp/api@1.0.0", "erp/auth@1.0.0"}})
	g.release(comp{ID: "erp/admin", Version: "1.0.0", Requires: []string{"erp/auth@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/portal@1.0.0")
	g.mustRun(dir, "add", "erp/admin@1.0.0")
	return g, dir
}

func TestDepsProjectTrees(t *testing.T) {
	g, dir := depsProject(t)
	r := g.mustRun(dir, "deps")
	want := "erp/portal@1.0.0\n" +
		"├── erp/api@1.0.0\n" +
		"│   ├── erp/db@1.0.0\n" +
		"│   └── infra/cache@1.0.0 (optional, not installed)\n" +
		"└── erp/auth@1.0.0\n" +
		"    └── erp/db@1.0.0 (shown above)\n" +
		"\n" +
		"erp/admin@1.0.0\n" +
		"└── erp/auth@1.0.0 (shown above)\n"
	assert.Equal(t, want, r.stdout)
}

func TestDepsMarksOptionalAndMissing(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "infra/cache", Version: "1.0.0"})
	g.release(comp{ID: "erp/api", Version: "1.0.0", Optional: []string{"infra/cache@1.0.0", "infra/mq@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")
	g.mustRun(dir, "add", "infra/cache@1.0.0")

	r := g.mustRun(dir, "deps")
	assert.Contains(t, r.stdout, "├── infra/cache@1.0.0 (optional)\n")
	assert.Contains(t, r.stdout, "└── infra/mq@1.0.0 (optional, not installed)\n")
	assert.NotContains(t, r.stdout, "\ninfra/cache@1.0.0\n", "被依赖的组件不单独成树")
}

// deps <id>：它自己那棵树，外加谁直接依赖它。
func TestDepsSingleComponentWithRequiredBy(t *testing.T) {
	g, dir := depsProject(t)
	r := g.mustRun(dir, "deps", "erp/auth")
	assert.Equal(t, "erp/auth@1.0.0\n└── erp/db@1.0.0\n\nRequired by: erp/admin@1.0.0, erp/portal@1.0.0\n", r.stdout)

	r = g.mustRun(dir, "deps", "erp/portal@1.0.0")
	assert.Contains(t, r.stdout, "Required by: nothing (top-level)")
}

// 没写版本时，这个 ID 在项目里的每个版本各一棵树。
func TestDepsAllVersionsOfID(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/db", Version: "1.0.0"})
	g.release(comp{ID: "erp/db", Version: "2.0.0"})
	g.release(comp{ID: "erp/old", Version: "1.0.0", Requires: []string{"erp/db@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/db@2.0.0")
	g.mustRun(dir, "add", "erp/old@1.0.0")

	r := g.mustRun(dir, "deps", "erp/db")
	assert.Equal(t, 2, strings.Count(r.stdout, "Required by:"))
	assert.Contains(t, r.stdout, "erp/db@1.0.0\n\nRequired by: erp/old@1.0.0\n")
	assert.Contains(t, r.stdout, "erp/db@2.0.0\n\nRequired by: nothing (top-level)\n")
}

func TestDepsUnknownComponent(t *testing.T) {
	g, dir := depsProject(t)
	r := g.run(dir, "deps", "crm/web")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "crm/web is not in this project")
	assert.Contains(t, r.stderr, "brickkit add crm/web")

	r = g.run(dir, "deps", "erp/auth@9.9.9")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "erp/auth@9.9.9 is not in this project")
}

func TestDepsEmptyProject(t *testing.T) {
	g := newGitOrgProject(t)
	r := g.mustRun(g.project(), "deps")
	require.Contains(t, r.stdout, "no components yet")
}

// 弱依赖环：两者都被依赖，没有顶层——照样各画出来，环上标 (cycle)、不无限展开。
func TestDepsWeakCycle(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/a", Version: "1.0.0", Optional: []string{"erp/b@1.0.0"}})
	g.release(comp{ID: "erp/b", Version: "1.0.0", Optional: []string{"erp/a@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/a@1.0.0")
	g.mustRun(dir, "add", "erp/b@1.0.0")

	r := g.mustRun(dir, "deps")
	assert.Contains(t, r.stdout, "(optional, cycle)")
	var roots []string
	for _, line := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "└") && !strings.HasPrefix(line, "├") {
			roots = append(roots, line)
		}
	}
	assert.Len(t, roots, 1, "先画的那个把另一个也展开了，不再单独成树：%s", r.stdout)
}

func TestDepsRejectsBadReference(t *testing.T) {
	g, dir := depsProject(t)
	r := g.run(dir, "deps", "erp/auth@^1.0.0")
	assert.NotEqual(t, clierr.ExitOK, r.code)
}

// 注记的括号与分隔符跟着语言走：中文输出里是"（弱依赖，未安装）"，不是半角的"(弱依赖, 未安装)"。
func TestDepsNotesUseTheLanguagesPunctuation(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0", Optional: []string{"infra/mq@1.0.0"}})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0")

	t.Setenv("BRICKKIT_LANG", "zh")
	t.Cleanup(func() { i18n.SetCurrent(i18n.EN) })
	r := g.mustRun(dir, "deps")
	assert.Contains(t, r.stdout, "└── infra/mq@1.0.0（弱依赖，未安装）\n")
}
