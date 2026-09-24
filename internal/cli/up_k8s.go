package cli

// 本文件是 deploy.target: k8s 那条路（005 §5）。
//
// 与 Docker 那条路共用前半段：读配置 → 升级检查 → 解析依赖 → 级联 → 注入。
// 从"生成什么文件"开始分岔，因为两边确实是两回事：
//
//	Docker  一份 docker-compose.yaml，交给 docker compose
//	K8s     一整个目录的清单，按顺序 kubectl apply，迁移还要 CLI 自己串行等待

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/msgid"
)

// k8sDirName 是 K8s 清单在 .brickkit/generated/ 下的子目录名。
const k8sDirName = "k8s"

// upK8s 生成 K8s 清单并交给集群。
func upK8s(ctx context.Context, opts *Options, flags upOptions, plan *upPlan) error {
	dir := filepath.Join(plan.layout.GeneratedDir(), k8sDirName)
	if err := k8s.WriteFiles(dir, plan.k8s.Files); err != nil {
		return err
	}

	opts.Printf("%s\n", i18n.T(msgid.CliUpK8sGeneratedManifests, i18n.Count(msgid.CountManifests, len(plan.k8s.Files)), displayPath(opts.WorkDir, dir)))
	opts.Printf("%s\n", i18n.T(msgid.CliUpK8sNamespace, plan.k8s.Namespace))
	renderResourceRequirements(opts, plan.k8s.Resources)
	renderNetworkPolicyNotice(opts, plan.k8s)
	// 与 Docker 侧同一条：在 --dry-run 的分岔之前说清会不会动数据库
	renderMigrations(opts, plan.migrations)

	if flags.dryRun {
		renderUpgradeSummary(opts, plan)
		opts.Printf("\n%s\n", i18n.T(msgid.CliUpK8sDryRunOnlyGeneratesManifests))
		opts.Printf("%s\n", i18n.T(msgid.CliUpK8sViewThemLsR, displayPath(opts.WorkDir, dir)))
		logging.Info(i18n.T(msgid.LogK8sManifestsGenerated), "dir", dir, "files", len(plan.k8s.Files))
		return nil
	}

	eng, err := resolveEngineFor(opts, plan.cfg)
	if err != nil {
		return err
	}
	return applyK8s(ctx, opts, eng, plan, dir, projectSelector(plan.cfg))
}

// renderNetworkPolicyNotice 提醒"策略生成了，但生不生效取决于集群的 CNI"。
//
// # 为什么必须每次都打印
//
// NetworkPolicy 在不支持执行的集群上是**无声失效**的：`kubectl apply` 成功、
// `kubectl get networkpolicy` 看得见、流量完全不受限制、没有任何报错。
// 所有现象都指向"策略生效了"，而实际一条也没生效——minikube / kind 的
// **默认** CNI 就属于这一类。
//
// 而 CLI **测不出来**：K8s 没有任何 API 能回答"本集群是否执行 NetworkPolicy"，
// 靠 grep 去猜会在托管集群（CNI 跑在控制面、用户看不见）上误报"不支持"，
// 把本来正确的部署拦下来。
//
// 既然测不出来，就必须说出来。从前这句话只写在 005 §5.13.0 与 003 §3.2 里，
// 而打开这个开关的人多半是从附录 D 抄了个字段——他不会回去读那两节。
// 于是完整的失败路径是：抄字段 → up 静默生成 → apply 成功 →
// get networkpolicy 看得见 → 以为收紧了，实际全通。
//
// **一个"打开了也可能完全没生效、而工具一个字不说"的安全功能，价值是负的**：
// 不做的时候大家知道自己没做。
//
// 与 renderResourceRequirements 同一个道理：每次都打印，不是只在出错时——
// 换个集群部署就换了一次前提，而这件事没有别的地方会提醒他。
func renderNetworkPolicyNotice(opts *Options, result *k8s.Result) {
	n := 0
	for _, ref := range result.Desired {
		if strings.HasPrefix(ref, "networkpolicy/") {
			n++
		}
	}
	if n == 0 {
		return
	}

	opts.Printf("\n%s\n", i18n.TN(msgid.CliUpK8sGeneratedNetworkpolicyManifestsDeployNetworkpolicy, n, n))
	opts.Printf("%s\n", i18n.T(msgid.CliUpK8sTheyOnlyTakeEffectWhen))
	opts.Printf("%s\n", i18n.T(msgid.CliUpK8sKubectlGetNetworkpolicyShowsThem))
	opts.Printf("%s\n", i18n.T(msgid.CliUpK8sTheDefaultCniOfMinikube))
	opts.Printf("%s\n", i18n.T(msgid.CliUpK8sThePlatformCanTDetect))
	opts.Printf("%s\n", i18n.T(msgid.CliUpK8sSeeDocsEnGuideNetwork))
}

