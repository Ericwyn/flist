package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"flist/internal/storage"
	"flist/internal/storage/local"
	"flist/internal/util"
)

// setupUpload 构造 UploadService（本地驱动 + staging + 路径锁），返回 svc 与 root。
func setupUpload(t *testing.T) (*UploadService, string) {
	t.Helper()
	rootReal, err := util.ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	backend := local.New(rootReal, t.TempDir())
	return NewUploadService(backend, util.NewPathLocker(), 0), rootReal
}

// uploadAll 走完 init→chunk→complete，返回落盘路径。data 按 chunkSize 切片。
func uploadAll(t *testing.T, svc *UploadService, dir, name string, data []byte, chunkSize int64, overwrite bool) string {
	t.Helper()
	ctx := context.Background()
	init, err := svc.Init(ctx, "user1", dir, name, "fp:"+name, int64(len(data)), chunkSize)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	cs := init.ChunkSize
	for i := 0; i < init.TotalChunks; i++ {
		start := int64(i) * cs
		end := start + cs
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		if _, err := svc.Chunk(ctx, init.UploadID, i, bytes.NewReader(data[start:end])); err != nil {
			t.Fatalf("Chunk %d: %v", i, err)
		}
	}
	res, err := svc.Complete(ctx, init.UploadID, overwrite)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	return res.Path
}

