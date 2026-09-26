package yamlfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/yamlfile"
)

const editSample = `# team deploy file
target: docker

components:
  - id: people/basic # the directory
    mode: disable # off for now
  - id: erp/backend@1.0.0
`

func openSample(t *testing.T) (*yamlfile.Edit, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "deploy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(editSample), 0o644))
	e, err := yamlfile.OpenEdit(path)
	require.NoError(t, err)
	return e, path
}

func saved(t *testing.T, e *yamlfile.Edit, path string) string {
	t.Helper()
	require.NoError(t, e.Save())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// 改一个字段，其余的注释、空行、顺序都原样留着——这是编辑器存在的全部理由。
func TestEditSetFieldKeepsCommentsAndBlankLines(t *testing.T) {
	e, path := openSample(t)

	require.True(t, e.SetField("components", "people/basic", "mode", "enabled"))

	assert.Equal(t, `# team deploy file
target: docker

components:
  - id: people/basic # the directory
    mode: enabled # off for now
  - id: erp/backend@1.0.0
`, saved(t, e, path))
}

func TestEditSetFieldAppendsMissingKey(t *testing.T) {
	e, path := openSample(t)

	require.True(t, e.SetField("components", "erp/backend@1.0.0", "mode", "local"))

	assert.Contains(t, saved(t, e, path), "  - id: erp/backend@1.0.0\n    mode: local\n")
}

// 删字段与写成别的值不是一回事：mode 不写才是"跟着上层走"。
func TestEditDeleteField(t *testing.T) {
	e, path := openSample(t)

	require.True(t, e.DeleteField("components", "people/basic", "mode"))
	assert.False(t, e.DeleteField("components", "people/basic", "mode"), "已经没有这个字段")

	assert.NotContains(t, saved(t, e, path), "mode:")
}

func TestEditUnknownEntry(t *testing.T) {
	e, _ := openSample(t)

	assert.False(t, e.SetField("components", "nope/nope", "mode", "enabled"))
	assert.False(t, e.DeleteField("components", "nope/nope", "mode"))
	assert.False(t, e.SetField("missing", "people/basic", "mode", "enabled"))
}

func TestEditAppendAndRemoveEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy.yaml")
	require.NoError(t, os.WriteFile(path, []byte("target: docker\ncomponents: []\n"), 0o644))
	e, err := yamlfile.OpenEdit(path)
	require.NoError(t, err)

	require.True(t, e.AppendEntry("components", "people/basic"))
	assert.False(t, e.AppendEntry("components", "people/basic"), "不重复写")
	require.True(t, e.AppendEntry("components", "erp/backend"))
	require.True(t, e.RemoveEntry("components", "people/basic"))
	assert.False(t, e.RemoveEntry("components", "people/basic"))

	assert.Equal(t, "target: docker\ncomponents:\n  - id: erp/backend\n", saved(t, e, path))
}

func TestOpenEditRejectsInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy.yaml")
	require.NoError(t, os.WriteFile(path, []byte("target: [\n"), 0o644))

	_, err := yamlfile.OpenEdit(path)
	require.Error(t, err)
}

// 部署文件里外壳的成员是嵌在外壳条目 members 下面的完整条目：按 id 找条目时也要找到它们
// （id 在整个文件里唯一，由部署文件校验保证）。
func TestEditFindsNestedMemberEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`target: docker
components:
  - id: erp/shell
    members:
      - id: erp/api # hosted
        mode: disable
`), 0o644))
	e, err := yamlfile.OpenEdit(path)
	require.NoError(t, err)

	require.True(t, e.DeleteField("components", "erp/api", "mode"))
	require.True(t, e.SetField("components", "erp/api", "localPort", "9000"))
	assert.Equal(t, `target: docker
components:
  - id: erp/shell
    members:
      - id: erp/api # hosted
        localPort: "9000"
`, saved(t, e, path))
}
