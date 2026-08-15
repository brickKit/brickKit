package k8s

// 本文件渲染数据库迁移 Job（005 §6.3）。

// MigrationJobName 是某个组件的迁移 Job 名。
//
// 命令层要用它做两件事：执行前 `kubectl delete job --ignore-not-found` 清理残留，
// 之后 `kubectl wait --for=condition=complete` 等它跑完（005 §6.3）。
func MigrationJobName(service string) string { return service + "-migration" }

// jobLabelsOf 是迁移 Job 的标签。
//
// 关键在 app：**绝不能**和组件本身一样。
// Service 的 selector 就是 `app: <版本化服务名>`，迁移 Pod 一旦带上同一个标签，
// 就会被登记成该 Service 的一个后端——它没有就绪探针，K8s 会认为它随时可用，
// 于是迁移期间打到这个组件的请求有一部分会被转发给一个根本不监听端口的 Pod，
// 表现成偶发的 connection refused。
func (p *plan) jobLabelsOf(c componentPlan) map[string]any {
	labels := p.labelsOf(c)
	labels[labelApp] = MigrationJobName(c.Service)
	labels[labelRole] = roleMigration
	return labels
}

// migrationJobDoc 渲染一个组件的迁移 Job。
func (p *plan) migrationJobDoc(c componentPlan) map[string]any {
	labels := p.jobLabelsOf(c)

	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":        MigrationJobName(c.Service),
			"namespace":   p.namespace,
			"labels":      labels,
			"annotations": p.annotationsOf(c),
		},
		"spec": map[string]any{
			// backoffLimit: 0——迁移失败不重试。
			//
			// 重试只会把同一个坏脚本再跑几遍：迁移失败几乎总是脚本或数据的问题，
			// 重跑既修不好，还可能在半成品状态上再叠一层（005 §6.3）
			"backoffLimit": 0,
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec":     p.migrationPodSpec(c),
			},
		},
	}
}

// migrationPodSpec 渲染迁移 Pod 的规格。
//
// 集群侧的要求（securityContext / imagePullSecrets）与主容器完全一致：
// 它跑在同一个命名空间里，用的是同一个私有镜像。
func (p *plan) migrationPodSpec(c componentPlan) map[string]any {
	spec := p.podSpec(c, p.migrationContainerDoc(c))
	spec["restartPolicy"] = "Never"
	return spec
}

// migrationContainerDoc 渲染迁移容器。
func (p *plan) migrationContainerDoc(c componentPlan) map[string]any {
	container := map[string]any{
		// 002 §8.4：用组件自己的镜像，迁移脚本与业务代码同版本
		"name":  containerName(c.Ref.ID) + "-migration",
		"image": c.Manifest.Deployment.Image,
		// K8s 的 command 整体替换镜像的 ENTRYPOINT，所以整条命令原样写进去即可。
		//
		// 这里与 compose 那边不一样：compose 的 command 只覆盖 CMD，得把命令
		// 拆成 entrypoint + command 两半，否则会拼成 `<entrypoint> migrate up`，
		// 参数错位，"迁移容器"实际上把服务起了起来（005 §6.3）
		"command": anySlice(c.Manifest.Migration.Command),
	}

	// 002 §8.5：环境变量与主容器完全一致——迁移连的必须是同一个库
	if env := p.envDoc(c); len(env) > 0 {
		container["env"] = env
	}
	if sc := p.securityContext(); sc != nil {
		container["securityContext"] = sc
	}
	// 探针与端口都不生成：一次性任务不监听端口，
	// 给它加探针只会让一个正常跑着的迁移被判成不健康
	return container
}

func anySlice(items []string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}
