package source

// 本文件是 git 安装源：版本就是 tag。仓库有三种来法：
//
//	baseUrl    一个组件一个仓库：sources[].baseUrl + <scope>-<name>（erp/backend → <baseUrl>erp-backend），
//	           组件在仓库根目录，tag 就是版本号（1.0.0，不带 v）
//	repo       一个仓库放多个组件（sources[].repo，monorepo）：组件各在一个子目录，tag 带命名空间
//	           <scope>-<name>/<版本>；子目录不写在任何地方，在那个 tag 的文件树里找
//	           metadata.id 是它的那份 component.yaml（componentDirs）
//	组件级     组件自己写的 source.repo（+ source.path 子目录）：只给这一个组件用
//
//	最新版本   精确版本形式的 tag 里最高的那个；v1.0.0、latest 这类 tag 不是版本
//
// 仓库本身的克隆、fetch、读文件在 gitcache.go。

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/docspec"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

// gitSource 是一个 git 安装源：按 baseUrl 推导仓库、一个 monorepo（sources[] 里的一项），
// 或者固定一个仓库（组件级 source.repo）。
type gitSource struct {
	sourceID string
	// baseURL 是组织地址；repoURL 非空时不用它。
	baseURL string
	// repoURL 是固定的仓库：sources[].repo（discover 为 true），或组件级 source.repo
	// （组件在仓库里的子目录是 subpath）。
	repoURL string
	subpath string
	// discover 表示这是一个 monorepo 源：组件的子目录在每个版本的 tag 里找。
	discover bool
	cache    *repoCache
}

func (s *gitSource) id() string   { return s.sourceID }
func (s *gitSource) kind() string { return "git" }
func (s *gitSource) close() error { return nil }

// repoFor 返回组件所在的仓库地址。
func (s *gitSource) repoFor(componentID string) string {
	if s.repoURL != "" {
		return s.repoURL
	}
	return strings.TrimRight(s.baseURL, "/") + "/" + strings.ReplaceAll(componentID, "/", "-")
}

// namespaced 报告这个源里的组件版本 tag 带不带 <scope>-<name>/ 命名空间：组件在仓库子目录里时带。
func (s *gitSource) namespaced() bool { return s.discover || s.subpath != "" }

// tagFor 是组件某个版本在这个源里的 tag。
func (s *gitSource) tagFor(componentID, version string) string {
	return tagPrefix(componentID, s.namespaced()) + version
}

// tagPrefix 是组件版本 tag 的前缀：仓库根目录下的组件没有前缀；子目录里的组件用 <scope>-<name>/。
func tagPrefix(componentID string, namespaced bool) string {
	if !namespaced {
		return ""
	}
	return strings.ReplaceAll(componentID, "/", "-") + "/"
}

// VersionTag 是组件某个版本的 git tag：<版本>，组件在仓库子目录时是 <scope>-<name>/<版本>。
func VersionTag(componentID, version, subpath string) string {
	return tagPrefix(componentID, subpath != "") + version
}

// repoWithTag 返回已经有 tag 的仓库：缓存里没有仓库先克隆，仓库里没有 tag 先 fetch。
// tag 仍然没有时 found 为 false。
func (s *gitSource) repoWithTag(ctx context.Context, componentID, tag string) (*gitRepo, bool, error) {
	repoURL := s.repoFor(componentID)
	repo := s.cache.get(repoURL)
	if err := repo.ensure(ctx); err != nil {
		return nil, false, s.failed(componentID, repoURL, err)
	}
	if s.cache.online {
		repo.refresh(ctx)
	}
	if repo.hasTag(ctx, tag) {
		return repo, true, nil
	}
	if err := repo.fetch(ctx); err != nil {
		return nil, false, s.failed(componentID, repoURL, err)
	}
	return repo, repo.hasTag(ctx, tag), nil
}

// gitVersion 是组件的一个版本在仓库里的位置：哪个仓库、哪个 tag、组件在哪个子目录。
type gitVersion struct {
	repo    *gitRepo
	tag     string
	subpath string
}

// path 是组件目录里的一个文件在仓库里的路径。
func (v gitVersion) path(file string) string { return path.Join(v.subpath, file) }

// at 找到组件某个版本：仓库有这个 tag 时 found 为 true；monorepo 源顺便找出组件在
// 这个 tag 里的子目录（找不到、找到两处都是错误，不是"这里没有"）。
func (s *gitSource) at(ctx context.Context, componentID, version string) (v gitVersion, found bool, err error) {
	tag := s.tagFor(componentID, version)
	repo, found, err := s.repoWithTag(ctx, componentID, tag)
	if err != nil || !found {
		return gitVersion{repo: repo, tag: tag}, false, err
	}
	v = gitVersion{repo: repo, tag: tag, subpath: s.subpath}
	if s.discover {
		if v.subpath, err = s.componentDir(ctx, repo, componentID, tag); err != nil {
			return v, false, err
		}
	}
	return v, true, nil
}

