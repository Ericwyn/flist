// Package service 的异步文件操作部分。
//
// FileOpService 把 copy/move/delete 改造成后台任务：请求立即返回 task_id，由单一
// worker 串行执行（NAS 机械盘场景避免磁头抖动），通过 SSE 推送项内字节进度。
// 业务规则（落点判定 / 自动避让 / 空间预检）复用 FileService 的导出方法，保证与
// 同步路径语义一致。任务状态存内存、随进程生命周期；已完成任务保留 10 分钟供
// 客户端断线重连取回最终结果，超期由 Sweep 清理。
package service

import (
	"context"
	"errors"
	"log/slog"
	"path"
	"sync"
	"time"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/util"
)

// 文件操作任务错误，handler 据此映射对外错误码。
// 必须为独立 sentinel，不可复用 storage.ErrBadOp，否则 errors.Is 无法区分
// not_found / busy / bad_request，handler 的 switch 会全部命中第一条。
var (
	ErrFileOpNotFound = errors.New("fileop: task not found")
	ErrFileOpBusy     = errors.New("fileop: queue busy")
)

const (
	// fileOpQueueSize 限制排队任务数，超出则拒绝（防止堆积无法完成的大批量任务）。
	fileOpQueueSize = 32
	// fileOpFinishedTTL 已完成任务在内存保留的时间，供客户端断线重连取最终结果。
	fileOpFinishedTTL = 10 * time.Minute
	// fileOpSpeedEMA 速率 EMA 平滑系数。
	fileOpSpeedEMA = 0.5
	// fileOpEstimateTimeout Start 阶段 TreeSize 估算的独立短 deadline：大目录递归
	// stat 求和不应拖慢 202 响应；超时则 totalBytes 置 0（降级，不阻断任务）。
	fileOpEstimateTimeout = 5 * time.Second
)

// fileOpProgressInterval 进度事件最小推送间隔，避免高频回调打满 SSE。
//（var 而非 const，便于测试调小以验证慢复制路径）。
var fileOpProgressInterval = 200 * time.Millisecond

