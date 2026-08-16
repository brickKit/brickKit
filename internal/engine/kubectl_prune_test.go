package engine

// 本文件是 P38「K8s 升级后旧版本变成孤儿」的行为测试。
//
// 缺陷本身：把版本号从 1.0.0 改成 2.0.0 再 `brickkit up`，新版本起来了，
// **旧版本的 Deployment / Service / NetworkPolicy / SA 全部继续运行**。
// `kubectl apply` 默认不删目录里没有的资源，而 K8s 这条路没有 compose 的
// `--remove-orphans` 兜底。后果是 `status` 看不见、`down` 也删不掉的永久泄漏。
//
// 已在 minikube 上真跑复现过（demo/hello 1.0.0 → 2.0.0）。
//
// 这里断言的是**引擎发出了哪些 kubectl 命令**——它是"把决定翻译成命令"的那一层。
// 真集群上的效果由 P38 的真跑验证覆盖。

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pruneRequest 造一次"部署 2.0.0"的请求，并让集群假装还留着 1.0.0。
func pruneRequest() UpRequest {
	return UpRequest{
		File:          "/p/.brickkit/generated/k8s",
		Project:       "brickkit-my-erp",
		Services:      []string{"demo-hello-2-0-0"},
		PruneSelector: "brickkit.io/project=my-erp",
	}
}

// clusterHas 让 `get ... -o name` 返回集群里现有的资源。
func clusterHas(rec *recorder, names ...string) {
	rec.output["-o name"] = strings.Join(names, "\n") + "\n"
}

// deleteCommand 返回那条 delete 命令；没有则返回空串。
func deleteCommand(rec *recorder) string {
	for _, command := range rec.commands() {
		// 迁移 Job 的清理也是 delete，但它带 job/ 前缀且在 apply 之前，
		// 这里只找清理孤儿的那条（它删的是带 kind 前缀的全名）
		if strings.Contains(command, "delete") && !strings.Contains(command, "delete job/") {
			return command
		}
	}
	return ""
}

// ============================================================
// 核心行为
// ============================================================

// 上一个版本留在集群里的资源要被删掉。
//
// 这是 P38 的核心：不删的话它会一直跑下去，而且 `down` 也带不走它。
func TestKubectlUpPrunesOrphanedOldVersion(t *testing.T) {
	rec := newRecorder()
	clusterHas(rec,
		"deployment.apps/demo-hello-1-0-0", // ← 上一个版本，已经没人要了
		"service/demo-hello-1-0-0",
		"deployment.apps/demo-hello-2-0-0", // ← 本次部署的
		"service/demo-hello-2-0-0",
	)

	require.NoError(t, kubectlWith(rec).Up(context.Background(), pruneRequest()))

	command := deleteCommand(rec)
	require.NotEmpty(t, command, "P38：必须发出清理命令，实际命令：%v", rec.commands())

	assert.Contains(t, command, "deployment.apps/demo-hello-1-0-0", "P38：旧版本要删")
	assert.Contains(t, command, "service/demo-hello-1-0-0", "P38：旧版本的 Service 也要删")
}

// 本次部署的资源**绝不能**被删。
//
// 这条比上一条更要紧：清理逻辑写反的后果是把正在服务的东西删掉，
// 那比留个孤儿严重得多。
func TestKubectlUpNeverPrunesCurrentVersion(t *testing.T) {
	rec := newRecorder()
	clusterHas(rec,
		"deployment.apps/demo-hello-1-0-0",
		"deployment.apps/demo-hello-2-0-0",
		"service/demo-hello-2-0-0",
	)

	require.NoError(t, kubectlWith(rec).Up(context.Background(), pruneRequest()))

	command := deleteCommand(rec)
	assert.NotContains(t, command, "demo-hello-2-0-0",
		"P38：本次部署的资源被删了——这比孤儿严重得多：%s", command)
}

// 迁移 Job 属于本次部署，不是孤儿。
//
// 它的名字是 <服务名>-migration，与服务名对不上；只按 Services 判断的话
// 会把刚跑完的迁移 Job 当成孤儿删掉。删掉本身不致命（它已经跑完了），
// 但会让 `kubectl logs job/...` 查不到迁移到底做了什么。
func TestKubectlUpKeepsMigrationJobs(t *testing.T) {
	rec := newRecorder()
	clusterHas(rec,
		"job.batch/demo-hello-2-0-0-migration",
		"deployment.apps/demo-hello-2-0-0",
	)

	req := pruneRequest()
	req.MigrationJobs = []string{"demo-hello-2-0-0-migration"}
	require.NoError(t, kubectlWith(rec).Up(context.Background(), req))

	assert.NotContains(t, deleteCommand(rec), "demo-hello-2-0-0-migration",
		"P38：本次的迁移 Job 不是孤儿")
}

// ============================================================
// 安全边界
// ============================================================

