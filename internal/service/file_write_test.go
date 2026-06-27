package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"flist/internal/model"
)

func TestMkdir(t *testing.T) {
	svc, root := setupTestRoot(t)

	p, err := svc.Mkdir("/newdir")
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if p != "/newdir" {
		t.Errorf("unexpected path %q", p)
	}
	if fi, err := os.Stat(filepath.Join(root, "newdir")); err != nil || !fi.IsDir() {
		t.Errorf("dir not created: %v", err)
	}

	// 已存在 → 冲突。
	if _, err := svc.Mkdir("/newdir"); err != ErrExists {
		t.Errorf("expected ErrExists, got %v", err)
	}

	// 父目录不存在（仅建单层）→ NotFound。
	if _, err := svc.Mkdir("/ghost/child"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound for missing parent, got %v", err)
	}
}

func TestMkdir_InvalidName(t *testing.T) {
	svc, _ := setupTestRoot(t)
	if _, err := svc.Mkdir("/.."); err == nil {
		t.Error("expected error for invalid name")
	}
}

func TestTouch(t *testing.T) {
	svc, root := setupTestRoot(t)

	p, err := svc.Touch("/empty.txt")
	if err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if p != "/empty.txt" {
		t.Errorf("unexpected path %q", p)
	}
	fi, err := os.Stat(filepath.Join(root, "empty.txt"))
	if err != nil || fi.Size() != 0 {
		t.Errorf("empty file not created: %v", err)
	}

	// 已存在 → 冲突，且不 truncate。
	writeFile(t, root, "data.txt", "keep me")
	if _, err := svc.Touch("/data.txt"); err != ErrExists {
		t.Errorf("expected ErrExists, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "data.txt")); string(b) != "keep me" {
		t.Error("existing file should not be truncated")
	}
}

func TestMove_Rename(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "old.txt", "content")

	results := svc.Move([]string{"/old.txt"}, "/new.txt")
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("rename failed: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); !os.IsNotExist(err) {
		t.Error("old.txt should be gone")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "new.txt")); string(b) != "content" {
		t.Errorf("new.txt content mismatch: %q", b)
	}
}

func TestMove_IntoDir(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "a.txt", "a")
	writeFile(t, root, "b.txt", "b")
	os.MkdirAll(filepath.Join(root, "dest"), 0o755)

	results := svc.Move([]string{"/a.txt", "/b.txt"}, "/dest")
	if len(results) != 2 || !results[0].OK || !results[1].OK {
		t.Fatalf("move into dir failed: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "a.txt")); err != nil {
		t.Error("a.txt not moved into dest")
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "b.txt")); err != nil {
		t.Error("b.txt not moved into dest")
	}
}

func TestMove_Conflict(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "src.txt", "s")
	writeFile(t, root, "exists.txt", "e")

	results := svc.Move([]string{"/src.txt"}, "/exists.txt")
	if results[0].OK || results[0].Error != "file_exists" {
		t.Errorf("expected file_exists conflict, got %+v", results[0])
	}
}

func TestMove_SrcNotFound(t *testing.T) {
	svc, _ := setupTestRoot(t)
	results := svc.Move([]string{"/ghost.txt"}, "/new.txt")
	if results[0].OK || results[0].Error != "path_not_found" {
		t.Errorf("expected path_not_found, got %+v", results[0])
	}
}

func TestMove_DirIntoOwnSubtree(t *testing.T) {
	svc, root := setupTestRoot(t)
	os.MkdirAll(filepath.Join(root, "parent", "child"), 0o755)

	// 把 /parent 移动进 /parent/child → 自包含，应被拒。
	results := svc.Move([]string{"/parent"}, "/parent/child")
	if results[0].OK || results[0].Error != "bad_request" {
		t.Errorf("expected bad_request for self-subtree move, got %+v", results[0])
	}
}

func TestMove_MultiToNonexistent(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "a.txt", "a")
	writeFile(t, root, "b.txt", "b")

	// dst 不存在且多个 src → 每项 not_a_dir。
	results := svc.Move([]string{"/a.txt", "/b.txt"}, "/nowhere")
	for _, res := range results {
		if res.OK || res.Error != "not_a_dir" {
			t.Errorf("expected not_a_dir, got %+v", res)
		}
	}
}

