package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// doArchive 发送打包下载请求并返回原始响应（zip 字节或 JSON 错误信封共用一个 recorder）。
func doArchive(t *testing.T, h http.Handler, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/fs/archive", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestArchive_RequiresAuth(t *testing.T) {
	h, _, _ := newFSTestServer(t)
	rec := doArchive(t, h, "", map[string]any{"paths": []string{"/x"}})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestArchive_EmptyPaths(t *testing.T) {
	h, token, _ := newFSTestServer(t)
	rec := doArchive(t, h, token, map[string]any{"paths": []string{}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env["code"].(float64) != 4000 {
		t.Errorf("expected code 4000, got %v", env["code"])
	}
}

func TestArchive_NotFound_NoZipBytes(t *testing.T) {
	h, token, _ := newFSTestServer(t)
	rec := doArchive(t, h, token, map[string]any{"paths": []string{"/missing.txt"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	// 预检失败时应返回 JSON 错误而非 zip。
	if ct := rec.Header().Get("Content-Type"); ct == "application/zip" {
		t.Errorf("should not send zip content-type on precheck failure")
	}
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env["code"].(float64) != 2001 {
		t.Errorf("expected code 2001, got %v", env["code"])
	}
}

func TestArchive_Success_ValidZip(t *testing.T) {
	h, token, root := newFSTestServer(t)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644)
	os.MkdirAll(filepath.Join(root, "dir", "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "dir", "b.txt"), []byte("bravo"), 0o644)
	os.WriteFile(filepath.Join(root, "dir", "sub", "c.txt"), []byte("charlie"), 0o644)

	rec := doArchive(t, h, token, map[string]any{
		"paths": []string{"/a.txt", "/dir"},
		"name":  "mypack",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("expected application/zip, got %q", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !bytes.Contains([]byte(cd), []byte("mypack.zip")) {
		t.Errorf("expected filename mypack.zip in disposition, got %q", cd)
	}

	body := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("response is not a valid zip: %v", err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(b)
	}
	if got["a.txt"] != "alpha" {
		t.Errorf("a.txt mismatch: %q", got["a.txt"])
	}
	if got["dir/b.txt"] != "bravo" {
		t.Errorf("dir/b.txt mismatch: %q", got["dir/b.txt"])
	}
	if got["dir/sub/c.txt"] != "charlie" {
		t.Errorf("dir/sub/c.txt mismatch: %q", got["dir/sub/c.txt"])
	}
}