// projectSelector 是本项目全部生成物共有的标签选择器。
//
// **up 与 down 共用这一个判据，全仓库只此一处。** 三个用途：
//
//	up   K8s 侧按它比对集群实际资源，清理上一次留下的孤儿（P38）
//	up   Docker 侧只用"空 / 非空"决定带不带 `--remove-orphans`
//	down 命名空间是运维建的那条路上，按它逐类删自己的资源
//
// ⚠️ **标签值是项目名，不是命名空间。** 引擎侧的 Project 在 K8s 下是命名空间，
// 写了 `deploy.namespace` 时与项目名毫不相干。`down` 从前在引擎里自己拼
// `LabelProject + "=" + req.Project`，于是那条路上一个资源都匹配不到——
// 八条 delete 全部命中 0 个对象、退出码 0，而 CLI 报"✅ 已停止全部组件"。
// 根因就是这个选择器被算了两遍而只有一遍算对，所以现在只留这一个出口。
//
// 这里曾经有一个"`--only` 时返回空串不清理"的分支：那时生成的部署文件只含
// 被点名的子集，其余组件全部落进清理的射程，一条 `up --only` 就会把正在服务的
// 组件下线。`--only` 已删（003 §4.3：要收窄范围就改 enabled），
// 生成物永远是完整的一份，这个分支也就没有了。
func projectSelector(cfg *config.Config) string {
	return k8s.LabelProject + "=" + cfg.Project
}

// applyK8s 把清单交给集群，然后如实汇报。
func applyK8s(
	ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan, dir, pruneSelector string,
) error {
	opts.Printf("\n%s\n", i18n.T(msgid.CliUpK8sDeployingToKubernetesNamespace, plan.k8s.Namespace))
	if len(plan.k8s.MigrationGroups) > 0 {
		// 16.14：先清旧 Job 再 apply，然后等它跑完
		opts.Printf("%s\n", i18n.T(msgid.CliUpK8sDatabaseMigrationsRunFirstThe))
	}

	var pruned []string
	if err := eng.Up(ctx, engine.UpRequest{
		File:            dir,
		Project:         plan.k8s.Namespace,
		Context:         plan.kubeContext,
		Services:        plan.services,
		MigrationGroups: plan.k8s.MigrationGroups,
		Desired:         plan.k8s.Desired,
		PruneSelector:   pruneSelector,
		OnPrune:         func(resource string) { pruned = append(pruned, resource) },
	}); err != nil {
		return engineFailure(i18n.T(msgid.CliUpK8sDeploy), err)
	}
	renderPruned(opts, pruned)

	statuses, err := eng.Status(ctx, plan.k8s.Namespace)
	if err != nil {
		// 部署是部署了，只是问不到状态：不该因此判定失败
		opts.Printf("%s\n", i18n.T(msgid.CliUpK8sTheClusterStateCouldNot, clierr.As(err).Message))
		opts.Printf("%s\n", i18n.T(msgid.CliUpK8sCheckAgainWithBrickkitStatus))
		return nil
	}
	return reportStarted(opts, plan, statuses)
}

// resolveEngineFor 按部署目标选引擎。
//
// K8s 与 Docker/Podman 不是"同一类引擎的两个牌子"，而是两种部署目标：
// 前者把清单交给集群，后两者在本机起容器。选错的后果在 Step 16 之前撞到过一次——
// 一个 target: k8s 的项目被按 Docker 处理，文件生成了、命令也成功了，
// 只是整个项目跑在了错误的编排器上。
//
// target: podman 走的是同一个 Compose 结构体，只是 bin 换成了 podman
// （engine.NewPodman）——这条路径**显式**来自配置，跟"没有配置、只是环境里
// 只装了 Podman"（engine.Detect 的 podmanNotEnabled 分支）是两回事，两者
// 不能混在一起判：前者是使用者自己选的，后者是平台替他猜的，猜出来的选择
// 不该被当成配置里选出来的。
func resolveEngineFor(opts *Options, cfg *config.Config) (engine.Engine, error) {
	if cfg != nil && cfg.Deploy.Target == config.TargetK8s {
		if opts.Engine != nil {
			return opts.Engine, nil
		}
		return engine.NewKubectl(), nil
	}
	if cfg != nil && cfg.Deploy.Target == config.TargetPodman {
		if opts.Engine != nil {
			return opts.Engine, nil
		}
		return engine.NewPodman(), nil
	}
	return resolveEngine(opts)
}

