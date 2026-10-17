package cli

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"strings"
	"time"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/procsup"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/runcmd"
	"github.com/brickkit/brickkit/internal/sessionlock"
	"github.com/brickkit/brickkit/internal/source"
	"github.com/brickkit/brickkit/internal/workspace"
)

// anyModeLocal 判断项目里有没有 mode: local 组件（看生效的那份部署文件）。
func anyModeLocal(proj *project.Project) bool {
	for _, c := range proj.Decl.Components {
		if proj.DeployEntry(c.ID, c.Version).Mode == deployfile.ModeLocal {
			return true
		}
	}
	return false
}

// checkLocalRepos 核对这次从本地仓库运行代码的组件：
//
//   - mode: local 的组件：平台自己在它的仓库里启动它，没有仓库就无从谈起
//   - mode: debug 的组件：进程由使用者在 IDE 里启动，仓库可能在任何地方——找得到才核对
//   - 以裸进程运行的外壳承载的成员：成员代码来自成员的本地仓库——找得到才核对
//
// 找到的仓库必须正是这次运行的版本，而且是默认版本：本地仓库永远是默认版本的代码
// 。容器部署拉镜像，不看本地仓库。
//
// 放在生成阶段查（buildUpPlan 里，--dry-run 也会走到）："生成阶段就该知道、
// 不该等运行时才炸"。
func checkLocalRepos(proj *project.Project, states *cascade.Result) error {
	p := clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpLocalReposInvalid)).
		WithSource(i18n.T(msgid.LabelFile), proj.DeployPath)
	hints := map[string]bool{}
	var hintOrder []string
	hint := func(h string) {
		if !hints[h] {
			hints[h] = true
			hintOrder = append(hintOrder, h)
		}
	}
	check := func(ref resolver.Ref, requireRepo bool) {
		field := deployEntryField(proj, ref)
		dir, ok := proj.LocalRepo(ref.ID)
		if !ok {
			if requireRepo {
				p.Add(field, i18n.T(msgid.CliUpNoLocalSourceFor, ref.ID))
			}
			return
		}
		if def, _ := proj.Decl.DefaultVersion(ref.ID); def != ref.Version {
			p.Add(field, i18n.T(msgid.CliUpLocalRepoNotDefault, ref.String(), def))
			hint(i18n.T(msgid.CliUpHintMakeDefault, ref.String()))
			return
		}
		version, err := project.LocalRepoVersion(dir)
		if err != nil {
			p.Add(field, i18n.T(msgid.CliUpLocalRepoUnreadable, ref.String(), dir, err.Error()))
			return
		}
		if version == ref.Version {
			return
		}
		p.Add(field, i18n.T(msgid.CliUpLocalRepoVersionMismatch, ref.String(), dir, version))
		hint(i18n.T(msgid.CliUpHintUpgradeToRepo, ref.ID+"@"+version))
		hint(i18n.T(msgid.CliUpHintCheckoutTag, dir, source.VersionTag(ref.ID, ref.Version, proj.LocalRepoSubpath(ref.ID))))
	}
	for _, c := range proj.Decl.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		entry := proj.DeployEntry(c.ID, c.Version)
		if !states.IsRunning(ref) || !entry.IsBareProcess() {
			continue
		}
		check(ref, entry.Mode == deployfile.ModeLocal)
		if !c.IsShell() {
			continue
		}
		for _, member := range proj.MembersOf(c.ID) {
			id, version := member.Key()
			if version == "" {
				version, _ = proj.Decl.DefaultVersion(id)
			}
			mref := resolver.Ref{ID: id, Version: version}
			if host, ok := states.HostOf(proj, mref); ok && host == ref {
				check(mref, false)
			}
		}
	}
	return p.WithHint(hintOrder...).Err()
}

// deployEntryField 返回覆盖这个组件版本的部署条目的字段路径（如 components[0].members[1]）。
func deployEntryField(proj *project.Project, ref resolver.Ref) string {
	l, _ := proj.Deploy.EntryAt(ref.ID, ref.Version, proj.Decl.IsDefault(ref.ID, ref.Version))
	return l.Field
}

