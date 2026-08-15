package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
)

// 两条发现路径。组件提供哪一条都行，都不提供就是"没有可展示的文档"。
const (
	// kindOpenAPI：组件在 /openapi.json 上暴露自己的 HTTP API 文档。
	kindOpenAPI = "openapi"
	// kindGRPC：组件开着 gRPC Reflection，可以不带 .proto 列出服务与方法。
	kindGRPC = "grpc"
)

// 探测状态。
const (
	statusOK          = "ok"          // 拿到了文档
	statusUnreachable = "unreachable" // 地址在，但连不上
	statusNoDocs      = "no-docs"     // 连上了，但两条路径都没有
	statusAbsent      = "absent"      // 平台没注入地址：这个弱依赖压根没装
)

// probeTimeout 是单次探测的超时。
//
// 短一点：文档聚合是个"顺手看看"的功能，不该让一个卡住的组件把整个页面
// 拖到打不开。7 个组件串行探完最坏也就几秒——而它们是并发探的。
const probeTimeout = 3 * time.Second

// Source 是一个被聚合的组件的文档状态。
type Source struct {
	ComponentID string `json:"componentId"`
	Endpoint    string `json:"-"` // 不外泄内部拓扑
	Status      string `json:"status"`
	// Reason 说明为什么没有文档，直接给人看。
	Reason string `json:"reason,omitempty"`
	// Kinds 是这个组件提供了哪几种文档。
	Kinds []string `json:"kinds"`
	// OpenAPI 是抓到的 OpenAPI 文档原文。
	OpenAPI json.RawMessage `json:"-"`
	// GRPCServices 是反射列出的服务与方法。
	GRPCServices []GRPCService `json:"grpcServices,omitempty"`
}

// GRPCService 是反射列出的一个 gRPC 服务。
type GRPCService struct {
	Name    string   `json:"name"`
	Methods []string `json:"methods,omitempty"`
}

// Discoverer 并发探测所有已注入地址的组件。
type Discoverer struct {
	http *http.Client
}

func NewDiscoverer() *Discoverer {
	return &Discoverer{http: &http.Client{Timeout: probeTimeout}}
}

// Discover 探测全部目标，返回按组件 ID 排序的结果。
//
// **任何一个组件出问题都不能影响其余的**（开发计划 28.3）：每个目标各自
// 捕获错误，最坏也只是它自己显示成"不可用"。这正是这个组件要验证的东西——
// 它弱依赖一大堆组件，而弱依赖缺席是常态，不是异常。
func (d *Discoverer) Discover(ctx context.Context, targets []Target) []Source {
	out := make([]Source, len(targets))

	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		go func(i int, target Target) {
			defer wg.Done()
			out[i] = d.probe(ctx, target)
		}(i, target)
	}
	wg.Wait()

	sort.Slice(out, func(i, j int) bool { return out[i].ComponentID < out[j].ComponentID })
	return out
}

