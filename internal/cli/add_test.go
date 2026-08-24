// 本文件是 Step 9「brickkit add」的业务行为测试，覆盖开发计划
// 9.1–9.7、9.14–9.22、9.25，以及延后项 P6 / P7 / P13 / P16 的回填。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/logging"
)

// runStdin 在指定目录执行 CLI，并喂入标准输入（用于确认提示）。
func runStdin(t *testing.T, dir, input string, args ...string) result {
	t.Helper()
	var out, errBuf bytes.Buffer
	opts := &Options{
		WorkDir:    dir,
		ConfigPath: DefaultConfigFile,
		LogLevel:   logging.LevelOff,
		Stdin:      strings.NewReader(input),
		Stdout:     &out,
		Stderr:     &errBuf,
	}
	code := Run(NewRootCommand(opts), opts, args)
	return result{stdout: out.String(), stderr: errBuf.String(), code: code}
}

// erpComponents 是 004 §3.3 输出样例里的那套组件：erp/backend 及其依赖。
func erpComponents() []comp {
	return []comp{
		{
			ID: "erp/backend", Version: "1.0.0",
			Requires: []string{"people/basic@1.0.0", "department/tree@1.0.0"},
			Optional: []string{"infra/redis-event-bus@1.0.0"},
		},
		{ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}},
		{
			ID: "department/tree", Version: "1.0.0",
			Artifacts: []string{"api-contract:proto/department/v1/department.proto", "api-docs:openapi.json"},
		},
		{ID: "infra/redis-event-bus", Version: "1.0.0"},
	}
}

// ============================================================
// 9.1 / 9.20 正常添加
// ============================================================

func TestAddWritesComponentToConfig(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0",
		Artifacts: []string{"api-docs:openapi.json"}})
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "people/basic@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))
	assert.Contains(t, r.stdout, "📦 添加 people/basic@1.0.0")
	assert.Contains(t, r.stdout, "✅ 已写入 brickkit.yaml（1 个组件）")
}

// 9.20 add 自动添加的组件不写 enabled 字段（004 §3.3 关键规则）。
func TestAddDoesNotWriteEnabledField(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0").code)
	assert.NotContains(t, f.config(t), "enabled")
}

// add 必须原样保留用户的注释与 ${ENV_VAR}（绝不能把密钥展开写回文件）。
func TestAddPreservesCommentsAndEnvRefs(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	f.writeConfig(t, `components: []

resources:
  # 这条注释也必须保留
  - kind: database
    engine: postgresql
    id: postgres-main
    host: localhost
    port: 5432
    password: ${POSTGRES_PASSWORD}
`)
	t.Setenv("POSTGRES_PASSWORD", "super-secret")

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0").code)

	out := f.config(t)
	assert.Contains(t, out, "# 这一行注释必须在 add / remove 之后依然存在")
	assert.Contains(t, out, "# 这条注释也必须保留")
	assert.Contains(t, out, "${POSTGRES_PASSWORD}", "密钥引用必须原样保留")
	assert.NotContains(t, out, "super-secret", "绝不能把展开后的密钥写回配置")
	assert.Contains(t, out, "people/basic")
}

// ============================================================
// 9.2 / 9.3 / 9.25 递归依赖与产物
// ============================================================

// 9.2 add 递归拉取依赖，全部写入 brickkit.yaml。
func TestAddPullsDependenciesRecursively(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, erpComponents()...)
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "erp/backend@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.ElementsMatch(t, []string{
		"erp/backend@1.0.0", "people/basic@1.0.0",
		"department/tree@1.0.0", "infra/redis-event-bus@1.0.0",
	}, f.refs(t))

	// 004 §3.3 的输出样例
	assert.Contains(t, r.stdout, "依赖 people/basic@1.0.0")
	assert.Contains(t, r.stdout, "弱依赖 infra/redis-event-bus@1.0.0")
	assert.Contains(t, r.stdout, "✅ 已写入 brickkit.yaml（4 个组件）")
}