// localComponentPlan 是一个 mode: local 组件要怎么启动的全部结论。
type localComponentPlan struct {
	Ref     resolver.Ref
	Service string
	Dir     string
	Command runcmd.Command
	Env     []string
	// Withhold 挡住继承来的、平台管的那些名字（platformOwned）
	Withhold func(name string) bool
	Port     int
	// Ports 是这个进程要在宿主机上绑的全部端口：Port、它的 extraPorts，是外壳时还有它替成员监听的端口。
	// 启动之前逐个确认空着（checkLocalPorts）。
	Ports []localPort
	// Migrations 是启动这个进程之前要从源码跑的迁移（local.migrationCommand），按顺序：是外壳时先跑它承载的、
	// 从源码运行的成员的（按启动顺序），最后是它自己的。任何一条失败，进程都不启动。
	Migrations []localMigration
	// StopGrace 是这个组件声明的停机宽限期（0 = 没声明）。
	StopGrace time.Duration
}

// localMigration 是一条从源码跑的迁移：组件目录里、用这个组件的那份环境跑它的 local.migrationCommand。
type localMigration struct {
	Ref resolver.Ref
	// Name 是输出前缀：<服务名>-migration，与 compose 里迁移容器的名字一致。
	Name     string
	Command  runcmd.Command
	Env      []string
	Withhold func(name string) bool
}

// localPort 是本机进程要绑的一个宿主机端口。
type localPort struct {
	Port int
	// Of 说明这是谁的端口：版本化服务名，额外端口再带上它的名字。
	Of string
	// Movable：进程自己的主端口，部署文件里写 localPort 就能挪开；其余端口写在 component.yaml 里，挪不动。
	Movable bool
}

// portsOf 列出本机进程要绑的端口：自己的主端口与 extraPorts，外壳还有它这次承载的每个成员的端口
// （外壳进程替成员在成员自己声明的端口上监听，见 internal/shell）。
func portsOf(service string, port int, m *manifest.Manifest, members []*resolver.Node) []localPort {
	out := []localPort{{Port: port, Of: service, Movable: true}}
	add := func(owner string, d manifest.Deployment, withMain bool) {
		if withMain && d.Port > 0 {
			out = append(out, localPort{Port: d.Port, Of: owner})
		}
		for _, extra := range d.ExtraPorts {
			out = append(out, localPort{Port: extra.Port, Of: owner + " (" + extra.Name + ")"})
		}
	}
	add(service, m.Deployment, false)
	for _, member := range members {
		add(manifest.ServiceName(member.Ref.ID, member.Ref.Version), member.Manifest.Deployment, true)
	}
	return out
}

// localGracePeriod 是 Ctrl+C 之后等多久再强杀：本次在本机跑的组件里声明得最长的那个，
// 都没声明时交给 procsup 的默认值。监管器一次停所有进程，等的就是最慢的那个。
func localGracePeriod(plans []localComponentPlan) time.Duration {
	var longest time.Duration
	for _, p := range plans {
		longest = max(longest, p.StopGrace)
	}
	return longest
}

