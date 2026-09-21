package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

// 这个文件测的是 resolveLang 那张优先级表，以及"切换语言不需要状态机知道
// 语言这回事"那条设计（见 install.go 的 Apply 注释）。install_test.go 那批
// 测试不碰语言，一律用 newInstaller 的默认解析结果（全新项目 → 当前 CLI
// 语言，测试进程里就是 i18n.EN）。

func newInstallerWithLang(t *testing.T, lang i18n.Lang) Installer {
	t.Helper()
	in := newInstaller(t)
	in.Lang = lang
	return in
}

// 显式指定的语言（brickkit init 传当前 CLI 语言、skills update --lang 传参数值）
// 优先于一切——哪怕 lock 已经记了别的语言。
func TestExplicitLangWinsOverLock(t *testing.T) {
	in := newInstallerWithLang(t, i18n.ZH)
	_, err := in.Apply()
	require.NoError(t, err)

	l, err := LoadLock(in.LockPath)
	require.NoError(t, err)
	assert.Equal(t, "zh", l.Lang)

	want, err := AssetsFor(ScopeProject, i18n.ZH)[0].Content()
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(in.Root, AssetsFor(ScopeProject, i18n.ZH)[0].Target))
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

// 没有显式指定时，沿用 lock 里已经记的语言——不随运行 CLI 的语言变，
// 这样两个语言不同的队友才不会把提交进仓库的技能文件改来改去。
func TestUnspecifiedLangFollowsLock(t *testing.T) {
	in := newInstallerWithLang(t, i18n.ZH)
	_, err := in.Apply()
	require.NoError(t, err)

	// 换一个不带 Lang 的 Installer 指向同一个项目，模拟"另一台机器、
	// 当前 CLI 语言是英文"再跑一次 skills update。
	again := Installer{Root: in.Root, LockPath: in.LockPath, Version: "0.2.0"}
	list, err := again.Status()
	require.NoError(t, err)
	for _, s := range list {
		assert.Equal(t, StateCurrent, s.State, s.Target)
	}
}

// lock 里有旧条目、但没有 Lang 字段：这是早于这个字段存在的项目，
// 那批资产历史上只有中文，回退到 zh 而不是当场改语言。
func TestLegacyLockWithoutLangFallsBackToZh(t *testing.T) {
	in := newInstaller(t)
	want, err := AssetsFor(ScopeProject, i18n.ZH)[0].Content()
	require.NoError(t, err)
	target := AssetsFor(ScopeProject, i18n.ZH)[0].Target
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(in.Root, target)), dirPerm))
	require.NoError(t, os.WriteFile(filepath.Join(in.Root, target), want, filePerm))

	l := &Lock{}
	l.Set(LockEntry{Path: target, Version: "0.0.1", Sum: Sum(want)})
	require.NoError(t, l.Save(in.LockPath)) // 没有 Lang 字段：模拟旧版本写的 lock

	st := stateOf(t, in, target)
	assert.Equal(t, StateCurrent, st.State, "回退到 zh 之后，磁盘上的旧中文内容应判「最新」")
}

// 全新项目（没有指定语言、也没有任何 lock 记录）：用当前 CLI 语言，
// 等价于"就当现在装一份"——这正是 brickkit init 想要的效果。
func TestFreshProjectWithNoLockUsesCurrentCLILanguage(t *testing.T) {
	prev := i18n.Current()
	defer i18n.SetCurrent(prev)
	i18n.SetCurrent(i18n.ZH)

	in := newInstaller(t) // 不显式指定 Lang
	res, err := in.Apply()
	require.NoError(t, err)
	require.NotEmpty(t, res.Written)

	l, err := LoadLock(in.LockPath)
	require.NoError(t, err)
	assert.Equal(t, "zh", l.Lang)
}

// 切换语言：磁盘上未被手改的文件天然判「待更新」，被手改的文件天然判
// 「已手改」并跳过——状态机本身不需要知道"语言"这回事，见 Apply 的注释。
func TestSwitchingLangUpdatesUnmodifiedFilesButSkipsHandEdits(t *testing.T) {
	in := newInstallerWithLang(t, i18n.ZH)
	_, err := in.Apply()
	require.NoError(t, err)

	// [0] 是 AGENTS.md（排序后最先），这里特意挑一份 SKILL.md 来手改，
	// 好让 AGENTS.md 本身留着验证"没手改的文件确实被换成了新语言"。
	handEditedTarget := AssetsFor(ScopeProject, i18n.ZH)[1].Target
	mine := []byte("我改过这一份\n")
	require.NoError(t, os.WriteFile(filepath.Join(in.Root, handEditedTarget), mine, filePerm))

	switched := newInstallerWithLang(t, i18n.EN)
	switched.Root, switched.LockPath = in.Root, in.LockPath

	res, err := switched.Apply()
	require.NoError(t, err)
	assert.NotContains(t, res.Written, handEditedTarget, "手改过的文件不该被语言切换覆盖")
	assert.Contains(t, res.Written, "AGENTS.md")

	after, err := os.ReadFile(filepath.Join(in.Root, "AGENTS.md"))
	require.NoError(t, err)
	enAgents, err := assetByTarget(i18n.EN, "AGENTS.md").Content()
	require.NoError(t, err)
	assert.Equal(t, string(enAgents), string(after))

	l, err := LoadLock(in.LockPath)
	require.NoError(t, err)
	assert.Equal(t, "en", l.Lang, "切换之后 lock 要记住新语言")

	editedAfter, err := os.ReadFile(filepath.Join(in.Root, handEditedTarget))
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(editedAfter), "手改的内容不能被语言切换动到")
}

func assetByTarget(lang i18n.Lang, target string) Asset {
	for _, a := range Assets(lang) {
		if a.Target == target {
			return a
		}
	}
	return Asset{}
}

// 无效的语言值（比如 --lang fr 解析出来的空 i18n.Lang，不该传进来；
// 这里守的是 resolveLang 对"lock.Lang 是脏数据"的容错——旧的、损坏的
// 或手改过的 lock 文件里塞了一个不认识的语言代码时，回退到下一层，
// 而不是让 AssetsFor 拿着一个不存在的语言目录空手而归）。
func TestResolveLangIgnoresUnrecognizedLockValue(t *testing.T) {
	in := newInstaller(t)
	l := &Lock{Lang: "fr"}
	l.Set(LockEntry{Path: "AGENTS.md", Version: "0.1.0", Sum: "sha256:whatever"})
	require.NoError(t, l.Save(in.LockPath))

	list, err := in.Status()
	require.NoError(t, err)
	require.NotEmpty(t, list, "不认识的语言代码要回退，而不是让资产列表变空")
}
