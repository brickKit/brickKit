package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/skills"
)

func TestSkillsStatusOnFreshProject(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "--name", "p", "--yes", "--no-skills").code)

	r := runIn(t, dir, "skills", "status")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "missing")
	assert.Contains(t, r.stdout, "brickkit-assemble")
}

// 不带子命令时等于 status——只读是安全的默认。
func TestSkillsBareIsStatus(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "--name", "p", "--yes", "--no-skills").code)

	r := runIn(t, dir, "skills")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "missing")

	_, err := os.Stat(filepath.Join(dir, ".claude"))
	assert.True(t, os.IsNotExist(err), "光看状态不该写文件")
}

func TestSkillsUpdateInstallsThenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "--name", "p", "--yes", "--no-skills").code)

	r := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, r.code, r.stderr)
	_, err := os.Stat(filepath.Join(dir, ".claude", "skills", "brickkit-assemble", "SKILL.md"))
	require.NoError(t, err)

	again := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, again.code, again.stderr)
	assert.Contains(t, again.stdout, "up to date")
}

func TestSkillsUpdateSkipsModifiedAndSaysHow(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "--name", "p", "--yes").code)

	p := filepath.Join(dir, ".claude", "skills", "brickkit-assemble", "SKILL.md")
	installed, err := os.ReadFile(p)
	require.NoError(t, err)
	mine := append([]byte("我改过了\n"), installed...) // 改了正文、记录行还在：已手改
	require.NoError(t, os.WriteFile(p, mine, 0o644))

	r := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "hand-edited")
	assert.Contains(t, r.stdout, "delete", "要告诉人怎么放弃本地修改")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after))
}

// status 要能说出是从哪个版本升上来的。
func TestSkillsStatusShowsOutdatedWithVersions(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "--name", "p", "--yes").code)

	// 伪造一份「上个版本写的」技能：内容与 lock 记录一致、与资产不同。
	old := []byte("上个版本的技能\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude", "skills", "brickkit-assemble", "SKILL.md"), old, 0o644))
	lockPath := filepath.Join(dir, ".brickkit", "skills.lock")
	require.NoError(t, os.WriteFile(lockPath, []byte(
		`{"entries":[{"path":".claude/skills/brickkit-assemble/SKILL.md","version":"0.0.1","sum":"`+
			sumOf(old)+`"}]}`+"\n"), 0o644))

	r := runIn(t, dir, "skills", "status")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "outdated")
	assert.Contains(t, r.stdout, "0.0.1")
	assert.Contains(t, r.stdout, "1 file needs refreshing")
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

// 独立组件仓库（一个组件一个仓库，通常没有 brickkit.yaml）：只装 brickkit-component 这一份技能，
// 不装拼装/部署/排障三个项目层面的技能；AGENTS.md 是组件自己的（代码地图那五节），不是项目导读。
func TestSkillsInComponentRepoManagesTheComponentSkillAndItsOwnGuide(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, comp{ID: "people/basic", Version: "1.0.0"}.files())

	st := runIn(t, dir, "skills", "status")
	require.Equal(t, 0, st.code, st.stderr)
	assert.Contains(t, st.stdout, ".claude/skills/brickkit-component/SKILL.md")
	assert.Contains(t, st.stdout, "missing")
	assert.Contains(t, st.stdout, "Component repository", "要说明这是组件仓库模式")
	assert.NotContains(t, st.stdout, "brickkit-deploy")

	up := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, up.code, up.stderr)
	_, err := os.Stat(filepath.Join(dir, ".claude", "skills", "brickkit-component", "SKILL.md"))
	require.NoError(t, err)
	agents := readFile(t, filepath.Join(dir, "AGENTS.md"))
	assert.True(t, strings.HasPrefix(agents, "# people/basic\n"))
	assert.Contains(t, agents, "## Code map", "组件自己的导读，不是项目那四节")
	assert.NotContains(t, agents, "## Overview")
	assert.Equal(t, "@AGENTS.md\n", readFile(t, filepath.Join(dir, "CLAUDE.md")))
	_, err = os.Stat(filepath.Join(dir, ".claude", "skills", "brickkit-assemble"))
	assert.True(t, os.IsNotExist(err))

	again := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, again.code, again.stderr)
	assert.Contains(t, again.stdout, "up to date")
}

// 组件仓库多半有自己的 AGENTS.md：作者写的一个字节都不动；skills update 是明确要求，
// 只在末尾追加 brickkit 维护的那一段。
func TestSkillsInComponentRepoKeepsOwnAgentsMdAndAppendsTheBlock(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, comp{ID: "people/basic", Version: "1.0.0"}.files())
	mine := "# 这个组件自己的说明\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(mine), 0o644))

	r := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, r.code, r.stdout+r.stderr)

	after := readFile(t, filepath.Join(dir, "AGENTS.md"))
	assert.True(t, strings.HasPrefix(after, mine), "作者写的内容被改了")
	assert.Contains(t, after, "<!-- brickkit:managed:begin lang=")
	assert.Contains(t, r.stdout, "appended the brickkit-maintained block")
}