// collectLocalComponents 按拓扑序收集全部 mode: local 组件，探测出各自的启动命令。
//
// hosted 是这次每个外壳承载的成员（hostedMembers）：以本机进程运行的外壳替它们监听端口。memberEnvFiles 是
// 其中从源码运行的那些（compose.Result.MemberEnvFiles）：它们的迁移在外壳启动之前从源码跑。
func collectLocalComponents(
	proj *project.Project, graph *resolver.Graph, order *resolver.Plan,
	localEnvFiles, memberEnvFiles []compose.LocalEnvFile, hosted map[resolver.Ref][]resolver.Ref,
) ([]localComponentPlan, error) {
	localMode := make(map[resolver.Ref]bool, len(proj.Decl.Components))
	for _, c := range proj.Decl.Components {
		if proj.DeployEntry(c.ID, c.Version).Mode == deployfile.ModeLocal {
			localMode[resolver.Ref{ID: c.ID, Version: c.Version}] = true
		}
	}
	if len(localMode) == 0 {
		return nil, nil
	}

	portOf := make(map[resolver.Ref]int, len(localEnvFiles))
	fileOf := make(map[resolver.Ref]compose.LocalEnvFile, len(localEnvFiles))
	for _, f := range localEnvFiles {
		portOf[f.Ref] = f.Port
		fileOf[f.Ref] = f
	}
	memberFileOf := make(map[resolver.Ref]compose.LocalEnvFile, len(memberEnvFiles))
	for _, f := range memberEnvFiles {
		memberFileOf[f.Ref] = f
	}

	var out []localComponentPlan
	for _, step := range order.Steps {
		ref := step.Ref
		if !localMode[ref] {
			continue
		}
		node := graph.Node(ref)
		if node == nil || node.Manifest == nil {
			continue
		}
		port, ok := portOf[ref]
		if !ok {
			continue // localEnvFiles 只含"这次真的在跑"的组件；跟 collectTargets 同一份 running 集合
		}

		dir, found := proj.LocalRepo(ref.ID)
		if !found {
			dir = workspace.SourceDir(proj.Layout, ref.ID)
		}
		hints := hintsFromManifest(node.Manifest.Local)
		cmd, err := runcmd.Detect(dir, hints, runcmd.Params{Port: port})
		if err != nil {
			return nil, detectionError(ref, err)
		}
		if err := cmd.CheckProgram(); err != nil {
			return nil, programMissingError(ref, err)
		}

		env, err := buildLocalEnv(ref, cmd.Language, fileOf[ref], port, cmd.Env)
		if err != nil {
			return nil, err
		}
		// 外壳承载的成员：端口由外壳进程替它们监听；从源码运行的那些，迁移在外壳启动之前从源码跑
		var members []*resolver.Node
		var migrations []localMigration
		hostedHere := map[resolver.Ref]bool{}
		for _, member := range hosted[ref] {
			hostedHere[member] = true
		}
		for _, ms := range order.Steps { // 按启动顺序：成员之间有依赖时，被依赖的先迁移
			if !hostedHere[ms.Ref] {
				continue
			}
			n := graph.Node(ms.Ref)
			if n == nil || n.Manifest == nil {
				continue
			}
			members = append(members, n)
			if file, fromSource := memberFileOf[ms.Ref]; fromSource {
				m, err := memberMigration(proj, n, ms.Service, file)
				if err != nil {
					return nil, err
				}
				if m != nil {
					migrations = append(migrations, *m)
				}
			}
		}
		if node.Manifest.Local != nil && len(node.Manifest.Local.MigrationCommand) > 0 {
			m := cmd.Sibling(node.Manifest.Local.MigrationCommand)
			if err := m.CheckProgram(); err != nil {
				return nil, migrationProgramMissingError(ref, err)
			}
			migrations = append(migrations, localMigration{
				Ref: ref, Name: compose.MigrationService(step.Service), Command: m, Env: env, Withhold: platformOwned(node.Manifest),
			})
		}

		out = append(out, localComponentPlan{
			Ref: ref, Service: step.Service, Dir: dir, Command: cmd, Env: env, Withhold: platformOwned(node.Manifest), Port: port,
			Ports:      portsOf(step.Service, port, node.Manifest, members),
			Migrations: migrations,
			StopGrace:  time.Duration(fileOf[ref].StopGracePeriodSeconds) * time.Second,
		})
	}
	return out, nil
}

// platformOwned 判断一个名字是不是平台管的：保留名（COMPONENT_ID、PORT、*_ENDPOINT……）与组件自己的
// 配置项。mode: local 的进程继承终端的环境，但这些名字只能来自平台——终端里 export 过的
// DEMO_BUS_ENDPOINT 会让"可选依赖缺席时变量根本不存在"失效，旧的 DB_HOST 会悄悄盖过 config/
// 里没写值的那一项（"环境从不隐式覆盖"）。
func platformOwned(m *manifest.Manifest) func(name string) bool {
	keys := map[string]bool{}
	if m != nil && m.ConfigSchema != nil {
		for k := range m.ConfigSchema.Properties {
			keys[k] = true
		}
	}
	return func(name string) bool {
		_, reserved := manifest.ReservedHitFor(name)
		return reserved || keys[name]
	}
}

// hintsFromManifest 把 component.yaml 的 local: 块翻译成 runcmd.Hints；
// 没写这个块（*Local 为 nil）就是全部留空，交给自动探测。
func hintsFromManifest(l *manifest.Local) runcmd.Hints {
	if l == nil {
		return runcmd.Hints{}
	}
	return runcmd.Hints{Language: l.Language, RunCommand: l.RunCommand}
}

