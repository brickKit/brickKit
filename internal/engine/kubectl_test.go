package engine

// 本文件是 kubectl 引擎的测试：命令序列怎么拼、输出怎么解析。
// 覆盖开发计划 16.14（执行前清理旧 Job）。
//
// 本机没有 kubectl、也没有集群（见开发进度 L7），因此这里只验证
// "把决定翻译成 kubectl 命令"这一段；真集群验证登记为 P25。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

func kubectlWith(rec *recorder) *Kubectl {
	k := NewKubectl()
	k.runner = rec.run
	// 夹具里的目录不真实存在；"目录不存在就跳过"另有专门的用例
	k.exists = func(string) bool { return true }
	return k
}

// commands 把记录到的调用还原成一行行命令，便于断言顺序。
func (r *recorder) commands() []string {
	out := make([]string, 0, len(r.calls))
	for _, call := range r.calls {
		out = append(out, strings.Join(call, " "))
	}
	return out
}

// indexOfCommand 返回第一条包含 sub 的命令的位置，没有则 -1。
func indexOfCommand(commands []string, sub string) int {
	for i, command := range commands {
		if strings.Contains(command, sub) {
			return i
		}
	}
	return -1
}

// ============================================================
// 启动顺序（005 §5.7）
// ============================================================

func TestKubectlUpAppliesInOrder(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Up(context.Background(), UpRequest{
		File: "/p/.brickkit/generated/k8s", Project: "brickkit-my-erp",
	}))

	commands := rec.commands()
	namespace := indexOfCommand(commands, "namespace.yaml")
	secrets := indexOfCommand(commands, "secrets")
	deployments := indexOfCommand(commands, "deployments")
	services := indexOfCommand(commands, "services")

	require.NotEqual(t, -1, namespace, "必须先建命名空间：%v", commands)
	assert.Less(t, namespace, secrets, "命名空间要在 Secret 之前——不然 Secret 无处可放")
	assert.Less(t, secrets, deployments, "Secret 要在 Deployment 之前——不然 Pod 起来就找不到密码")
	assert.Less(t, deployments, services)
}

func TestKubectlUpUsesNamespace(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Up(context.Background(), UpRequest{
		File: "/p/k8s", Project: "brickkit-my-erp",
	}))

	for _, command := range rec.commands() {
		if strings.Contains(command, "namespace.yaml") {
			// 建命名空间这条本身不能带 -n：那个命名空间还不存在
			assert.NotContains(t, command, "-n brickkit-my-erp")
			continue
		}
		assert.Contains(t, command, "-n brickkit-my-erp",
			"其余每条命令都必须指定命名空间，否则会落到 default 里：%s", command)
	}
}

// 16.14：迁移前先清掉可能残留的旧 Job。
//
// Job 的 spec 是不可变的：上一次失败留下的同名 Job 还在时，
// 直接 apply 会以 "field is immutable" 失败，而使用者只是改了迁移脚本重跑。
func TestKubectlUpDeletesStaleJobsBeforeApply(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Up(context.Background(), UpRequest{
		File: "/p/k8s", Project: "brickkit-my-erp",
		MigrationGroups: [][]string{{"people-basic-1-0-0-migration"}},
	}))

	commands := rec.commands()
	del := indexOfCommand(commands, "delete job/people-basic-1-0-0-migration")
	apply := indexOfCommand(commands, "migrations")

	require.NotEqual(t, -1, del, "16.14 必须先清理旧 Job：%v", commands)
	assert.Contains(t, commands[del], "--ignore-not-found", "16.14 没有旧 Job 时不能报错")
	assert.Less(t, del, apply, "16.14 清理要在 apply 之前")
}

