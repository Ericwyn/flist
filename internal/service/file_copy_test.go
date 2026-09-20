package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopy_File(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "src.txt", "hello")
	os.MkdirAll(filepath.Join(root, "dest"), 0o755)

	results := svc.Copy(context.Background(), []string{"/src.txt"}, "/dest", false)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("copy failed: %+v", results)
	}
	// 源仍在，副本生成且内容一致。
	if b, _ := os.ReadFile(filepath.Join(root, "src.txt")); string(b) != "hello" {
		t.Error("source should remain")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "dest", "src.txt")); string(b) != "hello" {
		t.Errorf("copy content mismatch: %q", b)
	}
}

func TestCopy_Tree(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "tree/a.txt", "a")
	writeFile(t, root, "tree/sub/b.txt", "b")
	os.MkdirAll(filepath.Join(root, "dest"), 0o755)

	results := svc.Copy(context.Background(), []string{"/tree"}, "/dest", false)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("copy tree failed: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "tree", "sub", "b.txt")); err != nil {
		t.Errorf("nested file not copied: %v", err)
	}
}

func TestCopy_ToSpecificName(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "src.txt", "x")

	// dst 不存在 + 单 src → 复制到指定名。
	results := svc.Copy(context.Background(), []string{"/src.txt"}, "/renamed.txt", false)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("copy to name failed: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil {
		t.Errorf("renamed copy missing: %v", err)
	}
}

func TestCopy_ToSpecificNameConflictStaysStrict(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "src.txt", "incoming")
	writeFile(t, root, "renamed.txt", "existing")

	results := svc.Copy(context.Background(), []string{"/src.txt"}, "/renamed.txt", true)
	if len(results) != 1 || results[0].OK || results[0].Error != "file_exists" {
		t.Fatalf("specific-name conflict should stay strict: %+v", results)
	}
}

func TestCopy_ConflictStrict(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "src.txt", "x")
	os.MkdirAll(filepath.Join(root, "dest"), 0o755)
	writeFile(t, root, "dest/src.txt", "existing")

	// 不开 auto_rename：落点同名 → file_exists。
	results := svc.Copy(context.Background(), []string{"/src.txt"}, "/dest", false)
	if results[0].OK || results[0].Error != "file_exists" {
		t.Errorf("expected file_exists, got %+v", results[0])
	}
}

func TestCopy_AutoRename(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "a.txt", "orig")
	os.MkdirAll(filepath.Join(root, "dest"), 0o755)
	writeFile(t, root, "dest/a.txt", "occupied")

	// 开 auto_rename：落点同名自动避让为 a (2).txt。
	results := svc.Copy(context.Background(), []string{"/a.txt"}, "/dest", true)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("auto-rename copy failed: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "a (2).txt")); err != nil {
		t.Errorf("expected 'a (2).txt', got err %v", err)
	}
	// 原占位文件未被覆盖。
	if b, _ := os.ReadFile(filepath.Join(root, "dest", "a.txt")); string(b) != "occupied" {
		t.Error("existing file should not be overwritten")
	}
}

func TestCopy_AutoRename_Sequence(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "a.txt", "orig")
	os.MkdirAll(filepath.Join(root, "dest"), 0o755)
	writeFile(t, root, "dest/a.txt", "x")
	writeFile(t, root, "dest/a (2).txt", "x")

	// dest/a.txt 与 a (2).txt 都已存在 → 避让到 a (3).txt。
	results := svc.Copy(context.Background(), []string{"/a.txt"}, "/dest", true)
	if !results[0].OK {
		t.Fatalf("copy failed: %+v", results[0])
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "a (3).txt")); err != nil {
		t.Errorf("expected 'a (3).txt', got err %v", err)
	}
}

func TestCopy_AutoRename_Dir(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "folder/inner.txt", "x")
	os.MkdirAll(filepath.Join(root, "dest", "folder"), 0o755)

	// 目录同名避让：无扩展名形如 "folder (2)"。
	results := svc.Copy(context.Background(), []string{"/folder"}, "/dest", true)
	if !results[0].OK {
		t.Fatalf("copy dir failed: %+v", results[0])
	}
	if fi, err := os.Stat(filepath.Join(root, "dest", "folder (2)")); err != nil || !fi.IsDir() {
		t.Errorf("expected dir 'folder (2)', got err %v", err)
	}
}

func TestCopy_SelfSubtree(t *testing.T) {
	svc, root := setupTestRoot(t)
	os.MkdirAll(filepath.Join(root, "parent", "child"), 0o755)

	// 把 /parent 复制进 /parent/child → 自包含被拒。
	results := svc.Copy(context.Background(), []string{"/parent"}, "/parent/child", false)
	if results[0].OK || results[0].Error != "bad_request" {
		t.Errorf("expected bad_request for self-subtree copy, got %+v", results[0])
	}
}

func TestCopy_SrcNotFound(t *testing.T) {
	svc, _ := setupTestRoot(t)
	results := svc.Copy(context.Background(), []string{"/ghost.txt"}, "/anywhere", false)
	if results[0].OK || results[0].Error != "path_not_found" {
		t.Errorf("expected path_not_found, got %+v", results[0])
	}
}

func TestCopy_MultiToNonexistent(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "a.txt", "a")
	writeFile(t, root, "b.txt", "b")

	// dst 不存在且多个 src → 每项 not_a_dir。
	results := svc.Copy(context.Background(), []string{"/a.txt", "/b.txt"}, "/nowhere", false)
	for _, res := range results {
		if res.OK || res.Error != "not_a_dir" {
			t.Errorf("expected not_a_dir, got %+v", res)
		}
	}
}

