package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
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
	assert.Contains(t, r.stderr+r.stdout, "component.yaml", "也要说清组件仓库这条路")

	_, err := os.Stat(filepath.Join(dir, "AGENTS.md"))
	assert.True(t, os.IsNotExist(err), "不是项目就一个文件都别写")
}

// 独立组件仓库（一个组件一个仓库，通常没有 brickkit.yaml）：skills 只管
// brickkit-component 这一份，不写项目导读，也不装拼装/部署/排障三个项目层面的技能。
func TestSkillsInComponentRepoManagesOnlyTheComponentSkill(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, comp{ID: "people/basic", Version: "1.0.0"}.files())

	st := runIn(t, dir, "skills", "status")
	require.Equal(t, 0, st.code, st.stderr)
	assert.Contains(t, st.stdout, ".claude/skills/brickkit-component/SKILL.md")
	assert.Contains(t, st.stdout, "缺失")
	assert.Contains(t, st.stdout, "组件仓库", "要说明这是组件仓库模式，不然人会奇怪怎么只有一个文件")
	assert.NotContains(t, st.stdout, "brickkit-deploy")
	assert.NotContains(t, st.stdout, "AGENTS.md")

	up := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, up.code, up.stderr)
	_, err := os.Stat(filepath.Join(dir, ".claude", "skills", "brickkit-component", "SKILL.md"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "AGENTS.md"))
	assert.True(t, os.IsNotExist(err), "组件仓库里不该被写进项目导读")
	_, err = os.Stat(filepath.Join(dir, ".claude", "skills", "brickkit-assemble"))
	assert.True(t, os.IsNotExist(err))

	again := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, again.code, again.stderr)
	assert.Contains(t, again.stdout, "已是最新")
}

// 组件仓库多半有自己的 AGENTS.md——一个字都不能碰。
func TestSkillsInComponentRepoLeavesOwnAgentsMdAlone(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, comp{ID: "people/basic", Version: "1.0.0"}.files())
	mine := []byte("# 这个组件自己的说明\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), mine, 0o644))

	require.Equal(t, 0, runIn(t, dir, "skills", "update").code)

	after, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after))
}

// 目录里既有 brickkit.yaml 又有 component.yaml 时按项目处理，和以前一样。
func TestSkillsPrefersProjectWhenBothFilesPresent(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p", "--no-skills").code)
	writeTree(t, dir, comp{ID: "people/basic", Version: "1.0.0"}.files())

	r := runIn(t, dir, "skills", "status")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "AGENTS.md", "按项目处理：完整的一套")
	assert.NotContains(t, r.stdout, "组件仓库")
}

func sumOf(b []byte) string {
	return skills.Sum(b)
}

func TestDetectScope(t *testing.T) {
	touch := func(t *testing.T, dir string, names ...string) {
		t.Helper()
		for _, name := range names {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x: 1\n"), 0o644))
		}
	}
	optsFor := func(dir, configPath string) *Options {
		return &Options{WorkDir: dir, ConfigPath: configPath}
	}

	t.Run("只有 brickkit.yaml 是项目", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "brickkit.yaml")
		scope, layout, err := detectScope(optsFor(dir, DefaultConfigFile))
		require.NoError(t, err)
		assert.Equal(t, skills.ScopeProject, scope)
		assert.Equal(t, dir, layout.Root)
	})

	t.Run("只有 component.yaml 是组件仓库", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "component.yaml")
		scope, _, err := detectScope(optsFor(dir, DefaultConfigFile))
		require.NoError(t, err)
		assert.Equal(t, skills.ScopeComponent, scope)
	})

	t.Run("两者都有时按项目算", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "brickkit.yaml", "component.yaml")
		scope, _, err := detectScope(optsFor(dir, DefaultConfigFile))
		require.NoError(t, err)
		assert.Equal(t, skills.ScopeProject, scope)
	})

	t.Run("--config 指向别的文件名", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "brickkit.prod.yaml")
		scope, _, err := detectScope(optsFor(dir, "brickkit.prod.yaml"))
		require.NoError(t, err)
		assert.Equal(t, skills.ScopeProject, scope)
	})

	t.Run("两者都没有报 PROJECT_MISSING", func(t *testing.T) {
		dir := t.TempDir()
		_, layout, err := detectScope(optsFor(dir, DefaultConfigFile))
		require.Error(t, err)
		e := clierr.As(err)
		assert.Equal(t, clierr.CodeProjectMissing, e.Code)
		assert.Contains(t, e.Message, "既不是 BrickKit 项目，也不是组件仓库")
		assert.Equal(t, dir, layout.Root, "出错时 Layout 仍然有效")
	})
}
