package manifest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/agentsmd"
	"github.com/brickkit/brickkit/internal/i18n"
)

func scaffoldByPath(t *testing.T, id string, opts ScaffoldOptions) ([]string, map[string]string) {
	t.Helper()
	files, err := Scaffold(id, opts)
	require.NoError(t, err)
	var paths []string
	byPath := map[string]string{}
	for _, f := range files {
		paths = append(paths, f.Path)
		byPath[f.Path] = string(f.Content)
	}
	return paths, byPath
}

func TestScaffoldWritesTheDocSet(t *testing.T) {
	prev := i18n.Current()
	defer i18n.SetCurrent(prev)
	i18n.SetCurrent(i18n.EN)

	paths, byPath := scaffoldByPath(t, "demo/quote", ScaffoldOptions{Contract: ContractOpenAPI})
	assert.Equal(t, []string{"component.yaml", "BRICKKIT.md", "AGENTS.md", "CLAUDE.md", "README.md", "api/openapi.yaml"}, paths)

	doc := byPath["BRICKKIT.md"]
	for _, h := range []string{"## Purpose", "## Before you deploy", "## Dependencies", "## Configuration", "## Contracts", "## Shell declaration"} {
		assert.Contains(t, doc, h)
	}
	assert.Contains(t, doc, "`api/openapi.yaml`")
	assert.NotContains(t, doc, "](", "BRICKKIT.md is read alone in the cache: no relative links")
	assert.Contains(t, doc, "<!-- TODO:")

	agents := byPath["AGENTS.md"]
	assert.True(t, strings.HasPrefix(agents, "# demo/quote\n"))
	b, err := agentsmd.Find(agents)
	require.NoError(t, err)
	assert.Equal(t, "en", b.Lang)
	assert.Contains(t, agents, "## Code map")
	assert.Equal(t, "@AGENTS.md\n", byPath["CLAUDE.md"])

	readme := byPath["README.md"]
	for _, h := range []string{"## Use it in a project", "## Documentation", "## Development"} {
		assert.Contains(t, readme, h)
	}
	assert.Contains(t, readme, "brickkit add demo/quote@0.1.0")
	assert.Contains(t, readme, "[BRICKKIT.md](BRICKKIT.md)")
	assert.Contains(t, readme, "[api/openapi.yaml](api/openapi.yaml)")
	assert.Contains(t, byPath["component.yaml"], "# repository:")
}

func TestScaffoldDocsFollowTheCLILanguage(t *testing.T) {
	prev := i18n.Current()
	defer i18n.SetCurrent(prev)
	i18n.SetCurrent(i18n.ZH)

	_, byPath := scaffoldByPath(t, "demo/quote", ScaffoldOptions{})
	assert.Contains(t, byPath["BRICKKIT.md"], "## 部署前准备")
	assert.Contains(t, byPath["AGENTS.md"], "## 代码地图")
	assert.Contains(t, byPath["AGENTS.md"], "lang=zh")
	assert.Contains(t, byPath["README.md"], "## 在项目里使用")
}

func TestScaffoldShellDeclaresMembers(t *testing.T) {
	_, byPath := scaffoldByPath(t, "erp/shell", ScaffoldOptions{Shell: true})
	assert.Contains(t, byPath["BRICKKIT.md"], scaffoldPlaceholderMember)
}
