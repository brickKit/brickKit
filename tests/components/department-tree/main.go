// department/tree 是 BrickKit 的部门树组件：一个**叶子组件**——
// 不依赖任何其他组件，只依赖一个 database 资源。
//
// 它同时是平台的验证夹具，验证四件事：
//   - 单端口双协议（HTTP + gRPC 共用 deployment.port）
//   - gRPC Reflection（不带 .proto 也能调）
//   - migration（平台在启动前执行，必须幂等）
//   - artifacts（proto 与 openapi 作为产物发布到市场）
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

func main() {
	if err := run(os.Args[1:]); err != nil {
		// 启动阶段的失败先于日志器存在，直接写 stderr
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("组件启动失败", "error", err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	logger := newLogger(os.Stdout, cfg.LogLevel, cfg.ComponentID)

	store, err := newPostgresStore(cfg.Database.DSN())
	if err != nil {
		return errors.New("连接数据库失败：" + err.Error())
	}
	defer func() { _ = store.Close() }()

	// migrate 子命令：平台在启动组件之前单独跑一次（002 §8.2、005 §6）
	if len(args) > 0 && args[0] == "migrate" {
		logger.Info("开始执行数据库迁移", "config", cfg.String())
		if err := migrate(context.Background(), store); err != nil {
			return errors.New("迁移失败：" + err.Error())
		}
		logger.Info("迁移完成")
		return nil
	}

	return serve(cfg, store, logger)
}

func serve(cfg config, store Store, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	srv := newServer(newService(store, cfg))
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
