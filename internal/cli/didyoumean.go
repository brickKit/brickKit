package cli

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/suggest"
)

// withDidYouMean 在"找不到这个组件"的报错上加一句"你是不是想写 …"（有相近的候选时）。
// 错误码与标题都不变；命令从不拿猜出来的名字去执行。
func withDidYouMean(err error, typed string, candidates []string) error {
	if err == nil {
		return nil
	}
	hint, ok := suggest.Hint(typed, candidates)
	if !ok {
		return err
	}
	return clierr.As(err).WithHint(hint)
}

// knownComponentIDs 是 add 的候选：项目里已有的、本地安装源里的、本机清单缓存里的组件（都不联网）。
func knownComponentIDs(decl *projfile.File, layout project.Layout, client *source.Client) []string {
	seen := map[string]bool{}
	for _, id := range decl.IDs() {
		seen[id] = true
	}
	if files, err := client.LocalManifestFiles(); err == nil {
		for _, f := range files {
			seen[f.ID] = true
		}
	}
	for _, id := range cachedManifestIDs(layout) {
		seen[id] = true
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// cachedManifestIDs 是 .brickkit/manifests/<scope>/<name>/ 下已经缓存过清单的组件。
func cachedManifestIDs(l project.Layout) []string {
	var out []string
	scopes, _ := os.ReadDir(l.ManifestsDir())
	for _, scope := range scopes {
		if !scope.IsDir() {
			continue
		}
		names, _ := os.ReadDir(filepath.Join(l.ManifestsDir(), scope.Name()))
		for _, name := range names {
			if name.IsDir() {
				out = append(out, scope.Name()+"/"+name.Name())
			}
		}
	}
	return out
}
