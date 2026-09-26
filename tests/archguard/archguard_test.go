// Package archguard 守住三层文件重构的一条边界：旧的单文件模型（internal/config、
// internal/override）已经删除，任何代码都不能再把它引回来。
//
// 这条要单独写成测试，而不是指望编译报错：包被删掉之后，有人照着旧文档或旧分支
// 把目录恢复出来再 import，编译是能过的——那时两套模型并存，命令读哪一套全凭运气。
package archguard

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// removed 是已经删除、不许再出现的包。
var removed = []string{"internal/config", "internal/override"}

const module = "github.com/brickkit/brickkit/"

// repoRoot 是仓库根（本文件在 tests/archguard/ 下）。
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return root
}

func TestRemovedPackagesStayDeleted(t *testing.T) {
	root := repoRoot(t)
	for _, dir := range removed {
		_, err := os.Stat(filepath.Join(root, dir))
		assert.True(t, os.IsNotExist(err), "%s 已随三层文件重构删除，不能再出现", dir)
	}
}

func TestNoOldModelImports(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// 隐藏目录（.git、.claude 里的旧工作树）与归档不是当前代码
			if path != root && (strings.HasPrefix(name, ".") || name == "archive" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		checked++
		for _, spec := range file.Imports {
			imported, _ := strconv.Unquote(spec.Path.Value)
			for _, dir := range removed {
				assert.NotEqual(t, module+dir, imported, "%s 还在 import 已删除的 %s", path, dir)
			}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Greater(t, checked, 100, "一个 Go 文件都没检查到，遍历坏了")
}
