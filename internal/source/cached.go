package source

// 本文件回答"本机已经知道这个组件有哪些版本"——给 TAB 补全用，所以**从不联网**：
// 只读项目的清单缓存、本地安装源的目录和已经存在的 git 仓库缓存；缓存里没有的仓库
// 不克隆，有的也不 fetch。答得不全是可以的，补全只是提示；卡住或偷偷下载不可以。

import (
	"context"
	"os"
	"sort"

	"github.com/brickkit/brickkit/internal/manifest"
)

// offlineVersioner 是能不联网说出已知版本的安装源。
type offlineVersioner interface {
	cachedVersions(ctx context.Context, componentID string) []string
}

// CachedVersions 是本机不联网就知道的版本，从低到高排好；一个都不知道时为空。
// 不是合法组件 ID 的输入（补全时什么都可能敲进来）直接不答：它拼不出缓存里的目录。
func (c *Client) CachedVersions(componentID string) []string {
	if manifest.ComponentIDProblem(componentID) != "" {
		return nil
	}
	ctx := context.Background()
	seen := map[string]bool{}
	entries, _ := os.ReadDir(c.layout.CachedManifestDir(componentID, ""))
	for _, e := range entries {
		if e.IsDir() && manifest.IsExactVersion(e.Name()) {
			seen[e.Name()] = true
		}
	}
	for _, f := range c.fetchersFor(componentID) {
		if o, ok := f.(offlineVersioner); ok {
			for _, v := range o.cachedVersions(ctx, componentID) {
				seen[v] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return manifest.CompareVersions(out[i], out[j]) < 0 })
	return out
}

// 本地源：目录里那一个版本（component.yaml 读不了就不答）。
func (s *localSource) cachedVersions(ctx context.Context, componentID string) []string {
	if v, err := singleVersionLatest(ctx, s, componentID); err == nil {
		return []string{v}
	}
	return nil
}

// git 源：仓库缓存里已经有的 tag。缓存目录不存在就不答——绝不为了补全去克隆。
func (s *gitSource) cachedVersions(ctx context.Context, componentID string) []string {
	if s.cache == nil || s.cache.root == "" {
		return nil
	}
	repoURL, subpath := s.locate(componentID)
	repo := s.cache.get(repoURL)
	if !isDir(repo.dir) {
		return nil
	}
	versions, _ := s.versions(ctx, repo, componentID, subpath)
	return versions
}
