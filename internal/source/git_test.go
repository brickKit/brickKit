package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
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

// 精心构造的地址（主机名写成 ..）不能让缓存目录跑出缓存根目录。
func TestRepoCacheDirNeverLeavesTheRoot(t *testing.T) {
	root := filepath.FromSlash("/cache")
	for _, url := range []string{"https://../evil", "git@..:x/y", "https://./a/../../b", "file://../../etc"} {
		dir := repoCacheDir(root, url)
		rel, err := filepath.Rel(root, dir)
		require.NoError(t, err, url)
		assert.False(t, strings.HasPrefix(rel, ".."), "%s → %s", url, dir)
	}
}

// 拿不到用户缓存目录时报错，而不是把仓库克隆到当前目录里。
func TestGitWithoutCacheDirIsAnError(t *testing.T) {
	org := newGitOrg(t)
	org.release(componentSpec{ID: "erp/api", Version: "1.0.0"})
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	cwd := t.TempDir()
	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(cwd))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	cfg := cfgWithSources(projfile.Source{Name: "org", Type: projfile.SourceTypeGit, BaseURL: gittest.BaseURL(org.dir)})
	c := newClient(t, newProject(t), cfg, Options{})
	_, err = c.Manifest(context.Background(), "erp/api", "1.0.0")
	require.Error(t, err)
	entries, _ := os.ReadDir(cwd)
	assert.Empty(t, entries, "当前目录里什么都不能留下")
}

// 进程在克隆途中被杀掉会留下 .tmp/clone-*：下一次克隆时清掉放了很久的（正在进行的不碰）。
func TestGitSweepsStaleTempClones(t *testing.T) {
	org := newGitOrg(t)
	org.release(componentSpec{ID: "erp/api", Version: "1.0.0"})
	stale := filepath.Join(org.cache, ".tmp", "clone-stale")
	fresh := filepath.Join(org.cache, ".tmp", "clone-fresh")
	for _, d := range []string{stale, fresh} {
		require.NoError(t, os.MkdirAll(filepath.Join(d, "objects"), 0o755))
	}
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))

	c, _ := org.client()
	_, err := c.Manifest(context.Background(), "erp/api", "1.0.0")
	require.NoError(t, err)
	assert.NoDirExists(t, stale)
	assert.DirExists(t, fresh, "可能是另一个进程正在克隆")
}

// git 启动之后才失败的克隆（地址是个普通目录，不是仓库）同样不留下任何东西。
func TestGitCloneFailingAfterStartLeavesNoRepo(t *testing.T) {
	notARepo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(notARepo, "README"), []byte("x"), 0o644))
	cache := t.TempDir()
	repo := newRepoCache(cache).get("file://" + filepath.ToSlash(notARepo))
	require.Error(t, repo.ensure(context.Background()))
	assert.NoDirExists(t, repo.dir)
	entries, _ := os.ReadDir(filepath.Join(cache, ".tmp"))
	assert.Empty(t, entries)
}

// 两个进程同时克隆同一个仓库：都成功，缓存里只有一份完整仓库，临时目录都清掉了。
func TestGitConcurrentClonesShareOneRepo(t *testing.T) {
	org := newGitOrg(t)
	remote := org.release(componentSpec{ID: "erp/api", Version: "1.0.0"})
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { errs <- newRepoCache(org.cache).get(remote.URL()).ensure(context.Background()) }()
	}
	for i := 0; i < 4; i++ {
		require.NoError(t, <-errs)
	}
	assert.DirExists(t, repoCacheDir(org.cache, remote.URL()))
	entries, _ := os.ReadDir(filepath.Join(org.cache, ".tmp"))
	assert.Empty(t, entries)
}

// 读 tag 里的文件时 git 自己失败了（被取消、仓库坏了）：是错误，不是"没有这个文件"。
func TestGitFileFailureIsNotMissing(t *testing.T) {
	org := newGitOrg(t)
	remote := org.release(componentSpec{ID: "erp/api", Version: "1.0.0"})
	repo := newRepoCache(org.cache).get(remote.URL())
	require.NoError(t, repo.ensure(context.Background()))

	_, ok, err := repo.file(context.Background(), "1.0.0", "nope.txt")
	require.NoError(t, err)
	assert.False(t, ok, "真没有这个文件")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = repo.file(ctx, "1.0.0", "component.yaml")
	assert.Error(t, err)
}

