package main

import (
	"context"
	"errors"
	"sync"
)

// errOrderNotFound 表示订单不存在。
var errOrderNotFound = errors.New("订单不存在")

// 订单状态。
const (
	orderPending  = "pending"
	orderApproved = "approved"
)

// Order 是一张订单。
//
// **它只存 ownerId，不存姓名。** 姓名与部门在 people/basic 里，查询时现取——
// 连接组件不重复存别人的主数据，否则人员改了名，这里就是一份永远对不上的旧数据。
type Order struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Amount  int    `json:"amount"`
	OwnerID string `json:"ownerId"`
	Status  string `json:"status"`
}

// enrichedOrder 是补全了人员信息之后的订单（对外的形状）。
type enrichedOrder struct {
	Order
	OwnerName       string `json:"ownerName"`
	OwnerDepartment string `json:"ownerDepartment"`
}

// Orders 是订单的存取接口。
type Orders interface {
	List(ctx context.Context) ([]Order, error)
	Approve(ctx context.Context, id string) error
}

// seedOrders 是内置的样例订单。
//
// **erp/backend 没有 database 资源**，订单放在内存里——这不是偷懒，而是为了
// 把"连接组件"这件事说清楚：它自己不掌握主数据，价值全在编排。
// 顺带也验证了平台的一条路径：一个**只有组件依赖、没有资源依赖**的组件
// 能不能被正确地解析、注入与启动。
func seedOrders() []Order {
	return []Order{
		{ID: "o-1", Title: "服务器采购", Amount: 120000, OwnerID: "p-001", Status: orderPending},
		{ID: "o-2", Title: "差旅报销", Amount: 3200, OwnerID: "p-001", Status: orderPending},
		{ID: "o-3", Title: "培训费用", Amount: 8000, OwnerID: "p-004", Status: orderPending},
	}
}

type memoryOrders struct {
	mu    sync.RWMutex
	items []Order
}

func newMemoryOrders(seed []Order) *memoryOrders {
	return &memoryOrders{items: append([]Order(nil), seed...)}
}

func (m *memoryOrders) List(context.Context) ([]Order, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return append([]Order(nil), m.items...), nil
}

func (m *memoryOrders) Approve(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.items {
		if m.items[i].ID == id {
			m.items[i].Status = orderApproved
			return nil
		}
	}
	return errOrderNotFound
}
