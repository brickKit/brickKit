package source

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/source/gittest"
)

// 本地安装源给出的正是这个版本时才算"本地"：目录已经升到 1.1.0，1.0.0 就不是本地的代码了。
func TestIsLocalIsVersionExact(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{ID: "erp/api", Version: "1.1.0"})
	c := newClient(t, layout, cfgWithSources(projfile.Source{Name: "dev", Type: projfile.SourceTypeLocal, Path: "./components"}), Options{})
	assert.True(t, c.IsLocal(context.Background(), "erp/api", "1.1.0"))
	assert.False(t, c.IsLocal(context.Background(), "erp/api", "1.0.0"))
	assert.False(t, c.IsLocal(context.Background(), "erp/none", "1.0.0"))
}

// 导出的是 tag 里的那一份，不是仓库的最新提交。
func TestExportSourceFromTag(t *testing.T) {
	org := newGitOrg(t)
	org.release(componentSpec{ID: "erp/api", Version: "1.0.0", Files: map[string]string{"main.go": "v1", "sub/x.txt": "x"}})
	org.release(componentSpec{ID: "erp/api", Version: "1.1.0", Files: map[string]string{"main.go": "v2"}})
	c, _ := org.client()

	dest := t.TempDir()
	require.NoError(t, c.ExportSource(context.Background(), "erp/api", "1.0.0", dest))
	assert.Equal(t, "v1", readFile(t, filepath.Join(dest, "main.go")))
	assert.Equal(t, "x", readFile(t, filepath.Join(dest, "sub", "x.txt")))
	assert.FileExists(t, filepath.Join(dest, "component.yaml"))
}

// monorepo 里的组件：导出的是它那个子目录，作为根。
func TestExportSourceMonorepoSubpath(t *testing.T) {
	mono := gittest.NewRemote(t, "platform")
	mono.TagAt("packages/erp-api", "erp-api/1.0.0", specFiles(componentSpec{ID: "erp/api", Version: "1.0.0", Files: map[string]string{"Dockerfile": "FROM scratch"}}))
	cfg := cfgWithSources()
	cfg.Components = []projfile.Component{{ID: "erp/api", Version: "1.0.0",
		Source: &projfile.ComponentSource{Type: projfile.SourceTypeGit, Repo: mono.URL(), Path: "packages/erp-api"}}}
	c := newClient(t, newProject(t), cfg, Options{RepoCacheDir: t.TempDir()})

	dest := t.TempDir()
	require.NoError(t, c.ExportSource(context.Background(), "erp/api", "1.0.0", dest))
	assert.Equal(t, "FROM scratch", readFile(t, filepath.Join(dest, "Dockerfile")))
	assert.NoDirExists(t, filepath.Join(dest, "packages"))
}

// 远端没了、缓存里有：照样导出。
func TestExportSourceOfflineFromCache(t *testing.T) {
	org := newGitOrg(t)
	remote := org.release(componentSpec{ID: "erp/api", Version: "1.0.0", Files: map[string]string{"main.go": "v1"}})
	first, _ := org.client()
	_, err := first.Manifest(context.Background(), "erp/api", "1.0.0")
	require.NoError(t, err)
	remote.Remove()

	c, _ := org.client()
	dest := t.TempDir()
	require.NoError(t, c.ExportSource(context.Background(), "erp/api", "1.0.0", dest))
	assert.Equal(t, "v1", readFile(t, filepath.Join(dest, "main.go")))
}

// 市场只给 Manifest 与镜像引用，没有源码可导出。
func TestExportSourceMarketHasNone(t *testing.T) {
	mock := newMarketMock(t, componentSpec{ID: "erp/api", Version: "1.0.0"})
	c := newClient(t, newProject(t), cfgWithSources(projfile.Source{Name: "m", Type: projfile.SourceTypeMarket, URL: mock.URL()}), Options{})
	err := c.ExportSource(context.Background(), "erp/api", "1.0.0", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erp/api@1.0.0")
}

// 归档里的条目、链接不能把文件写到目标目录外面（一个恶意 tag 就能做到）。
func TestUntarRejectsEscapes(t *testing.T) {
	for name, hdr := range map[string]tar.Header{
		"dotdot":   {Name: "../evil.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
		"absolute": {Name: "/tmp/evil.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
	} {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		h := hdr
		require.NoError(t, tw.WriteHeader(&h))
		_, _ = tw.Write([]byte("x"))
		require.NoError(t, tw.Close())
		dest := t.TempDir()
		err := untar(&buf, dest)
		if name == "absolute" && err == nil {
			// 绝对路径被接到 dest 下面：落在里面就不算逃逸
			assert.FileExists(t, filepath.Join(dest, "tmp", "evil.txt"))
			continue
		}
		require.Error(t, err, name)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"}))
	require.NoError(t, tw.Close())
	dest := t.TempDir()
	require.NoError(t, untar(&buf, dest))
	_, err := os.Lstat(filepath.Join(dest, "link"))
	assert.True(t, os.IsNotExist(err), "指到外面的链接不建")
}