func (d *Discoverer) probe(ctx context.Context, target Target) Source {
	source := Source{ComponentID: target.ComponentID, Endpoint: target.Endpoint, Kinds: []string{}}

	// 平台没注入地址 = 这个弱依赖压根没装。这是**正常状态**，不是故障：
	// 003 §4.3 说得明白，弱依赖缺席时完全不注入那个变量
	if target.Endpoint == "" {
		source.Status = statusAbsent
		source.Reason = "该组件未安装（平台没有注入它的地址）"
		return source
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var reachable bool

	if spec, err := d.fetchOpenAPI(ctx, target.Endpoint); err == nil {
		source.OpenAPI = spec
		source.Kinds = append(source.Kinds, kindOpenAPI)
		reachable = true
	} else if !errors.Is(err, errNoDocs) {
		// 连不上与"连上了但没这个端点"要分开：前者是组件挂了，
		// 后者只是它不提供 OpenAPI
		source.Reason = "无法连接"
	} else {
		reachable = true
	}

	if services, err := d.fetchGRPCServices(ctx, target.Endpoint); err == nil && len(services) > 0 {
		source.GRPCServices = services
		source.Kinds = append(source.Kinds, kindGRPC)
		reachable = true
	}

	switch {
	case len(source.Kinds) > 0:
		source.Status = statusOK
		source.Reason = ""
	case reachable:
		source.Status = statusNoDocs
		source.Reason = "组件在线，但既没有 /openapi.json，也没有开 gRPC Reflection"
	default:
		source.Status = statusUnreachable
		source.Reason = "组件暂时不可达"
	}
	return source
}

// errNoDocs 表示"连上了，但没有这份文档"——与"连不上"是两回事。
var errNoDocs = errors.New("组件不提供该文档")

// fetchOpenAPI 抓组件的 /openapi.json。
//
// FastAPI 之类的框架自带这个端点；Go 组件目前不提供，会走到 errNoDocs
// 那条分支，页面上如实显示"没有 HTTP 文档"。
func (d *Discoverer) fetchOpenAPI(ctx context.Context, endpoint string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(endpoint, "/")+"/openapi.json", nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errNoDocs
	}

	// 限制大小：一份 OpenAPI 文档再大也就几百 KB，
	// 不设上限的话一个坏掉的上游就能把内存吃干净
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, errNoDocs
	}
	return body, nil
}

// fetchGRPCServices 通过 gRPC Reflection 列出服务与方法。
//
// 反射的价值在于**不需要 .proto 文件**：文档组件不必预先 vendored 一堆契约，
// 组件升级加了新方法这里也自动跟上（021 验证过 grpcurl 能这么用）。
func (d *Discoverer) fetchGRPCServices(ctx context.Context, endpoint string) ([]GRPCService, error) {
	conn, err := grpc.NewClient(grpcTarget(endpoint),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	stream, err := reflectionpb.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.CloseSend() }()

	names, err := listServices(stream)
	if err != nil {
		return nil, err
	}

	out := make([]GRPCService, 0, len(names))
	for _, name := range names {
		// 反射服务本身不是业务 API，列出来只会干扰阅读
		if strings.HasPrefix(name, "grpc.reflection.") {
			continue
		}
		methods, err := listMethods(stream, name)
		if err != nil {
			// 单个服务查不到方法就只列服务名，不让整次探测失败
			methods = nil
		}
		out = append(out, GRPCService{Name: name, Methods: methods})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type reflectStream interface {
	Send(*reflectionpb.ServerReflectionRequest) error
	Recv() (*reflectionpb.ServerReflectionResponse, error)
}

func listServices(stream reflectStream) ([]string, error) {
	if err := stream.Send(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_ListServices{},
	}); err != nil {
		return nil, err
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	listing := resp.GetListServicesResponse()
	if listing == nil {
		return nil, errNoDocs
	}

	names := make([]string, 0, len(listing.GetService()))
	for _, service := range listing.GetService() {
		names = append(names, service.GetName())
	}
	return names, nil
}

// listMethods 取出某个服务的方法名。
//
// 反射返回的是 FileDescriptorProto 的原始字节。这里不引入 protobuf 描述符
// 解析库，而是**只提取方法名**——文档页面要的就是"有哪些方法可以调"，
// 完整的请求/响应结构由组件自己的 .proto 产物提供（002 §7 契约即产物）。
func listMethods(stream reflectStream, service string) ([]string, error) {
	if err := stream.Send(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: service,
		},
	}); err != nil {
		return nil, err
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	descriptors := resp.GetFileDescriptorResponse()
	if descriptors == nil {
		return nil, errNoDocs
	}

	var methods []string
	for _, raw := range descriptors.GetFileDescriptorProto() {
		methods = append(methods, methodNamesOf(raw, service)...)
	}
	sort.Strings(methods)
	return methods, nil
}

// Target 是一个探测目标。
type Target struct {
	ComponentID string
	Endpoint    string
}

// grpcTarget 把 http://host:port 转成 gRPC 要的 host:port。
//
// 与 erp/backend 里那个是同一件事：平台注入的是带 scheme 的 URL，
// 忘了转换时报的是 "dns resolver: missing address"，跟业务毫无关系。
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
