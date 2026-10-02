package cli

// 本文件实现 brickkit lint：离线的结构校验。
//
// 绝大部分规则都不是新的——只是把散在 up / add / publish 里、早就存在的结构检查，
// 收拢到一个不联网、不需要引擎、不写任何文件的入口。今天没有任何一个命令能对着
// 这两种东西单独跑一遍校验：
//   - 独立的组件仓库（只有 component.yaml、没有 brickkit.yaml）；
//   - 已经 add --local 过的本地组件——add --local 对已在配置里的同版本组件是静默跳过，
//     编辑之后引入的拼写错误，要到跑 up（或 up --dry-run）读到那份文件时才会暴露。
// 跨文件的检查走 up 同一条装载路径（lintCrossFile）：up 会拦的，lint 一样拦。
//
// 文档检查（doccheck）在组件仓库、工作台与项目里都跑，只报警告。
//
// 不做的事：不解析依赖图、不核对外壳这次承载的成员版本与它编进的版本（那要解析出依赖图，
// 可能联网，留给 up / graph；盘上有 Manifest 的组件，kind: shell 与 shell 块、成员确实编进外壳
// 照样查，见 lint_config.go）、不校验 configSchema 里 enum / minimum 对应的值
// （configSchema 是说明书，不是安全闸，见 docs/{en,zh}/11-reference/04-config-schema-spec.md）。

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/doccheck"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/skills"
	"github.com/brickkit/brickkit/internal/source"
)

// newLintCommand 实现 brickkit lint。
func newLintCommand(opts *Options) *cobra.Command {
	var strict, all bool

	cmd := &cobra.Command{
		Annotations:       findsProjectAnnotation(),
		Use:               "lint [<id>]",
		Short:             i18n.T(msgid.CliLintShort),
		GroupID:           groupProject,
		Long:              i18n.T(msgid.CliLintLong),
		Example:           i18n.T(msgid.CliLintExample),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeProjectIDs(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && len(args) == 1 {
				return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliLintIDAndAll)).
					WithHint(i18n.T(msgid.CliLintHintIDOrAll)).WithExit(clierr.ExitUsage)
			}
			// 与 build、deps 一样：在组件目录里不带参数，说的就是这个组件；--all 才是整个项目
			only := ""
			if len(args) == 1 {
				only = args[0]
			} else if !all {
				only, _ = componentHere(opts)
			}
			return runLint(opts, strict, only, len(args) == 1)
		},
	}
	addDeployFileFlags(cmd, opts)

	cmd.Flags().BoolVar(&strict, "strict", false, i18n.T(msgid.CliLintWarningsCountAsFailuresToo))
	cmd.Flags().BoolVar(&all, "all", false, i18n.T(msgid.CliLintAllFlag))
	return cmd
}

// lintFile 是对一个文件的检查结论。
type lintFile struct {
	// path 是给人看的路径（相对工作目录）。
	path     string
	errors   []*clierr.Error
	warnings []*clierr.Error
}

// runLint 检查一个项目、一个组件仓库，或项目里的一个组件（only 非空时）。named 说 only 是不是使用者写在命令行上的：
// 写了的，在组件仓库里没有项目可言要报错；从所在目录推断的不会出现在组件仓库里。
func runLint(opts *Options, strict bool, only string, named bool) error {
	scope, layout, err := detectScope(opts)
	if err != nil {
		return err
	}
	if named && scope == skills.ScopeComponent {
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliLintIDNeedsProject, project.FileDecl)).
			WithHint(i18n.T(msgid.CliLintHintIDNeedsProject)).WithExit(clierr.ExitUsage)
	}

	var files []lintFile
	var notes []string
	if only != "" && scope != skills.ScopeComponent {
		files, notes, err = lintOneComponent(opts, layout, only, strict)
		if err != nil {
			return err
		}
		return reportLint(opts, files, notes, strict)
	}
	if scope == skills.ScopeComponent {
		opts.Printf("%s\n", i18n.T(msgid.CliLintComponentRepositoryHasNoOnly, manifest.FileName, project.FileDecl, manifest.FileName))
		files = append(files, lintManifest(opts, filepath.Join(layout.Root, manifest.FileName), ""))
		if f, ok := componentDocs(opts, layout.Root); ok {
			files = append(files, f)
		}
	} else {
		files, notes = lintProject(opts, layout, strict)
	}

	return reportLint(opts, files, notes, strict)
}

