package cli

// 本文件补齐开发计划 Step 38 里的 6 处真缺口。
//
// 其余 22 项逐条查证后都已有覆盖（38.13–38.16 甚至有专门的
// `TestUpgradeAddedConfigKeyUsesDefault` 等四条），这里只补真的没有的。
//
// # 一、升级摘要比设计书少了四行（38.18 / 38.19 / 38.21 / 38.22）
//
// 004 §3.5.1 规定 `--dry-run` 的摘要长这样：
//
//	📋 升级变更摘要：
//	   people/basic: 1.0.0 → 1.1.0
//	   ├── 依赖变更：无
//	   ├── 新增配置项：enableNotification（默认 true）
//	   ├── 删除配置项：无
//	   ├── 数据库迁移：python manage.py migrate
//	   ├── artifacts 变更：openapi.json 更新
//	   └── 资源配额变更：无
//
// 而实现只输出了版本行、数据库迁移、旧版本产物三行——**六项里缺四项**。
//
// 缺的偏偏是"升级到底会改变什么"这个问题本身：使用者按下 `--dry-run`
// 就是想在真动手之前知道这个。只告诉他"有迁移"，
// 等于让他自己去 diff 两份 Manifest——而他多半不知道旧的那份在缓存里。
//
// # 二、没有人断言"升级之后新版本真的跑起来了"（38.1）
//
// 现有升级测试验的是缓存落盘（`TestUpgradeDownloadsNewVersionArtifacts`）
// 与输出文案（`TestUpgradeIsReported`），**没有一条断言引擎收到的是新版本的服务**。
// 回滚方向反而有（`TestSwitchingBackToAnInstalledVersionStartsIt` 断言了
// `people-basic-1-0-0`），升级方向没有。
//
// 这条要是坏了，`TestUpgradeIsReported` 照样绿——它只看 stdout 里有没有那几个字。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// ============================================================
// 38.1 升级后新版本真的被启动
// ============================================================

// 改完版本号执行 up，引擎收到的必须是**新版本**的服务名。
//
// 服务名带版本（people-basic-1-1-0），所以这条断言同时排除了
// "还在跑旧版本"和"新旧都起来了"两种错法。
func TestUpgradeActuallyStartsNewVersion(t *testing.T) {
	f := upgradableProject(t, comp{ID: "people/basic", Version: "1.1.0"})
	bumpTo(t, f, "1.1.0")

	eng := newFakeEngine()
	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"people-basic-1-1-0"}, eng.lastUp(t).Services,
		"38.1：升级之后跑的必须是新版本——现有用例只验了缓存与文案，"+
			"这条要是坏了它们照样全绿")
}

// ============================================================
// 38.7 升级引入未绑定的资源 → 报错阻断
// ============================================================

// 新版本新增了一个资源依赖，而 brickkit.yaml 里没有绑定它。
//
// 38.6（强依赖不可满足）与 38.8（弱依赖缺失）都有升级路径上的用例，
// 唯独资源这条只有 resolver 层的通用测试。而它恰恰是升级时最容易撞上的：
// 新版本开始用数据库了，使用者只改了版本号。
func TestUpgradeWithUnboundResourceIsBlocked(t *testing.T) {
	f := upgradableProject(t, comp{
		ID: "people/basic", Version: "1.1.0",
		ResourceDeps: []string{"database:postgres"},
	})
	bumpTo(t, f, "1.1.0")

	eng := newFakeEngine()
	r := runWithEngine(t, eng, f.Dir, "up")

	require.NotEqual(t, clierr.ExitOK, r.code,
		"38.7：新版本要数据库而没人绑，必须在启动前拦下：%s", r.stdout)
	assert.Empty(t, eng.ups, "38.7：拦下了就不该启动任何东西")
}

// 同一条检查在**普通 up**上也必须成立（006 §4.4、011 §5.3）。
//
// CheckResourceBindings 长期只挂在升级路径上，于是"从来没升级过"的项目
// 一次也没被检查过：组件声明了 database 却没人绑，`up` 一路绿灯，
// 生成的 service 里一个 DATABASE_* 都没有，要到运行时才炸成"连不上库"。
// 这正是平台最反对的静默失败。
func TestUpWithUnboundResourceIsBlocked(t *testing.T) {
	f := addedProject(t, []comp{{
		ID: "people/basic", Version: "1.0.0",
		ResourceDeps: []string{"database:postgres"},
	}}, "people/basic@1.0.0")
	f.writeConfig(t, "components:\n  - id: people/basic\n    version: 1.0.0\n")

	eng := newFakeEngine()
	r := runWithEngine(t, eng, f.Dir, "up")

	require.NotEqual(t, clierr.ExitOK, r.code,
		"组件要数据库而没人绑，必须在启动前拦下：%s", r.stdout)
	assert.Contains(t, r.stderr+r.stdout, "people/basic", "要说清是哪个组件")
	assert.Empty(t, eng.ups, "拦下了就不该启动任何东西")
}

