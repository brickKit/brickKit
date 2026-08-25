package engine

// 本文件只盯一件事：**生成器产出的子目录，引擎全都认识**。
//
// 这是一条接线测试。它防的不是某个函数算错，而是两个包各写各的字符串字面量：
// k8s 包新增一类清单（P26 的 networkpolicies / serviceaccounts 就是这么来的），
// 引擎这边忘了加，表现是"清单生成了、集群里却没有"——`brickkit up`
// 一路成功、退出码 0，只有去 kubectl get 才发现少了东西。
// down 那边漏掉更隐蔽：删不干净，下次 up 撞上残留。
//
// 放在 engine 包里而不是 k8s 包里，是因为要断言的是**引擎**认不认识；
// 反过来写的话，k8s 包就得反向依赖 engine。

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/k8s"
)

// 引擎的部署顺序必须与生成器的子目录**一一对应**。
//
// 两个方向都要查：少了 → 有清单不会被 apply；多了 → 引擎在找一个永远不存在的
// 目录（那个倒是无害，但说明有一边改过而另一边没跟上）。
func TestApplyOrderCoversGeneratedDirs(t *testing.T) {
	assert.ElementsMatch(t, k8s.ManifestDirs(), applyOrder,
		"引擎的 applyOrder 与 k8s.ManifestDirs() 不一致——"+
			"生成器新增一类清单时，engine/kubectl.go 的 applyOrder 也要加上")
}

// 删除的资源类型必须**逐类覆盖**每一个生成子目录，且顺序是部署顺序的严格反序。
//
// down 改成按标签删之后（不再 kubectl delete -f 目录），子目录名要换成资源类型，
// 中间隔了一张映射表——而这个文件存在的理由正是"两个包各写各的字符串字面量"。
// 映射表漏一项，deleteKinds 会**静默少删一类**：清单照样生成、照样 apply，
// down 却删不掉它，下次 up 撞上残留。所以这里查的是**数目对得上**，
// 不只是"已有的那几项排对了"。
//
// 反序不只是对称：先删 ingress 再删 deployment，中间那一小段时间里外面打进来的
// 请求会干脆地 404，而不是打到一个正在消失的后端上超时。
func TestDeleteKindsCoversEveryDirInReverseOrder(t *testing.T) {
	kinds := deleteKinds()

	if !assert.Len(t, kinds, len(applyOrder),
		"每个生成子目录都要对应一个删除类型——deleteKinds 里的映射表漏项了") {
		return
	}
	// 反序：ingress 在最前，secrets 在最后
	assert.Equal(t, "ingress", kinds[0], "ingress 必须最先删")
	assert.Equal(t, "secret", kinds[len(kinds)-1], "secret 最后删")

	index := map[string]int{}
	for i, kind := range kinds {
		index[kind] = i
	}
	assert.Less(t, index["ingress"], index["deployment"],
		"先删 ingress 再删 deployment，否则请求会打到正在消失的后端上超时")
}

// 删除类型不能含 namespace。
//
// 与 pruneKinds 同一条理由，但这里更要紧：down 的这条路专供
// "命名空间是运维建的"（deploy.createNamespace: false）——
// 那是别人的地盘，里面多半还跑着别的东西。
func TestDeleteKindsNeverTouchesNamespace(t *testing.T) {
	assert.NotContains(t, deleteKinds(), "namespace",
		"命名空间不是我们建的时候，删它等于把整个团队一起端了")
}

// ServiceAccount 与 NetworkPolicy 必须排在 deployments 之前。
//
// SA 排后面 → Pod 引用一个还不存在的 SA，创建被拒（ReplicaSet 会重试，
// 最终能起来，但 up 期间会看到一串莫名其妙的失败事件）。
// NetworkPolicy 排后面 → 有一段时间 Pod 已经在跑、策略还没铺上，谁都进得来。
func TestHardeningAppliedBeforeWorkloads(t *testing.T) {
	index := map[string]int{}
	for i, dir := range applyOrder {
		index[dir] = i
	}

	for _, dir := range []string{"serviceaccounts", "networkpolicies"} {
		assert.Less(t, index[dir], index["deployments"],
			"%s 必须排在 deployments 之前", dir)
	}
}

// PDB 必须在孤儿清理范围内（P35）。
//
// 漏了它的后果是**单向不可逆**：把 replicas 从 3 改回 1 之后，
// 生成物里不再有 PDB，`kubectl apply` 也不会删已经在集群里的那一份——
// 于是一份 maxUnavailable: 1 的 PDB 永远留在单副本组件上，
// 让节点从此排不空。而这正是 P35 当初决定不生成 PDB 的那个理由，
// 只不过换了个更隐蔽的入口。
func TestPruneCoversPodDisruptionBudget(t *testing.T) {
	assert.Contains(t, pruneKinds, "poddisruptionbudget",
		"P35：replicas 从 3 改回 1 时这份 PDB 必须被删掉，"+
			"否则它会永远留在单副本组件上让节点排不空")
}