// 迁移必须跑完才能起主服务：K8s 没有 compose 的 depends_on。
func TestKubectlUpWaitsForMigrationBeforeDeployments(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Up(context.Background(), UpRequest{
		File: "/p/k8s", Project: "brickkit-my-erp",
		MigrationGroups: [][]string{{"people-basic-1-0-0-migration"}},
	}))

	commands := rec.commands()
	wait := indexOfCommand(commands, "wait --for=condition=complete")
	apply := indexOfCommand(commands, "apply -f /p/k8s/migrations")
	deployments := indexOfCommand(commands, "deployments")

	require.NotEqual(t, -1, wait, "必须等迁移完成：%v", commands)
	assert.Less(t, apply, wait, "先 apply 再等")
	assert.Less(t, wait, deployments, "迁移没跑完就起主服务，等于让业务代码撞上旧表结构")
	assert.Contains(t, commands[wait], "job/people-basic-1-0-0-migration")
	assert.Contains(t, commands[wait], "--timeout", "不设超时的话，迁移卡住就会永远挂在这里")
}

// 没有迁移时不该出现任何 Job 相关的命令。
func TestKubectlUpWithoutMigrations(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Up(context.Background(), UpRequest{
		File: "/p/k8s", Project: "brickkit-my-erp",
	}))

	for _, command := range rec.commands() {
		assert.NotContains(t, command, "job/")
		assert.NotContains(t, command, "migrations")
	}
}

// 没有 Ingress 的项目不会生成 ingress/ 目录，这时不能去 apply 它。
//
// `kubectl apply -f 一个不存在的目录` 会直接失败——一个"没有对外组件"的
// 正常项目会因此起不来。
func TestKubectlSkipsMissingDirectories(t *testing.T) {
	rec := newRecorder()
	k := kubectlWith(rec)
	k.exists = func(dir string) bool { return !strings.HasSuffix(dir, "ingress") }

	require.NoError(t, k.Up(context.Background(), UpRequest{
		File: "/p/k8s", Project: "brickkit-my-erp",
	}))

	assert.Equal(t, -1, indexOfCommand(rec.commands(), "ingress"),
		"不存在的目录不能出现在命令里：%v", rec.commands())
	assert.NotEqual(t, -1, indexOfCommand(rec.commands(), "deployments"), "别的照常")
}

// 迁移失败要说清楚是哪个 Job，并告诉人去哪看日志。
func TestKubectlMigrationFailure(t *testing.T) {
	rec := newRecorder()
	rec.fail["wait --for=condition=complete"] = errors.New("timed out")
	rec.output["wait --for=condition=complete"] = "error: timed out waiting for the condition"

	err := kubectlWith(rec).Up(context.Background(), UpRequest{
		File: "/p/k8s", Project: "brickkit-my-erp",
		MigrationGroups: [][]string{{"people-basic-1-0-0-migration"}},
	})

	require.Error(t, err)
	assert.Equal(t, clierr.CodeMigrationFailed, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "people-basic-1-0-0-migration")
	assert.Contains(t, err.Error(), "kubectl logs", "要给出看日志的命令")
}

// 起完之后要等副本真正就绪，否则紧接着查状态会得到一个假的失败结论
// （与 compose 那边的 --wait 是同一个道理）。
func TestKubectlUpWaitsForRollout(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Up(context.Background(), UpRequest{
		File: "/p/k8s", Project: "brickkit-my-erp",
		Services: []string{"people-basic-1-0-0"},
	}))

	commands := rec.commands()
	rollout := indexOfCommand(commands, "rollout status")

	require.NotEqual(t, -1, rollout, "必须等副本就绪：%v", commands)
	assert.Contains(t, commands[rollout], "deployment/people-basic-1-0-0")
	assert.Contains(t, commands[rollout], "--timeout")
	assert.Greater(t, rollout, indexOfCommand(commands, "deployments"), "先 apply 再等")
}

// ============================================================
// 停止
// ============================================================

// 命名空间是我们建的：直接删命名空间，里面的东西跟着走。
//
// 比逐类删干净，也不会漏掉将来新增、而 deleteKinds 忘了登记的资源类型。
func TestKubectlDownDeletesOwnNamespace(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Down(context.Background(), DownRequest{
		Project: "brickkit-my-erp", DeleteNamespace: true,
	}))

	command := rec.lastCall(t)

	assert.Contains(t, command, "delete namespace brickkit-my-erp")
	assert.Contains(t, command, "--ignore-not-found", "没起过的项目 down 一次不该报错")
}

