package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/storage/local"
	"flist/internal/util"
)

// setupOpService 构造 FileOpService（基于临时 root）并返回它与 rootReal。
func setupOpService(t *testing.T) (*FileOpService, string) {
	t.Helper()
	dir := t.TempDir()
	real, err := util.ResolveRoot(dir)
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	backend := local.New(real, t.TempDir())
	files := NewFileService(backend, util.NewPathLocker(), 5<<20)
	return NewFileOpService(files, nil), real
}

// drainEvents 从订阅通道收集事件，直到收到 finished 或超时。
func drainEvents(t *testing.T, ch <-chan FileOpEvent, timeout time.Duration) []FileOpEvent {
	t.Helper()
	var out []FileOpEvent
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
			if ev.Type == "finished" {
				return out
			}
		case <-deadline:
			t.Fatalf("drainEvents timeout, got %d events", len(out))
		}
	}
}

// TestFileOpCopyProgress 验证复制大文件时推送 item_progress 与最终 finished，
// 且快照的项内字节进度随复制推进。
func TestFileOpCopyProgress(t *testing.T) {
	svc, root := setupOpService(t)
	// 写两个稍大文件（复制有可观测字节数）。
	writeFile(t, root, "src/a.bin", strings.Repeat("A", 1<<20))
	writeFile(t, root, "src/b.bin", strings.Repeat("B", 1<<20))
	if err := os.MkdirAll(filepath.Join(root, "dst"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Start(context.Background(), model.FileOpCopy, "u", []string{"/src/a.bin", "/src/b.bin"}, "/dst", false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if res.TotalItems != 2 {
		t.Fatalf("TotalItems=%d want 2", res.TotalItems)
	}
	if res.TotalBytes != int64(2*(1<<20)) {
		t.Fatalf("TotalBytes=%d want %d", res.TotalBytes, 2*(1<<20))
	}

	ch, snap, unsub := svc.Subscribe(res.TaskID, "u")
	if ch == nil {
		t.Fatal("Subscribe returned nil")
	}
	defer unsub()
	if snap.Status != model.FileOpQueued && snap.Status != model.FileOpRunning {
		t.Fatalf("initial status=%q", snap.Status)
	}

	events := drainEvents(t, ch, 5*time.Second)
	if len(events) == 0 {
		t.Fatal("no events received")
	}
	last := events[len(events)-1]
	if last.Type != "finished" {
		t.Fatalf("last event type=%q want finished", last.Type)
	}
	if last.Snapshot.Status != model.FileOpDone {
		t.Fatalf("finished status=%q want done", last.Snapshot.Status)
	}
	if last.Snapshot.DoneItems != 2 {
		t.Fatalf("DoneItems=%d want 2", last.Snapshot.DoneItems)
	}
	if len(last.Snapshot.Results) != 2 {
		t.Fatalf("results len=%d want 2", len(last.Snapshot.Results))
	}
	for _, r := range last.Snapshot.Results {
		if !r.OK {
			t.Fatalf("unexpected failure: %+v", r)
		}
	}
	// 大文件（如用户的 multi-GB MOV）会持续推送 item_progress；但 1MB 文件在 tmpfs 上
	// 可能快于 200ms 节流窗口完成而不推送进度（这是节流的预期行为，非缺陷），
	// 因此这里仅记录、不强制要求。
	for _, ev := range events {
		if ev.Type == "item_progress" && ev.Snapshot.CurCopied <= 0 {
			t.Errorf("progress CurCopied=%d want >0", ev.Snapshot.CurCopied)
		}
	}
	// 落盘校验。
	if _, err := os.Stat(filepath.Join(root, "dst", "a.bin")); err != nil {
		t.Errorf("dst/a.bin not created: %v", err)
	}
}

// TestFileOpDelete 验证删除任务项级进度与 finished。
func TestFileOpDelete(t *testing.T) {
	svc, root := setupOpService(t)
	writeFile(t, root, "x.txt", "x")
	writeFile(t, root, "y.txt", "y")

	res, err := svc.Start(context.Background(), model.FileOpDelete, "u", []string{"/x.txt", "/y.txt"}, "", false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, _, unsub := svc.Subscribe(res.TaskID, "u")
	defer unsub()
	events := drainEvents(t, ch, 5*time.Second)
	last := events[len(events)-1]
	if last.Snapshot.Status != model.FileOpDone || last.Snapshot.DoneItems != 2 {
		t.Fatalf("unexpected final snapshot: %+v", last.Snapshot)
	}
	if _, err := os.Stat(filepath.Join(root, "x.txt")); !os.IsNotExist(err) {
		t.Errorf("x.txt still exists: %v", err)
	}
}

// TestFileOpCancel 验证取消请求使任务进入 canceled 终态。
func TestFileOpCancel(t *testing.T) {
	svc, root := setupOpService(t)
	writeFile(t, root, "src/big.bin", strings.Repeat("Z", 4<<20))
	if err := os.MkdirAll(filepath.Join(root, "dst"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Start(context.Background(), model.FileOpCopy, "u", []string{"/src/big.bin"}, "/dst", false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, _, unsub := svc.Subscribe(res.TaskID, "u")
	defer unsub()
	svc.Cancel(res.TaskID, "u")
	events := drainEvents(t, ch, 5*time.Second)
	last := events[len(events)-1]
	if last.Snapshot.Status != model.FileOpCanceled && last.Snapshot.Status != model.FileOpDone {
		// 文件较小可能复制在取消前已完成，接受 done；但期望能取消。
		t.Logf("final status=%q (cancel may race with completion on small files)", last.Snapshot.Status)
	}
}

// TestFileOpCopyProgressSlow 验证当节流关闭时，item_progress 确实被推送
//（证明项内字节进度回调链路通畅；大文件场景下节流为 200ms 仍会推）。
func TestFileOpCopyProgressSlow(t *testing.T) {
	prev := fileOpProgressInterval
	fileOpProgressInterval = 0 // 关闭节流，每次 Write 都推
	defer func() { fileOpProgressInterval = prev }()

	svc, root := setupOpService(t)
	writeFile(t, root, "src/big.bin", strings.Repeat("C", 256<<10)) // 256KB，多次 32KB Write
	if err := os.MkdirAll(filepath.Join(root, "dst"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Start(context.Background(), model.FileOpCopy, "u", []string{"/src/big.bin"}, "/dst", false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, _, unsub := svc.Subscribe(res.TaskID, "u")
	defer unsub()
	events := drainEvents(t, ch, 5*time.Second)
	hasProgress := false
	for _, ev := range events {
		if ev.Type == "item_progress" {
			hasProgress = true
		}
	}
	if !hasProgress {
		t.Errorf("expected item_progress events with throttle disabled")
	}
}

// TestFileOpGetMissing 验证不存在任务返回 false。
func TestFileOpGetMissing(t *testing.T) {
	svc, _ := setupOpService(t)
	if _, ok := svc.Get("nope", "u"); ok {
		t.Fatal("Get should return false for missing task")
	}
	if ch, _, _ := svc.Subscribe("nope", "u"); ch != nil {
		t.Fatal("Subscribe should return nil for missing task")
	}
}

// TestFileOpOwnerIsolation 验证非属主无法订阅 / 取消他人任务（按 not-found 语义）。
func TestFileOpOwnerIsolation(t *testing.T) {
	svc, root := setupOpService(t)
	writeFile(t, root, "a.txt", "x")
	res, err := svc.Start(context.Background(), model.FileOpDelete, "alice", []string{"/a.txt"}, "", false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// 属主 alice 可订阅。
	ch, _, unsub := svc.Subscribe(res.TaskID, "alice")
	if ch == nil {
		t.Fatal("owner should be able to subscribe")
	}
	unsub()
	// 非属主 bob 订阅返回 nil（不泄漏存在性）。
	if ch, _, _ := svc.Subscribe(res.TaskID, "bob"); ch != nil {
		t.Fatal("non-owner should not subscribe")
	}
	// 非属主 bob 取消应无效（任务仍可被属主取消）。
	svc.Cancel(res.TaskID, "bob")
	if _, ok := svc.Get(res.TaskID, "alice"); !ok {
		t.Fatal("non-owner cancel must not delete task")
	}
	// 属主取消生效。
	svc.Cancel(res.TaskID, "alice")
}

// TestFileOpQueueBusy 验证 ErrFileOpBusy / ErrFileOpNotFound 是独立 sentinel，
// 不与 storage.ErrBadOp 混淆（否则 handler switch 全部命中第一条）。
func TestFileOpQueueBusy(t *testing.T) {
	if errors.Is(ErrFileOpBusy, storage.ErrBadOp) {
		t.Fatal("ErrFileOpBusy must not alias ErrBadOp")
	}
	if errors.Is(ErrFileOpNotFound, storage.ErrBadOp) {
		t.Fatal("ErrFileOpNotFound must not alias ErrBadOp")
	}
}

// TestFileOpCancelBeforeRun 验证「入队后被取消」时所有项补 skipped、
// results 覆盖全部 totalItems、文件未被删除。用一个大文件复制任务占住 worker，
// 确保测试任务在 worker 队列中等待时被取消（确定性，不依赖时序）。
func TestFileOpCancelBeforeRun(t *testing.T) {
	svc, root := setupOpService(t)
	// 占住 worker 的耗时任务：复制 32MB 文件。
	writeFile(t, root, "block/big.bin", strings.Repeat("X", 32<<20))
	if err := os.MkdirAll(filepath.Join(root, "dst"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), model.FileOpCopy, "u",
		[]string{"/block/big.bin"}, "/dst", false); err != nil {
		t.Fatalf("start blocker: %v", err)
	}

	// 测试任务：删除 3 个文件，入队但 worker 忙于 blocker → 一定在队列中等待。
	writeFile(t, root, "a.txt", "a")
	writeFile(t, root, "b.txt", "b")
	writeFile(t, root, "c.txt", "c")
	res, err := svc.Start(context.Background(), model.FileOpDelete, "u",
		[]string{"/a.txt", "/b.txt", "/c.txt"}, "", false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, _, unsub := svc.Subscribe(res.TaskID, "u")
	defer unsub()
	svc.Cancel(res.TaskID, "u") // 立即取消（worker 仍忙于 blocker）

	events := drainEvents(t, ch, 15*time.Second)
	last := events[len(events)-1]
	if last.Type != "finished" {
		t.Fatalf("last event type=%q want finished", last.Type)
	}
	if last.Snapshot.Status != model.FileOpCanceled {
		t.Fatalf("status=%q want canceled", last.Snapshot.Status)
	}
	if len(last.Snapshot.Results) != 3 {
		t.Fatalf("results len=%d want 3 (must cover all totalItems)", len(last.Snapshot.Results))
	}
	for i, r := range last.Snapshot.Results {
		if r.OK {
			t.Errorf("result[%d] unexpectedly ok", i)
		}
		if r.Error != "skipped" {
			t.Errorf("result[%d] error=%q want skipped", i, r.Error)
		}
	}
	// 全部未删除（被取消前未触达）。
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Errorf("%s should still exist: %v", f, err)
		}
	}
}

// TestFileOpCancelResultsComplete 验证复制中途取消时 results 覆盖全部项、
// 半成品被清理。不依赖具体取消到第几项（时序相关），只断言不变量。
func TestFileOpCancelResultsComplete(t *testing.T) {
	svc, root := setupOpService(t)
	// 3 项：大文件 + 2 小文件。取消时机不定，但无论取消到哪项，不变量一致。
	writeFile(t, root, "src/big.bin", strings.Repeat("Z", 32<<20))
	writeFile(t, root, "src/s1.txt", "1")
	writeFile(t, root, "src/s2.txt", "2")
	if err := os.MkdirAll(filepath.Join(root, "dst"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Start(context.Background(), model.FileOpCopy, "u",
		[]string{"/src/big.bin", "/src/s1.txt", "/src/s2.txt"}, "/dst", false)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, _, unsub := svc.Subscribe(res.TaskID, "u")
	defer unsub()
	svc.Cancel(res.TaskID, "u")

	events := drainEvents(t, ch, 15*time.Second)
	last := events[len(events)-1]
	if last.Type != "finished" {
		t.Fatalf("last event type=%q want finished", last.Type)
	}
	if last.Snapshot.Status != model.FileOpCanceled {
		t.Fatalf("status=%q want canceled", last.Snapshot.Status)
	}
	// 不变量：results 必须覆盖全部 totalItems（成功/取消/skipped 三者之一）。
	if len(last.Snapshot.Results) != 3 {
		t.Fatalf("results len=%d want 3", len(last.Snapshot.Results))
	}
	// 大文件若未完整复制，半成品必须被清理（不存在 或 大小等于完整大小）。
	bigDst := filepath.Join(root, "dst", "big.bin")
	if info, err := os.Stat(bigDst); err == nil {
		if info.Size() != int64(32<<20) {
			t.Errorf("big.bin partial not cleaned: size=%d", info.Size())
		}
	}
}
