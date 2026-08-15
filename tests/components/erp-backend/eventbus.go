package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// 事件类型。
const eventOrderApproved = "erp.order.approved"

// event 是发往事件总线的一条事件。
type event struct {
	Type    string    `json:"type"`
	Actor   string    `json:"actor"`
	Subject string    `json:"subject"`
	Time    time.Time `json:"time"`
}

// eventBus 是**弱依赖** infra/redis-event-bus 的出口。
//
// 弱依赖的定义是"有就用、没有就降级"（003 §4.3）：
//
//   - 没装这个组件时，平台**完全不注入** INFRA_REDIS_EVENT_BUS_ENDPOINT
//     （开发进度 D140：不注入空串，那会让组件以为"配了但值是空的"）
//   - 装了但调用失败时，业务照常完成
//
// 无论哪种情况都不能让审批失败——否则它就成了事实上的强依赖。
type eventBus interface {
	// Enabled 表示平台有没有注入这个弱依赖的地址。
	Enabled() bool
	Publish(ctx context.Context, e event) error
}

// disabledEventBus 是"平台没注入弱依赖地址"时用的实现。
//
// 用一个什么都不做的实现，而不是让 bus 为 nil：调用点就不必到处写
// `if s.bus != nil`，漏一处就是一次空指针 panic。
type disabledEventBus struct{}

func (disabledEventBus) Enabled() bool                        { return false }
func (disabledEventBus) Publish(context.Context, event) error { return nil }

// httpEventBus 通过 HTTP 把事件投递给 infra/redis-event-bus。
type httpEventBus struct {
	endpoint string
	client   *http.Client
}

// eventTimeout 比调用强依赖的超时更短。
//
// 事件是"顺手发一下"的事，业务结果不等它。让它拖住一个已经成功的审批
// 五秒钟，是把弱依赖的代价转嫁给了使用者。
const eventTimeout = 2 * time.Second

func newEventBus(endpoint string) eventBus {
	if strings.TrimSpace(endpoint) == "" {
		return disabledEventBus{}
	}
	return &httpEventBus{
		endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		client:   &http.Client{Timeout: eventTimeout},
	}
}

func (b *httpEventBus) Enabled() bool { return true }

func (b *httpEventBus) Publish(ctx context.Context, e event) error {
	ctx, cancel := context.WithTimeout(ctx, eventTimeout)
	defer cancel()

	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.endpoint+"/api/v1/events", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		return &busError{status: resp.StatusCode}
	}
	return nil
}

type busError struct{ status int }

func (e *busError) Error() string { return "事件总线返回了错误状态码" }
