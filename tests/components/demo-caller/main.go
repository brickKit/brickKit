// demo/caller 是 BrickKit 平台的自测组件：一个会调用别人的 HTTP 组件。
//
// 它覆盖平台最核心的几条承诺，让它们能被真容器验证：
//   - 强依赖地址通过环境变量注入（DEMO_HELLO_ENDPOINT），DNS 即注册中心（002 §5）
//   - 弱依赖缺失时自行降级，平台完全不注入该变量（002 §3.4）
//   - 资源连接信息按 kind 注入（DATABASE_*，006）
//   - 迁移命令在主服务启动前执行，失败要以非 0 退出码阻断（005）
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// 主端口固定 8080，与 component.yaml 的 deployment.port 一致。
const addr = ":8080"

// upstreamTimeout 是调用依赖组件的超时。组件之间是同步 HTTP 调用（002 §5.4）。
const upstreamTimeout = 3 * time.Second

// platformEnvKeys 是回显给调用方的环境变量（004 §5.6）。
// 弱依赖的 DEMO_BUS_ENDPOINT 也在列：它的值为空正好说明"平台没有注入"。
var platformEnvKeys = []string{
	"COMPONENT_ID",
	"COMPONENT_VERSION",
	"DEMO_HELLO_ENDPOINT",
	"DEMO_BUS_ENDPOINT",
	"DATABASE_HOST",
	"DATABASE_PORT",
	"DATABASE_NAME",
	"DATABASE_USER",
}

type server struct {
	componentID string
	version     string
	// helloEndpoint 是强依赖 demo/hello 的地址（平台注入，带版本化服务名）。
	helloEndpoint string
	// busEndpoint 是弱依赖的地址；缺失是正常情况，必须自行降级。
	busEndpoint string
}

func newServerFromEnv() *server {
	return &server{
		componentID:   envOr("COMPONENT_ID", "demo/caller"),
		version:       envOr("COMPONENT_VERSION", "1.0.0"),
		helloEndpoint: os.Getenv("DEMO_HELLO_ENDPOINT"),
		// 002 §3.4：弱依赖必须用安全方式读取，不存在就是不存在
		busEndpoint: os.Getenv("DEMO_BUS_ENDPOINT"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func (s *server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/v1/call", s.handleCall)
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/env", s.handleEnv)
	return mux
}

// handleHealthz 只检查本进程存活：上游挂了也不影响本组件的健康状态（002 §9.4）。
func (s *server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleCall 调用强依赖 demo/hello，并把上游响应原样回显。
func (s *server) handleCall(w http.ResponseWriter, r *http.Request) {
	if s.helloEndpoint == "" {
		writeJSON(w, http.StatusFailedDependency, map[string]any{
			"error": "强依赖地址未注入：环境变量 DEMO_HELLO_ENDPOINT 为空",
		})
		return
	}

	upstream, err := s.fetchHello(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":    "调用 demo/hello 失败：" + err.Error(),
			"endpoint": s.helloEndpoint,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"component": s.componentID,
		"version":   s.version,
		"endpoint":  s.helloEndpoint,
		"upstream":  upstream,
	})
}

func (s *server) fetchHello(ctx context.Context) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.helloEndpoint+"/api/v1/hello", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("上游返回状态码 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("上游响应不是合法 JSON")
	}
	return out, nil
}

// handleStatus 报告自身与各依赖的状态，弱依赖缺失时标记为 degraded。
func (s *server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	eventBus := "degraded"
	if s.busEndpoint != "" {
		eventBus = "available"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"component": s.componentID,
		"version":   s.version,
		"hello":     s.helloEndpoint,
		"eventBus":  eventBus,
	})
}

func (s *server) handleEnv(w http.ResponseWriter, _ *http.Request) {
	env := make(map[string]string, len(platformEnvKeys))
	for _, key := range platformEnvKeys {
		env[key] = os.Getenv(key)
	}
	writeJSON(w, http.StatusOK, map[string]any{"env": env})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// ============================================================
// migration
// ============================================================

// migrate 是 component.yaml 中 migration.command 调用的入口。
//
// 这里不做真实的建表：平台要验证的是"迁移在主服务前执行、失败能阻断"，
// 而不是某个业务表长什么样。所以它只做两件可控的事：
//   - 若注入了 DATABASE_HOST，验证数据库确实可达（证明资源注入是真的）
//   - MIGRATION_SHOULD_FAIL=1 时以非 0 退出码失败，供平台验证阻断行为
func migrate() error {
	if os.Getenv("MIGRATION_SHOULD_FAIL") == "1" {
		return errors.New("迁移被 MIGRATION_SHOULD_FAIL 开关置为失败（用于平台自测）")
	}

	host := os.Getenv("DATABASE_HOST")
	if host == "" {
		// 没有绑定数据库资源时跳过：迁移不该成为无资源组件的阻塞项
		return nil
	}

	port := envOr("DATABASE_PORT", "5432")
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)
	if err != nil {
		return fmt.Errorf("数据库不可达 %s:%s：%w", host, port, err)
	}
	return conn.Close()
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := migrate(); err != nil {
			logger.Error("迁移失败", "error", err.Error())
			os.Exit(1)
		}
		logger.Info("迁移完成")
		return
	}

	srv := newServerFromEnv()
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("组件已启动",
			"component", srv.componentID, "version", srv.version,
			"hello_endpoint", srv.helloEndpoint, "event_bus", srv.busEndpoint != "")
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("服务异常退出", "error", err.Error())
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("优雅退出失败", "error", err.Error())
	}
	logger.Info("组件已退出")
}
