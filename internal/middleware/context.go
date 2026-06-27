package middleware

import (
	"context"
	"encoding/json"
	"net"
	"net/http"

	"flist/internal/model"
)

type ctxKey int

const (
	ctxKeyUser ctxKey = iota
	ctxKeySessionID
	ctxKeyRequestID
)

// WithUser 将用户与会话 ID 注入上下文。
func WithUser(ctx context.Context, user *model.User, sessionID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyUser, user)
	ctx = context.WithValue(ctx, ctxKeySessionID, sessionID)
	return ctx
}

// UserFromContext 取出当前请求的用户，未认证时返回 nil。
func UserFromContext(ctx context.Context) *model.User {
	u, _ := ctx.Value(ctxKeyUser).(*model.User)
	return u
}

// SessionIDFromContext 取出当前会话 ID（令牌哈希）。
func SessionIDFromContext(ctx context.Context) string {
	s, _ := ctx.Value(ctxKeySessionID).(string)
	return s
}

// RequestIDFromContext 取出请求 ID。
func RequestIDFromContext(ctx context.Context) string {
	s, _ := ctx.Value(ctxKeyRequestID).(string)
	return s
}

// ClientIP 从请求中提取客户端 IP（不含端口）。
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// writeError 写出统一错误响应信封 {code, message, data}。
// 中间件层不依赖 handler 包，故内联一份最小实现。
func writeError(w http.ResponseWriter, status, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    code,
		"message": message,
		"data":    nil,
	})
}
