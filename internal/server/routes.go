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
	Config    *config.Config
	Auth      *service.AuthService
	Files     *service.FileService
	Bookmarks *service.BookmarkService
	Uploads   *service.UploadService
	Logger    *slog.Logger
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
	fileHandler := handler.NewFileHandler(d.Files, d.Uploads, d.Logger)
	bookmarkHandler := handler.NewBookmarkHandler(d.Bookmarks, d.Logger)

	// 写操作限流：10/s per IP（见 0.backend-design.md §9.3）。
	writeLimit := mw.NewWriteRateLimiter(10, 10)

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
			protected.Put("/auth/username", authHandler.ChangeUsername)

			// Phase 1 只读文件接口。
			protected.Get("/fs/list", fileHandler.List)
			protected.Get("/fs/stat", fileHandler.Stat)
			protected.Get("/fs/preview", fileHandler.Preview)
			protected.Get("/fs/download", fileHandler.Download)

			// Phase 5 打包下载（只读型重操作，流式 zip；仅受全局限流，不套写限流）。
			protected.Post("/fs/archive", fileHandler.Archive)

			// Phase 2 搜索（只读，仅受全局限流 + 自身超时/数量上限保护）。
			protected.Get("/fs/search", fileHandler.Search)

			// Phase 2 写操作（额外套写限流 10/s）。
			protected.Group(func(wr chi.Router) {
				wr.Use(writeLimit)
				wr.Post("/fs/mkdir", fileHandler.Mkdir)
				wr.Post("/fs/touch", fileHandler.Touch)
				wr.Post("/fs/move", fileHandler.Move)
				wr.Delete("/fs/delete", fileHandler.Delete)
				// Phase 3 复制（写操作）。
				wr.Post("/fs/copy", fileHandler.Copy)
				// Phase 4 上传 init / complete（写操作；分片本身见下方，走全局限流）。
				wr.Post("/fs/upload/init", fileHandler.UploadInit)
				wr.Post("/fs/upload/complete", fileHandler.UploadComplete)
			})

			// Phase 4 分片上传：仅受全局限流（50/s），避免大文件多分片被写限流 10/s 拖慢。
			protected.Post("/fs/upload/chunk", fileHandler.UploadChunk)

			// Phase 3 收藏夹（元数据操作，仅受全局限流）。
			protected.Get("/bookmarks", bookmarkHandler.List)
			protected.Post("/bookmarks", bookmarkHandler.Create)
			protected.Put("/bookmarks/reorder", bookmarkHandler.Reorder) // 须先于 /{id} 注册
			protected.Put("/bookmarks/{id}", bookmarkHandler.Update)
			protected.Delete("/bookmarks/{id}", bookmarkHandler.Delete)
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