// detectionReasonMessages 给 runcmd 的每个原因配一句话（%[1]s 是 Problem.Detail，%[2]s 是候选项）。
// runcmd 只给结构化的原因，不碰语言；话由这里按当前语言说。漏配的原因由测试拦下。
var detectionReasonMessages = map[runcmd.Reason]msgid.ID{
	runcmd.ReasonMarkerMissing:              msgid.CliUpReasonMarkerMissing,
	runcmd.ReasonUnreadableManifest:         msgid.CliUpReasonUnreadableManifest,
	runcmd.ReasonNoEntryPoint:               msgid.CliUpReasonNoEntryPoint,
	runcmd.ReasonMultipleEntryPoints:        msgid.CliUpReasonMultipleEntryPoints,
	runcmd.ReasonNoStartScript:              msgid.CliUpReasonNoStartScript,
	runcmd.ReasonConflictingPackageManagers: msgid.CliUpReasonConflictingPackageManagers,
	runcmd.ReasonUnsupportedPackageManager:  msgid.CliUpReasonUnsupportedPackageManager,
	runcmd.ReasonNotSpringBoot:              msgid.CliUpReasonNotSpringBoot,
	runcmd.ReasonMultiModule:                msgid.CliUpReasonMultiModule,
	runcmd.ReasonConflictingBuildTools:      msgid.CliUpReasonConflictingBuildTools,
}

// detectionError 把 runcmd 探测失败说成当前语言的话：标题说哪个组件起不来，
// 明细说为什么——认得出却给不出命令的每种语言各占一行。
func detectionError(ref resolver.Ref, err error) error {
	e := clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpCouldNotDetermineHowToStart, ref.String()))
	hint := i18n.T(msgid.CliUpWriteLocalRunCommandInThe, ref.ID)
	var (
		noCommand *runcmd.NoCommandError
		ambiguous *runcmd.AmbiguousError
		unknown   *runcmd.UnknownLanguageError
		dir       *runcmd.DirError
	)
	switch {
	case errors.As(err, &noCommand) && len(noCommand.Problems) == 0:
		e = e.WithDetail(i18n.T(msgid.LabelReason),
			i18n.T(msgid.CliUpDetectNoLanguage, noCommand.Dir, strings.Join(runcmd.Languages(), ", ")))
	case errors.As(err, &noCommand):
		e = e.WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliUpDetectNoCommand, noCommand.Dir))
		for _, p := range noCommand.Problems {
			e = e.WithDetail(p.Language, problemText(p))
		}
	case errors.As(err, &ambiguous):
		parts := make([]string, len(ambiguous.Candidates))
		for i, c := range ambiguous.Candidates {
			parts[i] = c.Language + " (" + strings.Join(c.Argv, " ") + ")"
		}
		e = e.WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliUpDetectAmbiguous, strings.Join(parts, ", ")))
		return e.WithHint(i18n.T(msgid.CliUpHintSetLocalLanguage, ref.ID), hint)
	case errors.As(err, &unknown):
		e = e.WithDetail(i18n.T(msgid.LabelReason),
			i18n.T(msgid.CliUpDetectUnknownLanguage, unknown.Language, strings.Join(runcmd.Languages(), ", ")))
	case errors.As(err, &dir) && dir.Err == nil:
		e = e.WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliUpDetectNotDir, dir.Dir))
	case errors.As(err, &dir):
		// 操作系统给的原因（permission denied 之类）原样带上：那是系统的话，不是本程序的
		e = e.WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.CliUpDetectDirUnreadable, dir.Dir, dir.Err.Error()))
	default:
		// runcmd 之外的失败（端口越界之类，CLI 自己选的值出了错）：只可能是 bug
		return e.WithDetail(i18n.T(msgid.LabelReason), err.Error()).WithHint(i18n.T(msgid.HintInternalBug))
	}
	return e.WithHint(hint)
}

// problemText 是一种语言"认得出、给不出命令"的原因。
func problemText(p runcmd.Problem) string {
	id, ok := detectionReasonMessages[p.Reason]
	if !ok {
		return string(p.Reason)
	}
	return i18n.T(id, p.Detail, strings.Join(p.Options, ", "))
}

func programMissingError(ref resolver.Ref, err error) error {
	reason := err.Error()
	var missing *runcmd.ProgramMissingError
	if errors.As(err, &missing) {
		reason = i18n.T(msgid.CliUpProgramNotFound, missing.Program)
		if missing.Program == "" {
			reason = i18n.T(msgid.CliUpStartCommandEmpty)
		}
	}
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpTheDetectedProgramIsNotInstalled, ref.String())).
		WithDetail(i18n.T(msgid.LabelReason), reason).
		WithHint(i18n.T(msgid.CliUpHintInstallProgram), i18n.T(msgid.CliUpWriteLocalRunCommandInThe, ref.ID))
}

