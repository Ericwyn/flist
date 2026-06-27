package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{"a.txt", "my file.txt", "目录", "with-dash_underscore", ".hidden"}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) unexpected error: %v", n, err)
		}
	}

	invalid := []string{"", ".", "..", "a/b", "a\x00b", strings.Repeat("x", 256)}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) expected error, got nil", n)
		}
	}
}

func TestCopyPath_File(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("hello world"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := CopyPath(src, dst); err != nil {
		t.Fatalf("CopyPath: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Errorf("content mismatch: %q", got)
	}
	fi, _ := os.Stat(dst)
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("perm mismatch: %v", fi.Mode().Perm())
	}
}

func TestCopyPath_DirTree(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("b"), 0o644)

	dst := filepath.Join(dir, "dst")
	if err := CopyPath(src, dst); err != nil {
		t.Fatalf("CopyPath: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); string(b) != "b" {
		t.Errorf("nested file not copied: %q", b)
	}
}

func TestCopyPath_DstExists(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	os.WriteFile(src, []byte("x"), 0o644)
	os.WriteFile(dst, []byte("y"), 0o644)

	if err := CopyPath(src, dst); err == nil {
		t.Error("expected error when dst exists, got nil")
	}
}

func TestRemovePath_DirTree(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tree")
	os.MkdirAll(filepath.Join(target, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(target, "a", "f.txt"), []byte("x"), 0o644)

	if err := RemovePath(target); err != nil {
		t.Fatalf("RemovePath: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("target still exists after RemovePath")
	}
}

func TestMovePath_Rename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	os.WriteFile(src, []byte("move me"), 0o644)

	if err := MovePath(src, dst); err != nil {
		t.Fatalf("MovePath: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("src still exists after move")
	}
	if b, _ := os.ReadFile(dst); string(b) != "move me" {
		t.Errorf("dst content mismatch: %q", b)
	}
}
