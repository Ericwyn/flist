package local

import (
	"context"
	"testing"

	"flist/internal/storage"
	"flist/internal/util"
)

func TestLocalUsage(t *testing.T) {
	real, err := util.ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	b := New(real)

	total, free, err := b.Usage(context.Background(), "/")
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if total == 0 || free == 0 || free > total {
		t.Errorf("unexpected usage total=%d free=%d", total, free)
	}
}

func TestLocalUsage_Traversal(t *testing.T) {
	real, err := util.ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	b := New(real)

	// 越界路径会被 SafeResolve 钳制回 root 内，仍返回 root 所在文件系统用量（不报错）。
	if _, _, err := b.Usage(context.Background(), "/../../.."); err != nil {
		t.Errorf("clamped traversal should still resolve usage, got %v", err)
	}

	// 确认实现了可选接口。
	var _ storage.Usager = b
}
