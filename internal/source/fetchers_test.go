package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/projfile"
)

// ============================================================
// local
// ============================================================

// path 指向的是文件而不是目录时，属于配置错误。
func TestLocalSourcePathIsFile(t *testing.T) {
	layout := newProject(t)
	writeFile(t, filepath.Join(layout.Root, "components"), "我是个文件")

	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "./components",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Format(), "is not a directory")
}

// 组件目录里的 component.yaml 不合法：报解析错误，而不是静默跳过。
func TestLocalSourceInvalidManifest(t *testing.T) {
	layout := newProject(t)
	writeFile(t, filepath.Join(layout.Root, "components", "people", "basic", "component.yaml"),
		"apiVersion: brickkit/v1\nkind: Component\nmetadata:\n  id: people/basic\n")

	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "./components",
	}), Options{})

	_, err := c.Manifest(context.Background(), "people/basic", "1.0.0")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeManifestInvalid, e.Code)
	out := e.Format()
	assert.Contains(t, out, "local-dev (local): people/basic@1.0.0", "错误应指出是哪个安装源的哪个组件")
	assert.Contains(t, out, "people/basic@1.0.0")
}

// local 源里的产物文件缺失：只警告，不阻断。
func TestLocalSourceMissingArtifactFile(t *testing.T) {
	layout := newProject(t)
	spec := protoSpec("department/tree", "1.0.0")
	delete(spec.Files, "openapi.json") // 声明了但仓库里没有
	writeComponent(t, filepath.Join(layout.Root, "components"), spec)

	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "./components",
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

	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "./components",
	}), Options{})

	ctx := context.Background()
	got, err := c.Manifest(ctx, "department/tree", "1.0.0")
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(sourceDir))

	res, err := c.DownloadArtifacts(ctx, got.Manifest)
	require.NoError(t, err)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[0].Format(), "the local install source path does not exist")
}

// ============================================================
// git
// ============================================================

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

// ============================================================
// local：归档目录（brickkit sync 的 .archived/）
// ============================================================

// 归档过的组件按 ID 仍然取得到。
//
// 这是 sync 可逆的前提：sync 把源码搬进 components/.archived/ 之后，
// brickkit.yaml 里那一行还在，级联计算就得读得到它的 Manifest。
// 取不到的话，up 会报"组件未找到"，而 sync 自己也要先解析全图——
// 连"把它移回来"都做不到（真跑复现过，只能手工 mv）。
func TestLocalSourceReadsArchivedComponent(t *testing.T) {
	layout := newProject(t)
	spec := protoSpec("department/tree", "1.0.0")
	// 直接写进归档目录，等价于 sync 归档之后的现场
	writeComponent(t, filepath.Join(layout.Root, "components", ".archived"), spec)

	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "./components",
	}), Options{})

	fetched, err := c.Manifest(context.Background(), "department/tree", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "department/tree", fetched.Manifest.Metadata.ID)

	// 产物同样要跟着回落，否则升级/刷新时会拿不到契约文件
	res, err := c.DownloadArtifacts(context.Background(), fetched.Manifest)
	require.NoError(t, err)
	assert.Empty(t, res.Warnings, "归档组件的产物也该取得到")
	assert.Len(t, res.Downloaded, 2)
}

// 活跃目录优先于归档目录：两处都在时，读的是使用者正在编辑的那份。
func TestLocalSourcePrefersActiveOverArchived(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{
		ID: "demo/hello", Version: "1.0.0", Description: "活跃的那份",
	})
	writeComponent(t, filepath.Join(root, ".archived"), componentSpec{
		ID: "demo/hello", Version: "1.0.0", Description: "归档的那份",
	})

	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "./components",
	}), Options{})

	fetched, err := c.Manifest(context.Background(), "demo/hello", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "活跃的那份", fetched.Manifest.Metadata.Description)
}

// 归档目录不参与 add --local 的扫描：刚归档的组件不该被批量添加又拽回配置里。
//
// 与上面两个用例是同一条分工线的两半：**扫描时看不见，按 ID 找时找得到。**
func TestLocalSourceListSkipsArchived(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "demo/hello", Version: "1.0.0"})
	writeComponent(t, filepath.Join(root, ".archived"), componentSpec{ID: "demo/caller", Version: "1.0.0"})

	c := newClient(t, layout, cfgWithSources(projfile.Source{
		Name: "local-dev", Type: projfile.SourceTypeLocal, Path: "./components",
	}), Options{})

	scan, err := c.LocalComponents(context.Background())
	require.NoError(t, err)

	var ids []string
	for _, item := range scan.Components {
		ids = append(ids, item.ID)
	}
	assert.Equal(t, []string{"demo/hello"}, ids, "归档的组件不该出现在扫描结果里")
	assert.Empty(t, scan.Problems)
}
