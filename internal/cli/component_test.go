// 本文件是 Step 9 的代码层单测：参数解析、确认提示、清理与 clone 的失败路径。
package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时权限位不生效")
	}
}

// ============================================================
// parseComponentRef
// ============================================================

func TestParseComponentRef(t *testing.T) {
	id, version, err := parseComponentRef("people/basic@1.0.0", true)
	require.NoError(t, err)
	assert.Equal(t, "people/basic", id)
	assert.Equal(t, "1.0.0", version)

	// remove 允许省略版本
	id, version, err = parseComponentRef("people/basic", false)
	require.NoError(t, err)
	assert.Equal(t, "people/basic", id)
	assert.Empty(t, version)

	// 前后空白无所谓
	id, _, err = parseComponentRef("  people/basic@1.0.0 ", true)
	require.NoError(t, err)
	assert.Equal(t, "people/basic", id)
}

func TestParseComponentRefErrors(t *testing.T) {
	cases := []struct {
		name     string
		arg      string
		require  bool
		contains string
	}{
		{"组件 ID 非法", "PeopleBasic@1.0.0", true, "<scope>/<name>"},
		{"组件 ID 含大写", "People/Basic@1.0.0", true, "组件 ID 不合法"},
		{"缺版本", "people/basic", true, "请指定精确版本"},
		{"版本非法", "people/basic@abc", true, "版本号不合法"},
		{"版本非精确", "people/basic@^1.0.0", true, "精确版本"},
		{"remove 的 ID 也要合法", "Nope", false, "组件 ID 不合法"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := parseComponentRef(c.arg, c.require)
			require.Error(t, err)
			e := clierr.As(err)
			assert.Equal(t, clierr.ExitUsage, e.ExitCode(), "参数写错属于用法错误")
			assert.Contains(t, e.Format(), c.contains)
		})
	}
}

// ============================================================
// confirm
// ============================================================

func TestConfirm(t *testing.T) {
	cases := map[string]bool{
		"y\n": true, "Y\n": true, "yes\n": true, "YES\n": true,
		"n\n": false, "\n": false, "": false, "随便\n": false,
	}
	for input, want := range cases {
		var out bytes.Buffer
		opts := &Options{Stdin: strings.NewReader(input), Stdout: &out}
		assert.Equal(t, want, confirm(opts, "继续？[y/N]: "), "输入 %q", input)
		assert.Contains(t, out.String(), "继续？[y/N]: ")
	}
}

// 没有标准输入时视为拒绝，且不能卡住。
func TestConfirmWithoutStdin(t *testing.T) {
	var out bytes.Buffer
	opts := &Options{Stdout: &out}

	assert.False(t, confirm(opts, "继续？[y/N]: "))
	assert.Contains(t, out.String(), "继续？")
}

func TestItoa(t *testing.T) {
	assert.Equal(t, "0", itoa(0))
	assert.Equal(t, "7", itoa(7))
	assert.Equal(t, "42", itoa(42))
	assert.Equal(t, "1024", itoa(1024))
}

// ============================================================
// 依赖分类
// ============================================================

// 同时被强依赖和弱依赖引用的组件按"强"算：它无论如何都要装。
func TestDependencyKinds(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir,
		comp{ID: "erp/a", Version: "1.0.0", Requires: []string{"x/shared@1.0.0"}},
		comp{ID: "erp/b", Version: "1.0.0", Optional: []string{"x/shared@1.0.0"}},
		comp{ID: "x/shared", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "erp/a@1.0.0").code)

	r := runIn(t, f.Dir, "add", "erp/b@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "弱依赖 x/shared@1.0.0", "对 erp/b 而言它是弱依赖")
}

// ============================================================
// 弱依赖提示（004 §3.3）
// ============================================================

// 装完就要说清楚"它写进配置了，但默认不会启动"。
//
// `add` 会把整棵依赖树写进 brickkit.yaml，弱依赖也在里面；而级联不会把只被
// 弱依赖引用的组件拉起来（003 §4.3）。不在这里说，使用者要到 `up` 之后
// 对着 docker ps 数容器才发现——003 §4.3 亲自认定这是"最容易想当然"的一处。
func TestAddHintsWeakDependencyWillNotStart(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir,
		comp{ID: "erp/backend", Version: "1.0.0", Optional: []string{"infra/bus@1.0.0"}},
		comp{ID: "infra/bus", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "erp/backend@1.0.0")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "infra/bus")
	assert.Contains(t, r.stdout, "默认不会启动")
	assert.Contains(t, r.stdout, "enabled: true", "要给出长期的办法")
	assert.Contains(t, r.stdout, "--only", "也要给出只这一次的办法")
}

