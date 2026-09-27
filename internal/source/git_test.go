package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/source/gittest"
)

// gitOrg 是一个"组织"：下面的每个组件仓库按 <scope>-<name> 命名，baseUrl 指向它。
type gitOrg struct {
	t       *testing.T
	dir     string
	cache   string
	remotes map[string]*gittest.Remote
}

func newGitOrg(t *testing.T) *gitOrg {
	return &gitOrg{t: t, dir: t.TempDir(), cache: t.TempDir(), remotes: map[string]*gittest.Remote{}}
}

// release 在组件仓库里打一个版本 tag（仓库根目录就是组件）。
func (o *gitOrg) release(spec componentSpec) *gittest.Remote {
	o.t.Helper()
	r := o.remote(spec.ID)
	r.Tag(spec.Version, specFiles(spec))
	return r
}

func (o *gitOrg) remote(id string) *gittest.Remote {
	o.t.Helper()
	if r, ok := o.remotes[id]; ok {
		return r
	}
	r := gittest.NewRemoteIn(o.t, o.dir, strings.ReplaceAll(id, "/", "-"))
	o.remotes[id] = r
	return r
}

func specFiles(spec componentSpec) map[string]string {
	files := map[string]string{"component.yaml": spec.yamlText()}
	for k, v := range spec.Files {
		files[k] = v
	}
	return files
}

// client 返回一个只配了这个 git 组织的项目客户端（每次一个新项目，共用同一个用户级仓库缓存）。
func (o *gitOrg) client(components ...projfile.Component) (*Client, project.Layout) {
	o.t.Helper()
	layout := newProject(o.t)
	cfg := cfgWithSources(projfile.Source{Name: "org", Type: projfile.SourceTypeGit, BaseURL: gittest.BaseURL(o.dir)})
	cfg.Components = components
	return newClient(o.t, layout, cfg, Options{RepoCacheDir: o.cache}), layout
}

func TestGitManifestAtTag(t *testing.T) {
	org := newGitOrg(t)
	org.release(componentSpec{ID: "erp/api", Version: "1.0.0", Description: "first"})
	org.release(componentSpec{ID: "erp/api", Version: "1.1.0", Description: "second"})
	c, layout := org.client()

	got, err := c.Manifest(context.Background(), "erp/api", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "first", got.Manifest.Metadata.Description)
	assert.FileExists(t, layout.CachedManifestPath("erp/api", "1.0.0"))

	got, err = c.Manifest(context.Background(), "erp/api", "1.1.0")
	require.NoError(t, err)
	assert.Equal(t, "second", got.Manifest.Metadata.Description)
}

// 提案 §9.4/9.5、附录 A12：仓库克隆一次后放在用户级缓存里，多个项目共用；
// 远端没了，另一个项目照样能读到之前见过的版本。
func TestGitReadsCachedRepoWithoutNetwork(t *testing.T) {
	org := newGitOrg(t)
	remote := org.release(componentSpec{ID: "erp/api", Version: "1.0.0"})
	org.release(componentSpec{ID: "erp/api", Version: "1.1.0"})
	first, _ := org.client()
	_, err := first.Manifest(context.Background(), "erp/api", "1.0.0")
	require.NoError(t, err)

	remote.Remove()
	second, _ := org.client()
	for _, v := range []string{"1.0.0", "1.1.0"} {
		got, err := second.Manifest(context.Background(), "erp/api", v)
		require.NoError(t, err, v)
		assert.Equal(t, v, got.Manifest.Metadata.Version)
	}
}

// 缓存的仓库里没有要的 tag：增量 fetch 一次再读（读取优先级第 3 级）。
func TestGitFetchesNewTag(t *testing.T) {
	org := newGitOrg(t)
	org.release(componentSpec{ID: "erp/api", Version: "1.0.0"})
	first, _ := org.client()
	_, err := first.Manifest(context.Background(), "erp/api", "1.0.0")
	require.NoError(t, err)

	org.release(componentSpec{ID: "erp/api", Version: "1.2.0"})
	second, _ := org.client()
	got, err := second.Manifest(context.Background(), "erp/api", "1.2.0")
	require.NoError(t, err)
	assert.Equal(t, "1.2.0", got.Manifest.Metadata.Version)
}

func TestGitLatestIsHighestExactTag(t *testing.T) {
	org := newGitOrg(t)
	r := org.release(componentSpec{ID: "erp/api", Version: "1.2.0"})
	org.release(componentSpec{ID: "erp/api", Version: "1.10.0"})
	r.Tag("v2.0.0", specFiles(componentSpec{ID: "erp/api", Version: "2.0.0"}))
	r.Tag("latest", specFiles(componentSpec{ID: "erp/api", Version: "3.0.0"}))
	c, _ := org.client()

	latest, err := c.LatestVersion(context.Background(), "erp/api")
	require.NoError(t, err)
	assert.Equal(t, "1.10.0", latest.Version)
}

