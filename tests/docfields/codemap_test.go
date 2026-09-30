package docfields_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AGENTS 的代码地图（§10）要让本地开发的 AI 从功能直达代码：internal/ 与 cmd/ 下每个包都在两份地图里，
// 地图里写的每个路径都真实存在。包加了、删了、挪了，地图不跟着改，这里就失败。
var codeMaps = map[string]string{"AGENTS.md": "## §10 Code map", "AGENTS.zh.md": "## §10 代码地图"}

var codePath = regexp.MustCompile("`((?:internal|cmd|tools|scripts|tests|market-server|schemas|\\.githooks|llms)/[^`]*|install\\.sh)`")

func codeMapSection(text, heading string) (string, bool) {
	i := strings.Index(text, "\n"+heading+"\n")
	if i < 0 {
		return "", false
	}
	rest := text[i+len(heading)+2:]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	return rest, true
}

func missingPackages(section string, pkgs []string) []string {
	var out []string
	for _, p := range pkgs {
		if !strings.Contains(section, "`"+p+"/`") {
			out = append(out, p)
		}
	}
	return out
}

func codeMapPaths(section string) []string {
	var out []string
	for _, m := range codePath.FindAllStringSubmatch(section, -1) {
		out = append(out, m[1])
	}
	return out
}

// packageDirs 是 internal/ 与 cmd/ 下每一个有非测试 Go 源文件的目录——嵌套的包（msgidgen、projecttest……）也算。
func packageDirs(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, top), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(repoRoot, filepath.Dir(p))
			rel = filepath.ToSlash(rel)
			if !seen[rel] {
				seen[rel] = true
				out = append(out, rel)
			}
			return nil
		})
		require.NoError(t, err)
	}
	return out
}

func TestCodeMapCoversEveryPackage(t *testing.T) {
	pkgs := packageDirs(t)
	for file, heading := range codeMaps {
		text, err := os.ReadFile(filepath.Join(repoRoot, file))
		require.NoError(t, err)
		section, ok := codeMapSection(string(text), heading)
		require.True(t, ok, "%s has no %q section", file, heading)
		assert.Empty(t, missingPackages(section, pkgs), "%s: packages missing from the code map", file)
		for _, p := range codeMapPaths(section) {
			if strings.Contains(p, "<") { // internal/cli/<command>.go 是写法说明，不是一个路径
				continue
			}
			if strings.ContainsAny(p, "*?") {
				matches, _ := filepath.Glob(filepath.Join(repoRoot, filepath.FromSlash(p)))
				assert.NotEmpty(t, matches, "%s: %s matches nothing", file, p)
				continue
			}
			_, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(strings.TrimSuffix(p, "/"))))
			assert.NoError(t, err, "%s: %s does not exist", file, p)
		}
	}
}

// 检测器自己要会失败：少一个包、写一个不存在的路径，都要被认出来。
func TestCodeMapGuardCatchesDrift(t *testing.T) {
	section := "| `internal/cli/` | x |\n| `internal/gone/` | y |\n"
	assert.Equal(t, []string{"internal/cascade"}, missingPackages(section, []string{"internal/cli", "internal/cascade"}))
	assert.Equal(t, []string{"internal/cli/", "internal/gone/"}, codeMapPaths(section))
	_, ok := codeMapSection("## §9 x\nbody\n", "## §10 Code map")
	assert.False(t, ok)
}