// 强依赖不该被提示——它会跟着启动。
func TestAddDoesNotHintForStrongDependency(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir,
		comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		comp{ID: "people/basic", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "erp/backend@1.0.0")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "默认不会启动")
}

// 已经在配置里的组件不再提示：这句话使用者上次已经见过了。
func TestAddDoesNotRepeatHintForExistingComponent(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir,
		comp{ID: "erp/backend", Version: "1.0.0", Optional: []string{"infra/bus@1.0.0"}},
		comp{ID: "infra/bus", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "erp/backend@1.0.0").code)

	r := runIn(t, f.Dir, "add", "erp/backend@1.0.0", "--yes")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "默认不会启动", "没有新增条目就没什么可提示的")
}

// **判定要看并集**：在一张图里是弱依赖、在另一张图里是强依赖时，它照样会启动。
//
// `add --local` 一次解析多个根、每个根一张图，而 `up` 面对的是整个配置合成的
// 一张图。逐图判断会把结论判反——那时提示说"它不会启动"，而 up 真的启动了它。
func TestAddLocalHintLooksAtTheUnionOfGraphs(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir,
		comp{ID: "erp/a", Version: "1.0.0", Requires: []string{"x/shared@1.0.0"}},
		comp{ID: "erp/b", Version: "1.0.0", Optional: []string{"x/shared@1.0.0"}},
		comp{ID: "x/shared", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "--local")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "默认不会启动",
		"erp/a 强依赖它，它会启动——提示说不会启动就是错的")
}

// --local 下真的只被弱依赖引用时，照样要提示。
func TestAddLocalHintsWeakOnlyComponent(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir,
		comp{ID: "erp/b", Version: "1.0.0", Optional: []string{"x/shared@1.0.0"}},
		comp{ID: "x/shared", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "--local")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "x/shared")
	assert.Contains(t, r.stdout, "默认不会启动")
}

// ============================================================
// 失败路径
// ============================================================

// 配置文件不可写时，add 报错而不是假装成功。
func TestAddConfigWriteFailure(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)

	// 备份目录用文件占位，使 add 前的 SaveLast 失败
	require.NoError(t, os.RemoveAll(f.Layout.BackupDir()))
	require.NoError(t, os.WriteFile(f.Layout.BackupDir(), []byte("占位"), 0o644))

	r := runIn(t, f.Dir, "add", "people/basic@1.0.0")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "创建备份目录失败")
	assert.Empty(t, f.refs(t), "备份失败时不得改动配置")
}

// artifacts 缓存目录不可删除时，remove 报错并说明原因。
func TestRemoveArtifactCleanupFailure(t *testing.T) {
	skipIfRoot(t)

	spec := comp{ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}}
	f := addedProject(t, []comp{spec}, "people/basic@1.0.0")

	artifactsRoot := f.Layout.ArtifactsDir()
	require.NoError(t, os.Chmod(artifactsRoot, 0o500)) // 只读：无法删除子目录
	t.Cleanup(func() { _ = os.Chmod(artifactsRoot, 0o755) })

	r := runIn(t, f.Dir, "remove", "people/basic")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "清理 artifacts 缓存失败")
}

// Manifest 缓存文件不可删除时同样报错。
func TestRemoveManifestCleanupFailure(t *testing.T) {
	skipIfRoot(t)

	f := addedProject(t, []comp{{ID: "people/basic", Version: "1.0.0"}}, "people/basic@1.0.0")

	manifestsDir := f.Layout.ManifestsDir()
	require.NoError(t, os.Chmod(manifestsDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(manifestsDir, 0o755) })

	r := runIn(t, f.Dir, "remove", "people/basic")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "清理 Manifest 缓存失败")
}

// --repo 时仓库地址不可达：报错，且不留下半个源码目录。
func TestAddRepoCloneFailure(t *testing.T) {
	spec := comp{ID: "people/basic", Version: "1.0.0"}
	market := newMockMarket(t, &mockComponent{
		Spec: spec, SourceType: "git",
		GitURL: filepath.Join(t.TempDir(), "no-such-repo.git"),
	})
	f := newProjectFixture(t, market.source())

	r := runIn(t, f.Dir, "add", "people/basic@1.0.0", "--repo")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "clone 失败")
	assert.NoDirExists(t, filepath.Join(f.Layout.ComponentsDir(), "people", "basic"))
}

