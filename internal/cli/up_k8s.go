package cli

// 本文件是 deploy.target: k8s 那条路。
//
// 与 Docker 那条路共用前半段：读配置 → 升级检查 → 解析依赖 → 级联 → 注入。
// 从"生成什么文件"开始分岔，因为两边确实是两回事：
//
//	Docker  一份 compose.yaml，交给 docker compose
//	K8s     一整个目录的清单，按顺序 kubectl apply，迁移还要 CLI 自己串行等待

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
)

// k8sDirName 是 K8s 清单在 .brickkit/generated/ 下的子目录名。
const k8sDirName = "k8s"

// upK8s 生成 K8s 清单并交给集群。
func upK8s(ctx context.Context, opts *Options, flags upOptions, plan *upPlan) error {
	if err := pruneOtherTarget(plan.proj.Layout, true); err != nil {
		return err
	}
	dir := filepath.Join(plan.proj.Layout.GeneratedDir(), k8sDirName)
	if err := k8s.WriteFiles(dir, plan.k8s.Files); err != nil {
		return err
	}

	opts.Printf("%s\n", i18n.T(msgid.CliUpK8sGeneratedManifests, i18n.Count(msgid.CountManifests, len(plan.k8s.Files)), opts.display(dir)))
	opts.Printf("%s\n", i18n.T(msgid.CliUpK8sNamespace, plan.k8s.Namespace))
	renderNetworkPolicyNotice(opts, plan.k8s)
	// 与 Docker 侧同一条：在 --dry-run 的分岔之前说清会不会动数据库
	renderMigrations(opts, plan.migrations)

	if flags.dryRun {
		renderUpgradeSummary(opts, plan)
		opts.Printf("\n%s\n", i18n.T(msgid.CliUpK8sDryRunOnlyGeneratesManifests))
		opts.Printf("%s\n", i18n.T(msgid.CliUpK8sViewThemLsR, opts.display(dir)))
		logging.Info(i18n.T(msgid.LogK8sManifestsGenerated), "dir", dir, "files", len(plan.k8s.Files))
		return nil
	}

	eng, err := resolveEngineFor(opts, plan.proj)
	if err != nil {
		return err
	}
	return applyK8s(ctx, opts, eng, plan, dir, projectSelector(plan.proj))
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
// 既然测不出来，就必须说出来。从前这句话只写在设计文档里，
// 而打开这个开关的人多半是从示例里抄了个字段——他不会回去读文档。
// 于是完整的失败路径是：抄字段 → up 静默生成 → apply 成功 →
// get networkpolicy 看得见 → 以为收紧了，实际全通。
//
// **一个"打开了也可能完全没生效、而工具一个字不说"的安全功能，价值是负的**：
// 不做的时候大家知道自己没做。
//
// 每次都打印，不是只在出错时——
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
	opts.Printf("%s\n", i18n.T(msgid.CliUpK8sSeeSecurityDoc))
}

// projectSelector 是本项目全部生成物共有的标签选择器。
//
// **up 与 down 共用这一个判据，全仓库只此一处。** 三个用途：
//
//	up   K8s 侧按它比对集群实际资源，清理上一次留下的孤儿
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
// 组件下线。`--only` 已删（要收窄范围就改 mode），
// 生成物永远是完整的一份，这个分支也就没有了。
func projectSelector(proj *project.Project) string {
	return k8s.LabelProject + "=" + proj.Decl.Project
}

// applyK8s 把清单交给集群，然后如实汇报。
func applyK8s(
	ctx context.Context, opts *Options, eng engine.Engine, plan *upPlan, dir, pruneSelector string,
) error {
	opts.Printf("\n%s\n", i18n.T(msgid.CliUpK8sDeployingToKubernetesNamespace, plan.k8s.Namespace))
	if len(plan.k8s.MigrationGroups) > 0 {
		// 先清旧 Job 再 apply，然后等它跑完
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
// 前者把清单交给集群，后两者在本机起容器。选错的后果真撞到过一次——
// 一个 target: k8s 的项目被按 Docker 处理，文件生成了、命令也成功了，
// 只是整个项目跑在了错误的编排器上。
//
// target: podman 走的是同一个 Compose 结构体，只是 bin 换成了 podman
// （engine.NewPodman）——这条路径**显式**来自配置，跟"没有配置、只是环境里
// 只装了 Podman"（engine.Detect 的 podmanNotEnabled 分支）是两回事，两者
// 不能混在一起判：前者是使用者自己选的，后者是平台替他猜的，猜出来的选择
// 不该被当成配置里选出来的。
func resolveEngineFor(opts *Options, proj *project.Project) (engine.Engine, error) {
	if proj != nil && proj.Deploy.Target == deployfile.TargetK8s {
		if opts.Engine != nil {
			return opts.Engine, nil
		}
		return engine.NewKubectl(), nil
	}
	if proj != nil && proj.Deploy.Target == deployfile.TargetPodman {
		if opts.Engine != nil {
			return opts.Engine, nil
		}
		return engine.NewPodman(), nil
	}
	return resolveEngine(opts)
}

// renderPruned 如实汇报清理掉了哪些孤儿资源。
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
