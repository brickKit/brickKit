// 本文件是 Step 8「配置备份与恢复」库层的业务行为测试。
//
// 覆盖开发计划 8.1（init 生成 .initial）、8.2 / 8.3（add / remove 前生成 .last 所依赖的
// 备份能力）与备份/恢复的全部边界；命令层的 8.4–8.7 见 internal/cli/reset_test.go。
package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
)

// newProject 初始化一个真实项目（走 brickkit init 的同一条代码路径）。
func newProject(t *testing.T) config.Layout {
	t.Helper()
	layout := config.NewLayout(t.TempDir(), "")
	_, err := config.InitProject(layout, "my-project")
	require.NoError(t, err)
	return layout
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "读取 %s", path)
	return string(data)
}

func writeConfig(t *testing.T, layout config.Layout, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(layout.ConfigPath(), []byte(content), 0o644))
}

// ============================================================
// 8.1 init 后 .initial 存在
// ============================================================

func TestInitialBackupCreatedByInit(t *testing.T) {
	layout := newProject(t)

	path := Path(layout, Initial)
	assert.Equal(t, filepath.Join(layout.BackupDir(), "brickkit.yaml.initial"), path)
	require.FileExists(t, path)
	assert.True(t, Exists(layout, Initial))
	assert.False(t, Exists(layout, Last), "init 不生成 .last")

	assert.Equal(t, readFile(t, layout.ConfigPath()), readFile(t, path),
		"初始备份必须与配置逐字节相同")
}

// ============================================================
// 8.2 / 8.3 add / remove 前创建 .last
// ============================================================

// SaveLast 是 add / remove 在修改配置前要调用的备份动作（003 §7.1）。
func TestSaveLastCopiesCurrentConfig(t *testing.T) {
	layout := newProject(t)
	writeConfig(t, layout, "project: my-project\ndeploy:\n  target: docker\ncomponents: []\n")

	rec, err := SaveLast(layout)
	require.NoError(t, err)

	assert.Equal(t, Last, rec.Kind)
	assert.Equal(t, Path(layout, Last), rec.Path)
	assert.Equal(t, layout.ConfigPath(), rec.ConfigPath)
	require.FileExists(t, rec.Path)
	assert.Equal(t, readFile(t, layout.ConfigPath()), readFile(t, rec.Path))
}

// 每次操作前都覆盖 .last：它表示"上一步操作前的状态"，不是历史堆栈。
func TestSaveLastOverwritesPrevious(t *testing.T) {
	layout := newProject(t)

	writeConfig(t, layout, "第一次\n")
	_, err := SaveLast(layout)
	require.NoError(t, err)

	writeConfig(t, layout, "第二次\n")
	_, err = SaveLast(layout)
	require.NoError(t, err)

	assert.Equal(t, "第二次\n", readFile(t, Path(layout, Last)))
}

// .initial 不因为 add / remove 而改变：它永远是 init 时的骨架。
func TestSaveLastDoesNotTouchInitial(t *testing.T) {
	layout := newProject(t)
	initial := readFile(t, Path(layout, Initial))

	writeConfig(t, layout, "改过的配置\n")
	_, err := SaveLast(layout)
	require.NoError(t, err)

	assert.Equal(t, initial, readFile(t, Path(layout, Initial)))
}

// 项目未初始化（没有 brickkit.yaml）时无从备份，报错要指出下一步。
func TestSaveLastWithoutConfig(t *testing.T) {
	layout := config.NewLayout(t.TempDir(), "")

	_, err := SaveLast(layout)
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeProjectMissing, e.Code)
	out := e.Format()
	assert.Contains(t, out, "brickkit.yaml")
	assert.Contains(t, out, "brickkit init")
}

// 备份目录被删掉时自动重建，不应因此失败。
func TestSaveLastRecreatesBackupDir(t *testing.T) {
	layout := newProject(t)
	require.NoError(t, os.RemoveAll(layout.BackupDir()))

	_, err := SaveLast(layout)
	require.NoError(t, err)
	assert.True(t, Exists(layout, Last))
}

// ============================================================
// 恢复（命令层验证见 8.4–8.7）
// ============================================================

func TestRestoreInitial(t *testing.T) {
	layout := newProject(t)
	initial := readFile(t, layout.ConfigPath())
	writeConfig(t, layout, "被改坏的配置\n")

	rec, err := Restore(layout, Initial)
	require.NoError(t, err)

	assert.Equal(t, Initial, rec.Kind)
	assert.Equal(t, initial, readFile(t, layout.ConfigPath()))
	assert.Equal(t, initial, readFile(t, Path(layout, Initial)), "备份文件本身不被消耗")
}

func TestRestoreLast(t *testing.T) {
	layout := newProject(t)
	writeConfig(t, layout, "add 之前的状态\n")
	_, err := SaveLast(layout)
	require.NoError(t, err)
	writeConfig(t, layout, "add 之后的状态\n")

	rec, err := Restore(layout, Last)
	require.NoError(t, err)

	assert.Equal(t, Last, rec.Kind)
	assert.Equal(t, "add 之前的状态\n", readFile(t, layout.ConfigPath()))
}

// 配置文件被误删后，reset 仍然能把它恢复回来（这正是备份的意义）。
func TestRestoreRecreatesDeletedConfig(t *testing.T) {
	layout := newProject(t)
	initial := readFile(t, layout.ConfigPath())
	require.NoError(t, os.Remove(layout.ConfigPath()))

	_, err := Restore(layout, Initial)
	require.NoError(t, err)
	assert.Equal(t, initial, readFile(t, layout.ConfigPath()))
}

// 8.7 备份文件不存在时报错。
func TestRestoreMissingBackup(t *testing.T) {
	layout := newProject(t)

	_, err := Restore(layout, Last)
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeBackupMissing, e.Code)
	out := e.Format()
	assert.Contains(t, out, "brickkit.yaml.last")
	assert.Contains(t, out, "brickkit add", "应说明 .last 是 add / remove 前生成的")
}

func TestRestoreMissingInitialBackup(t *testing.T) {
	layout := newProject(t)
	require.NoError(t, os.Remove(Path(layout, Initial)))

	_, err := Restore(layout, Initial)
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeBackupMissing, e.Code)
	assert.Contains(t, e.Format(), "brickkit.yaml.initial")
}

// ============================================================
// 多环境配置（004 §3.5）
// ============================================================

// --config brickkit.prod.yaml 时，备份名与恢复目标同步变化，互不串味。
func TestBackupFollowsConfigName(t *testing.T) {
	root := t.TempDir()
	prod := config.NewLayout(root, "brickkit.prod.yaml")
	_, err := config.InitProject(prod, "my-project")
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(prod.BackupDir(), "brickkit.prod.yaml.initial"), Path(prod, Initial))
	require.FileExists(t, Path(prod, Initial))

	writeConfig(t, prod, "改过的 prod 配置\n")
	_, err = SaveLast(prod)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(prod.BackupDir(), "brickkit.prod.yaml.last"), Path(prod, Last))

	// 默认配置的备份完全不受影响
	def := config.NewLayout(root, "")
	assert.False(t, Exists(def, Initial))
	assert.False(t, Exists(def, Last))
}
