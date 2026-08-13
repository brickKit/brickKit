// demo/hello 是 BrickKit 平台的自测组件：一个最小的 HTTP 组件。
//
// 它存在的目的不是业务，而是让平台自己有东西可测——环境变量注入、
// 部署文件生成、健康检查、多版本共存都需要一个真的能跑起来的容器。
//
// 组件开发约束（002 §1.4）：
//   - 配置只从环境变量读取，不硬编码
//   - /healthz 只检查本进程存活（§9.4）
//   - 日志为 JSON，输出到 stdout
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// 主端口固定 8080，与 component.yaml 的 deployment.port 一致。
const addr = ":8080"

// platformEnvKeys 是回显给调用方的环境变量（004 §5.6 平台注入的那几类）。
var platformEnvKeys = []string{
	"COMPONENT_ID",
	"COMPONENT_VERSION",
	"GREETING",
}

type server struct {
	componentID string
	version     string
	greeting    string
}

func newServerFromEnv() *server {
	return &server{
		componentID: envOr("COMPONENT_ID", "demo/hello"),
		version:     envOr("COMPONENT_VERSION", "1.0.0"),
		greeting:    envOr("GREETING", "你好"),
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
	mux.HandleFunc("/api/v1/hello", s.handleHello)
	mux.HandleFunc("/api/v1/env", s.handleEnv)
	return mux
}

// handleHealthz 只回答"本进程还活着吗"。
// 002 §9.4 明令禁止在这里检查数据库、依赖组件或任何外部系统。
func (s *server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *server) handleHello(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"component": s.componentID,
		"version":   s.version,
		"greeting":  s.greeting,
		"message":   s.greeting + "，我是 " + s.componentID + "@" + s.version,
	})
}

// handleEnv 回显平台注入的环境变量，供 CLI 验证注入结果。
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

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	srv := newServerFromEnv()

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("组件已启动",
			"component", srv.componentID, "version", srv.version, "addr", addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("服务异常退出", "error", err.Error())
			os.Exit(1)
		}
	}()

	// 容器停止时优雅退出：docker stop / K8s 都是先发 SIGTERM
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