// memberMigration 是本机外壳里一个从源码运行的成员的迁移：在成员的本地仓库里、用它在外壳里拿到的那份环境
// （file，地址指向本机）跑它的 local.migrationCommand。成员没写这个字段时返回 nil——迁移不跑，compose 已经提醒。
func memberMigration(proj *project.Project, n *resolver.Node, service string, file compose.LocalEnvFile) (*localMigration, error) {
	local := n.Manifest.Local
	if n.Manifest.Migration == nil || local == nil || len(local.MigrationCommand) == 0 {
		return nil, nil
	}
	dir, ok := proj.LocalRepo(n.Ref.ID)
	if !ok {
		return nil, nil // compose 只在找得到本地仓库时才把成员算作从源码运行
	}
	cmd, err := runcmd.Detect(dir, runcmd.Hints{Language: local.Language, RunCommand: local.MigrationCommand},
		runcmd.Params{Port: file.Port})
	if err != nil {
		return nil, detectionError(n.Ref, err)
	}
	if err := cmd.CheckProgram(); err != nil {
		return nil, migrationProgramMissingError(n.Ref, err)
	}
	env, err := buildLocalEnv(n.Ref, cmd.Language, file, file.Port, cmd.Env)
	if err != nil {
		return nil, err
	}
	return &localMigration{
		Ref: n.Ref, Name: compose.MigrationService(service), Command: cmd, Env: env, Withhold: platformOwned(n.Manifest),
	}, nil
}

// migrationProgramMissingError：local.migrationCommand 要的程序这台机器上没有。在启动任何东西之前报。
func migrationProgramMissingError(ref resolver.Ref, err error) error {
	reason := err.Error()
	var missing *runcmd.ProgramMissingError
	if errors.As(err, &missing) {
		reason = i18n.T(msgid.CliUpProgramNotFound, missing.Program)
	}
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpMigrationProgramNotInstalled, ref.String())).
		WithDetail(i18n.T(msgid.LabelReason), reason).
		WithHint(i18n.T(msgid.CliUpHintInstallProgram), i18n.T(msgid.CliUpHintCheckLocalMigrationCommand))
}

// localDebugEnvVarsToStrip 是每种语言里，一旦从启动 brickkit up 的那个 shell
// 继承到就会让进程在没人要求调试的情况下卡住等调试器连接的环境变量。
//
// procsup 启动子进程时继承当前环境（去掉 platformOwned 挡住的名字），再叠上 spec.Env（internal/procsup/
// supervisor.go）——mode: local 的子进程会带着这个终端会话的完整环境。如果这个
// shell 里曾经为了别的原因全局设过 NODE_OPTIONS=--inspect-brk 之类的值（哪怕
// 早忘了、这次运行根本没打算调试这个组件），子进程会原样继承、悄悄卡住。
//
// Go/Rust/.NET 不受影响（启动方式不会被任何环境变量触发调试等待）；Python 也
// 不受影响（debugpy 要显式包一层命令才生效，没有"环境变量意外触发"这回事）。
var localDebugEnvVarsToStrip = map[string][]string{
	runcmd.LangNode: {"NODE_OPTIONS"},
	runcmd.LangJava: {"JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS"},
}

// buildLocalEnv 把 vars 严格展开成 "KEY=VALUE" 列表，叠上语言适配器自己的
// 环境变量与 PORT。展开不了任何一个 ${VAR} 都直接报错、点名是哪个变量——
// 跟 local-debug.*.env 文件"展开不了就留着占位符"的宽松策略不同：这是真的
// 要喂给进程的环境，不是给人看的排障文件。
//
// language 是探测出的语言（手写 runCommand 又没标 language 时为空）——按它
// 无条件追加 localDebugEnvVarsToStrip 里对应变量的空值覆盖：不管调用方这次
// 有没有继承到这些变量，覆盖成空值都是无害的；Go 的 exec.Cmd.Env 对同名 key
// 取最后一个，这里追加在最后，天然盖掉 os.Environ() 里继承来的任何值，不需要
// procsup 知道这件事。
func buildLocalEnv(
	ref resolver.Ref, language string, file compose.LocalEnvFile, port int, cmdEnv []string,
) ([]string, error) {
	out := make([]string, 0, len(file.Vars)+len(cmdEnv)+2)
	for _, v := range file.Vars {
		// brickkit 自己拉起的进程对缺失的 ${VAR} 是严格的：带着字面量占位符启动，
		// 只会得到一个莫名其妙的运行时错误（IDE 读的那份文件对它宽松，那是给人看的）
		if missing, ok := file.Unresolved[v.Name]; ok {
			return nil, missingEnvVarError(ref, v.Name, missing)
		}
		out = append(out, v.Name+"="+v.Value.Text)
	}
	out = append(out, cmdEnv...)
	out = append(out, fmt.Sprintf("PORT=%d", port))
	for _, name := range localDebugEnvVarsToStrip[language] {
		out = append(out, name+"=")
	}
	return out, nil
}