func TestDelete(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "x.txt", "x")
	writeFile(t, root, "tree/sub/y.txt", "y")

	results := svc.Delete([]string{"/x.txt", "/tree"})
	if len(results) != 2 || !results[0].OK || !results[1].OK {
		t.Fatalf("delete failed: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, "x.txt")); !os.IsNotExist(err) {
		t.Error("x.txt should be gone")
	}
	if _, err := os.Stat(filepath.Join(root, "tree")); !os.IsNotExist(err) {
		t.Error("tree should be gone")
	}
}

func TestDelete_RootRejected(t *testing.T) {
	svc, _ := setupTestRoot(t)
	results := svc.Delete([]string{"/"})
	if results[0].OK || results[0].Error != "bad_request" {
		t.Errorf("deleting root should be rejected, got %+v", results[0])
	}
}

func TestDelete_NotFound(t *testing.T) {
	svc, _ := setupTestRoot(t)
	results := svc.Delete([]string{"/ghost"})
	if results[0].OK || results[0].Error != "path_not_found" {
		t.Errorf("expected path_not_found, got %+v", results[0])
	}
}

func TestSearch_Substring(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "report-2024.pdf", "x")
	writeFile(t, root, "sub/annual-report.txt", "y")
	writeFile(t, root, "notes.md", "z")

	res, err := svc.Search(context.Background(), "/", "report", SearchOptions{Recursive: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Errorf("expected 2 hits, got %d: %+v", len(res.Items), res.Items)
	}
}

func TestSearch_Glob(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "a.go", "x")
	writeFile(t, root, "b.go", "y")
	writeFile(t, root, "c.txt", "z")

	res, err := svc.Search(context.Background(), "/", "*.go", SearchOptions{Recursive: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Errorf("expected 2 go files, got %d", len(res.Items))
	}
}

func TestSearch_NonRecursive(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "top.txt", "x")
	writeFile(t, root, "sub/deep.txt", "y")

	res, err := svc.Search(context.Background(), "/", "txt", SearchOptions{Recursive: false, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	// 非递归仅匹配当层（top.txt），不含 sub/deep.txt。
	if len(res.Items) != 1 || res.Items[0].Name != "top.txt" {
		t.Errorf("non-recursive should match only top level, got %+v", res.Items)
	}
}

func TestSearch_LimitTruncated(t *testing.T) {
	svc, root := setupTestRoot(t)
	for _, n := range []string{"m1", "m2", "m3", "m4", "m5"} {
		writeFile(t, root, n+"-match.txt", "x")
	}
	res, err := svc.Search(context.Background(), "/", "match", SearchOptions{Recursive: true, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 || !res.Truncated {
		t.Errorf("expected 2 items truncated, got %d truncated=%v", len(res.Items), res.Truncated)
	}
}

func TestSearch_HiddenFilter(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, ".hidden-match.txt", "x")
	writeFile(t, root, "visible-match.txt", "y")

	res, _ := svc.Search(context.Background(), "/", "match", SearchOptions{Recursive: true, Limit: 100})
	if len(res.Items) != 1 || res.Items[0].Name != "visible-match.txt" {
		t.Errorf("hidden file should be filtered, got %+v", res.Items)
	}

	res2, _ := svc.Search(context.Background(), "/", "match", SearchOptions{Recursive: true, ShowHidden: true, Limit: 100})
	if len(res2.Items) != 2 {
		t.Errorf("show_hidden should include dotfile, got %d", len(res2.Items))
	}
}

func TestSearch_BaseNotDir(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "file.txt", "x")
	if _, err := svc.Search(context.Background(), "/file.txt", "x", SearchOptions{}); err != ErrNotDir {
		t.Errorf("expected ErrNotDir, got %v", err)
	}
}

func TestSearch_ContextCancelled(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "a-match.txt", "x")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	res, err := svc.Search(ctx, "/", "match", SearchOptions{Recursive: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Error("cancelled context should set TimedOut")
	}
}

func TestSearch_HitFields(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "docs/keyword.txt", "hello")

	res, err := svc.Search(context.Background(), "/", "keyword", SearchOptions{Recursive: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(res.Items))
	}
	hit := res.Items[0]
	if hit.Path != "/docs/keyword.txt" || hit.Type != model.TypeFile || hit.Size != 5 {
		t.Errorf("unexpected hit: %+v", hit)
	}
	if hit.ModTime.IsZero() || time.Since(hit.ModTime) > time.Hour {
		t.Errorf("unexpected mod time: %v", hit.ModTime)
	}
}
