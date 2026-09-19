package cli

import (
	"os"
	"path/filepath"
	"strings"
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
// 今天要跑到 up（要引擎、要走完整级联）才会发现。
func TestLintCatchesTypoInAlreadyAddedLocalComponent(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	appendTo(t, manifestPath(f, "demo/hello"), "dependancies:\n  components: []\n")

	r := runWithLogs(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "dependancies")
	assert.Contains(t, r.stdout, "未知字段")
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
	assert.Contains(t, r.stdout, "本地安装源路径不存在")
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
	assert.Contains(t, r.stdout, "来源："+filepath.Join("shared", "demo", "hello", "component.yaml"),
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

// 承诺是"纯只读、不联网"，所以要证明：目录树前后一致，且指向不可达地址的 market / git 源不影响结果。
func TestLintIsReadOnlyAndOffline(t *testing.T) {
	dir := t.TempDir()
	sources := append(
		oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"}),
		"  - id: unreachable-market\n    type: market\n    url: http://127.0.0.1:1/api/v1\n",
		"  - id: unreachable-git\n    type: git\n    url: http://127.0.0.1:1/x.git\n",
	)
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
}

func TestLintRejectsPositionalArguments(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	assert.Equal(t, clierr.ExitUsage, runIn(t, f.Dir, "lint", "extra").code)
}
