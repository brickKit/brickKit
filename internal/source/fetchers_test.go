package source

import (
	"context"
	"errors"
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
// local
// ============================================================

// path 指向的是文件而不是目录时，属于配置错误。
func TestLocalSourcePathIsFile(t *testing.T) {
	layout := newProject(t)
	writeFile(t, filepath.Join(layout.Root, "components"), "我是个文件")

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "不是目录")
}

// 组件目录里的 component.yaml 不合法：报解析错误，而不是静默跳过。
func TestLocalSourceInvalidManifest(t *testing.T) {
	layout := newProject(t)
	writeFile(t, filepath.Join(layout.Root, "components", "people", "basic", "component.yaml"),
		"apiVersion: brickkit/v1\nkind: Component\nmetadata:\n  id: people/basic\n")

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeManifestInvalid, e.Code)
	out := e.Format()
	assert.Contains(t, out, "local-dev（local）：people/basic@1.0.0", "错误应指出是哪个安装源的哪个组件")
	assert.Contains(t, out, "people/basic@1.0.0")
}

// local 源里的产物文件缺失：只警告，不阻断（004 §10.1）。
func TestLocalSourceMissingArtifactFile(t *testing.T) {
	layout := newProject(t)
	spec := protoSpec("department/tree", "1.0.0")
	delete(spec.Files, "openapi.json") // 声明了但仓库里没有
	writeComponent(t, filepath.Join(layout.Root, "components"), spec)

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	assert.Len(t, res.Downloaded, 1)
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0].Format(), "api-docs / openapi.json")
}

// 安装源目录在取产物时才消失：报配置错误（属于"真失败"，不是"没有该产物"）。
func TestLocalSourceDisappearsBeforeArtifactDownload(t *testing.T) {
	layout := newProject(t)
	spec := protoSpec("department/tree", "1.0.0")
	sourceDir := filepath.Join(layout.Root, "components")
	writeComponent(t, sourceDir, spec)

	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "local-dev", Type: config.SourceTypeLocal, Path: "./components",
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(sourceDir))

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[0].Format(), "本地安装源路径不存在")
}

// ============================================================
// git
// ============================================================

// git 源同样要把产物下载到 .brickkit/artifacts/。
func TestGitSourceDownloadsArtifacts(t *testing.T) {
	repo := t.TempDir()
	spec := protoSpec("department/tree", "1.0.0")
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
	assert.Empty(t, res.Warnings)
	assert.Len(t, res.Downloaded, 2)
	assert.Equal(t, spec.Files["openapi.json"], readFile(t, filepath.Join(
		layout.ArtifactsDir(), "department-tree-1-0-0", "api-docs", "openapi.json")))
}

// 单组件仓库的产物同样能取到（component.yaml 在仓库根目录）。
func TestGitSourceSingleComponentRepoArtifacts(t *testing.T) {
	repo := t.TempDir()
	spec := protoSpec("department/tree", "1.0.0")
	writeFile(t, filepath.Join(repo, "component.yaml"), spec.yamlText())
	for name, content := range spec.Files {
		writeFile(t, filepath.Join(repo, filepath.FromSlash(name)), content)
	}
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
	assert.Len(t, res.Downloaded, 2)
	assert.Empty(t, res.Warnings)
}

// 仓库里是别的版本时不能张冠李戴地把产物取回来（多版本共存的正确性前提）。
func TestGitSourceArtifactVersionMismatch(t *testing.T) {
	repo := t.TempDir()
	writeComponent(t, repo, protoSpec("department/tree", "1.0.0"))
	url := newGitRepo(t, repo)

	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: url,
	}), Options{})

	// 手上拿的是 2.0.0 的 Manifest（例如来自另一个安装源）
	v2, err := manifest.Parse([]byte(protoSpec("department/tree", "2.0.0").yamlText()), "test")
	require.NoError(t, err)

	res, err := c.DownloadArtifacts(context.Background(), v2)
	require.NoError(t, err)
	assert.Empty(t, res.Downloaded)
	require.Len(t, res.Warnings, 2)
	assert.NoDirExists(t, filepath.Join(layout.ArtifactsDir(), "department-tree-2-0-0"))
}

// clone 失败在本次运行内只做一次，错误被复用且不会越积越长。
func TestGitSourceCloneFailureIsReusedNotAccumulated(t *testing.T) {
	layout := newProject(t)
	c := newClient(t, layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: filepath.Join(t.TempDir(), "nope.git"),
	}), Options{})

	ctx := context.Background()
	_, err1 := c.Manifest(ctx, "people/basic", "1.0.0")
	require.Error(t, err1)
	_, err2 := c.Manifest(ctx, "people/basic", "1.0.0")
	require.Error(t, err2)

	assert.Equal(t, clierr.As(err1).Format(), clierr.As(err2).Format(),
		"同一个 clone 失败重复报出时，错误内容必须一致")
}

// Close 后临时 clone 目录被清理。
func TestGitSourceCloseRemovesCheckout(t *testing.T) {
	repo := t.TempDir()
	writeComponent(t, repo, componentSpec{ID: "people/basic", Version: "1.0.0"})
	url := newGitRepo(t, repo)

	layout := newProject(t)
	c, err := New(layout, cfgWithSources(config.Source{
		ID: "my-git", Type: config.SourceTypeGit, URL: url,
	}), Options{})
	require.NoError(t, err)

	_, err = c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.NoError(t, err)

	gs := c.fetchers[0].(*gitSource)
	checkout := gs.dir
	require.DirExists(t, checkout)

	require.NoError(t, c.Close())
	assert.NoDirExists(t, checkout)
	assert.NoError(t, c.Close(), "重复 Close 应该是安全的")
}

func TestFirstLine(t *testing.T) {
	assert.Equal(t, "fatal: repository not found",
		firstLine("\n  fatal: repository not found\nmore\n", errors.New("exit 128")))
	assert.Equal(t, "exit 128", firstLine("   \n\n", errors.New("exit 128")))
}

// ============================================================
// manifestMatches
// ============================================================

func TestManifestMatches(t *testing.T) {
	yaml := []byte("metadata:\n  id: people/basic\n  version: 1.0.0\n")

	assert.True(t, manifestMatches(yaml, "people/basic", "1.0.0"))
	assert.False(t, manifestMatches(yaml, "people/basic", "2.0.0"))
	assert.False(t, manifestMatches(yaml, "department/tree", "1.0.0"))
	assert.False(t, manifestMatches([]byte("： 不是合法 YAML ："), "people/basic", "1.0.0"))
}
