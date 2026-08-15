// infra/api-docs 是 BrickKit 的文档聚合组件：把各组件的 API 文档收拢到一个入口。
//
// 它在平台里的位置很特殊：**全部依赖都是弱依赖**。七个目标组件装了几个就展示
// 几个，一个都没装也照样起得来——文档入口不该因为某个业务组件没装就打不开。
// 开发计划 28.3 要验的正是这一点。
//
// 两条发现路径（计划 §0.2：Swagger UI + gRPC Reflection 聚合）：
//
//	OpenAPI    GET {endpoint}/openapi.json —— FastAPI 之类的框架自带
//	gRPC       Reflection —— **不需要 .proto 文件**，组件升级加了新方法也自动跟上
//
// 两条都没有的组件如实显示"没有可展示的文档"，而不是让页面空着让人猜。
//
// 组件开发约束（002 §1.4）：配置只从环境变量读、/healthz 只检查本进程、
// 日志为 JSON 输出到 stdout、容器不以 root 运行。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// defaultWebRoot 是 Swagger UI 与首页所在目录（见 Dockerfile）。
const defaultWebRoot = "/app/web"

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("组件启动失败", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 {
		// 不认识的参数必须报错，不能默默启动服务（与 002 §8.5.1 同一条道理）
		return errors.New("未知的参数：" + os.Args[1] + "（本组件不接受任何参数）")
	}

	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	logger := newLogger(os.Stdout, cfg.LogLevel, cfg.ComponentID)

	webRoot := defaultWebRoot
	if custom := os.Getenv("WEB_ROOT"); custom != "" {
		webRoot = custom
	}

	svc := newService(NewDiscoverer(), cfg, logger, webRoot)
	return serve(cfg, svc, logger)
}

func serve(cfg config, svc *service, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	srv := &http.Server{Handler: svc.routes(), ReadHeaderTimeout: readHeaderTimeout}

	errs := make(chan error, 1)
	go func() {
		logger.Info("组件已就绪", "addr", listenAddr, "config", cfg.String())
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		logger.Info("收到停止信号，正在关闭")
	}
	return srv.Close()
}
