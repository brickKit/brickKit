package cli

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/gitrepo"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/skills"
	"github.com/brickkit/brickkit/internal/version"
)

// initFlags 是 brickkit init 的参数。
type initFlags struct {
	name      string
	noSkills  bool
	yes       bool
	hooksOnly bool
}

// newInitCommand 实现 brickkit init（提案 §11.5）：带名字是创建式（新建 <name>/ 目录），
// 不带名字是补全式（在当前目录缺什么补什么）。两种模式走同一个补全原语。
func newInitCommand(opts *Options) *cobra.Command {
	var f initFlags
	cmd := &cobra.Command{
		Use:     i18n.T(msgid.CliInitInitProjectName),
		Short:   i18n.T(msgid.CliInitShort),
		GroupID: groupProject,
		Long:    i18n.T(msgid.CliInitLong),
		Example: i18n.T(msgid.CliInitExample),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.hooksOnly {
				if len(args) > 0 {
					return clierr.New(clierr.CodeInvalidArgument,
						i18n.T(msgid.CliInitBrickkitInitHooksOnlyInstalls)).
						WithExit(clierr.ExitUsage).WithHint(i18n.T(msgid.CliInitHintHooksNoName))
				}
				return installCommitHook(opts, project.NewLayout(opts.WorkDir), true)
			}
			if len(args) == 1 {
				if f.name != "" {
					return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.CliInitNameWithArg)).
						WithHint(i18n.T(msgid.CliInitHintNameOrArg)).WithExit(clierr.ExitUsage)
				}
				return runInitCreate(opts, args[0], f)
			}
			return runInitComplete(opts, f)
		},
	}
	cmd.Flags().StringVar(&f.name, "name", "", i18n.T(msgid.CliInitFlagName))
	cmd.Flags().BoolVar(&f.noSkills, "no-skills", false,
		i18n.T(msgid.CliInitDonTInstallTheAi))
	cmd.Flags().BoolVar(&f.yes, "yes", false, i18n.T(msgid.CliInitFlagYes))
	cmd.Flags().BoolVar(&f.hooksOnly, "hooks", false,
		i18n.T(msgid.CliInitOnlyInstallThePreCommit))
	return cmd
}

// runInitCreate 是创建式：新建 <name>/ 目录并补全出整套骨架。目录已存在且不空就拒绝——
// 往别人已有的目录里写是补全式的事，要在那个目录里显式跑 brickkit init。
func runInitCreate(opts *Options, name string, f initFlags) error {
	if err := projfile.ValidateProjectName(name); err != nil {
		return err
	}
	dir := filepath.Join(opts.WorkDir, name)
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		return clierr.New(clierr.CodeProjectExists, i18n.T(msgid.CliInitPathNotADir, name)).
			WithDetail(i18n.T(msgid.LabelPath), dir).
			WithHint(i18n.T(msgid.CliInitHintOtherName))
	}
	if empty, err := project.DirIsEmpty(dir); err == nil && !empty {
		return clierr.New(clierr.CodeProjectExists, i18n.T(msgid.CliInitDirNotEmpty, name)).
			WithDetail(i18n.T(msgid.LabelDir), dir).
			WithHint(i18n.T(msgid.CliInitHintCompleteInstead, name))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.IOFailed, i18n.T(msgid.ActionMkdir))).
			WithDetail(i18n.T(msgid.LabelPath), dir).WithCause(err).WithHint(i18n.T(msgid.HintCheckDiskAccess))
	}
	layout := project.NewLayout(dir)
	plan, err := project.PlanComplete(layout, name)
	if err != nil {
		return err
	}
	if err := plan.Apply(layout); err != nil {
		return err
	}
	logging.Info(i18n.T(msgid.LogProjectInitialized), "project", name, "config", layout.DeclPath())

	opts.Printf("%s\n", i18n.T(msgid.CliInitProjectInitialized, name))
	opts.Printf("   📄 %-21s%s\n", project.FileDecl, i18n.T(msgid.CliInitProjectConfig))
	opts.Printf("   📄 %-21s%s\n", project.FileDeploy, i18n.T(msgid.CliInitDeployFile))
	opts.Printf("   📁 %-21s%s\n", project.DirConfig+"/", i18n.T(msgid.CliInitConfigDir))
	// components/ 与 shell/ 是两个默认本地安装源，点出来
	opts.Printf("   📁 %-21s%s\n", project.DirComponents+"/", i18n.T(msgid.CliInitComponentSourceConfiguredAsThe))
	opts.Printf("   📁 %-21s%s\n", project.DirShell+"/", i18n.T(msgid.CliInitShellDir))
	opts.Printf("   📁 %-21s%s\n", project.DirBrickkit+"/", i18n.T(msgid.CliInitCliWorkingDirectory))
	opts.Printf("   📄 %-21s%s\n", project.FileProjectDoc, i18n.T(msgid.CliInitProjectDoc))
	if err := finishInit(opts, layout, f, false); err != nil {
		return err
	}

	opts.Printf("\n")
	printNextSteps(opts, []nextStep{
		{cmd: "cd " + name},
		{cmd: "brickkit add --local", what: i18n.T(msgid.CliInitBrickkitAddLocalAddEvery)},
		{cmd: "brickkit add <scope>/<name>@<version>", what: i18n.T(msgid.CliInitStepAddComponent)},
		{cmd: "brickkit up", what: i18n.T(msgid.CliInitBrickkitUpStartEverythingIn)},
	})
	return nil
}

