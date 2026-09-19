// 本文件是 brickkit new 的业务行为测试：默认路径、--path、--contract、
// 已存在目录、非法 ID。
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 默认不带 --path：写到 components/<scope>/<name>/component.yaml，
// 内容能通过 Parse + Validate。
func TestNewDefaultPath(t *testing.T) {
	r := run(t, "new", "demo/widget")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "✅ 已生成组件骨架：demo/widget")
	assert.Contains(t, r.stdout, filepath.Join("components", "demo", "widget", "component.yaml"))
}

// --path 写到指定目录，且不再套 <scope>/<name> 这一层——目录本身就是仓库根。
func TestNewWithPath(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "new", "demo/widget", "--path", "somewhere/else")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	data, err := os.ReadFile(filepath.Join(dir, "somewhere", "else", manifest.FileName))
	require.NoError(t, err)
	m, err := manifest.Parse(data, "")
	require.NoError(t, err)
	require.NoError(t, m.Validate())
	assert.Equal(t, "demo/widget", m.Metadata.ID)

	_, err = os.Stat(filepath.Join(dir, "components"))
	assert.True(t, os.IsNotExist(err), "--path 指定后不该再额外建 components/ 目录")
}

// --contract openapi 顺带生成一份占位契约文件，并写进 artifacts。
func TestNewWithOpenAPIContract(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "new", "demo/widget", "--contract", "openapi")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, filepath.Join("components", "demo", "widget", "api", "openapi.yaml"))

	componentDir := filepath.Join(dir, "components", "demo", "widget")
	data, err := os.ReadFile(filepath.Join(componentDir, manifest.FileName))
	require.NoError(t, err)
	m, err := manifest.Parse(data, "")
	require.NoError(t, err)
	require.Len(t, m.Artifacts, 1)
	assert.Equal(t, []string{"api/openapi.yaml"}, m.Artifacts[0].Files)

	_, err = os.Stat(filepath.Join(componentDir, "api", "openapi.yaml"))
	require.NoError(t, err, "契约占位文件必须真的被写出来，不能只在 Manifest 里声明")
}

// 目标目录已存在时拒绝，不覆盖使用者已有的东西。
func TestNewRefusesExistingDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "components", "demo", "widget"), 0o755))

	r := runIn(t, dir, "new", "demo/widget")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "❌ 错误：目标目录已存在")
}

// 非法组件 ID 直接拒绝，退出码为用法错误。
func TestNewRejectsInvalidID(t *testing.T) {
	r := run(t, "new", "NotValid")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "组件 ID 不合法")
}

// 非法 --contract 取值直接拒绝。
func TestNewRejectsInvalidContract(t *testing.T) {
	r := run(t, "new", "demo/widget", "--contract", "grpc-web")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "--contract 取值不合法")
}
