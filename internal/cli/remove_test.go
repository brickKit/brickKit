// 本文件是 Step 9「brickkit remove」的业务行为测试，覆盖开发计划
// 9.8–9.13、9.23、9.24，以及延后项 P9 / P16 的回填。
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// addedProject 建一个已经 add 过若干组件的项目。
func addedProject(t *testing.T, comps []comp, refs ...string) *projectFixture {
	t.Helper()
	dir := t.TempDir()
	sources := localSource(t, dir, comps...)
	f := newProjectFixtureAt(t, dir, sources...)
	for _, ref := range refs {
		r := runIn(t, f.Dir, "add", ref, "--yes")
		require.Equal(t, clierr.ExitOK, r.code, "add %s：%s%s", ref, r.stdout, r.stderr)
	}
	return f
}

// ============================================================
// 9.10 / 9.11 / 9.12 / 9.13 正常移除
// ============================================================

func TestRemoveDeletesEntryAndCaches(t *testing.T) {
	spec := comp{ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}}
	f := addedProject(t, []comp{spec}, "people/basic@1.0.0")

	manifestCache := filepath.Join(f.Layout.ManifestsDir(), "people-basic-1.0.0.yaml")
	artifactDir := filepath.Join(f.Layout.ArtifactsDir(), "people-basic-1-0-0")
	require.FileExists(t, manifestCache)
	require.DirExists(t, artifactDir)

	r := runIn(t, f.Dir, "remove", "people/basic")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Empty(t, f.refs(t))
	assert.NoFileExists(t, manifestCache, "9.13 应清理 Manifest 缓存")
	assert.NoDirExists(t, artifactDir, "9.12 应清理 artifacts 缓存")

	// 004 §3.4 输出样例
	assert.Contains(t, r.stdout, "✅ 已移除 people/basic@1.0.0")
	assert.Contains(t, r.stdout, "🗑️ 已清理 Manifest 缓存")
	assert.Contains(t, r.stdout, "🗑️ 已清理 artifacts 缓存")
}

// 9.10 多版本共存时指定版本移除，另一个版本完好。
func TestRemoveWithVersionKeepsOtherVersion(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}},
		{ID: "people/basic", Version: "2.0.0", Artifacts: []string{"api-docs:openapi.json"}},
	}
	f := addedProject(t, comps, "people/basic@1.0.0", "people/basic@2.0.0")

	r := runIn(t, f.Dir, "remove", "people/basic@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.Equal(t, []string{"people/basic@2.0.0"}, f.refs(t))
	assert.NoDirExists(t, filepath.Join(f.Layout.ArtifactsDir(), "people-basic-1-0-0"))
	assert.DirExists(t, filepath.Join(f.Layout.ArtifactsDir(), "people-basic-2-0-0"),
		"另一个版本的产物必须保留")
	assert.FileExists(t, filepath.Join(f.Layout.ManifestsDir(), "people-basic-2.0.0.yaml"))
}

// 9.11 remove 自动删除源码目录。
func TestRemoveDeletesSourceDirectory(t *testing.T) {
	spec := comp{ID: "people/basic", Version: "1.0.0"}
	repo := newComponentRepo(t, spec)
	market := newMockMarket(t, &mockComponent{Spec: spec, SourceType: "git", GitURL: repo})
	f := newProjectFixture(t, market.source())
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0", "--repo").code)

	srcDir := filepath.Join(f.Layout.ComponentsDir(), "people", "basic")
	require.DirExists(t, srcDir)

	r := runIn(t, f.Dir, "remove", "people/basic")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NoDirExists(t, srcDir)
	assert.Contains(t, r.stdout, "🗑️ 已删除源码目录 components/people/basic/")
}

// 同 ID 还有其他版本时，源码目录必须保留（源码目录按组件 ID 而非版本组织）。
func TestRemoveKeepsSourceDirWhenAnotherVersionRemains(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "people/basic", Version: "2.0.0"},
	}
	f := addedProject(t, comps, "people/basic@1.0.0", "people/basic@2.0.0")

	srcDir := filepath.Join(f.Layout.ComponentsDir(), "people", "basic")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "main.go"), []byte("package main"), 0o644))

	r := runIn(t, f.Dir, "remove", "people/basic@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.DirExists(t, srcDir, "还有 2.0.0 在用这份源码目录")
	assert.NotContains(t, r.stdout, "已删除源码目录")
}

// 源码被 brickkit sync 归档后再 remove：归档目录不能留下孤儿。
func TestRemoveDeletesArchivedSourceDirectory(t *testing.T) {
	f := addedProject(t, []comp{{ID: "people/basic", Version: "1.0.0"}}, "people/basic@1.0.0")

	archived := filepath.Join(f.Layout.ArchivedDir(), "people", "basic")
	require.NoError(t, os.MkdirAll(archived, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(archived, "main.go"), []byte("package main"), 0o644))

	r := runIn(t, f.Dir, "remove", "people/basic")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.NoDirExists(t, archived)
	assert.NoDirExists(t, filepath.Join(f.Layout.ArchivedDir(), "people"), "空的 scope 目录要一并收走")
	assert.Contains(t, r.stdout, "🗑️ 已删除归档源码目录 components/.archived/people/basic")
}

