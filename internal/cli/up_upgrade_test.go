// 本文件是 Step 15-C 的升级路径测试（004 §3.5.1、002 §7.7）。
// 回填 P10（升级时拉新版本 Manifest 与产物）。
//
// "升级"在 BrickKit 里就是一件事：把 brickkit.yaml 里的版本号改了，然后 up。
// 所以这些用例都长这样：先装 1.0.0 跑一次，再把版本号改成 1.1.0 再跑一次。
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// upgradableProject 装好 1.0.0，安装源里同时备着 1.1.0。
func upgradableProject(t *testing.T, newVersion comp) *projectFixture {
	t.Helper()

	comps := []comp{
		{ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}},
		newVersion,
	}
	// add 会把 1.0.0 的 Manifest 与产物落进缓存——这正是升级前的状态
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
`)
	return f
}

// bumpTo 把 brickkit.yaml 里的版本号改掉——这就是使用者做的全部动作。
func bumpTo(t *testing.T, f *projectFixture, version string) {
	t.Helper()
	f.writeConfig(t, "components:\n  - id: people/basic\n    version: "+version+"\n")
}

// ============================================================
// P10 升级时拉新版本 Manifest 与产物
// ============================================================

func TestUpgradeDownloadsNewVersionArtifacts(t *testing.T) {
	f := upgradableProject(t, comp{
		ID: "people/basic", Version: "1.1.0", Artifacts: []string{"api-docs:openapi.json"},
	})
	bumpTo(t, f, "1.1.0")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.FileExists(t,
		filepath.Join(f.Dir, ".brickkit", "manifests", "people-basic-1.1.0.yaml"),
		"P10：新版本 Manifest 要落进缓存")
	assert.FileExists(t,
		filepath.Join(f.Dir, ".brickkit", "artifacts", "people-basic-1-1-0",
			"api-docs", "openapi.json"),
		"P10：新版本的产物要下载到新版本化服务名目录下")
}

// 旧版本的产物保留：调用方可能还指着旧版本（002 §7.8、开发计划 38.4）。
func TestUpgradeKeepsOldVersionArtifacts(t *testing.T) {
	f := upgradableProject(t, comp{
		ID: "people/basic", Version: "1.1.0", Artifacts: []string{"api-docs:openapi.json"},
	})
	bumpTo(t, f, "1.1.0")

	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), f.Dir, "up").code)

	assert.FileExists(t,
		filepath.Join(f.Dir, ".brickkit", "artifacts", "people-basic-1-0-0",
			"api-docs", "openapi.json"),
		"旧版本产物不该被清掉")
}

// 升级要说出来：使用者改了个版本号，得看到平台确实按升级处理了。
func TestUpgradeIsReported(t *testing.T) {
	f := upgradableProject(t, comp{ID: "people/basic", Version: "1.1.0"})
	bumpTo(t, f, "1.1.0")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "检测到版本变更")
	assert.Contains(t, r.stdout, "1.0.0")
	assert.Contains(t, r.stdout, "1.1.0")
}

// 版本号没动就不该冒出升级的输出。
func TestNoUpgradeMessageWhenVersionUnchanged(t *testing.T) {
	f := upgradableProject(t, comp{ID: "people/basic", Version: "1.1.0"})

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "升级")
}

// 首次安装不是升级：缓存里本来就没有这个组件的任何版本。
func TestFirstInstallIsNotAnUpgrade(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "升级")
}

// ============================================================
// 002 §7.7 升级时的五项检查
//
// 这五项没有专门的"升级检查"入口——它们本来就在常规 up 路径上（解析拿不到
// Manifest 就报错、强依赖缺失报错、弱依赖缺失警告、循环依赖报错、资源未绑定
// 报错）。升级路径从前另跑一遍 resolver.CheckUpgrade，是同一套判断的第二份
// 拷贝，而且复制得不完整（--dry-run 不降级、不过滤 enabled: false）。
//
// 那份拷贝已删除。这里的用例改为走**真正在跑的那条路**：改版本号、up、看结果。
// ============================================================

// 检查项 1：新版本的 Manifest 取不到 → 报错阻断。
func TestUpgradeToUnavailableVersionIsBlocked(t *testing.T) {
	f := upgradableProject(t, comp{ID: "people/basic", Version: "1.1.0"})
	bumpTo(t, f, "9.9.9") // 安装源里没有这个版本
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code, "002 §7.7 检查项 1")
	assert.Contains(t, r.stderr, "people/basic@9.9.9")
	assert.Empty(t, eng.ups, "取不到就别启动")
}

// 检查项 5：新版本引入了循环依赖 → 报错阻断。
func TestUpgradeIntroducingACycleIsBlocked(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0"},
		// 1.1.0 依赖 department/tree，而 department/tree 又反过来依赖它
		{ID: "people/basic", Version: "1.1.0", Requires: []string{"department/tree@1.0.0"}},
		{ID: "department/tree", Version: "1.0.0", Requires: []string{"people/basic@1.1.0"}},
	}
	f := addedProject(t, comps, "people/basic@1.0.0")
	bumpTo(t, f, "1.1.0")
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code, "002 §7.7 检查项 5")
	assert.Contains(t, r.stderr, "循环依赖")
	assert.Empty(t, eng.ups)
}

// 新版本引入了一个取不到的强依赖 → 报错阻断，且不启动任何东西。
func TestUpgradeWithUnsatisfiableDependencyIsBlocked(t *testing.T) {
	f := upgradableProject(t, comp{
		ID: "people/basic", Version: "1.1.0", Requires: []string{"infra/vault@9.9.9"},
	})
	bumpTo(t, f, "1.1.0")
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code, "002 §7.7 检查项 2")
	assert.Contains(t, r.stderr, "infra/vault")
	assert.Empty(t, eng.ups, "升级检查没过就别启动")
}

// 新版本新增了弱依赖但取不到 → 警告，照常启动（002 §7.7 检查项 4）。
func TestUpgradeWithMissingWeakDependencyOnlyWarns(t *testing.T) {
	f := upgradableProject(t, comp{
		ID: "people/basic", Version: "1.1.0", Optional: []string{"infra/bus@1.0.0"},
	})
	bumpTo(t, f, "1.1.0")
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout+r.stderr, "infra/bus")
	assert.NotEmpty(t, eng.ups, "弱依赖缺失不阻断")
}

// ============================================================
// 004 §3.5.1 --dry-run 的升级变更摘要
// ============================================================

func TestDryRunShowsUpgradeSummary(t *testing.T) {
	f := upgradableProject(t, comp{
		ID: "people/basic", Version: "1.1.0",
		Migration: []string{"python", "manage.py", "migrate"},
	})
	bumpTo(t, f, "1.1.0")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "版本变更摘要")
	assert.Contains(t, r.stdout, "people/basic: 1.0.0 → 1.1.0")
	assert.Contains(t, r.stdout, "数据库迁移", "新版本声明了迁移，要提醒")
	assert.Contains(t, r.stdout, "python manage.py migrate")
}

// TestUpgradeSummaryIgnoresSidecarCacheFiles 是一条回归。
//
// .brickkit/manifests/ 里除了 Manifest 本身，还躺着签名缓存
// （people-basic-1.0.0.sig.json，Step 20 引入）。扫描这个目录找"已缓存的版本"
// 时如果不筛扩展名，去掉一层 .json 会得到 people-basic-1.0.0.sig，
// "版本号"就成了 1.0.0.sig，一路显示到升级摘要里：
//
//	people/basic: 1.0.0.sig → 1.1.0
//
// 这正是加签名缓存那天真跑出来的现象。目录里将来还会放别的东西，
// 所以筛的是"只认 .yaml"，不是"排掉 .sig.json"。
func TestUpgradeSummaryIgnoresSidecarCacheFiles(t *testing.T) {
	f := upgradableProject(t, comp{ID: "people/basic", Version: "1.1.0"})
	bumpTo(t, f, "1.1.0")

	// 手动放一个签名缓存，模拟这个版本当初是带签名装进来的
	sidecar := filepath.Join(f.Layout.ManifestsDir(), "people-basic-1.0.0.sig.json")
	require.NoError(t, os.WriteFile(sidecar,
		[]byte(`{"sourceKind":"market","signature":{"algorithm":"cosign"}}`), 0o644))

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "people/basic: 1.0.0 → 1.1.0")
	assert.NotContains(t, r.stdout, "1.0.0.sig", "缓存旁边的文件不是版本号")
}

// 新版本没有迁移时，摘要里要如实说"无"，而不是留一个空行让人猜。
func TestUpgradeSummaryWithoutMigration(t *testing.T) {
	f := upgradableProject(t, comp{ID: "people/basic", Version: "1.1.0"})
	bumpTo(t, f, "1.1.0")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "数据库迁移：无")
}

// 改回一个已经装过的版本：该拉的都在本地了，不再重复走升级流程，
// 但必须照常按新配置启动那个版本。
//
// 判据是"这个版本的东西本地有没有"，不是"版本号有没有变过"——
// CLI 不持有运行时状态（004 §1.3），它记不住上一次跑的是哪个版本。
func TestSwitchingBackToAnInstalledVersionStartsIt(t *testing.T) {
	f := upgradableProject(t, comp{ID: "people/basic", Version: "1.1.0"})
	bumpTo(t, f, "1.1.0")
	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), f.Dir, "up").code)

	bumpTo(t, f, "1.0.0")
	eng := newFakeEngine()
	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"people-basic-1-0-0"}, eng.lastUp(t).Services)
	assert.NotContains(t, r.stdout, "升级", "本地都有，没有要拉的东西")
}

// 缓存目录被清空时不该把每个组件都当成升级。
func TestMissingCacheIsNotTreatedAsUpgrade(t *testing.T) {
	f := upgradableProject(t, comp{ID: "people/basic", Version: "1.1.0"})
	require.NoError(t, os.RemoveAll(filepath.Join(f.Dir, ".brickkit", "manifests")))

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "升级")
}

// ============================================================
// 基线：从哪个版本升上来
// ============================================================

// versionedProject 备好同一个组件的若干版本，装上第一个。
func versionedProject(t *testing.T, versions ...string) *projectFixture {
	t.Helper()
	comps := make([]comp, 0, len(versions))
	for _, v := range versions {
		comps = append(comps, comp{ID: "people/basic", Version: v,
			ConfigSchema: []string{"logLevel:info"}})
	}
	f := addedProject(t, comps, "people/basic@"+versions[0])
	bumpTo(t, f, versions[0])
	return f
}

// 连续第二次升级，基线要取"上一个"，不是缓存里字典序最前的那个。
//
// 判据从前是"缓存里有别的版本"，from 取 os.ReadDir 顺序的第一个——那是文件名的
// 字典序，与"当前跑的是哪个"毫无关系。1.0.0 → 2.0.0 → 3.0.0 走完，缓存里躺着
// {1.0.0, 2.0.0}，于是第二次升级被报成 "1.0.0 → 3.0.0"，六项摘要也拿 1.0.0
// 当基线去 diff——2.0.0 早就有的配置项被报成"新增"。
//
// 那六行是使用者决定要不要升的唯一依据，说假话比不说更糟。
func TestSecondUpgradeReportsThePreviousVersion(t *testing.T) {
	f := versionedProject(t, "1.0.0", "2.0.0", "3.0.0")

	bumpTo(t, f, "2.0.0")
	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), f.Dir, "up").code)

	bumpTo(t, f, "3.0.0")
	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "2.0.0 → 3.0.0", "基线是上一个版本：%s", r.stdout)
	assert.NotContains(t, r.stdout, "1.0.0 → 3.0.0", "1.0.0 是上上个版本，不是基线")
}

// 加一个共存版本不是升级：配置里两个版本都还在，没有谁被换掉。
//
// 从前只要"缓存里有别的版本"就报升级，于是照 003 §8.3 加第二个版本条目
// （灰度迁移的标准写法）会看到一句"⬆️ 检测到版本变更"——
// 而使用者要的恰恰是两个一起跑。
func TestAddingACoexistingVersionIsNotAnUpgrade(t *testing.T) {
	f := versionedProject(t, "1.0.0", "2.0.0")

	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: people/basic
    version: 2.0.0
`)
	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "检测到版本变更",
		"两个版本都在配置里，谁都没被换掉：%s", r.stdout)
}

