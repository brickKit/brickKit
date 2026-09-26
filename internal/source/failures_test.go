package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/projfile"
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

	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "./components",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "Check the file permissions")
}

// 安装源目录不可访问（父目录无执行权限）。
func TestLocalSourceInaccessiblePath(t *testing.T) {
	skipIfRoot(t)

	layout := newProject(t)
	blocked := filepath.Join(layout.Root, "blocked")
	require.NoError(t, os.MkdirAll(filepath.Join(blocked, "inner"), 0o755))
	require.NoError(t, os.Chmod(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "./blocked/inner",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "is not accessible")
}

// 组件不在本地源里时下载产物：警告，不阻断。
func TestArtifactsWhenComponentNotInSource(t *testing.T) {
	layout := newProject(t)
	require.NoError(t, os.MkdirAll(filepath.Join(layout.Root, "components"), 0o755))

	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "./components",
	}), Options{})

	m, err := manifest.Parse([]byte(protoSpec("department/tree", "1.0.0").yamlText()), "test")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(context.Background(), m)
	require.NoError(t, err)
	assert.Empty(t, res.Downloaded)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[0].Format(), "none of the install sources has this artifact file")
}

// 市场的产物列表响应无法解析：警告，不阻断安装。
func TestMarketArtifactListGarbage(t *testing.T) {
	mock := newMarketMock(t, protoSpec("department/tree", "1.0.0"))
	mock.garbageArtifactList = true

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "brickkit-market", Type: projfile.SourceTypeMarket, URL: mock.URL(),
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	assert.Empty(t, res.Downloaded)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[0].Format(), "artifact list returned by the Market could not be parsed")
}

// 市场的产物列表端点异常：警告，不阻断安装。
func TestMarketArtifactListFailure(t *testing.T) {
	mock := newMarketMock(t, protoSpec("department/tree", "1.0.0"))
	mock.failArtifactList = true

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "brickkit-market", Type: projfile.SourceTypeMarket, URL: mock.URL(),
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