// 命名空间不是我们建的时候，只删带本项目标签的资源，绝不碰命名空间。
//
// 那是别人的命名空间，里面多半还跑着别的东西——删掉等于把整个团队一起端了。
//
// ⚠️ 夹具刻意让**命名空间与项目名不同**（`deploy.namespace: team-a-prod` +
// `project: my-erp`）。这条用例从前两者都写 team-a-prod，于是
// `-l brickkit.io/project=team-a-prod` 看上去天经地义——而生成物上的标签值
// 是项目名，真集群上这个选择器一个资源都匹配不到：八条 delete 全部命中
// 0 个对象、退出码 0，CLI 报"✅ 已停止全部组件"。用例把 bug 锁成了预期行为。
func TestKubectlDownKeepsForeignNamespace(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Down(context.Background(), DownRequest{
		Project:  "team-a-prod",
		Selector: "brickkit.io/project=my-erp",
	}))

	for _, command := range rec.commands() {
		assert.NotContains(t, command, "delete namespace", "不能碰命名空间")
		assert.Contains(t, command, "-n team-a-prod", "-n 用的是命名空间")
		assert.Contains(t, command, "-l brickkit.io/project=my-erp",
			"而标签选择器用的是项目名——两者不是一回事")
		assert.NotContains(t, command, "brickkit.io/project=team-a-prod",
			"拿命名空间当标签值，真集群上一个资源都匹配不到")
	}
	assert.NotEqual(t, -1, indexOfCommand(rec.commands(), "delete deployment"),
		"但该删的还是要删")
}

// 选择器为空时宁可中止，也不能发出 `-l ""`。
//
// 走到逐类删这条路，恰恰说明命名空间是别人的。而 `kubectl delete deployment
// -l "" -n ns` 匹配的是**该命名空间里的全部** Deployment——少传一个字段
// 就把别的团队一起端了，代价无法挽回。
func TestKubectlDownRefusesEmptySelector(t *testing.T) {
	rec := newRecorder()

	err := kubectlWith(rec).Down(context.Background(), DownRequest{
		Project: "team-a-prod", DeleteNamespace: false,
	})

	require.Error(t, err, "没有选择器就不该删任何东西")
	assert.Empty(t, rec.commands(), "一条 kubectl 都不该发出去")
}

// down 一个字节都不读生成目录（005 §5.9.3）。
//
// 那份目录回答的是"这次打算部署什么"，而 `up --dry-run` 也会重写它。
// 拿它当"上次实际部署了什么"来删，少一个文件就漏删一个 Deployment，
// 而命令照样报成功——Docker 侧真跑出过这个结果，K8s 侧是同一个毛病。
//
// DownRequest 里已经没有 File 字段（那是结构性的保证），这里再守一道：
// 就算将来有人从别处把路径传进来，命令里也不该出现 -f。
func TestKubectlDownNeverReadsTheGeneratedDir(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Down(context.Background(), DownRequest{
		Project: "team-a-prod", Selector: "brickkit.io/project=my-erp",
	}))

	for _, command := range rec.commands() {
		assert.NotContains(t, command, "-f ", "不能用 -f 删")
		assert.NotContains(t, command, "-R", "更不能递归扫整个生成目录")
	}
}

// --context 要出现在每一条命令里。
//
// 只在执行前校验一次 current-context 是不够的：校验与执行之间有时间差，
// 使用者可能在另一个终端把 context 切走了。
func TestKubectlPassesContext(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Up(context.Background(), UpRequest{
		File: "/p/k8s", Project: "ns", Context: "prod-cluster",
		Services: []string{"people-basic-1-0-0"},
	}))

	for _, command := range rec.commands() {
		assert.Contains(t, command, "--context prod-cluster", command)
	}
}

