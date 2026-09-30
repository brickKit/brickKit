package cli

// 本文件是 TAB 补全的候选（设计 §7.1）：组件 ID、@ 之后的版本、部署文件、语言。
//
// 三条规矩：
//   - 从不联网。版本只来自本机已经有的东西（项目的清单缓存、本地安装源、已存在的 git 仓库缓存）。
//   - 从不报错。项目外、项目文件写坏了，都只是"没有候选"——补全里冒出一个报错块只会搅乱命令行。
//   - 不走 enterProject：补全的输出只能是候选，不能有"📁 项目"那一行。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/source"
)

const noFileComp = cobra.ShellCompDirectiveNoFileComp

// completionProject 从当前目录向上找项目并读 brickkit.yaml；任何一步不成就是"没有项目"（不报错）。
func completionProject(opts *Options) (*projfile.File, project.Layout, bool) {
	root, found, err := project.FindRoot(opts.WorkDir)
	if err != nil || !found {
		return nil, project.Layout{}, false
	}
	l := project.NewLayout(root)
	decl, err := projfile.ParseFile(l.DeclPath())
	if err != nil {
		return nil, project.Layout{}, false
	}
	return decl, l, true
}

// refCandidates 说一类 <id>[@<version>] 参数的候选从哪来。
type refCandidates struct {
	// ids 是 @ 之前的候选。
	ids func(decl *projfile.File, l project.Layout, client *source.Client) []string
	// versions 是敲了 <id>@ 之后的候选版本；nil 表示这个参数不带版本。
	versions func(decl *projfile.File, client *source.Client, id string) []string
}

// completeRefs 把一类候选接成 cobra 的补全函数：只补第一个位置参数。
func completeRefs(opts *Options, c refCandidates) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, noFileComp
		}
		decl, l, ok := completionProject(opts)
		if !ok {
			return nil, noFileComp
		}
		// 安装源客户端只是把 brickkit.yaml 里的源摆好，构造时不碰网络；后面也只调不联网的方法
		client, err := newSourceClient(opts, l, decl, source.Options{})
		if err != nil {
			return nil, noFileComp
		}
		defer func() { _ = client.Close() }()

		id, _, hasAt := manifest.SplitRef(toComplete)
		if !hasAt {
			return withPrefix(c.ids(decl, l, client), toComplete), noFileComp
		}
		if c.versions == nil {
			return nil, noFileComp
		}
		var refs []string
		for _, v := range c.versions(decl, client, id) {
			refs = append(refs, id+"@"+v)
		}
		return withPrefix(refs, toComplete), noFileComp
	}
}

// completeProjectComponents：项目里已经声明的组件（remove、deps、build）；@ 之后是项目里声明的版本。
func completeProjectComponents(opts *Options) cobra.CompletionFunc {
	return completeRefs(opts, refCandidates{ids: projectIDs, versions: projectVersions})
}

// completeProjectIDs：只要组件 ID、不带版本的参数（up --focus）。
func completeProjectIDs(opts *Options) cobra.CompletionFunc {
	return completeRefs(opts, refCandidates{ids: projectIDs})
}

// completeUpgradeCandidates：项目里的组件；@ 之后是本机已知的版本（要升到的那个）。
func completeUpgradeCandidates(opts *Options) cobra.CompletionFunc {
	return completeRefs(opts, refCandidates{ids: projectIDs, versions: cachedVersions})
}

// completeAddCandidates：项目里的、本地安装源里的、清单缓存里的组件；@ 之后是本机已知的版本。
func completeAddCandidates(opts *Options) cobra.CompletionFunc {
	return completeRefs(opts, refCandidates{ids: knownComponentIDs, versions: cachedVersions})
}

func projectIDs(decl *projfile.File, _ project.Layout, _ *source.Client) []string {
	return decl.IDs()
}

func projectVersions(decl *projfile.File, _ *source.Client, id string) []string {
	return decl.Versions(id)
}

func cachedVersions(_ *projfile.File, client *source.Client, id string) []string {
	return client.CachedVersions(id)
}

// completeDeployFiles：-f 的值是相对项目根的部署文件，候选就是根目录下的 deploy*.yaml。
func completeDeployFiles(opts *Options) cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		_, l, ok := completionProject(opts)
		if !ok {
			return nil, noFileComp
		}
		entries, err := os.ReadDir(l.Root)
		if err != nil {
			return nil, noFileComp
		}
		var out []string
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() && strings.HasPrefix(name, "deploy") && filepath.Ext(name) == ".yaml" {
				out = append(out, name)
			}
		}
		return withPrefix(out, toComplete), noFileComp
	}
}

// completeLanguages：lang set 与 skills update --lang 的值。
func completeLanguages(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, noFileComp
	}
	return withPrefix(i18n.LangNames(), toComplete), noFileComp
}

// withPrefix 只留下以已敲部分开头的候选，排好序。
func withPrefix(candidates []string, toComplete string) []cobra.Completion {
	var out []cobra.Completion
	for _, c := range candidates {
		if strings.HasPrefix(c, toComplete) {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}
