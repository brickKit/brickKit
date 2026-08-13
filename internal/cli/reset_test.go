// 本文件是 Step 8「brickkit reset」的命令层业务行为测试，覆盖开发计划 8.4–8.7。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/backup"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
)

// initProject 在 dir 中执行一次真实的 brickkit init。
func initProject(t *testing.T, dir string, args ...string) {
	t.Helper()
	r := runIn(t, dir, append([]string{"init", "my-project"}, args...)...)
	require.Equal(t, clierr.ExitOK, r.code, "init 应成功：%s%s", r.stdout, r.stderr)
}

func overwriteConfig(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// ============================================================
// 8.4 reset 恢复到初始状态
// ============================================================

func TestResetRestoresInitial(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)

	layout := config.NewLayout(dir, "")
	initial := readFile(t, layout.ConfigPath())
	overwriteConfig(t, layout.ConfigPath(), "project: my-project\ncomponents:\n  - id: people/basic\n    version: 1.0.0\n")

	r := runIn(t, dir, "reset")
	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, initial, readFile(t, layout.ConfigPath()), "配置应恢复为 init 时的骨架")

	// 004 §3.10 / 003 §7.2 的输出样例
	assert.Contains(t, r.stdout, "🔄 已恢复 brickkit.yaml 到初始状态")
	assert.Contains(t, r.stdout, "备份位置：.brickkit/backup/brickkit.yaml.initial")
	assert.Contains(t, r.stdout, "恢复时间：")
	assert.Regexp(t, `恢复时间：\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`, r.stdout)
}

// 备份文件本身不被消耗：可以反复 reset。
func TestResetIsRepeatable(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)
	layout := config.NewLayout(dir, "")
	initial := readFile(t, layout.ConfigPath())

	for i := 0; i < 3; i++ {
		overwriteConfig(t, layout.ConfigPath(), "被改坏的配置\n")
		r := runIn(t, dir, "reset")
		require.Equal(t, clierr.ExitOK, r.code, r.stderr)
		assert.Equal(t, initial, readFile(t, layout.ConfigPath()))
	}
}

// 配置文件被误删后仍能恢复。
func TestResetRecreatesDeletedConfig(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)
	layout := config.NewLayout(dir, "")
	initial := readFile(t, layout.ConfigPath())
	require.NoError(t, os.Remove(layout.ConfigPath()))

	r := runIn(t, dir, "reset")
	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, initial, readFile(t, layout.ConfigPath()))
}

// ============================================================
// 8.5 reset --last 恢复到上一次备份
// ============================================================

func TestResetLastRestoresPreviousState(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)
	layout := config.NewLayout(dir, "")

	// 模拟 add / remove：修改配置前先备份（Step 9 接线后由命令自动完成）
	overwriteConfig(t, layout.ConfigPath(), "上一次操作前的状态\n")
	_, err := backup.SaveLast(layout)
	require.NoError(t, err)
	overwriteConfig(t, layout.ConfigPath(), "本次操作后的状态\n")

	r := runIn(t, dir, "reset", "--last")
	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, "上一次操作前的状态\n", readFile(t, layout.ConfigPath()))

	assert.Contains(t, r.stdout, "🔄 已恢复 brickkit.yaml 到上一次备份")
	assert.Contains(t, r.stdout, "备份位置：.brickkit/backup/brickkit.yaml.last")
}

// reset 与 reset --last 恢复的是两个不同的快照，互不影响。
func TestResetAndResetLastAreIndependent(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)
	layout := config.NewLayout(dir, "")
	initial := readFile(t, layout.ConfigPath())

	overwriteConfig(t, layout.ConfigPath(), "上一次操作前的状态\n")
	_, err := backup.SaveLast(layout)
	require.NoError(t, err)
	overwriteConfig(t, layout.ConfigPath(), "当前状态\n")

	require.Equal(t, clierr.ExitOK, runIn(t, dir, "reset").code)
	assert.Equal(t, initial, readFile(t, layout.ConfigPath()))

	require.Equal(t, clierr.ExitOK, runIn(t, dir, "reset", "--last").code)
	assert.Equal(t, "上一次操作前的状态\n", readFile(t, layout.ConfigPath()),
		"reset 到初始状态不应破坏 .last")
}