// runInitComplete 是补全式：当前目录缺什么补什么，已有的一个字节都不动；.gitignore 只校验、
// 不改。非空目录先打印计划、等确认。最后跑一遍与 lint 相同的装载，确认项目能 up。
func runInitComplete(opts *Options, f initFlags) error {
	layout := project.NewLayout(opts.WorkDir)
	name, err := project.ProjectNameFor(layout, f.name)
	if err != nil {
		return err
	}
	plan, err := project.PlanComplete(layout, name)
	if err != nil {
		return err
	}
	empty, err := project.DirIsEmpty(layout.Root)
	if err != nil {
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.IOFailed, i18n.T(msgid.ConfigActionReadFile))).
			WithDetail(i18n.T(msgid.LabelPath), layout.Root).WithCause(err).WithHint(i18n.T(msgid.HintCheckDiskAccess))
	}
	if !empty {
		opts.Printf("%s\n", i18n.T(msgid.CliInitCompletePlanHeader))
		renderCompletePlan(opts, plan)
		if !f.yes && !confirm(opts, i18n.T(msgid.CliInitCompleteConfirm)) {
			opts.Printf("%s\n", i18n.T(msgid.CliInitCompleteCancelled))
			return nil
		}
	}
	if err := plan.Apply(layout); err != nil {
		return err
	}
	logging.Info(i18n.T(msgid.LogProjectInitialized), "project", name, "config", layout.DeclPath())

	opts.Printf("%s\n", i18n.T(msgid.CliInitCompleted, name))
	if empty {
		renderCompletePlan(opts, plan)
	}
	renderGitignoreWarning(opts, project.FileGitignore, plan.GitignoreMissing)
	if plan.ProjectDocUnmanaged {
		opts.Printf("   ℹ️ %s\n", i18n.T(msgid.CliInitProjectDocUnmanaged, project.FileProjectDoc))
	}
	renderInsideProjectNote(opts, layout.Root)
	return finishInit(opts, layout, f, true)
}

// renderInsideProjectNote：刚补全的目录是外面某个项目的本地组件时说一句——焦点运行不需要
// 工作台；而这里有了 brickkit.yaml 之后，这个目录里的命令就用这个工作台，不再往上找。
func renderInsideProjectNote(opts *Options, dir string) {
	outer, found, err := project.FindRoot(filepath.Dir(dir))
	if err != nil || !found {
		return
	}
	l := project.NewLayout(outer)
	decl, err := projfile.ParseFile(l.DeclPath())
	if err != nil {
		return
	}
	if id, inside := componentAt(l, decl, dir); inside {
		opts.Printf("   💡 %s\n", i18n.T(msgid.CliInitInsideProjectNote, id))
	}
}

// renderGitignoreWarning 大声说出 .gitignore 缺的必需条目（提案 §11.5 的核心防线）：
// brickkit init 与 add --local --init 的子工作台走同一处，谁也不能悄悄放过。
func renderGitignoreWarning(opts *Options, file string, missing []string) {
	if len(missing) == 0 {
		return
	}
	w := clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliInitGitignoreMissing)).
		WithDetail(i18n.T(msgid.LabelFile), file)
	for _, rule := range missing {
		w = w.WithDetail(i18n.T(msgid.CliInitGitignoreMissingEntry), rule)
	}
	opts.Printf("%s", opts.render(w.WithHint(i18n.T(msgid.CliInitHintAddGitignore))))
}

