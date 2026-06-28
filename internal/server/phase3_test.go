package server

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"flist/internal/config"
	"flist/internal/service"
	"flist/internal/storage/local"
	"flist/internal/store"
	"flist/internal/util"
)

// newPhase3TestServer 构造带 FileService + BookmarkService 的路由，返回令牌、root 与 store。
func newPhase3TestServer(t *testing.T) (http.Handler, string, string) {
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

	rootReal, err := util.ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := local.New(rootReal, t.TempDir())
	files := service.NewFileService(backend, util.NewPathLocker(), 5<<20)
	bookmarks := service.NewBookmarkService(st, backend)

	router, err := NewRouter(Deps{
		Config:    &config.Config{SessionTTL: time.Hour},
		Auth:      auth,
		Files:     files,
		Bookmarks: bookmarks,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return router, res.Token, rootReal
}

func TestFSCopy(t *testing.T) {
	h, token, root := newPhase3TestServer(t)
	os.WriteFile(filepath.Join(root, "src.txt"), []byte("data"), 0o644)
	os.MkdirAll(filepath.Join(root, "dest"), 0o755)

	_, env := doJSON(t, h, http.MethodPost, "/api/fs/copy", token,
		map[string]any{"src": []string{"/src.txt"}, "dst": "/dest"})
	data := env["data"].(map[string]any)
	results := data["results"].([]any)
	if results[0].(map[string]any)["ok"] != true {
		t.Fatalf("copy not ok: %v", results[0])
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "src.txt")); err != nil {
		t.Error("copy should exist in dest")
	}
	// 源仍在。
	if _, err := os.Stat(filepath.Join(root, "src.txt")); err != nil {
		t.Error("source should remain after copy")
	}
}

func TestFSCopy_AutoRename(t *testing.T) {
	h, token, root := newPhase3TestServer(t)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("orig"), 0o644)
	os.MkdirAll(filepath.Join(root, "dest"), 0o755)
	os.WriteFile(filepath.Join(root, "dest", "a.txt"), []byte("occupied"), 0o644)

	_, env := doJSON(t, h, http.MethodPost, "/api/fs/copy", token,
		map[string]any{"src": []string{"/a.txt"}, "dst": "/dest", "auto_rename": true})
	data := env["data"].(map[string]any)
	results := data["results"].([]any)
	if results[0].(map[string]any)["ok"] != true {
		t.Fatalf("auto-rename copy not ok: %v", results[0])
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "a (2).txt")); err != nil {
		t.Error("expected 'a (2).txt' to exist")
	}
}

func TestBookmarkCRUD_E2E(t *testing.T) {
	h, token, root := newPhase3TestServer(t)
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)

	// 创建。
	_, env := doJSON(t, h, http.MethodPost, "/api/bookmarks", token,
		map[string]any{"name": "文档", "path": "/docs"})
	if env["code"].(float64) != 0 {
		t.Fatalf("create bookmark failed: %v", env)
	}
	created := env["data"].(map[string]any)
	id := int64(created["id"].(float64))

	// 列表含一条且有效。
	_, listEnv := doJSON(t, h, http.MethodGet, "/api/bookmarks", token, nil)
	bms := listEnv["data"].(map[string]any)["bookmarks"].([]any)
	if len(bms) != 1 || bms[0].(map[string]any)["valid"] != true {
		t.Fatalf("unexpected list: %v", bms)
	}

	// 重命名。
	_, upEnv := doJSON(t, h, http.MethodPut, "/api/bookmarks/"+itoa(id), token,
		map[string]any{"name": "我的文档"})
	if upEnv["code"].(float64) != 0 {
		t.Fatalf("update failed: %v", upEnv)
	}

	// 删除。
	_, delEnv := doJSON(t, h, http.MethodDelete, "/api/bookmarks/"+itoa(id), token, nil)
	if delEnv["code"].(float64) != 0 {
		t.Fatalf("delete failed: %v", delEnv)
	}
	_, listEnv2 := doJSON(t, h, http.MethodGet, "/api/bookmarks", token, nil)
	bms2 := listEnv2["data"].(map[string]any)["bookmarks"].([]any)
	if len(bms2) != 0 {
		t.Errorf("expected empty after delete, got %d", len(bms2))
	}
}

func TestBookmark_RejectFile_E2E(t *testing.T) {
	h, token, root := newPhase3TestServer(t)
	os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644)

	_, env := doJSON(t, h, http.MethodPost, "/api/bookmarks", token,
		map[string]any{"path": "/file.txt"})
	if env["code"].(float64) != 2008 {
		t.Errorf("expected 2008 not_a_dir for file bookmark, got %v", env["code"])
	}
}

func TestBookmark_Duplicate_E2E(t *testing.T) {
	h, token, root := newPhase3TestServer(t)
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)

	doJSON(t, h, http.MethodPost, "/api/bookmarks", token, map[string]any{"path": "/docs"})
	_, env := doJSON(t, h, http.MethodPost, "/api/bookmarks", token, map[string]any{"path": "/docs"})
	if env["code"].(float64) != 3001 {
		t.Errorf("expected 3001 bookmark_exists, got %v", env["code"])
	}
}

func TestBookmark_RequiresAuth(t *testing.T) {
	h, _, _ := newPhase3TestServer(t)
	rec, _ := doJSON(t, h, http.MethodGet, "/api/bookmarks", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", rec.Code)
	}
}

func TestBookmark_Reorder_E2E(t *testing.T) {
	h, token, root := newPhase3TestServer(t)
	for _, n := range []string{"a", "b"} {
		os.MkdirAll(filepath.Join(root, n), 0o755)
	}
	_, ea := doJSON(t, h, http.MethodPost, "/api/bookmarks", token, map[string]any{"path": "/a"})
	_, eb := doJSON(t, h, http.MethodPost, "/api/bookmarks", token, map[string]any{"path": "/b"})
	idA := int64(ea["data"].(map[string]any)["id"].(float64))
	idB := int64(eb["data"].(map[string]any)["id"].(float64))

	// 反转：b 在前。
	_, env := doJSON(t, h, http.MethodPut, "/api/bookmarks/reorder", token,
		map[string]any{"orders": []map[string]any{
			{"id": idB, "sort_order": 1},
			{"id": idA, "sort_order": 2},
		}})
	if env["code"].(float64) != 0 {
		t.Fatalf("reorder failed: %v", env)
	}
	_, listEnv := doJSON(t, h, http.MethodGet, "/api/bookmarks", token, nil)
	bms := listEnv["data"].(map[string]any)["bookmarks"].([]any)
	if bms[0].(map[string]any)["path"] != "/b" {
		t.Errorf("reorder failed, first should be /b, got %v", bms[0])
	}
}

// itoa 把 id 转为 URL 路径段。
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
