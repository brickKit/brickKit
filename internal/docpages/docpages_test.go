package docpages

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/version"
)

// 代码里指到的每一页，两种语言都要有：报错建议照着它让人去读。
func TestReferencedPagesExist(t *testing.T) {
	for _, id := range Referenced {
		for _, lang := range Langs() {
			assert.True(t, Exists(lang, id), "%s/%s", lang, id)
		}
	}
}

// 消息目录、技能、AGENTS.md 模板里写的 brickkit docs <页>，都要指到真有的页（两种语言）。
func TestEveryMentionedPageExists(t *testing.T) {
	root := filepath.Join("..", "..")
	mention := regexp.MustCompile("brickkit docs ([0-9][0-9]-[A-Za-z0-9./-]*[A-Za-z0-9]|README)")
	var files []string
	for _, glob := range []string{"internal/i18n/locales/*.yaml", "internal/skills/assets/*/claude/skills/*/SKILL.md", "internal/agentsmd/templates/*/*.md", "AGENTS.md", "AGENTS.zh.md"} {
		m, err := filepath.Glob(filepath.Join(root, glob))
		require.NoError(t, err)
		files = append(files, m...)
	}
	for _, lang := range Langs() {
		require.NoError(t, filepath.WalkDir(filepath.Join(root, "docs", lang), func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
				files = append(files, p)
			}
			return err
		}))
	}
	require.NotEmpty(t, files)
	found := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, m := range mention.FindAllStringSubmatch(string(data), -1) {
			found++
			for _, lang := range Langs() {
				assert.True(t, Exists(lang, m[1]), "%s names %q, which %s doesn't have", f, m[1], lang)
			}
		}
	}
	assert.NotZero(t, found, "the scan must see the hints it guards")
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"04-shell/02-json-injection":                   "04-shell/02-json-injection",
		"04-shell/02-json-injection.md":                "04-shell/02-json-injection",
		"docs/en/04-shell/02-json-injection.md":        "04-shell/02-json-injection",
		"docs/zh/04-shell/02-json-injection.md#anchor": "04-shell/02-json-injection",
		"04-shell":           "04-shell",
		"04-shell/":          "04-shell",
		"04-shell/README.md": "04-shell",
		"04-shell/../06-architecture/09-error-codes.md": "06-architecture/09-error-codes",
		"":       "README",
		"README": "README",
	} {
		assert.Equal(t, want, Normalize(in), in)
	}
}

func TestListAndRead(t *testing.T) {
	pages := List("en")
	require.NotEmpty(t, pages)
	assert.Equal(t, len(pages), len(List("zh")), "the two trees mirror each other")
	ids := map[string]string{}
	for _, p := range pages {
		ids[p.ID] = p.Title
		assert.NotEmpty(t, p.Title, p.ID)
	}
	assert.Contains(t, ids, "README")
	assert.Contains(t, ids, "04-shell", "a directory's README is named by the directory")
	assert.Contains(t, ids, "04-shell/02-json-injection")

	body, err := Read("zh", "04-shell")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(body, "# "))
	_, err = Read("en", "04-shell/nope")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = Read("fr", "README")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestURL(t *testing.T) {
	old := version.Version
	defer func() { version.Version = old }()
	version.Version = "v1.1.0"
	assert.Equal(t, Repository+"/blob/v1.1.0/docs/en/04-shell/README.md", URL("en", "04-shell"), "a release points at its own tag")
	version.Version = "v1.1.0-3-gabc1234-dirty"
	assert.Equal(t, Repository+"/blob/main/docs/zh/04-shell/README.md", URL("zh", "04-shell"), "a dev build points at main")
	assert.Equal(t, Repository+"/blob/main/docs/en/04-shell/02-json-injection.md", URL("en", "04-shell/02-json-injection"))
}
