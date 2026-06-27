package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler 返回托管前端静态产物的处理器，并实现 SPA history 路由 fallback：
//   - 命中真实静态文件 → 直接返回该文件
//   - 路径以 /api/ 开头 → 返回 404（交由 API 路由处理，不被静态接管）
//   - 其余未命中路径 → 回退返回 index.html（支持刷新子路由不 404）
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))

	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API 路径不被静态服务接管。
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		reqPath := strings.TrimPrefix(r.URL.Path, "/")
		if reqPath == "" {
			serveIndex(w, index)
			return
		}

		// 命中真实文件则直接交给 FileServer。
		if f, err := sub.Open(reqPath); err == nil {
			if st, serr := f.Stat(); serr == nil && !st.IsDir() {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
			f.Close()
		}

		// 未命中：SPA fallback 回退到 index.html。
		serveIndex(w, index)
	}), nil
}

func serveIndex(w http.ResponseWriter, index []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(index)
}
