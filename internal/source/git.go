package source

// 本文件是 git 安装源（提案 §9）：一个组件一个仓库，一个版本一个 tag。
//
//	仓库地址   sources[].baseUrl + <scope>-<name>（erp/backend → <baseUrl>erp-backend）；
//	           组件自己写了 source.repo 时用它（提案 §9.3）
//	版本       tag 就是版本号（1.0.0，不带 v）；组件在仓库子目录（source.path）时
//	           tag 带命名空间 <scope>-<name>/<版本>（附录 A9），同一个 monorepo 里的
//	           组件各打各的 tag
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
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
)

// gitSource 是一个 git 安装源：按 baseUrl 推导仓库（sources[] 里的一项），
// 或者固定一个仓库（组件级 source.repo）。
type gitSource struct {
	sourceID string
	// baseURL 是组织地址；repoURL 非空时不用它。
	baseURL string
	// repoURL 与 subpath 是组件级来源：固定仓库、组件在仓库里的子目录。
	repoURL string
	subpath string
	cache   *repoCache
}

func (s *gitSource) id() string   { return s.sourceID }
func (s *gitSource) kind() string { return "git" }
func (s *gitSource) close() error { return nil }

// locate 返回组件的仓库地址与它在仓库里的子目录。
func (s *gitSource) locate(componentID string) (repoURL, subpath string) {
	if s.repoURL != "" {
		return s.repoURL, s.subpath
	}
	return strings.TrimRight(s.baseURL, "/") + "/" + strings.ReplaceAll(componentID, "/", "-"), ""
}

// tagPrefix 是这个组件的 tag 前缀：仓库根目录下的组件没有前缀；子目录里的组件用
// <scope>-<name>/（附录 A9）。
func tagPrefix(componentID, subpath string) string {
	if subpath == "" {
		return ""
	}
	return strings.ReplaceAll(componentID, "/", "-") + "/"
}

// VersionTag 是组件某个版本的 git tag：<版本>，组件在仓库子目录时是 <scope>-<name>/<版本>。
func VersionTag(componentID, version, subpath string) string {
	return tagPrefix(componentID, subpath) + version
}

// repoWithTag 返回已经有 tag 的仓库：缓存里没有仓库先克隆，仓库里没有 tag 先 fetch。
// tag 仍然没有时 found 为 false。
func (s *gitSource) repoWithTag(ctx context.Context, componentID, tag string) (*gitRepo, bool, error) {
	repoURL, _ := s.locate(componentID)
	repo := s.cache.get(repoURL)
	if err := repo.ensure(ctx); err != nil {
		return nil, false, s.failed(componentID, repoURL, err)
	}
	if repo.hasTag(ctx, tag) {
		return repo, true, nil
	}
	if err := repo.fetch(ctx); err != nil {
		return nil, false, s.failed(componentID, repoURL, err)
	}
	return repo, repo.hasTag(ctx, tag), nil
}

func (s *gitSource) manifestBytes(ctx context.Context, componentID, version string) ([]byte, error) {
	if version == "" {
		return nil, errNotFound
	}
	repoURL, subpath := s.locate(componentID)
	tag := tagPrefix(componentID, subpath) + version
	repo, found, err := s.repoWithTag(ctx, componentID, tag)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, s.tagMissing(ctx, repo, componentID, version, repoURL)
	}
	file := path.Join(subpath, manifest.FileName)
	data, ok, err := repo.file(ctx, tag, file)
	if err != nil {
		return nil, s.failed(componentID, repoURL, err)
	}
	if !ok {
		return nil, manifestUnusable(s, componentID, i18n.T(msgid.SourceGitNoManifestAtTag, tag, file),
			i18n.T(msgid.SourceHintGitTagIsRelease))
	}
	return data, nil
}

func (s *gitSource) latestVersion(ctx context.Context, componentID string) (string, error) {
	repoURL, subpath := s.locate(componentID)
	repo := s.cache.get(repoURL)
	if err := repo.ensure(ctx); err != nil {
		return "", s.failed(componentID, repoURL, err)
	}
	if err := repo.fetch(ctx); err != nil {
		return "", s.failed(componentID, repoURL, err)
	}
	versions, err := s.versions(ctx, repo, componentID, subpath)
	if err != nil {
		return "", s.failed(componentID, repoURL, err)
	}
	if len(versions) == 0 {
		return "", errNotFound
	}
	return versions[len(versions)-1], nil
}

