package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/skills"
)

func TestSkillsStatusOnFreshProject(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p", "--no-skills").code)

	r := runIn(t, dir, "skills", "status")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "缺失")
	assert.Contains(t, r.stdout, "AGENTS.md")
}

// 不带子命令时等于 status——只读是安全的默认。
func TestSkillsBareIsStatus(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p", "--no-skills").code)

	r := runIn(t, dir, "skills")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "缺失")

	_, err := os.Stat(filepath.Join(dir, "AGENTS.md"))
	assert.True(t, os.IsNotExist(err), "光看状态不该写文件")
}

func TestSkillsUpdateInstallsThenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p", "--no-skills").code)

	r := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, r.code, r.stderr)
	_, err := os.Stat(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)

	again := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, again.code, again.stderr)
	assert.Contains(t, again.stdout, "已是最新")
}

func TestSkillsUpdateSkipsModifiedAndSaysHow(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p").code)

	p := filepath.Join(dir, "AGENTS.md")
	mine := []byte("我改过了\n")
	require.NoError(t, os.WriteFile(p, mine, 0o644))

	r := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "已手改")
	assert.Contains(t, r.stdout, "删掉", "要告诉人怎么放弃本地修改")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after))
}

// status 要能说出是从哪个版本升上来的。
func TestSkillsStatusShowsOutdatedWithVersions(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p").code)

	// 伪造一份「上个版本写的」AGENTS.md：内容与 lock 记录一致、与资产不同。
	old := []byte("上个版本的导读\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), old, 0o644))
	lockPath := filepath.Join(dir, ".brickkit", "skills.lock")
	require.NoError(t, os.WriteFile(lockPath, []byte(
		`{"entries":[{"path":"AGENTS.md","version":"0.0.1","sum":"`+
			sumOf(old)+`"}]}`+"\n"), 0o644))

	r := runIn(t, dir, "skills", "status")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "待更新")
	assert.Contains(t, r.stdout, "0.0.1")
	assert.Contains(t, r.stdout, "需要刷新")
}

// 未初始化的目录里跑 skills 要说清楚，而不是默默在别人家里建 .claude/。
func TestSkillsRefusesOutsideProject(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "skills", "update")
	assert.NotEqual(t, 0, r.code)
	assert.Contains(t, r.stderr+r.stdout, "brickkit init")

	_, err := os.Stat(filepath.Join(dir, "AGENTS.md"))
	assert.True(t, os.IsNotExist(err), "不是项目就一个文件都别写")
}

func sumOf(b []byte) string {
	return skills.Sum(b)
}
