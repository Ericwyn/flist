package middleware

import "net/http"

// CORS 在配置了 allowOrigin 时按白名单放行跨域请求（前后端分离调试用）。
// allowOrigin 为空时返回直通中间件，不写任何 CORS 头（同源部署）。
func CORS(allowOrigin string) func(http.Handler) http.Handler {
	if allowOrigin == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && origin == allowOrigin {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", allowOrigin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-Id")
				h.Add("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