// componentDir 在 monorepo 的 tag 里找组件所在的子目录：metadata.id 是它的那份 component.yaml。
// 子目录不写在任何文件里，所以组件在两个版本之间换了目录也照样找得到；一处都没有、
// 或者有两处，都要说出来——随便挑一处会静默地装错东西。
func (s *gitSource) componentDir(ctx context.Context, repo *gitRepo, componentID, tag string) (string, error) {
	dirs, err := repo.componentDirs(ctx, tag)
	if err != nil {
		return "", s.failed(componentID, repo.url, err)
	}
	found := dirs.byID[componentID]
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", clierr.As(manifestUnusable(s, componentID, i18n.T(msgid.SourceGitNoComponentAtTag, tag, componentID),
			i18n.T(msgid.SourceHintGitMonorepoComponentAt))).
			WithDetail(i18n.T(msgid.SourceLabelGitComponentFiles), dirs.describe())
	default:
		return "", manifestUnusable(s, componentID, i18n.T(msgid.SourceGitComponentTwiceAtTag, tag, componentID,
			strings.Join(displayDirs(found), i18n.T(msgid.ListSeparator))), i18n.T(msgid.SourceHintGitMonorepoOneCopy))
	}
}

func (s *gitSource) manifestBytes(ctx context.Context, componentID, version string) ([]byte, error) {
	if version == "" {
		return nil, errNotFound
	}
	v, found, err := s.at(ctx, componentID, version)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, s.tagMissing(ctx, v.repo, componentID, version)
	}
	file := v.path(manifest.FileName)
	data, ok, err := v.repo.file(ctx, v.tag, file)
	if err != nil {
		return nil, s.failed(componentID, v.repo.url, err)
	}
	if !ok {
		return nil, manifestUnusable(s, componentID, i18n.T(msgid.SourceGitNoManifestAtTag, v.tag, file),
			i18n.T(msgid.SourceHintGitTagIsRelease))
	}
	return data, nil
}

func (s *gitSource) latestVersion(ctx context.Context, componentID string) (string, error) {
	repoURL := s.repoFor(componentID)
	repo := s.cache.get(repoURL)
	if err := repo.ensure(ctx); err != nil {
		return "", s.failed(componentID, repoURL, err)
	}
	if err := repo.fetch(ctx); err != nil {
		failure := s.failed(componentID, repoURL, err)
		// 最新版本以远端为准，连不上就不能回答——但不静默回落到缓存里的最高版本：
		// 它未必是远端的最新。点名缓存里已有的版本，写明其中一个就不用联网
		if cached, _ := s.versions(ctx, repo, componentID); unreachable(err) && len(cached) > 0 {
			failure = clierr.As(failure).WithHint(
				i18n.T(msgid.SourceHintGitOfflinePinVersion, componentID, strings.Join(cached, ", ")))
		}
		return "", failure
	}
	versions, err := s.versions(ctx, repo, componentID)
	if err != nil {
		return "", s.failed(componentID, repoURL, err)
	}
	if len(versions) == 0 {
		return "", errNotFound
	}
	return versions[len(versions)-1], nil
}

// versions 列出仓库里这个组件的版本（精确版本形式的 tag，带前缀的去掉前缀），从低到高。
func (s *gitSource) versions(ctx context.Context, repo *gitRepo, componentID string) ([]string, error) {
	tags, err := repo.tags(ctx)
	if err != nil {
		return nil, err
	}
	prefix := tagPrefix(componentID, s.namespaced())
	var out []string
	for _, tag := range tags {
		v, ok := strings.CutPrefix(tag, prefix)
		if ok && manifest.IsExactVersion(v) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return manifest.CompareVersions(out[i], out[j]) < 0 })
	return out, nil
}

func (s *gitSource) artifactFile(ctx context.Context, componentID, version string, _ manifest.Artifact, file string) ([]byte, error) {
	return s.fileAtVersion(ctx, componentID, version, file)
}

// docFiles 读这个版本 tag 里组件目录下的 BRICKKIT.md 与译本。
func (s *gitSource) docFiles(ctx context.Context, componentID, version string) (map[string][]byte, error) {
	v, found, err := s.at(ctx, componentID, version)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errNotFound
	}
	names, err := v.repo.names(ctx, v.tag, v.subpath)
	if err != nil {
		return nil, s.failed(componentID, v.repo.url, err)
	}
	files := map[string][]byte{}
	for _, n := range names {
		if base, _, ok := docspec.SplitTranslation(n); !ok || base != docspec.FileBrickkit {
			continue
		}
		if data, err := s.fileAtVersion(ctx, componentID, version, n); err == nil {
			files[n] = data
		}
	}
	return files, nil
}

// fileAtVersion 读组件某个版本里的文件（相对组件目录）；没有时返回 errNotFound。
func (s *gitSource) fileAtVersion(ctx context.Context, componentID, version, file string) ([]byte, error) {
	v, found, err := s.at(ctx, componentID, version)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errNotFound
	}
	data, ok, err := v.repo.file(ctx, v.tag, v.path(file))
	if err != nil {
		return nil, s.failed(componentID, v.repo.url, err)
	}
	if !ok {
		return nil, errNotFound
	}
	return data, nil
}