func TestMove_AutoRename(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "a.txt", "moving")
	os.MkdirAll(filepath.Join(root, "dest"), 0o755)
	writeFile(t, root, "dest/a.txt", "occupied")

	// 剪切粘贴：移入目录同名自动避让。
	results := svc.Move(context.Background(), []string{"/a.txt"}, "/dest", true)
	if !results[0].OK {
		t.Fatalf("auto-rename move failed: %+v", results[0])
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "a (2).txt")); err != nil {
		t.Errorf("expected moved 'a (2).txt', got err %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Error("source should be gone after move")
	}
}

func TestCopy_MergeDirsAndRenameNestedFiles(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "source/photos/2025/a.jpg", "incoming")
	writeFile(t, root, "source/photos/2025/info.txt", "info")
	writeFile(t, root, "dest/photos/2025/a.jpg", "existing")
	writeFile(t, root, "dest/photos/2025/readme.txt", "readme")

	results := svc.CopyWithPolicy(context.Background(), []string{"/source/photos"}, "/dest", ConflictMergeDirs)
	if len(results) != 1 || !results[0].OK || results[0].Outcome != "merged" {
		t.Fatalf("merge copy failed: %+v", results)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "dest/photos/2025/a.jpg")); string(got) != "existing" {
		t.Fatalf("existing file was overwritten: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "dest/photos/2025/a (2).jpg")); string(got) != "incoming" {
		t.Fatalf("nested conflict was not renamed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "dest/photos/2025/info.txt")); err != nil {
		t.Fatalf("new nested file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "source/photos")); err != nil {
		t.Fatalf("copy should retain source: %v", err)
	}
}

func TestMove_MergeDirsRemovesEmptySource(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "source/F.I.R/a.txt", "incoming")
	os.MkdirAll(filepath.Join(root, "dest", "F.I.R"), 0o755)

	results := svc.MoveWithPolicy(context.Background(), []string{"/source/F.I.R"}, "/dest", ConflictMergeDirs)
	if len(results) != 1 || !results[0].OK || results[0].Outcome != "merged" {
		t.Fatalf("merge move failed: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "F.I.R", "a.txt")); err != nil {
		t.Fatalf("merged file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "source", "F.I.R")); !os.IsNotExist(err) {
		t.Fatalf("empty source directory should be removed, err=%v", err)
	}
}

func TestCopy_RenameDottedDirectoryKeepsFullName(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "F.I.R/inner.txt", "incoming")
	os.MkdirAll(filepath.Join(root, "dest", "F.I.R"), 0o755)

	results := svc.Copy(context.Background(), []string{"/F.I.R"}, "/dest", true)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("dotted directory copy failed: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "F.I.R (2)", "inner.txt")); err != nil {
		t.Fatalf("expected full directory name suffix: %v", err)
	}
	if strings.Contains(results[0].Target, ".R") && strings.Contains(results[0].Target, "(2).R") {
		t.Fatalf("directory was incorrectly treated as a file: %q", results[0].Target)
	}
}

func TestCopy_RenameDottedFileKeepsExtension(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "F.I.R", "incoming")
	os.MkdirAll(filepath.Join(root, "dest"), 0o755)
	writeFile(t, root, "dest/F.I.R", "existing")

	results := svc.CopyWithPolicy(context.Background(), []string{"/F.I.R"}, "/dest", ConflictRename)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("dotted file copy failed: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "F.I (2).R")); err != nil {
		t.Fatalf("expected dotted file suffix: %v", err)
	}
}

func TestCopy_MergeTypeMismatchRenamesIncoming(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "source/item", "incoming file")
	os.MkdirAll(filepath.Join(root, "dest", "item"), 0o755)

	results := svc.CopyWithPolicy(context.Background(), []string{"/source/item"}, "/dest", ConflictMergeDirs)
	if len(results) != 1 || !results[0].OK || results[0].Outcome != "renamed" {
		t.Fatalf("type mismatch should be renamed: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "item (2)")); err != nil {
		t.Fatalf("renamed type-mismatch file missing: %v", err)
	}
}

func TestInspectTransfer_DirectoryAndBatchConflicts(t *testing.T) {
	svc, root := setupTestRoot(t)
	os.MkdirAll(filepath.Join(root, "A", "one"), 0o755)
	os.MkdirAll(filepath.Join(root, "B", "one"), 0o755)
	os.MkdirAll(filepath.Join(root, "C", "two"), 0o755)
	os.MkdirAll(filepath.Join(root, "D", "one"), 0o755)
	writeFile(t, root, "A/file.txt", "a")
	writeFile(t, root, "C/file.txt", "c")

	inspection, err := svc.InspectTransfer(context.Background(), TransferMove, []string{"/A/one", "/C/two", "/A/file.txt", "/C/file.txt"}, "/D")
	if err != nil {
		t.Fatalf("inspect failed: %v", err)
	}
	if !inspection.HasConflicts || inspection.DirectoryConflicts != 1 || inspection.OtherConflicts != 1 {
		t.Fatalf("unexpected inspection: %+v", inspection)
	}
}

func TestTreeSize(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "tree/a.txt", "12345")     // 5 bytes
	writeFile(t, root, "tree/sub/b.txt", "67890") // 5 bytes

	n, err := svc.treeSize(context.Background(), "/tree")
	if err != nil {
		t.Fatal(err)
	}
	if n != 10 {
		t.Errorf("expected total 10 bytes, got %d", n)
	}
}
