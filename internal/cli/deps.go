package cli

// 本文件实现 brickkit deps：把依赖关系打印成树。
//
// brickkit.yaml 只锁版本、不写依赖——依赖是组件的内在属性，写在各自的 component.yaml 里；
// 两处都写，对不上时就不知道听谁的。要看依赖关系，就用这条命令从 Manifest 里读出来。
//
// 与 graph 读的是同一张解析好的依赖图（同一个安装源客户端，没缓存的 Manifest 要联网），
// 只是画法不同：graph 给要提交、要渲染的 Mermaid，deps 给终端里一眼能读的树。

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

func newDepsCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Annotations:       findsProjectAnnotation(),
		Use:               "deps [<id>[@<version>]]",
		Short:             i18n.T(msgid.CliDepsShort),
		GroupID:           groupProject,
		Long:              i18n.T(msgid.CliDepsLong),
		Example:           i18n.T(msgid.CliDepsExample),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeProjectComponents(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			} else if id, ok := componentHere(opts); ok {
				target = id
			}
			return runDeps(cmd.Context(), opts, target)
		},
	}
}

func runDeps(ctx context.Context, opts *Options, target string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// 依赖关系与部署方式无关：不读本地模式（与 graph 一致）
	load := opts.loadOptions()
	load.NoLocal = true
	proj, err := project.Load(opts.WorkDir, load)
	if err != nil {
		return err
	}
	for _, w := range proj.Warnings {
		_, _ = fmt.Fprint(opts.Stderr, opts.render(w))
	}

	var id, version string
	if target != "" {
		if id, version, err = parseComponentRef(target); err != nil {
			return err
		}
	}
	if len(proj.Decl.Components) == 0 {
		if id != "" {
			return depsNotInProject(target, id, proj.Decl.IDs())
		}
		opts.Printf("%s\n", i18n.T(msgid.CliDepsNoComponents))
		return nil
	}

	client, err := newSourceClient(opts, proj.Layout, proj.Decl, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	graph, err := resolver.New(resolver.FromSource(client)).ResolveProject(ctx, proj)
	if err != nil {
		return err
	}
	for _, w := range graph.Warnings {
		_, _ = fmt.Fprint(opts.Stderr, opts.render(w))
	}

	t := &depsTree{graph: graph, printed: map[resolver.Ref]bool{}}
	if id == "" {
		opts.Printf("%s", t.project(proj))
		return nil
	}

	var refs []resolver.Ref
	for _, v := range graph.Versions(id) {
		if version == "" || v == version {
			refs = append(refs, resolver.Ref{ID: id, Version: v})
		}
	}
	if len(refs) == 0 {
		return depsNotInProject(target, id, proj.Decl.IDs())
	}
	var blocks []string
	for _, ref := range refs {
		t.printed = map[resolver.Ref]bool{}
		blocks = append(blocks, t.render(ref)+"\n"+requiredByLine(graph.Node(ref))+"\n")
	}
	opts.Printf("%s", strings.Join(blocks, "\n"))
	return nil
}

func depsNotInProject(target, id string, known []string) error {
	return withDidYouMean(clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliDepsNotInProject, target)).
		WithHint(i18n.T(msgid.CliDepsHintAdd, id), i18n.T(msgid.CliDepsHintList)), id, known)
}

// requiredByLine 列出直接依赖这个组件版本的组件（强弱依赖都算），另起一行列出配置用 $endpoint: 引用它的组件。
func requiredByLine(node *resolver.Node) string {
	line := i18n.T(msgid.CliDepsRequiredByNone)
	if len(node.ReferencedBy) > 0 {
		line = i18n.T(msgid.CliDepsRequiredByNothing) // 只被引用：不是顶层
	}
	if len(node.Dependents) > 0 {
		line = i18n.T(msgid.CliDepsRequiredBy, sortedRefs(node.Dependents))
	}
	if len(node.ReferencedBy) > 0 {
		line += "\n" + i18n.T(msgid.CliDepsReferencedBy, sortedRefs(node.ReferencedBy))
	}
	return line
}

func sortedRefs(refs []resolver.Ref) string {
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		names = append(names, r.String())
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// depsTree 把依赖图画成树。一个组件版本在一次输出里只展开一次，再出现时标"见上"：
// 菱形依赖的共享底层（数据库、鉴权）会被很多组件依赖，每次都展开，树会按层数成倍膨胀。
type depsTree struct {
	graph   *resolver.Graph
	printed map[resolver.Ref]bool
}

// project 画全项目：每个顶层组件（项目里没有谁依赖它、也没有谁的配置引用它——与 cascade 的"顶层"同一个判据）
// 一棵树，按 brickkit.yaml 的顺序。
// 没被任何一棵树画到的（只在弱依赖环里互相依赖）也各自成树，一个都不漏。
func (t *depsTree) project(proj *project.Project) string {
	var order []resolver.Ref
	for _, c := range proj.Decl.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		if t.graph.Has(ref) {
			order = append(order, ref)
		}
	}
	var blocks []string
	for _, ref := range order {
		if len(t.graph.Node(ref).Users()) == 0 {
			blocks = append(blocks, t.render(ref))
		}
	}
	for _, ref := range order {
		if !t.printed[ref] {
			blocks = append(blocks, t.render(ref))
		}
	}
	return strings.Join(blocks, "\n")
}

// render 画以 ref 为根的一棵树。
func (t *depsTree) render(ref resolver.Ref) string {
	var b strings.Builder
	b.WriteString(ref.String() + "\n")
	t.printed[ref] = true
	t.children(&b, ref, "", map[resolver.Ref]bool{ref: true})
	return b.String()
}

type depsChild struct {
	ref                           resolver.Ref
	optional, missing, referenced bool
}

func (t *depsTree) children(b *strings.Builder, ref resolver.Ref, prefix string, path map[resolver.Ref]bool) {
	node := t.graph.Node(ref)
	var kids []depsChild
	for _, r := range node.Requires {
		kids = append(kids, depsChild{ref: r})
	}
	for _, r := range node.Optional {
		kids = append(kids, depsChild{ref: r, optional: true})
	}
	for _, r := range node.MissingOptional {
		kids = append(kids, depsChild{ref: r, optional: true, missing: true})
	}
	for _, r := range node.References {
		kids = append(kids, depsChild{ref: r, referenced: true})
	}
	for i, k := range kids {
		branch, next := "├── ", prefix+"│   "
		if i == len(kids)-1 {
			branch, next = "└── ", prefix+"    "
		}
		line := k.ref.String()
		var notes []string
		switch {
		case k.missing:
			notes = append(notes, i18n.T(msgid.CliDepsOptionalMissing))
		case k.optional:
			notes = append(notes, i18n.T(msgid.CliDepsOptional))
		case k.referenced:
			notes = append(notes, i18n.T(msgid.CliDepsReferenced))
		}
		expand := !k.missing
		switch {
		case k.missing:
		case path[k.ref]:
			notes = append(notes, i18n.T(msgid.CliDepsCycle))
			expand = false
		case t.printed[k.ref]:
			notes = append(notes, i18n.T(msgid.CliDepsShownAbove))
			expand = false
		}
		if len(notes) > 0 {
			line += i18n.T(msgid.CliDepsNotes, strings.Join(notes, i18n.T(msgid.CliDepsNoteSeparator)))
		}
		b.WriteString(prefix + branch + line + "\n")
		if !expand {
			continue
		}
		t.printed[k.ref] = true
		path[k.ref] = true
		t.children(b, k.ref, next, path)
		delete(path, k.ref)
	}
}