// renderCompletePlan 列出补全要创建、跳过的文件与 .gitignore 的缺项。
func renderCompletePlan(opts *Options, plan *project.CompletePlan) {
	for _, rel := range plan.Create {
		opts.Printf("   ✅ %s\n", i18n.T(msgid.CliInitPlanCreate, rel))
	}
	if plan.GitignoreCreate {
		opts.Printf("   ✅ %s\n", i18n.T(msgid.CliInitPlanCreate, project.FileGitignore))
	}
	if plan.ProjectDoc {
		opts.Printf("   ✅ %s\n", i18n.T(msgid.CliInitPlanCreate, project.FileProjectDoc))
	}
	for _, rel := range plan.Skip {
		opts.Printf("   ⏭️  %s\n", i18n.T(msgid.CliInitPlanSkip, rel))
	}
	if len(plan.GitignoreMissing) > 0 {
		opts.Printf("   ⚠️  %s\n", i18n.T(msgid.CliInitPlanGitignore, strings.Join(plan.GitignoreMissing, i18n.T(msgid.ListSeparator))))
	}
}

// finishInit 是两种模式共同的收尾：技能、提交前检查、项目文档的组件表。
// check 为真时（补全式）先做收尾校验：装载失败说明补全了但还跑不起来，命令以失败结束。
func finishInit(opts *Options, layout project.Layout, f initFlags, check bool) error {
	if !f.noSkills {
		if err := installSkills(opts, layout); err != nil {
			return err
		}
	}
	if err := installCommitHook(opts, layout, false); err != nil {
		return err
	}
	proj, err := project.Load(layout.Root, project.LoadOptions{NoLocal: true})
	if err != nil {
		if !check {
			return err
		}
		// 问题本身原样打印（与 up / lint 报的是同一块），命令再以一句总结失败
		opts.Printf("%s", opts.render(clierr.As(err)))
		return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliInitClosingCheckFailed)).
			WithHint(i18n.T(msgid.CliInitHintRunLint)).
			WithCause(err)
	}
	if _, err := project.WriteProjectDoc(layout, proj); err != nil {
		return err
	}
	if check {
		opts.Printf("   ✅ %s\n", i18n.T(msgid.CliInitClosingCheckPassed))
	}
	return nil
}

// installSkills 装入 AI 助手技能，并把跳过的文件说清楚。
//
// 装不上是**错误**而不是静默跳过：init 说了它会装，那就得装上或者说明为什么没装。
// 但错误里要讲明项目本身已经建好了——否则人会以为整个 init 都白跑了。
func installSkills(opts *Options, layout project.Layout) error {
	in := skills.Installer{
		Root:     layout.Root,
		LockPath: layout.SkillsLockPath(),
		Version:  version.Version,
		// 显式钉住这次调用当下的 CLI 语言：init 是"就当现在装一份"，
		// 不该被这个目录里可能残留的旧 skills.lock（比如清空 brickkit.yaml
		// 后重新 init）悄悄带偏语言。
		Lang: i18n.Current(),
	}
	res, err := in.Apply()
	if err != nil {
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliInitErrorFailedToInstallThe)).
			WithDetail(i18n.T(msgid.LabelReason), err.Error()).
			WithHint(
				i18n.T(msgid.CliInitTheProjectItselfHasBeen),
				i18n.T(msgid.CliInitAfterFixingThePermissionsRun),
				i18n.T(msgid.CliInitYouCanAlsoDoWithout),
			).
			WithCause(err)
	}

	if len(res.Written) > 0 {
		opts.Printf("   📁 %-21s%s\n", ".claude/skills/", i18n.T(msgid.CliInitAiAssistantSkills))
		opts.Printf("   📄 %-21s%s\n", "AGENTS.md", i18n.T(msgid.CliInitAiAssistantProjectGuide))
	}
	renderSkillsLangFallback(opts, in.Lang, res.Lang)
	// 跳过的必须说出来。默默不装，用户会以为装了、然后奇怪它为什么没效果。
	for _, s := range res.Skipped {
		opts.Printf("   ⏭  %-21s%s\n", s.Target, i18n.T(msgid.CliInitAlreadyExistsLeftUnchanged, s.State.Label()))
	}
	logging.Info(i18n.T(msgid.LogSkillsInstalled),
		"written", len(res.Written), "skipped", len(res.Skipped))
	return nil
}

