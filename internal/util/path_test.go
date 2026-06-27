package util

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func mustResolveRoot(t *testing.T, dir string) string {
	t.Helper()
	real, err := ResolveRoot(dir)
	if err != nil {
		t.Fatalf("ResolveRoot(%q) error: %v", dir, err)
	}
	return real
}

func TestSafeResolve_Normal(t *testing.T) {
	root := mustResolveRoot(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := SafeResolve(root, "/docs/a.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, "docs", "a.txt")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSafeResolve_RootItself(t *testing.T) {
	root := mustResolveRoot(t, t.TempDir())
	got, err := SafeResolve(root, "/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("got %q want %q", got, root)
	}
}

func TestSafeResolve_TraversalClamped(t *testing.T) {
	root := mustResolveRoot(t, t.TempDir())
	// path.Clean 会把越根的 .. 钳制在根内，故解析到 root 自身而非报错。
	got, err := SafeResolve(root, "/../../../etc/passwd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(got) || (got != root && !hasPrefixSep(got, root)) {
		t.Errorf("resolved path %q escaped root %q", got, root)
	}
}

func TestSafeResolve_NonexistentTarget(t *testing.T) {
	root := mustResolveRoot(t, t.TempDir())
	// 目标不存在时应通过父目录解析得到候选路径（用于创建类操作）。
	got, err := SafeResolve(root, "/newdir/newfile.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, "newdir", "newfile.txt")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSafeResolve_SymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires admin on Windows")
	}
	root := mustResolveRoot(t, t.TempDir())
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 在 root 内创建指向 root 外的符号链接。
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	if _, err := SafeResolve(root, "/escape/secret"); err != ErrPathTraversal {
		t.Errorf("expected ErrPathTraversal, got %v", err)
	}
}

func TestCleanAPIPath(t *testing.T) {
	cases := map[string]string{
		"":              "/",
		"/":             "/",
		"docs":          "/docs",
		"/docs/":        "/docs",
		"/docs/../a":    "/a",
		"/../../etc":    "/etc",
		"//docs//a.txt": "/docs/a.txt",
		"/a/./b":        "/a/b",
	}
	for in, want := range cases {
		if got := CleanAPIPath(in); got != want {
			t.Errorf("CleanAPIPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func hasPrefixSep(target, root string) bool {
	prefix := root
	if prefix[len(prefix)-1] != os.PathSeparator {
		prefix += string(os.PathSeparator)
	}
	return len(target) > len(prefix) && target[:len(prefix)] == prefix
}
