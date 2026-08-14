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
	"strconv"
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
		return runMigrate(context.Background(), args[1:], cfg, store, logger)
	}

	return serve(cfg, store, logger)
}

// runMigrate 处理 migrate 及其子命令。
//
//	migrate           执行尚未执行过的迁移
//	migrate down [n]  回退最近 n 个（默认 1）
//	migrate reset     全部回退（开发与测试用）
//
// down / reset 是给开发和测试用的，让人能反复把库搭起来、拆掉。
// 生产环境的结构问题请用一个新的 up 迁移去修（002 §8.9）。
func runMigrate(
	ctx context.Context, args []string, cfg config, store *postgresStore, logger *slog.Logger,
) error {
	if len(args) == 0 {
		logger.Info("开始执行数据库迁移", "config", cfg.String())
		if err := store.migrate(ctx, cfg.ComponentID); err != nil {
			return errors.New("迁移失败：" + err.Error())
		}
		logger.Info("迁移完成")
		return nil
	}

	switch args[0] {
	case "down":
		count := 1
		if len(args) > 1 {
			parsed, err := strconv.Atoi(args[1])
			if err != nil || parsed < 1 {
				return errors.New("migrate down 的参数必须是正整数，当前是：" + args[1])
			}
			count = parsed
		}
		logger.Info("开始回退迁移", "count", count)
		if err := store.rollback(ctx, cfg.ComponentID, count); err != nil {
			return errors.New("回退失败：" + err.Error())
		}
		logger.Info("回退完成", "count", count)
		return nil

	case "reset":
		logger.Warn("开始全部回退（该操作会删除本组件的表与数据）")
		if err := store.rollback(ctx, cfg.ComponentID, 0); err != nil {
			return errors.New("回退失败：" + err.Error())
		}
		logger.Info("已全部回退")
		return nil

	default:
		return errors.New("未知的 migrate 子命令：" + args[0] + "（可用：down [n] | reset）")
	}
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