// installCommitHook 装提交前检查用的 pre-commit hook。
//
// # 为什么 init 顺带装时要多一条"项目根 == 仓库根"
//
// init 完全可能跑在一个**跟本项目无关的仓库**的子目录里——本仓库的
// docs/archive/guide/playground/ 就是这样（它在 brickKit 自己的仓库里）。那时候
// 自动装等于往别人的 .git/hooks 里写东西，而那个人根本没要求过。
//
// 所以顺带装只服务最常见的那一种：项目根就是仓库根。嵌套的项目要装，
// 就自己显式说一句 brickkit init --hooks——那时 explicit 为真，装不上是错误。
func installCommitHook(opts *Options, layout project.Layout, explicit bool) error {
	// 显式请求时先确认这儿真有个项目。
	//
	// 没有 brickkit.yaml 却把 hook 装上去，是这套设计一直在防的那种**静默误判**：
	// 使用者敲完命令以为"闸门开了"，而那个 hook 在这儿永远不会响——它要比对的
	// 意图声明根本不存在。多项目仓库里从仓库根跑这条命令，撞的正是这一条，
	// 而他要的是进每个项目根各跑一次。
	//
	// 只在 explicit 时查：runInit 顺带装的那一次，配置是它自己刚生成的。
	if explicit {
		if _, err := os.Stat(layout.DeployPath()); err != nil {
			return clierr.New(clierr.CodeProjectMissing,
				i18n.T(msgid.CliInitErrorThereIsNoHere, project.FileDeploy)).
				WithDetail(i18n.T(msgid.CliInitLocationsSearched), layout.DeployPath()).
				WithHint(
					i18n.T(msgid.CliInitThePreCommitHookHas, project.FileDeploy),
					i18n.T(msgid.CliInitFirstRunBrickkitInitProject),
					i18n.T(msgid.CliInitWhenOneRepositoryHoldsSeveral),
				).WithCause(err)
		}
	}

	repo, err := gitrepo.Open(layout.Root)
	if err != nil {
		if explicit {
			return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliInitErrorThisIsNotA)).
				WithHint(
					i18n.T(msgid.CliInitAPreCommitHookCan),
					i18n.T(msgid.CliInitRunGitInitFirstThen),
				)
		}
		hookHint(opts)
		return nil
	}

	rel, ok := repo.Rel(layout.Root)
	if !ok {
		if explicit {
			return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliInitErrorTheProjectRootIs)).
				WithDetail(i18n.T(msgid.CliInitProject), layout.Root).
				WithDetail(i18n.T(msgid.LabelRepo), repo.Root()).WithHint(i18n.T(msgid.CliInitHintHooksNeedRepo))
		}
		hookHint(opts)
		return nil
	}
	if !explicit && rel != "." {
		// 项目嵌在别人的仓库里：不替他决定往那个仓库写东西
		hookHint(opts)
		return nil
	}

	path, added, err := installHook(repo,
		hookProject{Dir: rel}, brickkitBinPath(), version.Version)
	if err != nil {
		return err
	}

	display := path
	if p, ok := repo.Rel(path); ok {
		display = p
	}
	if !added && explicit {
		// 说"已经装过了"会误导：这一次**确实**重写了文件——版本戳与写死在
		// 里面的可执行文件绝对路径都跟着刷新了。升级 CLI 之后跑这条命令的人
		// 要的正是这个刷新，看到"已经装过了"却会以为什么都没发生。
		opts.Printf("%s\n", i18n.T(msgid.CliInitThePreCommitHookWas, version.Version, display))
		return nil
	}
	// %-21s 只管对齐：路径恰好占满 21 列时（`.git/hooks/pre-commit` 就是）
	// 它和后面的说明会粘在一起，所以那种情况补一个空格。
	sep := ""
	if utf8.RuneCountInString(display) >= 21 {
		sep = " "
	}
	opts.Printf("   🪝 %-21s%s%s\n", display, sep, i18n.T(msgid.CliInitCheckTheComponentLayoutBefore))
	return nil
}

// hookHint 在没自动装上时说清怎么补装。
//
// 装不上是**常态而不是错误**：init 常常跑在 git init 之前，项目也常常嵌在
// 别人的仓库里。但不说一声，使用者就永远不知道有这道闸门。
func hookHint(opts *Options) {
	opts.Printf("   💡 %s\n",
		i18n.T(msgid.CliInitIfComponentSourceGoesInto))
}

// brickkitBinPath 返回当前可执行文件的绝对路径，取不到就回落到裸名字。
//
// 把绝对路径写进 hook 是必要的：GUI 客户端（VS Code 的源代码管理面板、
// macOS 上从 Finder 启动的客户端）的 PATH 常常不含 ~/.local/bin，
// 只写 "brickkit" 会让 hook 在那些地方一律走"找不到就放行"那一支。
func brickkitBinPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "brickkit"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}
