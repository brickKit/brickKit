package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

const localTeamDeploy = "# team deploy file\ntarget: docker\ncomponents:\n  - id: erp/api\n"

// localProject 是一个有一个组件的项目；deploy.yaml 带注释，用来确认 local on 是逐字节复制。
func localProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"brickkit.yaml": "project: shop\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n",
		"deploy.yaml":   localTeamDeploy,
	})
	return dir
}

func mustLocal(t *testing.T, dir string, args ...string) result {
	t.Helper()
	r := runIn(t, dir, append([]string{"local"}, args...)...)
	require.Equal(t, clierr.ExitOK, r.code, "%v\n%s", args, r.stdout+r.stderr)
	return r
}

func TestLocalOnCopiesDeployBytes(t *testing.T) {
	dir := localProject(t)
	r := mustLocal(t, dir, "on")
	assert.Equal(t, localTeamDeploy, readFile(t, filepath.Join(dir, "deploy.local.yaml")), "逐字节复制，注释也在")
	assert.FileExists(t, filepath.Join(dir, ".brickkit", "local-mode"))
	assert.Contains(t, r.stdout, "Local mode is on")
	assert.Contains(t, r.stdout, "copied from deploy.yaml")
}

// 附录 A16：off 不删文件，再 on 时沿用它——绝不覆盖使用者的本地修改。
func TestLocalOnReusesExistingFile(t *testing.T) {
	dir := localProject(t)
	mine := "target: docker\ncomponents:\n  - id: erp/api\n    mode: debug\n    localPort: 8081\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml"), []byte(mine), 0o644))

	r := mustLocal(t, dir, "on")
	assert.Equal(t, mine, readFile(t, filepath.Join(dir, "deploy.local.yaml")))
	assert.Contains(t, r.stdout, "existing deploy.local.yaml")
	assert.Contains(t, r.stdout, "brickkit local refresh")

	r = mustLocal(t, dir, "on")
	assert.Contains(t, r.stdout, "already on")
}

func TestLocalOffKeepsFile(t *testing.T) {
	dir := localProject(t)
	mustLocal(t, dir, "on")
	r := mustLocal(t, dir, "off")
	assert.NoFileExists(t, filepath.Join(dir, ".brickkit", "local-mode"))
	assert.FileExists(t, filepath.Join(dir, "deploy.local.yaml"), "off 只停止读取，不删文件")
	assert.Contains(t, r.stdout, "Local mode is off")

	r = mustLocal(t, dir, "off")
	assert.Contains(t, r.stdout, "already off")
}

func TestLocalStatusReportsState(t *testing.T) {
	dir := localProject(t)
	r := mustLocal(t, dir)
	assert.Contains(t, r.stdout, "Local mode: off")
	assert.Contains(t, r.stdout, "deploy.local.yaml: none")

	mustLocal(t, dir, "on")
	mustLocal(t, dir, "off")
	r = mustLocal(t, dir, "status")
	assert.Contains(t, r.stdout, "Local mode: off")
	assert.Contains(t, r.stdout, "deploy.local.yaml: present (not read while local mode is off)")
}

// 团队加了组件、本地文件没跟上：status 说清过期，并给出 refresh 这条路。
func TestLocalStatusReportsStale(t *testing.T) {
	dir := localProject(t)
	mustLocal(t, dir, "on")
	writeTree(t, dir, map[string]string{
		"brickkit.yaml": "project: shop\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n  - id: crm/web\n    version: 1.0.0\n",
		"deploy.yaml":   localTeamDeploy + "  - id: crm/web\n",
	})
	r := mustLocal(t, dir, "status")
	assert.Contains(t, r.stdout, "Local mode: on")
	assert.Contains(t, r.stdout, "crm/web")
	assert.Contains(t, r.stdout, "brickkit local refresh")

	writeTree(t, dir, map[string]string{"deploy.local.yaml": localTeamDeploy + "  - id: crm/web\n"})
	r = mustLocal(t, dir, "status")
	assert.Contains(t, r.stdout, "matches brickkit.yaml")
}