// FileOpEvent 是推送给 SSE 订阅者的事件。
type FileOpEvent struct {
	Type     string          `json:"type"` // snapshot | item_start | item_progress | item_done | finished
	Snapshot model.FileOpSnapshot `json:"snapshot"`
	Index    int             `json:"index,omitempty"`
	Name     string          `json:"name,omitempty"`
	Size     int64           `json:"size,omitempty"`
	Copied   int64           `json:"copied,omitempty"`
	OK       bool            `json:"ok,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// fileOpTask 是一次异步文件操作的内存状态。
type fileOpTask struct {
	id        string
	op        string
	userScope string
	srcs      []string
	dst       string
	autoRename bool
	startedAt  time.Time
	finishedAt time.Time // 终态写入时间，Sweep 据此判定 TTL（不可用 startedAt，否则长任务一完成即被清）

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	snapshot model.FileOpSnapshot
	results  []model.OpResult
	finished bool

	// 进度节流与速率计算（仅 worker 写、订阅者读 snapshot，故由 mu 保护）。
	lastEmit   time.Time
	lastCopied int64
	lastTs     time.Time

	subs map[chan FileOpEvent]struct{}
}

// FileOpService 提供异步 copy/move/delete 任务编排，单一 worker 串行执行。
type FileOpService struct {
	files  *FileService
	logger *slog.Logger

	jobs chan *fileOpTask
	mu   sync.Mutex
	tasks map[string]*fileOpTask
}

// NewFileOpService 构造异步文件操作服务并启动后台 worker。
func NewFileOpService(files *FileService, logger *slog.Logger) *FileOpService {
	if logger == nil {
		logger = slog.Default()
	}
	s := &FileOpService{
		files:  files,
		logger: logger,
		jobs:   make(chan *fileOpTask, fileOpQueueSize),
		tasks:  make(map[string]*fileOpTask),
	}
	go s.worker()
	return s
}

// Start 创建并入队一个任务，返回任务句柄。队列满时返回 ErrFileOpBusy。
// op ∈ {copy,move,delete}。totalBytes 为 best-effort 估算（treeSize），失败置 0。
func (s *FileOpService) Start(ctx context.Context, op, userScope string, srcs []string, dst string, autoRename bool) (*model.FileOpStartResult, error) {
	if len(srcs) == 0 {
		return nil, storage.ErrBadOp
	}
	if op != model.FileOpDelete && dst == "" {
		return nil, storage.ErrBadOp
	}

	// 估算总量（best-effort：单项 stat 失败 / 目录过大 / 超时都不阻断任务）。
	// 用独立短 deadline 与请求 ctx 解耦，避免大目录递归 stat 拖慢 202 响应、
	// 或客户端提前断开导致估算中断。超时则 totalBytes 置 0（前端显示未知）。
	var totalBytes int64
	estCtx, estCancel := context.WithTimeout(context.Background(), fileOpEstimateTimeout)
	for _, src := range srcs {
		if n, err := s.files.TreeSize(estCtx, src); err == nil {
			totalBytes += int64(n)
		}
	}
	estCancel()

	id, err := util.GenerateToken()
	if err != nil {
		return nil, err
	}
	taskCtx, cancel := context.WithCancel(context.Background())
	t := &fileOpTask{
		id:        id,
		op:        op,
		userScope: userScope,
		srcs:      srcs,
		dst:       dst,
		autoRename: autoRename,
		startedAt: time.Now(),
		ctx:       taskCtx,
		cancel:    cancel,
		subs:      make(map[chan FileOpEvent]struct{}),
		results:   make([]model.OpResult, 0, len(srcs)),
	}
	t.snapshot = model.FileOpSnapshot{
		Op:         op,
		Status:     model.FileOpQueued,
		TotalItems: len(srcs),
		TotalBytes: totalBytes,
		CurIndex:   -1,
		StartedAt:  t.startedAt,
	}

	s.mu.Lock()
	s.tasks[id] = t
	s.mu.Unlock()

	select {
	case s.jobs <- t:
	default:
		s.mu.Lock()
		delete(s.tasks, id)
		s.mu.Unlock()
		cancel()
		return nil, ErrFileOpBusy
	}

	return &model.FileOpStartResult{
		TaskID:     id,
		Op:         op,
		TotalItems: len(srcs),
		TotalBytes: totalBytes,
	}, nil
}

// Cancel 请求取消一个任务（幂等：不存在 / 非本人 / 已结束均不报错，统一静默）。
// 不存在或非属主时不做任何动作，避免侧信道确认 taskID 存在性。
func (s *FileOpService) Cancel(taskID, userScope string) {
	s.mu.Lock()
	t, ok := s.tasks[taskID]
	s.mu.Unlock()
	if ok && t.userScope == userScope {
		t.cancel()
	}
}

// Subscribe 订阅任务的事件流。返回事件通道、当前快照与取消订阅函数。
// 任务不存在或非本人属主时返回 nil（按 not-found 语义，不泄漏存在性）。
func (s *FileOpService) Subscribe(taskID, userScope string) (<-chan FileOpEvent, model.FileOpSnapshot, func()) {
	s.mu.Lock()
	t, ok := s.tasks[taskID]
	s.mu.Unlock()
	if !ok || t.userScope != userScope {
		return nil, model.FileOpSnapshot{}, nil
	}
	ch := make(chan FileOpEvent, 64)
	t.mu.Lock()
	snap := t.snapshot
	finished := t.finished
	results := t.results
	if finished {
		// 已结束时 subs 已被 finishTask 置 nil，不能再注册，否则写 nil map 崩溃。
		t.mu.Unlock()
		// 发一条 finished 后关闭。results 在锁内拷贝进快照，避免锁外读竞态。
		go func() {
			snap.Results = results
			ev := FileOpEvent{Type: "finished", Snapshot: snap}
			select {
			case ch <- ev:
			default:
			}
			close(ch)
		}()
		return ch, snap, func() {}
	}
	t.subs[ch] = struct{}{}
	t.mu.Unlock()
	return ch, snap, func() {
		t.mu.Lock()
		delete(t.subs, ch)
		t.mu.Unlock()
	}
}

// Get 返回任务的当前快照（用于断线重连或非 SSE 查询）。
// 不存在或非本人属主返回 false。
func (s *FileOpService) Get(taskID, userScope string) (model.FileOpSnapshot, bool) {
	s.mu.Lock()
	t, ok := s.tasks[taskID]
	s.mu.Unlock()
	if !ok || t.userScope != userScope {
		return model.FileOpSnapshot{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	snap := t.snapshot
	snap.Results = t.results
	return snap, true
}

// worker 单一消费者，串行执行任务（FIFO）。
func (s *FileOpService) worker() {
	for t := range s.jobs {
		s.runTask(t)
	}
}

// runTask 执行单个任务的完整流程：标 running → 逐项处理 → 标终态 → 推 finished。
func (s *FileOpService) runTask(t *fileOpTask) {
	// 若已取消（入队后被 cancel），所有项均未触达——补 skipped 使 results 覆盖全部项。
	if t.ctx.Err() != nil {
		s.finishRemainingCanceled(t, 0)
		s.finishTask(t, model.FileOpCanceled, t.results)
		return
	}

	s.setRunning(t)
	s.emit(t, FileOpEvent{Type: "snapshot"})

	switch t.op {
	case model.FileOpCopy:
		s.execTransfer(t, true)
	case model.FileOpMove:
		s.execTransfer(t, false)
	case model.FileOpDelete:
		s.execDelete(t)
	default:
		s.finishTask(t, model.FileOpFailed, []model.OpResult{{Src: "", OK: false, Error: "bad_request"}})
	}
}

// execTransfer 执行 copy（isCopy=true）或 move 的逐项处理。
func (s *FileOpService) execTransfer(t *fileOpTask, isCopy bool) {
	ctx := t.ctx
	cleanedDst := util.CleanAPIPath(t.dst)
	dstExists, dstIsDir := s.files.StatDir(ctx, cleanedDst)
	single := len(t.srcs) == 1

	pc, _ := s.files.backend.(storage.ProgressCopier)

	var doneBytes int64
	for i, src := range t.srcs {
		if ctx.Err() != nil {
			// 入队后被取消、尚未开始该项：已处理项留在 results，剩余补 skipped。
			s.finishRemainingCanceled(t, i)
			s.finishTask(t, model.FileOpCanceled, t.results)
			return
		}
		srcClean := util.CleanAPIPath(src)
		target, fail := s.files.TransferTarget(ctx, srcClean, cleanedDst, dstExists, dstIsDir, single, t.autoRename)
		if fail != nil {
			t.results = append(t.results, *fail)
			s.emitItemDone(t, i, fail.Src, false, fail.Error)
			continue
		}

		// 复制 / 跨盘 move 都会搬运字节，预检目标盘空间；同盘 rename 不消耗
		// 空间，CheckSpace 内部走 Usager 取目标盘可用空间，同盘必然充足，不会误拦。
		// 驱动未实现 Usager 时 CheckSpace 直接返回 nil，无副作用。
		if err := s.files.CheckSpace(ctx, srcClean, path.Dir(target)); err != nil {
			t.results = append(t.results, opFail(srcClean, err))
			s.emitItemDone(t, i, srcClean, false, errCodeName(err))
			continue
		}

		// 项信息：取 stat 得到 name/size。
		name, size := s.itemInfo(ctx, srcClean, target)
		s.startItem(t, i, name, size)

		var execErr error
		if pc != nil {
			cb := func(copied int64) { s.reportProgress(t, i, copied, size) }
			if isCopy {
				execErr = pc.CopyWithProgress(ctx, srcClean, target, cb)
			} else {
				execErr = pc.MoveWithProgress(ctx, srcClean, target, cb)
			}
		} else {
			// 驱动不支持 ProgressCopier：回退到普通 Copy/Move（无项内字节进度）。
			if isCopy {
				execErr = s.files.backend.Copy(ctx, srcClean, target)
			} else {
				execErr = s.files.backend.Move(ctx, srcClean, target)
			}
		}

		// CopyWithProgress / Copy 返回后，先判 execErr 再判 ctx.Err，避免竞态：
		// 若文件恰好写完（execErr==nil）但 ctx 同时被取消，应如实标成功而非 canceled。
		if execErr != nil {
			if ctx.Err() != nil {
				// 取消中断复制：copyFile 已清理半成品 dst。
				t.results = append(t.results, model.OpResult{Src: srcClean, OK: false, Error: "canceled"})
				s.emitItemDone(t, i, srcClean, false, "canceled")
				s.finishRemainingCanceled(t, i+1)
				s.finishTask(t, model.FileOpCanceled, t.results)
				return
			}
			t.results = append(t.results, opFail(srcClean, execErr))
			s.emitItemDone(t, i, srcClean, false, errCodeName(execErr))
			continue
		}
		// execErr == nil：该项成功（即使 ctx 恰好取消，文件已完整落盘，如实标成功）。
		t.results = append(t.results, model.OpResult{Src: srcClean, OK: true})
		if size > 0 {
			doneBytes += size
		}
		s.muSnapshot(t, func(snap *model.FileOpSnapshot) {
			snap.DoneItems = i + 1
			snap.DoneBytes = doneBytes
		})
		s.emitItemDone(t, i, srcClean, true, "")
		if ctx.Err() != nil {
			// 该项刚好完成、用户同时取消：该项成功，剩余项 skipped。
			s.finishRemainingCanceled(t, i+1)
			s.finishTask(t, model.FileOpCanceled, t.results)
			return
		}
	}
	s.finishTask(t, model.FileOpDone, t.results)
}

// execDelete 逐项删除。删除无项内字节进度，仅项级 done。
func (s *FileOpService) execDelete(t *fileOpTask) {
	ctx := t.ctx
	for i, p := range t.srcs {
		if ctx.Err() != nil {
			s.finishRemainingCanceled(t, i)
			s.finishTask(t, model.FileOpCanceled, t.results)
			return
		}
		cleaned := util.CleanAPIPath(p)
		name, size := s.itemInfo(ctx, cleaned, "")
		s.startItem(t, i, name, size)
		err := s.files.backend.Remove(ctx, cleaned)
		// 先判 err 再判 ctx.Err，避免删除恰好完成却被标 canceled 的竞态。
		if err != nil {
			if ctx.Err() != nil {
				t.results = append(t.results, model.OpResult{Src: cleaned, OK: false, Error: "canceled"})
				s.emitItemDone(t, i, cleaned, false, "canceled")
				s.finishRemainingCanceled(t, i+1)
				s.finishTask(t, model.FileOpCanceled, t.results)
				return
			}
			t.results = append(t.results, opFail(cleaned, err))
			s.emitItemDone(t, i, cleaned, false, errCodeName(err))
			continue
		}
		t.results = append(t.results, model.OpResult{Src: cleaned, OK: true})
		s.muSnapshot(t, func(snap *model.FileOpSnapshot) { snap.DoneItems = i + 1 })
		s.emitItemDone(t, i, cleaned, true, "")
		if ctx.Err() != nil {
			s.finishRemainingCanceled(t, i+1)
			s.finishTask(t, model.FileOpCanceled, t.results)
			return
		}
	}
	s.finishTask(t, model.FileOpDone, t.results)
}

// finishRemainingCanceled 为 fromIndex..len(srcs)-1 的未开始项补 skipped 结果，
// 使 results 覆盖全部 totalItems——详情面板据此展示完整项列表（含未触达的项）。
// 不发 item_done 事件：这些项从未 startItem，事件无意义；最终 finished 携带 results[] 即可。
func (s *FileOpService) finishRemainingCanceled(t *fileOpTask, fromIndex int) {
	for i := fromIndex; i < len(t.srcs); i++ {
		t.results = append(t.results, model.OpResult{
			Src:   util.CleanAPIPath(t.srcs[i]),
			OK:    false,
			Error: "skipped",
		})
	}
}

// itemInfo 取 src 的 basename 与 size（用于 UI 展示）。失败时 size=0。
func (s *FileOpService) itemInfo(ctx context.Context, srcClean, _ string) (string, int64) {
	name := path.Base(srcClean)
	if info, err := s.files.backend.Stat(ctx, srcClean); err == nil {
		return info.Name, info.Size
	}
	return name, 0
}

// startItem 标记开始处理第 i 项并推送 item_start。
func (s *FileOpService) startItem(t *fileOpTask, i int, name string, size int64) {
	t.mu.Lock()
	t.snapshot.CurIndex = i
	t.snapshot.CurName = name
	t.snapshot.CurSize = size
	t.snapshot.CurCopied = 0
	t.snapshot.Speed = 0
	t.lastCopied = 0
	t.lastTs = time.Time{}
	t.lastEmit = time.Now()
	t.mu.Unlock()
	s.emit(t, FileOpEvent{Type: "item_start", Index: i, Name: name, Size: size})
}

// reportProgress 处理项内字节进度：节流推送 + EMA 速率。每个 src 项内 copied 从 0 起。
func (s *FileOpService) reportProgress(t *fileOpTask, index int, copied, size int64) {
	t.mu.Lock()
	t.snapshot.CurCopied = copied
	now := time.Now()
	// 速率 EMA。
	if !t.lastTs.IsZero() {
		dt := now.Sub(t.lastTs).Seconds()
		if dt > 0 && copied > t.lastCopied {
			inst := float64(copied-t.lastCopied) / dt
			prev := float64(t.snapshot.Speed)
			if prev == 0 {
				t.snapshot.Speed = int64(inst)
			} else {
				t.snapshot.Speed = int64(prev*fileOpSpeedEMA + inst*(1-fileOpSpeedEMA))
			}
		}
	}
	// 节流：距上次推送不足间隔则只更新快照、不推送（速率基线不更新避免抖动）。
	if now.Sub(t.lastEmit) < fileOpProgressInterval {
		t.mu.Unlock()
		return
	}
	t.lastEmit = now
	t.lastCopied = copied
	t.lastTs = now
	t.mu.Unlock()
	s.emit(t, FileOpEvent{Type: "item_progress", Index: index, Copied: copied})
}

// emitItemDone 推送 item_done 并复位当前项字段。
func (s *FileOpService) emitItemDone(t *fileOpTask, i int, src string, ok bool, errName string) {
	t.mu.Lock()
	t.snapshot.CurCopied = t.snapshot.CurSize // 项完成时进度拉满
	t.snapshot.Speed = 0
	t.mu.Unlock()
	s.emit(t, FileOpEvent{Type: "item_done", Index: i, OK: ok, Error: errName})
}

// setRunning 标记任务运行中。
func (s *FileOpService) setRunning(t *fileOpTask) {
	t.mu.Lock()
	t.snapshot.Status = model.FileOpRunning
	t.mu.Unlock()
}

// finishTask 标记终态、推送 finished 并关闭订阅 channel（随后任务保留至 TTL 由 Sweep 清理）。
func (s *FileOpService) finishTask(t *fileOpTask, status string, results []model.OpResult) {
	t.mu.Lock()
	t.snapshot.Status = status
	t.snapshot.CurIndex = -1
	t.snapshot.CurName = ""
	t.snapshot.CurCopied = 0
	t.snapshot.Speed = 0
	t.snapshot.Results = results
	t.snapshot.Error = ""
	if status == model.FileOpFailed && len(results) > 0 {
		t.snapshot.Error = results[0].Error
	}
	t.finished = true
	t.finishedAt = time.Now()
	snap := t.snapshot
	subs := t.subs
	t.subs = nil
	t.mu.Unlock()
	ev := FileOpEvent{Type: "finished", Snapshot: snap}
	for ch := range subs {
		// finished 是终态事件，不可像进度事件那样随意丢弃，否则慢客户端断线重连
		// 期间永远拿不到终态、前端卡在 running。缓冲满时丢弃一条历史进度事件
		// 腾位，保证 finished 入队（方案 B）。
		select {
		case ch <- ev:
		default:
			select {
			case <-ch: // 丢弃最旧的一条进度事件
			default:
			}
			select {
			case ch <- ev:
			default:
			}
		}
		close(ch)
	}
	s.logger.Info("fileop finished",
		slog.String("task_id", t.id),
		slog.String("op", t.op),
		slog.String("status", status),
		slog.Int("items", len(results)),
		slog.String("user", t.userScope),
	)
}

// emit 向所有订阅者非阻塞推送一个事件（订阅者缓冲满则丢弃，进度可丢）。
// 快照始终在此处锁内填充，保证调用方无需各自取快照、避免锁外读取竞态。
func (s *FileOpService) emit(t *fileOpTask, ev FileOpEvent) {
	t.mu.Lock()
	ev.Snapshot = t.snapshot
	subs := t.subs
	t.mu.Unlock()
	for ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// muSnapshot 在锁内修改快照的便捷封装。
func (s *FileOpService) muSnapshot(t *fileOpTask, fn func(snap *model.FileOpSnapshot)) {
	t.mu.Lock()
	fn(&t.snapshot)
	t.mu.Unlock()
}

// Sweep 清理已完成且超过 TTL 的任务（断线重连窗口外）。返回清理数量。
// 仅清理 finished 任务，且按完成时间（finishedAt）而非发起时间判定——
// 否则长任务一完成即被清，断线重连窗口名存实亡。未完成（queued/running）绝不清理。
func (s *FileOpService) Sweep() int {
	cutoff := time.Now().Add(-fileOpFinishedTTL)
	var stale []string
	s.mu.Lock()
	for id, t := range s.tasks {
		t.mu.Lock()
		old := t.finished && !t.finishedAt.IsZero() && t.finishedAt.Before(cutoff)
		t.mu.Unlock()
		if old {
			stale = append(stale, id)
		}
	}
	for _, id := range stale {
		delete(s.tasks, id)
	}
	s.mu.Unlock()
	return len(stale)
}
