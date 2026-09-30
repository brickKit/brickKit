package project

// 本文件记下"这个项目上一次运行的是哪些组件版本"：up（含 --dry-run）每次生成部署文件后写一份，
// 版本变更的提示拿它做基线。
//
// 不能拿 Manifest 缓存做基线：缓存是永久的，fetch 过的版本、被拒的 add、连带移除
// 的兼容版本都在里面，"缓存里有、配置里没有"不等于"上次跑的是它"。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/manifest"
)

// FileLastRun 是 .brickkit/ 下的上次运行记录：每行一个 <组件ID>@<版本>。
const FileLastRun = "last-run"

// LastRunPath 是上次运行记录的路径。
func (l Layout) LastRunPath() string { return l.path(DirBrickkit, FileLastRun) }

// WriteLastRun 记下这次运行的组件版本（每行一个，排好序）。写不进不阻断：它只影响下一次的提示。
func WriteLastRun(l Layout, refs []string) {
	sorted := append([]string{}, refs...)
	sort.Strings(sorted)
	if err := os.MkdirAll(filepath.Dir(l.LastRunPath()), 0o755); err == nil {
		_ = os.WriteFile(l.LastRunPath(), []byte(strings.Join(sorted, "\n")+"\n"), 0o644)
	}
}

// ReadLastRun 读上次运行记录：组件 ID → 版本。还没有记录时 ok 为 false。
func ReadLastRun(l Layout) (map[string][]string, bool) {
	data, err := os.ReadFile(l.LastRunPath())
	if err != nil {
		return nil, false
	}
	out := map[string][]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if id, version, ok := manifest.SplitRef(strings.TrimSpace(line)); ok && id != "" {
			out[id] = append(out[id], version)
		}
	}
	return out, true
}
