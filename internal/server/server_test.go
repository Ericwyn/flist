package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"flist/internal/config"
	"flist/internal/service"
	"flist/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=foreign_keys(ON)"
	st, err := store.OpenWithDSN(dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	auth := service.NewAuthService(st, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, _, err := auth.EnsureAdmin("admin", "secret12"); err != nil {
		t.Fatal(err)
	}

	router, err := NewRouter(Deps{
		Config: &config.Config{SessionTTL: time.Hour},
		Auth:   auth,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func doJSON(t *testing.T, h http.Handler, method, path, token string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var env map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
	}
	return rec, env
}

func TestHealth(t *testing.T) {
	h := newTestServer(t)
	rec, env := doJSON(t, h, http.MethodGet, "/api/system/health", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if env["code"].(float64) != 0 {
		t.Errorf("expected code 0, got %v", env["code"])
	}
}

func TestLoginMeLogoutFlow(t *testing.T) {
	h := newTestServer(t)

	// 1. 登录获取令牌。
	rec, env := doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": "admin", "password": "secret12",
	})
	if rec.Code != http.StatusOK || env["code"].(float64) != 0 {
		t.Fatalf("login failed: status=%d env=%v", rec.Code, env)
	}
	data := env["data"].(map[string]any)
	token := data["token"].(string)
	if token == "" {
		t.Fatal("expected token")
	}

	// 2. 带令牌访问 me 成功。
	rec, env = doJSON(t, h, http.MethodGet, "/api/auth/me", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("me failed: status=%d", rec.Code)
	}
	if env["data"].(map[string]any)["username"] != "admin" {
		t.Errorf("unexpected me data: %v", env["data"])
	}

	// 3. 登出。
	rec, _ = doJSON(t, h, http.MethodPost, "/api/auth/logout", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout failed: status=%d", rec.Code)
	}

	// 4. 登出后令牌失效。
	rec, _ = doJSON(t, h, http.MethodGet, "/api/auth/me", token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after logout, got %d", rec.Code)
	}
}

func TestMe_NoToken(t *testing.T) {
	h := newTestServer(t)
	rec, env := doJSON(t, h, http.MethodGet, "/api/auth/me", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if env["code"].(float64) != 1001 {
		t.Errorf("expected code 1001, got %v", env["code"])
	}
}

func TestMe_BadToken(t *testing.T) {
	h := newTestServer(t)
	rec, _ := doJSON(t, h, http.MethodGet, "/api/auth/me", "garbage", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	h := newTestServer(t)
	rec, env := doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": "admin", "password": "wrong",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if env["code"].(float64) != 1002 {
		t.Errorf("expected code 1002, got %v", env["code"])
	}
}

func TestStaticFallback(t *testing.T) {
	h := newTestServer(t)

	// 未知子路由应回退到 index.html（SPA history 路由）。
	req := httptest.NewRequest(http.MethodGet, "/some/spa/route", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA fallback, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected html content-type, got %q", ct)
	}

	// 未匹配的 API 路径不应回退到 index.html，应返回 JSON 404。
	req = httptest.NewRequest(http.MethodGet, "/api/nonexistent", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown API, got %d", rec.Code)
	}
}