// 9.3 add 自动下载 artifacts 到 .brickkit/artifacts/<版本化服务名>/<type>/。
func TestAddDownloadsArtifacts(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, erpComponents()...)
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "erp/backend@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.FileExists(t, filepath.Join(f.Layout.ArtifactsDir(),
		"department-tree-1-0-0", "api-contract", "proto", "department", "v1", "department.proto"))
	assert.FileExists(t, filepath.Join(f.Layout.ArtifactsDir(),
		"people-basic-1-0-0", "api-docs", "openapi.json"))
	// Manifest 缓存（003 §7.1）
	assert.FileExists(t, filepath.Join(f.Layout.ManifestsDir(), "erp-backend-1.0.0.yaml"))
	assert.Contains(t, r.stdout, "📁 已下载 artifacts 到 .brickkit/artifacts/（3 个文件）")
}

// 9.25 artifacts 下载失败时警告但继续（004 §10.1）。
func TestAddArtifactDownloadFailureWarnsButContinues(t *testing.T) {
	market := newMockMarket(t, &mockComponent{
		Spec:         comp{ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}},
		SourceType:   "git",
		FailDownload: true,
	})
	f := newProjectFixture(t, market.source())

	r := runIn(t, f.Dir, "add", "people/basic@1.0.0")
	assert.Equal(t, clierr.ExitOK, r.code, "产物下载失败不得阻断安装")
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))
	assert.Contains(t, r.stdout, "⚠️")
	assert.Contains(t, r.stdout, "openapi.json")
}

// 弱依赖缺失时警告但继续（004 §4.5），并且不写入 brickkit.yaml。
func TestAddMissingOptionalDependencyWarns(t *testing.T) {
	dir := t.TempDir()
	comps := erpComponents()
	sources := localSource(t, dir, comps[0], comps[1], comps[2]) // 不提供 infra/redis-event-bus
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "erp/backend@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.ElementsMatch(t, []string{
		"erp/backend@1.0.0", "people/basic@1.0.0", "department/tree@1.0.0",
	}, f.refs(t))
	assert.Contains(t, r.stdout, "⚠️")
	assert.Contains(t, r.stdout, "infra/redis-event-bus@1.0.0")
	assert.Contains(t, r.stdout, "INFRA_REDIS_EVENT_BUS_ENDPOINT")
}

// 强依赖缺失时报错阻断，且 brickkit.yaml 不被修改（004 §10.2）。
func TestAddMissingStrongDependencyBlocks(t *testing.T) {
	dir := t.TempDir()
	comps := erpComponents()
	sources := localSource(t, dir, comps[0], comps[1]) // 缺 department/tree
	f := newProjectFixtureAt(t, dir, sources...)
	before := f.config(t)

	r := runIn(t, f.Dir, "add", "erp/backend@1.0.0")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "强依赖缺失")
	assert.Contains(t, r.stderr, "department/tree@1.0.0")
	assert.Equal(t, before, f.config(t), "失败时不得修改 brickkit.yaml")
}

// ============================================================
// 9.4 / 9.5 / 9.6 / 9.7 多版本与重复添加
// ============================================================

// 9.5 同 ID 不同版本默认共存。
func TestAddSecondVersionCoexists(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir,
		comp{ID: "people/basic", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "2.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0").code)
	r := runIn(t, f.Dir, "add", "people/basic@2.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.Equal(t, []string{"people/basic@1.0.0", "people/basic@2.0.0"}, f.refs(t))
	assert.Contains(t, r.stdout, "多版本共存")
}

// ============================================================
// 不指定版本：默认装最新版（004 §3.3）
// ============================================================

// 不写 @版本 时解析到市场上最新的可安装版本，并把**精确版本**钉进 brickkit.yaml。
func TestAddWithoutVersionResolvesLatest(t *testing.T) {
	market := newMockMarket(t,
		&mockComponent{Spec: comp{ID: "people/basic", Version: "1.0.0"}},
		&mockComponent{Spec: comp{ID: "people/basic", Version: "10.0.0"}},
		&mockComponent{Spec: comp{ID: "people/basic", Version: "2.0.0"}},
	)
	f := newProjectFixture(t, market.source())

	r := runIn(t, f.Dir, "add", "people/basic", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Equal(t, []string{"people/basic@10.0.0"}, f.refs(t), "按数字比大小，不是字符串")
	assert.Contains(t, r.stdout, "未指定版本")
	assert.Contains(t, r.stdout, "people/basic@10.0.0")
	assert.Contains(t, f.config(t), "10.0.0", "配置里写的必须是解析后的精确版本")
}

// 本地源一个组件只有一份目录、一个版本，那个版本就是"最新"。
func TestAddWithoutVersionFromLocalSource(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "2.3.1"})
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "people/basic", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Equal(t, []string{"people/basic@2.3.1"}, f.refs(t))
	assert.Contains(t, r.stdout, "local-0", "要说清楚这个版本是哪个源给的")
}

