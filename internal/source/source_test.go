// 本文件是 Step 6「安装源实现」的业务行为测试，逐项覆盖开发计划 6.1–6.13。
// 这些用例只依赖对外行为（安装源 → Manifest / artifacts / 缓存），不依赖内部实现细节。
package source

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// ============================================================
// 6.1 / 6.2 local 安装源
// ============================================================

// 6.1 local 安装源读取本地 component.yaml。
func TestLocalSourceReadsComponentYAML(t *testing.T) {
	layout := newProject(t)
	sourceDir := filepath.Join(layout.Root, "components")
	writeComponent(t, sourceDir, componentSpec{
		ID:          "department/tree",
		Version:     "1.0.0",
		Description: "提供部门树的增删改查能力",
		Image:       "registry.brickkit.io/department-tree:1.0.0",
	})

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	got, err := c.Manifest(context.Background(), "department/tree", "1.0.0")
	require.NoError(t, err)

	assert.Equal(t, "department/tree", got.Manifest.Metadata.ID)
	assert.Equal(t, "1.0.0", got.Manifest.Metadata.Version)
	assert.Equal(t, "提供部门树的增删改查能力", got.Manifest.Metadata.Description)
	assert.Equal(t, "registry.brickkit.io/department-tree:1.0.0", got.Manifest.Deployment.Image)
	assert.Equal(t, "local-dev", got.SourceID, "应记录提供该 Manifest 的安装源")
	assert.False(t, got.FromCache, "首次获取来自安装源而非缓存")
}

// 6.2 local 安装源路径不存在时报错。
func TestLocalSourcePathMissing(t *testing.T) {
	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./not-exist",
	}), Options{})

	_, err := c.Manifest(context.Background(), "department/tree", "1.0.0")
	require.Error(t, err)

	e := clierr.As(err)
	out := e.Format()
	assert.Contains(t, out, "local-dev", "错误应指出是哪个安装源")
	assert.Contains(t, out, "not-exist", "错误应指出不存在的路径")
}

// local 源目录存在但组件不在其中：不是致命错误，只是"该源没有"（供 6.10 回落）。
func TestComponentNotFoundInAnySource(t *testing.T) {
	layout := newProject(t)
	require.NoError(t, os.MkdirAll(filepath.Join(layout.Root, "components"), 0o755))

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeComponentNotFound, e.Code)
	out := e.Format()
	assert.Contains(t, out, "people/basic@1.0.0")
	assert.Contains(t, out, "所有安装源", "对齐 004 §10.2：该组件在所有安装源中均未找到")
	assert.Contains(t, out, "local-dev", "应列出已尝试的安装源")
}

// 同一组件在源中存在、但版本不匹配时，视为该源没有这个版本。
func TestLocalSourceVersionMismatchIsNotFound(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0",
	})

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "2.0.0")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeComponentNotFound, clierr.As(err).Code)
}

// ============================================================
// 6.3 / 6.4 git 安装源
// ============================================================

// 6.3 git 安装源 clone 仓库并读取 Manifest。
func TestGitSourceClonesRepoAndReadsManifest(t *testing.T) {
	repo := t.TempDir()
	writeComponent(t, repo, componentSpec{
		ID: "people/basic", Version: "1.0.0", Description: "来自 git 仓库",
	})
	url := newGitRepo(t, repo)

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: url,
	}), Options{})

	got, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "people/basic", got.Manifest.Metadata.ID)
	assert.Equal(t, "来自 git 仓库", got.Manifest.Metadata.Description)
	assert.Equal(t, "my-git", got.SourceID)
}

// 单组件仓库（仓库根目录直接是 component.yaml）也应被识别。
func TestGitSourceSingleComponentRepo(t *testing.T) {
	repo := t.TempDir()
	spec := componentSpec{ID: "people/basic", Version: "1.0.0"}
	writeFile(t, filepath.Join(repo, "component.yaml"), spec.yamlText())
	url := newGitRepo(t, repo)

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: url,
	}), Options{})

	got, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "people/basic", got.Manifest.Metadata.ID)
}

// 6.4 git 安装源 URL 不可达时报错。
func TestGitSourceUnreachableURL(t *testing.T) {
	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit,
		URL: filepath.Join(t.TempDir(), "no-such-repo.git"),
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeCloneFailed, e.Code)
	out := e.Format()
	assert.Contains(t, out, "my-git")
	assert.Contains(t, out, "no-such-repo.git")
	assert.Contains(t, out, "建议", "004 §10.1：网络错误应建议检查网络")
}

