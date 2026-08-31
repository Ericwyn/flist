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
	"strings"
	"testing"
)

func writeServerZIP(t *testing.T, filename string) {
	t.Helper()
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("nested/hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func doExtractRequest(h http.Handler, token, archivePath string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"path": archivePath})
	req := httptest.NewRequest(http.MethodPost, "/api/fs/op/extract", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestExtract_RequiresAuth(t *testing.T) {
	h, _, root := newFSTestServer(t)
	writeServerZIP(t, filepath.Join(root, "archive.zip"))
	rec := doExtractRequest(h, "", "/archive.zip")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestExtract_RejectsUnsupportedName(t *testing.T) {
	h, token, root := newFSTestServer(t)
	if err := os.WriteFile(filepath.Join(root, "archive.7z"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := doExtractRequest(h, token, "/archive.7z")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unsupported_archive") {
		t.Fatalf("unexpected error body: %s", rec.Body.String())
	}
}

func TestExtract_RejectsEmptyPathAndDirectory(t *testing.T) {
	h, token, root := newFSTestServer(t)
	if err := os.Mkdir(filepath.Join(root, "folder.zip"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		archivePath string
		wantCode    int
		wantMessage string
	}{
		{name: "empty path", archivePath: "", wantCode: http.StatusBadRequest, wantMessage: "path required"},
		{name: "directory", archivePath: "/folder.zip", wantCode: http.StatusBadRequest, wantMessage: "not_a_file"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doExtractRequest(h, token, tc.archivePath)
			if rec.Code != tc.wantCode || !strings.Contains(rec.Body.String(), tc.wantMessage) {
				t.Fatalf("status=%d body=%s, want status=%d message=%q", rec.Code, rec.Body.String(), tc.wantCode, tc.wantMessage)
			}
		})
	}
}

func TestExtract_AcceptedAndProgressCompletes(t *testing.T) {
	h, token, root := newFSTestServer(t)
	writeServerZIP(t, filepath.Join(root, "archive.zip"))
	rec := doExtractRequest(h, token, "/archive.zip")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			TaskID string `json:"task_id"`
			Op     string `json:"op"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.TaskID == "" || env.Data.Op != "extract" {
		t.Fatalf("unexpected start response: %s", rec.Body.String())
	}

	progressReq := httptest.NewRequest(http.MethodGet, "/api/fs/op/progress?id="+env.Data.TaskID, nil)
	progressReq.Header.Set("Authorization", "Bearer "+token)
	progressRec := httptest.NewRecorder()
	h.ServeHTTP(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("progress status=%d body=%s", progressRec.Code, progressRec.Body.String())
	}
	if !strings.Contains(progressRec.Body.String(), `"status":"done"`) {
		t.Fatalf("progress stream lacks done snapshot: %s", progressRec.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(root, "archive", "nested", "hello.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("extracted file=(%q,%v)", got, err)
	}
}
