package local

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flist/internal/storage"
	"flist/internal/util"
)

// newUploader 构造一个带 root 与 staging 的本地驱动用于上传测试。
func newUploader(t *testing.T) (*Local, string, string) {
	t.Helper()
	rootReal, err := util.ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	staging := t.TempDir()
	return New(rootReal, staging), rootReal, staging
}

// stageAll 把 chunks 依序写入暂存区。
func stageAll(t *testing.T, b *Local, id string, chunks [][]byte) {
	t.Helper()
	for i, c := range chunks {
		n, err := b.StageChunk(context.Background(), id, i, bytes.NewReader(c))
		if err != nil {
			t.Fatalf("StageChunk %d: %v", i, err)
		}
		if n != int64(len(c)) {
			t.Fatalf("StageChunk %d wrote %d, want %d", i, n, len(c))
		}
	}
}

func TestUploader_StageAndMerge(t *testing.T) {
	b, root, _ := newUploader(t)
	id := "upload-abc_123"
	chunks := [][]byte{[]byte("hello "), []byte("brave "), []byte("world")}
	stageAll(t, b, id, chunks)

	if err := b.MergeUpload(context.Background(), id, "/out.txt", len(chunks), false); err != nil {
		t.Fatalf("MergeUpload: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "out.txt"))
	if err != nil {
		t.Fatalf("read merged: %v", err)
	}
	if string(got) != "hello brave world" {
		t.Errorf("merged content = %q", got)
	}
	// 合并成功后暂存区应被清理。
	if _, err := os.Stat(b.stagingPath(id)); !os.IsNotExist(err) {
		t.Errorf("staging dir should be removed after merge, err=%v", err)
	}
}

func TestUploader_StageChunkIdempotent(t *testing.T) {
	b, root, _ := newUploader(t)
	id := "id1"
	// 同一 index 重传：后写覆盖前写。
	b.StageChunk(context.Background(), id, 0, strings.NewReader("AAAA"))
	b.StageChunk(context.Background(), id, 0, strings.NewReader("BBBB"))

	if err := b.MergeUpload(context.Background(), id, "/x.txt", 1, false); err != nil {
		t.Fatalf("merge: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "x.txt"))
	if string(got) != "BBBB" {
		t.Errorf("retransmit should overwrite, got %q", got)
	}
}

func TestUploader_MergeMissingChunk(t *testing.T) {
	b, _, _ := newUploader(t)
	id := "id2"
	// 只写 0、2，缺 1。
	b.StageChunk(context.Background(), id, 0, strings.NewReader("a"))
	b.StageChunk(context.Background(), id, 2, strings.NewReader("c"))

	err := b.MergeUpload(context.Background(), id, "/y.txt", 3, false)
	if err == nil {
		t.Fatal("merge with missing chunk should fail")
	}
}

func TestUploader_MergeNoOverwrite(t *testing.T) {
	b, root, _ := newUploader(t)
	os.WriteFile(filepath.Join(root, "exists.txt"), []byte("old"), 0o644)

	id := "id3"
	stageAll(t, b, id, [][]byte{[]byte("new")})
	err := b.MergeUpload(context.Background(), id, "/exists.txt", 1, false)
	if err != storage.ErrExists {
		t.Errorf("expected ErrExists without overwrite, got %v", err)
	}
}

func TestUploader_MergeOverwrite(t *testing.T) {
	b, root, _ := newUploader(t)
	os.WriteFile(filepath.Join(root, "exists.txt"), []byte("old-content"), 0o644)

	id := "id4"
	stageAll(t, b, id, [][]byte{[]byte("brand-new")})
	if err := b.MergeUpload(context.Background(), id, "/exists.txt", 1, true); err != nil {
		t.Fatalf("overwrite merge: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "exists.txt"))
	if string(got) != "brand-new" {
		t.Errorf("overwrite content = %q", got)
	}
}

func TestUploader_MergeOverwriteDirRejected(t *testing.T) {
	b, root, _ := newUploader(t)
	os.MkdirAll(filepath.Join(root, "adir"), 0o755)

	id := "id5"
	stageAll(t, b, id, [][]byte{[]byte("x")})
	// 目标是目录，即便 overwrite=true 也不可用文件覆盖。
	if err := b.MergeUpload(context.Background(), id, "/adir", 1, true); err != storage.ErrExists {
		t.Errorf("overwriting a dir should fail with ErrExists, got %v", err)
	}
}

func TestUploader_MergeParentMissing(t *testing.T) {
	b, _, _ := newUploader(t)
	id := "id6"
	stageAll(t, b, id, [][]byte{[]byte("x")})
	// 父目录不存在 → NotFound。
	if err := b.MergeUpload(context.Background(), id, "/no/such/dir/file.txt", 1, false); err != storage.ErrNotFound {
		t.Errorf("missing parent should be ErrNotFound, got %v", err)
	}
}

func TestUploader_AbortChunkAndUpload(t *testing.T) {
	b, _, staging := newUploader(t)
	id := "id7"
	stageAll(t, b, id, [][]byte{[]byte("a"), []byte("b")})

	// 删单个分片。
	if err := b.AbortChunk(id, 1); err != nil {
		t.Fatalf("AbortChunk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, id, "1.part")); !os.IsNotExist(err) {
		t.Errorf("chunk 1 should be removed")
	}

	// 删整个暂存区（幂等）。
	if err := b.AbortUpload(id); err != nil {
		t.Fatalf("AbortUpload: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, id)); !os.IsNotExist(err) {
		t.Errorf("staging dir should be removed")
	}
	// 再删一次不报错。
	if err := b.AbortUpload(id); err != nil {
		t.Errorf("AbortUpload should be idempotent, got %v", err)
	}
}

func TestUploader_SweepStaging(t *testing.T) {
	b, _, staging := newUploader(t)
	// 新会话（不应被清）。
	stageAll(t, b, "fresh", [][]byte{[]byte("x")})
	// 旧会话（mtime 设为 48h 前）。
	stageAll(t, b, "stale", [][]byte{[]byte("y")})
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(filepath.Join(staging, "stale"), old, old)

	n, err := b.SweepStaging(24 * time.Hour)
	if err != nil {
		t.Fatalf("SweepStaging: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 stale dir cleaned, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(staging, "stale")); !os.IsNotExist(err) {
		t.Error("stale staging dir should be removed")
	}
	if _, err := os.Stat(filepath.Join(staging, "fresh")); err != nil {
		t.Error("fresh staging dir should remain")
	}
}

func TestUploader_InvalidUploadID(t *testing.T) {
	b, _, _ := newUploader(t)
	// 含路径分隔符的 id 应被拒，防止逃逸 staging。
	if _, err := b.StageChunk(context.Background(), "../escape", 0, strings.NewReader("x")); err != storage.ErrBadOp {
		t.Errorf("malicious upload_id should be rejected, got %v", err)
	}
}

func TestUploader_NoStagingDirNotSupported(t *testing.T) {
	rootReal, _ := util.ResolveRoot(t.TempDir())
	b := New(rootReal, "") // 空 staging → 不支持上传。
	if _, err := b.StageChunk(context.Background(), "id", 0, strings.NewReader("x")); err != storage.ErrNotSupported {
		t.Errorf("empty staging should yield ErrNotSupported, got %v", err)
	}
}