// ============================================================
// 6.5 / 6.6 market 安装源
// ============================================================

// 6.5 market 安装源调用 API 获取 Manifest（Mock API，验证请求）。
func TestMarketSourceFetchesManifest(t *testing.T) {
	spec := protoSpec("people/basic", "1.2.0")
	spec.Description = "来自市场"
	mock := newMarketMock(t, spec)
	mock.token = "tok-abc"

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket,
		URL: mock.URL(), AuthToken: "tok-abc",
	}), Options{})

	got, err := c.Manifest(context.Background(), "people/basic", "1.2.0")
	require.NoError(t, err)
	assert.Equal(t, "people/basic", got.Manifest.Metadata.ID)
	assert.Equal(t, "1.2.0", got.Manifest.Metadata.Version)
	assert.Equal(t, "来自市场", got.Manifest.Metadata.Description)

	reqs := mock.recorded()
	require.Len(t, reqs, 1, "获取 Manifest 只需要一次请求")
	assert.Equal(t, http.MethodGet, reqs[0].Method)
	assert.Equal(t, "/api/v1/components/people/basic/versions/1.2.0/manifest", reqs[0].Path,
		"007 §4.5 / 004 §3.3：GET /api/v1/components/{id}/versions/{ver}/manifest")
	assert.Equal(t, "Bearer tok-abc", reqs[0].Auth, "authToken 应作为 Bearer Token 发送")
}

// 市场直接返回 YAML 正文（不带 JSON 信封）时同样可解析。
func TestMarketSourceAcceptsRawYAMLBody(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})
	mock.rawYAML = true

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	got, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "people/basic", got.Manifest.Metadata.ID)
}

// 市场中没有该版本时视为"该源没有"，而不是致命错误。
func TestMarketSourceVersionNotFound(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "9.9.9")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeComponentNotFound, clierr.As(err).Code)
}

// 6.6 market 安装源 API 不可达时报错。
func TestMarketSourceUnreachable(t *testing.T) {
	mock := newMarketMock(t)
	url := mock.URL()
	mock.server.Close() // 关掉服务：端口不再监听

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: url,
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeNetworkUnreachable, e.Code)
	out := e.Format()
	assert.Contains(t, out, "brickkit-market")
	assert.Contains(t, out, "网络", "004 §10.1：网络错误应建议检查网络")
}

// 市场返回 401 时，提示登录（004 §10.2 未登录市场）。
func TestMarketSourceUnauthorized(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})
	mock.token = "tok-abc" // 客户端不带 token

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeAuthRequired, e.Code)
	assert.Contains(t, e.Format(), "brickkit login")
}

// 已登录时优先用 .brickkit/credentials 中的 Token，忽略 authToken（004 §5.3 Token 优先级）。
func TestMarketSourcePrefersCredentialsOverAuthToken(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})
	mock.token = "tok-from-login"

	layout := newProject(t)
	writeFile(t, layout.CredentialsPath(), `{
  "type": "password",
  "marketUrl": "`+mock.URL()+`",
  "username": "zhangsan",
  "token": "tok-from-login",
  "expiresAt": "2099-01-01T00:00:00Z"
}`)

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket,
		URL: mock.URL(), AuthToken: "tok-from-yaml",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok-from-login", mock.recorded()[0].Auth)
}

// Token 过期时报错提示重新登录（004 §5.3）。
func TestMarketSourceExpiredCredentials(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})

	layout := newProject(t)
	writeFile(t, layout.CredentialsPath(), `{
  "type": "password",
  "marketUrl": "`+mock.URL()+`",
  "token": "tok-old",
  "expiresAt": "2020-01-01T00:00:00Z"
}`)

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeTokenExpired, e.Code)
	assert.Contains(t, e.Format(), "brickkit login")
}

// ============================================================
// 6.7 Manifest 缓存
// ============================================================

// 6.7 Manifest 缓存到 .brickkit/manifests/。
func TestManifestCachedToDisk(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0",
	})
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)

	// 003 §7.3 的命名：.brickkit/manifests/people-basic-1.0.0.yaml
	cached := filepath.Join(layout.ManifestsDir(), "people-basic-1.0.0.yaml")
	require.FileExists(t, cached)

	// 缓存文件本身必须是可再次解析的合法 Manifest
	m, err := manifest.ParseFile(cached)
	require.NoError(t, err)
	assert.Equal(t, "people/basic", m.Metadata.ID)
	assert.Equal(t, "1.0.0", m.Metadata.Version)
}