// blocked / draft 装不上，选最新版时必须跳过——
// 否则不写版本号的人会稳定解析到一个装不上的版本。
func TestAddWithoutVersionSkipsNonInstallable(t *testing.T) {
	market := newMockMarket(t,
		&mockComponent{Spec: comp{ID: "people/basic", Version: "1.0.0"}},
		&mockComponent{Spec: comp{ID: "people/basic", Version: "2.0.0"}, Status: "draft"},
		&mockComponent{Spec: comp{ID: "people/basic", Version: "3.0.0"}, Status: "blocked"},
	)
	f := newProjectFixture(t, market.source())

	r := runIn(t, f.Dir, "add", "people/basic", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))
}

// 同 ID 已经装了别的版本：先问一句再共存。回答 n 时配置一个字节都不动。
func TestAddWithoutVersionPromptsBeforeCoexisting(t *testing.T) {
	market := newMockMarket(t,
		&mockComponent{Spec: comp{ID: "people/basic", Version: "1.0.0"}},
		&mockComponent{Spec: comp{ID: "people/basic", Version: "2.0.0"}},
	)
	f := newProjectFixture(t, market.source())
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0", "--yes").code)
	before := f.config(t)

	r := runStdin(t, f.Dir, "n\n", "add", "people/basic")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "已有 1.0.0")
	assert.Contains(t, r.stdout, "2.0.0")
	assert.Equal(t, before, f.config(t), "回答 n 时配置不变")
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))
}

// --yes 时不问，直接共存。
func TestAddWithoutVersionCoexistsWithYes(t *testing.T) {
	market := newMockMarket(t,
		&mockComponent{Spec: comp{ID: "people/basic", Version: "1.0.0"}},
		&mockComponent{Spec: comp{ID: "people/basic", Version: "2.0.0"}},
	)
	f := newProjectFixture(t, market.source())
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0", "--yes").code)

	r := runIn(t, f.Dir, "add", "people/basic", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"people/basic@1.0.0", "people/basic@2.0.0"}, f.refs(t))
}

// 解析到的版本恰好就是已装的那个：走原来的"已存在，是否刷新"分支，不提共存。
func TestAddWithoutVersionWhenLatestAlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0", "--yes").code)

	r := runIn(t, f.Dir, "add", "people/basic", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))
	assert.Contains(t, r.stdout, "已存在")
	assert.NotContains(t, r.stdout, "共存")
}

// 范围约束照旧拒绝：能省略的是版本号本身，不是"可以写个范围"（012 §2.2）。
func TestAddStillRejectsRangeVersion(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "people/basic@^1.0.0")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "精确版本")
}

// 所有源都没有这个组件：报错要点名组件，并给出"指定精确版本重试"的出路。
func TestAddWithoutVersionUnknownComponent(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "department/tree", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	before := f.config(t)

	r := runIn(t, f.Dir, "add", "people/basic")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "people/basic")
	assert.Equal(t, before, f.config(t), "解析失败时配置不能动")
}

// 9.6 同 ID 相同版本：提示已存在并询问；回答 n 时不改动配置。
func TestAddSameVersionPromptsWhenExisting(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0").code)
	before := f.config(t)

	r := runStdin(t, f.Dir, "n\n", "add", "people/basic@1.0.0")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "已存在")
	assert.Contains(t, r.stdout, "是否刷新")
	assert.Equal(t, before, f.config(t), "回答 n 时配置不变")
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t), "不得写入重复条目")
}

