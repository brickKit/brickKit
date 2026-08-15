// erp/backend 是 BrickKit 的**连接组件**：它自己几乎没有数据，
// 价值全在"把别的组件正确地串起来"。
//
// 它是整条依赖链的汇合点，也是平台最完整的一次验证：
//
//	auth/password-login    强依赖，HTTP  —— 这个令牌是谁的
//	authorization/rbac     强依赖，gRPC  —— 这个人能不能做这件事
//	people/basic           强依赖，gRPC  —— 补全姓名与部门（走 extraPorts 的 9090）
//	infra/redis-event-bus  **弱依赖**    —— 有就发事件，没有就跳过
//
// 三件它独有的验证点：
//
//   - **弱依赖降级**：事件总线缺席或调用失败时，审批照常成功、状态照常改变。
//     若因此让业务失败，弱依赖就成了事实上的强依赖。
//   - **extraPorts 注入**：people/basic 是 Python 组件，grpcio 不能与 HTTP 共用
//     端口，因此平台额外注入 PEOPLE_BASIC_GRPC_ENDPOINT。
//   - **没有资源依赖**：它只依赖组件，不绑定任何 database / cache——
//     这条路径前面几个组件都没走过。
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
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("组件启动失败", "error", err.Error())
		os.Exit(1)
	}
}

// run 启动服务。
//
// 本组件**没有 migrate 子命令**：它不绑定任何资源，没有自己的表。
// Manifest 里也就没有 migration 字段——平台据此不会创建迁移容器。
func run() error {
	if len(os.Args) > 1 {
		// 不认识的参数必须报错，不能默默启动服务（002 §8.5.1 的同一条道理）
		return errors.New("未知的参数：" + os.Args[1] + "（本组件不接受任何参数）")
	}

	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	logger := newLogger(os.Stdout, cfg.LogLevel, cfg.ComponentID)

	authz, err := newAuthorizationClient(cfg.AuthorizationEndpoint)
	if err != nil {
		return errors.New("连接 authorization/rbac 失败：" + err.Error())
	}
	defer func() { _ = authz.Close() }()

	people, err := newPeopleClient(cfg.PeopleGRPCEndpoint)
	if err != nil {
		return errors.New("连接 people/basic 失败：" + err.Error())
	}
	defer func() { _ = people.Close() }()

	bus := newEventBus(cfg.EventBusEndpoint)
	if !bus.Enabled() {
		// 弱依赖缺席是正常状态，但要说一声——否则"为什么没有事件"会查很久
		logger.Warn("事件总线未启用，审批事件不会发出",
			"原因", "平台没有注入 INFRA_REDIS_EVENT_BUS_ENDPOINT（弱依赖 infra/redis-event-bus 未启用）",
			"影响", "仅事件不发送，业务功能不受影响")
	}

	svc := newService(
		newMemoryOrders(seedOrders()),
		newAuthClient(cfg.AuthEndpoint),
		authz, people, bus, cfg,
	)
	return serve(cfg, svc, logger)
}

func serve(cfg config, svc *service, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Handler:           svc.routes(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

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
