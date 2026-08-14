// 本文件验证"单端口双协议"这件事本身（开发计划 21.2 / 21.3）：
// 同一个监听端口既能收 HTTP/1.1 的 REST 请求，也能收 gRPC（HTTP/2 cleartext）请求，
// 并且 gRPC Reflection 可用（grpcurl 不带 proto 文件也能调）。
package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1"

	departmentv1 "github.com/brickkit/components/department-tree/gen/department/v1"
)

// startServer 在随机端口上起一个真实的服务器，返回其地址。
func startServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败：%v", err)
	}

	svc := newService(newMemoryStore(seedTree()...),
		config{ComponentID: "department/tree", Version: "1.0.0"})
	srv := newServer(svc)

	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })

	return listener.Addr().String()
}

// dialGRPC 连到同一个端口上的 gRPC 服务。
func dialGRPC(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接 gRPC 失败：%v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// 21.1 + 21.2：同一个端口，HTTP 与 gRPC 都能用。
func TestSinglePortServesBothProtocols(t *testing.T) {
	addr := startServer(t)

	// HTTP/1.1
	resp, err := http.Get("http://" + addr + "/api/v1/departments")
	if err != nil {
		t.Fatalf("HTTP 请求失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP 期望 200，实际 %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("HTTP 响应不是 JSON：%s", raw)
	}

	// gRPC（HTTP/2 cleartext，同一个端口）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := departmentv1.NewDepartmentServiceClient(dialGRPC(t, addr))
	grpcResp, err := client.ListDepartments(ctx, &departmentv1.ListDepartmentsRequest{})
	if err != nil {
		t.Fatalf("21.2 同端口 gRPC 调用失败：%v", err)
	}

	if int(grpcResp.Total) != int(body["total"].(float64)) {
		t.Fatalf("两种协议看到的数据量不一致：gRPC=%d HTTP=%v", grpcResp.Total, body["total"])
	}
}

// 21.3 gRPC Reflection 可用：grpcurl 不带 .proto 也能列出服务并调用。
func TestGRPCReflectionListsService(t *testing.T) {
	addr := startServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := reflectpb.NewServerReflectionClient(dialGRPC(t, addr))
	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		t.Fatalf("21.3 反射服务不可用：%v", err)
	}

	err = stream.Send(&reflectpb.ServerReflectionRequest{
		MessageRequest: &reflectpb.ServerReflectionRequest_ListServices{ListServices: ""},
	})
	if err != nil {
		t.Fatalf("发送反射请求失败：%v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("接收反射响应失败：%v", err)
	}

	listed := resp.GetListServicesResponse()
	if listed == nil {
		t.Fatalf("反射响应里没有服务列表：%v", resp)
	}

	var names []string
	for _, s := range listed.Service {
		names = append(names, s.Name)
	}
	if !contains(names, "department.v1.DepartmentService") {
		t.Fatalf("21.3 反射应列出 department.v1.DepartmentService，实际：%v", names)
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// 健康检查走的是 HTTP，必须在同一个端口上可达 ——
// compose 的 healthcheck 与 K8s 探针都只认 deployment.port（002 §5.5）。
func TestHealthzOnTheSamePort(t *testing.T) {
	addr := startServer(t)

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("健康检查请求失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", resp.StatusCode)
	}
}

// 未知路径给 404 JSON，不给 Go 默认的纯文本。
func TestUnknownPathReturnsJSON404(t *testing.T) {
	addr := startServer(t)

	resp, err := http.Get("http://" + addr + "/nope")
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("期望 404，实际 %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct == "" || ct[:16] != "application/json" {
		t.Fatalf("期望 JSON 响应，实际 Content-Type=%q", ct)
	}
}