// localSkippedNote 说明"本地组件的 component.yaml 没能检查"，与 brickkit.yaml 没通过时的那条对称。
// 原因已经由前面那条错误块说了，这里只交代后果：汇总里的文件数因此比实际少。
//
// 是函数而不是常量：文案要跟着语言变，包初始化时语言还没确定。
func localSkippedNote() string {
	return i18n.T(msgid.CliLintLocalEnumerationFailed, manifest.FileName)
}

// lintProject 逐个检查三层文件，再做一遍跨文件校验，最后检查本地安装源里的每一份 component.yaml。
//
// 先逐个文件查、再跨文件查：单个文件的错误要落在它自己的文件名下，人才知道去改哪一份；
// 跨文件的问题（部署文件漏了组件、$var: 没定义、config 文件对不上组件）只有各文件都
// 能读时才问得出来，单独列一行。
func lintProject(opts *Options, layout project.Layout, strict bool) ([]lintFile, []string) {
	head := lintFile{path: opts.display(layout.DeclPath())}
	decl, err := projfile.ParseFile(layout.DeclPath())
	if err != nil {
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, []string{
			i18n.T(msgid.CliLintBrickkitYAMLDidNotPass, manifest.FileName),
		}
	}

	files := []lintFile{head}
	parsed := true
	for _, d := range lintDeployFiles(opts, layout) {
		f := lintFile{path: opts.display(d.path)}
		_, warnings, err := deployfile.ParseFile(d.path, d.role)
		if err != nil {
			f.errors = append(f.errors, clierr.As(err))
			parsed = false
		}
		f.warnings = append(f.warnings, warnings...)
		files = append(files, f)
	}
	var notes []string
	if parsed {
		cross, crossNotes := lintCrossFile(opts, strict)
		files = append(files, cross...)
		notes = append(notes, crossNotes...)
	}

	// 项目自己的 AGENTS.md / CLAUDE.md：与本地源无关，枚举不了本地源也照样查。
	// 工作台（根目录有 component.yaml）的这两份是组件的文档，在下面按组件查。
	if _, err := os.Stat(filepath.Join(layout.Root, manifest.FileName)); err != nil {
		files = append(files, lintDocs(opts, layout.Root, doccheck.Project(layout.Root)))
	}

	// 直接 source.New，不走 newSourceClient：后者会先去读 installer.publicKeys 指向的公钥文件，
	// 而公钥缺失是 up / add 该报的事，不该让一条"离线校验 YAML"的命令因此失败。
	// source.New 本身不联网——三种安装源都是惰性的，只有真去取 Manifest 才会碰网络，lint 从不取。
	//
	// 这是整个 CLI 里唯一不经 newSourceClient（也就是不带签名策略）的取源客户端。这个例外
	// **只安全在** lint 只调 LocalManifestFiles、从不取 Manifest：将来谁在这里加一次
	// client.Manifest，就等于悄悄绕过验签，还会写 .brickkit/manifests/ 缓存、可能联网，
	// "纯只读、不联网"的承诺当场破功。
	client, err := source.New(layout, decl, source.Options{})
	if err != nil {
		files[0].errors = append(files[0].errors, clierr.As(err))
		return files, append(notes, localSkippedNote())
	}
	defer func() { _ = client.Close() }()

	found, err := client.LocalManifestFiles()
	if err != nil {
		files[0].errors = append(files[0].errors, clierr.As(err))
		return files, append(notes, localSkippedNote())
	}
	own := filepath.Join(layout.Root, manifest.FileName)
	ownListed := false
	var docs []lintFile
	for _, f := range found {
		files = append(files, lintManifest(opts, f.Path, f.ID))
		if same, _ := sameFile(f.Path, own); same {
			ownListed = true
		}
		// 本地源里的组件是这个项目里正在开发的：它们的文档也查
		if d, ok := componentDocs(opts, filepath.Dir(f.Path)); ok {
			docs = append(docs, d)
		}
	}
	// 组件仓库兼作工作台：它要发布的那份 component.yaml 不在任何本地源里，照样要查
	if _, err := os.Stat(own); err == nil && !ownListed {
		files = append(files, lintManifest(opts, own, ""))
		if d, ok := componentDocs(opts, layout.Root); ok {
			docs = append(docs, d)
		}
	}
	return append(files, docs...), notes
}