func missingEnvVarError(ref resolver.Ref, envVarName, missingVarName string) error {
	return clierr.New(clierr.CodeConfigInvalid,
		i18n.T(msgid.CliUpMissingEnvVarFor, ref.String(), missingVarName, envVarName)).WithHint(i18n.T(msgid.CliUpHintSetEnvVar, missingVarName))
}

// renderLocalComponentCommands 在 --dry-run 下告诉使用者每个 mode: local
// 组件探测出的启动命令是什么——"看看会发生什么"这条命令的整个意义所在，
// 本地组件不该是唯一说不清楚的部分。
func renderLocalComponentCommands(opts *Options, plans []localComponentPlan) {
	if len(plans) == 0 {
		return
	}
	opts.Printf("\n%s\n", i18n.T(msgid.CliUpLocalComponentsWouldStart))
	for _, p := range plans {
		opts.Printf("   %s  %s\n", p.Ref, strings.Join(p.Command.Argv, " "))
	}
}

// startupProbeTimeout 是等一个本地组件监听期望端口的上限——不是健康检查，
// 只是"命令跑起来了没有"的一次性检测（procsup 包文档）。
const startupProbeTimeout = 30 * time.Second

// runLocalComponents 拿会话锁，按拓扑序拉起每个本地组件，阻塞到全部退出、
// 某个进程崩溃触发整个会话收尾、或者用户按了 Ctrl+C。
//
// 顺序上，这一步永远在 eng.Up() 之后调用（设计决定第 5 条）：容器已经在跑，
// 本地进程依赖的容器地址已经可用，不需要把容器和本地进程交织进同一个
// 拓扑序循环里。
func runLocalComponents(
	ctx context.Context, opts *Options, layout project.Layout,
	plans []localComponentPlan, crashLines int,
) error {
	if len(plans) == 0 {
		return nil
	}

	lock, err := sessionlock.Acquire(layout.SessionLockPath())
	if err != nil {
		var held *sessionlock.HeldError
		if errors.As(err, &held) {
			return clierr.New(clierr.CodeConfigInvalid,
				i18n.T(msgid.CliUpSessionAlreadyRunning, held.Info.PID)).WithHint(i18n.T(msgid.CliUpHintStopSession, held.Info.PID))
		}
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpFailedToAcquireTheSession)).WithCause(err).WithHint(i18n.T(msgid.HintCheckDiskAccess))
	}
	defer func() { _ = lock.Release() }()

	ctx, stop := signal.NotifyContext(ctx, procsup.StopSignals...)
	defer stop()

	widest := 0
	for _, p := range plans {
		widest = max(widest, len(p.Service))
		for _, m := range p.Migrations {
			widest = max(widest, len(m.Name))
		}
	}
	// 启动任何一个之前：端口被别的程序占着时，进程要么绑定失败退出，要么就绪探测连上的是那个程序、
	// 报出一个假的"已监听"。所以先查，被占的一次列全，一个都不启动
	if err := checkLocalPorts(plans); err != nil {
		return err
	}

	sup, err := procsup.New(procsup.Options{Out: opts.Stdout, NameWidth: widest, TailLines: crashLines,
		GracePeriod: localGracePeriod(plans)})
	if err != nil {
		return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpFailedToStartTheLocal)).WithCause(err).WithHint(i18n.T(msgid.HintInternalBug))
	}
	defer sup.Shutdown()

	// 从这里往下，状态行一律走 sup.Printf，不直接用 opts.Printf：一旦有进程
	// 启动，procsup 自己的输出转发协程就在并发写 opts.Stdout 了，sup.Printf
	// 复用的是它内部同一把锁，直接写会跟它产生真实的数据竞争（下面的说明）。
	sup.Printf("\n%s\n", i18n.T(msgid.CliUpStartingLocalComponents, len(plans)))

