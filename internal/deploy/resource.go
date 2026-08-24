// Package deploy 放两种部署目标（Docker / K8s）共用的结论。
//
// 只放"与目标无关"的东西：怎么渲染是 compose / k8s 各自的事，
// 但"使用者还需要先准备什么"两边算出来的必须是同一个答案——
// 两处各算一遍，迟早会出现"docker 下提示要建库、k8s 下不提示"这种事。
package deploy

import (
	"sort"

	"github.com/brickkit/brickkit/internal/config"
)

// ResourceRequirement 是一个**必须先跑起来**的基础资源。
//
// 006 §9.1：平台不部署基础资源，也不创建库。它们由运维/使用者提前准备，
// 平台只负责把连接信息注入组件。
//
// 但"不代为部署"不等于"不说清楚"：不列出来的话，组件会在启动或迁移时
// 抛出一句难以定位的 `connection refused` / `database "xxx" does not exist`——
// 一句把**环境没准备好**指向**平台或组件**的错误。
type ResourceRequirement struct {
	// ID / Kind / Engine 直接来自 brickkit.yaml 的声明。
	ID     string
	Kind   string
	Engine string
	Host   string
	Port   int
	// Components 是本次会用到它的组件，按 ID 排序。
	Components []string
	// Databases 是要在它上面预先创建的库（只有 kind: database 有）。
	Databases []DatabaseRequirement
}

// DatabaseRequirement 是一个需要**使用者预先创建**的数据库。
//
// 006 §9.5：CLI 不创建数据库。库名由平台通过 DATABASE_NAME 注入，
// 建库是一次性操作，由使用者/运维执行。
type DatabaseRequirement struct {
	// Name 是要创建的库名（bindings[].database）。
	Name string
	// Components 是使用该库的组件。
	Components []string
	// CreateSQL 是可直接执行的建库语句。
	CreateSQL string
}

// Requirements 汇总本次启动的组件需要哪些基础资源先跑起来（006 §9.1、§9.5）。
//
// componentIDs 是本次会跑起来的组件 ID。`local: true` 的组件同样算数：
// 它不生成容器，但它照样要连自己的库——漏掉它，使用者就会在 IDE 里
// 对着 `connection refused` 发懵。
//
// 只列**被本次启动的组件绑定过**的资源：配置里可能声明了整套环境的资源，
// 而这次只跑其中几个组件；把没人用的也列出来，等于要求使用者为一个
// 根本不跑的组件先把库建好。
func Requirements(cfg *config.Config, componentIDs []string) []ResourceRequirement {
	if cfg == nil {
		return nil
	}

	used := make(map[string]bool, len(componentIDs))
	for _, id := range componentIDs {
		used[id] = true
	}

	var out []ResourceRequirement
	for _, resource := range cfg.Resources {
		req := requirementOf(resource, used)
		if req == nil {
			continue
		}
		out = append(out, *req)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// requirementOf 把一条资源声明折算成"这次要不要它、要它做什么"。
// 没有任何启动中的组件绑定它时返回 nil。
func requirementOf(resource config.Resource, used map[string]bool) *ResourceRequirement {
	req := ResourceRequirement{
		ID: resource.ID, Kind: resource.Kind, Engine: resource.Engine,
		Host: resource.Host, Port: resource.Port,
	}

	// 同一个库可能被多个组件共用（006 §7.3），按库名归集
	byDatabase := map[string]*DatabaseRequirement{}
	var order []string

	for _, binding := range resource.Bindings {
		if !used[binding.ComponentID] {
			continue
		}
		req.Components = append(req.Components, binding.ComponentID)

		if resource.Kind != config.ResourceKindDatabase || binding.Database == "" {
			continue
		}
		if existing, ok := byDatabase[binding.Database]; ok {
			existing.Components = append(existing.Components, binding.ComponentID)
			continue
		}
		byDatabase[binding.Database] = &DatabaseRequirement{
			Name:       binding.Database,
			Components: []string{binding.ComponentID},
			CreateSQL:  `CREATE DATABASE "` + binding.Database + `"`,
		}
		order = append(order, binding.Database)
	}

	if len(req.Components) == 0 {
		return nil
	}

	sort.Strings(req.Components)
	sort.Strings(order)
	for _, name := range order {
		db := byDatabase[name]
		sort.Strings(db.Components)
		req.Databases = append(req.Databases, *db)
	}
	return &req
}

// Databases 汇总本次启动需要预先创建的全部数据库（006 §9.5）。
//
// 保留这个入口是因为"要建哪些库"与"要起哪些资源"是两个不同的问题：
// 前者是一次性动作，后者每次启动都得满足。
func Databases(cfg *config.Config, componentIDs []string) []DatabaseRequirement {
	var out []DatabaseRequirement
	for _, req := range Requirements(cfg, componentIDs) {
		out = append(out, req.Databases...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
