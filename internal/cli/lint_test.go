package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/logging"
)

// runWithLogs 把日志级别打开再执行：错误码只出现在 stderr 的 JSON 日志行里
// （❌ 块本身不带码，AGENTS §10），而 runIn 默认把日志关了。
func runWithLogs(t *testing.T, dir string, args ...string) result {
	t.Helper()
	return runWith(t, func(o *Options) { o.LogLevel = logging.LevelInfo }, dir, args...)
}

// newLintFixture 建一个带本地源的项目，并把 comps 都 add 进 brickkit.yaml。
// （不叫 lintProject：那是 lint.go 里生产代码的函数名，同一个包里不能重名。）
func newLintFixture(t *testing.T, comps ...comp) *projectFixture {
	t.Helper()
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, oneLocalSource(t, dir, comps...)...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "--local").code)
	return f
}

// manifestPath 是 oneLocalSource 把组件写到的位置。
func manifestPath(f *projectFixture, id string) string {
	return filepath.Join(f.Dir, "shared", filepath.FromSlash(id), "component.yaml")
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	old, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(old, []byte(text)...), 0o644))
}

func TestLintCleanProject(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"}, comp{ID: "demo/caller", Version: "1.0.0"})

	r := runIn(t, f.Dir, "lint")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "✅ brickkit.yaml\n")
	assert.Contains(t, r.stdout, "✅ "+filepath.Join("shared", "demo", "hello", "component.yaml")+"\n")
	assert.Contains(t, r.stdout, "检查了 3 个文件：0 个有错误，0 条警告")
}

// 这条是 lint 存在的理由：已经 add 过的本地组件，编辑之后引入拼写错误，
// 要到跑 up（或 up --dry-run）读到那份文件时才会暴露。
func TestLintCatchesTypoInAlreadyAddedLocalComponent(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	appendTo(t, manifestPath(f, "demo/hello"), "dependancies:\n  components: []\n")

	r := runWithLogs(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "dependancies")
	assert.Contains(t, r.stdout, "unknown field")
	assert.Contains(t, r.stdout, "1 个有错误")
	assert.Contains(t, r.stderr, "LINT_FAILED")
}

func TestLintReportsEveryBrokenFileNotJustTheFirst(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"}, comp{ID: "demo/caller", Version: "1.0.0"})
	appendTo(t, manifestPath(f, "demo/hello"), "dependancies: []\n")
	appendTo(t, manifestPath(f, "demo/caller"), "migrations: {}\n")

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "dependancies")
	assert.Contains(t, r.stdout, "migrations")
	assert.Contains(t, r.stdout, "2 个有错误")
}

func TestLintNotYetAddedComponentIsAlsoChecked(t *testing.T) {
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})...)
	appendTo(t, manifestPath(f, "demo/hello"), "dependancies: []\n")

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code, "没 add 过也照样检查：编辑这份文件的人就是使用者自己")
}

func TestLintInvalidBrickkitYamlSkipsLocalSources(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	body := f.config(t)
	require.Contains(t, body, "target: docker")
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(),
		[]byte(strings.Replace(body, "target: docker", "target: swarm", 1)), 0o644))

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "deploy.target")
	assert.NotContains(t, r.stdout, filepath.Join("shared", "demo"), "brickkit.yaml 没通过就不去扫本地源，报告里不出现任何本地组件的路径")
	assert.Contains(t, r.stdout, "ℹ️")
	assert.Contains(t, r.stdout, "1 个有错误")
}

func TestLintMissingLocalSourceDirectoryIsReported(t *testing.T) {
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, "  - id: gone\n    type: local\n    path: ./nowhere\n")

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "the local install source path does not exist")
}