func TestUpload_FullRoundTrip(t *testing.T) {
	svc, root := setupUpload(t)
	// 用小 chunkSize 强制多分片（会被钳制到 minChunkSize，但 totalChunks 仍按归一后算）。
	data := bytes.Repeat([]byte("0123456789"), 100000) // ~1MB
	p := uploadAll(t, svc, "/", "big.bin", data, 256<<10, false)
	if p != "/big.bin" {
		t.Errorf("unexpected path %q", p)
	}
	got, err := os.ReadFile(filepath.Join(root, "big.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("content mismatch: len got=%d want=%d", len(got), len(data))
	}
}

func TestUpload_ResumeByFingerprint(t *testing.T) {
	svc, _ := setupUpload(t)
	ctx := context.Background()
	data := bytes.Repeat([]byte("x"), 600<<10) // 经 256KiB chunk → 3 片
	init, err := svc.Init(ctx, "user1", "/", "r.bin", "fp-resume", int64(len(data)), 256<<10)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	// 只传第 0 片。
	if _, err := svc.Chunk(ctx, init.UploadID, 0, bytes.NewReader(data[:init.ChunkSize])); err != nil {
		t.Fatalf("chunk0: %v", err)
	}

	// 再次 Init（相同指纹）应复用会话并返回已收分片 [0]。
	init2, err := svc.Init(ctx, "user1", "/", "r.bin", "fp-resume", int64(len(data)), 256<<10)
	if err != nil {
		t.Fatalf("init resume: %v", err)
	}
	if init2.UploadID != init.UploadID {
		t.Errorf("resume should reuse upload_id, got %q vs %q", init2.UploadID, init.UploadID)
	}
	if len(init2.Received) != 1 || init2.Received[0] != 0 {
		t.Errorf("resume should report received [0], got %v", init2.Received)
	}
}

func TestUpload_ResumeUserIsolation(t *testing.T) {
	svc, _ := setupUpload(t)
	ctx := context.Background()
	data := bytes.Repeat([]byte("x"), 300<<10)
	init1, _ := svc.Init(ctx, "alice", "/", "f.bin", "same-fp", int64(len(data)), 256<<10)

	// 另一用户相同指纹不应命中 alice 的会话。
	init2, _ := svc.Init(ctx, "bob", "/", "f.bin", "same-fp", int64(len(data)), 256<<10)
	if init2.UploadID == init1.UploadID {
		t.Error("different users should not share upload session")
	}
}

func TestUpload_IncompleteMissing(t *testing.T) {
	svc, _ := setupUpload(t)
	ctx := context.Background()
	data := bytes.Repeat([]byte("y"), 600<<10) // 3 片
	init, _ := svc.Init(ctx, "u", "/", "m.bin", "fp-m", int64(len(data)), 256<<10)
	// 只传第 0、2 片，缺 1。
	svc.Chunk(ctx, init.UploadID, 0, bytes.NewReader(data[:init.ChunkSize]))
	svc.Chunk(ctx, init.UploadID, 2, bytes.NewReader(data[2*init.ChunkSize:]))

	res, err := svc.Complete(ctx, init.UploadID, false)
	if err != ErrUploadIncomplete {
		t.Fatalf("expected ErrUploadIncomplete, got %v", err)
	}
	if res == nil || len(res.Missing) != 1 || res.Missing[0] != 1 {
		t.Errorf("expected missing [1], got %+v", res)
	}
}

func TestUpload_ChunkWrongSizeRejected(t *testing.T) {
	svc, _ := setupUpload(t)
	ctx := context.Background()
	data := bytes.Repeat([]byte("z"), 600<<10) // 3 片，每片 256KiB（末片更小）
	init, _ := svc.Init(ctx, "u", "/", "w.bin", "fp-w", int64(len(data)), 256<<10)
	// 第 0 片传错误大小（少于应有）。
	_, err := svc.Chunk(ctx, init.UploadID, 0, bytes.NewReader([]byte("too short")))
	if err != storage.ErrBadOp {
		t.Errorf("wrong-size chunk should be rejected, got %v", err)
	}
}

func TestUpload_DefaultNoOverwrite(t *testing.T) {
	svc, root := setupUpload(t)
	os.WriteFile(filepath.Join(root, "dup.bin"), []byte("existing"), 0o644)

	ctx := context.Background()
	data := []byte("new data")
	init, _ := svc.Init(ctx, "u", "/", "dup.bin", "fp-dup", int64(len(data)), 256<<10)
	svc.Chunk(ctx, init.UploadID, 0, bytes.NewReader(data))
	if _, err := svc.Complete(ctx, init.UploadID, false); err != storage.ErrExists {
		t.Errorf("complete without overwrite on existing should be ErrExists, got %v", err)
	}
}

func TestUpload_Overwrite(t *testing.T) {
	svc, root := setupUpload(t)
	os.WriteFile(filepath.Join(root, "dup.bin"), []byte("existing-old"), 0o644)
	uploadAll(t, svc, "/", "dup.bin", []byte("fresh"), 256<<10, true)
	got, _ := os.ReadFile(filepath.Join(root, "dup.bin"))
	if string(got) != "fresh" {
		t.Errorf("overwrite content = %q", got)
	}
}

func TestUpload_TooLarge(t *testing.T) {
	rootReal, _ := util.ResolveRoot(t.TempDir())
	backend := local.New(rootReal, t.TempDir())
	svc := NewUploadService(backend, util.NewPathLocker(), 1024) // 上限 1KiB

	_, err := svc.Init(context.Background(), "u", "/", "huge.bin", "fp", 2048, 256<<10)
	if err != ErrUploadTooLarge {
		t.Errorf("expected ErrUploadTooLarge, got %v", err)
	}
}

func TestUpload_DirNotFound(t *testing.T) {
	svc, _ := setupUpload(t)
	_, err := svc.Init(context.Background(), "u", "/ghost", "f.bin", "fp", 10, 256<<10)
	if err != storage.ErrNotFound {
		t.Errorf("init into nonexistent dir should be ErrNotFound, got %v", err)
	}
}

func TestUpload_NotFoundSession(t *testing.T) {
	svc, _ := setupUpload(t)
	ctx := context.Background()
	if _, err := svc.Chunk(ctx, "bogus-id", 0, bytes.NewReader([]byte("x"))); err != ErrUploadNotFound {
		t.Errorf("chunk on unknown session should be ErrUploadNotFound, got %v", err)
	}
	if _, err := svc.Complete(ctx, "bogus-id", false); err != ErrUploadNotFound {
		t.Errorf("complete on unknown session should be ErrUploadNotFound, got %v", err)
	}
}

func TestUpload_AbortClears(t *testing.T) {
	svc, _ := setupUpload(t)
	ctx := context.Background()
	data := bytes.Repeat([]byte("x"), 300<<10)
	init, _ := svc.Init(ctx, "u", "/", "a.bin", "fp-a", int64(len(data)), 256<<10)
	svc.Chunk(ctx, init.UploadID, 0, bytes.NewReader(data[:init.ChunkSize]))

	svc.Abort(init.UploadID)
	// abort 后会话不存在。
	if _, err := svc.Chunk(ctx, init.UploadID, 1, bytes.NewReader(data[init.ChunkSize:])); err != ErrUploadNotFound {
		t.Errorf("after abort session should be gone, got %v", err)
	}
}

func TestUpload_SweepExpired(t *testing.T) {
	svc, _ := setupUpload(t)
	ctx := context.Background()
	data := bytes.Repeat([]byte("x"), 300<<10)
	init, _ := svc.Init(ctx, "u", "/", "s.bin", "fp-s", int64(len(data)), 256<<10)

	// 手动把会话的 lastSeen 调老。
	svc.mu.Lock()
	sess := svc.sessions[init.UploadID]
	svc.mu.Unlock()
	sess.mu.Lock()
	sess.lastSeen = time.Now().Add(-48 * time.Hour)
	sess.mu.Unlock()

	n := svc.Sweep(24 * time.Hour)
	if n != 1 {
		t.Errorf("expected 1 stale session swept, got %d", n)
	}
	if _, err := svc.Chunk(ctx, init.UploadID, 0, bytes.NewReader(data[:init.ChunkSize])); err != ErrUploadNotFound {
		t.Errorf("swept session should be gone, got %v", err)
	}
}

func TestUpload_ConcurrentSameTarget(t *testing.T) {
	svc, root := setupUpload(t)
	ctx := context.Background()

	// 两个上传会话指向同一目标，并发 complete，仅其一应成功，另一 ErrExists。
	mk := func(fp string) string {
		init, err := svc.Init(ctx, "u", "/", "race.bin", fp, 5, 256<<10)
		if err != nil {
			t.Fatalf("init: %v", err)
		}
		svc.Chunk(ctx, init.UploadID, 0, bytes.NewReader([]byte("hello")))
		return init.UploadID
	}
	id1 := mk("fp-1")
	id2 := mk("fp-2")

	errs := make(chan error, 2)
	go func() { _, e := svc.Complete(ctx, id1, false); errs <- e }()
	go func() { _, e := svc.Complete(ctx, id2, false); errs <- e }()
	e1, e2 := <-errs, <-errs

	okCount := 0
	existsCount := 0
	for _, e := range []error{e1, e2} {
		switch e {
		case nil:
			okCount++
		case storage.ErrExists:
			existsCount++
		default:
			t.Errorf("unexpected error: %v", e)
		}
	}
	if okCount != 1 || existsCount != 1 {
		t.Errorf("expected exactly one success and one ErrExists, got ok=%d exists=%d", okCount, existsCount)
	}
	if _, err := os.Stat(filepath.Join(root, "race.bin")); err != nil {
		t.Error("target should exist after race")
	}
}