// 第二次获取直接命中缓存，不再访问安装源。
func TestManifestServedFromCache(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	first, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.False(t, first.FromCache)

	second, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.True(t, second.FromCache, "第二次应命中 .brickkit/manifests/ 缓存")
	assert.Equal(t, "people/basic", second.Manifest.Metadata.ID)
	assert.Len(t, mock.recorded(), 1, "命中缓存时不应再请求市场")
}

// 本地源不吃 Manifest 缓存。
//
// 本地源的 component.yaml 就在使用者硬盘上、正被他编辑。缓存一份快照的话，
// 改了端口 / 迁移命令 / 配额之后 `brickkit up` 依旧按旧的生成，而且**一声不吭**。
// 缓存是为了省网络往返，本地源根本没有网络往返，也就没有缓存的理由。
func TestLocalSourceManifestIsNeverStale(t *testing.T) {
	layout := newProject(t)
	sourceDir := filepath.Join(layout.Root, "components")
	writeComponent(t, sourceDir, componentSpec{
		ID: "department/tree", Version: "1.0.0", Description: "改之前",
	})

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	first, err := c.Manifest(context.Background(), "department/tree", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, "改之前", first.Manifest.Metadata.Description)

	// 使用者改了组件（这正是把它放在本地源里的原因）
	writeComponent(t, sourceDir, componentSpec{
		ID: "department/tree", Version: "1.0.0", Description: "改之后",
	})

	second, err := c.Manifest(context.Background(), "department/tree", "1.0.0")
	require.NoError(t, err)

	assert.Equal(t, "改之后", second.Manifest.Metadata.Description,
		"本地源的改动必须立刻生效，否则改了不生效且没有任何提示")
	assert.False(t, second.FromCache)
	assert.Equal(t, "local-dev", second.SourceID)
}

// 远程源该缓存还是缓存：那里的同一个版本内容不会变，省下的是真实的网络往返。
func TestMarketSourceStillUsesCache(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	second, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)

	assert.True(t, second.FromCache)
	assert.Len(t, mock.recorded(), 1)
}

// 本地源里没有这个组件时，不该因为"探了一下"就绕开缓存去打市场。
func TestCacheStillUsedWhenLocalSourceLacksTheComponent(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "people/basic", Version: "1.0.0"})

	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"),
		componentSpec{ID: "department/tree", Version: "1.0.0"})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
		config.Source{ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL()},
	), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	second, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)

	assert.True(t, second.FromCache, "该组件不来自本地源，缓存照常生效")
	assert.Len(t, mock.recorded(), 1)
}

// ============================================================
// 6.8 / 6.12 / 6.13 artifacts 缓存
// ============================================================

// 6.8 artifacts 缓存到 .brickkit/artifacts/；6.12 按版本化服务名 + type 组织目录。
func TestArtifactsCachedByServiceName(t *testing.T) {
	layout := newProject(t)
	spec := protoSpec("department/tree", "1.0.0")
	writeComponent(t, filepath.Join(layout.Root, "components"), spec)

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	got, err := c.Manifest(context.Background(), "department/tree", "1.0.0")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(context.Background(), got.Manifest)
	require.NoError(t, err)
	assert.Empty(t, res.Warnings)
	assert.Len(t, res.Downloaded, 2)
	assert.Empty(t, res.Cached)

	// 003 §7.3：.brickkit/artifacts/<版本化服务名>/<type>/<文件路径>
	base := filepath.Join(layout.ArtifactsDir(), "department-tree-1-0-0")
	proto := filepath.Join(base, "api-contract", "proto", "department", "v1", "department.proto")
	docs := filepath.Join(base, "api-docs", "openapi.json")
	require.FileExists(t, proto)
	require.FileExists(t, docs)
	assert.Equal(t, spec.Files["proto/department/v1/department.proto"], readFile(t, proto))
	assert.Equal(t, spec.Files["openapi.json"], readFile(t, docs))
}

