package cli

// 本文件是 P38 在**命令层**的行为：什么时候允许清理孤儿，清理了要不要说。
//
// 引擎那一层负责"怎么清"（internal/engine/kubectl_prune_test.go），
// 这一层负责"该不该清"——而这正是整个修复里最危险的判断：
// `--only` 下只部署子集，照着本次的 Services 清理会把没点名的组件全删掉。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 正常 up：带上按项目标签收窄的选择器。
func TestUpK8sEnablesPrune(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "up")
	require.Equal(t, 0, r.code, r.stderr)

	assert.Equal(t, "brickkit.io/project=my-erp", eng.lastUp(t).PruneSelector,
		"P38：升级后要能清掉旧版本，且只清本项目的东西")
}

// `--only` 时**绝不清理**。
//
// 这是整个修复里最危险的一处。`--only people/basic` 下 Services 只有被点名的
// 那些，其余组件照样在集群里正常服务——把它们当孤儿删掉，
// 后果比 P38 本身（多跑一个旧版本）严重得多：那是**把正在服务的组件下线**。
//
// 命令层用"不给选择器"表达这件事，引擎那边空选择器就直接返回。
func TestUpK8sDoesNotPruneWithOnly(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--only", "people/basic")
	require.Equal(t, 0, r.code, r.stderr)

	assert.Empty(t, eng.lastUp(t).PruneSelector,
		"P38：--only 只部署子集，没点名的组件不是孤儿——清了就是把它们下线")
}

// 清理掉什么要**告诉使用者**。
//
// 悄悄删东西不可接受：集群里少了什么，人得知道。
// 尤其是他其实误删了配置、本意并非下线那个组件的时候——
// 那时这行输出是他唯一的线索。
func TestUpK8sReportsPruned(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()
	eng.pruned = []string{"deployment.apps/people-basic-0-9-0", "service/people-basic-0-9-0"}

	r := runWithEngine(t, eng, f.Dir, "up")
	require.Equal(t, 0, r.code, r.stderr)

	assert.Contains(t, r.stdout, "people-basic-0-9-0", "P38：删了什么要说出来：%s", r.stdout)
	assert.Contains(t, r.stdout, "清理", "P38：要说清这是清理动作：%s", r.stdout)
}

// 没清理任何东西时不要输出噪音。
//
// 每次 up 都打一行"已清理 0 个"，会让人以为每次升级都动了什么。
func TestUpK8sSaysNothingWhenNothingPruned(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "up")
	require.Equal(t, 0, r.code, r.stderr)

	assert.NotContains(t, r.stdout, "清理旧版本", "P38：没清理就别提这件事：%s", r.stdout)
}

// Docker 目标用**同一个判据**决定要不要清理孤儿。
//
// 这里曾经断言 Docker 永远不设 PruneSelector，理由是"compose 的
// --remove-orphans 自己兜底"。那个理由不成立：`--only` 生成的 compose
// 文件只含被点名的子集，无条件 --remove-orphans 会把其余组件与
// CLI 托管的资源容器一起删掉——正是 pruneSelectorFor 论证过不可接受的事。
func TestUpDockerPrunesOnlyWhenNotRestricted(t *testing.T) {
	t.Run("整个项目一起起：要清理", func(t *testing.T) {
		f := composeProject(t)
		eng := newFakeEngine()

		r := runWithEngine(t, eng, f.Dir, "up")
		require.Equal(t, 0, r.code, r.stderr)

		assert.NotEmpty(t, eng.lastUp(t).PruneSelector,
			"配置里删掉的组件要跟着下线，否则容器会永远留着")
	})

	t.Run("--only 只起子集：不清理", func(t *testing.T) {
		f := composeProject(t)
		eng := newFakeEngine()

		r := runWithEngine(t, eng, f.Dir, "up", "--only", "people/basic")
		require.Equal(t, 0, r.code, r.stderr)

		assert.Empty(t, eng.lastUp(t).PruneSelector,
			"没点名的组件不是孤儿，删它们等于把线上组件下线")
	})
}
