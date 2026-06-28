package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"flist/internal/storage/local"
	"flist/internal/store"
	"flist/internal/util"
)

// setupBookmarkSvc 构造 BookmarkService（内存库 + 临时 root），返回 svc、root 路径与用户 ID。
func setupBookmarkSvc(t *testing.T) (*BookmarkService, string, int64) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=foreign_keys(ON)"
	st, err := store.OpenWithDSN(dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	uid, err := st.CreateUser("tester", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	dir := t.TempDir()
	real, err := util.ResolveRoot(dir)
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	return NewBookmarkService(st, local.New(real, t.TempDir())), real, uid
}

func TestBookmark_CreateAndList(t *testing.T) {
	svc, root, uid := setupBookmarkSvc(t)
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)

	bm, err := svc.Create(context.Background(), uid, "文档", "/docs")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if bm.Name != "文档" || bm.Path != "/docs" || !bm.Valid {
		t.Errorf("unexpected bookmark: %+v", bm)
	}

	items, err := svc.List(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Path != "/docs" || !items[0].Valid {
		t.Errorf("unexpected list: %+v", items)
	}
}

func TestBookmark_NameFallback(t *testing.T) {
	svc, root, uid := setupBookmarkSvc(t)
	os.MkdirAll(filepath.Join(root, "photos"), 0o755)

	// name 为空 → 回落为 basename。
	bm, err := svc.Create(context.Background(), uid, "", "/photos")
	if err != nil {
		t.Fatal(err)
	}
	if bm.Name != "photos" {
		t.Errorf("expected name fallback 'photos', got %q", bm.Name)
	}
}

func TestBookmark_RejectFile(t *testing.T) {
	svc, root, uid := setupBookmarkSvc(t)
	writeFile(t, root, "file.txt", "x")

	// 收藏文件应被拒（仅允许目录）。
	if _, err := svc.Create(context.Background(), uid, "", "/file.txt"); err != ErrNotDir {
		t.Errorf("expected ErrNotDir for file, got %v", err)
	}
}

func TestBookmark_RejectNonexistent(t *testing.T) {
	svc, _, uid := setupBookmarkSvc(t)
	if _, err := svc.Create(context.Background(), uid, "", "/ghost"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBookmark_DuplicateRejected(t *testing.T) {
	svc, root, uid := setupBookmarkSvc(t)
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)

	if _, err := svc.Create(context.Background(), uid, "a", "/docs"); err != nil {
		t.Fatal(err)
	}
	// 同一路径重复收藏 → ErrBookmarkExists。
	if _, err := svc.Create(context.Background(), uid, "b", "/docs"); err != ErrBookmarkExists {
		t.Errorf("expected ErrBookmarkExists, got %v", err)
	}
}

func TestBookmark_Update(t *testing.T) {
	svc, root, uid := setupBookmarkSvc(t)
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	bm, _ := svc.Create(context.Background(), uid, "old", "/docs")

	if err := svc.Update(context.Background(), uid, bm.ID, "new"); err != nil {
		t.Fatalf("update: %v", err)
	}
	items, _ := svc.List(context.Background(), uid)
	if items[0].Name != "new" {
		t.Errorf("rename failed, got %q", items[0].Name)
	}
}

func TestBookmark_Delete(t *testing.T) {
	svc, root, uid := setupBookmarkSvc(t)
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	bm, _ := svc.Create(context.Background(), uid, "x", "/docs")

	if err := svc.Delete(context.Background(), uid, bm.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	items, _ := svc.List(context.Background(), uid)
	if len(items) != 0 {
		t.Errorf("expected empty after delete, got %d", len(items))
	}
}

func TestBookmark_UpdateNotFound(t *testing.T) {
	svc, _, uid := setupBookmarkSvc(t)
	if err := svc.Update(context.Background(), uid, 999, "x"); err != ErrBookmarkNotFound {
		t.Errorf("expected ErrBookmarkNotFound, got %v", err)
	}
}

func TestBookmark_CrossUserIsolation(t *testing.T) {
	svc, root, uid := setupBookmarkSvc(t)
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	bm, _ := svc.Create(context.Background(), uid, "x", "/docs")

	// 另一个用户不能改 / 删本人收藏。
	otherUID := uid + 100
	if err := svc.Update(context.Background(), otherUID, bm.ID, "hacked"); err != ErrBookmarkNotFound {
		t.Errorf("cross-user update should be denied, got %v", err)
	}
	if err := svc.Delete(context.Background(), otherUID, bm.ID); err != ErrBookmarkNotFound {
		t.Errorf("cross-user delete should be denied, got %v", err)
	}
}

func TestBookmark_InvalidDetection(t *testing.T) {
	svc, root, uid := setupBookmarkSvc(t)
	docs := filepath.Join(root, "docs")
	os.MkdirAll(docs, 0o755)
	svc.Create(context.Background(), uid, "x", "/docs")

	// 删除目录后，List 应标记 Valid=false。
	os.RemoveAll(docs)
	items, _ := svc.List(context.Background(), uid)
	if len(items) != 1 || items[0].Valid {
		t.Errorf("deleted dir bookmark should be invalid, got %+v", items)
	}
}

func TestBookmark_Reorder(t *testing.T) {
	svc, root, uid := setupBookmarkSvc(t)
	for _, n := range []string{"a", "b", "c"} {
		os.MkdirAll(filepath.Join(root, n), 0o755)
	}
	a, _ := svc.Create(context.Background(), uid, "a", "/a")
	b, _ := svc.Create(context.Background(), uid, "b", "/b")
	c, _ := svc.Create(context.Background(), uid, "c", "/c")

	// 反转顺序：c, b, a。
	orders := []store.BookmarkOrder{
		{ID: c.ID, SortOrder: 1},
		{ID: b.ID, SortOrder: 2},
		{ID: a.ID, SortOrder: 3},
	}
	if err := svc.Reorder(context.Background(), uid, orders); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	items, _ := svc.List(context.Background(), uid)
	if len(items) != 3 || items[0].Name != "c" || items[2].Name != "a" {
		t.Errorf("reorder failed: %+v", items)
	}
}

func TestBookmark_RootValid(t *testing.T) {
	svc, _, uid := setupBookmarkSvc(t)
	// 收藏根目录：name 缺省回落「我的文件」，且有效。
	bm, err := svc.Create(context.Background(), uid, "", "/")
	if err != nil {
		t.Fatalf("create root bookmark: %v", err)
	}
	if bm.Name != "我的文件" || !bm.Valid {
		t.Errorf("unexpected root bookmark: %+v", bm)
	}
}
