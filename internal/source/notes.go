package source

// 本文件是发版说明：作者在 brickkit release --notes / publish --notes 写下的那段 Markdown。
// upgrade 用它告诉使用者，从现在的版本到目标版本之间每个版本改了什么。
//
// 说明从提供目标版本的那个安装源读：git 源读带注释的 tag 里的说明，市场读版本的 changelog。
// 本地源没有版本历史——目录里只有正在改的那一份——所以没有说明。

import (
	"context"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/manifest"
)

// VersionNotes 是一个版本与它的发版说明。
type VersionNotes struct {
	Version string
	Notes   string
}

// noter 是能给出发版说明的安装源：版本 → 说明，只含写了说明的版本。
type noter interface {
	releaseNotes(ctx context.Context, componentID string) (map[string]string, error)
}

// ReleaseNotes 返回组件在 (from, to] 之间每个写了说明的版本的说明，从低到高。
//
// 说明来自提供 to 这个版本的安装源（与 Manifest 同一个优先顺序）；那个源给不了说明
// （本地源），或者谁都不提供 to，结果为空、不算错误。读说明本身失败（git 出错、市场连不上）是错误，
// 由调用方决定怎么说——说明只是给人看的，从不挡住升级。
func (c *Client) ReleaseNotes(ctx context.Context, id, from, to string) ([]VersionNotes, error) {
	for _, f := range c.fetchersFor(id) {
		raw, err := f.manifestBytes(ctx, id, to)
		if err != nil || !manifestMatches(raw, id, to) {
			continue
		}
		n, ok := f.(noter)
		if !ok {
			return nil, nil
		}
		all, err := n.releaseNotes(ctx, id)
		if err != nil {
			return nil, err
		}
		var out []VersionNotes
		for v, text := range all {
			if manifest.CompareVersions(v, from) > 0 && manifest.CompareVersions(v, to) <= 0 {
				out = append(out, VersionNotes{Version: v, Notes: text})
			}
		}
		sort.Slice(out, func(i, j int) bool { return manifest.CompareVersions(out[i].Version, out[j].Version) < 0 })
		return out, nil
	}
	return nil, nil
}

// releaseNotes 读这个组件每个版本 tag 上的说明：只有带注释的 tag 有说明（轻量 tag 的
// %(contents) 是它指向的提交的说明，不是发版说明）。
func (s *gitSource) releaseNotes(ctx context.Context, componentID string) (map[string]string, error) {
	repoURL, subpath := s.locate(componentID)
	repo := s.cache.get(repoURL)
	if err := repo.ensure(ctx); err != nil {
		return nil, s.failed(componentID, repoURL, err)
	}
	prefix := tagPrefix(componentID, subpath)
	// 一条记录：tag 名 \x00 对象类型 \x00 说明 \x01——说明里可以有任何换行
	out, err := runGit(ctx, repo.dir, "for-each-ref",
		"--format=%(refname:strip=2)%00%(objecttype)%00%(contents)%01", "refs/tags/"+prefix)
	if err != nil {
		return nil, s.failed(componentID, repoURL, err)
	}
	notes := map[string]string{}
	for _, record := range strings.Split(string(out), "\x01") {
		fields := strings.SplitN(strings.TrimLeft(record, "\n"), "\x00", 3)
		if len(fields) != 3 || fields[1] != "tag" {
			continue
		}
		v, ok := strings.CutPrefix(fields[0], prefix)
		text := strings.TrimRight(fields[2], "\n")
		if ok && manifest.IsExactVersion(v) && strings.TrimSpace(text) != "" {
			notes[v] = text
		}
	}
	return notes, nil
}

// releaseNotes 读市场里每个能装的版本的 changelog（publish --notes 写进去的）。
func (s *marketSource) releaseNotes(ctx context.Context, componentID string) (map[string]string, error) {
	body, err := s.get(ctx, "/components/"+componentID+"/versions", nil)
	if err != nil {
		return nil, err
	}
	list, err := decodeVersionList(body, s.sourceID)
	if err != nil {
		return nil, err
	}
	notes := map[string]string{}
	for _, v := range list {
		if v.installable() && strings.TrimSpace(v.Changelog) != "" {
			notes[v.Version] = strings.TrimRight(v.Changelog, "\n")
		}
	}
	return notes, nil
}