// `--dry-run` 只警告，不阻断。
//
// 那条命令的语义是"告诉我会发生什么"。拿它阻断的话，一个还没配资源的项目
// 连"看看会生成什么"都做不到——而试用指南 04 讲 enabled 三态时用的正是
// `up --only ... --dry-run`，那时资源还没登场。
func TestUpDryRunWarnsButDoesNotBlockOnUnboundResource(t *testing.T) {
	f := addedProject(t, []comp{{
		ID: "people/basic", Version: "1.0.0",
		ResourceDeps: []string{"database:postgres"},
	}}, "people/basic@1.0.0")
	f.writeConfig(t, "components:\n  - id: people/basic\n    version: 1.0.0\n")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code,
		"--dry-run 不该因为资源没绑就失败：%s", r.stdout+r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "资源依赖未满足", "但必须说出来：%s", out)
	assert.Contains(t, out, "people/basic", "要说清是哪个组件：%s", out)
}

// 不启动的组件不参与这条检查。
//
// 试用指南 02 §2.5 教的正是"用 enabled: false 把暂时不用的关掉，而不是删掉"，
// 那些组件的资源当然还没绑。拿它们去卡住 up，等于逼使用者要么删组件、
// 要么为一个根本不跑的容器编一份数据库配置。
func TestUpSkipsBindingCheckForComponentsThatDoNotStart(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "people/basic", Version: "1.0.0", ResourceDeps: []string{"database:postgres"}},
	}, "demo/hello@1.0.0", "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: people/basic
    version: 1.0.0
    enabled: false
