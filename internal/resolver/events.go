package resolver

import (
	"slices"

	"github.com/brickkit/brickkit/internal/manifest"
)

// 本文件把 component.yaml 的 events 声明连成"谁发布、谁订阅"。
//
// # 为什么它不在依赖图里
//
// 事件经消息系统走，发布方与订阅方互不知道对方：订阅方不需要发布方先起来，发布方没有订阅方也照常发。
// 平台对消息系统一无所知，也不去连它。所以这种关系不是 Node 上的边，不进启动顺序、不进级联、不进地址与网络策略——
// 它只在被问到的时候（graph、deps、lint）按名字现算，算的依据只有各组件 Manifest 里声明的那几行字符串。
//
// 匹配规则只有一条（manifest.EventMatches）：订阅项以 * 结尾按前缀，否则完全相等。

// EventParty 是一个组件版本和它声明的事件。
type EventParty struct {
	Ref    Ref
	Events *manifest.Events
}

// EventIndex 回答"这个事件谁在发、谁在收"。Parties 的顺序就是各个结果里组件的顺序。
type EventIndex struct {
	Parties []EventParty
}

// EventFlow 是一条异步边：Publisher 发布的事件里，有 Subscriber 订阅的。
type EventFlow struct {
	Publisher, Subscriber Ref
	// Subscriptions 是 Subscriber 声明的订阅项里收得到 Publisher 事件的那些，按声明顺序——
	// 图上标的是它：订阅方写的原话，一整类事件也只占一行。
	Subscriptions []string
}

// EventIndex 是图里所有声明了事件的组件版本，按解析顺序。
func (g *Graph) EventIndex() EventIndex {
	var x EventIndex
	for _, node := range g.Nodes {
		if node.Manifest != nil && node.Manifest.Events != nil {
			x.Parties = append(x.Parties, EventParty{Ref: node.Ref, Events: node.Manifest.Events})
		}
	}
	return x
}

// Events 是 ref 声明的事件；没声明时返回空的声明，调用方不必判 nil。
func (x EventIndex) Events(ref Ref) manifest.Events {
	for _, p := range x.Parties {
		if p.Ref == ref {
			return *p.Events
		}
	}
	return manifest.Events{}
}

// Subscribers 是订阅了名为 name 的事件的组件版本（发布方自己订阅自己的事件也算）。
func (x EventIndex) Subscribers(name string) []Ref {
	var out []Ref
	for _, p := range x.Parties {
		if slices.ContainsFunc(p.Events.Subscribes, func(s string) bool { return manifest.EventMatches(s, name) }) {
			out = append(out, p.Ref)
		}
	}
	return out
}

// Publishers 是发布了订阅项 subscription 收得到的事件的组件版本。
func (x EventIndex) Publishers(subscription string) []Ref {
	var out []Ref
	for _, p := range x.Parties {
		if len(x.Matched(subscription, p.Ref)) > 0 {
			out = append(out, p.Ref)
		}
	}
	return out
}

// Matched 是 publisher 发布的事件里，订阅项 subscription 收得到的那些，按发布方声明的顺序。
func (x EventIndex) Matched(subscription string, publisher Ref) []string {
	var out []string
	for _, name := range x.Events(publisher).Publishes {
		if manifest.EventMatches(subscription, name) {
			out = append(out, name)
		}
	}
	return out
}

// Published 是项目里发布的全部事件名，去重，按 Parties 的顺序。
func (x EventIndex) Published() []string {
	var out []string
	for _, p := range x.Parties {
		for _, name := range p.Events.Publishes {
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}

// PublishersOf 是发布了名为 name 的事件的组件版本。
func (x EventIndex) PublishersOf(name string) []Ref {
	var out []Ref
	for _, p := range x.Parties {
		if slices.Contains(p.Events.Publishes, name) {
			out = append(out, p.Ref)
		}
	}
	return out
}

// Flows 是全部异步边：先按发布方、再按订阅方（都是 Parties 的顺序）。一个组件订阅自己发布的事件不是边。
func (x EventIndex) Flows() []EventFlow {
	var out []EventFlow
	for _, pub := range x.Parties {
		if len(pub.Events.Publishes) == 0 {
			continue
		}
		for _, sub := range x.Parties {
			if sub.Ref == pub.Ref {
				continue
			}
			var matched []string
			for _, s := range sub.Events.Subscribes {
				if len(x.Matched(s, pub.Ref)) > 0 {
					matched = append(matched, s)
				}
			}
			if len(matched) > 0 {
				out = append(out, EventFlow{Publisher: pub.Ref, Subscriber: sub.Ref, Subscriptions: matched})
			}
		}
	}
	return out
}

// EventSubscription 是某个组件版本声明的一个订阅项。
type EventSubscription struct {
	Ref          Ref
	Subscription string
}

// Unpublished 是项目里没有任何组件发布的订阅项（收自己发布的事件算有人发布），按 Parties 与声明的顺序。
func (x EventIndex) Unpublished() []EventSubscription {
	var out []EventSubscription
	for _, p := range x.Parties {
		for _, s := range p.Events.Subscribes {
			if len(x.Publishers(s)) == 0 {
				out = append(out, EventSubscription{Ref: p.Ref, Subscription: s})
			}
		}
	}
	return out
}
