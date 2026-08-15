// Package deploy 放两种部署目标（Docker / K8s）共用的结论。
//
// 只放"与目标无关"的东西：怎么渲染是 compose / k8s 各自的事，
// 但"使用者还需要先建哪些数据库"两边算出来的必须是同一个答案——
// 两处各算一遍，迟早会出现"docker 下提示建库、k8s 下不提示"这种事。
package deploy

import (
	"sort"

	"github.com/brickkit/brickkit/internal/config"
)

// DatabaseRequirement 是一个需要**使用者预先创建**的数据库。
//
// 006 §9.1/§9.5：CLI 不创建数据库。但平台有责任说清楚要建哪些——
// 否则组件会在迁移阶段抛出一句难以定位的 `database "xxx" does not exist`。
type DatabaseRequirement struct {
	// ResourceID 是 brickkit.yaml 中的资源 ID。
	ResourceID string
	Host       string
	Port       int
	// Name 是要创建的库名（bindings[].database）。
	Name string
	// Components 是使用该库的组件。
	Components []string
	// CreateSQL 是可直接执行的建库语句。
	CreateSQL string
}

// Databases 汇总本次启动的组件需要哪些库（006 §9.5）。
//
// componentIDs 是本次会跑起来的组件 ID。local: true 的组件同样算数：
// 它不生成容器，但它照样要连自己的库——漏掉它，使用者就会在 IDE 里
// 对着 `database "xxx" does not exist` 发懵。
func Databases(cfg *config.Config, componentIDs []string) []DatabaseRequirement {
	if cfg == nil {
		return nil
	}

	used := make(map[string]bool, len(componentIDs))
	for _, id := range componentIDs {
		used[id] = true
	}

	byKey := map[string]*DatabaseRequirement{}
	for _, resource := range cfg.Resources {
		if resource.Kind != config.ResourceKindDatabase {
			continue
		}
		for _, binding := range resource.Bindings {
			if binding.Database == "" || !used[binding.ComponentID] {
				continue
			}
			key := resource.ID + "/" + binding.Database
			if existing, ok := byKey[key]; ok {
				existing.Components = append(existing.Components, binding.ComponentID)
				continue
			}
			byKey[key] = &DatabaseRequirement{
				ResourceID: resource.ID,
				Host:       resource.Host,
				Port:       resource.Port,
				Name:       binding.Database,
				Components: []string{binding.ComponentID},
				CreateSQL:  `CREATE DATABASE "` + binding.Database + `"`,
			}
		}
	}

	out := make([]DatabaseRequirement, 0, len(byKey))
	for _, req := range byKey {
		sort.Strings(req.Components)
		out = append(out, *req)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
