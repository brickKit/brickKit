package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// people/basic 调用侧的语义错误。
//
// 两者必须分开：主体不在了是**认证失败**（401），依赖挂了是**服务不可用**
// （503）。混成一种会让使用者以为自己密码错了，去反复重试、去改密码，
// 而真正的问题在另一个组件上。
var (
	errPersonNotFound        = errors.New("人员不存在")
	errDependencyUnavailable = errors.New("people/basic 不可用")
)

// person 是 people/basic 返回的人员（只取本组件用得上的字段）。
type person struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DepartmentID string `json:"departmentId"`
}

// peopleClient 是强依赖 people/basic 的客户端接口。
//
// 抽成接口，是为了让"依赖挂了会怎样""人没了会怎样"这两种最要紧的分支
// 能在单元测试里被稳定地造出来——它们恰恰是真环境里最难复现的。
type peopleClient interface {
	GetPerson(ctx context.Context, id string) (person, error)
}

// httpPeopleClient 通过 HTTP 调用 people/basic。
//
// 地址来自平台注入的 PEOPLE_BASIC_ENDPOINT（003 §4.5），
// 组件自己不知道也不该知道对方部署在哪。
type httpPeopleClient struct {
	endpoint string
	client   *http.Client
}

// dependencyTimeout 是调用强依赖的超时。
//
// 必须有：没有超时的话，people/basic 卡住会把本组件的连接一个个耗尽，
// 最后连 /healthz 都挤不进来——一个组件的慢拖垮两个组件。
const dependencyTimeout = 5 * time.Second

func newPeopleClient(endpoint string) *httpPeopleClient {
	return &httpPeopleClient{
		endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		client:   &http.Client{Timeout: dependencyTimeout},
	}
}

func (c *httpPeopleClient) GetPerson(ctx context.Context, id string) (person, error) {
	endpoint := c.endpoint + "/api/v1/people/" + url.PathEscape(id)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return person{}, errDependencyUnavailable
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return person{}, errDependencyUnavailable
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return person{}, errPersonNotFound
	case resp.StatusCode != http.StatusOK:
		// 5xx、以及任何我们没预料到的状态码，都算依赖不可用：
		// 我们无从判断那是什么，不能拿它当"这个人不存在"来用
		return person{}, errDependencyUnavailable
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return person{}, errDependencyUnavailable
	}

	var p person
	if err := json.Unmarshal(body, &p); err != nil {
		return person{}, errDependencyUnavailable
	}
	if p.ID == "" {
		return person{}, errPersonNotFound
	}
	return p, nil
}
