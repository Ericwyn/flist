package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFSMkdir_AndList(t *testing.T) {
	h, token, root := newFSTestServer(t)

	rec, env := doJSON(t, h, http.MethodPost, "/api/fs/mkdir", token, map[string]string{"path": "/newdir"})
	if rec.Code != http.StatusOK || env["code"].(float64) != 0 {
		t.Fatalf("mkdir failed: code=%d env=%v", rec.Code, env)
	}
	if fi, err := os.Stat(filepath.Join(root, "newdir")); err != nil || !fi.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}

	// 再次创建应冲突 2004。
	_, env2 := doJSON(t, h, http.MethodPost, "/api/fs/mkdir", token, map[string]string{"path": "/newdir"})
	if env2["code"].(float64) != 2004 {
		t.Errorf("expected 2004 file_exists, got %v", env2["code"])
	}
}

func TestFSTouch_AndStat(t *testing.T) {
	h, token, _ := newFSTestServer(t)

	rec, env := doJSON(t, h, http.MethodPost, "/api/fs/touch", token, map[string]string{"path": "/empty.txt"})
	if rec.Code != http.StatusOK || env["code"].(float64) != 0 {
		t.Fatalf("touch failed: %v", env)
	}
	_, statEnv := doJSON(t, h, http.MethodGet, "/api/fs/stat?path=/empty.txt", token, nil)
	if statEnv["code"].(float64) != 0 {
		t.Errorf("stat after touch failed: %v", statEnv)
	}
}

func TestFSMove_Rename(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "old.txt"), []byte("data"), 0o644)

	_, env := doJSON(t, h, http.MethodPost, "/api/fs/move", token,
		map[string]any{"src": []string{"/old.txt"}, "dst": "/new.txt"})
	data := env["data"].(map[string]any)
	results := data["results"].([]any)
	first := results[0].(map[string]any)
	if first["ok"] != true {
		t.Fatalf("rename not ok: %v", first)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); !os.IsNotExist(err) {
		t.Error("old.txt should be gone")
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Error("new.txt should exist")
	}
}

func TestFSDelete(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "victim.txt"), []byte("x"), 0o644)

	_, env := doJSON(t, h, http.MethodDelete, "/api/fs/delete", token,
		map[string]any{"paths": []string{"/victim.txt"}})
	data := env["data"].(map[string]any)
	results := data["results"].([]any)
	if results[0].(map[string]any)["ok"] != true {
		t.Fatalf("delete not ok: %v", results[0])
	}
	if _, err := os.Stat(filepath.Join(root, "victim.txt")); !os.IsNotExist(err) {
		t.Error("victim.txt should be gone")
	}
}

func TestFSMkdir_InvalidName(t *testing.T) {
	h, token, _ := newFSTestServer(t)
	// PathGuard 不拦截普通名，但 service 校验非法名。用合法路径形态、非法 basename。
	_, env := doJSON(t, h, http.MethodPost, "/api/fs/mkdir", token, map[string]string{"path": "/foo\x00bar"})
	if env["code"].(float64) != 2006 {
		t.Errorf("expected 2006 name_invalid, got %v", env["code"])
	}
}

func TestFSMkdir_RequiresAuth(t *testing.T) {
	h, _, _ := newFSTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/fs/mkdir", bytes.NewReader([]byte(`{"path":"/x"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", rec.Code)
	}
}

func TestFSDelete_PathGuardBlocksTraversal(t *testing.T) {
	h, token, _ := newFSTestServer(t)
	// PathGuard 在 query 层拦截，但 delete 用 body；越界由 SafeResolve 钳制。
	// 这里验证 body 内的越界路径被钳制到 root 内（不逃逸），删除不存在项返回 path_not_found。
	_, env := doJSON(t, h, http.MethodDelete, "/api/fs/delete", token,
		map[string]any{"paths": []string{"/../../etc/passwd"}})
	data := env["data"].(map[string]any)
	results := data["results"].([]any)
	first := results[0].(map[string]any)
	// 钳制后为 /etc/passwd（root 内），通常不存在 → path_not_found。
	if first["ok"] == true {
		t.Error("traversal path should not succeed")
	}
}

func TestFSSearch(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "report.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "report2.txt"), []byte("y"), 0o644)

	_, env := doJSON(t, h, http.MethodGet, "/api/fs/search?q=report", token, nil)
	if env["code"].(float64) != 0 {
		t.Fatalf("search failed: %v", env)
	}
	data := env["data"].(map[string]any)
	items := data["items"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 search hits, got %d", len(items))
	}
}

func TestFSSearch_MissingQuery(t *testing.T) {
	h, token, _ := newFSTestServer(t)
	_, env := doJSON(t, h, http.MethodGet, "/api/fs/search", token, nil)
	if env["code"].(float64) != 4000 {
		t.Errorf("expected 4000 for missing q, got %v", env["code"])
	}
}
