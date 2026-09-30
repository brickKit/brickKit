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

// 报错里的文件路径按使用者所在的目录显示：在项目根就是 deploy.local.yaml，在组件目录里是
// ../../../deploy.local.yaml——绝不是一长串绝对路径。
func TestErrorPathsAreShownFromWhereYouStand(t *testing.T) {
	dir := focusFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "local", "on")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	editFile(t, in(dir, "deploy.local.yaml"), "target: docker", "target: nowhere")

	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	assert.Contains(t, r.stderr, i18n.T(msgid.DetailLine, i18n.T(msgid.LabelFile), "deploy.local.yaml")+"\n")
	assert.NotContains(t, r.stderr, dir)

	r = runWithEngine(t, newFakeEngine(), in(dir, "components", "erp", "portal"), "status")
	assert.Contains(t, r.stderr, i18n.T(msgid.DetailLine, i18n.T(msgid.LabelFile), "../../../deploy.local.yaml")+"\n")
	assert.NotContains(t, r.stderr, dir)
}

func TestRelativizeOnlyTouchesPathsUnderTheProject(t *testing.T) {
	o := &Options{WorkDir: "/w/my-shop", CallDir: "/w/my-shop/components/demo/caller"}
	assert.Equal(t, "File: ../../../deploy.yaml", o.relativize("File: /w/my-shop/deploy.yaml"))
	assert.Equal(t, "in . (my-shop)", o.relativize("in /w/my-shop/components/demo/caller (my-shop)"))
	assert.Equal(t, "../../..", o.relativize("/w/my-shop"))
	assert.Equal(t, "/w/my-shop2/deploy.yaml", o.relativize("/w/my-shop2/deploy.yaml"), "a sibling directory is not the project")
	assert.Equal(t, "/app/caller migrate", o.relativize("/app/caller migrate"))
	assert.Equal(t, "见 ../../../deploy.yaml：", o.relativize("见 /w/my-shop/deploy.yaml："))
}

// 守卫：命令层渲染报错只经过 render / shown（shown.go 与 root.go 的总出口）。直接 .Format() 的
// 新写法会绕过路径改写，把绝对路径又摆回使用者面前。
func TestErrorsAreRenderedOnlyThroughShown(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "shown.go" {
			continue
		}
		data, err := os.ReadFile(f)
		require.NoError(t, err)
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, ").Format()") || strings.Contains(line, ".Format())") ||
				(strings.Contains(line, "clierr.Render(") && !strings.Contains(line, "opts.shown(")) {
				t.Errorf("%s:%d renders an error without opts.render / opts.shown: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