// 市场源的 artifacts 通过市场 API 下载（007 §9.1）。
func TestArtifactsDownloadedFromMarket(t *testing.T) {
	spec := protoSpec("department/tree", "1.0.0")
	mock := newMarketMock(t, spec)

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	got, err := c.Manifest(context.Background(), "department/tree", "1.0.0")
	require.NoError(t, err)
	res, err := c.DownloadArtifacts(context.Background(), got.Manifest)
	require.NoError(t, err)
	require.Empty(t, res.Warnings)
	assert.Len(t, res.Downloaded, 2)

	paths := make([]string, 0, len(mock.recorded()))
	for _, r := range mock.recorded() {
		paths = append(paths, r.Path)
	}
	assert.Contains(t, paths, "/api/v1/components/department/tree/versions/1.0.0/artifacts",
		"应先获取产物列表")
	assert.Contains(t, paths, "/api/v1/components/department/tree/versions/1.0.0/artifacts/art-0/download",
		"再逐个下载产物文件")

	assert.FileExists(t, filepath.Join(layout.ArtifactsDir(), "department-tree-1-0-0",
		"api-contract", "proto", "department", "v1", "department.proto"))
}

// 004 §10.1：产物下载失败只警告，不阻断安装。
func TestArtifactDownloadFailureIsWarningOnly(t *testing.T) {
	spec := protoSpec("department/tree", "1.0.0")
	mock := newMarketMock(t, spec)
	mock.failDownload = true

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	got, err := c.Manifest(context.Background(), "department/tree", "1.0.0")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(context.Background(), got.Manifest)
	require.NoError(t, err, "产物下载失败不应阻断")
	assert.Empty(t, res.Downloaded)
	require.Len(t, res.Warnings, 2)
	assert.True(t, res.Warnings[0].Warning, "应渲染为 ⚠️ 警告")
	assert.Contains(t, res.Warnings[0].Format(), "department/tree@1.0.0")
}

// 6.13 多版本 artifacts 独立存储。
func TestArtifactsOfMultipleVersionsAreIndependent(t *testing.T) {
	layout := newProject(t)
	sourceDir := filepath.Join(layout.Root, "components")

	// 两个版本放在两个安装源目录里（local 源按 <scope>/<name> 定位，同名目录只能放一个版本）
	v1 := protoSpec("department/tree", "1.0.0")
	v2 := protoSpec("department/tree", "2.0.0")
	writeComponent(t, sourceDir, v1)
	writeComponent(t, filepath.Join(layout.Root, "components-v2"), v2)

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-v1", Type: config.SourceTypeLocal, Path: "./components"},
		config.Source{ID: "local-v2", Type: config.SourceTypeLocal, Path: "./components-v2"},
	), Options{})

	ctx := context.Background()
	for _, ver := range []string{"1.0.0", "2.0.0"} {
		got, err := c.Manifest(ctx, "department/tree", ver)
		require.NoError(t, err)
		_, err = c.DownloadArtifacts(ctx, got.Manifest)
		require.NoError(t, err)
	}

	p1 := filepath.Join(layout.ArtifactsDir(), "department-tree-1-0-0", "api-docs", "openapi.json")
	p2 := filepath.Join(layout.ArtifactsDir(), "department-tree-2-0-0", "api-docs", "openapi.json")
	require.FileExists(t, p1)
	require.FileExists(t, p2)
	assert.Contains(t, readFile(t, p1), `"version":"1.0.0"`)
	assert.Contains(t, readFile(t, p2), `"version":"2.0.0"`, "新版本不得覆盖旧版本的产物")

	// Manifest 缓存同样按版本区分
	assert.FileExists(t, filepath.Join(layout.ManifestsDir(), "department-tree-1.0.0.yaml"))
	assert.FileExists(t, filepath.Join(layout.ManifestsDir(), "department-tree-2.0.0.yaml"))
}

// 已缓存的产物文件不重复下载。
func TestArtifactsSkippedWhenCached(t *testing.T) {
	spec := protoSpec("department/tree", "1.0.0")
	mock := newMarketMock(t, spec)

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)
	_, err = c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	before := len(mock.recorded())

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	assert.Empty(t, res.Downloaded)
	assert.Len(t, res.Cached, 2)
	assert.Len(t, mock.recorded(), before, "命中缓存时不应再请求市场")
}

// ============================================================
// 6.9 --refresh
// ============================================================