starting:
	for _, p := range plans {
		for _, m := range p.Migrations {
			switch outcome, err := runLocalMigration(ctx, sup, m); outcome {
			case migrationFailed:
				return err
			case migrationCanceled:
				return renderCrashSummary(sup, sup.Exits(), crashLines)
			case migrationInterrupted:
				break starting
			}
		}
		proc, err := sup.Start(procsup.Spec{Name: p.Service, Argv: p.Command.Argv, Dir: p.Dir, Env: p.Env, Withhold: p.Withhold})
		if errors.Is(err, procsup.ErrStopped) {
			break // 已经有别的进程崩了，会话在收尾：别再启动新的
		}
		if err != nil {
			return clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpFailedToStartTheLocal)).
				WithDetail(i18n.T(msgid.LabelComponent), p.Service).WithCause(err).WithHint(i18n.T(msgid.CliUpHintCheckStartCommand))
		}

		// 组件的身份统一用 p.Service（版本化服务名）——procsup 给这个进程自己
		// 输出加的行前缀（Spec.Name 就是它）、容器组件在 reportStarted 里的
		// 汇报，用的都是这同一个名字，这里的状态行不该另起一套 ID@version。
		switch procsup.WaitListening(ctx, p.Port, proc.Done(), startupProbeTimeout) {
		case procsup.ProbeExited:
			// 进程自己没起来，是硬失败；也可能是别的进程崩了、这个是被叫停的——
			// 到 Run 返回之后统一从 Exits() 里看谁 Crashed()，这里不重复判断
		case procsup.ProbeTimedOut:
			sup.Printf("%s\n", i18n.T(msgid.CliUpWarningStillNotListeningOn, p.Service, p.Port))
		case procsup.ProbeCanceled:
			return renderCrashSummary(sup, sup.Exits(), crashLines)
		case procsup.ProbeListening:
			sup.Printf("%s\n", i18n.T(msgid.CliUpListeningOnPort, p.Service, p.Port))
		}
	}

	sup.Run(ctx)

	return renderCrashSummary(sup, sup.Exits(), crashLines)
}

type migrationOutcome int

const (
	migrationDone migrationOutcome = iota
	// migrationFailed：迁移非零退出，进程不启动，会话收尾（与容器的迁移失败挡住主服务同理）。
	migrationFailed
	// migrationCanceled：用户按了 Ctrl+C。
	migrationCanceled
	// migrationInterrupted：别的进程崩了，会话已经在收尾，迁移被叫停或根本没起。
	migrationInterrupted
)

// runLocalMigration 在启动进程之前跑一条从源码的迁移：组件目录里、那个组件的环境，输出带着
// <服务名>-migration 的前缀（与 compose 里迁移容器的名字一致），等它退出。
func runLocalMigration(ctx context.Context, sup *procsup.Supervisor, m localMigration) (migrationOutcome, error) {
	sup.Printf("%s\n", i18n.T(msgid.CliUpRunningLocalMigration, m.Name, strings.Join(m.Command.Argv, " ")))
	mig, err := sup.Start(procsup.Spec{Name: m.Name, Argv: m.Command.Argv, Dir: m.Command.Dir, Env: m.Env, Withhold: m.Withhold})
	if errors.Is(err, procsup.ErrStopped) {
		return migrationInterrupted, nil
	}
	if err != nil {
		return migrationFailed, clierr.New(clierr.CodeInternal, i18n.T(msgid.CliUpFailedToStartTheLocal)).
			WithDetail(i18n.T(msgid.LabelComponent), m.Name).WithCause(err).WithHint(i18n.T(msgid.CliUpHintCheckLocalMigrationCommand))
	}
	select {
	case <-mig.Done():
	case <-ctx.Done():
		return migrationCanceled, nil
	}
	exit := mig.Exit()
	switch {
	case exit.Stopped:
		return migrationInterrupted, nil
	case exit.Code != 0:
		// 非零退出在 procsup 看来是崩溃，它已经在收尾；等收尾完成，报错才是最后一屏
		sup.Shutdown()
		how := i18n.T(msgid.CliUpExitCode, exit.Code)
		if exit.Signal != "" {
			how = i18n.T(msgid.CliUpKilledBySignal, exit.Signal)
		}
		return migrationFailed, clierr.New(clierr.CodeMigrationFailed, i18n.T(msgid.EngineMigrationFailed)).
			WithDetail(i18n.T(msgid.LabelComponent), m.Ref.String()).
			WithDetail(i18n.T(msgid.LabelCommand), strings.Join(m.Command.Argv, " ")).
			WithDetail(i18n.T(msgid.LabelReason), how).
			WithHint(i18n.T(msgid.CliUpLocalMigrationBlocksProcess, m.Name), i18n.T(msgid.CliUpLocalMigrationFixAndRerun))
	}
	sup.Printf("%s\n", i18n.T(msgid.CliUpLocalMigrationDone, m.Name))
	return migrationDone, nil
}