// 回退（2.0.0 → 1.0.0）也是一次版本变更，同样要报。
//
// 从前完全检测不到：判据是"配置里的版本不在缓存里"，而回退的目标恰恰在缓存里。
// 于是 005 §5.10 教的"改版本号重新 up"这条回退路径上，摘要一个字都没有。
func TestRollbackIsReported(t *testing.T) {
	f := versionedProject(t, "1.0.0", "2.0.0")

	bumpTo(t, f, "2.0.0")
	require.Equal(t, clierr.ExitOK, runWithEngine(t, newFakeEngine(), f.Dir, "up").code)

	bumpTo(t, f, "1.0.0")
	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "2.0.0 → 1.0.0", "回退同样要报出来：%s", r.stdout)
}

// ============================================================
// 升级路径不该有第二套判据
// ============================================================

// `--dry-run` 在升级时同样不阻断。
//
// 升级路径从前自己跑一遍 CheckUpgrade，里面的资源绑定检查是**无条件阻断**的，
// 而常规路径在 --dry-run 下降级成警告（004 §4.4）。同一份配置、同一个缺失，
// 只因为版本号变了就从"警告"变成"退出码 1"——而升级恰恰是最想先预览一下的时候。
func TestDryRunDoesNotBlockOnUnboundResourceDuringUpgrade(t *testing.T) {
	f := upgradableProject(t, comp{
		ID: "people/basic", Version: "1.1.0",
		ResourceDeps: []string{"database:postgres"},
	})
	bumpTo(t, f, "1.1.0")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code,
		"--dry-run 不该因为资源没绑就失败，升级时也一样：%s", r.stdout+r.stderr)
	assert.Contains(t, r.stdout+r.stderr, "资源依赖未满足", "但必须说出来")
}

