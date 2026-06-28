package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestFSContent_ReadAndSave(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "todo.md"), []byte("# Todo\n"), 0o644)

	// 读取。
	_, env := doJSON(t, h, http.MethodGet, "/api/fs/content?path=/todo.md", token, nil)
	if env["code"].(float64) != 0 {
		t.Fatalf("read content failed: %v", env)
	}
	data := env["data"].(map[string]any)
	if data["content"] != "# Todo\n" || data["editable"] != true {
		t.Errorf("unexpected content data: %v", data)
	}
	rev := data["revision"].(map[string]any)["token"].(string)

	// 保存（带正确 revision）。
	_, saveEnv := doJSON(t, h, http.MethodPut, "/api/fs/content", token, map[string]any{
		"path": "/todo.md", "content": "# Todo\n- item\n", "expected_revision": rev,
	})
	if saveEnv["code"].(float64) != 0 {
		t.Fatalf("save failed: %v", saveEnv)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "todo.md")); string(got) != "# Todo\n- item\n" {
		t.Errorf("saved content mismatch: %q", got)
	}
}

func TestFSContent_Conflict(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "todo.md"), []byte("v1"), 0o644)

	// 读取拿到 revision。
	_, env := doJSON(t, h, http.MethodGet, "/api/fs/content?path=/todo.md", token, nil)
	rev := env["data"].(map[string]any)["revision"].(map[string]any)["token"].(string)

	// 外部修改文件，使旧 revision 失效。
	os.WriteFile(filepath.Join(root, "todo.md"), []byte("v2-external"), 0o644)

	rec, conflictEnv := doJSON(t, h, http.MethodPut, "/api/fs/content", token, map[string]any{
		"path": "/todo.md", "content": "v3", "expected_revision": rev,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	if conflictEnv["code"].(float64) != 2012 {
		t.Errorf("expected code 2012, got %v", conflictEnv["code"])
	}
	cdata := conflictEnv["data"].(map[string]any)
	if cdata["current_revision"] == nil {
		t.Error("conflict should carry current_revision")
	}
	// 文件不应被改动。
	if got, _ := os.ReadFile(filepath.Join(root, "todo.md")); string(got) != "v2-external" {
		t.Errorf("file should be unchanged on conflict: %q", got)
	}
}

func TestFSContent_ForceOverwrite(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("orig"), 0o644)

	_, env := doJSON(t, h, http.MethodPut, "/api/fs/content", token, map[string]any{
		"path": "/f.txt", "content": "forced", "force": true,
	})
	if env["code"].(float64) != 0 {
		t.Fatalf("force save failed: %v", env)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "f.txt")); string(got) != "forced" {
		t.Errorf("force overwrite content mismatch: %q", got)
	}
}

func TestFSContent_MissingRevisionRejected(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("x"), 0o644)

	rec, env := doJSON(t, h, http.MethodPut, "/api/fs/content", token, map[string]any{
		"path": "/f.txt", "content": "y",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if env["code"].(float64) != 2016 {
		t.Errorf("expected code 2016 invalid_revision, got %v", env["code"])
	}
}

func TestFSContent_Binary(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "data.bin"), []byte{0x00, 0x01, 0xff}, 0o644)

	rec, env := doJSON(t, h, http.MethodGet, "/api/fs/content?path=/data.bin", token, nil)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d", rec.Code)
	}
	if env["code"].(float64) != 2013 {
		t.Errorf("expected code 2013, got %v", env["code"])
	}
}

func TestFSContent_RequiresAuth(t *testing.T) {
	h, _, _ := newFSTestServer(t)
	rec, _ := doJSON(t, h, http.MethodGet, "/api/fs/content?path=/x", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", rec.Code)
	}
}

func TestFSSpace_Local(t *testing.T) {
	h, token, _ := newFSTestServer(t)

	_, env := doJSON(t, h, http.MethodGet, "/api/fs/space?path=/", token, nil)
	if env["code"].(float64) != 0 {
		t.Fatalf("space failed: %v", env)
	}
	data := env["data"].(map[string]any)
	space := data["space"].(map[string]any)
	if space["supported"] != true {
		t.Fatal("local should support space")
	}
	if space["total"].(float64) == 0 {
		t.Error("expected non-zero total")
	}
	mount := data["mount"].(map[string]any)
	if mount["name"] != "local" || mount["prefix"] != "/" {
		t.Errorf("unexpected mount: %v", mount)
	}
}

func TestFSSpace_PathNotFound(t *testing.T) {
	h, token, _ := newFSTestServer(t)
	rec, env := doJSON(t, h, http.MethodGet, "/api/fs/space?path=/ghost", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if env["code"].(float64) != 2001 {
		t.Errorf("expected code 2001, got %v", env["code"])
	}
}

func TestSystemInfo_NoDiskFields(t *testing.T) {
	h, token, _ := newFSTestServer(t)
	_, env := doJSON(t, h, http.MethodGet, "/api/system/info", token, nil)
	if env["code"].(float64) != 0 {
		t.Fatalf("system info failed: %v", env)
	}
	data := env["data"].(map[string]any)
	if _, ok := data["disk_total"]; ok {
		t.Error("system/info should no longer carry disk_total")
	}
	if data["os"] == nil || data["arch"] == nil {
		t.Errorf("system/info should carry os/arch: %v", data)
	}
}
