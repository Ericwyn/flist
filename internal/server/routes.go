package server

import (
	"log/slog"
	"net/http"

	"flist/internal/config"
	"flist/internal/handler"
	mw "flist/internal/middleware"
	"flist/internal/service"
	"flist/web"

	"github.com/go-chi/chi/v5"
)

// Deps 汇集路由注册所需的依赖。
type Deps struct {
	Config *config.Config
	Auth   *service.AuthService
	Files  *service.FileService
	Logger *slog.Logger
}

// NewRouter 装配中间件管道与路由。
func NewRouter(d Deps) (http.Handler, error) {
	r := chi.NewRouter()

	// 全局中间件管道（顺序见 0.backend-design.md §10）。
	r.Use(mw.Recovery(d.Logger))
	r.Use(mw.RequestID)
	r.Use(mw.Logger(d.Logger))
	r.Use(mw.RateLimit(50, 50))
	r.Use(mw.CORS(d.Config.CORSOrigin))
	r.Use(mw.SecurityHeaders)

	authHandler := handler.NewAuthHandler(d.Auth, d.Config.SessionTTL)
	systemHandler := handler.NewSystemHandler()
	fileHandler := handler.NewFileHandler(d.Files)

	r.Route("/api", func(api chi.Router) {
		// 公开路由（无需认证）。
		api.Get("/system/health", systemHandler.Health)
		api.Post("/auth/login", authHandler.Login)

		// 受保护路由。
		api.Group(func(protected chi.Router) {
			protected.Use(mw.Auth(d.Auth))
			protected.Use(mw.PathGuard)

			protected.Post("/auth/logout", authHandler.Logout)
			protected.Get("/auth/me", authHandler.Me)
			protected.Put("/auth/password", authHandler.ChangePassword)

			// Phase 1 只读文件接口。
			protected.Get("/fs/list", fileHandler.List)
			protected.Get("/fs/stat", fileHandler.Stat)
			protected.Get("/fs/preview", fileHandler.Preview)
			protected.Get("/fs/download", fileHandler.Download)
		})

		// 未匹配的 API 路径返回 404（统一信封）。
		api.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			handler.Fail(w, http.StatusNotFound, handler.CodeBadRequest, "not_found")
		})
	})

	// 前端静态资源 + SPA fallback（兜底所有非 /api 路径）。
	staticHandler, err := web.Handler()
	if err != nil {
		return nil, err
	}
	r.NotFound(staticHandler.ServeHTTP)

	return r, nil
}