// 回答 y 时刷新缓存，同样不写重复条目。
func TestAddSameVersionConfirmRefresh(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0").code)

	// 篡改 Manifest 缓存，验证刷新确实重新拉取
	cache := filepath.Join(f.Layout.ManifestsDir(), "people-basic-1.0.0.yaml")
	require.NoError(t, os.WriteFile(cache, []byte("被改坏的缓存\n"), 0o644))

	r := runStdin(t, f.Dir, "y\n", "add", "people/basic@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, readFile(t, cache), "people/basic", "缓存应被重新拉取")
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))
}

// 9.7 同 ID 相同版本 + --yes：不提问，直接刷新。
// 9.4 --yes 非交互模式：不产生任何交互提示。
func TestAddSameVersionWithYesRefreshesWithoutPrompt(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0").code)

	cache := filepath.Join(f.Layout.ManifestsDir(), "people-basic-1.0.0.yaml")
	require.NoError(t, os.WriteFile(cache, []byte("被改坏的缓存\n"), 0o644))

	// 标准输入为空：--yes 下不得等待输入
	r := runStdin(t, f.Dir, "", "add", "people/basic@1.0.0", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "是否刷新")
	assert.Contains(t, r.stdout, "已刷新")
	assert.Contains(t, readFile(t, cache), "people/basic")
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))
}

// 9.4 新增组件时本来就没有交互提示。
func TestAddYesOnFreshComponentHasNoPrompt(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)

	r := runStdin(t, f.Dir, "", "add", "people/basic@1.0.0", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "是否")
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))
}

// ============================================================
// 9.21 / 9.22 参数与错误
// ============================================================

// 9.21 add 不存在的组件报错。
func TestAddUnknownComponent(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	before := f.config(t)

	r := runIn(t, f.Dir, "add", "nope/missing@1.0.0")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "nope/missing@1.0.0")
	assert.Equal(t, before, f.config(t))
}

// 9.22 版本号格式错误报错。
func TestAddInvalidVersion(t *testing.T) {
	f := newProjectFixture(t)

	r := runIn(t, f.Dir, "add", "people/basic@abc")
	assert.Equal(t, clierr.ExitUsage, r.code, "参数值写错属于用法错误")
	assert.Contains(t, r.stderr, "精确版本")
}

// 不带版本号时要去安装源查最新版——一个安装源都没配的项目，
// 报的应该是"没有可用的安装源"这个真问题，而不是笼统的用法错误。
func TestAddWithoutVersionNeedsASource(t *testing.T) {
	f := newProjectFixture(t)

	r := runIn(t, f.Dir, "add", "people/basic")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "没有可用的安装源")
}

func TestAddRequiresArgument(t *testing.T) {
	f := newProjectFixture(t)

	r := runIn(t, f.Dir, "add")
	assert.Equal(t, clierr.ExitUsage, r.code)
}

// 未初始化的目录里执行 add：报错而不是写出半个项目。
func TestAddInUninitializedDir(t *testing.T) {
	r := runIn(t, t.TempDir(), "add", "people/basic@1.0.0")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "❌")
}

// ============================================================
// 9.14–9.19 --repo / --repo-all（P6 回填）
// ============================================================

// 9.14 --repo clone 开源组件的完整 Git 仓库到 components/<scope>/<name>/。
func TestAddRepoClonesOpenSourceComponent(t *testing.T) {
	spec := comp{ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}}
	repo := newComponentRepo(t, spec)
	market := newMockMarket(t, &mockComponent{Spec: spec, SourceType: "git", GitURL: repo})
	f := newProjectFixture(t, market.source())

	r := runIn(t, f.Dir, "add", "people/basic@1.0.0", "--repo")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	target := filepath.Join(f.Layout.ComponentsDir(), "people", "basic")
	assert.DirExists(t, filepath.Join(target, ".git"), "应是完整的 Git 仓库")
	assert.FileExists(t, filepath.Join(target, "component.yaml"))
	assert.FileExists(t, filepath.Join(target, "README.md"))
	assert.Contains(t, r.stdout, "📁 已 clone 源码到 components/people/basic/")
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))
}

