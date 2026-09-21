// 本文件测 brickkit add --local：一次把本地安装源里的组件全加进来（004 §3.3）。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

func TestAddLocalAddsEveryComponent(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir,
		comp{ID: "demo/hello", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "2.1.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "--local")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Equal(t, []string{"demo/hello@1.0.0", "people/basic@2.1.0"}, f.refs(t))
	assert.Contains(t, r.stdout, "local-shared")
	assert.Contains(t, r.stdout, "2 components")
}

// 依赖照常递归拉取：本地源里的 caller 依赖 hello，两个都会进配置。
func TestAddLocalPullsDependencies(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir,
		comp{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		comp{ID: "demo/hello", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "--local")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"demo/hello@1.0.0", "demo/caller@1.0.0"}, f.refs(t),
		"依赖先于依赖方写入")
}

// 已经在配置里的同版本组件静静跳过，不重复写条目。
func TestAddLocalSkipsAlreadyConfigured(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir,
		comp{ID: "demo/hello", Version: "1.0.0"},
		comp{ID: "people/basic", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "demo/hello@1.0.0", "--yes").code)

	r := runIn(t, f.Dir, "add", "--local")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Equal(t, []string{"demo/hello@1.0.0", "people/basic@1.0.0"}, f.refs(t))
	assert.Contains(t, r.stdout, "is already in brickkit.yaml")
}

// 同 ID 但版本不同：跳过并单独列出来，不替人决定多起一个容器。
func TestAddLocalSkipsDifferentVersionWithNotice(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "2.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
resources: []
`)

	r := runIn(t, f.Dir, "add", "--local")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Equal(t, []string{"demo/hello@1.0.0"}, f.refs(t), "配置里的 1.0.0 不动，2.0.0 不加")
	assert.Contains(t, r.stdout, "is 2.0.0 locally")
	assert.Contains(t, r.stdout, "1.0.0 in the configuration")
	assert.Contains(t, r.stdout, "brickkit add demo/hello@2.0.0")
}

// 一个组件解析失败 → 整体中止，配置一个字节不动。
func TestAddLocalAbortsWhenOneComponentFails(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir,
		comp{ID: "demo/hello", Version: "1.0.0"},
		// 强依赖谁都拿不到
		comp{ID: "demo/broken", Version: "1.0.0", Requires: []string{"nobody/here@9.9.9"}},
	)
	f := newProjectFixtureAt(t, dir, sources...)
	before := f.config(t)

	r := runIn(t, f.Dir, "add", "--local")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "demo/broken")
	assert.Equal(t, before, f.config(t), "配置一个字节都不能动")
	assert.Empty(t, f.refs(t))
}

// 一个 local 源都没配：要说清楚是"没有本地安装源"，而不是"没找到组件"。
func TestAddLocalWithoutLocalSource(t *testing.T) {
	market := newMockMarket(t, &mockComponent{Spec: comp{ID: "people/basic", Version: "1.0.0"}})
	f := newProjectFixture(t, market.source())

	r := runIn(t, f.Dir, "add", "--local")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "local install source")
}

// 本地源是空目录：提示一下就好，不是错误。
func TestAddLocalOnEmptySource(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "shared"), 0o755))
	f := newProjectFixtureAt(t, dir, "  - id: local-shared\n    type: local\n    path: ./shared\n")

	r := runIn(t, f.Dir, "add", "--local")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "No usable components were found")
	assert.Empty(t, f.refs(t))
}

// 全部组件都已在配置里：不写配置，也不报错。
func TestAddLocalWhenEverythingAlreadyConfigured(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "demo/hello@1.0.0", "--yes").code)
	before := f.config(t)

	r := runIn(t, f.Dir, "add", "--local")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, before, f.config(t))
	assert.Contains(t, r.stdout, "brickkit.yaml is unchanged")
}

// 坏组件必须被点名，而不是从"扫到几个"里悄悄少掉一个。
//
// 静默跳过时，components/ 下明明躺着两个目录、CLI 却说"扫到 1 个"，
// 少的那个连名字都不出现——使用者会去翻安装源配置，而问题在他自己的 component.yaml 里。
func TestAddLocalNamesBrokenComponents(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})
	// 再放一个版本号非法的
	writeTree(t, filepath.Join(dir, "shared", "demo", "broken"), map[string]string{
		"component.yaml": strings.Replace(
			comp{ID: "demo/broken", Version: "1.0.0"}.yamlText(), "version: 1.0.0", "version: latest", 1),
	})
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "--local")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Contains(t, r.stdout, "demo/broken", "坏组件要被点名")
	assert.Contains(t, r.stdout, "latest", "要说清楚坏在哪")
	assert.Equal(t, []string{"demo/hello@1.0.0"}, f.refs(t), "好的那个照常装上")
}

// 不写版本、组件又是坏的：报错要说中要害，不能回落成"组件未找到"。
func TestAddWithoutVersionOnBrokenComponent(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "shared", "demo", "broken"), map[string]string{
		"component.yaml": strings.Replace(
			comp{ID: "demo/broken", Version: "1.0.0"}.yamlText(), "version: 1.0.0", "version: latest", 1),
	})
	f := newProjectFixtureAt(t, dir,
		"  - id: local-shared\n    type: local\n    path: ./shared\n")

	r := runIn(t, f.Dir, "add", "demo/broken")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "latest", "要点出那个非法的版本号")
	assert.NotContains(t, r.stderr, "not found in any install source",
		"组件就在那儿，不该说找不到")
}

// 扫到的组件全是版本冲突时，末尾那句不能说成"都已在配置中"——它们并不在。
func TestAddLocalWordingWhenAllConflict(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "2.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
resources: []
`)

	r := runIn(t, f.Dir, "add", "--local")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "skipped")
	assert.NotContains(t, r.stdout, "Every local component is already in the configuration",
		"2.0.0 并不在配置里，这句话与上一行自相矛盾")
	assert.Contains(t, r.stdout, "versions all differ from the configuration")
}

// ============================================================
// 互斥规则
// ============================================================

// --local 不接组件 ID：两种用法说的是两件事。
func TestAddLocalRejectsComponentArgument(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "demo/hello@1.0.0", "--local")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "--local")
}

// --local 不能和 --repo / --repo-all 同用：本地组件的源码本来就在盘上。
func TestAddLocalRejectsRepoFlags(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)

	for _, flag := range []string{"--repo", "--repo-all"} {
		r := runIn(t, f.Dir, "add", "--local", flag)
		assert.Equal(t, clierr.ExitUsage, r.code, flag)
		assert.Contains(t, r.stderr, flag)
	}
}

// 本地源是作者自己的工作副本：属性声明里拼错的键（defualt）会被解析器静默丢掉，
// add --local 扫描时就该说一声——只是警告，组件照常加进来。
func TestAddLocalWarnsOnMisspelledPropertyKey(t *testing.T) {
	dir := t.TempDir()
	c := comp{ID: "demo/hello", Version: "1.0.0", ConfigSchema: []string{"greeting:hi"}}
	sources := oneLocalSource(t, dir, c)
	replaceInFile(t, filepath.Join(dir, "shared", "demo", "hello", "component.yaml"), "default:", "defualt:")
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "--local")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "configSchema.properties.greeting.defualt")
	assert.Contains(t, r.stdout, "did you mean default")
	assert.Equal(t, []string{"demo/hello@1.0.0"}, f.refs(t), "警告不影响装配")
}