// gitPin 是一个组件版本取自哪个仓库的哪个 tag、当时那个 tag 指向哪个提交。它跟着清单缓存记下来：
// 版本发布了就不该再变，tag 后来指向了别的提交，缓存里这一份就不再是这个版本（Client.checkPin）。
type gitPin struct {
	Repo   string `json:"repo"`
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
}

// pin 返回这个版本此刻的 gitPin；读不出来时为 nil（不记，以后也就不核对）。
func (s *gitSource) pin(ctx context.Context, componentID, version string) *gitPin {
	v, found, err := s.at(ctx, componentID, version)
	if err != nil || !found {
		return nil
	}
	commit, ok := v.repo.tagCommit(ctx, v.tag)
	if !ok {
		return nil
	}
	return &gitPin{Repo: v.repo.url, Tag: v.tag, Commit: commit}
}

func (s *gitSource) origin(ctx context.Context, componentID, version string) (*Origin, error) {
	// 只有这个仓库真有这个版本时才是它的来源：排在后面的源才可能是真正的提供方
	v, found, err := s.at(ctx, componentID, version)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errNotFound
	}
	return &Origin{
		SourceID: s.sourceID, Type: OriginGit, GitURL: v.repo.url, Tag: v.tag, CacheDir: v.repo.dir,
	}, nil
}

// failed 把一次 git 调用的失败做成给人看的错误：点名组件与仓库地址，原样带出 git 的报错，
// 给出宿主机鉴权的三个检查方向。
func (s *gitSource) failed(componentID, repoURL string, err error) error {
	if errors.Is(err, errNoRepoCache) {
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SourceNoRepoCache, componentID)).
			WithDetail(i18n.T(msgid.LabelRepo), repoURL).
			WithHint(i18n.T(msgid.SourceHintSetCacheHome))
	}
	// 错误码只分两类：根本没连上（值得原样重试），与连上了却被拒绝（鉴权失败，或者仓库不存在——
	// 托管平台对这两种常给同一句话）。后一类原样重试多少次都一样，报成网络错误会让脚本一直重试下去。
	code := clierr.CodeAuthFailed
	if unreachable(err) {
		code = clierr.CodeNetworkUnreachable
	}
	e := clierr.New(code, i18n.T(msgid.SourceGitFetchFailed, componentID)).
		WithDetail(i18n.T(msgid.LabelSource), i18n.T(msgid.SourceIDWithKind, s.id(), s.kind())).
		WithDetail(i18n.T(msgid.LabelRepo), repoURL).
		WithDetail(i18n.T(msgid.SourceLabelGitError), lastLines(err.Error(), 10))
	if code == clierr.CodeNetworkUnreachable {
		// 根本没连上：谈 SSH key、credential helper 只会把人引到错的方向
		e = e.WithHint(i18n.T(msgid.SourceHintGitNetwork))
	} else {
		e = e.WithHint(
			i18n.T(msgid.SourceHintGitSSH),
			i18n.T(msgid.SourceHintGitHTTPS),
			i18n.T(msgid.SourceHintGitCI),
		)
	}
	return e.WithCause(err)
}

// unreachableSignatures 是 git（curl、ssh）连不上远端时的原话：离线、主机名解析不了、端口没人听。
// 鉴权失败、仓库不存在不在其中——那时远端是连上了的。
var unreachableSignatures = []string{
	"could not resolve host",
	"could not resolve hostname",
	"temporary failure in name resolution",
	"failed to connect to",
	"connection refused",
	"connection timed out",
	"operation timed out",
	"network is unreachable",
	"no route to host",
}

// unreachable 报告一次 git 失败是不是"根本没连上远端"。
func unreachable(err error) bool {
	text := strings.ToLower(err.Error())
	for _, sig := range unreachableSignatures {
		if strings.Contains(text, sig) {
			return true
		}
	}
	return false
}

// tagMissing 说清楚"仓库在，但没有这个版本的 tag"，列出仓库里有的版本。
//
// monorepo 源里一个版本都没有的组件不在这个仓库里：那是"这里没有"，交给下一个源，
// 不该报成这个仓库缺了一个 tag。
func (s *gitSource) tagMissing(ctx context.Context, repo *gitRepo, componentID, version string) error {
	versions, _ := s.versions(ctx, repo, componentID)
	if s.discover && len(versions) == 0 {
		return errNotFound
	}
	available := strings.Join(versions, ", ")
	if available == "" {
		available = i18n.T(msgid.SourceGitNoVersionTags)
	}
	return clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.SourceGitTagMissing, componentID+"@"+version)).
		WithDetail(i18n.T(msgid.LabelRepo), repo.url).
		WithDetail(i18n.T(msgid.SourceLabelGitTag), s.tagFor(componentID, version)).
		WithDetail(i18n.T(msgid.SourceLabelGitVersions), available).
		WithHint(i18n.T(msgid.SourceHintGitTagIsRelease))
}

// lastLines 取文本的最后 n 行：git 的报错可能很长，要紧的通常在最后。
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
