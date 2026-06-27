package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Server 包裹 http.Server，提供启动与优雅关闭。
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// New 构造服务器。
func New(addr string, handler http.Handler, logger *slog.Logger) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// Start 在前台监听并阻塞，直到出错或 ListenAndServe 返回。
func (s *Server) Start() error {
	s.logger.Info("server listening", slog.String("addr", s.httpServer.Addr))
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown 在给定超时内优雅关闭。
func (s *Server) Shutdown(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s.logger.Info("server shutting down")
	return s.httpServer.Shutdown(ctx)
}
