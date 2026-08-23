package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// ============================================================
// 构造与路径
// ============================================================

func TestNewWithNilConfig(t *testing.T) {
	c, err := New(newProject(t), nil, Options{})
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Empty(t, c.fetchers)
	assert.NoError(t, c.Close())
}

func TestNewRejectsUnknownSourceType(t *testing.T) {
	_, err := New(newProject(t), cfgWithSources(config.Source{ID: "x", Type: "ftp"}), Options{})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "market、git 或 local")
}

func TestResolvePath(t *testing.T) {
	layout := config.NewLayout("/projects/erp", "")
	c := &Client{layout: layout}

	assert.Equal(t, filepath.Join("/projects/erp", "components"), c.resolvePath("./components"))
	assert.Equal(t, filepath.Join("/projects/erp", "a", "b"), c.resolvePath("a/b"))
	assert.Equal(t, filepath.FromSlash("/opt/shared"), c.resolvePath("/opt/shared"))
	assert.Equal(t, "/projects/erp", c.resolvePath(""))
}

func TestCachePaths(t *testing.T) {
	layout := config.NewLayout("/projects/erp", "")
	c := &Client{layout: layout}

	assert.Equal(t, filepath.FromSlash("/projects/erp/.brickkit/manifests/people-basic-1.0.0.yaml"),
		c.ManifestCachePath("people/basic", "1.0.0"))
	assert.Equal(t, filepath.FromSlash("/projects/erp/.brickkit/artifacts/people-basic-1-0-0"),
		c.ArtifactDir("people/basic", "1.0.0"))
}

// ============================================================
// 组件引用校验
// ============================================================

func TestManifestRejectsInvalidRef(t *testing.T) {
	c := newClient(t, newProject(t), cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})
	ctx := context.Background()

	_, err := c.Manifest(ctx, "PeopleBasic", "1.0.0")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInvalidArgument, clierr.As(err).Code)
	assert.Contains(t, clierr.As(err).Format(), "<scope>/<name>")

	_, err = c.Manifest(ctx, "people/basic", "^1.0.0")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInvalidArgument, clierr.As(err).Code)
	assert.Contains(t, clierr.As(err).Format(), "精确版本")

	_, err = c.Manifest(ctx, "people/basic", "1.0")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInvalidArgument, clierr.As(err).Code)
}

// ============================================================
// 缓存的健壮性
// ============================================================

// 缓存损坏时不该让命令失败：忽略缓存，重新从安装源拉取。
func TestCorruptedManifestCacheIsRefetched(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0", Description: "安装源中的版本",
	})
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	writeFile(t, c.ManifestCachePath("people/basic", "1.0.0"), "： 这不是合法 YAML ：")

	got, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.False(t, got.FromCache)
	assert.Equal(t, "安装源中的版本", got.Manifest.Metadata.Description)
	assert.Contains(t, readFile(t, c.ManifestCachePath("people/basic", "1.0.0")), "安装源中的版本")
}

// 缓存文件里装的是别的组件（例如手工改错了）：同样重新拉取。
func TestCacheOfWrongComponentIsRefetched(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0",
	})
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	writeFile(t, c.ManifestCachePath("people/basic", "1.0.0"),
		componentSpec{ID: "department/tree", Version: "9.9.9"}.yamlText())

	got, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "people/basic", got.Manifest.Metadata.ID)
	assert.False(t, got.FromCache)
}

// 缓存目录不可写时报错（而不是悄悄地不写缓存）。
func TestManifestCacheWriteFailure(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0",
	})
	// 用文件占住 .brickkit/manifests 的位置，使 MkdirAll 失败
	writeFile(t, layout.ManifestsDir(), "占位")

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "写入 Manifest 缓存失败")
}

// 产物写入失败只警告，不阻断。
func TestArtifactWriteFailureIsWarningOnly(t *testing.T) {
	layout := newProject(t)
	spec := protoSpec("department/tree", "1.0.0")
	writeComponent(t, filepath.Join(layout.Root, "components"), spec)

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)

	// 用文件占住产物目录的位置
	writeFile(t, filepath.Join(c.ArtifactDir("department/tree", "1.0.0"), "api-docs"), "占位")

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	assert.Len(t, res.Downloaded, 1, "另一个产物不受影响")
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0].Format(), "openapi.json")
}

// ============================================================
// DownloadArtifacts 的其他分支
// ============================================================

func TestDownloadArtifactsNilManifest(t *testing.T) {
	c := newClient(t, newProject(t), cfgWithSources(), Options{})

	_, err := c.DownloadArtifacts(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInternal, clierr.As(err).Code)
}