// 命名空间由运维建时不生成 namespace.yaml，也就不能去 apply 它。
func TestKubectlSkipsMissingNamespaceManifest(t *testing.T) {
	rec := newRecorder()
	k := kubectlWith(rec)
	k.exists = func(path string) bool { return !strings.HasSuffix(path, "namespace.yaml") }

	require.NoError(t, k.Up(context.Background(), UpRequest{File: "/p/k8s", Project: "team-a"}))

	assert.Equal(t, -1, indexOfCommand(rec.commands(), "namespace.yaml"),
		"不存在的清单不能出现在命令里：%v", rec.commands())
}

func TestKubectlCurrentContext(t *testing.T) {
	rec := newRecorder()
	rec.output["config current-context"] = "prod-cluster\n"

	current, err := kubectlWith(rec).CurrentContext(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "prod-cluster", current, "结尾的换行要去掉")
}

// ============================================================
// 状态
// ============================================================

const deploymentsJSON = `{
  "items": [
    {
      "metadata": {"name": "people-basic-1-0-0"},
      "spec": {"replicas": 1, "template": {"spec": {"containers": [
        {"ports": [{"containerPort": 8080}, {"containerPort": 9090}]}
      ]}}},
      "status": {"replicas": 1, "readyReplicas": 1}
    },
    {
      "metadata": {"name": "department-tree-1-0-0"},
      "spec": {"replicas": 1, "template": {"spec": {"containers": [
        {"ports": [{"containerPort": 8080}]}
      ]}}},
      "status": {"replicas": 1}
    }
  ]
}`

func TestKubectlStatus(t *testing.T) {
	rec := newRecorder()
	rec.output["get deployments"] = deploymentsJSON

	statuses, err := kubectlWith(rec).Status(context.Background(), "brickkit-my-erp")

	require.NoError(t, err)
	require.Len(t, statuses, 2)

	byService := map[string]Status{}
	for _, s := range statuses {
		byService[s.Service] = s
	}

	ready := byService["people-basic-1-0-0"]
	assert.True(t, ready.Running(), "副本就绪就是在跑")
	assert.Equal(t, "healthy", ready.Health)
	assert.Equal(t, "8080/tcp, 9090/tcp", ready.Ports)

	// 副本没就绪的不能算"在跑"：这时候请求打过去是通不了的
	assert.False(t, byService["department-tree-1-0-0"].Running())
	assert.Equal(t, "starting", byService["department-tree-1-0-0"].Health)
}

func TestKubectlStatusOnEmptyNamespace(t *testing.T) {
	rec := newRecorder()
	rec.output["get deployments"] = `{"items": []}`

	statuses, err := kubectlWith(rec).Status(context.Background(), "brickkit-my-erp")

	require.NoError(t, err)
	assert.Empty(t, statuses)
}

// 命名空间根本不存在时不该报错：那只是"还没 up 过"。
func TestKubectlStatusOnMissingNamespace(t *testing.T) {
	rec := newRecorder()
	rec.fail["get deployments"] = errors.New("exit 1")
	rec.output["get deployments"] = `Error from server (NotFound): namespaces "brickkit-my-erp" not found`

	statuses, err := kubectlWith(rec).Status(context.Background(), "brickkit-my-erp")

	require.NoError(t, err)
	assert.Empty(t, statuses)
}

// ============================================================
// 其余
// ============================================================

// 镜像拉取权限由集群的 kubelet 决定，开发机上的 docker login 说明不了任何问题。
func TestKubectlCheckImageIsNoop(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).CheckImage(context.Background(), "registry.example.com/x:1"))
	assert.Empty(t, rec.calls, "不该为此执行任何命令")
}

func TestKubectlMissingBinary(t *testing.T) {
	rec := newRecorder()
	rec.fail["apply"] = errors.New(`exec: "kubectl": executable file not found in $PATH`)

	err := kubectlWith(rec).Up(context.Background(), UpRequest{File: "/p/k8s", Project: "ns"})

	require.Error(t, err)
	assert.Equal(t, clierr.CodeEngineMissing, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "kubectl")
}

func TestKubectlName(t *testing.T) {
	assert.Equal(t, K8s, NewKubectl().Name())
}