// 6.9 --refresh 强制重新拉取 Manifest 与 artifacts。
func TestRefreshForcesRefetch(t *testing.T) {
	layout := newProject(t)
	spec := protoSpec("department/tree", "1.0.0")
	spec.Description = "安装源中的版本"
	writeComponent(t, filepath.Join(layout.Root, "components"), spec)

	cfg := cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	})
	ctx := context.Background()

	c := newClient(t, layout, cfg, Options{})
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)
	_, err = c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)

	// 篡改缓存
	cachedManifest := filepath.Join(layout.ManifestsDir(), "department-tree-1.0.0.yaml")
	tampered := strings.Replace(spec.yamlText(), "安装源中的版本", "被改过的缓存", 1)
	writeFile(t, cachedManifest, tampered)
	cachedProto := filepath.Join(layout.ArtifactsDir(), "department-tree-1-0-0",
		"api-contract", "proto", "department", "v1", "department.proto")
	writeFile(t, cachedProto, "// 被改过的缓存\n")

	// 不带 --refresh：本地源照样不吃缓存——那份 component.yaml 与 .proto
	// 就在使用者硬盘上、跟着代码一起改（见 TestLocalSourceManifestIsNeverStale）
	stale, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "安装源中的版本", stale.Manifest.Metadata.Description)

	_, err = c.DownloadArtifacts(ctx, stale.Manifest)
	require.NoError(t, err)
	assert.Equal(t, spec.Files["proto/department/v1/department.proto"], readFile(t, cachedProto),
		"本地源的产物同样以硬盘上那份为准")

	// 把缓存再改回去，验证 --refresh 这条路本身
	writeFile(t, cachedProto, "// 被改过的缓存\n")

	// 带 --refresh：忽略缓存重新拉取，缓存被更新
	fresh := newClient(t, layout, cfg, Options{Refresh: true})
	got2, err := fresh.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "安装源中的版本", got2.Manifest.Metadata.Description)
	assert.False(t, got2.FromCache)
	assert.Contains(t, readFile(t, cachedManifest), "安装源中的版本", "缓存文件应被更新")

	res, err := fresh.DownloadArtifacts(ctx, got2.Manifest)
	require.NoError(t, err)
	assert.Len(t, res.Downloaded, 2)
	assert.Empty(t, res.Cached)
	assert.Equal(t, spec.Files["proto/department/v1/department.proto"], readFile(t, cachedProto))
}

// ============================================================
// 6.10 / 6.11 安装源优先级与开关
// ============================================================

// 6.10 安装源优先级：靠前的优先。
func TestSourcePriorityFirstWins(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0", Description: "来自本地目录",
	})
	mock := newMarketMock(t, componentSpec{
		ID: "people/basic", Version: "1.0.0", Description: "来自市场",
	})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
		config.Source{ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL()},
	), Options{})

	got, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "来自本地目录", got.Manifest.Metadata.Description, "应使用配置中靠前的安装源")
	assert.Equal(t, "local-dev", got.SourceID)
	assert.Empty(t, mock.recorded(), "靠前的源命中后不应再访问靠后的源")
}

// 靠前的源没有该组件时，自动回落到靠后的源。
func TestSourceFallsBackToNextSource(t *testing.T) {
	layout := newProject(t)
	require.NoError(t, os.MkdirAll(filepath.Join(layout.Root, "components"), 0o755))
	mock := newMarketMock(t, componentSpec{
		ID: "people/basic", Version: "1.0.0", Description: "来自市场",
	})

	c := newClient(t, layout, cfgWithSources(
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
		config.Source{ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL()},
	), Options{})

	got, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "brickkit-market", got.SourceID)
	assert.Equal(t, "来自市场", got.Manifest.Metadata.Description)
}

// 6.11 安装源 enabled: false 时跳过。
func TestDisabledSourceIsSkipped(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0", Description: "来自本地目录",
	})
	mock := newMarketMock(t, componentSpec{
		ID: "people/basic", Version: "1.0.0", Description: "来自市场",
	})

	c := newClient(t, layout, cfgWithSources(
		// 被禁用的本地源：即便有该组件也不使用
		config.Source{
			ID: "local-dev", Type: config.SourceTypeLocal,
			Path: "./components", Enabled: boolPtr(false),
		},
		config.Source{ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL()},
	), Options{})

	got, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "来自市场", got.Manifest.Metadata.Description)
	assert.Equal(t, "brickkit-market", got.SourceID)
}

// 被禁用的源即使配置有误（路径不存在）也不应导致失败。
func TestDisabledBrokenSourceDoesNotFail(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0",
	})

	c := newClient(t, layout, cfgWithSources(
		config.Source{
			ID: "broken", Type: config.SourceTypeLocal,
			Path: "./nope", Enabled: boolPtr(false),
		},
		config.Source{ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components"},
	), Options{})

	got, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "local-dev", got.SourceID)
}

// 一个安装源都没有配置时，报错要说清楚该怎么办。
func TestNoEnabledSources(t *testing.T) {
	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)

	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "sources")
}
