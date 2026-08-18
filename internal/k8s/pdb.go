package k8s

// 本文件生成 PodDisruptionBudget（P35，005 §5.8）。
//
// # 它保护什么
//
// 节点排空（`kubectl drain`，集群升级、机器下线都会用到）会驱逐节点上的 Pod。
// 没有 PDB 时，K8s 可以把一个组件的副本**同时**全部驱逐——服务瞬间归零，
// 而这一切在运维看来只是"我升级了一下集群"。
//
// # 单副本时坚决不生成
//
// 这条比"生成得对"更重要。单副本下 PDB 无论怎么写都是死路，
// 已在 calico 集群上真跑确认：
//
//	minAvailable: 1     ALLOWED DISRUPTIONS 恒为 0，节点永远排不空
//	maxUnavailable: 1   一个副本随时可被驱逐，等于没写
//
// 而**代价落在别人身上**：一个排不空的节点，报错现场是几个月后运维执行
// drain 的终端，根因却是当初 brickkit.yaml 里的一个开关，
// 两者之间没有任何线索相连。

import "github.com/brickkit/brickkit/internal/config"

// pdbDoc 渲染一个组件的 PodDisruptionBudget。
//
// # 为什么用 maxUnavailable 而不是 minAvailable
//
// 固定副本数下两者等价（replicas=3 时 minAvailable:2 == maxUnavailable:1），
// 但 maxUnavailable **随副本数自动成立**：使用者把 3 改成 5 时不用回来改 PDB。
// 写死 minAvailable: N-1 的话，改副本数就会悄悄改变容错度，且没有任何提示。
//
// 取 1 而不是百分比：这是**安全的那一边**。副本很多时排空会慢一些，
// 但慢从来不会变成故障，而一次多驱逐几个会。
func (p *plan) pdbDoc(c componentPlan) map[string]any {
	return map[string]any{
		"apiVersion": "policy/v1",
		"kind":       "PodDisruptionBudget",
		"metadata": map[string]any{
			"name":      c.Service,
			"namespace": p.namespace,
			"labels":    p.labelsOf(c),
		},
		"spec": map[string]any{
			"maxUnavailable": 1,
			// selector 必须与 Deployment 选中的是同一批 Pod。对不上的话
			// PDB 保护的是空集——`kubectl get pdb` 看着一切正常，
			// 而 drain 时该拦的一个都没拦住
			"selector": map[string]any{
				"matchLabels": map[string]any{labelApp: c.Service},
			},
		},
	}
}

// needsPDB 判断这个组件要不要 PDB。
//
// 判据是**实际副本数**，不是"有没有写 replicas"：显式写 replicas: 1
// 与不写完全等价，都不该生成。
func needsPDB(entry config.Component) bool {
	return entry.ReplicaCount() > 1
}
