package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"flist/internal/config"
	"flist/internal/service"
	"flist/internal/storage/local"
	"flist/internal/store"
	"flist/internal/util"
)

// newFSTestServer 构造一个带 FileService 的路由，并返回有效令牌与 root 目录。
func newFSTestServer(t *testing.T) (http.Handler, string, string) {
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
	res, err := auth.Login("admin", "secret12", "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}

	rootDir := t.TempDir()
	rootReal, err := util.ResolveRoot(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	files := service.NewFileService(local.New(rootReal, t.TempDir()), util.NewPathLocker(), 5<<20)

	router, err := NewRouter(Deps{
		Config: &config.Config{SessionTTL: time.Hour},
		Auth:   auth,
		Files:  files,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return router, res.Token, rootReal
}

func TestFSList_RequiresAuth(t *testing.T) {
	h, _, _ := newFSTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/fs/list?path=/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", rec.Code)
	}
}

func TestFSList_OK(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644)

	req := httptest.NewRequest(http.MethodGet, "/api/fs/list?path=/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var env map[string]any
	json.Unmarshal(rec.Body.Bytes(), &env)
	if env["code"].(float64) != 0 {
		t.Errorf("expected code 0, got %v", env["code"])
	}
	data := env["data"].(map[string]any)
	if data["total"].(float64) != 1 {
		t.Errorf("expected total 1, got %v", data["total"])
	}
}

func TestFSPreview_Text(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "note.txt"), []byte("preview me"), 0o644)

	req := httptest.NewRequest(http.MethodGet, "/api/fs/preview?path=/note.txt", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var env map[string]any
	json.Unmarshal(rec.Body.Bytes(), &env)
	data := env["data"].(map[string]any)
	if data["type"] != "text" || data["content"] != "preview me" {
		t.Errorf("unexpected preview data: %v", data)
	}
}

func TestFSDownload_Full(t *testing.T) {
	h, token, root := newFSTestServer(t)
	content := "0123456789"
	os.WriteFile(filepath.Join(root, "dl.txt"), []byte(content), 0o644)

	req := httptest.NewRequest(http.MethodGet, "/api/fs/download?path=/dl.txt", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != content {
		t.Errorf("body mismatch: %q", rec.Body.String())
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("expected ETag header")
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Error("expected Accept-Ranges: bytes")
	}
}

func TestFSDownload_Range(t *testing.T) {
	h, token, root := newFSTestServer(t)
	content := "0123456789"
	os.WriteFile(filepath.Join(root, "dl.txt"), []byte(content), 0o644)

	req := httptest.NewRequest(http.MethodGet, "/api/fs/download?path=/dl.txt", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rec.Code)
	}
	if rec.Body.String() != "2345" {
		t.Errorf("range body mismatch: %q", rec.Body.String())
	}
	if cr := rec.Header().Get("Content-Range"); cr != "bytes 2-5/10" {
		t.Errorf("unexpected Content-Range: %q", cr)
	}
}

func TestFSDownload_IfNoneMatch(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "dl.txt"), []byte("cacheme"), 0o644)

	// 先取得 ETag。
	req1 := httptest.NewRequest(http.MethodGet, "/api/fs/download?path=/dl.txt", nil)
	req1.Header.Set("Authorization", "Bearer "+token)
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}

	// 带 If-None-Match 再请求应得 304。
	req2 := httptest.NewRequest(http.MethodGet, "/api/fs/download?path=/dl.txt", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Errorf("expected 304, got %d", rec2.Code)
	}
}

func TestFSDownload_Dir(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.MkdirAll(filepath.Join(root, "adir"), 0o755)

	req := httptest.NewRequest(http.MethodGet, "/api/fs/download?path=/adir", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for dir download, got %d", rec.Code)
	}
	var env map[string]any
	json.Unmarshal(rec.Body.Bytes(), &env)
	if env["code"].(float64) != 2007 {
		t.Errorf("expected code 2007, got %v", env["code"])
	}
}

func TestFSStat_NotFound(t *testing.T) {
	h, token, _ := newFSTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/fs/stat?path=/ghost", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	var env map[string]any
	json.Unmarshal(rec.Body.Bytes(), &env)
	if env["code"].(float64) != 2001 {
		t.Errorf("expected code 2001, got %v", env["code"])
	}
}

func TestFSList_PathGuardBlocksTraversal(t *testing.T) {
	h, token, _ := newFSTestServer(t)
	// PathGuard 应拦截显式 .. 越界参数。
	req := httptest.NewRequest(http.MethodGet, "/api/fs/list?path=/../../etc", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 from PathGuard, got %d", rec.Code)
	}
	var env map[string]any
	json.Unmarshal(rec.Body.Bytes(), &env)
	if env["code"].(float64) != 2002 {
		t.Errorf("expected code 2002, got %v", env["code"])
	}
}