// 目录里既有 brickkit.yaml 又有 component.yaml 时按项目处理，和以前一样。
func TestSkillsPrefersProjectWhenBothFilesPresent(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "--name", "p", "--yes", "--no-skills").code)
	writeTree(t, dir, comp{ID: "people/basic", Version: "1.0.0"}.files())

	r := runIn(t, dir, "skills", "status")
	require.Equal(t, 0, r.code, r.stderr)
	assert.Contains(t, r.stdout, "brickkit-deploy", "按项目处理：完整的一套")
	assert.NotContains(t, r.stdout, "Component repository")
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
	optsFor := func(dir string) *Options { return &Options{WorkDir: dir} }

	t.Run("只有 brickkit.yaml 是项目", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "brickkit.yaml")
		scope, layout, err := detectScope(optsFor(dir))
		require.NoError(t, err)
		assert.Equal(t, skills.ScopeProject, scope)
		assert.Equal(t, dir, layout.Root)
	})

	t.Run("只有 component.yaml 是组件仓库", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "component.yaml")
		scope, _, err := detectScope(optsFor(dir))
		require.NoError(t, err)
		assert.Equal(t, skills.ScopeComponent, scope)
	})

	t.Run("两者都有时按项目算", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "brickkit.yaml", "component.yaml")
		scope, _, err := detectScope(optsFor(dir))
		require.NoError(t, err)
		assert.Equal(t, skills.ScopeProject, scope)
	})

	t.Run("两者都没有报 PROJECT_MISSING", func(t *testing.T) {
		dir := t.TempDir()
		_, layout, err := detectScope(optsFor(dir))
		require.Error(t, err)
		e := clierr.As(err)
		assert.Equal(t, clierr.CodeProjectMissing, e.Code)
		assert.Contains(t, e.Message, "neither a BrickKit project nor a component repository")
		assert.Equal(t, dir, layout.Root, "出错时 Layout 仍然有效")
	})
}

// skills update --lang 换的是项目选定的语言：技能换一套，AGENTS.md 维护区跟着换，作者写的部分一个字不动。
func TestSkillsUpdateLangSwitchesTheBlock(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "--name", "p", "--yes").code)
	before := readFile(t, filepath.Join(dir, "AGENTS.md"))
	authored := before[:strings.Index(before, "<!-- brickkit:managed:begin")]

	r := runIn(t, dir, "skills", "update", "--lang", "zh")
	require.Equal(t, 0, r.code, r.stdout+r.stderr)
	after := readFile(t, filepath.Join(dir, "AGENTS.md"))
	assert.True(t, strings.HasPrefix(after, authored))
	assert.Contains(t, after, "lang=zh")
	assert.Contains(t, after, "## 组件")

	st := runIn(t, dir, "skills", "status")
	assert.Contains(t, st.stdout, "Skill language: zh")
	assert.Contains(t, st.stdout, "block maintained by brickkit present (lang=zh)")
}

// 同事新克隆：.brickkit/ 不进 Git，克隆里没有它——技能文件自己带着记录，照样认得出、升得上去。
func TestSkillsFreshCloneWithoutBrickkitDir(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "--name", "p", "--yes").code)
	require.NoError(t, os.RemoveAll(filepath.Join(dir, ".brickkit")))

	st := runIn(t, dir, "skills", "status")
	require.Equal(t, 0, st.code, st.stderr)
	assert.NotContains(t, st.stdout, "untracked")
	assert.NotContains(t, st.stdout, "need refreshing")
	assert.Contains(t, st.stdout, "block maintained by brickkit present (lang=en)")
}

// 本次改动之前建的老项目，在一台没有旧 skills.lock 的机器上（同事的克隆）：旧版 CLI 装的 AGENTS.md 与技能
// 照样认得出——AGENTS.md 整份换成新骨架，技能升到当前版本，而不是一直卡在"未托管"。
func TestSkillsUpdateMigratesAnOldProjectWithoutTheLock(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "--name", "p", "--yes", "--no-skills").code)
	oldAgents, err := os.ReadFile(filepath.Join("..", "skills", "testdata", "legacy", "AGENTS.md"))
	require.NoError(t, err)
	oldSkill, err := os.ReadFile(filepath.Join("..", "skills", "testdata", "legacy", "assemble-SKILL.md"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), oldAgents, 0o644))
	writeTree(t, dir, map[string]string{".claude/skills/brickkit-assemble/SKILL.md": string(oldSkill)})

	r := runIn(t, dir, "skills", "update")
	require.Equal(t, 0, r.code, r.stdout+r.stderr)
	agents := readFile(t, filepath.Join(dir, "AGENTS.md"))
	assert.Contains(t, agents, "## Overview", "the old CLI-installed guide became the new skeleton")
	assert.NotContains(t, r.stdout, "untracked")
	st := runIn(t, dir, "skills", "status")
	assert.NotContains(t, st.stdout, "untracked")
	assert.NotContains(t, st.stdout, "need refreshing")
}

// CLI 升级后维护段里的平台规则变了：status 说它过期了、update 会刷新，而不是一直说"在"。
func TestSkillsStatusSaysWhenTheBlockIsOutdated(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, comp{ID: "people/basic", Version: "1.0.0"}.files()) // 它的维护段是空的：与当前 CLI 写的不一样
	st := runIn(t, dir, "skills", "status")
	require.Equal(t, 0, st.code, st.stderr)
	assert.Contains(t, st.stdout, "outdated; update refreshes it")

	require.Equal(t, 0, runIn(t, dir, "skills", "update").code)
	st = runIn(t, dir, "skills", "status")
	assert.Contains(t, st.stdout, "block maintained by brickkit present (lang=en)")
	assert.NotContains(t, st.stdout, "need refreshing")
}
