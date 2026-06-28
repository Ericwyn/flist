package service

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// readArchive 把 svc 对 paths 打包的 zip 读入内存，解析为 name->内容 与 name->Method 两张表。
// 目录条目（以 / 结尾）内容为空串。
func readArchive(t *testing.T, svc *FileService, paths []string) (map[string]string, map[string]uint16) {
	t.Helper()
	targets, err := svc.ResolveArchiveTargets(context.Background(), paths)
	if err != nil {
		t.Fatalf("ResolveArchiveTargets: %v", err)
	}
	var buf bytes.Buffer
	if werr := svc.WriteArchive(context.Background(), &buf, targets, nil); werr != nil {
		t.Fatalf("WriteArchive: %v", werr)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	contents := make(map[string]string)
	methods := make(map[string]uint16)
	for _, f := range zr.File {
		methods[f.Name] = f.Method
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", f.Name, err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		contents[f.Name] = string(b)
	}
	return contents, methods
}

func TestArchive_SingleFile(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "hello.txt", "world")

	contents, _ := readArchive(t, svc, []string{"/hello.txt"})
	if len(contents) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(contents), contents)
	}
	if contents["hello.txt"] != "world" {
		t.Errorf("content mismatch: %q", contents["hello.txt"])
	}
}

func TestArchive_Directory(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "tree/a.txt", "AAA")
	writeFile(t, root, "tree/sub/b.txt", "BBB")
	// 空目录应保留。
	if err := os.MkdirAll(filepath.Join(root, "tree", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	contents, _ := readArchive(t, svc, []string{"/tree"})
	if contents["tree/a.txt"] != "AAA" {
		t.Errorf("tree/a.txt mismatch: %q", contents["tree/a.txt"])
	}
	if contents["tree/sub/b.txt"] != "BBB" {
		t.Errorf("tree/sub/b.txt mismatch: %q", contents["tree/sub/b.txt"])
	}
	if _, ok := contents["tree/"]; !ok {
		t.Error("top dir entry missing")
	}
	if _, ok := contents["tree/empty/"]; !ok {
		t.Error("empty dir entry should be preserved")
	}
}

func TestArchive_MultipleMixed(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "a.txt", "a")
	writeFile(t, root, "dir/b.txt", "b")

	contents, _ := readArchive(t, svc, []string{"/a.txt", "/dir"})
	if contents["a.txt"] != "a" {
		t.Errorf("a.txt missing: %v", contents)
	}
	if contents["dir/b.txt"] != "b" {
		t.Errorf("dir/b.txt missing: %v", contents)
	}
}

func TestArchive_DedupTopNames(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "x/name.txt", "first")
	writeFile(t, root, "y/name.txt", "second")

	// 两个不同目录下的同名文件，zip 顶层名须去重。
	contents, _ := readArchive(t, svc, []string{"/x/name.txt", "/y/name.txt"})
	if contents["name.txt"] != "first" {
		t.Errorf("name.txt mismatch: %q", contents["name.txt"])
	}
	if contents["name (2).txt"] != "second" {
		t.Errorf("deduped name mismatch: %q (%v)", contents["name (2).txt"], contents)
	}
}

func TestArchive_IncludesHidden(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "d/.secret", "hidden")
	writeFile(t, root, "d/visible.txt", "shown")

	contents, _ := readArchive(t, svc, []string{"/d"})
	if contents["d/.secret"] != "hidden" {
		t.Errorf("hidden file should be archived: %v", contents)
	}
}

func TestArchive_SkipsSymlink(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "d/real.txt", "real")
	// 在目录内建一个指向 real.txt 的符号链接，打包应跳过它。
	if err := os.Symlink(filepath.Join(root, "d", "real.txt"), filepath.Join(root, "d", "link.txt")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	contents, _ := readArchive(t, svc, []string{"/d"})
	if contents["d/real.txt"] != "real" {
		t.Errorf("real file missing: %v", contents)
	}
	if _, ok := contents["d/link.txt"]; ok {
		t.Errorf("symlink should be skipped, got entry: %v", contents)
	}
}

func TestArchive_SmartCompression(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "doc.txt", "text should deflate")
	writeFile(t, root, "pic.jpg", "pretend-jpeg-bytes")
	writeFile(t, root, "pack.zip", "pretend-zip-bytes")

	_, methods := readArchive(t, svc, []string{"/doc.txt", "/pic.jpg", "/pack.zip"})
	if methods["doc.txt"] != zip.Deflate {
		t.Errorf("txt should use Deflate, got %d", methods["doc.txt"])
	}
	if methods["pic.jpg"] != zip.Store {
		t.Errorf("jpg should use Store, got %d", methods["pic.jpg"])
	}
	if methods["pack.zip"] != zip.Store {
		t.Errorf("zip should use Store, got %d", methods["pack.zip"])
	}
}

func TestArchive_ResolveNotFound(t *testing.T) {
	svc, _ := setupTestRoot(t)
	if _, err := svc.ResolveArchiveTargets(context.Background(), []string{"/nope.txt"}); err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestArchive_ResolveTraversal(t *testing.T) {
	svc, _ := setupTestRoot(t)
	// 越界路径预检应失败（CleanAPIPath 会消解 ..，故构造越界靠 backend 拒绝；
	// 这里用一个明显不存在的清理后路径，至少应返回 NotFound 而非 panic）。
	if _, err := svc.ResolveArchiveTargets(context.Background(), []string{"/../../etc/passwd"}); err == nil {
		t.Fatal("expected error for traversal/missing path")
	}
}

func TestArchive_RejectsRoot(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "a.txt", "a")

	// 禁止打包整个 root：无论以 "/" 还是会被归一化为 "/" 的形式传入都应被拒绝。
	for _, p := range []string{"/", "", "//", "/.."} {
		if _, err := svc.ResolveArchiveTargets(context.Background(), []string{p}); err == nil {
			t.Errorf("expected error when archiving root via %q", p)
		}
	}
	// 与其他合法路径混选时，含 root 仍应整体拒绝。
	if _, err := svc.ResolveArchiveTargets(context.Background(), []string{"/a.txt", "/"}); err == nil {
		t.Error("expected error when root mixed with other paths")
	}
}
