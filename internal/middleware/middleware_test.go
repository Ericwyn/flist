package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"flist/internal/model"
)

// stubValidator 实现 TokenValidator 用于测试 Auth 中间件。
type stubValidator struct {
	user *model.User
	sid  string
	err  error
}

func (s stubValidator) Validate(token string) (*model.User, string, error) {
	if s.err != nil {
		return nil, "", s.err
	}
	return s.user, s.sid, nil
}

func TestAuth_InjectsUser(t *testing.T) {
	v := stubValidator{user: &model.User{ID: 1, Username: "admin"}, sid: "sid1"}
	var gotUser *model.User
	h := Auth(v)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotUser == nil || gotUser.Username != "admin" {
		t.Errorf("user not injected: %+v", gotUser)
	}
}

func TestAuth_Rejects(t *testing.T) {
	v := stubValidator{err: http.ErrNoCookie}
	h := Auth(v)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be reached")
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestExtractToken(t *testing.T) {
	// Bearer 头优先。
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer mytoken")
	if got := ExtractToken(req); got != "mytoken" {
		t.Errorf("bearer: got %q", got)
	}

	// 回落到 Cookie。
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "cookietoken"})
	if got := ExtractToken(req); got != "cookietoken" {
		t.Errorf("cookie: got %q", got)
	}

	// 都没有则空。
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	if got := ExtractToken(req); got != "" {
		t.Errorf("none: got %q", got)
	}
}

func TestPathGuard(t *testing.T) {
	reached := false
	h := PathGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		query   string
		blocked bool
	}{
		{"path=/docs/a.txt", false},
		{"path=/docs/../a.txt", false}, // 清理后无 .. ，合法
		{"path=/../../etc/passwd", true},
		{"path=" + "C:\\Windows", true},
		{"path=/normal", false},
	}
	for _, c := range cases {
		reached = false
		req := httptest.NewRequest(http.MethodGet, "/x?"+c.query, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if c.blocked && reached {
			t.Errorf("query %q should have been blocked", c.query)
		}
		if !c.blocked && !reached {
			t.Errorf("query %q should have passed (status %d)", c.query, rec.Code)
		}
	}
}
