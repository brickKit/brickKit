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

// 候选按已敲的前缀过滤；@ 之后给的是项目里声明的版本。
func TestCompleteFiltersByWhatWasTyped(t *testing.T) {
	dir := focusFixture(t)
	assert.Equal(t, []string{"erp/worker"}, candidates(t, runIn(t, dir, "__complete", "remove", "erp/w")))
	assert.Equal(t, []string{"erp/api@1.0.0"}, candidates(t, runIn(t, dir, "__complete", "deps", "erp/api@")))
}

// --focus 只要组件 ID。
func TestCompleteFocus(t *testing.T) {
	dir := focusFixture(t)
	got := candidates(t, runIn(t, dir, "__complete", "up", "--focus", ""))
	assert.Subset(t, got, []string{"erp/api", "erp/portal"})
	for _, c := range got {
		assert.NotContains(t, c, "@")
	}
}

// graph 自己注册 -f（它不接 --no-local）：-f 的候选照样是项目根下的部署文件。
func TestCompleteGraphDeployFile(t *testing.T) {
	dir := focusFixture(t)
	got := candidates(t, runIn(t, dir, "__complete", "graph", "-f", ""))
	assert.Contains(t, got, "deploy.yaml")
}