// renderPruned 如实汇报清理掉了哪些孤儿资源（P38）。
//
// 悄悄删东西不可接受：集群里少了什么，使用者得知道——
// 尤其是他其实误删了 brickkit.yaml 里的一行、本意并非下线那个组件的时候，
// 这几行输出是他唯一的线索。
func renderPruned(opts *Options, pruned []string) {
	if len(pruned) == 0 {
		return
	}

	opts.Printf("\n%s\n", i18n.T(msgid.CliUpK8sCleanedUpLeftoversFromAn, i18n.Count(msgid.CountItems, len(pruned))))
	for _, resource := range pruned {
		opts.Printf("   - %s\n", resource)
	}
	opts.Printf("%s\n", i18n.T(msgid.CliUpK8sTheyCarryThisProjectS))
	logging.Info(i18n.T(msgid.LogOrphansPruned), "count", len(pruned))
}

// ============================================================
// ${VAR} 的取值来源
// ============================================================

// envLookup 返回 K8s 生成时用的变量查找函数：先看进程环境，再看项目根的 .env。
//
// 顺序不能反：CI 里靠环境变量注入真实密码，本地靠 .env——
// 让 .env 盖掉环境变量，等于让开发机上的假密码顶掉 CI 传进来的真密码。
func envLookup(workDir string) func(string) (string, bool) {
	var dotenv map[string]string

	return func(name string) (string, bool) {
		if value, ok := os.LookupEnv(name); ok {
			return value, true
		}
		if dotenv == nil {
			dotenv = readDotEnv(filepath.Join(workDir, ".env"))
		}
		value, ok := dotenv[name]
		return value, ok
	}
}

// readDotEnv 读项目根目录的 .env。
//
// 文件不存在是正常情况（很多项目不用 .env），返回空表即可。
func readDotEnv(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	return parseDotEnv(string(data))
}

// parseDotEnv 解析 .env 文件内容，规则贴着 docker compose 自己的解释来——
// 这个文件同时被 `docker compose` 读，两边对同一个文件解释不一致才是真正的
// 问题所在（brickKit 反馈：local-debug.*.env 序列化多行值和特殊字符会截断
// 或解析错误——v0.4.4 复核结果指出，这里从前逐行按 `=` 切、只 Trim 掉值两端
// 各一个引号字符，一个跨多个物理行的双引号/单引号值会在第一行就被切断，
// PEM 私钥这类值只剩第一行）。
//
// 下面每一条规则都是拿真实 `docker compose config` 跑出来对照过的
// （见 up_k8s_test.go），不是凭印象猜的写法：
//
//   - 空行、trim 后以 `#` 开头的整行，当注释跳过
//   - 支持 `export KEY=value` 的 export 前缀
//   - 双引号里的值支持 `\\` `\"` `\n` `\r` `\t` 转义，且可以跨多个物理行——
//     一份跨二十几行的 PEM 私钥，只要整份用双引号包住，就能被当成一个值读全
//   - 单引号里的值原样保留、不处理任何转义，同样可以跨多个物理行
//   - 不加引号的值：等号两侧的空白被去掉；空白紧跟 `#` 才算行内注释
//     （`C=val#hash` 里的 `#` 前面不是空白，不算注释，原样保留）；
//     引号闭合之后同一行剩下的内容原样丢弃（`D="quoted"junk` 取到的是 `quoted`）
func parseDotEnv(content string) map[string]string {
	out := map[string]string{}

	// 统一成 \n：后面所有下标运算只处理一种换行，回车留给双引号转义
	// （\r）自己产生，不会和"这是不是行尾"这件事混在一起。
	runes := []rune(strings.ReplaceAll(content, "\r\n", "\n"))
	n := len(runes)

	for i := 0; i < n; {
		// 跳过这一行开头的水平空白，判断是不是空行/整行注释
		for i < n && (runes[i] == ' ' || runes[i] == '\t') {
			i++
		}
		if i >= n || runes[i] == '\n' {
			i = skipToNextLine(runes, i)
			continue
		}
		if runes[i] == '#' {
			i = skipToNextLine(runes, i)
			continue
		}

		key, value, ok := parseDotEnvAssignment(runes, &i)
		if ok {
			out[key] = value
		}
		i = skipToNextLine(runes, i)
	}
	return out
}

