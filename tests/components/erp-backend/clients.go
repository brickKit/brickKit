package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	authorizationv1 "github.com/brickkit/components/erp-backend/gen/authorization/v1"
	peoplev1 "github.com/brickkit/components/erp-backend/gen/people/v1"
)

// 调用下游时的语义错误。
//
// 三者必须分开：令牌无效是 401，人不存在要按业务决定，依赖挂了是 503。
// 混成一种，一次网络抖动就会被报成"你的令牌过期了"，使用者会去反复登录。
var (
	errTokenInvalid          = errors.New("令牌无效")
	errPersonNotFound        = errors.New("人员不存在")
	errDependencyUnavailable = errors.New("依赖组件不可用")
)

// dependencyTimeout 是调用任一下游的超时。
//
// 必须有：没有超时的话，一个下游卡住会把本组件的连接一个个耗尽，
// 最后连 /healthz 都挤不进来——一个组件的慢拖垮一整条链。
const dependencyTimeout = 5 * time.Second

// ============================================================
// auth/password-login（HTTP）
// ============================================================

type identity struct {
	PersonID     string `json:"personId"`
	Username     string `json:"username"`
	DepartmentID string `json:"departmentId"`
}

type loginResult struct {
	Token    string `json:"token"`
	PersonID string `json:"personId"`
	Username string `json:"username"`
}

// authClient 是对 auth/password-login 的调用。
//
// 抽成接口，是为了让"依赖挂了会怎样""令牌无效会怎样"这两种最要紧的分支
// 能在单元测试里被稳定地造出来——它们恰恰是真环境里最难复现的。
type authClient interface {
	Login(ctx context.Context, username, password string) (loginResult, error)
	Verify(ctx context.Context, token string) (identity, error)
}

type httpAuthClient struct {
	endpoint string
	client   *http.Client
}

func newAuthClient(endpoint string) *httpAuthClient {
	return &httpAuthClient{
		endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		client:   &http.Client{Timeout: dependencyTimeout},
	}
}

func (c *httpAuthClient) Login(ctx context.Context, username, password string) (loginResult, error) {
	var out loginResult
	err := c.post(ctx, "/api/v1/login",
		map[string]string{"username": username, "password": password}, &out)
	return out, err
}

func (c *httpAuthClient) Verify(ctx context.Context, token string) (identity, error) {
	var out identity
	err := c.post(ctx, "/api/v1/verify", map[string]string{"token": token}, &out)
	return out, err
}

func (c *httpAuthClient) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return errDependencyUnavailable
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return errDependencyUnavailable
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return errDependencyUnavailable
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		// 401 是**业务答案**（这个令牌不对），不是故障
		return errTokenInvalid
	case resp.StatusCode != http.StatusOK:
		return errDependencyUnavailable
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errDependencyUnavailable
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errDependencyUnavailable
	}
	return nil
}

// ============================================================
// authorization/rbac（gRPC）
// ============================================================

type authorizationClient interface {
	Check(ctx context.Context, personID, permission string) (bool, error)
}

type grpcAuthorizationClient struct {
	conn   *grpc.ClientConn
	client authorizationv1.AuthorizationServiceClient
}

func newAuthorizationClient(endpoint string) (*grpcAuthorizationClient, error) {
	conn, err := dialGRPC(endpoint)
	if err != nil {
		return nil, err
	}
	return &grpcAuthorizationClient{
		conn:   conn,
		client: authorizationv1.NewAuthorizationServiceClient(conn),
	}, nil
}

func (c *grpcAuthorizationClient) Close() error { return c.conn.Close() }

func (c *grpcAuthorizationClient) Check(ctx context.Context, personID, permission string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dependencyTimeout)
	defer cancel()

	resp, err := c.client.Check(ctx, &authorizationv1.CheckRequest{
		PersonId: personID, Permission: permission,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// rbac 说没这个人：那就是没权限，不是故障
			return false, nil
		}
		return false, errDependencyUnavailable
	}
	return resp.GetAllowed(), nil
}

// ============================================================
// people/basic（gRPC，走 extraPorts 的 9090）
// ============================================================

type person struct {
	ID             string
	Name           string
	DepartmentID   string
	DepartmentName string
}

type peopleClient interface {
	GetPerson(ctx context.Context, id string) (person, error)
}

type grpcPeopleClient struct {
	conn   *grpc.ClientConn
	client peoplev1.PeopleServiceClient
}

func newPeopleClient(endpoint string) (*grpcPeopleClient, error) {
	conn, err := dialGRPC(endpoint)
	if err != nil {
		return nil, err
	}
	return &grpcPeopleClient{conn: conn, client: peoplev1.NewPeopleServiceClient(conn)}, nil
}

func (c *grpcPeopleClient) Close() error { return c.conn.Close() }

func (c *grpcPeopleClient) GetPerson(ctx context.Context, id string) (person, error) {
	ctx, cancel := context.WithTimeout(ctx, dependencyTimeout)
	defer cancel()

	resp, err := c.client.GetPerson(ctx, &peoplev1.GetPersonRequest{Id: id})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return person{}, errPersonNotFound
		}
		return person{}, errDependencyUnavailable
	}
	return person{
		ID:             resp.GetId(),
		Name:           resp.GetName(),
		DepartmentID:   resp.GetDepartmentId(),
		DepartmentName: resp.GetDepartmentName(),
	}, nil
}

// dialGRPC 建立到某个组件的 gRPC 连接。
//
// 平台注入的 *_ENDPOINT 是带 scheme 的 URL（形如 `<scheme>://<服务名>:<端口>`），
// 而 gRPC 要的是 host:port——**这一步很容易漏**，漏了的表现是
// "dns resolver: missing address" 之类跟业务毫无关系的错。
//
// 用 grpc.NewClient 而不是 Dial：它不在这里阻塞等连接，
// 下游还没起来时本组件照样能先启动，等真正调用时再连（005 §7 的启动顺序问题）。
func dialGRPC(endpoint string) (*grpc.ClientConn, error) {
	return grpc.NewClient(grpcTarget(endpoint),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// grpcTarget 把 http://host:port 这样的地址转成 gRPC 要的 host:port。
func grpcTarget(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if !strings.Contains(endpoint, "://") {
		return strings.TrimSuffix(endpoint, "/")
	}
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return endpoint
}
