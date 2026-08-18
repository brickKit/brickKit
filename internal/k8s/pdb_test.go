package k8s_test

// 本文件测 PodDisruptionBudget 生成（P35，005 §5.8）。
//
// # 为什么这件事拖到现在才做
//
// PDB 的作用是"节点排空/升级时，别把我的副本一次全干掉"。但在
// **单副本**下它无论怎么写都是死路——这是在 calico 集群上真跑确认的：
//
//	minAvailable: 1     ALLOWED DISRUPTIONS 恒为 0，`kubectl drain` 永远排不空
//	maxUnavailable: 1   一个副本随时可被驱逐，等于没写
//
// 所以前提是先能配多副本。两者是同一件事的两半。
//
// # 代价落在别人身上，所以宁可不生成
//
// 一个排不空的节点，报错现场是几个月后运维执行 `kubectl drain` 的终端，
// 而根因是当初 brickkit.yaml 里的一个开关——两者之间没有任何线索相连。
// 这就是"单副本时坚决不生成"这条规则值得用测试钉死的原因。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
)

const pdbPath = "poddisruptionbudgets/people-basic-1-0-0.yaml"

// pdbBuilder 造一个副本数可指定的项目。
func pdbBuilder(t *testing.T, replicas *int) *builder {
	t.Helper()

	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Replicas: replicas})
	return b
}

// 单副本时**坚决不生成**。
//
// 这是整个 P35 里最重要的一条。生成了的话，几个月后运维 `kubectl drain`
// 会永远排不空，而现场没有任何线索指向 brickkit.yaml。
func TestNoPDBForSingleReplica(t *testing.T) {
	result := pdbBuilder(t, nil).generate()

	assert.False(t, hasFile(result, pdbPath),
		"P35：单副本下 PDB 必然让节点排不空，而报错现场在几个月后运维的终端上")
}

// 显式写 replicas: 1 同样不生成。
//
// 判据是"实际副本数"，不是"有没有写这个字段"。
func TestNoPDBForExplicitSingleReplica(t *testing.T) {
	result := pdbBuilder(t, intPtr(1)).generate()

	assert.False(t, hasFile(result, pdbPath),
		"P35：判据是实际副本数，不是有没有写 replicas")
}

// 多副本时生成。
func TestPDBGeneratedForMultipleReplicas(t *testing.T) {
	doc := pdbBuilder(t, intPtr(3)).doc(pdbPath)

	assert.Equal(t, "policy/v1", doc["apiVersion"])
	assert.Equal(t, "PodDisruptionBudget", doc["kind"])
}

// 用 maxUnavailable: 1，不用 minAvailable。
//
// 两者在固定副本数下等价，但 maxUnavailable **随副本数自动成立**：
// 使用者把 3 改成 5 时不用回来改 PDB。而 minAvailable 写死 N-1 的话，
// 改副本数就会悄悄改变容错度——且没有任何提示。
func TestPDBUsesMaxUnavailable(t *testing.T) {
	doc := pdbBuilder(t, intPtr(3)).doc(pdbPath)

	spec, ok := doc["spec"].(map[string]any)
	require.True(t, ok, "%v", doc)

	assert.Equal(t, 1, spec["maxUnavailable"],
		"P35：maxUnavailable 随副本数自动成立，minAvailable 得跟着副本数一起改")
	assert.NotContains(t, spec, "minAvailable",
		"P35：两个都写的话 K8s 直接拒绝这份清单")
}

// selector 必须和 Deployment 选中的是同一批 Pod。
//
// 选错的后果是"PDB 存在、但保护的是空集"——`kubectl get pdb` 看着一切正常，
// 而 drain 时该拦的一个都没拦住。
func TestPDBSelectorMatchesDeployment(t *testing.T) {
	b := pdbBuilder(t, intPtr(3))
	pdb := b.doc(pdbPath)
	deployment := b.doc("deployments/people-basic-1-0-0.yaml")

	assert.Equal(t,
		dig(t, deployment, "spec", "selector", "matchLabels"),
		dig(t, pdb, "spec", "selector", "matchLabels"),
		"P35：selector 对不上时 PDB 保护的是空集——看着正常，drain 时一个都没拦住")
}

// PDB 要落在正确的命名空间里，并带上项目标签。
//
// 带标签是为了能被 P38 的孤儿清理认领：不带的话，副本数改回 1 之后
// 这份 PDB 会**永远留在集群里**，继续拦着 drain。
func TestPDBHasNamespaceAndProjectLabel(t *testing.T) {
	doc := pdbBuilder(t, intPtr(3)).doc(pdbPath)

	assert.Equal(t, "brickkit-my-erp", dig(t, doc, "metadata", "namespace"))
	assert.Equal(t, "my-erp", dig(t, doc, "metadata", "labels", "brickkit.io/project"),
		"P35：不带项目标签的话，副本数改回 1 之后这份 PDB 会永远留在集群里拦着 drain")
}

// external 组件不生成 PDB（它压根没有 Deployment）。
func TestNoPDBForExternalComponent(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("infra/notifier", "1.0.0", 8080), config.Component{
		External: &config.External{Project: "platform-shared"},
	})

	result := b.generate()
	for _, path := range pathsOf(result) {
		assert.NotContains(t, path, "poddisruptionbudget",
			"P35：它由别的项目部署，这边连 Deployment 都没有")
	}
}
