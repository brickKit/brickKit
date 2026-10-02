package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

func TestDocsListsEveryPage(t *testing.T) {
	r := runIn(t, t.TempDir(), "docs")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "documentation (en)")
	assert.Contains(t, r.stdout, "04-shell/02-json-injection")
	assert.Contains(t, r.stdout, "Config as JSON", "each page with its title")
}

// 页 ID、抄来的路径、目录名都认；页里的链接相对这一页，拼出来的 ../ 路径也认。
func TestDocsPrintsAPage(t *testing.T) {
	for _, arg := range []string{"04-shell/02-json-injection", "docs/en/04-shell/02-json-injection.md", "04-shell/../04-shell/02-json-injection.md"} {
		r := runIn(t, t.TempDir(), "docs", arg)
		require.Equal(t, clierr.ExitOK, r.code, r.stderr)
		assert.True(t, strings.HasPrefix(r.stdout, "# Config as JSON\n"), arg)
	}
	r := runIn(t, t.TempDir(), "docs", "04-shell", "--lang", "zh")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.True(t, strings.HasPrefix(r.stdout, "# "))
	assert.Contains(t, r.stdout, "外壳")
}

func TestDocsUnknownPageAndLanguage(t *testing.T) {
	r := runIn(t, t.TempDir(), "docs", "04-shell/02-json-injectin")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "there is no page 04-shell/02-json-injectin")
	assert.Contains(t, r.stderr, "04-shell/02-json-injection", "did you mean")

	r = runIn(t, t.TempDir(), "docs", "--lang", "fr")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "en, zh")
}

// 报错建议指到文档时，给的是 CLI 里能直接打开的那一页。
func TestFieldReferenceHintNamesADocsPage(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	appendTo(t, f.Dir+"/deploy.yaml", "taget: docker\n")
	r := runIn(t, f.Dir, "lint")
	assert.Contains(t, r.stdout, "brickkit docs 11-reference/03-deploy-yaml-schema")
	assert.Contains(t, r.stdout, "/docs/en/11-reference/03-deploy-yaml-schema.md")
}
