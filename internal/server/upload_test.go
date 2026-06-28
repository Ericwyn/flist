package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

// newUploadTestServer 构造一个注入了 UploadService 的路由，返回令牌与 root。
func newUploadTestServer(t *testing.T, maxUpload int64) (http.Handler, string, string) {
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
	locker := util.NewPathLocker()
	files := service.NewFileService(backend, locker, 5<<20)
	uploads := service.NewUploadService(backend, locker, maxUpload)

	router, err := NewRouter(Deps{
		Config:  &config.Config{SessionTTL: time.Hour},
		Auth:    auth,
		Files:   files,
		Uploads: uploads,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return router, res.Token, rootReal
}

// postChunk 上传单个二进制分片。
func postChunk(t *testing.T, h http.Handler, token, uploadID string, index int, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/fs/upload/chunk?upload_id=" + uploadID + "&index=" + strconv.Itoa(index)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUpload_E2E_RoundTrip(t *testing.T) {
	h, token, root := newUploadTestServer(t, 0)
	content := bytes.Repeat([]byte("abcdefghij"), 100000) // ~1MB
	chunkSize := int64(256 << 10)

	// init
	_, initEnv := doJSON(t, h, http.MethodPost, "/api/fs/upload/init", token, map[string]any{
		"dir": "/", "name": "movie.bin", "total_size": len(content),
		"chunk_size": chunkSize, "fingerprint": "fp-e2e",
	})
	if initEnv["code"].(float64) != 0 {
		t.Fatalf("init failed: %v", initEnv)
	}
	data := initEnv["data"].(map[string]any)
	uploadID := data["upload_id"].(string)
	totalChunks := int(data["total_chunks"].(float64))
	cs := int64(data["chunk_size"].(float64))

	// 乱序上传分片（倒序）以验证按 index 拼接。
	for i := totalChunks - 1; i >= 0; i-- {
		start := int64(i) * cs
		end := start + cs
		if end > int64(len(content)) {
			end = int64(len(content))
		}
		if rec := postChunk(t, h, token, uploadID, i, content[start:end]); rec.Code != http.StatusOK {
			t.Fatalf("chunk %d failed: %d", i, rec.Code)
		}
	}

	// complete
	_, compEnv := doJSON(t, h, http.MethodPost, "/api/fs/upload/complete", token, map[string]any{
		"upload_id": uploadID, "overwrite": false,
	})
	if compEnv["code"].(float64) != 0 {
		t.Fatalf("complete failed: %v", compEnv)
	}

	got, err := os.ReadFile(filepath.Join(root, "movie.bin"))
	if err != nil {
		t.Fatalf("read merged: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("merged content mismatch: got %d bytes, want %d", len(got), len(content))
	}

	// list 应见该文件。
	_, listEnv := doJSON(t, h, http.MethodGet, "/api/fs/list?path=/", token, nil)
	items := listEnv["data"].(map[string]any)["items"].([]any)
	found := false
	for _, it := range items {
		if it.(map[string]any)["name"] == "movie.bin" {
			found = true
		}
	}
	if !found {
		t.Error("uploaded file should appear in list")
	}
}

func TestUpload_E2E_ResumeAfterInterrupt(t *testing.T) {
	h, token, root := newUploadTestServer(t, 0)
	content := bytes.Repeat([]byte("z"), 600<<10) // 3 片
	chunkSize := int64(256 << 10)

	_, initEnv := doJSON(t, h, http.MethodPost, "/api/fs/upload/init", token, map[string]any{
		"dir": "/", "name": "resume.bin", "total_size": len(content),
		"chunk_size": chunkSize, "fingerprint": "fp-resume",
	})
	data := initEnv["data"].(map[string]any)
	uploadID := data["upload_id"].(string)
	cs := int64(data["chunk_size"].(float64))
	totalChunks := int(data["total_chunks"].(float64))

	// 只传第 0 片，模拟中断。
	postChunk(t, h, token, uploadID, 0, content[:cs])

	// 重新 init（相同指纹）→ 应复用并返回已收 [0]。
	_, init2 := doJSON(t, h, http.MethodPost, "/api/fs/upload/init", token, map[string]any{
		"dir": "/", "name": "resume.bin", "total_size": len(content),
		"chunk_size": chunkSize, "fingerprint": "fp-resume",
	})
	d2 := init2["data"].(map[string]any)
	if d2["upload_id"].(string) != uploadID {
		t.Error("resume should reuse upload_id")
	}
	received := d2["received"].([]any)
	if len(received) != 1 || int(received[0].(float64)) != 0 {
		t.Errorf("resume should report received [0], got %v", received)
	}

	// 补传剩余分片。
	for i := 1; i < totalChunks; i++ {
		start := int64(i) * cs
		end := start + cs
		if end > int64(len(content)) {
			end = int64(len(content))
		}
		postChunk(t, h, token, uploadID, i, content[start:end])
	}
	_, compEnv := doJSON(t, h, http.MethodPost, "/api/fs/upload/complete", token, map[string]any{
		"upload_id": uploadID,
	})
	if compEnv["code"].(float64) != 0 {
		t.Fatalf("complete after resume failed: %v", compEnv)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "resume.bin")); !bytes.Equal(got, content) {
		t.Error("resumed upload content mismatch")
	}
}

func TestUpload_E2E_IncompleteReturnsMissing(t *testing.T) {
	h, token, _ := newUploadTestServer(t, 0)
	content := bytes.Repeat([]byte("y"), 600<<10) // 3 片
	chunkSize := int64(256 << 10)

	_, initEnv := doJSON(t, h, http.MethodPost, "/api/fs/upload/init", token, map[string]any{
		"dir": "/", "name": "inc.bin", "total_size": len(content), "chunk_size": chunkSize,
	})
	data := initEnv["data"].(map[string]any)
	uploadID := data["upload_id"].(string)
	cs := int64(data["chunk_size"].(float64))
	// 只传第 0 片。
	postChunk(t, h, token, uploadID, 0, content[:cs])

	_, compEnv := doJSON(t, h, http.MethodPost, "/api/fs/upload/complete", token, map[string]any{
		"upload_id": uploadID,
	})
	if compEnv["code"].(float64) != 2011 {
		t.Fatalf("expected 2011 upload_incomplete, got %v", compEnv["code"])
	}
	missing := compEnv["data"].(map[string]any)["missing"].([]any)
	if len(missing) != 2 {
		t.Errorf("expected 2 missing chunks, got %v", missing)
	}
}

func TestUpload_E2E_Overwrite(t *testing.T) {
	h, token, root := newUploadTestServer(t, 0)
	os.WriteFile(filepath.Join(root, "dup.bin"), []byte("old"), 0o644)
	content := []byte("brand new content")

	// 第一次：不覆盖 → complete 应 file_exists。
	upload := func(overwrite bool) map[string]any {
		_, initEnv := doJSON(t, h, http.MethodPost, "/api/fs/upload/init", token, map[string]any{
			"dir": "/", "name": "dup.bin", "total_size": len(content), "chunk_size": 256 << 10,
			"fingerprint": "fp-ow-" + strconv.FormatBool(overwrite),
		})
		uploadID := initEnv["data"].(map[string]any)["upload_id"].(string)
		postChunk(t, h, token, uploadID, 0, content)
		_, compEnv := doJSON(t, h, http.MethodPost, "/api/fs/upload/complete", token, map[string]any{
			"upload_id": uploadID, "overwrite": overwrite,
		})
		return compEnv
	}

	if env := upload(false); env["code"].(float64) != 2004 {
		t.Errorf("expected 2004 file_exists without overwrite, got %v", env["code"])
	}
	if env := upload(true); env["code"].(float64) != 0 {
		t.Fatalf("overwrite upload failed: %v", env)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "dup.bin")); string(got) != string(content) {
		t.Errorf("overwrite content = %q", got)
	}
}

func TestUpload_E2E_TooLarge(t *testing.T) {
	h, token, _ := newUploadTestServer(t, 1024) // 上限 1KiB
	_, initEnv := doJSON(t, h, http.MethodPost, "/api/fs/upload/init", token, map[string]any{
		"dir": "/", "name": "huge.bin", "total_size": 4096, "chunk_size": 256 << 10,
	})
	if initEnv["code"].(float64) != 2009 {
		t.Errorf("expected 2009 upload_too_large, got %v", initEnv["code"])
	}
}

func TestUpload_E2E_RequiresAuth(t *testing.T) {
	h, _, _ := newUploadTestServer(t, 0)
	rec, _ := doJSON(t, h, http.MethodPost, "/api/fs/upload/init", "", map[string]any{
		"dir": "/", "name": "x.bin", "total_size": 10,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", rec.Code)
	}
}
