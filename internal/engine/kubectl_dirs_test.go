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

// 删除顺序是部署顺序的严格反序。
//
// 反序不只是对称：先删 ingress 再删 deployment，中间那一小段时间里外面打进来的
// 请求会干脆地 404，而不是打到一个正在消失的后端上超时。
func TestManifestDirsIsReverseOfApplyOrder(t *testing.T) {
	dirs := manifestDirs()

	if !assert.Len(t, dirs, len(applyOrder)) {
		return
	}
	for i, dir := range dirs {
		assert.Equal(t, applyOrder[len(applyOrder)-1-i], dir,
			"删除顺序必须是部署顺序的反序，第 %d 项对不上", i)
	}
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