// lintOneComponent 只查项目里的一个组件：它的 component.yaml 与文档（本地源里有它时），以及它在项目里的配置。
//
// 别的组件的问题不报：在一个组件目录里改到一半，项目里另一个组件没改好，不该让这里的检查也是红的。
// 项目本身装载不了（三层对不上、升级留下的重复键）时照样报——那样 up 也起不来这个组件。
func lintOneComponent(opts *Options, layout project.Layout, id string, strict bool) ([]lintFile, []string, error) {
	head := lintFile{path: opts.display(layout.DeclPath())}
	decl, err := projfile.ParseFile(layout.DeclPath())
	if err != nil {
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, []string{i18n.T(msgid.CliLintBrickkitYAMLDidNotPass, manifest.FileName)}, nil
	}

	var files []lintFile
	var notes []string
	// 与 lintProject 一样直接 source.New：只列本地源里的文件，从不取 Manifest（见那里的说明）
	var local []string // 这个组件在本地源里的 component.yaml
	client, err := source.New(layout, decl, source.Options{})
	if err == nil {
		defer func() { _ = client.Close() }()
		found, ferr := client.LocalManifestFiles()
		if ferr != nil {
			err = ferr
		}
		for _, f := range found {
			if f.ID == id {
				local = append(local, f.Path)
			}
		}
	}
	if err != nil {
		head.errors = append(head.errors, clierr.As(err))
		files = append(files, head)
	}
	// 工作台：项目根自己就是这个组件
	own := filepath.Join(layout.Root, manifest.FileName)
	if m, perr := manifest.ParseFile(own); len(local) == 0 && perr == nil && m.Metadata.ID == id {
		local = append(local, own)
	}
	added := slices.Contains(decl.IDs(), id)
	if !added && len(local) == 0 {
		return nil, nil, withDidYouMean(clierr.New(clierr.CodeComponentNotFound, i18n.T(msgid.CliLintNotInProject, id)).
			WithHint(i18n.T(msgid.CliDepsHintAdd, id), i18n.T(msgid.CliLintHintAll)), id, decl.IDs())
	}
	opts.Printf("%s\n", i18n.T(msgid.CliLintOnlyComponent, id))
	for _, path := range local {
		dirID := id
		if path == own {
			dirID = "" // 工作台的目录名不是 <scope>/<name>
		}
		files = append(files, lintManifest(opts, path, dirID))
		if d, ok := componentDocs(opts, filepath.Dir(path)); ok {
			files = append(files, d)
		}
	}
	if len(local) == 0 {
		notes = append(notes, i18n.T(msgid.CliLintOnlyNotLocal, id, manifest.FileName))
	}
	// 本地源里有、还没 add 的组件：没有配置可查（整个项目的 lint 也是这样查它的）
	if !added {
		return files, append(notes, i18n.T(msgid.CliLintOnlyNotAdded, id)), nil
	}

	cf := lintFile{path: i18n.T(msgid.CliLintComponentConfig, id, project.DirConfig+"/")}
	proj, err := project.Load(opts.WorkDir, opts.loadOptions())
	if err != nil {
		cf.errors = append(cf.errors, clierr.As(err))
		return append(files, cf), notes, nil
	}
	cfg := lintConfig(proj, strict, id)
	cf.errors = append(cf.errors, cfg.errors...)
	cf.warnings = append(cf.warnings, cfg.warnings...)
	if len(cfg.unchecked) > 0 {
		notes = append(notes, i18n.T(msgid.CliLintConfigUnchecked, strings.Join(cfg.unchecked, i18n.T(msgid.ListSeparator))))
	}
	for _, u := range cfg.unreadable {
		notes = append(notes, i18n.T(msgid.CliLintManifestUnreadable, u[0], strings.TrimPrefix(strings.TrimSpace(u[1]), "❌ ")))
	}
	return append(files, cf), notes, nil
}

func sameFile(a, b string) (bool, error) {
	ia, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ia, ib), nil
}

// deployToLint 是一份要单独检查的部署文件。
type deployToLint struct {
	path string
	role deployfile.Role
}

// lintDeployFiles 列出要检查的部署文件：-f 给了就只查那一份；否则查 deploy.yaml，
// 以及存在时的 deploy.local.yaml（不管本地模式开没开——它迟早会被用上）。
func lintDeployFiles(opts *Options, layout project.Layout) []deployToLint {
	if opts.DeployFile != "" {
		return []deployToLint{{layout.Resolve(opts.DeployFile), deployfile.RoleTeam}}
	}
	out := []deployToLint{{layout.DeployPath(), deployfile.RoleTeam}}
	if _, err := os.Stat(layout.DeployLocalPath()); err == nil {
		out = append(out, deployToLint{layout.DeployLocalPath(), deployfile.RoleLocal})
	}
	return out
}