// 活跃与归档两处都有源码时，remove 要把两处都清干净。
func TestRemoveDeletesBothActiveAndArchivedSource(t *testing.T) {
	f := addedProject(t, []comp{{ID: "people/basic", Version: "1.0.0"}}, "people/basic@1.0.0")

	active := filepath.Join(f.Layout.ComponentsDir(), "people", "basic")
	archived := filepath.Join(f.Layout.ArchivedDir(), "people", "basic")
	require.NoError(t, os.MkdirAll(active, 0o755))
	require.NoError(t, os.MkdirAll(archived, 0o755))

	r := runIn(t, f.Dir, "remove", "people/basic")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.NoDirExists(t, active)
	assert.NoDirExists(t, archived)
	assert.Contains(t, r.stdout, "🗑️ 已删除源码目录 components/people/basic/")
	assert.Contains(t, r.stdout, "🗑️ 已删除归档源码目录 components/.archived/people/basic")
}

// 同 ID 还有其他版本时，归档源码和活跃源码一样必须保留。
func TestRemoveKeepsArchivedSourceWhenAnotherVersionRemains(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "people/basic", Version: "2.0.0"},
	}
	f := addedProject(t, comps, "people/basic@1.0.0", "people/basic@2.0.0")

	archived := filepath.Join(f.Layout.ArchivedDir(), "people", "basic")
	require.NoError(t, os.MkdirAll(archived, 0o755))

	r := runIn(t, f.Dir, "remove", "people/basic@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.DirExists(t, archived, "还有 2.0.0 在用这份源码目录")
	assert.NotContains(t, r.stdout, "已删除归档源码目录")
}

// remove 不破坏其他条目、注释与 ${ENV_VAR}。
func TestRemovePreservesRestOfConfig(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "department/tree", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "people/basic@1.0.0", "department/tree@1.0.0")

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "remove", "people/basic").code)

	out := f.config(t)
	assert.Equal(t, []string{"department/tree@1.0.0"}, f.refs(t))
	assert.Contains(t, out, "# 这一行注释必须在 add / remove 之后依然存在")
	assert.NotContains(t, out, "people/basic")
}

// ============================================================
// remove 与资源绑定
// ============================================================

// remove 掉最后一个版本时，指向它的资源绑定一并解除。
//
// 不解除的后果不是"多一行没用的配置"，而是**项目直接锁死**：
// 这条绑定曾经是校验硬错误，于是一条报成功的 remove 之后，
// 此后每一个命令都会撞在同一堵墙上，而错误说的是资源配置，
// 与使用者刚做的事对不上号。
func TestRemoveClearsResourceBindings(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "department/tree", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "people/basic@1.0.0", "department/tree@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: department/tree
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: pg-main
    host: host.docker.internal
    port: 5432
    password: ${PG_PASSWORD}
    bindings:
      - componentId: people/basic
        database: people
      - componentId: department/tree
        database: department
`)

	r := runIn(t, f.Dir, "remove", "people/basic")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "🗑️ 已解除资源绑定：pg-main")

	cfg := f.parsed(t)
	require.Len(t, cfg.Resources, 1, "资源声明本身要留着——库还在那儿跑")
	require.Len(t, cfg.Resources[0].Bindings, 1)
	assert.Equal(t, "department/tree", cfg.Resources[0].Bindings[0].ComponentID)
	assert.Empty(t, cfg.DanglingBindings())

	// 真正要守住的是这一条：remove 之后项目还能用
	assert.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "order").code,
		"remove 之后配置必须仍然可用")
	assert.Contains(t, f.config(t), "${PG_PASSWORD}", "改绑定不能顺手展开密钥引用")
}

// 同 ID 还有其他版本时绑定必须保留：绑定按组件 ID 记，不带版本（003 §5.3）。
func TestRemoveKeepsBindingWhenAnotherVersionRemains(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "people/basic", Version: "2.0.0"},
	}
	f := addedProject(t, comps, "people/basic@1.0.0", "people/basic@2.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: people/basic
    version: 2.0.0
resources:
  - kind: database
    engine: postgresql
    id: pg-main
    host: host.docker.internal
    port: 5432
    bindings:
      - componentId: people/basic
        database: people
`)

	r := runIn(t, f.Dir, "remove", "people/basic@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "已解除资源绑定")

	cfg := f.parsed(t)
	require.Len(t, cfg.Resources[0].Bindings, 1, "2.0.0 还要用这条绑定")
	assert.Equal(t, "people/basic", cfg.Resources[0].Bindings[0].ComponentID)
}

