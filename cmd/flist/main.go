package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"flist/internal/config"
	"flist/internal/server"
	"flist/internal/service"
	"flist/internal/storage"
	"flist/internal/storage/local"
	"flist/internal/store"
	"flist/internal/util"
)

func main() {
	// 用 LevelVar 持有日志级别，便于配置加载后按 --debug 动态调整。
	levelVar := new(slog.LevelVar) // 默认 Info
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: levelVar}))
	slog.SetDefault(logger)

	if err := run(logger, levelVar); err != nil {
		logger.Error("fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger, levelVar *slog.LevelVar) error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}

	// --debug：把日志级别降到 Debug，输出上传分片等细节。
	if cfg.Debug {
		levelVar.Set(slog.LevelDebug)
		logger.Info("debug logging enabled")
	}

	st, err := store.Open(cfg.Data)
	if err != nil {
		return err
	}
	defer st.Close()

	authSvc := service.NewAuthService(st, cfg.SessionTTL, logger)

	// --reset-admin：重置管理员凭据为 admin + 随机密码后退出，不启动服务。
	if cfg.ResetAdmin {
		genPass, err := authSvc.ResetAdmin("admin", "")
		if err != nil {
			return fmt.Errorf("reset admin failed: %w", err)
		}
		logger.Info("admin credentials reset; login and change it in settings",
			slog.String("username", "admin"),
			slog.String("password", genPass),
		)
		return nil
	}

	// 启动时标准化并校验 root（必须已存在的目录）。
	rootReal, err := util.ResolveRoot(cfg.Root)
	if err != nil {
		return err
	}
	logger.Info("root resolved", slog.String("root", rootReal))

	// 分片上传暂存根目录（与 root 解耦，置于数据目录下，便于整洁与回收）。
	stagingDir := filepath.Join(cfg.Data, "uploads.tmp")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return fmt.Errorf("create upload staging dir: %w", err)
	}

	backend, err := buildBackend(cfg, rootReal, stagingDir, logger)
	if err != nil {
		return err
	}
	fileSvc := service.NewFileService(backend)
	bookmarkSvc := service.NewBookmarkService(st, backend)
	uploadSvc := service.NewUploadService(backend, util.NewPathLocker(), cfg.MaxUpload)

	created, genPass, err := authSvc.EnsureAdmin("admin", "")
	if err != nil {
		return err
	}
	if created {
		logger.Warn("initial admin created with generated password; change it after first login",
			slog.String("username", "admin"),
			slog.String("password", genPass),
		)
	}

	// 后台定时清理过期会话。
	cleanupCtx, stopCleanup := context.WithCancel(context.Background())
	defer stopCleanup()
	go runSessionCleanup(cleanupCtx, authSvc, logger)
	// 后台定时清理过期上传会话与孤儿暂存分片（24h）。
	go runUploadSweep(cleanupCtx, uploadSvc, logger)

	router, err := server.NewRouter(server.Deps{
		Config:    cfg,
		Auth:      authSvc,
		Files:     fileSvc,
		Bookmarks: bookmarkSvc,
		Uploads:   uploadSvc,
		Logger:    logger,
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

// buildBackend 装配存储驱动。
//
// 当前默认形态：单个本地驱动「透明挂载」在根（与改造前的 --root 行为完全一致，
// 路径语义不变）。这是后续扩展的唯一接入点：当引入 WebDAV / 网盘驱动后，
// 在此构造各驱动并用 storage.NewMux 组合成「本地 + 多网盘」的虚拟命名空间，
// 上层 service / handler 无需任何改动。
//
// 示例（待 webdav 驱动落地后启用）：
//
//	mounts := []storage.Mount{
//	    {Name: "local", Backend: local.New(rootReal, stagingDir)},
//	    {Name: "box1",  Backend: webdav.New(cfg.WebDAV[0])},
//	}
//	return storage.NewMux(mounts), nil
//
// stagingDir 为分片上传暂存根目录，仅本地驱动承载上传时需要（远程驱动有自己的上传实现）。
func buildBackend(_ *config.Config, rootReal, stagingDir string, _ *slog.Logger) (storage.Backend, error) {
	return local.New(rootReal, stagingDir), nil
}

// runUploadSweep 每小时清理一次过期上传会话与孤儿暂存分片（保留窗口 24h），直到 ctx 取消。
func runUploadSweep(ctx context.Context, uploads *service.UploadService, logger *slog.Logger) {
	const maxAge = 24 * time.Hour
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n := uploads.Sweep(maxAge); n > 0 {
				logger.Info("stale upload sessions cleaned", slog.Int("count", n))
			}
		}
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
