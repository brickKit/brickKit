// Package projecttest 在测试里用几行 YAML 搭一个三层项目并装载它。
package projecttest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/project"
)

// Files 是 相对路径 → 内容。
type Files map[string]string

// Write 把文件写进 root。
func Write(t testing.TB, root string, files Files) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}

// Load 在临时目录里写好文件并装载，失败直接让测试失败。
func Load(t testing.TB, files Files) *project.Project {
	t.Helper()
	root := t.TempDir()
	Write(t, root, files)
	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	return p
}