// 枚举遇到第一个出错的源就整体失败，别的本地源里的组件因此一份也没查，汇总里的文件数
// 会被低估。所以必须有一行 ℹ️ 说出来——否则使用者在改好 path 之前，不知道还有文件没被检查。
// 两个源的先后都要覆盖：好源在前，它的文件已经枚举到了、却随错误一起丢掉；好源在后，根本没轮到它。
func TestLintBrokenLocalSourceSaysTheOthersWereSkipped(t *testing.T) {
	const broken = "  - id: gone\n    type: local\n    path: ./nowhere\n"
	for _, order := range []struct {
		name        string
		brokenFirst bool
	}{{"坏源在后", false}, {"坏源在前", true}} {
		t.Run(order.name, func(t *testing.T) {
			dir := t.TempDir()
			sources := append(oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"}), broken)
			if order.brokenFirst {
				sources = []string{broken, sources[0]}
			}
			f := newProjectFixtureAt(t, dir, sources...)
			// 好源里放一处笔误：它一旦被检查，就会多出一个有错误的文件
			appendTo(t, manifestPath(f, "demo/hello"), "dependancies: []\n")

			r := runIn(t, f.Dir, "lint")
			assert.Equal(t, clierr.ExitError, r.code)
			assert.Contains(t, r.stdout, "the local install source path does not exist")
			assert.Contains(t, r.stdout, "ℹ️ 本地安装源枚举失败，已跳过本地组件的 component.yaml")
			assert.NotContains(t, r.stdout, "dependancies", "好源里的组件没被检查")
			assert.Contains(t, r.stdout, "检查了 1 个文件：1 个有错误，0 条警告")

			// 修好 path（这里是让那个目录存在）：好源里的组件被检查到了，那行说明也随之消失
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "nowhere"), 0o755))
			fixed := runIn(t, f.Dir, "lint")
			assert.Equal(t, clierr.ExitError, fixed.code)
			assert.Contains(t, fixed.stdout, "dependancies")
			assert.NotContains(t, fixed.stdout, "ℹ️")
			assert.Contains(t, fixed.stdout, "检查了 2 个文件：1 个有错误，0 条警告")
		})
	}
}

func TestLintDirectoryNameMustMatchMetadataID(t *testing.T) {
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})...)
	other := comp{ID: "demo/other", Version: "1.0.0"}
	require.NoError(t, os.WriteFile(manifestPath(f, "demo/hello"), []byte(other.yamlText()), 0o644))

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "demo/hello")
	assert.Contains(t, r.stdout, "demo/other")
	assert.Contains(t, r.stdout, "对不上")
}

func TestLintIgnoresArchivedComponents(t *testing.T) {
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})...)
	writeTree(t, filepath.Join(dir, "shared", ".archived", "demo", "old"),
		map[string]string{"component.yaml": "这不是合法的 YAML: [\n"})

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 枚举只看文件在不在（source.LocalManifestFiles 特意把读不动的也列出来），
// 所以"读不了"要由 lint 自己报出来，而且不能挡住其余文件的检查。
func TestLintReportsUnreadableManifestAndKeepsGoing(t *testing.T) {
	skipIfRoot(t)
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"}, comp{ID: "demo/caller", Version: "1.0.0"})
	locked := manifestPath(f, "demo/hello")
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "读取 component.yaml 失败")
	assert.Contains(t, r.stdout, filepath.Join("shared", "demo", "hello", "component.yaml"))
	assert.Contains(t, r.stdout, "✅ "+filepath.Join("shared", "demo", "caller", "component.yaml")+"\n",
		"一份读不动，不该让别的组件也没被检查")
	assert.Contains(t, r.stdout, "检查了 3 个文件：1 个有错误，0 条警告")
}

// 同一份文件里多处笔误：PropertyKeyWarnings 合成一条警告逐条列出，
// 汇总数按"块"数——所以两个拼错的键仍是 1 条警告，而不是 2 条。
func TestLintSeveralPropertyTyposInOneFileIsOneWarning(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	appendTo(t, manifestPath(f, "demo/hello"), `configSchema:
  type: object
  properties:
    pageSize:
      type: integer
      defualt: 20
      descripton: 每页条数
`)

	r := runIn(t, f.Dir, "lint")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "defualt")
	assert.Contains(t, r.stdout, "descripton")
	assert.Contains(t, r.stdout, "1 条警告")
}

const misspelledPropertyKey = `configSchema:
  type: object
  properties:
    pageSize:
      type: integer
      defualt: 20
`

func TestLintWarningsDoNotFailWithoutStrict(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	// 追加到 healthCheck 之后即可：configSchema 是顶层键，位置无所谓
	appendTo(t, manifestPath(f, "demo/hello"), misspelledPropertyKey)

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "defualt")
	assert.Contains(t, r.stdout, "1 条警告")
}

func TestLintStrictTurnsWarningsIntoFailure(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	appendTo(t, manifestPath(f, "demo/hello"), misspelledPropertyKey)

	r := runWithLogs(t, f.Dir, "lint", "--strict")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "defualt")
	assert.Contains(t, r.stderr, "LINT_FAILED")
	assert.Contains(t, r.stderr, "--strict")
}