// lintCrossFile 用 up 同一条装载路径做跨文件校验：up 会拦的，lint 一样拦；再对装载好的项目做
// 配置检查（lint_config.go）。
//
// 团队的 deploy.yaml 与个人的 deploy.local.yaml 都要与 brickkit.yaml 一致：
// 这次装载用的是其中一份，另一份（存在的话）再单独装载一遍——本地模式关着时的
// deploy.local.yaml 迟早会被用上，开着时团队文件也不能因此没人查。-f 指定了文件时
// 两份都不看（-f 完全忽略本地文件）。
func lintCrossFile(opts *Options, strict bool) ([]lintFile, []string) {
	proj, err := project.Load(opts.WorkDir, opts.loadOptions())
	f := lintFile{path: i18n.T(msgid.CliLintCrossFile, project.FileDecl, lintedDeployName(opts, proj), project.DirConfig+"/")}
	if err != nil {
		f.errors = append(f.errors, clierr.As(err))
		return []lintFile{f}, nil
	}
	renderFocus(opts, proj)
	if err := checkNestedCopies(opts, proj); err != nil {
		f.errors = append(f.errors, clierr.As(err))
	}
	// 焦点组件要从源码跑：up 设焦点时就会拦下没有本地源码的焦点，lint 对手写的也提前说
	if err := focusSourceError(opts, proj); err != nil {
		f.errors = append(f.errors, clierr.As(err))
	}
	f.warnings = append(f.warnings, proj.Warnings...)
	cfg := lintConfig(proj, strict, "")
	f.errors = append(f.errors, cfg.errors...)
	f.warnings = append(f.warnings, cfg.warnings...)
	var notes []string
	if len(cfg.unchecked) > 0 {
		notes = append(notes, i18n.T(msgid.CliLintConfigUnchecked, strings.Join(cfg.unchecked, i18n.T(msgid.ListSeparator))))
	}
	for _, u := range cfg.unreadable {
		notes = append(notes, i18n.T(msgid.CliLintManifestUnreadable, u[0], strings.TrimPrefix(strings.TrimSpace(u[1]), "❌ ")))
	}
	files := []lintFile{f}
	if opts.DeployFile != "" {
		return files, notes
	}

	other := func(name string, load project.LoadOptions) {
		o := lintFile{path: i18n.T(msgid.CliLintCrossFile, project.FileDecl, name, project.DirConfig+"/")}
		if _, err := project.Load(opts.WorkDir, load); err != nil {
			o.errors = append(o.errors, clierr.As(err))
		}
		files = append(files, o)
	}
	if proj.DeploySource == project.DeployLocal {
		other(project.FileDeploy, project.LoadOptions{NoLocal: true})
	} else if _, err := os.Stat(proj.Layout.DeployLocalPath()); err == nil {
		other(project.FileDeployLocal, project.LoadOptions{ForceLocal: true})
	}
	return files, notes
}

// lintedDeployName 是跨文件校验实际用的那份部署文件名（装载失败时按选择规则推断）。
func lintedDeployName(opts *Options, proj *project.Project) string {
	switch {
	case proj != nil:
		return opts.display(proj.DeployPath)
	case opts.DeployFile != "":
		return opts.DeployFile
	default:
		return project.FileDeploy
	}
}

// lintManifest 检查一份 component.yaml。dirID 非空时（项目模式）还要核对目录名与 metadata.id 一致。
func lintManifest(opts *Options, path, dirID string) lintFile {
	f := lintFile{path: opts.display(path)}

	raw, err := os.ReadFile(path)
	if err != nil {
		f.errors = append(f.errors, clierr.New(clierr.CodeManifestInvalid,
			i18n.T(msgid.ManifestReadFailed, manifest.FileName)).
			WithDetail(i18n.T(msgid.LabelPath), f.path).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(i18n.T(msgid.ProblemHintCheckPermissions)))
		return f
	}

	m, err := manifest.Parse(raw, f.path)
	if err != nil {
		f.errors = append(f.errors, clierr.As(err))
	} else {
		if dirID != "" && m.Metadata.ID != dirID {
			f.errors = append(f.errors, clierr.New(clierr.CodeManifestInvalid,
				i18n.T(msgid.CliLintErrorTheComponentIDIn, manifest.FileName)).
				WithDetail(i18n.T(msgid.LabelFile), f.path).
				WithDetail(i18n.T(msgid.CliLintDirectoryName), dirID).
				WithDetail("metadata.id", m.Metadata.ID).
				WithHint(i18n.T(msgid.CliLintALocalInstallSourceFinds, manifest.FileName)))
		}
		// 是 up 那条警告的超集：up 只在配置项有默认值、或被 config 覆盖时才走到保留变量检查，
		// 这里把 configSchema 里声明的每一项都查一遍——与市场发布时同一个范围，
		// 作者在发布之前就该知道
		for _, w := range inject.ReservedKeyWarnings(m) {
			// 它的块里本来只有组件 ID；检查一堆文件时得告诉人是哪一份
			f.warnings = append(f.warnings, w.WithDetail(i18n.T(msgid.ManifestLabelOrigin), f.path))
		}
	}
	// 与 Parse 成败无关：它只依赖 YAML 本身，语法错误时自己返回 nil
	f.warnings = append(f.warnings, manifest.PropertyKeyWarnings(raw, f.path)...)
	return f
}

