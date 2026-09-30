package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitIn 在 dir 里跑 git，不读使用者的全局配置；允许 file:// 的 submodule。
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "protocol.file.allow=always"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// 导出的源码树（不是 git 仓库）里也读得出 .gitmodules 登记的路径。
func TestSubmodulePathsInAnExportedTree(t *testing.T) {
	root := t.TempDir()
	lib, app, out := filepath.Join(root, "lib"), filepath.Join(root, "app"), filepath.Join(root, "out")
	for _, d := range []string{lib, app, out} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	gitIn(t, lib, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(lib, "lib.go"), []byte("package lib\n"), 0o644))
	gitIn(t, lib, "add", ".")
	gitIn(t, lib, "commit", "-qm", "lib")
	gitIn(t, app, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(app, "component.yaml"), []byte("x: 1\n"), 0o644))
	gitIn(t, app, "submodule", "add", "-q", lib, "components/demo/lib")
	gitIn(t, app, "add", ".")
	gitIn(t, app, "commit", "-qm", "app")
	archive := exec.Command("sh", "-c", "git archive --format=tar HEAD | tar -x -C "+out)
	archive.Dir = app
	b, err := archive.CombinedOutput()
	require.NoError(t, err, string(b))

	assert.Equal(t, []string{"components/demo/lib"}, SubmodulePathsIn(out))
	assert.Nil(t, SubmodulePathsIn(t.TempDir()))
}
