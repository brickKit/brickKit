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
// 生成物永远是完整的一份（`--only` 已删，003 §4.3：要收窄范围就改 enabled），
// 所以清理是无条件的——配置里关掉的组件，它的容器也要跟着消失。
// `down` 的帮助文本把这条路写成了"只停其中几个"的正解，它必须真的通
// （down_test.go 的 TestDisablingAComponentRemovesItsContainerOnNextUp）。
func TestUpDockerAlwaysPrunes(t *testing.T) {
	f := composeProject(t)
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "up")
	require.Equal(t, 0, r.code, r.stderr)

	assert.NotEmpty(t, eng.lastUp(t).PruneSelector,
		"配置里关掉的组件要跟着下线，否则容器会永远留着")
}