// reportLint 把结论打印出来，并决定这条命令是成功还是失败。
//
// 报告写 stdout：它是这条命令的产出，不是"命令失败了"的错误。失败时另外返回一个
// 汇总错误，走 stderr 与 JSON 日志行的老路径。
func reportLint(opts *Options, files []lintFile, notes []string, strict bool) error {
	failed, warned := 0, 0
	for _, f := range files {
		logFindings(opts, f)
		if len(f.errors) == 0 && len(f.warnings) == 0 {
			opts.Printf("✅ %s\n", f.path)
			continue
		}
		for _, e := range f.errors {
			opts.Printf("%s", opts.renderCoded(e))
		}
		for _, w := range f.warnings {
			opts.Printf("%s", opts.renderCoded(w))
		}
		if len(f.errors) > 0 {
			failed++
		}
		warned += len(f.warnings)
	}

	for _, n := range notes {
		opts.Printf("ℹ️ %s\n", n)
	}
	opts.Printf("\n%s\n", i18n.T(msgid.CliLintCheckedFilesWithErrorsWarnings, i18n.Count(msgid.CountFiles, len(files)), failed, i18n.Count(msgid.CountWarnings, warned)))

	if failed == 0 && (!strict || warned == 0) {
		return nil
	}
	e := clierr.New(clierr.CodeLintFailed, i18n.T(msgid.CliLintErrorTheStructureCheckDid)).
		WithDetail(i18n.T(msgid.CliLintChecked), i18n.Count(msgid.CountFiles, len(files)))
	if failed > 0 {
		e = e.WithDetail(i18n.T(msgid.CliLintWithErrors), i18n.Count(msgid.CountFiles, failed))
	}
	if strict && warned > 0 {
		e = e.WithDetail(i18n.T(msgid.CliLintWarnings), i18n.T(msgid.CliLintStrictWarningsCountAsFailures, warned))
	}
	return e.WithHint(i18n.T(msgid.CliLintFixThemAtTheLocations))
}

// lintDocs 是一组文档的检查结果，在报告里占一行：dir 下的文档（路径按使用者所在的目录写）。
func lintDocs(opts *Options, dir string, warnings []*clierr.Error) lintFile {
	label := i18n.T(msgid.LabelFile)
	for _, w := range warnings {
		for i, d := range w.Details {
			if d.Key == label {
				w.Details[i].Value = opts.display(filepath.Join(dir, filepath.FromSlash(d.Value)))
			}
		}
	}
	return lintFile{path: i18n.T(msgid.CliLintDocsEntry, opts.display(dir)+string(filepath.Separator)), warnings: warnings}
}

// componentDocs 查 dir 里那个组件的文档。component.yaml 校验不过时照样查，只是不核对清单里的事实
// （它自己的错误已经报过）：两类问题一次说全，作者不必修完一类再跑一遍才看到另一类。
func componentDocs(opts *Options, dir string) (lintFile, bool) {
	path := filepath.Join(dir, manifest.FileName)
	if _, err := os.Stat(path); err != nil {
		return lintFile{}, false
	}
	m, err := manifest.ParseFile(path)
	if err != nil {
		m = nil
	}
	return lintDocs(opts, dir, doccheck.Component(dir, m)), true
}

// logFindings 把每条查出的问题记一行 info 级的日志，带上 error_code 与文件：要结构化数据的脚本用它
// （报告里每一条的标题行也带着码）。默认的 warn 级别下不出现——一个项目里几十条文档警告，不该每条都多一行 JSON。
func logFindings(opts *Options, f lintFile) {
	label := i18n.T(msgid.LabelFile)
	for _, e := range append(append([]*clierr.Error(nil), f.errors...), f.warnings...) {
		file := f.path
		for _, d := range e.Details {
			if d.Key == label {
				file = d.Value
			}
		}
		logging.Info(i18n.T(msgid.LogLintFinding), "error_code", string(e.Code), "warning", e.Warning, "file", file)
	}
}
