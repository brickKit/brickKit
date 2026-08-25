// 本文件是 Step 9 中 brickkit.yaml 原地编辑器的单元测试。
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), DefaultConfigFile)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// 骨架里的 `components: []` 是流式空序列，加入条目后必须切回块式。
func TestAddComponentToEmptySkeleton(t *testing.T) {
	path := writeTemp(t, string(Skeleton("my-erp", DefaultConfigFile)))

	edit, err := OpenEdit(path)
	require.NoError(t, err)
	assert.True(t, edit.AddComponent("people/basic", "1.0.0"))
	require.NoError(t, edit.Save())

	out := read(t, path)
	assert.Contains(t, out, "components:\n  - id: people/basic\n    version: 1.0.0\n")
	assert.NotContains(t, out, "components: [")

	cfg, err := ParseConfigFile(path)
	require.NoError(t, err)
	require.Len(t, cfg.Components, 1)
	assert.Equal(t, "people/basic", cfg.Components[0].ID)
	assert.Equal(t, "1.0.0", cfg.Components[0].Version)
	assert.Nil(t, cfg.Components[0].Enabled, "add 写进来的组件不带 enabled 字段")
}

// 注释、字段顺序与 ${ENV_VAR} 都要原样保留。
func TestEditPreservesCommentsAndEnvRefs(t *testing.T) {
	path := writeTemp(t, `# 顶部注释
project: my-erp

deploy:
  target: docker   # 部署目标

sources:
  # 市场源
  - id: market
    type: market
    url: https://market.example.com/api/v1
    authToken: ${MARKET_TOKEN}

components:
  - id: department/tree   # 先加的
    version: 1.0.0

resources:
  - kind: database
    engine: postgresql
    id: pg
    host: localhost
    port: 5432
    password: ${POSTGRES_PASSWORD}
`)

	edit, err := OpenEdit(path)
	require.NoError(t, err)
	require.True(t, edit.AddComponent("people/basic", "1.0.0"))
	require.NoError(t, edit.Save())

	out := read(t, path)
	assert.Contains(t, out, "# 顶部注释")
	assert.Contains(t, out, "# 部署目标")
	assert.Contains(t, out, "# 市场源")
	assert.Contains(t, out, "# 先加的")
	assert.Contains(t, out, "${MARKET_TOKEN}")
	assert.Contains(t, out, "${POSTGRES_PASSWORD}")
	assert.Contains(t, out, "people/basic")
}

func TestAddComponentIsIdempotent(t *testing.T) {
	path := writeTemp(t, string(Skeleton("my-erp", DefaultConfigFile)))

	edit, err := OpenEdit(path)
	require.NoError(t, err)
	assert.True(t, edit.AddComponent("people/basic", "1.0.0"))
	assert.False(t, edit.AddComponent("people/basic", "1.0.0"), "同 ID 同版本不重复写入")
	assert.True(t, edit.AddComponent("people/basic", "2.0.0"), "同 ID 不同版本是新的条目")
	assert.True(t, edit.HasComponent("people/basic", "2.0.0"))
	assert.False(t, edit.HasComponent("people/basic", "3.0.0"))
	require.NoError(t, edit.Save())

	cfg, err := ParseConfigFile(path)
	require.NoError(t, err)
	assert.Len(t, cfg.Components, 2)
}

func TestRemoveComponent(t *testing.T) {
	path := writeTemp(t, `project: my-erp
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
  - id: people/basic
    version: 2.0.0
    enabled: false      # 用户手写的钉住/禁用意图
  - id: department/tree
    version: 1.0.0
`)

	edit, err := OpenEdit(path)
	require.NoError(t, err)
	assert.True(t, edit.RemoveComponent("people/basic", "1.0.0"))
	assert.False(t, edit.RemoveComponent("people/basic", "9.9.9"))
	require.NoError(t, edit.Save())

	out := read(t, path)
	assert.Contains(t, out, "# 用户手写的钉住/禁用意图", "其他条目的注释不受影响")

	cfg, err := ParseConfigFile(path)
	require.NoError(t, err)
	require.Len(t, cfg.Components, 2)
	assert.Equal(t, "2.0.0", cfg.Components[0].Version)
	assert.True(t, cfg.Components[0].IsDisabled())
}

// 删空之后仍是合法配置（空列表，不是 null）。
func TestRemoveLastComponent(t *testing.T) {
	path := writeTemp(t, `project: my-erp
deploy:
  target: docker
components:
  - id: people/basic
    version: 1.0.0
`)

	edit, err := OpenEdit(path)
	require.NoError(t, err)
	require.True(t, edit.RemoveComponent("people/basic", "1.0.0"))
	require.NoError(t, edit.Save())

	cfg, err := ParseConfigFile(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.Components)
}

// 配置里根本没有 components 键时自动补一个。
func TestAddComponentCreatesMissingKey(t *testing.T) {
	path := writeTemp(t, "project: my-erp\ndeploy:\n  target: docker\n")

	edit, err := OpenEdit(path)
	require.NoError(t, err)
	require.True(t, edit.AddComponent("people/basic", "1.0.0"))
	require.NoError(t, edit.Save())

	cfg, err := ParseConfigFile(path)
	require.NoError(t, err)
	require.Len(t, cfg.Components, 1)
}

