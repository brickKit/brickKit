package resolver

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/manifest"
)

func eventIndex() (x EventIndex, crm, finance, audit, quiet Ref) {
	crm = Ref{ID: "crm/opportunity", Version: "1.0.0"}
	finance = Ref{ID: "erp/finance", Version: "1.0.0"}
	audit = Ref{ID: "infra/audit", Version: "1.0.0"}
	quiet = Ref{ID: "infra/quiet", Version: "1.0.0"}
	x = EventIndex{Parties: []EventParty{
		{Ref: crm, Events: &manifest.Events{Publishes: []string{"crm.won.v1", "crm.lost.v1"}}},
		{Ref: finance, Events: &manifest.Events{
			Publishes:  []string{"erp.invoice.v1"},
			Subscribes: []string{"crm.won.v1", "erp.invoice.v1", "mdm.customer.*"},
		}},
		{Ref: audit, Events: &manifest.Events{Subscribes: []string{"crm.*", "crm.won.v1", "erp.*"}}},
	}}
	return
}

func TestEventIndexAnswersWhoIsOnTheOtherEnd(t *testing.T) {
	x, crm, finance, audit, quiet := eventIndex()

	assert.Equal(t, []Ref{finance, audit}, x.Subscribers("crm.won.v1"))
	assert.Equal(t, []Ref{audit}, x.Subscribers("crm.lost.v1"))
	assert.Equal(t, []Ref{finance, audit}, x.Subscribers("erp.invoice.v1"), "发布方自己订阅自己的事件也算")
	assert.Empty(t, x.Subscribers("hr.employee.hired.v1"))

	assert.Equal(t, []Ref{crm}, x.Publishers("crm.*"))
	assert.Equal(t, []Ref{crm}, x.PublishersOf("crm.won.v1"))
	assert.Empty(t, x.Publishers("mdm.customer.*"))
	assert.Equal(t, []string{"crm.won.v1", "crm.lost.v1"}, x.Matched("crm.*", crm), "按发布方声明的顺序")
	assert.Equal(t, []string{"crm.won.v1", "crm.lost.v1", "erp.invoice.v1"}, x.Published())

	assert.Equal(t, manifest.Events{}, x.Events(quiet), "没声明事件的组件：空的声明，不是 nil")
}

// 边按发布方、再按订阅方排；标签是订阅方的原话（声明顺序），一个前缀收到几个事件也只算一项。
// 订阅自己发布的事件不是边。
func TestEventFlows(t *testing.T) {
	x, crm, finance, audit, _ := eventIndex()
	assert.Equal(t, []EventFlow{
		{Publisher: crm, Subscriber: finance, Subscriptions: []string{"crm.won.v1"}},
		{Publisher: crm, Subscriber: audit, Subscriptions: []string{"crm.*", "crm.won.v1"}},
		{Publisher: finance, Subscriber: audit, Subscriptions: []string{"erp.*"}},
	}, x.Flows())
}

// 没人发布的订阅项：自己发布的不算，前缀下一个事件都没有的才算。
func TestEventUnpublished(t *testing.T) {
	x, _, finance, _, _ := eventIndex()
	assert.Equal(t, []EventSubscription{{Ref: finance, Subscription: "mdm.customer.*"}}, x.Unpublished())
	assert.Empty(t, EventIndex{}.Unpublished())
	assert.Empty(t, EventIndex{}.Flows())
}
