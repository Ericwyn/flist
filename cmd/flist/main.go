package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"flist/internal/config"
	"flist/internal/server"
	"flist/internal/service"
	"flist/internal/store"
	"flist/internal/util"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}

	// 启动时标准化并校验 root（必须已存在的目录）。
	rootReal, err := util.ResolveRoot(cfg.Root)
	if err != nil {
		return err
	}
	logger.Info("root resolved", slog.String("root", rootReal))

	st, err := store.Open(cfg.Data)
	if err != nil {
		return err
	}
	defer st.Close()

	authSvc := service.NewAuthService(st, cfg.SessionTTL, logger)

	created, genPass, err := authSvc.EnsureAdmin(cfg.AdminUser, cfg.AdminPass)
	if err != nil {
		return err
	}
	if created {
		if genPass != "" {
			logger.Warn("initial admin created with generated password; change it after first login",
				slog.String("username", cfg.AdminUser),
				slog.String("password", genPass),
			)
		} else {
			logger.Info("initial admin created", slog.String("username", cfg.AdminUser))
		}
	}

	// 后台定时清理过期会话。
	cleanupCtx, stopCleanup := context.WithCancel(context.Background())
	defer stopCleanup()
	go runSessionCleanup(cleanupCtx, authSvc, logger)

	router, err := server.NewRouter(server.Deps{
		Config: cfg,
		Auth:   authSvc,
		Logger: logger,
	})
	if err != nil {
		return err
	}

	srv := server.New(cfg.Addr, router, logger)

	// 在独立 goroutine 启动，主 goroutine 等待信号。
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		stopCleanup()
		return srv.Shutdown(10 * time.Second)
	}
}

// runSessionCleanup 每小时清理一次过期会话，直到 ctx 取消。
func runSessionCleanup(ctx context.Context, auth *service.AuthService, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := auth.CleanupExpiredSessions()
			if err != nil {
				logger.Error("session cleanup failed", slog.Any("error", err))
				continue
			}
			if n > 0 {
				logger.Info("expired sessions cleaned", slog.Int64("count", n))
			}
		}
	}
}
