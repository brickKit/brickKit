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

// writeBlock 写一份带维护区的 AGENTS.md：项目选定的语言记在那里（由 skills update 写，这里模拟）。
func writeBlock(t *testing.T, root, lang string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"),
		[]byte("# P\n\n<!-- brickkit:managed:begin lang="+lang+" -->\n<!-- brickkit:managed:end -->\n"), filePerm))
}

func legacyLock(t *testing.T, in *Installer, l *Lock) {
	t.Helper()
	in.LegacyLockPath = filepath.Join(in.Root, ".brickkit", "skills.lock")
	require.NoError(t, l.Save(in.LegacyLockPath))
}

// 显式指定的语言（brickkit init 传当前 CLI 语言、skills update --lang 传参数值）优先于一切。
func TestExplicitLangWinsOverBlock(t *testing.T) {
	in := newInstallerWithLang(t, i18n.ZH)
	writeBlock(t, in.Root, "en")
	res, err := in.Apply()
	require.NoError(t, err)
	assert.Equal(t, i18n.ZH, res.Lang)

	a := AssetsFor(ScopeProject, i18n.ZH)[0]
	want, err := a.Content()
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(in.Root, a.Target))
	require.NoError(t, err)
	body, _, _, _ := ReadMarker(got)
	assert.Equal(t, string(normalize(want)), string(body))
}

// 没有显式指定时，沿用 AGENTS.md 维护区记的语言——不随运行 CLI 的语言变，
// 这样两个语言不同的队友才不会把提交进仓库的技能文件改来改去。
func TestUnspecifiedLangFollowsBlock(t *testing.T) {
	in := newInstallerWithLang(t, i18n.ZH)
	_, err := in.Apply()
	require.NoError(t, err)
	writeBlock(t, in.Root, "zh")

	// 另一台机器、当前 CLI 语言是英文，再看一次状态
	again := Installer{Root: in.Root, Version: "0.2.0"}
	list, err := again.Status()
	require.NoError(t, err)
	for _, s := range list {
		assert.Equal(t, StateCurrent, s.State, s.Target)
	}
}

// 还没迁移的项目：旧 lock 里有旧条目、但没有 Lang 字段，那批资产历史上只有中文，回退到 zh。
func TestLegacyLockWithoutLangFallsBackToZh(t *testing.T) {
	in := newInstaller(t)
	a := AssetsFor(ScopeProject, i18n.ZH)[0]
	want, err := a.Content()
	require.NoError(t, err)
	writeAt(t, in, a.Target, want)
	l := &Lock{}
	l.Set(LockEntry{Path: a.Target, Version: "0.0.1", Sum: Sum(want)})
	legacyLock(t, &in, l) // 没有 Lang 字段：模拟更早的版本写的 lock

	lang, err := in.ResolvedLang()
	require.NoError(t, err)
	assert.Equal(t, i18n.ZH, lang)
	assert.Equal(t, StateOutdated, stateOf(t, in, a.Target).State, "内容就是中文那份，补上记录")
}

// 全新项目（没有指定语言、没有维护区、也没有旧 lock）：用当前 CLI 语言，
// 等价于"就当现在装一份"——这正是 brickkit init 想要的效果。
func TestFreshProjectUsesCurrentCLILanguage(t *testing.T) {
	prev := i18n.Current()
	defer i18n.SetCurrent(prev)
	i18n.SetCurrent(i18n.ZH)

	in := newInstaller(t)
	res, err := in.Apply()
	require.NoError(t, err)
	require.NotEmpty(t, res.Written)
	assert.Equal(t, i18n.ZH, res.Lang)
}

// 切换语言：磁盘上未被手改的文件天然判「待更新」，被手改的文件天然判
// 「已手改」并跳过——状态机本身不需要知道"语言"这回事，见 Apply 的注释。
func TestSwitchingLangUpdatesUnmodifiedFilesButSkipsHandEdits(t *testing.T) {
	in := newInstallerWithLang(t, i18n.ZH)
	_, err := in.Apply()
	require.NoError(t, err)

	// [0] 是 brickkit-assemble（排序后最先），这里特意挑另一份来手改，
	// 好让 brickkit-assemble 本身留着验证"没手改的文件确实被换成了新语言"。
	handEditedTarget := AssetsFor(ScopeProject, i18n.ZH)[1].Target
	p := filepath.Join(in.Root, handEditedTarget)
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	mine := append([]byte("我改过这一份\n"), data...)
	require.NoError(t, os.WriteFile(p, mine, filePerm))

	switched := newInstallerWithLang(t, i18n.EN)
	switched.Root = in.Root
	res, err := switched.Apply()
	require.NoError(t, err)
	assert.NotContains(t, res.Written, handEditedTarget, "手改过的文件不该被语言切换覆盖")
	assert.Contains(t, res.Written, assemble)

	after, err := os.ReadFile(filepath.Join(in.Root, filepath.FromSlash(assemble)))
	require.NoError(t, err)
	en, err := assetByTarget(i18n.EN, assemble).Content()
	require.NoError(t, err)
	body, _, _, _ := ReadMarker(after)
	assert.Equal(t, string(normalize(en)), string(body))

	editedAfter, err := os.ReadFile(p)
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

// 维护区或旧 lock 里塞了不认识的语言代码时，回退到下一层，而不是让资产列表变空。
func TestResolveLangIgnoresUnrecognizedValues(t *testing.T) {
	in := newInstaller(t)
	l := &Lock{Lang: "not-a-language"}
	l.Set(LockEntry{Path: assemble, Version: "0.1.0", Sum: "sha256:whatever"})
	legacyLock(t, &in, l)
	writeBlock(t, in.Root, "xx")

	list, err := in.Status()
	require.NoError(t, err)
	require.NotEmpty(t, list, "不认识的语言代码要回退，而不是让资产列表变空")
}

// 登记了、却还没有技能资产的语言：装源语言的资产，报出实际装的语言，而不是报错或什么都不装。
// （没登记的代码 xx 代替"登记了但没资产"：AssetLang 只看内嵌的资产目录。）
func TestAssetsFallBackToSourceLanguage(t *testing.T) {
	assert.Equal(t, i18n.Lang("zh"), AssetLang("zh"))
	assert.Equal(t, i18n.SourceLang(), AssetLang("xx"))
	assert.NotEmpty(t, Assets("xx"))
	assert.Equal(t, len(Assets(i18n.SourceLang())), len(Assets("xx")))
}

func TestInstallingALanguageWithoutAssetsInstallsTheSourceLanguage(t *testing.T) {
	in := newInstallerWithLang(t, "xx")
	res, err := in.Apply()
	require.NoError(t, err)
	assert.NotEmpty(t, res.Written)
	assert.Equal(t, i18n.SourceLang(), res.Lang, "Apply 报出实际装的语言，调用方据此告诉使用者")

	for _, a := range AssetsFor(ScopeProject, i18n.SourceLang()) {
		want, err := a.Content()
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(in.Root, a.Target))
		require.NoError(t, err)
		body, _, _, _ := ReadMarker(got)
		assert.Equal(t, string(normalize(want)), string(body), a.Target)
	}
	lang, err := in.ResolvedLang()
	require.NoError(t, err)
	assert.Equal(t, i18n.SourceLang(), lang, "skills status 报的也是实际装的语言")
}
