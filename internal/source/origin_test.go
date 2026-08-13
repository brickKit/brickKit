// 本文件测试组件来源信息（开源 git / 闭源 registry），供 brickkit add --repo 使用。
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

// 市场返回 sourceType: git + gitUrl → 开源组件，可 clone。
func TestOriginFromMarketOpenSource(t *testing.T) {
	spec := componentSpec{ID: "people/basic", Version: "1.0.0"}
	mock := newMarketMock(t, spec)
	mock.gitURL = "https://github.com/brickkit/people-basic.git"

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	origin, err := c.Origin(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "brickkit-market", origin.SourceID)
	assert.Equal(t, OriginGit, origin.Type)
	assert.Equal(t, "https://github.com/brickkit/people-basic.git", origin.GitURL)
	assert.True(t, origin.IsOpenSource())
}

// 市场返回 sourceType: registry → 闭源组件，没有仓库地址。
func TestOriginFromMarketClosedSource(t *testing.T) {
	spec := componentSpec{ID: "authorization/rbac", Version: "1.0.0"}
	mock := newMarketMock(t, spec)
	mock.sourceType = OriginRegistry

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	origin, err := c.Origin(context.Background(), "authorization/rbac", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, OriginRegistry, origin.Type)
	assert.Empty(t, origin.GitURL)
	assert.False(t, origin.IsOpenSource())
}

// 本地目录源没有 Git 仓库地址。
func TestOriginFromLocalSource(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0",
	})
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	origin, err := c.Origin(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "local-dev", origin.SourceID)
	assert.Equal(t, OriginLocal, origin.Type)
	assert.False(t, origin.IsOpenSource())

	// 版本对不上时视为该源没有
	_, err = c.Origin(context.Background(), "people/basic", "2.0.0")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeComponentNotFound, clierr.As(err).Code)
}

// git 安装源：安装源本身的仓库地址就是组件的仓库地址。
func TestOriginFromGitSource(t *testing.T) {
	repo := t.TempDir()
	writeComponent(t, repo, componentSpec{ID: "people/basic", Version: "1.0.0"})
	url := newGitRepo(t, repo)

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: url,
	}), Options{})

	origin, err := c.Origin(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, OriginGit, origin.Type)
	assert.Equal(t, url, origin.GitURL)
	assert.True(t, origin.IsOpenSource())

	_, err = c.Origin(context.Background(), "people/basic", "2.0.0")
	require.Error(t, err)
}

// 组件在所有源里都没有 / 没有安装源 / 引用非法。
func TestOriginErrorPaths(t *testing.T) {
	layout := newProject(t)
	ctx := context.Background()

	empty := newClient(t, layout, cfgWithSources(), Options{})
	_, err := empty.Origin(ctx, "people/basic", "1.0.0")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	_, err = c.Origin(ctx, "people/basic", "not-a-version")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInvalidArgument, clierr.As(err).Code)

	_, err = c.Origin(ctx, "people/basic", "1.0.0")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code, "安装源路径不存在是配置错误")
}

// 靠前的安装源优先：来源信息也要来自同一个源。
func TestOriginFollowsSourcePriority(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0",
	})
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})
	mock.gitURL = "https://example.com/people-basic.git"

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
		config.Source{ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL()},
	), Options{})

	origin, err := c.Origin(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "local-dev", origin.SourceID)
	assert.Empty(t, mock.recorded(), "靠前的源命中后不该再问市场")
}

// ============================================================
// 信封解析
// ============================================================

func TestOriginFromBody(t *testing.T) {
	o := originFromBody([]byte(`{"success":true,"data":{"sourceType":"git","gitUrl":"https://x.git"}}`), "m")
	assert.Equal(t, OriginGit, o.Type)
	assert.Equal(t, "https://x.git", o.GitURL)
	assert.Equal(t, "m", o.SourceID)

	// 没有 data 包一层时也认
	o = originFromBody([]byte(`{"sourceType":"registry"}`), "m")
	assert.Equal(t, OriginRegistry, o.Type)
	assert.Empty(t, o.GitURL)

	// 不是 JSON（裸 YAML 正文）：来源未知，但不报错
	o = originFromBody([]byte("metadata:\n  id: people/basic\n"), "m")
	assert.Empty(t, o.Type)
	assert.False(t, o.IsOpenSource())

	// 字段类型不对时忽略
	o = originFromBody([]byte(`{"data":{"sourceType":123,"gitUrl":true}}`), "m")
	assert.Empty(t, o.Type)
	assert.Empty(t, o.GitURL)
}

func TestIsOpenSource(t *testing.T) {
	assert.False(t, (*Origin)(nil).IsOpenSource())
	assert.False(t, (&Origin{Type: OriginGit}).IsOpenSource(), "没有仓库地址就 clone 不了")
	assert.False(t, (&Origin{Type: OriginLocal, GitURL: "x"}).IsOpenSource())
	assert.True(t, (&Origin{Type: OriginGit, GitURL: "x"}).IsOpenSource())
}
