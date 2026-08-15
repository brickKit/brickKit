// authorization/rbac 是 BrickKit 的授权组件：回答"某个人能不能做某件事"。
//
// 它同时是平台的验证夹具，验证两件前面的组件都没覆盖的事：
//   - **cache 资源**（Redis）：一个"有了更快、没有也能跑"的可选依赖
//   - 单端口双协议 + 强依赖 + 缓存三者叠在一起时的降级行为
//
// 权限来自两条路径的并集：
//
//	直接授予这个人的角色  +  授予这个人所在**部门**的角色
//
// 第二条是它强依赖 people/basic 的原因——部门在那边，没有它就算不出完整权限。
//
// 三个依赖，三种不同的故障表现（这是本组件最值得抄的地方）：
//
//	PostgreSQL    数据源，挂了 → 503
//	people/basic  数据源，挂了且缓存未命中 → 503（**不做部分降级**）
//	Redis         加速器，挂了 → 照常回源，只是慢一点
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
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("组件启动失败", "error", err.Error())
		os.Exit(1)
	}
}

// 运行模式。迁移容器与主容器用的是同一个镜像，靠参数区分（002 §8.4）。
const (
	modeServe   = "serve"
	modeMigrate = "migrate"
)

// parseArgs 认参数：要么启动服务，要么执行迁移，没有第三种。
//
// **不认识的参数必须报错，绝不能回落到"那就启动服务吧"**（002 §8.5.1）。
func parseArgs(args []string) (mode string, rest []string, err error) {
	if len(args) == 0 {
		return modeServe, nil, nil
	}
	if args[0] == modeMigrate {
		return modeMigrate, args[1:], nil
	}
	return "", nil, errors.New(
		"未知的参数：" + args[0] + "（可用：不带参数启动服务 | migrate [down [n] | reset]）")
}

func run(args []string) error {
	mode, migrateArgs, err := parseArgs(args)
	if err != nil {
		return err
	}

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

	if mode == modeMigrate {
		return runMigrate(context.Background(), migrateArgs, cfg, store, logger)
	}

	return serve(cfg, store, logger)
}

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

// newCache 按配置选缓存实现。
//
// 没绑定 cache 资源时用内存实现而不是报错：Redis 在这里是加速器不是数据源
// （见 Cache 的说明）。进程内缓存对单副本部署完全够用，多副本时各自缓存、
// 各自按 TTL 过期，也不会算错——只是命中率低一些。
func newCache(cfg config, logger *slog.Logger) (Cache, func() error) {
	if !cfg.Cache.Enabled() {
		logger.Warn("未绑定 cache 资源，改用进程内缓存",
			"提示", "在 brickkit.yaml 中绑定一个 kind: cache / engine: redis 的资源可获得跨副本共享的缓存")
		return newMemoryCache(), func() error { return nil }
	}

	cache := newRedisCache(cfg.Cache)
	return cache, cache.Close
}

func serve(cfg config, store Store, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cache, closeCache := newCache(cfg, logger)
	defer func() { _ = closeCache() }()

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	svc := newService(store, newPeopleClient(cfg.PeopleEndpoint), cache, cfg)
	srv := newServer(svc)

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
