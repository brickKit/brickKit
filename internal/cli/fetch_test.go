// 本文件覆盖 brickkit fetch：只取产物、不装组件（003 §4.9 跨项目共用组件）。
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// fetchProject 建一个空项目，安装源里放着一个带产物的组件（但没装进配置）。
func fetchProject(t *testing.T, comps ...comp) *projectFixture {
	t.Helper()
	dir := t.TempDir()
	sources := localSource(t, dir, comps...)
	return newProjectFixtureAt(t, dir, sources...)
}

// fetch 的**全部要点**：产物到手，而 brickkit.yaml 一个字没动。
//
// 这是它与 add 唯一的、也是最要紧的区别。跨项目的服务写进配置的后果是
// 平台在本项目里再部署一份——两个实例，而两边都以为自己是唯一那个。
func TestFetchDownloadsArtifactsWithoutTouchingConfig(t *testing.T) {
	f := fetchProject(t, comp{
		ID: "infra/notifier", Version: "1.0.0",
		Artifacts: []string{"api-contract:proto/notifier/v1/notifier.proto"},
	})
	before := f.config(t)

	r := runIn(t, f.Dir, "fetch", "infra/notifier@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.FileExists(t, filepath.Join(f.Layout.ArtifactsDir(),
		"infra-notifier-1-0-0", "api-contract", "proto/notifier/v1/notifier.proto"))
	assert.Equal(t, before, f.config(t), "fetch 绝不能修改 brickkit.yaml")
	assert.Empty(t, f.refs(t), "组件不该出现在配置里")
	assert.Contains(t, r.stdout, "未写入 brickkit.yaml")
}

// 产物落到与 add 完全相同的位置：.brickkit/artifacts/<版本化服务名>/
//
// 目录名带版本是这条命令的价值所在——平台在跨项目这条路上不再检查版本，
// 目录名成了唯一记着"客户端照哪个版本写的"的地方。
func TestFetchUsesVersionedServiceNameDirectory(t *testing.T) {
	f := fetchProject(t,
		comp{ID: "infra/notifier", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"}},
		comp{ID: "infra/notifier", Version: "2.0.0", Artifacts: []string{"api-docs:openapi.json"}},
	)

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "fetch", "infra/notifier@1.0.0").code)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "fetch", "infra/notifier@2.0.0").code)

	for _, dir := range []string{"infra-notifier-1-0-0", "infra-notifier-2-0-0"} {
		assert.FileExists(t, filepath.Join(f.Layout.ArtifactsDir(), dir, "api-docs", "openapi.json"),
			"两个版本各占一个目录，不会互相覆盖")
	}
}

// 不写版本号时取安装源里的最新版本，并说清楚解析到了哪个。
//
// 用市场源而不是本地源：本地源一个组件只有一份目录、一个版本，
// 那里的"最新"永远是它自己，验不出"按数字比大小"这件事。
func TestFetchWithoutVersionResolvesLatest(t *testing.T) {
	arts := []string{"api-docs:openapi.json"}
	market := newMockMarket(t,
		&mockComponent{Spec: comp{ID: "infra/notifier", Version: "1.0.0", Artifacts: arts}},
		&mockComponent{Spec: comp{ID: "infra/notifier", Version: "10.0.0", Artifacts: arts}},
		&mockComponent{Spec: comp{ID: "infra/notifier", Version: "2.0.0", Artifacts: arts}},
	)
	f := newProjectFixture(t, market.source())

	r := runIn(t, f.Dir, "fetch", "infra/notifier")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Contains(t, r.stdout, "infra/notifier@10.0.0", "按数字比大小，不是字符串")
	assert.DirExists(t, filepath.Join(f.Layout.ArtifactsDir(), "infra-notifier-10-0-0"))
	assert.NoDirExists(t, filepath.Join(f.Layout.ArtifactsDir(), "infra-notifier-2-0-0"))
	assert.Empty(t, f.refs(t), "解析最新版本也不该把它写进配置")
}

// 组件一个产物都没声明：说清楚是"它没有"，不是"下载失败了"。
//
// 打印一个空的成功，使用者会以为是网络问题，转头去查安装源。
func TestFetchComponentWithoutArtifactsSaysSo(t *testing.T) {
	f := fetchProject(t, comp{ID: "infra/notifier", Version: "1.0.0"})

	r := runIn(t, f.Dir, "fetch", "infra/notifier@1.0.0")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	assert.Contains(t, r.stdout, "没有声明任何产物")
	assert.NotContains(t, r.stdout, "已下载")
}

// 组件在所有安装源里都找不到：报错，而不是静悄悄地什么都不做。
func TestFetchUnknownComponentErrors(t *testing.T) {
	f := fetchProject(t, comp{ID: "infra/notifier", Version: "1.0.0"})

	r := runIn(t, f.Dir, "fetch", "nope/missing@1.0.0")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "组件未找到")
}

// 声明了产物、但一个都没拿到 → **错误**，不是警告。
//
// 与 add 刻意相反：add 装的是组件，产物只是开发时辅助；
// fetch 的全部目的就是产物，一个都没拿到还退出码 0，
// 使用者会拿着一个空目录去生成客户端，直到编译报错才发现。
func TestFetchErrorsWhenNoArtifactSucceeds(t *testing.T) {
	f := fetchProject(t, comp{
		ID: "infra/notifier", Version: "1.0.0",
		Artifacts: []string{"api-docs:openapi.json"},
	})

	// 把安装源里那个产物文件删掉，Manifest 仍声明着它
	matches, err := filepath.Glob(filepath.Join(f.Dir, "src0", "infra", "notifier", "openapi.json"))
	require.NoError(t, err)
	require.Len(t, matches, 1, "夹具应在这里放了产物文件")
	require.NoError(t, os.Remove(matches[0]))

	r := runIn(t, f.Dir, "fetch", "infra/notifier@1.0.0")
	assert.Equal(t, clierr.ExitError, r.code, "一个产物都没拿到不能算成功：%s", r.stdout)
	assert.Contains(t, r.stderr, "一个都没下载成功")
}

// fetch 不接受多个参数：一次一个，版本要人工确认过（见 003 §4.9）。
func TestFetchRejectsMultipleArgs(t *testing.T) {
	f := fetchProject(t, comp{ID: "infra/notifier", Version: "1.0.0"})

	r := runIn(t, f.Dir, "fetch", "infra/notifier@1.0.0", "infra/audit@1.0.0")
	assert.Equal(t, clierr.ExitUsage, r.code)
}
