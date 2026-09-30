package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 在项目的子目录里跑项目命令：命令作用于项目，第一行说明用的是哪个项目。
func TestProjectCommandFromSubdirectoryUsesTheProject(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	r := runWithEngine(t, newFakeEngine(), filepath.Join(dir, "config"), "status")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, i18n.T(msgid.CliProjectFoundAbove, "..", "shop"))
}

// 在项目根目录跑：不打印那一行（没有向上找）。
func TestProjectCommandAtTheRootSaysNothingExtra(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	r := runWithEngine(t, newFakeEngine(), dir, "status")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "📁")
}

// 只作用于当前目录的命令不向上找：release 读的是当前目录的 component.yaml。
func TestReleaseDoesNotLookUpward(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	r := runIn(t, filepath.Join(dir, "config"), "release")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.NotContains(t, r.stdout, "📁")
}

// new --path 的相对路径相对使用者所在的目录，不是项目根。
func TestNewPathIsRelativeToWhereYouAre(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	sub := filepath.Join(dir, "config")
	r := runIn(t, sub, "new", "demo/x", "--path", "x")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.FileExists(t, filepath.Join(sub, "x", "component.yaml"))
}

// 子目录里显示的路径相对使用者所在的目录，像 git 一样。
func TestPathsAreShownRelativeToWhereYouAre(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	r := runWithEngine(t, newFakeEngine(), filepath.Join(dir, "config"), "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, filepath.Join("..", ".brickkit", "generated"))
}