// 没有 artifacts 声明的组件：不产生任何文件，也不报错。
func TestDownloadArtifactsWithoutArtifacts(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0",
	})
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "people/basic", "1.0.0")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	assert.Empty(t, res.Downloaded)
	assert.Empty(t, res.Warnings)
	assert.NoDirExists(t, c.ArtifactDir("people/basic", "1.0.0"))
}

// Manifest 中的组件引用非法时，不去拼接目录名。
func TestDownloadArtifactsRejectsInvalidRef(t *testing.T) {
	c := newClient(t, newProject(t), cfgWithSources(), Options{})

	_, err := c.DownloadArtifacts(context.Background(), &manifest.Manifest{
		Metadata: manifest.Metadata{ID: "people/basic", Version: "latest"},
	})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeInvalidArgument, clierr.As(err).Code)
}

// 一个安装源都没有时，下载产物给出的是"没有安装源"而不是空结果。
func TestDownloadArtifactsWithoutSources(t *testing.T) {
	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(), Options{})

	m, err := manifest.Parse([]byte(protoSpec("department/tree", "1.0.0").yamlText()), "test")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(context.Background(), m)
	require.NoError(t, err)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[0].Format(), "未配置 sources")
}

// 纵深防御：产物路径越出组件目录时拒绝写入（008）。
func TestArtifactPathTraversalIsRefused(t *testing.T) {
	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	// 绕开 Manifest 校验直接构造（正常路径下 002 §2.3 的校验已拦住）
	m := &manifest.Manifest{
		Metadata: manifest.Metadata{ID: "department/tree", Version: "1.0.0"},
		Artifacts: []manifest.Artifact{
			{Type: "api-contract", Files: []string{"../../../etc/passwd"}},
		},
	}
	res, err := c.DownloadArtifacts(context.Background(), m)
	require.NoError(t, err)
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0].Format(), "越出")
	assert.NoFileExists(t, filepath.Join(layout.ArtifactsDir(), "etc", "passwd"))
}

// ============================================================
// 工具函数
// ============================================================

func TestWithinDir(t *testing.T) {
	base := filepath.FromSlash("/tmp/artifacts/people-basic-1-0-0")

	assert.True(t, withinDir(base, filepath.Join(base, "api-docs", "openapi.json")))
	assert.True(t, withinDir(base, base))
	assert.False(t, withinDir(base, filepath.Join(base, "..", "other", "x")))
	assert.False(t, withinDir(base, filepath.FromSlash("/etc/passwd")))
}

func TestReasonOf(t *testing.T) {
	assert.Equal(t, "所有安装源中都没有该产物文件", reasonOf(errNotFound))
	assert.Equal(t, "市场不可达：连接被拒绝", reasonOf(
		clierr.New(clierr.CodeNetworkUnreachable, "错误：市场不可达").
			WithDetail("安装源", "m").WithDetail("原因", "连接被拒绝")))
	assert.Equal(t, "没有原因明细", reasonOf(
		clierr.New(clierr.CodeInternal, "错误：没有原因明细")))
}

func TestWriteFileAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c.txt")

	require.NoError(t, writeFileAll(path, []byte("hello")))
	assert.Equal(t, "hello", readFile(t, path))

	// 父目录位置被文件占住时报错
	blocked := filepath.Join(dir, "file", "x.txt")
	writeFile(t, filepath.Join(dir, "file"), "占位")
	assert.Error(t, writeFileAll(blocked, []byte("x")))
}

func TestCloseReportsFirstError(t *testing.T) {
	c := &Client{fetchers: []fetcher{
		&stubFetcher{sourceID: "ok"},
		&stubFetcher{sourceID: "bad", closeErr: os.ErrPermission},
		&stubFetcher{sourceID: "bad2", closeErr: os.ErrClosed},
	}}
	assert.ErrorIs(t, c.Close(), os.ErrPermission)
}

// stubFetcher 用于测试 Client 层与安装源实现无关的行为。
type stubFetcher struct {
	sourceID string
	closeErr error
}

func (s *stubFetcher) id() string   { return s.sourceID }
func (s *stubFetcher) kind() string { return "stub" }
func (s *stubFetcher) close() error { return s.closeErr }
func (s *stubFetcher) manifestBytes(context.Context, string, string) ([]byte, error) {
	return nil, errNotFound
}
func (s *stubFetcher) latestVersion(context.Context, string) (string, error) {
	return "", errNotFound
}
func (s *stubFetcher) artifactFile(context.Context, string, string, manifest.Artifact, string) ([]byte, error) {
	return nil, errNotFound
}
func (s *stubFetcher) origin(context.Context, string, string) (*Origin, error) {
	return nil, errNotFound
}
