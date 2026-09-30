package cli

// 本文件是"组件源码只放一处"在命令层的部分（设计 §5）：发现嵌套在别的组件目录里的
// 组件副本就报错，列出每一份在哪、上层有没有同一个 ID、这一份的字节在别处有没有副本。
// 从不替使用者挪动或删除——那可能是有没推送改动的仓库，上层那份也可能不一样。

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/gitrepo"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/workspace"
)

// checkNestedCopies 在项目的本地组件目录里找嵌套的组件副本；有就返回 CONFIG_CONFLICT。
func checkNestedCopies(opts *Options, proj *project.Project) error {
	copies, err := proj.NestedCopies()
	if err != nil || len(copies) == 0 {
		return err
	}
	e := clierr.New(clierr.CodeConfigConflict, i18n.T(msgid.ProjectNestedCopies))
	for _, c := range copies {
		parts := []string{i18n.T(msgid.ProjectNestedCopyDetail, c.ID, c.Inside)}
		if c.TopHasIt {
			parts = append(parts, i18n.T(msgid.ProjectNestedTopHasIt, c.ID))
		}
		// 这一份的字节在别处有没有：没提交、没推送、不是仓库——挪走或删掉之前都得先知道
		if risk := workspace.DeletionRisk(c.Dir); risk != "" {
			parts = append(parts, risk)
		}
		e = e.WithDetail(opts.display(c.Dir), strings.Join(parts, i18n.T(msgid.SemicolonSeparator)))
	}
	return e.WithHint(i18n.T(msgid.ProjectNestedHintOnePlace))
}

// refuseRepoInNestedWorkbench：当前项目是嵌在另一个项目本地源里的工作台时，--repo 会在组件目录里
// 再克隆出一份源码（设计 §5）。拒绝，并指向外面那个项目。
func refuseRepoInNestedWorkbench(opts *Options) error {
	outer, found, err := project.FindRoot(filepath.Dir(opts.WorkDir))
	if err != nil || !found {
		return err
	}
	l := project.NewLayout(outer)
	decl, err := projfile.ParseFile(l.DeclPath())
	if err != nil {
		return nil // 外面那个项目读不了：不是这次命令的事，不因此拦下
	}
	if _, inside := componentAt(l, decl, opts.WorkDir); !inside {
		return nil
	}
	shown := opts.display(outer)
	return clierr.New(clierr.CodeConfigConflict, i18n.T(msgid.CliAddRepoInNestedWorkbench, shown)).
		WithHint(i18n.T(msgid.CliAddHintRepoFromOuter, shown))
}

// emptySubmodules 是 dir 的 .gitmodules 里登记了、而目录是空的（或不存在）的 submodule 路径。
// 使用者自己拉过的 submodule 有内容，不算。
func emptySubmodules(dir string) []string {
	var out []string
	for _, rel := range gitrepo.SubmodulePathsIn(dir) {
		entries, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || len(entries) == 0 {
			out = append(out, rel)
		}
	}
	return out
}