// versions 列出仓库里这个组件的版本（精确版本形式的 tag，带前缀的去掉前缀），从低到高。
func (s *gitSource) versions(ctx context.Context, repo *gitRepo, componentID, subpath string) ([]string, error) {
	tags, err := repo.tags(ctx)
	if err != nil {
		return nil, err
	}
	prefix := tagPrefix(componentID, subpath)
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

func (s *gitSource) docBytes(ctx context.Context, componentID, version string) ([]byte, error) {
	return s.fileAtVersion(ctx, componentID, version, project.FileCachedDoc)
}

// fileAtVersion 读组件某个版本里的文件（相对组件目录）；没有时返回 errNotFound。
func (s *gitSource) fileAtVersion(ctx context.Context, componentID, version, file string) ([]byte, error) {
	repoURL, subpath := s.locate(componentID)
	tag := tagPrefix(componentID, subpath) + version
	repo, found, err := s.repoWithTag(ctx, componentID, tag)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errNotFound
	}
	data, ok, err := repo.file(ctx, tag, path.Join(subpath, file))
	if err != nil {
		return nil, s.failed(componentID, repoURL, err)
	}
	if !ok {
		return nil, errNotFound
	}
	return data, nil
}

func (s *gitSource) origin(ctx context.Context, componentID, version string) (*Origin, error) {
	repoURL, subpath := s.locate(componentID)
	// 只有这个仓库真有这个版本时才是它的来源：排在后面的源才可能是真正的提供方
	if _, found, err := s.repoWithTag(ctx, componentID, tagPrefix(componentID, subpath)+version); err != nil || !found {
		if err != nil {
			return nil, err
		}
		return nil, errNotFound
	}
	return &Origin{
		SourceID: s.sourceID, Type: OriginGit, GitURL: repoURL,
		Subpath: subpath, Tag: tagPrefix(componentID, subpath) + version,
		CacheDir: s.cache.get(repoURL).dir,
	}, nil
}

// failed 把一次 git 调用的失败做成给人看的错误：点名组件与仓库地址，原样带出 git 的报错，
// 给出宿主机鉴权的三个检查方向（提案 §9.9）。
func (s *gitSource) failed(componentID, repoURL string, err error) error {
	if errors.Is(err, errNoRepoCache) {
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SourceNoRepoCache, componentID)).
			WithDetail(i18n.T(msgid.LabelRepo), repoURL).
			WithHint(i18n.T(msgid.SourceHintSetCacheHome))
	}
	e := clierr.New(clierr.CodeNetworkUnreachable, i18n.T(msgid.SourceGitFetchFailed, componentID)).
		WithDetail(i18n.T(msgid.LabelSource), i18n.T(msgid.SourceIDWithKind, s.id(), s.kind())).
		WithDetail(i18n.T(msgid.LabelRepo), repoURL).
		WithDetail(i18n.T(msgid.SourceLabelGitError), lastLines(err.Error(), 10)).
		WithHint(
			i18n.T(msgid.SourceHintGitSSH),
			i18n.T(msgid.SourceHintGitHTTPS),
			i18n.T(msgid.SourceHintGitCI),
		)
	return e.WithCause(err)
}

// tagMissing 说清楚"仓库在，但没有这个版本的 tag"，列出仓库里有的版本。
func (s *gitSource) tagMissing(ctx context.Context, repo *gitRepo, componentID, version, repoURL string) error {
	_, subpath := s.locate(componentID)
	versions, _ := s.versions(ctx, repo, componentID, subpath)
	available := strings.Join(versions, ", ")
	if available == "" {
		available = i18n.T(msgid.SourceGitNoVersionTags)
	}
	return clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.SourceGitTagMissing, componentID+"@"+version)).
		WithDetail(i18n.T(msgid.LabelRepo), repoURL).
		WithDetail(i18n.T(msgid.SourceLabelGitTag), tagPrefix(componentID, subpath)+version).
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
