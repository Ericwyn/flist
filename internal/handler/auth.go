package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"flist/internal/middleware"
	"flist/internal/service"
)

// AuthHandler 处理认证相关接口。
type AuthHandler struct {
	auth        *service.AuthService
	sessionTTL  time.Duration
	secureCookie bool // 生产可置 true（HTTPS 下），Phase 0 默认 false 便于本地调试
}

// NewAuthHandler 构造认证处理器。
func NewAuthHandler(auth *service.AuthService, sessionTTL time.Duration) *AuthHandler {
	return &AuthHandler{auth: auth, sessionTTL: sessionTTL}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Username  string `json:"username"`
}

// Login 处理 POST /api/auth/login。
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "bad_request")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		failBadRequest(w, "username and password required")
		return
	}

	res, err := h.auth.Login(req.Username, req.Password, middleware.ClientIP(r))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAccountLocked):
			Fail(w, http.StatusTooManyRequests, CodeAccountLocked, "account_locked")
		case errors.Is(err, service.ErrInvalidCredentials):
			Fail(w, http.StatusUnauthorized, CodeInvalidCredentials, "invalid_credentials")
		default:
			failInternal(w)
		}
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    res.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
		Expires:  res.ExpiresAt,
	})

	OK(w, loginResponse{
		Token:     res.Token,
		ExpiresAt: res.ExpiresAt.Unix(),
		Username:  res.User.Username,
	})
}

// Logout 处理 POST /api/auth/logout。
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sessionID := middleware.SessionIDFromContext(r.Context())
	if sessionID != "" {
		if err := h.auth.Logout(sessionID); err != nil {
			failInternal(w)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	OK(w, nil)
}

// Me 处理 GET /api/auth/me。
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		failUnauthorized(w)
		return
	}
	OK(w, map[string]any{
		"id":       user.ID,
		"username": user.Username,
	})
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangePassword 处理 PUT /api/auth/password。
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		failUnauthorized(w)
		return
	}
	var req changePasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "bad_request")
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		failBadRequest(w, "old_password and new_password required")
		return
	}

	sessionID := middleware.SessionIDFromContext(r.Context())
	err := h.auth.ChangePassword(user.ID, sessionID, req.OldPassword, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			Fail(w, http.StatusUnauthorized, CodeInvalidCredentials, "invalid_credentials")
		case errors.Is(err, service.ErrWeakPassword), errors.Is(err, service.ErrPasswordTooLong):
			Fail(w, http.StatusBadRequest, CodeWeakPassword, "weak_password")
		default:
			failInternal(w)
		}
		return
	}
	OK(w, nil)
}
