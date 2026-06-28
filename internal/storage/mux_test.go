package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/storage/local"
	"flist/internal/util"
)

// newLocalMount 在临时目录上建一个 local 驱动挂载点，并预置若干文件。
func newLocalMount(t *testing.T, name string, files map[string]string) storage.Mount {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	real, err := util.ResolveRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return storage.Mount{Name: name, Backend: local.New(real, "")}
}

func TestMux_ListRoot(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{"a.txt": "a"}),
		newLocalMount(t, "box1", map[string]string{"b.txt": "b"}),
	})

	items, err := mux.List(context.Background(), "/", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("virtual root should list 2 mounts, got %d", len(items))
	}
	if items[0].Name != "local" || items[0].Type != model.TypeDir {
		t.Errorf("first mount wrong: %+v", items[0])
	}
	if items[1].Name != "box1" {
		t.Errorf("second mount wrong: %+v", items[1])
	}
}

func TestMux_RouteIntoMount(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{"a.txt": "hello"}),
		newLocalMount(t, "box1", map[string]string{"sub/b.txt": "world"}),
	})

	// 列 /local 下应见 a.txt。
	items, err := mux.List(context.Background(), "/local", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "a.txt" {
		t.Fatalf("expected a.txt under /local, got %+v", items)
	}

	// Stat /box1/sub/b.txt。
	info, err := mux.Stat(context.Background(), "/box1/sub/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "b.txt" || info.Size != 5 {
		t.Errorf("unexpected stat: %+v", info)
	}
}

func TestMux_MountIsolation(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{"a.txt": "a"}),
		newLocalMount(t, "box1", map[string]string{"b.txt": "b"}),
	})

	// box1 下不应看到 local 的文件。
	if _, err := mux.Stat(context.Background(), "/box1/a.txt"); err != storage.ErrNotFound {
		t.Errorf("box1 should not see local's a.txt, got %v", err)
	}
}

func TestMux_UnknownMount(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{newLocalMount(t, "local", nil)})
	if _, err := mux.Stat(context.Background(), "/ghost/x"); err != storage.ErrNotFound {
		t.Errorf("unknown mount should be ErrNotFound, got %v", err)
	}
}

func TestMux_WriteAtMountLevelRejected(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{newLocalMount(t, "local", nil)})
	// 在挂载点层级（rel="/"）建目录应被拒。
	if err := mux.Mkdir(context.Background(), "/local"); err != storage.ErrBadOp {
		t.Errorf("mkdir at mount level should be ErrBadOp, got %v", err)
	}
	// 在虚拟根创建一个不存在的挂载点 → 该挂载点不存在。
	if err := mux.Mkdir(context.Background(), "/newmount"); err != storage.ErrNotFound {
		t.Errorf("mkdir of unknown mount should be ErrNotFound, got %v", err)
	}
}

func TestMux_CrossMountMoveNotSupported(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{"a.txt": "a"}),
		newLocalMount(t, "box1", nil),
	})
	err := mux.Move(context.Background(), "/local/a.txt", "/box1/a.txt")
	if err != storage.ErrNotSupported {
		t.Errorf("cross-mount move should be ErrNotSupported, got %v", err)
	}
}

func TestMux_SameMountMove(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{"a.txt": "a"}),
	})
	if err := mux.Move(context.Background(), "/local/a.txt", "/local/b.txt"); err != nil {
		t.Fatalf("same-mount move should succeed, got %v", err)
	}
	if _, err := mux.Stat(context.Background(), "/local/b.txt"); err != nil {
		t.Errorf("moved file should exist at new path, got %v", err)
	}
}

func TestMux_WalkPrefixesMountName(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{"sub/keyword.txt": "x"}),
		newLocalMount(t, "box1", map[string]string{"keyword.txt": "y"}),
	})

	var paths []string
	err := mux.Walk(context.Background(), "/", false, func(rel string, info model.FileInfo) error {
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// 应包含带挂载点前缀的路径。
	want := map[string]bool{
		"local":                 false,
		"local/sub":             false,
		"local/sub/keyword.txt": false,
		"box1":                  false,
		"box1/keyword.txt":      false,
	}
	for _, p := range paths {
		if _, ok := want[p]; ok {
			want[p] = true
		}
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("walk should have visited %q; got paths=%v", p, paths)
		}
	}
}

func TestMux_WalkIntoMount(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{"sub/deep.txt": "x", "top.txt": "y"}),
	})

	var paths []string
	err := mux.Walk(context.Background(), "/local", false, func(rel string, info model.FileInfo) error {
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 在挂载点子树内遍历，relPath 相对该挂载点（不带 local 前缀）。
	found := map[string]bool{}
	for _, p := range paths {
		found[p] = true
	}
	if !found["top.txt"] || !found["sub/deep.txt"] {
		t.Errorf("walk into mount missing entries, got %v", paths)
	}
}

func TestMux_Usage(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", nil),
	})

	// 路由到挂载点 → 委托其 Usager。
	total, free, err := mux.Usage(context.Background(), "/local")
	if err != nil {
		t.Fatalf("Usage into mount: %v", err)
	}
	if total == 0 || free == 0 || free > total {
		t.Errorf("unexpected usage total=%d free=%d", total, free)
	}

	// 虚拟根无单一存储用量 → ErrNotSupported。
	if _, _, err := mux.Usage(context.Background(), "/"); err != storage.ErrNotSupported {
		t.Errorf("virtual root usage should be ErrNotSupported, got %v", err)
	}

	// 未知挂载点 → ErrNotFound。
	if _, _, err := mux.Usage(context.Background(), "/ghost"); err != storage.ErrNotFound {
		t.Errorf("unknown mount usage should be ErrNotFound, got %v", err)
	}
}