// `--only` 时**绝不清理**。
//
// 这是整个修复里最危险的一处：`--only` 下 Services 只是子集，
// 照着它清理会把没点名的组件全部删掉——那比 P38 本身危险得多。
// 命令层用"不给选择器"表达这件事。
func TestKubectlUpDoesNotPruneWhenSelectorEmpty(t *testing.T) {
	rec := newRecorder()
	clusterHas(rec, "deployment.apps/demo-hello-1-0-0")

	req := pruneRequest()
	req.PruneSelector = "" // ← --only 时命令层不给选择器
	require.NoError(t, kubectlWith(rec).Up(context.Background(), req))

	assert.Empty(t, deleteCommand(rec),
		"P38：没有选择器时一条清理命令都不该发：%v", rec.commands())
	for _, command := range rec.commands() {
		assert.NotContains(t, command, "-o name",
			"P38：连查询都不该发——查了就说明逻辑还在往下走")
	}
}

// 查询必须带项目标签。
//
// 不带的话会扫到同一个命名空间里别人的东西——很多组织把多个项目
// 放在同一个命名空间里，删错的代价是别人的服务下线。
func TestKubectlUpPruneIsScopedByProjectLabel(t *testing.T) {
	rec := newRecorder()
	clusterHas(rec, "deployment.apps/demo-hello-1-0-0")

	require.NoError(t, kubectlWith(rec).Up(context.Background(), pruneRequest()))

	var query string
	for _, command := range rec.commands() {
		if strings.Contains(command, "-o name") {
			query = command
		}
	}
	require.NotEmpty(t, query, "P38：应该先查一次集群里有什么")

	assert.Contains(t, query, "-l brickkit.io/project=my-erp", "P38：必须按项目标签收窄")
	assert.Contains(t, query, "-n brickkit-my-erp", "P38：必须限定命名空间")
}

// 命名空间永远不在清理范围内。
//
// namespace.yaml 上也有 brickkit.io/project 标签，一旦被算进去，
// 一次普通的升级就会把整个命名空间连同里面所有东西删掉。
func TestKubectlUpNeverPrunesNamespace(t *testing.T) {
	rec := newRecorder()
	clusterHas(rec, "deployment.apps/demo-hello-1-0-0")

	require.NoError(t, kubectlWith(rec).Up(context.Background(), pruneRequest()))

	for _, command := range rec.commands() {
		if strings.Contains(command, "-o name") || strings.Contains(command, "delete") {
			assert.NotContains(t, command, "namespace",
				"P38：命名空间绝不能进清理范围：%s", command)
		}
	}
}

// 集群里没有多余资源时，不发 delete。
//
// 每次 up 都发一条空的 delete 不会出错，但会在输出里留下噪音，
// 让人以为"每次升级都清理了点什么"。
func TestKubectlUpSkipsDeleteWhenNothingOrphaned(t *testing.T) {
	rec := newRecorder()
	clusterHas(rec, "deployment.apps/demo-hello-2-0-0", "service/demo-hello-2-0-0")

	require.NoError(t, kubectlWith(rec).Up(context.Background(), pruneRequest()))

	assert.Empty(t, deleteCommand(rec), "P38：没有孤儿就不该发 delete")
}

// ============================================================
// 时机与汇报
// ============================================================

// 清理发生在**滚动更新完成之后**。
//
// 顺序反了的话，新版本还没就绪就把旧版本删了，中间有一段没有任何实例
// 能服务请求——升级从"无感知"变成"短暂 502"。
func TestKubectlUpPrunesAfterRollout(t *testing.T) {
	rec := newRecorder()
	clusterHas(rec, "deployment.apps/demo-hello-1-0-0")

	require.NoError(t, kubectlWith(rec).Up(context.Background(), pruneRequest()))

	commands := rec.commands()
	rollout := indexOfCommand(commands, "rollout status")
	prune := indexOfCommand(commands, "delete deployment.apps/demo-hello-1-0-0")

	require.NotEqual(t, -1, rollout, "应该等过滚动更新")
	require.NotEqual(t, -1, prune, "应该清理过")
	assert.Less(t, rollout, prune,
		"P38：必须等新版本就绪再删旧的，否则升级过程中会有一段谁都服务不了")
}

// 清理掉什么要能回传给命令层。
//
// 悄悄删东西是不可接受的：使用者得知道集群里少了什么，
// 尤其在他其实是误删了配置、本意并非下线那个组件的时候。
func TestKubectlUpReportsPrunedResources(t *testing.T) {
	rec := newRecorder()
	clusterHas(rec, "deployment.apps/demo-hello-1-0-0", "service/demo-hello-1-0-0")

	var pruned []string
	req := pruneRequest()
	req.OnPrune = func(resource string) { pruned = append(pruned, resource) }

	require.NoError(t, kubectlWith(rec).Up(context.Background(), req))

	assert.ElementsMatch(t,
		[]string{"deployment.apps/demo-hello-1-0-0", "service/demo-hello-1-0-0"}, pruned,
		"P38：删了什么要如实回传，命令层才能告诉使用者")
}

// 查询失败不能让整次部署失败。
//
// 部署已经成功了，清理只是收尾。因为查不到集群状态就把一次成功的 up
// 判成失败，会让人以为服务没起来而去做多余的回滚。
func TestKubectlUpSurvivesPruneQueryFailure(t *testing.T) {
	rec := newRecorder()
	rec.fail["-o name"] = assertAnError{}

	err := kubectlWith(rec).Up(context.Background(), pruneRequest())

	assert.NoError(t, err, "P38：清理查询失败不该让整次部署失败")
}

type assertAnError struct{}

func (assertAnError) Error() string { return "集群暂时查不到" }
