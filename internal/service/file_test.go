package service

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"flist/internal/model"
	"flist/internal/storage/local"
	"flist/internal/util"
)

// setupTestRoot 创建一个测试用 root 目录树并返回 FileService 与 rootReal。
func setupTestRoot(t *testing.T) (*FileService, string) {
	t.Helper()
	dir := t.TempDir()
	real, err := util.ResolveRoot(dir)
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	return NewFileService(local.New(real, t.TempDir())), real
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestList_BasicAndDirFirst(t *testing.T) {
	svc, root := setupTestRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "b.txt", "bb")
	writeFile(t, root, "a.txt", "a")

	res, err := svc.List(context.Background(), "/", ListOptions{Sort: "name", Order: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 3 {
		t.Fatalf("expected 3 items, got %d", res.Total)
	}
	// 目录优先，然后 a.txt、b.txt。
	if res.Items[0].Name != "sub" || res.Items[0].Type != model.TypeDir {
		t.Errorf("dir should be first, got %+v", res.Items[0])
	}
	if res.Items[1].Name != "a.txt" || res.Items[2].Name != "b.txt" {
		t.Errorf("file order wrong: %v %v", res.Items[1].Name, res.Items[2].Name)
	}
}

func TestList_SortSizeDesc(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "small.txt", "x")
	writeFile(t, root, "big.txt", strings.Repeat("y", 100))

	res, err := svc.List(context.Background(), "/", ListOptions{Sort: "size", Order: "desc"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Items[0].Name != "big.txt" {
		t.Errorf("expected big.txt first on size desc, got %s", res.Items[0].Name)
	}
}

func TestList_HiddenFilter(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, ".hidden", "h")
	writeFile(t, root, "visible.txt", "v")

	res, _ := svc.List(context.Background(), "/", ListOptions{})
	if res.Total != 1 || res.Items[0].Name != "visible.txt" {
		t.Errorf("hidden file should be filtered, got total=%d", res.Total)
	}

	res2, _ := svc.List(context.Background(), "/", ListOptions{ShowHidden: true})
	if res2.Total != 2 {
		t.Errorf("show_hidden should include dotfile, got total=%d", res2.Total)
	}
}

func TestList_Paging(t *testing.T) {
	svc, root := setupTestRoot(t)
	for _, n := range []string{"f1", "f2", "f3", "f4", "f5"} {
		writeFile(t, root, n+".txt", "x")
	}
	res, err := svc.List(context.Background(), "/", ListOptions{Sort: "name", Page: 2, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 5 {
		t.Errorf("total should be 5, got %d", res.Total)
	}
	if len(res.Items) != 2 {
		t.Fatalf("page size 2 expected 2 items, got %d", len(res.Items))
	}
	// 第 2 页应为 f3、f4。
	if res.Items[0].Name != "f3.txt" || res.Items[1].Name != "f4.txt" {
		t.Errorf("page 2 wrong items: %s %s", res.Items[0].Name, res.Items[1].Name)
	}
}

func TestList_NotADir(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "file.txt", "x")
	if _, err := svc.List(context.Background(), "/file.txt", ListOptions{}); err != ErrNotDir {
		t.Errorf("expected ErrNotDir, got %v", err)
	}
}

func TestList_Traversal(t *testing.T) {
	svc, root := setupTestRoot(t)
	// 在 root 内建一个 sub 目录，并放一个文件。
	writeFile(t, root, "sub/inside.txt", "x")

	// 越根路径 /../../sub 经 CleanAPIPath 钳制为 /sub，仍落在 root 内，
	// 应成功列出 root/sub 而非逃逸到真实文件系统。
	res, err := svc.List(context.Background(), "/../../sub", ListOptions{})
	if err != nil {
		t.Fatalf("clamped traversal should resolve within root, got %v", err)
	}
	if res.Path != "/sub" {
		t.Errorf("expected clamped path /sub, got %q", res.Path)
	}
	if res.Total != 1 || res.Items[0].Name != "inside.txt" {
		t.Errorf("should list root/sub contents, got %+v", res.Items)
	}
}

func TestStat(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "doc.txt", "hello")

	info, err := svc.Stat(context.Background(), "/doc.txt")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "doc.txt" || info.Type != model.TypeFile || info.Size != 5 {
		t.Errorf("unexpected stat: %+v", info)
	}

	if _, err := svc.Stat(context.Background(), "/nonexistent"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPreview_Text(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "note.txt", "hello world")

	res, err := svc.PreviewText(context.Background(), "/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "text" || res.Content != "hello world" {
		t.Errorf("unexpected preview: %+v", res)
	}
	if res.Truncated {
		t.Error("small file should not be truncated")
	}
}

func TestPreview_Truncated(t *testing.T) {
	svc, root := setupTestRoot(t)
	big := strings.Repeat("a", previewMaxBytes+100)
	writeFile(t, root, "big.txt", big)

	res, err := svc.PreviewText(context.Background(), "/big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Error("large text should be truncated")
	}
	if len(res.Content) != previewMaxBytes {
		t.Errorf("content should be capped at %d, got %d", previewMaxBytes, len(res.Content))
	}
}

func TestPreview_Binary(t *testing.T) {
	svc, root := setupTestRoot(t)
	full := filepath.Join(root, "data.bin")
	if err := os.WriteFile(full, []byte{0x00, 0x01, 0x02, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := svc.PreviewText(context.Background(), "/data.bin")
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "binary" || res.Content != "" {
		t.Errorf("expected binary type with no content, got %+v", res)
	}
}

func TestPreview_ImageType(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "pic.png", "fakepng")
	res, err := svc.PreviewText(context.Background(), "/pic.png")
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "image" {
		t.Errorf("expected image type, got %q", res.Type)
	}
}

func TestPreview_Dir(t *testing.T) {
	svc, root := setupTestRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PreviewText(context.Background(), "/adir"); err != ErrNotFile {
		t.Errorf("expected ErrNotFile for dir, got %v", err)
	}
}

func TestOpenForDownload(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "dl.txt", "download me")

	target, err := svc.OpenForDownload(context.Background(), "/dl.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer target.File.Close()
	if target.Info.Size != 11 {
		t.Errorf("unexpected size: %d", target.Info.Size)
	}

	if _, err := svc.OpenForDownload(context.Background(), "/"); err != ErrNotFile {
		t.Errorf("expected ErrNotFile for root dir, got %v", err)
	}
}

func TestList_SymlinkInternal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires admin on Windows")
	}
	svc, root := setupTestRoot(t)
	writeFile(t, root, "target.txt", "data")
	if err := os.Symlink(filepath.Join(root, "target.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	res, err := svc.List(context.Background(), "/", ListOptions{Sort: "name"})
	if err != nil {
		t.Fatal(err)
	}
	var link *model.FileInfo
	for i := range res.Items {
		if res.Items[i].Name == "link.txt" {
			link = &res.Items[i]
		}
	}
	if link == nil {
		t.Fatal("link.txt not found")
	}
	if !link.IsSymlink {
		t.Error("link should be marked as symlink")
	}
	if link.Unreachable {
		t.Error("internal symlink should be reachable")
	}
	if link.SymlinkTarget != "/target.txt" {
		t.Errorf("unexpected symlink target: %q", link.SymlinkTarget)
	}
}

func TestList_SymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires admin on Windows")
	}
	svc, root := setupTestRoot(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	res, err := svc.List(context.Background(), "/", ListOptions{Sort: "name"})
	if err != nil {
		t.Fatal(err)
	}
	var esc *model.FileInfo
	for i := range res.Items {
		if res.Items[i].Name == "escape" {
			esc = &res.Items[i]
		}
	}
	if esc == nil {
		t.Fatal("escape link not found")
	}
	if !esc.Unreachable {
		t.Error("escaping symlink should be marked Unreachable")
	}
	if esc.SymlinkTarget != "" {
		t.Errorf("escaping symlink should not expose target, got %q", esc.SymlinkTarget)
	}
}

func TestSortStableMtime(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "old.txt", "o")
	time.Sleep(10 * time.Millisecond)
	writeFile(t, root, "new.txt", "n")
	// 显式设置修改时间以避免时序抖动。
	old := time.Now().Add(-time.Hour)
	recent := time.Now()
	os.Chtimes(filepath.Join(root, "old.txt"), old, old)
	os.Chtimes(filepath.Join(root, "new.txt"), recent, recent)

	res, _ := svc.List(context.Background(), "/", ListOptions{Sort: "mtime", Order: "asc"})
	if res.Items[0].Name != "old.txt" {
		t.Errorf("mtime asc should put old first, got %s", res.Items[0].Name)
	}
}