// checkLocalPorts 确认本机进程要绑的端口都空着（procsup.CheckPortFree）；被占的全部列进一条 PORT_CONFLICT。
//
// 在引擎处理完容器之后才查：上一次 up 留下的容器要等引擎换掉才放开端口，提前查会误报。
// mode: debug 的端口不查——它本来就该被你在 IDE 里起的进程占着。
func checkLocalPorts(plans []localComponentPlan) error {
	var e *clierr.Error
	movable := false
	seen := map[int]bool{}
	for _, p := range plans {
		for _, port := range p.Ports {
			if seen[port.Port] {
				continue
			}
			seen[port.Port] = true
			err := procsup.CheckPortFree(port.Port)
			if err == nil {
				continue
			}
			if e == nil {
				e = clierr.New(clierr.CodePortConflict, i18n.T(msgid.CliUpLocalPortsTaken))
			}
			reason := err.Error()
			if errors.Is(err, procsup.ErrPortAnswering) {
				reason = i18n.T(msgid.CliUpPortAnswering)
			}
			e = e.WithDetail(i18n.T(msgid.CliUpLabelPort, port.Port), port.Of+": "+reason)
			movable = movable || port.Movable
		}
	}
	if e == nil {
		return nil
	}
	hints := []string{i18n.T(msgid.CliUpHintStopThePortHolder)}
	if movable {
		hints = append(hints, i18n.T(msgid.CliUpHintMoveWithLocalPort))
	}
	return e.WithHint(hints...)
}

// renderCrashSummary 在最后一屏只打印崩溃的那几个进程，不被收尾时其余进程
// 的输出冲走（Plan 2 的设计决定 1）。被叫停的、干净退出的都不打印。
//
// crashLines 是使用者原始传的 --crash-lines 值，跟 procsup.Exit.Tail 里
// 实际捕获了多少行是两回事：procsup.Options.TailLines 把 <= 0 当成"调用方
// 没配，给个够用的默认值"（它自己的 TestTailKeepsOnlyTheConfiguredNumberOfLines
// 已经锁死了这条约定，是它作为通用监管库的合理默认，不该为了这一个调用方改）；
// 而 --crash-lines 的帮助文本对使用者的承诺是"0 = 只打印崩溃信息，不带
// 输出行"——同一个 0，两层意思完全相反。这条 CLI 专属的承诺只能在 CLI 自己
// 这层兑现：crashLines <= 0 时，不管 procsup 内部实际捕获、塞进 Exit.Tail
// 的是默认的 20 行还是别的，这里都不打印任何一行（手动验证
// 时用真实进程试 --crash-lines 0 才发现两层语义对不上）。
//
// 用 sup.Printf 而不是 opts.Printf 是同一个理由：这个函数从 runLocalComponents
// 里调用时，procsup 的收尾（体面停掉其余进程）可能还在并发写 opts.Stdout。
func renderCrashSummary(sup *procsup.Supervisor, exits []procsup.Exit, crashLines int) error {
	var crashed []procsup.Exit
	for _, e := range exits {
		if e.Crashed() {
			crashed = append(crashed, e)
		}
	}
	if len(crashed) == 0 {
		return nil
	}

	sup.Printf("\n%s\n", i18n.T(msgid.CliUpTheFollowingLocalComponentsCrashed))
	for _, e := range crashed {
		how := i18n.T(msgid.CliUpExitCode, e.Code)
		if e.Signal != "" {
			how = i18n.T(msgid.CliUpKilledBySignal, e.Signal)
		}
		sup.Printf("   %s  %s  %s\n", e.Name, how, e.Duration.Round(time.Second))
		if crashLines <= 0 {
			continue
		}
		for _, line := range e.Tail {
			sup.Printf("      %s\n", line)
		}
	}
	return clierr.New(clierr.CodeEngineFailed, i18n.T(msgid.CliUpLocalComponentsCrashed, len(crashed))).WithHint(i18n.T(msgid.CliUpHintCrashSummary))
}