// 一份文件既有错误又有警告：两块都要打印，汇总里"有错误"按文件数（1 个），警告按条数（1 条）。
// 这也钉住 lintManifest 里 PropertyKeyWarnings 与 Parse 成败无关——它若被挪进 Parse 成功的分支，
// 顶层键写错的文件里那条 defualt 笔误警告就会消失，使用者改完错误才第一次看到它。
func TestLintFileWithBothAnErrorAndAWarningReportsBoth(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	appendTo(t, manifestPath(f, "demo/hello"), "dependancies: []\n"+misspelledPropertyKey)

	r := runWithLogs(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	errorBlock := strings.Index(r.stdout, "❌ Error: component.yaml failed validation")
	warningBlock := strings.Index(r.stdout, "⚠️ Warning: some keys declared on configSchema items won't take effect")
	require.GreaterOrEqual(t, errorBlock, 0, "错误块要打印：%s", r.stdout)
	require.GreaterOrEqual(t, warningBlock, 0, "警告块也要打印：%s", r.stdout)
	assert.Less(t, errorBlock, warningBlock, "同一个文件里，错误在前、警告在后")
	assert.Contains(t, r.stdout, "dependancies: unknown field")
	assert.Contains(t, r.stdout, "defualt: unknown field")
	assert.Contains(t, r.stdout, "检查了 2 个文件：1 个有错误，1 条警告")
	assert.Contains(t, r.stderr, "LINT_FAILED")
}

func TestLintWarnsWhenConfigKeyCollidesWithReservedVariable(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	appendTo(t, manifestPath(f, "demo/hello"), `configSchema:
  type: object
  properties:
    databaseHost:
      type: string
`)

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "DATABASE_HOST")
	assert.Contains(t, r.stdout, "来源: "+filepath.Join("shared", "demo", "hello", "component.yaml"),
		"块里要带上文件路径，否则多个组件时不知道是哪一份")
}

func TestLintStandaloneComponentRepository(t *testing.T) {
	dir := t.TempDir()
	c := comp{ID: "demo/hello", Version: "1.0.0"}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "component.yaml"), []byte(c.yamlText()), 0o644))

	r := runIn(t, dir, "lint")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "组件仓库")
	assert.Contains(t, r.stdout, "✅ component.yaml")
	assert.Contains(t, r.stdout, "检查了 1 个文件")

	appendTo(t, filepath.Join(dir, "component.yaml"), "dependancies: []\n")
	bad := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitError, bad.code)
	assert.Contains(t, bad.stdout, "dependancies")
}

func TestLintOutsideAnyProjectFails(t *testing.T) {
	r := runWithLogs(t, t.TempDir(), "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "PROJECT_MISSING")
}

// 承诺是"纯只读、不联网"，所以要证明：目录树前后一致，且 lint 一个请求也没有发给 market / git 源。
//
// 两个远程源排在本地源**前面**：取 Manifest 一类的回归会先去碰它们。本地源排第一的话，
// 取 Manifest 在第一个源就命中了，永远走不到网络这一步。它们的地址是一个数请求的服务器，
// 而不是一个连不上的地址——连不上会被安装源当成"这个源没有"、悄悄落到下一个源，
// 联网了也看不出来；请求数不会。
func TestLintIsReadOnlyAndOffline(t *testing.T) {
	var requests atomic.Int64
	watched := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(watched.Close)

	dir := t.TempDir()
	sources := append([]string{
		"  - id: watched-market\n    type: market\n    url: " + watched.URL + "/api/v1\n",
		"  - id: watched-git\n    type: git\n    url: " + watched.URL + "/x.git\n",
	}, oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})...)
	f := newProjectFixtureAt(t, dir, sources...)

	snapshot := func() map[string]string {
		out := map[string]string{}
		require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			require.NoError(t, err)
			if d.IsDir() {
				out[p] = "<dir>"
				return nil
			}
			data, err := os.ReadFile(p)
			require.NoError(t, err)
			out[p] = string(data)
			return nil
		}))
		return out
	}
	before := snapshot()

	r := runIn(t, f.Dir, "lint")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, before, snapshot(), "lint 一个字节都不该写")
	assert.Zero(t, requests.Load(), "lint 联网了：有请求发给了 market / git 源")
}

func TestLintRejectsPositionalArguments(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	assert.Equal(t, clierr.ExitUsage, runIn(t, f.Dir, "lint", "extra").code)
}
