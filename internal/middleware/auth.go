package middleware

import (
	"net/http"
	"strings"

	"flist/internal/model"
)

// SessionCookieName 是会话令牌的 Cookie 名。
const SessionCookieName = "flist_session"

// TokenValidator 校验令牌并返回用户与会话 ID（令牌哈希）。
// 由 service.AuthService.Validate 满足，避免 middleware 直接依赖 service 包。
type TokenValidator interface {
	Validate(token string) (*model.User, string, error)
}

// ExtractToken 从 Authorization Bearer 头（优先）或 Cookie 中取出令牌。
func ExtractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth != "" {
		const prefix = "Bearer "
		if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
			return strings.TrimSpace(auth[len(prefix):])
		}
	}
	if c, err := r.Cookie(SessionCookieName); err == nil {
		return c.Value
	}
	return ""
}

// Auth 校验会话令牌，成功则将用户注入上下文，失败返回 401。
func Auth(v TokenValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ExtractToken(r)
			user, sessionID, err := v.Validate(token)
			if err != nil || user == nil {
				writeError(w, http.StatusUnauthorized, 1001, "unauthorized")
				return
			}
			ctx := WithUser(r.Context(), user, sessionID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