// --repo-all 遇到已有源码目录时跳过（批量操作不该因为一个目录已存在就整体失败）。
func TestAddRepoAllSkipsExistingDirectory(t *testing.T) {
	spec := comp{ID: "people/basic", Version: "1.0.0"}
	repo := newComponentRepo(t, spec)
	market := newMockMarket(t, &mockComponent{Spec: spec, SourceType: "git", GitURL: repo})
	f := newProjectFixture(t, market.source())

	existing := filepath.Join(f.Layout.ComponentsDir(), "people", "basic")
	require.NoError(t, os.MkdirAll(existing, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(existing, "mine.txt"), []byte("我的源码"), 0o644))

	r := runIn(t, f.Dir, "add", "people/basic@1.0.0", "--repo-all")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "已有源码目录，跳过 clone")
	assert.Equal(t, "我的源码", readFile(t, filepath.Join(existing, "mine.txt")))
}

// --repo-all 遇到本地安装源的组件：跳过而不是报错。
func TestAddRepoAllSkipsLocalSourceComponent(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)

	r := runIn(t, f.Dir, "add", "people/basic@1.0.0", "--repo-all")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "无 Git 仓库地址，跳过 clone")
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))
}

// 本地源的产物每次都以硬盘上那份为准，不吃缓存。
//
// 本地源的 .proto / openapi.json 跟着代码一起改；缓存住会让调用方
// 按旧契约生成客户端，而且没有任何提示。"已是最新（缓存中 N 个文件）"
// 这条提示只对远程源成立（缓存命中的行为见 internal/source 的用例）。
func TestAddRereadsLocalSourceArtifacts(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir,
		comp{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		comp{ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}},
	)
	f := newProjectFixtureAt(t, dir, sources...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "people/basic@1.0.0").code)

	r := runIn(t, f.Dir, "add", "erp/backend@1.0.0")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "已下载 artifacts")
	assert.NotContains(t, r.stdout, "已是最新")
}

// runAdd / runRemove 允许 ctx 为 nil（cobra 在某些路径下不注入 context）。
func TestRunAddAndRemoveWithNilContext(t *testing.T) {
	dir := t.TempDir()
	sources := localSource(t, dir, comp{ID: "people/basic", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)

	var out, errBuf bytes.Buffer
	opts := &Options{
		WorkDir: f.Dir, ConfigPath: DefaultConfigFile, LogLevel: logging.LevelOff,
		Stdout: &out, Stderr: &errBuf,
	}
	//nolint:staticcheck // 显式传 nil 是本用例的目的
	require.NoError(t, runAdd(nil, opts, "people/basic@1.0.0", addFlags{}))
	assert.Equal(t, []string{"people/basic@1.0.0"}, f.refs(t))

	//nolint:staticcheck // 同上
	require.NoError(t, runRemove(nil, opts, "people/basic"))
	assert.Empty(t, f.refs(t))
}

// 配置读不出来时静默跳过多版本提示（防御性分支）。
func TestRenderCoexistenceWithUnreadableConfig(t *testing.T) {
	var out bytes.Buffer
	opts := &Options{Stdout: &out}

	renderCoexistence(opts, config.NewLayout(t.TempDir(), ""), "people/basic")
	assert.Empty(t, out.String())
}

// 某个组件的产物下载整体失败时记为警告，不中断其他组件。
func TestDownloadArtifactsWarnsInsteadOfFailing(t *testing.T) {
	layout := config.NewLayout(t.TempDir(), "")
	client, err := source.New(layout, nil, source.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	// 版本号非法的 Manifest：DownloadArtifacts 直接返回错误
	broken := &manifest.Manifest{
		Metadata: manifest.Metadata{ID: "people/basic", Version: "latest"},
	}
	graph := &resolver.Graph{Nodes: []*resolver.Node{
		{Ref: resolver.Ref{ID: "people/basic", Version: "latest"}, Manifest: broken},
	}}

	sum := downloadArtifacts(context.Background(), client, graph)
	assert.Zero(t, sum.downloaded)
	require.Len(t, sum.warnings, 1)
	assert.Contains(t, sum.warnings[0].Format(), "版本号不合法")
}