// 唯一一条绑定被解除后留下 `bindings: []`：资源还在，只是暂时没人用。
func TestRemoveLeavesEmptyBindingList(t *testing.T) {
	f := addedProject(t, []comp{{ID: "people/basic", Version: "1.0.0"}}, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: pg-main
    host: host.docker.internal
    port: 5432
    bindings:
      - componentId: people/basic
        database: people
`)

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "remove", "people/basic").code)

	cfg := f.parsed(t)
	require.Len(t, cfg.Resources, 1)
	assert.Empty(t, cfg.Resources[0].Bindings)
}

// ============================================================
// 9.8 / 9.24 依赖方检查
// ============================================================

// 9.8 有强依赖方时报错并列出依赖方（002 §3.9 / 004 §3.4）。
func TestRemoveBlockedByStrongDependent(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		{ID: "authorization/rbac", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		{ID: "department/tree", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0", "authorization/rbac@1.0.0")
	before := f.config(t)

	r := runIn(t, f.Dir, "remove", "department/tree")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "无法移除 department/tree")
	assert.Contains(t, r.stderr, "erp/backend")
	assert.Contains(t, r.stderr, "authorization/rbac")
	assert.Contains(t, r.stderr, "请先移除依赖方")
	assert.Equal(t, before, f.config(t), "被阻断时配置不得改动")
}

// 依赖方依赖的是另一个版本时，不阻止移除本版本。
func TestRemoveNotBlockedByDependentOnOtherVersion(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@2.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "people/basic", Version: "2.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0", "people/basic@1.0.0")

	r := runIn(t, f.Dir, "remove", "people/basic@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.ElementsMatch(t, []string{"erp/backend@1.0.0", "people/basic@2.0.0"}, f.refs(t))
}

// 9.24 只有弱依赖方时不阻止移除，但要输出警告。
func TestRemoveWeakDependentWarnsButProceeds(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Optional: []string{"infra/redis-event-bus@1.0.0"}},
		{ID: "infra/redis-event-bus", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")

	r := runIn(t, f.Dir, "remove", "infra/redis-event-bus")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.Equal(t, []string{"erp/backend@1.0.0"}, f.refs(t))
	assert.Contains(t, r.stdout, "⚠️")
	assert.Contains(t, r.stdout, "erp/backend@1.0.0")
	assert.Contains(t, r.stdout, "弱依赖")
}

// ============================================================
// 9.9 / 9.23 参数与错误
// ============================================================

// 9.9 多版本共存时不指定版本要报错（004 §3.4 输出样例）。
func TestRemoveMultiVersionRequiresVersion(t *testing.T) {
	comps := []comp{
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "people/basic", Version: "2.0.0"},
	}
	f := addedProject(t, comps, "people/basic@1.0.0", "people/basic@2.0.0")
	before := f.config(t)

	r := runIn(t, f.Dir, "remove", "people/basic")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "people/basic 存在多个版本（1.0.0, 2.0.0）")
	assert.Contains(t, r.stderr, "brickkit remove people/basic@1.0.0")
	assert.Equal(t, before, f.config(t))
}

// 9.23 remove 不存在的组件报错。
func TestRemoveUnknownComponent(t *testing.T) {
	f := addedProject(t, []comp{{ID: "people/basic", Version: "1.0.0"}}, "people/basic@1.0.0")

	r := runIn(t, f.Dir, "remove", "nope/missing")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "nope/missing")
	assert.Contains(t, r.stderr, "brickkit.yaml")
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))
}

// 指定了不存在的版本时，提示现有版本。
func TestRemoveUnknownVersion(t *testing.T) {
	f := addedProject(t, []comp{{ID: "people/basic", Version: "1.0.0"}}, "people/basic@1.0.0")

	r := runIn(t, f.Dir, "remove", "people/basic@9.9.9")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "9.9.9")
	assert.Contains(t, r.stderr, "1.0.0", "应提示当前有哪些版本")
}

func TestRemoveRequiresArgument(t *testing.T) {
	f := newProjectFixture(t)

	r := runIn(t, f.Dir, "remove")
	assert.Equal(t, clierr.ExitUsage, r.code)
}

func TestRemoveInUninitializedDir(t *testing.T) {
	r := runIn(t, t.TempDir(), "remove", "people/basic")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "❌")
}

// 依赖方的 Manifest 缓存缺失、且安装源也取不到时，无法确认依赖关系：
// 给出警告而不是假装没有依赖方，也不因此阻断。
func TestRemoveWarnsWhenDependentManifestUnavailable(t *testing.T) {
	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")

	// 制造"取不到 erp/backend 的 Manifest"：删缓存 + 去掉安装源
	require.NoError(t, os.Remove(filepath.Join(f.Layout.ManifestsDir(), "erp-backend-1.0.0.yaml")))
	f.Sources = nil
	body := "components:\n  - id: erp/backend\n    version: 1.0.0\n  - id: people/basic\n    version: 1.0.0\n"
	f.writeConfig(t, body)

	r := runIn(t, f.Dir, "remove", "people/basic")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "⚠️")
	assert.Contains(t, r.stdout, "无法确认")
	assert.Contains(t, r.stdout, "erp/backend@1.0.0")
}
