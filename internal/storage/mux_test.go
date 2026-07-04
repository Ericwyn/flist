package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestMux_CrossMountCopyFile(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{"a.txt": "hello world"}),
		newLocalMount(t, "box1", nil),
	})
	if err := mux.Copy(context.Background(), "/local/a.txt", "/box1/a.txt"); err != nil {
		t.Fatalf("cross-mount copy should succeed, got %v", err)
	}
	// 目标存在且内容一致。
	info, err := mux.Stat(context.Background(), "/box1/a.txt")
	if err != nil {
		t.Fatalf("copied file should exist, got %v", err)
	}
	if info.Size != int64(len("hello world")) {
		t.Errorf("copied size mismatch: %d", info.Size)
	}
	// 源仍在（copy 不删源）。
	if _, err := mux.Stat(context.Background(), "/local/a.txt"); err != nil {
		t.Errorf("source should remain after copy, got %v", err)
	}
}

func TestMux_CrossMountCopyDir(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{
			"dir/a.txt":     "aaa",
			"dir/sub/b.txt": "bbb",
		}),
		newLocalMount(t, "box1", nil),
	})
	if err := mux.Copy(context.Background(), "/local/dir", "/box1/dir"); err != nil {
		t.Fatalf("cross-mount dir copy should succeed, got %v", err)
	}
	for _, p := range []string{"/box1/dir/a.txt", "/box1/dir/sub/b.txt"} {
		if _, err := mux.Stat(context.Background(), p); err != nil {
			t.Errorf("expected %s to exist after dir copy, got %v", p, err)
		}
	}
}

func TestMux_CrossMountMove(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{"a.txt": "data"}),
		newLocalMount(t, "box1", nil),
	})
	if err := mux.Move(context.Background(), "/local/a.txt", "/box1/a.txt"); err != nil {
		t.Fatalf("cross-mount move should succeed, got %v", err)
	}
	if _, err := mux.Stat(context.Background(), "/box1/a.txt"); err != nil {
		t.Errorf("moved file should exist at destination, got %v", err)
	}
	// move = 复制 + 删源，源应消失。
	if _, err := mux.Stat(context.Background(), "/local/a.txt"); err != storage.ErrNotFound {
		t.Errorf("source should be removed after move, got %v", err)
	}
}

func TestMux_CrossMountCopyExists(t *testing.T) {
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{"a.txt": "a"}),
		newLocalMount(t, "box1", map[string]string{"a.txt": "existing"}),
	})
	if err := mux.Copy(context.Background(), "/local/a.txt", "/box1/a.txt"); err != storage.ErrExists {
		t.Errorf("copy onto existing target should be ErrExists, got %v", err)
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

func TestMux_AddRemoveMount(t *testing.T) {
	mux := storage.NewMux(nil) // 空命名空间，模拟设备 Mux
	if len(mux.Mounts()) != 0 {
		t.Fatalf("empty mux should have no mounts")
	}

	dev := newLocalMount(t, "sdc1", map[string]string{"file.txt": "x"})
	if err := mux.AddMount(dev); err != nil {
		t.Fatalf("AddMount: %v", err)
	}
	// 重复同名 → ErrExists。
	if err := mux.AddMount(dev); err != storage.ErrExists {
		t.Errorf("duplicate AddMount should be ErrExists, got %v", err)
	}

	// route / List 反映新挂载点。
	if _, err := mux.Stat(context.Background(), "/sdc1/file.txt"); err != nil {
		t.Errorf("mounted device file should be reachable, got %v", err)
	}
	items, err := mux.List(context.Background(), "/", false)
	if err != nil || len(items) != 1 || items[0].Name != "sdc1" {
		t.Errorf("root should list the added mount, got %+v err=%v", items, err)
	}

	// 移除后返回被移除的 backend，且不再可达。
	if b := mux.RemoveMount("sdc1"); b == nil {
		t.Errorf("RemoveMount should return the removed backend")
	}
	if _, err := mux.Stat(context.Background(), "/sdc1/file.txt"); err != storage.ErrNotFound {
		t.Errorf("removed mount should be unreachable, got %v", err)
	}
	// 幂等移除。
	if b := mux.RemoveMount("sdc1"); b != nil {
		t.Errorf("removing absent mount should return nil")
	}
}

func TestMux_ConcurrentAddRemoveList(t *testing.T) {
	mux := storage.NewMux(nil)
	base := newLocalMount(t, "base", map[string]string{"a.txt": "a"})
	if err := mux.AddMount(base); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "dev" + string(rune('a'+n%10))
			_ = mux.AddMount(storage.Mount{Name: name, Backend: base.Backend})
			_, _ = mux.List(context.Background(), "/", false)
			_ = mux.Mounts()
			mux.RemoveMount(name)
		}(i)
	}
	// 并发读：持续列举 base 挂载点。
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = mux.Stat(context.Background(), "/base/a.txt")
		}()
	}
	wg.Wait()
}

// cancelFirstReadBackend 包装一个 Backend，在首次 Open 的文件被读取时立刻取消 ctx，
// 用于验证跨挂载复制的取消 + 半成品清理。
func TestMux_CrossMountCopyCancel(t *testing.T) {
	// 用较大内容确保 io.Copy 分多次 Write，取消能在中途生效。
	big := strings.Repeat("0123456789", 200000) // ~2MB
	mux := storage.NewMux([]storage.Mount{
		newLocalMount(t, "local", map[string]string{"big.bin": big}),
		newLocalMount(t, "box1", nil),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消：复制应在第一次 ctx 检查即中止

	err := mux.CopyWithProgress(ctx, "/local/big.bin", "/box1/big.bin", func(int64) {})
	if err == nil {
		t.Fatalf("cancelled copy should return error")
	}
	// 目标不应残留半成品（streamWriter 中途放弃时清理）。
	if _, serr := mux.Stat(context.Background(), "/box1/big.bin"); serr != storage.ErrNotFound {
		t.Errorf("cancelled copy should leave no target file, got %v", serr)
	}
	// 源保留。
	if _, serr := mux.Stat(context.Background(), "/local/big.bin"); serr != nil {
		t.Errorf("source must remain after cancelled copy, got %v", serr)
	}
}
