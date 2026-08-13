// Command server 是 BrickKit Market（组件市场后端）的入口。
//
// 市场只回答两个问题（001 §6）：有什么可以装？谁有权装？
// 它不安装组件、不运行组件、不管运行状态。
//
// 配置全部来自环境变量（运维指南 §5.1），启动顺序：
// 读配置 → 连库并建表 → 连对象存储并建 bucket → 引导管理员 → 监听。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/brickkit/market-server/internal/config"
	"github.com/brickkit/market-server/internal/handler"
	"github.com/brickkit/market-server/internal/repo"
	"github.com/brickkit/market-server/internal/service"
	"github.com/brickkit/market-server/internal/storage"
)

// version 由构建时注入：go build -ldflags "-X main.version=..."。
var version = "dev"

// shutdownTimeout 是收到停止信号后等待在途请求结束的时间。
// 上传/下载产物可能正在传输，直接切断会留下半截文件。
const shutdownTimeout = 15 * time.Second

func main() {
	if err := run(); err != nil {
		log.Printf("市场启动失败：%v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	cfg.Version = version
	log.Printf("BrickKit Market %s 启动中：%s", version, cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 数据库：建表放在启动流程里，部署时不需要额外跑一次迁移命令。
	// 表结构用的是 CREATE TABLE IF NOT EXISTS，重启是幂等的。
	db, err := repo.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("连接数据库失败：%w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("初始化库表失败：%w", err)
	}

	// 对象存储：bucket 不存在时自动创建，省掉运维指南里手工建桶那一步
	store, err := storage.NewS3Store(cfg.Storage)
	if err != nil {
		return fmt.Errorf("连接对象存储失败：%w", err)
	}
	if err := store.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("准备 bucket %s 失败：%w", cfg.Storage.Bucket, err)
	}

	svc := service.New(db, store, service.Options{TokenTTL: cfg.TokenTTL})
	if err := svc.EnsureAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		return fmt.Errorf("引导管理员账号失败：%w", err)
	}

	server := &http.Server{
		Addr:    ":" + strconv.Itoa(cfg.Port),
		Handler: handler.New(svc, handler.Options{Version: version}),
		// 读超时要能容下产物上传，写超时要能容下产物下载
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	errs := make(chan error, 1)
	go func() {
		log.Printf("市场已就绪，监听 %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return fmt.Errorf("监听失败：%w", err)
	case <-ctx.Done():
		log.Printf("收到停止信号，等待在途请求结束（最多 %s）", shutdownTimeout)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("关闭服务失败：%w", err)
	}
	log.Print("市场已停止")
	return nil
}
