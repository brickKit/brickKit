// Package archguard 守住两条边界：
//
//   - 旧的单文件模型（internal/config、internal/override）已经删除，任何代码都不能再把它引回来。
//   - 主模块从不 import market-server。市场模块在主模块之下、借 replace 导入主模块的
//     internal/ 包（同一份 Manifest 规则）；方向只有这一个——反过来，市场的依赖
//     （aws、pq）就进了 CLI。
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
	checked := eachImport(t, root, "", func(path, imported string) {
		for _, dir := range removed {
			assert.NotEqual(t, module+dir, imported, "%s 还在 import 已删除的 %s", path, dir)
		}
	})
	assert.Greater(t, checked, 100, "一个 Go 文件都没检查到，遍历坏了")
}

func TestMainModuleNeverImportsMarketServer(t *testing.T) {
	root := repoRoot(t)
	market := filepath.Join(root, "market-server")
	checked := eachImport(t, root, market, func(path, imported string) {
		assert.False(t, strings.HasPrefix(imported, module+"market-server"),
			"%s import 了市场模块 %s：依赖方向只能是市场 → 主模块", path, imported)
	})
	assert.Greater(t, checked, 100, "一个 Go 文件都没检查到，遍历坏了")
}

// eachImport 对 root 下每个当前的 Go 文件的每个 import 调一次 fn，返回检查过的文件数。
// skip 非空时整个目录跳过。
func eachImport(t *testing.T, root, skip string, fn func(path, imported string)) int {
	t.Helper()
	fset := token.NewFileSet()
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// 隐藏目录（.git、.claude 里的旧工作树）与归档不是当前代码
			if path == skip || path != root && (strings.HasPrefix(name, ".") || name == "archive" || name == "node_modules") {
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
			fn(path, imported)
		}
		return nil
	})
	require.NoError(t, err)
	return checked
}