// 附录 A9：组件在仓库子目录里（monorepo）时，tag 带命名空间 <scope>-<name>/<版本>；
// 同一个仓库里不带命名空间的 tag 属于别的东西，不认。
func TestGitMonorepoNamespacedTag(t *testing.T) {
	mono := gittest.NewRemote(t, "platform")
	mono.Tag("1.0.0", map[string]string{"README.md": "not a component"})
	mono.TagAt("packages/erp-api", "erp-api/1.0.0", specFiles(componentSpec{ID: "erp/api", Version: "1.0.0", Description: "from monorepo"}))
	mono.TagAt("packages/erp-api", "erp-api/1.1.0", specFiles(componentSpec{ID: "erp/api", Version: "1.1.0"}))

	layout := newProject(t)
	cfg := cfgWithSources(projfile.Source{Name: "org", Type: projfile.SourceTypeGit, BaseURL: "file:///nowhere/"})
	cfg.Components = []projfile.Component{{ID: "erp/api", Version: "1.0.0",
		Source: &projfile.ComponentSource{Type: projfile.SourceTypeGit, Repo: mono.URL(), Path: "packages/erp-api"}}}
	c := newClient(t, layout, cfg, Options{RepoCacheDir: t.TempDir()})

	got, err := c.Manifest(context.Background(), "erp/api", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "from monorepo", got.Manifest.Metadata.Description)
	latest, err := c.LatestVersion(context.Background(), "erp/api")
	require.NoError(t, err)
	assert.Equal(t, "1.1.0", latest.Version)
}

func TestGitMissingTagListsExisting(t *testing.T) {
	org := newGitOrg(t)
	org.release(componentSpec{ID: "erp/api", Version: "1.0.0"})
	org.release(componentSpec{ID: "erp/api", Version: "1.1.0"})
	c, _ := org.client()

	_, err := c.Manifest(context.Background(), "erp/api", "9.9.9")
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Format(), "1.0.0, 1.1.0")
}

// 提案 §9.9：鉴权、仓库不存在这类失败原样带出 git 的报错，再给三条检查方向；绝不挂住等输入。
func TestGitAuthFailurePassesStderr(t *testing.T) {
	org := newGitOrg(t)
	c, _ := org.client()

	_, err := c.Manifest(context.Background(), "erp/missing", "1.0.0")
	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "erp-missing", "点名试过的仓库地址")
	assert.Contains(t, text, "fatal:", "带出 git 自己的报错")
	assert.Contains(t, text, "SSH")
	assert.Contains(t, text, "credential")
}

