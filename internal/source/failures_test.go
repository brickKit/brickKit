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

// skipIfRoot 跳过依赖文件权限的用例：root 无视权限位，测不出效果。
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时权限位不生效")
	}
}

// 组件的 component.yaml 不可读：报权限错误，而不是当成"该源没有这个组件"。
func TestLocalSourceUnreadableManifest(t *testing.T) {
	skipIfRoot(t)

	layout := newProject(t)
	dir := writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{
		ID: "people/basic", Version: "1.0.0",
	})
	path := filepath.Join(dir, manifest.FileName)
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "检查文件权限")
}

// 安装源目录不可访问（父目录无执行权限）。
func TestLocalSourceInaccessiblePath(t *testing.T) {
	skipIfRoot(t)

	layout := newProject(t)
	blocked := filepath.Join(layout.Root, "blocked")
	require.NoError(t, os.MkdirAll(filepath.Join(blocked, "inner"), 0o755))
	require.NoError(t, os.Chmod(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./blocked/inner",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "无法访问")
}

// 组件不在本地源里时下载产物：警告，不阻断。
func TestArtifactsWhenComponentNotInSource(t *testing.T) {
	layout := newProject(t)
	require.NoError(t, os.MkdirAll(filepath.Join(layout.Root, "components"), 0o755))

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	m, err := manifest.Parse([]byte(protoSpec("department/tree", "1.0.0").yamlText()), "test")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(context.Background(), m)
	require.NoError(t, err)
	assert.Empty(t, res.Downloaded)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[0].Format(), "所有安装源中都没有该产物文件")
}

// git 仓库中的文件不可读。
func TestGitSourceUnreadableFile(t *testing.T) {
	skipIfRoot(t)

	repo := t.TempDir()
	writeComponent(t, repo, protoSpec("department/tree", "1.0.0"))
	url := newGitRepo(t, repo)

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: url,
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)

	// clone 完成后把工作区里的 component.yaml 设为不可读
	checkout := c.fetchers[0].(*gitSource).dir
	path := filepath.Join(checkout, "department", "tree", manifest.FileName)
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[0].Format(), "读取 Git 仓库文件失败")
}

// 仓库中的产物文件不可读：警告，不阻断。
func TestGitSourceUnreadableArtifactFile(t *testing.T) {
	skipIfRoot(t)

	repo := t.TempDir()
	writeComponent(t, repo, protoSpec("department/tree", "1.0.0"))
	url := newGitRepo(t, repo)

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: url,
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)

	path := filepath.Join(c.fetchers[0].(*gitSource).dir, "department", "tree", "openapi.json")
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	assert.Len(t, res.Downloaded, 1, "另一个产物不受影响")
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0].Format(), "读取 Git 仓库文件失败")
}

// 仓库里没有这个组件：不是失败，只是"该源没有"。
func TestGitSourceComponentNotInRepo(t *testing.T) {
	repo := t.TempDir()
	writeComponent(t, repo, componentSpec{ID: "department/tree", Version: "1.0.0"})
	url := newGitRepo(t, repo)

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: url,
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeComponentNotFound, clierr.As(err).Code)
}

// Manifest 已在缓存里、但 clone 失败时下载产物：警告，不阻断。
func TestGitSourceCloneFailureDuringArtifacts(t *testing.T) {
	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: filepath.Join(t.TempDir(), "nope.git"),
	}), Options{})

	m, err := manifest.Parse([]byte(protoSpec("department/tree", "1.0.0").yamlText()), "test")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(context.Background(), m)
	require.NoError(t, err)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[0].Format(), "Git 仓库克隆失败")
}

// 仓库中缺少 Manifest 声明的产物文件：警告，不阻断。
func TestGitSourceMissingArtifactFile(t *testing.T) {
	repo := t.TempDir()
	spec := protoSpec("department/tree", "1.0.0")
	delete(spec.Files, "openapi.json")
	writeComponent(t, repo, spec)
	url := newGitRepo(t, repo)

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: url,
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	assert.Len(t, res.Downloaded, 1)
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0].Format(), "openapi.json")
}

// clone 需要的临时目录无法创建。
func TestGitSourceTempDirUnavailable(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "not-exist"))

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: "https://example.com/x.git",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeCloneFailed, e.Code)
	assert.Contains(t, e.Format(), "无法创建临时目录")
}

// clone 后 component.yaml 变得不可读：Manifest 获取阶段就报错。
func TestGitSourceUnreadableManifestOnRefetch(t *testing.T) {
	skipIfRoot(t)

	repo := t.TempDir()
	writeComponent(t, repo, componentSpec{ID: "people/basic", Version: "1.0.0"})
	url := newGitRepo(t, repo)

	layout := newProject(t)
	// Refresh 保证第二次调用仍会去读安装源，而不是命中 Manifest 缓存
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: url,
	}), Options{Refresh: true})

	ctx := context.Background()
	_, err := c.Manifest(ctx, "people/basic", "1.0.0")
	require.NoError(t, err)

	path := filepath.Join(c.fetchers[0].(*gitSource).dir, "people", "basic", manifest.FileName)
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err = c.Manifest(ctx, "people/basic", "1.0.0")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeCloneFailed, clierr.As(err).Code)
	assert.Contains(t, clierr.As(err).Format(), "读取 Git 仓库文件失败")
}

// 市场的产物列表响应无法解析：警告，不阻断安装。
func TestMarketArtifactListGarbage(t *testing.T) {
	mock := newMarketMock(t, protoSpec("department/tree", "1.0.0"))
	mock.garbageArtifactList = true

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	assert.Empty(t, res.Downloaded)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[0].Format(), "产物列表无法解析")
}

// 市场的产物列表端点异常：警告，不阻断安装。
func TestMarketArtifactListFailure(t *testing.T) {
	mock := newMarketMock(t, protoSpec("department/tree", "1.0.0"))
	mock.failArtifactList = true

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	assert.Empty(t, res.Downloaded)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[0].Format(), "503")
}
