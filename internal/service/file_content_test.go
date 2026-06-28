package service

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/storage/local"
	"flist/internal/util"
)

// setupTestRootWithMaxEdit 同 setupTestRoot，但指定可编辑大小上限（字节）。
func setupTestRootWithMaxEdit(t *testing.T, maxEdit int64) (*FileService, string) {
	t.Helper()
	real, err := util.ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	return NewFileService(local.New(real, t.TempDir()), util.NewPathLocker(), maxEdit), real
}

func TestReadContent_OK(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "note.md", "# title\nbody\n")

	res, err := svc.ReadContent(context.Background(), "/note.md")
	if err != nil {
		t.Fatalf("ReadContent: %v", err)
	}
	if res.Content != "# title\nbody\n" || res.Path != "/note.md" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestReadContent_TooLargeUsesServiceLimit(t *testing.T) {
	// maxEdit=10：超过上限的文件应被拒绝。
	svc, root := setupTestRootWithMaxEdit(t, 10)
	writeFile(t, root, "big.txt", "0123456789ABCDEF")

	if _, err := svc.ReadContent(context.Background(), "/big.txt"); err != storage.ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestSaveContent_OptimisticLock(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "todo.md", "old")

	cur, err := svc.ReadContent(context.Background(), "/todo.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveContent(context.Background(), "/todo.md", []byte("new"), cur.Revision, false); err != nil {
		t.Fatalf("SaveContent: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "todo.md")); string(got) != "new" {
		t.Errorf("content mismatch: %q", got)
	}

	// 用过期 revision 再保存应冲突。
	if _, err := svc.SaveContent(context.Background(), "/todo.md", []byte("x"), cur.Revision, false); err != storage.ErrFileModified {
		t.Errorf("expected ErrFileModified, got %v", err)
	}
}

func TestSaveContent_TooLarge(t *testing.T) {
	svc, root := setupTestRootWithMaxEdit(t, 8)
	writeFile(t, root, "f.txt", "x")
	cur, _ := svc.ReadContent(context.Background(), "/f.txt")

	if _, err := svc.SaveContent(context.Background(), "/f.txt", []byte("0123456789"), cur.Revision, false); err != storage.ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestSaveContent_ConcurrentSerialized(t *testing.T) {
	svc, root := setupTestRoot(t)
	writeFile(t, root, "race.txt", "init")

	// 并发保存（force=true 绕过乐观锁，仅验证 path lock 串行化下不产生交错写）。
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			content := []byte(string(rune('A' + n%26)))
			_, _ = svc.SaveContent(context.Background(), "/race.txt", content, model.FileRevision{}, true)
		}(i)
	}
	wg.Wait()

	// 最终内容应为某次完整写入（长度为 1），不应是交错的脏数据。
	got, _ := os.ReadFile(filepath.Join(root, "race.txt"))
	if len(got) != 1 {
		t.Errorf("expected single-byte final content from serialized writes, got %q", got)
	}
}

func TestSpace_Local(t *testing.T) {
	svc, _ := setupTestRoot(t)
	res, err := svc.Space(context.Background(), "/")
	if err != nil {
		t.Fatalf("Space: %v", err)
	}
	if !res.Space.Supported {
		t.Fatal("local should support space")
	}
	if res.Space.Total == 0 || res.Space.Free == 0 {
		t.Errorf("unexpected space: %+v", res.Space)
	}
	if res.Space.Free != res.Space.Available {
		t.Errorf("free and available should match in phase 1: %+v", res.Space)
	}
	if res.Space.Used+res.Space.Free != res.Space.Total {
		t.Errorf("used+free should equal total: %+v", res.Space)
	}
	if res.Mount.Name != "local" || res.Mount.Prefix != "/" {
		t.Errorf("unexpected mount: %+v", res.Mount)
	}
}

func TestSpace_PathNotFound(t *testing.T) {
	svc, _ := setupTestRoot(t)
	if _, err := svc.Space(context.Background(), "/ghost"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound for missing path, got %v", err)
	}
}