// 迁移卡住时，要把人指向 **events**，不是日志。
//
// 真集群上撞到的：命名空间打着 restricted 标签时，`kubectl apply` 那个 Job
// **成功**（Job 对象建出来了），但 Job 控制器创建 Pod 时被准入控制拒绝。
// 于是 Job 既不 Complete 也不 Failed，`kubectl wait` 静默挂满 10 分钟，
// 而**一条日志都没有**（Pod 根本没被创建），只有 Job 的 events 里写着原因。
func TestKubectlMigrationTimeoutPointsAtEvents(t *testing.T) {
	rec := newRecorder()
	rec.fail["wait --for=condition=complete"] = errors.New("timed out")
	rec.output["wait --for=condition=complete"] = "error: timed out waiting for the condition"

	err := kubectlWith(rec).Up(context.Background(), UpRequest{
		File: "/p/k8s", Project: "team-a",
		MigrationGroups: [][]string{{"people-basic-1-0-0-migration"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubectl describe job/people-basic-1-0-0-migration",
		"Pod 没被创建时没有日志可看，只能看 events")
	assert.Contains(t, err.Error(), "准入控制", "要点出这个最常见的原因")
}

// 同一个组件的多个版本，迁移 Job 必须一个跑完再下发下一个。
//
// 全部 apply 完再逐个 wait 是不够的：Job 一 apply 就开始跑，两个版本会同时
// 对同一个库、用同一个 component_id 跑那批重合的迁移，空库上必有一个撞主键
// 退出（002 §8.11、§8.10；分组理由见 k8s.Result.MigrationGroups）。
func TestKubectlRunsGroupedMigrationsInOrder(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Up(context.Background(), UpRequest{
		File: "/p/k8s", Project: "brickkit-my-erp",
		MigrationGroups: [][]string{{"demo-hello-1-0-0-migration", "demo-hello-2-0-0-migration"}},
	}))

	commands := rec.commands()
	applyFirst := indexOfCommand(commands, "apply -f /p/k8s/migrations/demo-hello-1-0-0-migration.yaml")
	waitFirst := indexOfCommand(commands, "wait --for=condition=complete --timeout="+migrationTimeout+" job/demo-hello-1-0-0-migration")
	applySecond := indexOfCommand(commands, "apply -f /p/k8s/migrations/demo-hello-2-0-0-migration.yaml")

	require.NotEqual(t, -1, applyFirst, "要逐个 apply：%v", commands)
	require.NotEqual(t, -1, waitFirst, "要逐个等：%v", commands)
	require.NotEqual(t, -1, applySecond, "第二个也要 apply：%v", commands)
	assert.Less(t, applyFirst, waitFirst, "先 apply 再等")
	assert.Less(t, waitFirst, applySecond,
		"1.0.0 的迁移必须**跑完**才下发 2.0.0 的——同时下发就等于让它们抢同一个库")
}

// 不同组件之间不串：它们的 component_id 不同，主键已经让它们互不相干，
// 串起来只会平白拖慢 up。所以两个组的 Job 要在互相等待之前就都下发出去。
func TestKubectlAppliesIndependentMigrationsTogether(t *testing.T) {
	rec := newRecorder()

	require.NoError(t, kubectlWith(rec).Up(context.Background(), UpRequest{
		File: "/p/k8s", Project: "brickkit-my-erp",
		MigrationGroups: [][]string{
			{"demo-hello-1-0-0-migration"},
			{"people-basic-1-0-0-migration"},
		},
	}))

	commands := rec.commands()
	applyA := indexOfCommand(commands, "apply -f /p/k8s/migrations/demo-hello-1-0-0-migration.yaml")
	applyB := indexOfCommand(commands, "apply -f /p/k8s/migrations/people-basic-1-0-0-migration.yaml")
	waitA := indexOfCommand(commands, "wait --for=condition=complete --timeout="+migrationTimeout+" job/demo-hello-1-0-0-migration")

	require.NotEqual(t, -1, applyA)
	require.NotEqual(t, -1, applyB)
	require.NotEqual(t, -1, waitA)
	assert.Less(t, applyB, waitA,
		"两个组彼此独立，第二组不该等第一组跑完才下发——那是白白串行")
}