func TestLocalRefreshBacksUpAndSummarises(t *testing.T) {
	dir := localProject(t)
	old := "target: docker\ncomponents:\n  - id: erp/api\n    mode: debug\n    localPort: 8081\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml"), []byte(old), 0o644))

	r := mustLocal(t, dir, "refresh")
	assert.Equal(t, old, readFile(t, filepath.Join(dir, "deploy.local.yaml.bak")))
	assert.Equal(t, localTeamDeploy, readFile(t, filepath.Join(dir, "deploy.local.yaml")))
	assert.Contains(t, r.stdout, "deploy.local.yaml.bak")
	assert.Contains(t, r.stdout, "2 local changes")
	assert.Contains(t, r.stdout, "- [erp/api] mode: debug (now unset)")
	assert.Contains(t, r.stdout, "- [erp/api] localPort: 8081 (now unset)")
}

func TestLocalRefreshNothingToMerge(t *testing.T) {
	dir := localProject(t)
	mustLocal(t, dir, "on")
	r := mustLocal(t, dir, "refresh")
	assert.Contains(t, r.stdout, "no local changes")
}

func TestLocalRefreshReplacesOldBackup(t *testing.T) {
	dir := localProject(t)
	mustLocal(t, dir, "on")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml.bak"), []byte("older\n"), 0o644))
	r := mustLocal(t, dir, "refresh")
	assert.Equal(t, localTeamDeploy, readFile(t, filepath.Join(dir, "deploy.local.yaml.bak")))
	assert.Contains(t, r.stdout, "previous deploy.local.yaml.bak was replaced")
}

func TestLocalRefreshWithoutFileFails(t *testing.T) {
	dir := localProject(t)
	r := runIn(t, dir, "local", "refresh")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "brickkit local on")
	assert.NoFileExists(t, filepath.Join(dir, "deploy.local.yaml"))
}

func TestLocalRefreshKeepsSwitch(t *testing.T) {
	dir := localProject(t)
	mustLocal(t, dir, "on")
	mustLocal(t, dir, "off")
	mustLocal(t, dir, "refresh")
	assert.NoFileExists(t, filepath.Join(dir, ".brickkit", "local-mode"), "refresh 不改开关")
}

func TestLocalOutsideProjectFails(t *testing.T) {
	r := runIn(t, t.TempDir(), "local", "on")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "deploy.yaml")
}

// 摘要的三种写法：值不同（当前为）、团队文件没有这个字段（当前未设置）、整个条目没了。
func TestLocalRefreshSummaryKinds(t *testing.T) {
	dir := localProject(t)
	writeTree(t, dir, map[string]string{"deploy.yaml": "target: docker\ncomponents:\n  - id: erp/api\n    mode: enabled\n"})
	old := "target: docker\ncomponents:\n  - id: erp/api\n    mode: debug\n  - id: crm/old\n    mode: debug\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml"), []byte(old), 0o644))

	r := mustLocal(t, dir, "refresh")
	assert.Contains(t, r.stdout, "- [erp/api] mode: debug (now `enabled`)")
	assert.Contains(t, r.stdout, "- [crm/old] the whole entry (deploy.yaml no longer has it)")
	assert.Contains(t, r.stdout, "2 local changes")
}

// 旧文件写坏了（YAML 语法错误）正是最需要重新生成的时候：照样刷新、备份，摘要说对比不了。
func TestLocalRefreshBrokenOldFile(t *testing.T) {
	dir := localProject(t)
	broken := "target: [docker\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml"), []byte(broken), 0o644))

	r := mustLocal(t, dir, "refresh")
	assert.Equal(t, broken, readFile(t, filepath.Join(dir, "deploy.local.yaml.bak")))
	assert.Equal(t, localTeamDeploy, readFile(t, filepath.Join(dir, "deploy.local.yaml")))
	assert.Contains(t, r.stdout, "could not be compared")
}

func TestLocalOnOutsideProjectIsNotALocalDir(t *testing.T) {
	r := runIn(t, t.TempDir(), "local", "refresh")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "brickkit init")
}
