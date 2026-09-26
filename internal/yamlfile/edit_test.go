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

// 删掉一个键时，写在它上方的注释不跟着丢：那常常是一段小节说明，不属于这一个键。
// 挪到下一个键上；它是最后一个键时挪到前一个值的下方。行尾注释属于这个键本身，一起删。
func TestEditDeleteFieldKeepsNeighbourComment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`target: docker
components:
  - id: a/b
    # local tweaks below
    mode: disable # off for now
    localPort: 9000
  - id: c/d
    # last one
    mode: disable
`), 0o644))
	e, err := yamlfile.OpenEdit(path)
	require.NoError(t, err)
	require.True(t, e.DeleteField("components", "a/b", "mode"))
	require.True(t, e.DeleteField("components", "c/d", "mode"))
	out := saved(t, e, path)
	assert.Contains(t, out, "# local tweaks below\n    localPort: 9000")
	assert.NotContains(t, out, "off for now", "行尾注释属于被删的键")
	assert.Contains(t, out, "# last one")
}

// Save 先写临时文件再改名：写不进去时原文件一个字节都不动，也不留下临时文件；权限保持原样。
func TestEditSaveIsAtomic(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 不受目录权限限制")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(editSample), 0o640))
	e, err := yamlfile.OpenEdit(path)
	require.NoError(t, err)
	require.True(t, e.SetField("components", "people/basic", "mode", "enabled"))

	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	require.Error(t, e.Save())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, editSample, string(data), "写失败时原文件不动")

	require.NoError(t, os.Chmod(dir, 0o755))
	require.NoError(t, e.Save())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "不留临时文件")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "权限保持原样")
}

// RemoveEntry 同样找得到嵌在外壳下面的成员条目；最后一个成员删掉后 members 键一并去掉。
func TestEditRemoveNestedMember(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`target: docker
components:
  - id: erp/shell
    members:
      - id: erp/api
      - id: erp/worker
`), 0o644))
	e, err := yamlfile.OpenEdit(path)
	require.NoError(t, err)
	require.True(t, e.RemoveEntry("components", "erp/api"))
	require.True(t, e.RemoveEntry("components", "erp/worker"))
	assert.Equal(t, "target: docker\ncomponents:\n  - id: erp/shell\n", saved(t, e, path))
}

// deploy.yaml 可能是指向共享配置的符号链接：Save 写到链接指向的文件，链接本身原样保留——
// 否则改名会把链接换成一份普通文件，悄悄与共享配置分了叉。
func TestEditSaveWritesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "shared.yaml")
	link := filepath.Join(dir, "deploy.yaml")
	require.NoError(t, os.WriteFile(real, []byte(editSample), 0o644))
	require.NoError(t, os.Symlink(real, link))

	e, err := yamlfile.OpenEdit(link)
	require.NoError(t, err)
	require.True(t, e.SetField("components", "people/basic", "mode", "enabled"))
	require.NoError(t, e.Save())

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "链接本身保留")
	data, err := os.ReadFile(real)
	require.NoError(t, err)
	assert.Contains(t, string(data), "mode: enabled", "改动写进链接指向的文件")
}

// 被删的键是最后一个、前面是一个块结构的值（labels 映射）：注释留在这个条目里，
// 下一个条目的排版不受影响。
func TestEditDeleteLastFieldAfterBlockValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deploy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`components:
  - id: a/b
    labels:
      x: "1"
    # last one
    mode: disable
  - id: c/d
`), 0o644))
	e, err := yamlfile.OpenEdit(path)
	require.NoError(t, err)
	require.True(t, e.DeleteField("components", "a/b", "mode"))
	assert.Equal(t, `components:
  - id: a/b
    labels:
      x: "1"
    # last one
  - id: c/d
`, saved(t, e, path))
}