// 真的鉴权失败（HTTPS 回 401）：GIT_TERMINAL_PROMPT=0 让 git 立刻失败而不是等输入密码，
// 报错带着 git 自己的说法与三条检查方向。
func TestGitAuthRequiredFailsWithoutPrompting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	layout := newProject(t)
	cfg := cfgWithSources(projfile.Source{Name: "org", Type: projfile.SourceTypeGit, BaseURL: srv.URL + "/myorg/"})
	c := newClient(t, layout, cfg, Options{RepoCacheDir: t.TempDir()})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := c.Manifest(ctx, "erp/api", "1.0.0")
	require.Error(t, err)
	require.NoError(t, ctx.Err(), "没有挂住等密码")
	text := clierr.As(err).Format()
	assert.Contains(t, text, srv.URL+"/myorg/erp-api")
	assert.Contains(t, text, "credential")
}

// 仓库地址写错（或仓库不存在）：点名那个地址、带出 git 自己的话，并提醒"地址写错也是这个样子"——
// git 对"不存在"与"没权限"给的是同一句话，只说鉴权会把人引去查一把本来就没问题的钥匙。
func TestGitMissingRepositoryNamesTheAddress(t *testing.T) {
	org := newGitOrg(t)
	c, _ := org.client()

	_, err := c.Manifest(context.Background(), "erp/api", "1.0.0")

	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeNetworkUnreachable, e.Code)
	out := e.Format()
	assert.Contains(t, out, gittest.BaseURL(org.dir), "报出的是它实际去找的那个地址")
	assert.Contains(t, out, "erp-api")
	assert.Contains(t, out, "mistyped", "提醒地址写错也会这样失败")
}

// 连不上远端（离线、主机名解析不了）时，建议不能只谈鉴权；查"最新版本"又必须联网（附录 A8），
// 所以点名本机缓存里已有的版本，告诉使用者写明版本号就不用联网。不静默回落到缓存里的最高版本：
// 那可能不是远端的最新，而使用者以为是。
func TestGitLatestOfflineNamesCachedVersions(t *testing.T) {
	org := newGitOrg(t)
	org.release(componentSpec{ID: "erp/api", Version: "1.0.0"})
	r := org.release(componentSpec{ID: "erp/api", Version: "1.2.0"})
	first, _ := org.client()
	_, err := first.LatestVersion(context.Background(), "erp/api")
	require.NoError(t, err)

	// 远端变得不可达：缓存里的 origin 指向一个没人监听的端口（连接被拒绝，不依赖 DNS）
	cached := repoCacheDir(org.cache, r.URL())
	_, err = runGit(context.Background(), cached, "remote", "set-url", "origin", "http://127.0.0.1:1/erp-api")
	require.NoError(t, err)

	second, _ := org.client()
	_, err = second.LatestVersion(context.Background(), "erp/api")
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeNetworkUnreachable, e.Code)
	assert.Contains(t, e.Hints, i18n.T(msgid.SourceHintGitNetwork))
	assert.NotContains(t, e.Hints, i18n.T(msgid.SourceHintGitSSH), "连不上远端时不该把人引向 SSH key")
	assert.Contains(t, e.Hints, i18n.T(msgid.SourceHintGitOfflinePinVersion, "erp/api", "1.0.0, 1.2.0"))

	// 写明一个缓存里已有的版本：不联网也能取到
	got, err := second.Manifest(context.Background(), "erp/api", "1.2.0")
	require.NoError(t, err)
	assert.Equal(t, "1.2.0", got.Manifest.Metadata.Version)
}

// 鉴权一类的失败（仓库不存在、没有权限）照旧给三个鉴权方向。
func TestGitAuthFailureKeepsAuthHints(t *testing.T) {
	s := &gitSource{}
	err := s.failed("erp/api", "https://git.example.com/erp-api",
		errors.New("fatal: could not read Username for 'https://git.example.com': terminal prompts disabled"))
	e := clierr.As(err)
	assert.Contains(t, e.Hints, i18n.T(msgid.SourceHintGitSSH))
	assert.NotContains(t, e.Hints, i18n.T(msgid.SourceHintGitNetwork))
}
