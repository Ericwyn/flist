package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recovery 捕获 handler 中的 panic，记录堆栈并返回 500。
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						slog.Any("panic", rec),
						slog.String("path", r.URL.Path),
						slog.String("request_id", RequestIDFromContext(r.Context())),
						slog.String("stack", string(debug.Stack())),
					)
					writeError(w, http.StatusInternalServerError, 9001, "internal_error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