// 克隆失败（或被中断）不留下半个仓库：下一次从头克隆。
func TestGitFailedCloneLeavesNoRepo(t *testing.T) {
	org := newGitOrg(t)
	org.release(componentSpec{ID: "erp/api", Version: "1.0.0"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := org.client()
	_, err := c.Manifest(ctx, "erp/api", "1.0.0")
	require.Error(t, err)
	entries, _ := os.ReadDir(org.cache)
	for _, e := range entries {
		assert.Equal(t, ".tmp", e.Name(), "只有临时目录，没有半个仓库")
	}

	c, _ = org.client()
	_, err = c.Manifest(context.Background(), "erp/api", "1.0.0")
	require.NoError(t, err)
}

func TestRepoCacheDirNormalisesAddresses(t *testing.T) {
	root := filepath.FromSlash("/cache")
	cases := map[string]string{
		"https://github.com/myorg/erp-api":         "github.com/myorg/erp-api.git",
		"https://github.com/myorg/erp-api.git":     "github.com/myorg/erp-api.git",
		"https://user:tok@github.com/myorg/x/":     "github.com/myorg/x.git",
		"https://git.example.com:8443/a/b":         "git.example.com_8443/a/b.git",
		"ssh://git@github.com/myorg/erp-api.git":   "github.com/myorg/erp-api.git",
		"git@github.com:myorg/erp-api.git":         "github.com/myorg/erp-api.git",
		"file:///srv/repos/erp-api":                "local/srv/repos/erp-api.git",
		"/srv/repos/erp-api.git":                   "local/srv/repos/erp-api.git",
		"https://github.com/myorg/../escape/x.git": "github.com/myorg/escape/x.git",
	}
	for url, want := range cases {
		assert.Equal(t, filepath.Join(root, filepath.FromSlash(want)), repoCacheDir(root, url), url)
	}
}

// 组件自己写了 source.repo：只从那个仓库取，不按 baseUrl 推导（提案 §9.3）。
func TestComponentSourceOverride(t *testing.T) {
	org := newGitOrg(t)
	org.release(componentSpec{ID: "third/pay", Version: "1.0.0", Description: "from baseUrl"})
	other := gittest.NewRemote(t, "payment-gateway")
	other.Tag("1.0.0", specFiles(componentSpec{ID: "third/pay", Version: "1.0.0", Description: "from override"}))

	c, _ := org.client(projfile.Component{ID: "third/pay", Version: "1.0.0",
		Source: &projfile.ComponentSource{Type: projfile.SourceTypeGit, Repo: other.URL()}})
	got, err := c.Manifest(context.Background(), "third/pay", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "from override", got.Manifest.Metadata.Description)

	origin, err := c.Origin(context.Background(), "third/pay", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, OriginGit, origin.Type)
	assert.Equal(t, other.URL(), origin.GitURL)
	assert.Equal(t, "1.0.0", origin.Tag)
}

// 产物与组件文档同样从 tag 里读。
func TestGitArtifactsAndDoc(t *testing.T) {
	org := newGitOrg(t)
	org.release(componentSpec{ID: "erp/api", Version: "1.0.0",
		Artifacts: []artifactSpec{{Type: "api-docs", Files: []string{"docs/openapi.json"}}},
		Files:     map[string]string{"docs/openapi.json": `{"openapi":"3.0.0"}`, "BRICKKIT.md": "# erp/api\n"}})
	c, layout := org.client()

	got, err := c.Manifest(context.Background(), "erp/api", "1.0.0")
	require.NoError(t, err)
	res, err := c.DownloadArtifacts(context.Background(), got.Manifest)
	require.NoError(t, err)
	require.Empty(t, res.Warnings)
	assert.Equal(t, `{"openapi":"3.0.0"}`, readFile(t, filepath.Join(c.ArtifactDir("erp/api", "1.0.0"), "api-docs", "docs", "openapi.json")))
	assert.Equal(t, "# erp/api\n", readFile(t, layout.CachedDocPath("erp/api", "1.0.0")))
}

func TestProjfileGitSourceValidation(t *testing.T) {
	cases := map[string]struct {
		src  projfile.ComponentSource
		want string
	}{
		"path escapes":  {projfile.ComponentSource{Type: projfile.SourceTypeGit, Repo: "https://x/y", Path: "../other"}, "components[0].source.path"},
		"path absolute": {projfile.ComponentSource{Type: projfile.SourceTypeGit, Repo: "https://x/y", Path: "/abs"}, "components[0].source.path"},
	}
	for name, tc := range cases {
		src := tc.src
		f := &projfile.File{Project: "p", Components: []projfile.Component{{ID: "erp/api", Version: "1.0.0", Source: &src}}}
		err := f.Validate()
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), tc.want, name)
	}

	f := &projfile.File{Project: "p", Components: []projfile.Component{
		{ID: "erp/api", Version: "1.1.0", Source: &projfile.ComponentSource{Type: projfile.SourceTypeGit, Repo: "https://x/a"}},
		{ID: "erp/api", Version: "1.0.0", RequiredBy: []string{"erp/other"}, Source: &projfile.ComponentSource{Type: projfile.SourceTypeGit, Repo: "https://x/b"}},
		{ID: "erp/other", Version: "1.0.0"},
	}}
	err := f.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[1].source", "同一个组件的几行来源不一致")
}

// 两个 git 组织：组件只在第二个里。第一个的仓库在、却没有这个版本——来源（--repo 用）
// 要落到真正有它的那一个。
func TestGitOriginIsTheSourceThatHasTheVersion(t *testing.T) {
	first, second := newGitOrg(t), newGitOrg(t)
	first.release(componentSpec{ID: "erp/api", Version: "0.1.0"})
	second.release(componentSpec{ID: "erp/api", Version: "1.0.0"})
	cfg := cfgWithSources(
		projfile.Source{Name: "first", Type: projfile.SourceTypeGit, BaseURL: gittest.BaseURL(first.dir)},
		projfile.Source{Name: "second", Type: projfile.SourceTypeGit, BaseURL: gittest.BaseURL(second.dir)},
	)
	c := newClient(t, newProject(t), cfg, Options{RepoCacheDir: t.TempDir()})

	origin, err := c.Origin(context.Background(), "erp/api", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "second", origin.SourceID)
	got, err := c.Manifest(context.Background(), "erp/api", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", got.Manifest.Metadata.Version)
}

// 仓库在、却一个版本 tag 都没有：不是"这里有它"。
func TestGitLatestWithoutVersionTags(t *testing.T) {
	org := newGitOrg(t)
	r := org.remote("erp/api")
	r.Tag("draft", specFiles(componentSpec{ID: "erp/api", Version: "0.0.1"}))
	c, _ := org.client()
	_, err := c.LatestVersion(context.Background(), "erp/api")
	require.Error(t, err)
}
