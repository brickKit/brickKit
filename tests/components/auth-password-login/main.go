// auth/password-login 是 BrickKit 的口令登录组件：校验用户名口令、签发 JWT。
//
// 它同时是平台的验证夹具，验证一件 department/tree 与 people/basic 都没覆盖的事：
// **一个强依赖别人、但自己没有可展示数据的组件**——它的全部价值在于"能不能
// 在依赖正常/异常时给出正确的判断"，而不是"能不能查出一张表"。
//
// 职责边界（这是它最值得抄的地方）：
//
//	本组件   只管"怎么证明你是你"：用户名 → 口令哈希 → 令牌
//	people/basic  管"你是谁"：姓名、部门、职务
//
// 分开之后，员工从人员系统里消失，凭据表哪怕还留着也登录不进来——
// 主体的存废由人员系统说了算，不会出现两份会漂移的身份数据。
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

// 运行模式。迁移容器与主容器用的是同一个镜像，靠参数区分（002 §8.4）。
const (
	modeServe   = "serve"
	modeMigrate = "migrate"
)

// parseArgs 认参数：要么启动服务，要么执行迁移，没有第三种。
//
// **不认识的参数必须报错，绝不能回落到"那就启动服务吧"。** 否则一个拼错的
// 迁移命令会让迁移容器变成服务容器：它永不退出，主服务永远等不到"迁移完成"，
// 整个项目卡在 Created——而日志里写着"组件已就绪"（002 §8.5.1）。
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

	// 签发器在这里构造，弱密钥会让组件**根本起不来**（见 newTokenIssuer）。
	// 一个用弱密钥跑着的认证组件，表面上一切正常，只有被攻破时才会发现
	issuer, err := newTokenIssuer(cfg.JWTSecret, cfg.TokenTTL, nil)
	if err != nil {
		return err
	}

	return serve(cfg, store, issuer, logger)
}

// runMigrate 处理 migrate 及其子命令。
//
//	migrate           执行尚未执行过的迁移
//	migrate down [n]  回退最近 n 个（默认 1）
//	migrate reset     全部回退（开发与测试用）
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

func serve(cfg config, store Store, issuer *tokenIssuer, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	svc := newService(store, newPeopleClient(cfg.PeopleEndpoint), issuer, cfg)
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