// parseDotEnvAssignment 解析当前行里的一条 `[export] KEY=value`。
//
// i 进来时指向这一行第一个非空白字符，出去时停在值结束的位置——调用方
// 只管跳到下一行，不关心值到底是被引号闭合、还是不加引号一路读到换行。
func parseDotEnvAssignment(runes []rune, i *int) (key, value string, ok bool) {
	n := len(runes)

	start := *i
	for *i < n && runes[*i] != '\n' && runes[*i] != '=' {
		*i++
	}
	if *i >= n || runes[*i] != '=' {
		return "", "", false // 这一行没有 `=`，不是一条赋值
	}
	key = strings.TrimSpace(string(runes[start:*i]))
	key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
	*i++ // 跳过 `=`

	for *i < n && (runes[*i] == ' ' || runes[*i] == '\t') {
		*i++
	}

	switch {
	case *i < n && runes[*i] == '"':
		value = scanDotEnvQuoted(runes, i, '"', true)
	case *i < n && runes[*i] == '\'':
		value = scanDotEnvQuoted(runes, i, '\'', false)
	default:
		value = scanDotEnvUnquoted(runes, i)
	}
	return key, value, true
}

// scanDotEnvQuoted 从开引号处（*i 指向它）读到匹配的闭引号，返回引号内的内容。
//
// 跨物理行是这个函数存在的全部意义：闭引号找不到就一直往后读，读到的
// 换行符原样计入内容——一份多行 PEM 私钥用一对引号包住，读出来就是
// 包含真换行的完整值，不会在第一个 `\n` 那里假装"这一条赋值结束了"。
func scanDotEnvQuoted(runes []rune, i *int, quote rune, escaped bool) string {
	n := len(runes)
	*i++ // 跳过开引号

	var b strings.Builder
	for *i < n {
		c := runes[*i]
		if c == quote {
			*i++ // 跳过闭引号；闭引号之后同一行剩下的内容留给 skipToNextLine 原样丢弃
			return b.String()
		}
		if escaped && c == '\\' && *i+1 < n {
			switch runes[*i+1] {
			case 'n':
				b.WriteRune('\n')
				*i += 2
				continue
			case 'r':
				b.WriteRune('\r')
				*i += 2
				continue
			case 't':
				b.WriteRune('\t')
				*i += 2
				continue
			case '"':
				b.WriteRune('"')
				*i += 2
				continue
			case '\\':
				b.WriteRune('\\')
				*i += 2
				continue
			}
			// 不认识的转义：反斜杠原样保留，不悄悄吞掉数据
		}
		b.WriteRune(c)
		*i++
	}
	// 一直没找到闭引号：文件写错了，把已经读到的都算数，总比整条丢掉强
	return b.String()
}

// scanDotEnvUnquoted 读一个不加引号的值：读到行尾，" #" 触发行内注释截断，
// 首尾空白去掉。
func scanDotEnvUnquoted(runes []rune, i *int) string {
	n := len(runes)
	start := *i
	end := start
	for *i < n && runes[*i] != '\n' {
		if runes[*i] == '#' && *i > start &&
			(runes[*i-1] == ' ' || runes[*i-1] == '\t') {
			break
		}
		*i++
		end = *i
	}
	return strings.TrimSpace(string(runes[start:end]))
}

// skipToNextLine 把 i 挪到下一行的开头（跳过当前的 `\n`），已经在文件末尾就原地不动。
func skipToNextLine(runes []rune, i int) int {
	n := len(runes)
	for i < n && runes[i] != '\n' {
		i++
	}
	if i < n {
		i++ // 跳过 \n 本身
	}
	return i
}