// ============================================================
// 8.6 reset 后提示需要重新 brickkit up
// ============================================================

func TestResetPrintsUpReminder(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)

	r := runIn(t, dir, "reset")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "⚠️ 注意：恢复后需要重新执行 brickkit up 使变更生效")
}

// ============================================================
// 8.7 备份文件不存在时报错
// ============================================================

func TestResetWithoutInitialBackup(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)
	layout := config.NewLayout(dir, "")
	require.NoError(t, os.Remove(backup.Path(layout, backup.Initial)))
	overwriteConfig(t, layout.ConfigPath(), "当前状态\n")

	r := runIn(t, dir, "reset")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "❌")
	assert.Contains(t, r.stderr, "brickkit.yaml.initial")
	assert.Equal(t, "当前状态\n", readFile(t, layout.ConfigPath()), "失败时不得改动当前配置")
}

// 还没执行过 add / remove 就 reset --last：提示 .last 是怎么来的。
func TestResetLastWithoutBackup(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)

	r := runIn(t, dir, "reset", "--last")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "brickkit.yaml.last")
	assert.Contains(t, r.stderr, "brickkit add")
	assert.Contains(t, r.stderr, "brickkit reset", "应提示可以改用 reset 恢复到初始状态")
}

// 未初始化的目录里执行 reset：报错而不是 panic。
func TestResetInUninitializedDir(t *testing.T) {
	r := runIn(t, t.TempDir(), "reset")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "❌")
}

// ============================================================
// 多环境配置与用法
// ============================================================

// --config brickkit.prod.yaml 时恢复的是 prod 的备份。
func TestResetWithCustomConfigFile(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir, "--config", "brickkit.prod.yaml")

	layout := config.NewLayout(dir, "brickkit.prod.yaml")
	initial := readFile(t, layout.ConfigPath())
	overwriteConfig(t, layout.ConfigPath(), "改过的 prod 配置\n")

	r := runIn(t, dir, "reset", "--config", "brickkit.prod.yaml")
	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, initial, readFile(t, layout.ConfigPath()))
	assert.Contains(t, r.stdout, "已恢复 brickkit.prod.yaml 到初始状态")
	assert.Contains(t, r.stdout, filepath.ToSlash(filepath.Join(
		config.DirBrickkit, config.DirBackup, "brickkit.prod.yaml.initial")))
}

func TestResetRejectsExtraArgs(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)

	r := runIn(t, dir, "reset", "extra")
	assert.Equal(t, clierr.ExitUsage, r.code)
}

// 注入时钟后，"恢复时间"是可锁定的 UTC RFC3339 时间戳。
func TestResetTimestampUsesInjectedClock(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)

	var out, errBuf bytes.Buffer
	opts := &Options{
		WorkDir:    dir,
		ConfigPath: DefaultConfigFile,
		LogLevel:   logging.LevelOff,
		Stdout:     &out,
		Stderr:     &errBuf,
		Now: func() time.Time {
			return time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
		},
	}
	code := Run(NewRootCommand(opts), opts, []string{"reset"})

	require.Equal(t, clierr.ExitOK, code, errBuf.String())
	assert.Contains(t, out.String(), "恢复时间：2026-08-08T10:00:00Z")
}

// ============================================================
// 8.2 / 8.3 的命令接线（Step 9 已回填 P16）
// ============================================================

// add / remove 前自动生成 .last，由 add_test.go / remove_test.go 的
// TestAddCreatesLastBackup、TestRemoveCreatesLastBackup 验证；
// 这里只确认 reset --last 能真正撤销一次 add。
func TestResetLastUndoesAdd(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	before := f.config(t)

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0").code)
	require.NotEqual(t, before, f.config(t))

	r := runIn(t, f.Dir, "reset", "--last")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Equal(t, before, f.config(t))
	assert.Empty(t, f.refs(t))
}