// 升级一个 enabled: false 的组件不该被资源检查拦下。
//
// 006 §4.4 明写"只查本次会启动的组件"。升级路径从前无条件查目标组件，
// 于是它在让使用者给一个刚刚关掉的组件去配数据库。
func TestUpgradingADisabledComponentSkipsBindingCheck(t *testing.T) {
	f := upgradableProject(t, comp{
		ID: "people/basic", Version: "1.1.0",
		ResourceDeps: []string{"database:postgres"},
	})
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.1.0
    enabled: false
`)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code,
		"它这次根本不跑，不该逼人为它配资源：%s", r.stdout+r.stderr)
}

// ============================================================
// --dry-run 要说清"会不会动数据库"
// ============================================================

// 迁移提示从前在 --dry-run 的提前返回**之后**，于是 dry-run 里一个字都没有。
//
// "这次会动哪些库"恰恰是 --dry-run 最该回答的问题之一，而它与升不升级无关。
// 从前唯一会提到它的地方是升级摘要里那一行——还得先靠一个猜出来的基线才会出现。
func TestDryRunListsMigrations(t *testing.T) {
	f := addedProject(t, []comp{{
		ID: "people/basic", Version: "1.0.0",
		Migration: []string{"/app/people-basic", "migrate"},
	}}, "people/basic@1.0.0")
	f.writeConfig(t, "components:\n  - id: people/basic\n    version: 1.0.0\n")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "数据库迁移", "dry-run 必须说清会不会动数据库：%s", r.stdout)
	assert.Contains(t, r.stdout, "/app/people-basic migrate")
}
