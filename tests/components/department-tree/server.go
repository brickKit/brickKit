package main

import (
	"net"
	"net/http"
	"strings"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	departmentv1 "github.com/brickkit/components/department-tree/gen/department/v1"
)

// server 在**同一个端口**上同时提供 HTTP/1.1 REST 与 gRPC（009 §Go 组件）。
//
// 做法：用 h2c 承载明文 HTTP/2，再按 Content-Type 分流——
// `application/grpc` 交给 gRPC 服务器，其余交给 REST 路由。
//
// 为什么值得这么做：Manifest 里只声明一个 deployment.port，
// 平台的健康检查、_ENDPOINT 注入、K8s Service 都只认这一个端口。
// 双端口方案（如 Python 组件）得额外声明 extraPorts，能省则省。
type server struct {
	http *http.Server
	grpc *grpc.Server
}

func newServer(svc *service) *server {
	grpcServer := grpc.NewServer()
	departmentv1.RegisterDepartmentServiceServer(grpcServer, svc)

	// 21.3 反射：让 grpcurl 不带 .proto 文件也能列出并调用服务。
	// 对一个"契约即产物"的平台来说，这是排障时最省事的入口。
	reflection.Register(grpcServer)

	rest := svc.routes()
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isGRPCRequest(r) {
			grpcServer.ServeHTTP(w, r)
			return
		}
		rest.ServeHTTP(w, r)
	})

	return &server{
		http: &http.Server{
			// h2c：没有 TLS 的 HTTP/2。容器网络内部通信不需要 TLS，
			// 而 gRPC 必须跑在 HTTP/2 上。
			Handler:           h2c.NewHandler(router, &http2.Server{}),
			ReadHeaderTimeout: readHeaderTimeout,
		},
		grpc: grpcServer,
	}
}

// isGRPCRequest 判断这是不是一次 gRPC 调用。
func isGRPCRequest(r *http.Request) bool {
	return r.ProtoMajor == 2 &&
		strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc")
}

// Serve 在给定的监听器上提供服务。
func (s *server) Serve(listener net.Listener) error { return s.http.Serve(listener) }

// Close 停止服务。
func (s *server) Close() error {
	s.grpc.Stop()
	return s.http.Close()
}