// `components:` 写成 null 时等价于空列表。
func TestAddComponentToNullComponents(t *testing.T) {
	path := writeTemp(t, "project: my-erp\ndeploy:\n  target: docker\ncomponents:\n")

	edit, err := OpenEdit(path)
	require.NoError(t, err)
	assert.False(t, edit.HasComponent("people/basic", "1.0.0"))
	assert.False(t, edit.RemoveComponent("people/basic", "1.0.0"))
	require.True(t, edit.AddComponent("people/basic", "1.0.0"))
	require.NoError(t, edit.Save())

	cfg, err := ParseConfigFile(path)
	require.NoError(t, err)
	require.Len(t, cfg.Components, 1)
}

// 序列里混入非映射条目（用户写错）时跳过，不 panic。
func TestFindComponentIgnoresNonMappingEntries(t *testing.T) {
	path := writeTemp(t, `project: my-erp
deploy:
  target: docker
components:
  - 这是一个写错的标量条目
  - id: people/basic
    version: 1.0.0
`)

	edit, err := OpenEdit(path)
	require.NoError(t, err)
	assert.True(t, edit.HasComponent("people/basic", "1.0.0"))
	assert.True(t, edit.RemoveComponent("people/basic", "1.0.0"))
}

// ============================================================
// 错误路径
// ============================================================

func TestOpenEditMissingFile(t *testing.T) {
	_, err := OpenEdit(filepath.Join(t.TempDir(), "nope.yaml"))
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeProjectMissing, e.Code)
	assert.Contains(t, e.Format(), "brickkit init")
}

func TestOpenEditInvalidYAML(t *testing.T) {
	path := writeTemp(t, "project: [未闭合\n")

	_, err := OpenEdit(path)
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
}

func TestOpenEditNonMappingRoot(t *testing.T) {
	for _, content := range []string{"- 顶层是数组\n", ""} {
		_, err := OpenEdit(writeTemp(t, content))
		require.Error(t, err, "内容：%q", content)
		assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	}
}

func TestOpenEditUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时权限位不生效")
	}
	path := writeTemp(t, "project: my-erp\n")
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err := OpenEdit(path)
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "读取配置失败")
}

func TestSaveWriteFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultConfigFile)
	require.NoError(t, os.WriteFile(path, []byte("project: my-erp\n"), 0o644))

	edit, err := OpenEdit(path)
	require.NoError(t, err)
	require.True(t, edit.AddComponent("people/basic", "1.0.0"))

	// 用目录占住配置文件的位置
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.MkdirAll(path, 0o755))

	err = edit.Save()
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "写入配置失败")
}

// ============================================================
// 排版还原
// ============================================================

// yaml 往返会压掉顶层块之间的空行，Save 必须把它们补回来。
func TestSavePreservesBlankLinesBetweenBlocks(t *testing.T) {
	original := `# 头部注释
project: my-erp

deploy:
  target: docker

# 安装源
sources:
  - id: local-dev
    type: local
    path: ./components

components: []

resources: []
`
	path := writeTemp(t, original)

	edit, err := OpenEdit(path)
	require.NoError(t, err)
	require.True(t, edit.AddComponent("people/basic", "1.0.0"))
	require.NoError(t, edit.Save())

	out := read(t, path)
	assert.Contains(t, out, "project: my-erp\n\ndeploy:")
	assert.Contains(t, out, "\n\n# 安装源\nsources:", "空行要补在注释块之前")
	assert.Contains(t, out, "\n\ncomponents:")
	assert.Contains(t, out, "\n\nresources:")

	// 补空行不能破坏可解析性
	cfg, err := ParseConfigFile(path)
	require.NoError(t, err)
	assert.Len(t, cfg.Components, 1)
	assert.Len(t, cfg.Sources, 1)
}

// 原文本来就没有空行时不凭空加。
func TestSaveKeepsCompactLayout(t *testing.T) {
	path := writeTemp(t, "project: my-erp\ndeploy:\n  target: docker\ncomponents: []\n")

	edit, err := OpenEdit(path)
	require.NoError(t, err)
	require.True(t, edit.AddComponent("people/basic", "1.0.0"))
	require.NoError(t, edit.Save())

	assert.NotContains(t, read(t, path), "\n\n")
}

func TestKeysPrecededByBlankLine(t *testing.T) {
	got := keysPrecededByBlankLine([]byte("a: 1\n\nb: 2\n# 注释\n\nc: 3\nd: 4\n"))
	assert.True(t, got["b"])
	assert.True(t, got["c"], "注释块之上的空行也算")
	assert.False(t, got["a"])
	assert.False(t, got["d"])
}

func TestRestoreBlankLinesNoop(t *testing.T) {
	encoded := []byte("a: 1\nb: 2\n")
	assert.Equal(t, encoded, restoreBlankLines([]byte("a: 1\nb: 2\n"), encoded))
}
