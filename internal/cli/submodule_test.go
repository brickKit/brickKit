package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// releaseWithSubmodule 发布 demo/app@1.0.0：它的仓库把另一个仓库挂成 components/demo/lib 这个 submodule。
func releaseWithSubmodule(t *testing.T, g *gitOrgProject) {
	t.Helper()
	git := func(dir string, args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "protocol.file.allow=always"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	lib := filepath.Join(g.org, "lib-src")
	app := filepath.Join(g.org, "demo-app")
	for _, d := range []string{lib, app} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	git(lib, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(lib, "lib.go"), []byte("package lib\n"), 0o644))
	git(lib, "add", ".")
	git(lib, "commit", "-qm", "lib")
	git(app, "init", "-q")
	c := comp{ID: "demo/app", Version: "1.0.0", Image: "-"}
	require.NoError(t, os.WriteFile(filepath.Join(app, "component.yaml"), []byte(c.yamlText()), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(app, "Dockerfile"), []byte("FROM scratch\n"), 0o644))
	git(app, "submodule", "add", "-q", lib, "components/demo/lib")
	git(app, "add", ".")
	git(app, "commit", "-qm", "app")
	git(app, "tag", "1.0.0")
}

func TestAddRepoNotesUnfetchedSubmodules(t *testing.T) {
	g := newGitOrgProject(t)
	releaseWithSubmodule(t, g)
	dir := g.project()
	r := g.mustRun(dir, "add", "demo/app@1.0.0", "--repo")
	assert.Contains(t, r.stdout, i18n.T(msgid.CliAddSubmodulesNotFetched, "demo/app@1.0.0", "components/demo/lib"))
}

func TestBuildWarnsAboutEmptySubmoduleDirectories(t *testing.T) {
	g := newGitOrgProject(t)
	releaseWithSubmodule(t, g)
	dir := g.project()
	g.mustRun(dir, "add", "demo/app@1.0.0")
	r := g.mustRun(dir, "build", "demo/app", "--force")
	assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.CliBuildSubmodulesEmpty, "demo/app@1.0.0"))
	assert.Contains(t, r.stdout+r.stderr, "components/demo/lib")
}

// submodule 没拉是平台刻意不做的事，不是配置写错：警告有它自己的码（与 MIGRATION_SKIPPED 同类），
// 脚本才分得清"该修配置"与"该发布镜像"。
func TestEmptySubmoduleWarningHasItsOwnCode(t *testing.T) {
	w := submodulesEmptyWarning("demo/lib@1.0.0", []string{"third_party/sdk"})
	assert.True(t, w.Warning)
	assert.Equal(t, clierr.CodeSubmodulesSkipped, w.Code)
}