`)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code,
		"被显式关掉的组件不该因为没绑资源而卡住整个项目：%s", r.stdout+r.stderr)
}

// ============================================================
// 38.18 / 38.19 / 38.21 / 38.22 升级摘要要说清"改了什么"
// ============================================================

// upgradeSummary 装好 old、把版本号改成 new，跑一次 --dry-run 并返回输出。
//
// 安装源里额外备着 department/tree——依赖变更那条用例要新增一个依赖，
// 而取不到的依赖会在算摘要之前就被拦下（002 §7.7）。
func upgradeSummary(t *testing.T, old, updated comp) string {
	t.Helper()

	// add old 会把它的 Manifest 落进缓存——这正是"升级前"的状态
	f := addedProject(t,
		[]comp{old, updated, {ID: "department/tree", Version: "1.0.0"}}, old.ref())
	f.writeConfig(t, "components:\n  - id: "+updated.ID+"\n    version: "+updated.Version+"\n")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	return r.stdout
}

// 38.18 依赖变更要报出来。
//
// 这是六项里最要紧的一条：新版本多依赖一个组件，意味着这次升级会
// **多起一个容器**。使用者有权在按下 up 之前知道。
func TestSummaryReportsDependencyChange(t *testing.T) {
	out := upgradeSummary(t,
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "1.1.0", Requires: []string{"department/tree@1.0.0"}})

	assert.Contains(t, out, "依赖变更", "38.18：%s", out)
	assert.Contains(t, out, "department/tree",
		"38.18：要点名多出来的是谁，只说'有变化'等于没说：%s", out)
}

// 依赖没变时也要如实说"无"，而不是把这一行藏起来。
//
// 藏起来会让人分不清"没有变化"和"平台没检查这一项"。
func TestSummaryReportsNoDependencyChange(t *testing.T) {
	out := upgradeSummary(t,
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "1.1.0"})

	assert.Contains(t, out, "依赖变更：无", "38.18：%s", out)
}

// 38.19 新增配置项要报出来，并带上默认值。
//
// 带默认值是因为：新增项走的是默认值（38.13），使用者要判断
// 这个默认值对不对，而不只是知道"多了个配置项"。
func TestSummaryReportsAddedConfigKey(t *testing.T) {
	out := upgradeSummary(t,
		comp{ID: "people/basic", Version: "1.0.0", ConfigSchema: []string{"greeting:你好"}},
		comp{ID: "people/basic", Version: "1.1.0",
			ConfigSchema: []string{"greeting:你好", "enableNotification:true"}})

	assert.Contains(t, out, "新增配置项", "38.19：%s", out)
	assert.Contains(t, out, "enableNotification", "38.19：%s", out)
	assert.Contains(t, out, "true",
		"38.19：要带上默认值——新增项走的就是它，使用者要判断这个值对不对：%s", out)
}

// 38.19 删除配置项要报出来。
//
// 这条比新增更值得说：删掉的配置项如果 brickkit.yaml 里还写着覆盖值，
// 那个覆盖会被**静默忽略**（38.14，有意如此）。不提醒的话，
// 使用者会以为自己的配置仍然生效。
func TestSummaryReportsRemovedConfigKey(t *testing.T) {
	out := upgradeSummary(t,
		comp{ID: "people/basic", Version: "1.0.0",
			ConfigSchema: []string{"greeting:你好", "legacyMode:off"}},
		comp{ID: "people/basic", Version: "1.1.0", ConfigSchema: []string{"greeting:你好"}})

	assert.Contains(t, out, "删除配置项", "38.19：%s", out)
	assert.Contains(t, out, "legacyMode", "38.19：%s", out)
}

// 38.21 artifacts 变更要报出来。
func TestSummaryReportsArtifactChange(t *testing.T) {
	out := upgradeSummary(t,
		comp{ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}},
		comp{ID: "people/basic", Version: "1.1.0",
			Artifacts: []string{"api-docs:openapi.json", "sdk:client.ts"}})

	assert.Contains(t, out, "artifacts 变更", "38.21：%s", out)
	assert.Contains(t, out, "client.ts", "38.21：要点名新增的产物：%s", out)
}

// 38.22 资源配额变更要报出来。
//
// 配额变了意味着这次升级可能因为节点资源不够而起不来，
// 这是少数几个"升级失败但和组件代码无关"的原因之一。
func TestSummaryReportsResourceQuotaChange(t *testing.T) {
	out := upgradeSummary(t,
		comp{ID: "people/basic", Version: "1.0.0", CPU: "500m", Memory: "256Mi"},
		comp{ID: "people/basic", Version: "1.1.0", CPU: "2", Memory: "1Gi"})

	assert.Contains(t, out, "资源配额变更", "38.22：%s", out)
	assert.Contains(t, out, "1Gi",
		"38.22：要给出新配额——只说'变了'的话使用者还得自己去翻 Manifest：%s", out)
}

// 六行要齐。
//
// 设计书 §3.5.1 把摘要定成固定的六项，缺哪一项都会让人以为
// "平台没检查这一方面"。所以这条不测某一项，测的是**框架完整**。
func TestSummaryHasEverySection(t *testing.T) {
	out := upgradeSummary(t,
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "1.1.0"})

	for _, section := range []string{
		"依赖变更", "新增配置项", "删除配置项", "数据库迁移", "artifacts 变更", "资源配额变更",
	} {
		assert.Contains(t, out, section,
			"38.17：设计书 004 §3.5.1 规定的六项里缺了「%s」——"+
				"缺一项就会让人以为平台没检查这一方面：%s", section, out)
	}
}

// 旧 Manifest 读不到时报"未知"，绝不能报"无"。
//
// 缓存可能被清过、文件可能坏了。这时候唯一不能做的事就是输出"无变化"——
// 那是一句**看起来完全正常的假话**，使用者会据此认为可以放心升级。
// 说"未知"至少让他知道这一项没算出来。
func TestSummarySaysUnknownWhenOldManifestIsUnreadable(t *testing.T) {
	f := addedProject(t,
		[]comp{{ID: "people/basic", Version: "1.0.0"}, {ID: "people/basic", Version: "1.1.0"}},
		"people/basic@1.0.0")

	// 把缓存里的旧 Manifest 弄坏，但保留文件——升级判据看的是"这个版本在不在缓存里"
	old := filepath.Join(f.Layout.ManifestsDir(), "people-basic-1.0.0.yaml")
	require.NoError(t, os.WriteFile(old, []byte("这不是 YAML: {{{\n"), 0o644))

	f.writeConfig(t, "components:\n  - id: people/basic\n    version: 1.1.0\n")
	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "未知",
		"38.18：旧 Manifest 读不到时必须说'未知'：%s", r.stdout)
	assert.NotContains(t, r.stdout, "依赖变更：无",
		"38.18：**绝不能报'无'**——那是一句看起来正常的假话，"+
			"使用者会据此认为可以放心升级：%s", r.stdout)
}

// ============================================================
// 38.26 升级后 status 显示新版本
// ============================================================

// 升级之后执行 status，显示的必须是新版本。
func TestStatusShowsNewVersionAfterUpgrade(t *testing.T) {
	f := upgradableProject(t, comp{ID: "people/basic", Version: "1.1.0"})
	bumpTo(t, f, "1.1.0")

	eng := newFakeEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "status")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "1.1.0", "38.26：升级后 status 要显示新版本：%s", r.stdout)
	assert.NotContains(t, r.stdout, "1.0.0",
		"38.26：旧版本已经不在配置里了，不该还显示着——"+
			"那会让人以为升级没生效，或者两个版本都在跑")
}
