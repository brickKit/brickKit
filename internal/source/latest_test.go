// 本文件测 Client.LatestVersion：brickkit add 不写版本时靠它决定装哪个版本。
package source

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
)

// 本地源按 <scope>/<name> 定位，目录里就一份 component.yaml——
// 它写的是哪个版本，这个源能给的就只有那个版本。
func TestLatestVersionFromLocalSource(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "2.3.1",
	})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	got, err := c.LatestVersion(context.Background(), "people/basic")
	require.NoError(t, err)
	assert.Equal(t, "2.3.1", got.Version)
	assert.Equal(t, "local-dev", got.SourceID)
	assert.Equal(t, "local", got.SourceKind)
}

func TestLatestVersionFromGitSource(t *testing.T) {
	layout := newProject(t)
	repoDir := t.TempDir()
	writeComponent(t, repoDir, componentSpec{ID: "people/basic", Version: "1.4.0"})
	url := newGitRepo(t, repoDir)

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "team-git", Type: config.SourceTypeGit, URL: url},
	), Options{})

	got, err := c.LatestVersion(context.Background(), "people/basic")
	require.NoError(t, err)
	assert.Equal(t, "1.4.0", got.Version)
	assert.Equal(t, "team-git", got.SourceID)
}

// 市场有真正的版本列表，取最大的那个——而且按**数字**比，
// 不然 10.0.0 会被字符串比较判成比 2.0.0 小。
func TestLatestVersionFromMarketPicksHighest(t *testing.T) {
	mock := newMarketMock(t,
		componentSpec{ID: "people/basic", Version: "2.0.0"},
		componentSpec{ID: "people/basic", Version: "10.0.0"},
		componentSpec{ID: "people/basic", Version: "1.9.9"},
	)

	c := newClient(t, newProject(t), cfgWithSources(
		config.Source{ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL()},
	), Options{})

	got, err := c.LatestVersion(context.Background(), "people/basic")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0", got.Version)
	assert.Equal(t, "brickkit-market", got.SourceID)
}

// blocked 版本装不上（007 §6），选最新版时必须跳过它——
// 否则不写版本号的人会稳定解析到一个装不上的版本。
func TestLatestVersionSkipsNonInstallableVersions(t *testing.T) {
	mock := newMarketMock(t,
		componentSpec{ID: "people/basic", Version: "1.0.0"},
		componentSpec{ID: "people/basic", Version: "2.0.0"},
		componentSpec{ID: "people/basic", Version: "3.0.0"},
	)
	mock.versionStatus = map[string]string{
		"people/basic@3.0.0": "blocked",
		"people/basic@2.0.0": "draft",
	}

	c := newClient(t, newProject(t), cfgWithSources(
		config.Source{ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL()},
	), Options{})

	got, err := c.LatestVersion(context.Background(), "people/basic")
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", got.Version, "3.0.0 被下架、2.0.0 还是草稿，都不能装")
}

// deprecated 可以装（只是要提示风险），不能跳过。
func TestLatestVersionAcceptsDeprecated(t *testing.T) {
	mock := newMarketMock(t,
		componentSpec{ID: "people/basic", Version: "1.0.0"},
		componentSpec{ID: "people/basic", Version: "2.0.0"},
	)
	mock.versionStatus = map[string]string{"people/basic@2.0.0": "deprecated"}

	c := newClient(t, newProject(t), cfgWithSources(
		config.Source{ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL()},
	), Options{})

	got, err := c.LatestVersion(context.Background(), "people/basic")
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", got.Version)
}

// 003 §6.5：第一个有这个组件的源说了算，不跨源比大小。
// 跨源比会让"到底装了哪个源的东西"变得不可预测。
func TestLatestVersionFirstSourceWithComponentWins(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0",
	})
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "9.9.9"})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
		config.Source{ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL()},
	), Options{})

	got, err := c.LatestVersion(context.Background(), "people/basic")
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", got.Version, "靠前的源里有，就不该再去后面的源比大小")
	assert.Equal(t, "local-dev", got.SourceID)
}

// 靠前的源没有这个组件时，继续往后找——和 Manifest 一个规矩。
func TestLatestVersionFallsThroughToNextSource(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "department/tree", Version: "1.0.0",
	})
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "2.0.0"})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
		config.Source{ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL()},
	), Options{})

	got, err := c.LatestVersion(context.Background(), "people/basic")
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", got.Version)
	assert.Equal(t, "brickkit-market", got.SourceID)
}

// 所有源都没有：报错要点名组件，并说清楚是"哪些源都找过了"。
func TestLatestVersionNotFoundInAnySource(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "department/tree", Version: "1.0.0",
	})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	_, err := c.LatestVersion(context.Background(), "people/basic")
	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "people/basic")
	assert.Contains(t, text, "local-dev")
}

// 一个组件都没有的项目：没有安装源时要说"没有配置安装源"，而不是"组件不存在"。
func TestLatestVersionWithoutSources(t *testing.T) {
	c := newClient(t, newProject(t), cfgWithSources(), Options{})

	_, err := c.LatestVersion(context.Background(), "people/basic")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "安装源")
}