// 9.15 --repo 指定闭源组件时报错（004 §3.3 输出样例）。
func TestAddRepoClosedSourceComponentFails(t *testing.T) {
	spec := comp{ID: "authorization/rbac", Version: "1.0.0"}
	market := newMockMarket(t, &mockComponent{Spec: spec, SourceType: "registry"})
	f := newProjectFixture(t, market.source())

	r := runIn(t, f.Dir, "add", "authorization/rbac@1.0.0", "--repo")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "clone 失败：该组件为闭源组件")
	assert.Contains(t, r.stderr, "authorization/rbac@1.0.0")
	assert.Contains(t, r.stderr, "registry")
	assert.NoDirExists(t, filepath.Join(f.Layout.ComponentsDir(), "authorization", "rbac"))
}

// 9.16 --repo 目标目录已存在时报错（可能是正在开发的源码）。
func TestAddRepoExistingDirectoryFails(t *testing.T) {
	spec := comp{ID: "people/basic", Version: "1.0.0"}
	repo := newComponentRepo(t, spec)
	market := newMockMarket(t, &mockComponent{Spec: spec, SourceType: "git", GitURL: repo})
	f := newProjectFixture(t, market.source())

	existing := filepath.Join(f.Layout.ComponentsDir(), "people", "basic")
	require.NoError(t, os.MkdirAll(existing, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(existing, "my-work.txt"), []byte("我的源码"), 0o644))

	r := runIn(t, f.Dir, "add", "people/basic@1.0.0", "--repo")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "clone 失败：目录已存在")
	assert.Contains(t, r.stderr, "components/people/basic/")
	assert.Equal(t, "我的源码", readFile(t, filepath.Join(existing, "my-work.txt")),
		"已有源码一个字节都不能动")
}

// 9.17 / 9.18 --repo-all clone 所有开源依赖，闭源组件跳过并提示。
func TestAddRepoAllClonesOpenSourceAndSkipsClosed(t *testing.T) {
	backendSpec := comp{
		ID: "erp/backend", Version: "1.0.0",
		Requires: []string{"people/basic@1.0.0", "authorization/rbac@1.0.0"},
	}
	peopleSpec := comp{ID: "people/basic", Version: "1.0.0"}
	rbacSpec := comp{ID: "authorization/rbac", Version: "1.0.0"}

	market := newMockMarket(t,
		&mockComponent{Spec: backendSpec, SourceType: "git", GitURL: newComponentRepo(t, backendSpec)},
		&mockComponent{Spec: peopleSpec, SourceType: "git", GitURL: newComponentRepo(t, peopleSpec)},
		&mockComponent{Spec: rbacSpec, SourceType: "registry"},
	)
	f := newProjectFixture(t, market.source())

	r := runIn(t, f.Dir, "add", "erp/backend@1.0.0", "--repo-all")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.DirExists(t, filepath.Join(f.Layout.ComponentsDir(), "erp", "backend", ".git"))
	assert.DirExists(t, filepath.Join(f.Layout.ComponentsDir(), "people", "basic", ".git"))
	assert.NoDirExists(t, filepath.Join(f.Layout.ComponentsDir(), "authorization", "rbac"))

	assert.Contains(t, r.stdout, "⏭️ authorization/rbac")
	assert.Contains(t, r.stdout, "闭源组件，跳过 clone")
	assert.Contains(t, r.stdout, "📁 已 clone 2 个开源组件仓库（跳过 1 个闭源组件）")
}

// 9.19 已存在的组件再次 add + --repo：不重复写入 brickkit.yaml，只执行 clone。
func TestAddRepoOnConfiguredComponentOnlyClones(t *testing.T) {
	spec := comp{ID: "people/basic", Version: "1.0.0"}
	repo := newComponentRepo(t, spec)
	market := newMockMarket(t, &mockComponent{Spec: spec, SourceType: "git", GitURL: repo})
	f := newProjectFixture(t, market.source())

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0", "--yes").code)
	before := f.config(t)

	r := runIn(t, f.Dir, "add", "people/basic@1.0.0", "--repo", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Equal(t, before, f.config(t), "已存在的组件不得被重复写入")
	assert.DirExists(t, filepath.Join(f.Layout.ComponentsDir(), "people", "basic", ".git"))
}

// 来自本地安装源的组件没有 Git 仓库地址，--repo 要给出可理解的报错。
func TestAddRepoFromLocalSourceFails(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "people/basic@1.0.0", "--repo")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "clone 失败")
	assert.Contains(t, r.stderr, "本地安装源")
}
