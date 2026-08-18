// 本文件是 Step 8 的代码层单测：种类映射、未知种类与各类 IO 失败路径。
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

// skipIfRoot 跳过依赖文件权限的用例：root 无视权限位。
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时权限位不生效")
	}
}

func TestKindLabel(t *testing.T) {
	assert.Equal(t, "初始状态", Initial.Label())
	assert.Equal(t, "上一次备份", Last.Label())
	assert.Equal(t, "unknown", Kind("unknown").Label())
}

func TestPathAndExistsWithUnknownKind(t *testing.T) {
	layout := newProject(t)

	assert.Empty(t, Path(layout, Kind("nope")))
	assert.False(t, Exists(layout, Kind("nope")))
}

// 备份位置被目录占住时不算存在（否则会把目录当备份读）。
func TestExistsIgnoresDirectory(t *testing.T) {
	layout := newProject(t)
	require.NoError(t, os.Remove(Path(layout, Initial)))
	require.NoError(t, os.MkdirAll(Path(layout, Initial), 0o755))

	assert.False(t, Exists(layout, Initial))
}

func TestUnknownKindIsInternalError(t *testing.T) {
	layout := newProject(t)

	_, err := Restore(layout, Kind("nope"))
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInternal, clierr.As(err).Code)

	_, err = save(layout, Kind("nope"))
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInternal, clierr.As(err).Code)
}

// 配置文件不可读时报 IO 错误（而不是当成"项目未初始化"）。
func TestSaveLastUnreadableConfig(t *testing.T) {
	skipIfRoot(t)

	layout := newProject(t)
	require.NoError(t, os.Chmod(layout.ConfigPath(), 0o000))
	t.Cleanup(func() { _ = os.Chmod(layout.ConfigPath(), 0o644) })

	_, err := SaveLast(layout)
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "读取配置失败")
	assert.Contains(t, e.Format(), "检查文件与目录权限")
}

// 备份目录的位置被普通文件占住。
func TestSaveLastBackupDirBlocked(t *testing.T) {
	layout := newProject(t)
	require.NoError(t, os.RemoveAll(layout.BackupDir()))
	require.NoError(t, os.WriteFile(layout.BackupDir(), []byte("占位"), 0o644))

	_, err := SaveLast(layout)
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "创建备份目录失败")
}

// 备份文件的位置被目录占住，写入失败。
func TestSaveLastWriteBlocked(t *testing.T) {
	layout := newProject(t)
	require.NoError(t, os.MkdirAll(Path(layout, Last), 0o755))

	_, err := SaveLast(layout)
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "写入备份失败")
}

// 备份存在但不可读。
func TestRestoreUnreadableBackup(t *testing.T) {
	skipIfRoot(t)

	layout := newProject(t)
	path := Path(layout, Initial)
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err := Restore(layout, Initial)
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "读取备份失败")
}

// 配置文件的位置被目录占住时，恢复失败但备份完好。
func TestRestoreWriteBlocked(t *testing.T) {
	layout := newProject(t)
	require.NoError(t, os.Remove(layout.ConfigPath()))
	require.NoError(t, os.MkdirAll(layout.ConfigPath(), 0o755))

	_, err := Restore(layout, Initial)
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "写入配置失败")
	assert.True(t, Exists(layout, Initial), "失败不应损坏备份")
}

// 项目根目录不存在时，恢复会连目录一起建出来。
func TestRestoreCreatesMissingProjectDir(t *testing.T) {
	layout := newProject(t)
	initial, err := os.ReadFile(Path(layout, Initial))
	require.NoError(t, err)

	nested := config.NewLayout(filepath.Join(layout.Root, "sub", "dir"), "")
	require.NoError(t, os.MkdirAll(nested.BackupDir(), 0o755))
	require.NoError(t, os.WriteFile(Path(nested, Initial), initial, 0o644))

	rec, err := Restore(nested, Initial)
	require.NoError(t, err)
	assert.FileExists(t, rec.ConfigPath)
}

// 配置文件所在目录无法创建（路径上有普通文件挡着）。
func TestRestoreCannotCreateConfigDir(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("占位"), 0o644))

	// --config 用绝对路径指向被挡住的目录；备份仍在项目根之下
	layout := config.NewLayout(root, filepath.Join(blocked, "brickkit.yaml"))
	require.NoError(t, os.MkdirAll(layout.BackupDir(), 0o755))
	require.NoError(t, os.WriteFile(Path(layout, Initial), []byte("project: x\n"), 0o644))

	_, err := Restore(layout, Initial)
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "创建项目目录失败")
}

// 记录里的相对路径是用于输出的，必须相对项目根。
func TestRecordRelPath(t *testing.T) {
	layout := newProject(t)

	rec, err := SaveLast(layout)
	require.NoError(t, err)
	assert.Equal(t, ".brickkit/backup/brickkit.yaml.last", rec.RelPath)
	assert.Equal(t, filepath.Join(layout.BackupDir(), "brickkit.yaml.last"), rec.Path)
}

// 项目根是相对路径时（brickkit 在当前目录执行），显示路径也要保持相对形式。
func TestDisplayFallsBackToAbsolutePath(t *testing.T) {
	layout := config.NewLayout("relative-root", "")
	assert.Equal(t, ".brickkit/backup/brickkit.yaml.initial",
		display(layout, Path(layout, Initial)))

	// 目标不在项目根之下时，Rel 仍能给出 ../ 形式，不 panic
	assert.NotEmpty(t, display(layout, filepath.Join("other", "x.yaml")))

	// 相对根 + 绝对路径无法求相对路径：原样返回绝对路径，不返回空串
	abs := filepath.Join(string(filepath.Separator), "tmp", "elsewhere", "brickkit.yaml")
	assert.Equal(t, abs, display(layout, abs))
}
