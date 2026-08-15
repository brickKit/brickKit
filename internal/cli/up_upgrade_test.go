// 本文件是 Step 15-C 的升级路径测试（004 §3.5.1、002 §7.7）。
// 回填 P10（升级时拉新版本 Manifest 与产物）与 P15（CheckUpgrade 接线）。
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
	assert.Contains(t, r.stdout, "升级")
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
// P15 CheckUpgrade 接线（002 §7.7）
// ============================================================

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
	assert.Contains(t, r.stdout, "升级变更摘要")
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
