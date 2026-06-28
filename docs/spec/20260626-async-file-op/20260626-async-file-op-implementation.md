# 异步文件操作任务实现细节

本文档对应 `20260626-async-file-op-overview.md`，记录已落地的具体代码改造点，供 review。

## 改动范围总览

| 模块 | 文件 | 改动类型 |
| --- | --- | --- |
| Util | `internal/util/fsop.go` | 改造：进度变体 + ctx 可取消 |
| Storage | `internal/storage/backend.go` | 新增：`ProgressCopier` 可选接口 |
| Local 驱动 | `internal/storage/local/local.go` | 改造：实现 `ProgressCopier`，Copy/Move 共享校验 |
| Service | `internal/service/file.go` | 新增导出方法 |
| Service | `internal/service/fileop.go` | 新建：`FileOpService` |
| Model | `internal/model/fileop.go` | 新建：任务 DTO |
| Handler | `internal/handler/fileop.go` | 新建：异步接口 + SSE handler |
| Handler | `internal/handler/errors.go` | 新增错误码 |
| Routes | `internal/server/routes.go` | 注册路由 + `Deps.FileOps` |
| Main | `cmd/flist/main.go` | 装配 + sweep goroutine |
| 前端类型 | `frontend/src/types.ts` | 新增 `FileOpTask` / `FileOpItem` 等 |
| 前端 API | `frontend/src/lib/api.ts` | 新增 `api.fs.op.*` |
| 前端路径 | `frontend/src/lib/path.ts` | 新增 `baseName` 工具函数 |
| 前端 Store | `frontend/src/fileOpStore.ts` | 新建：SSE 订阅 + 任务状态 + localStorage 恢复 |
| 前端 Store | `frontend/src/fsStore.ts` | 改造：paste/remove 委托 |
| 前端组件 | `frontend/src/components/TransferPanel.tsx` | 新增 `FileOpRow` + `FileOpDetailModal` |
| 测试 | `internal/service/fileop_test.go` | 新建：集成测试 |

---

## 1. Util 进度变体（`internal/util/fsop.go`）

### 改动

`CopyPath` / `MovePath` 原签名无 `context`、无进度回调。改造为：

```go
// 保留原签名（透明兼容旧调用方，内部用 context.Background()）
func CopyPath(src, dst string) error {
    return copyPath(context.Background(), src, dst, 0, nil)
}

// 新增：进度 + ctx 可取消
func CopyPathWithProgress(ctx context.Context, src, dst string, onProgress func(copied int64)) error {
    return copyPath(ctx, src, dst, 0, onProgress)
}
```

`copyPath` / `copyFile` 增加 `ctx context.Context` 与 `onProgress func(int64)` 参数，并在递归每个目录条目前检查 `ctx.Err()`。

### 进度注入：`progressWriter`

```go
type progressWriter struct {
    w   io.Writer
    ctx context.Context
    n   int64
    fn  func(int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
    if err := p.ctx.Err(); err != nil {
        return 0, err          // ctx 取消则中止 io.Copy
    }
    n, err := p.w.Write(b)
    p.n += int64(n)
    p.fn(p.n)                  // 回调当前文件累计已复制字节
    return n, err
}
```

`copyFile` 中 `io.Copy(&progressWriter{w: out, ctx: ctx, fn: onProgress}, in)`，复制失败 / 取消时 `out.Close()` + `os.Remove(dst)` 清理半成品（原逻辑保留）。

### Move 进度变体

```go
func MovePathWithProgress(ctx context.Context, src, dst string, onProgress func(int64)) error {
    return movePath(ctx, src, dst, onProgress)
}

func movePath(ctx, src, dst, onProgress) error {
    err := os.Rename(src, dst)
    if err == nil { return nil }          // 同盘 rename 瞬时，无进度
    if !isCrossDevice(err) { return err }
    // 跨盘回退：复制（带进度）+ 删源；失败 / 取消清理半成品 dst
    if cerr := copyPath(ctx, src, dst, 0, onProgress); cerr != nil {
        _ = removePath(dst, 0)
        return cerr
    }
    return removePath(src, 0)
}
```

### 关键点

- 每进入一个普通文件，`onProgress` 计数从 0 重新开始（项内字节进度，非全局累计）。
- `ctx` 取消由 `progressWriter.Write` 检测，`io.Copy` 收到错误后 `copyFile` 清理半成品。
- `RemovePath` 不改（删除无字节进度需求，项级粒度由 `FileOpService` 处理）。

---

## 2. Storage `ProgressCopier` 接口（`internal/storage/backend.go`）

```go
// ProgressFunc 由带进度的复制 / 移动回调上报：参数为当前 src 项内已处理字节数。
type ProgressFunc func(copied int64)

// ProgressCopier 可选接口：驱动支持带进度回调与取消的复制 / 移动。
type ProgressCopier interface {
    CopyWithProgress(ctx context.Context, src, dst string, fn ProgressFunc) error
    MoveWithProgress(ctx context.Context, src, dst string, fn ProgressFunc) error
}
```

未实现时 `FileOpService` 用类型断言探测，回退到普通 `backend.Copy/Move`（仅项级进度）。与 `Walker` / `Usager` / `Uploader` 同构的可选接口模式。

---

## 3. Local 驱动实现（`internal/storage/local/local.go`）

### Copy / Move 重构共享校验

原 `Copy` / `Move` 各自重复「校验 root / 名称 / SafeResolve / 落点不存在 / 自身子树」逻辑。重构为内部 `b.copy(ctx, src, dst, fn)` / `b.move(ctx, src, dst, fn)`，公共校验只写一份，`fn` 为 nil 走普通 `util.CopyPath/MovePath`，非 nil 走进度变体：

```go
func (b *Local) Copy(ctx, src, dst) error           { return b.copy(ctx, src, dst, nil) }
func (b *Local) CopyWithProgress(ctx, src, dst, fn) error { return b.copy(ctx, src, dst, fn) }

func (b *Local) copy(ctx, src, dst string, fn storage.ProgressFunc) error {
    // ... 校验 + SafeResolve + isSubpath ...
    var cerr error
    if fn == nil {
        cerr = util.CopyPath(srcLocal, dstLocal)
    } else {
        cerr = util.CopyPathWithProgress(ctx, srcLocal, dstLocal, func(copied int64) { fn(copied) })
    }
    return mapErr(cerr)
}
```

`Move` 同构。新增接口断言 `_ storage.ProgressCopier = (*Local)(nil)`。

---

## 4. FileService 导出方法（`internal/service/file.go`）

异步路径需复用同步路径的业务规则，导出 4 个原 unexported 方法（实现不变，仅加导出包装）：

| 导出方法 | 对应原方法 | 用途 |
| --- | --- | --- |
| `StatDir(ctx, dst) (exists, isDir)` | `statDir` | 探测 dst 是否已存在目录 |
| `TransferTarget(ctx, src, dst, dstExists, dstIsDir, single, autoRename) (string, *OpResult)` | `transferTarget` | 计算单项落点（移入目录 / 重命名 / 自动避让） |
| `CheckSpace(ctx, src, dstDir) error` | `checkSpace` | 复制前磁盘空间预检 |
| `TreeSize(ctx, src) (uint64, error)` | `treeSize` | 递归 stat 求和，用于 totalBytes 估算 |

原 unexported 方法保留（同步路径 `Copy`/`Move` 仍调用），导出版仅做 `util.CleanAPIPath` 包装后委托。**保证异步与同步语义完全一致**，不重复实现落点 / 避让 / 空间规则。

---

## 5. FileOpService（`internal/service/fileop.go`，新建）

### 结构

```go
type FileOpService struct {
    files  *FileService
    logger *slog.Logger
    jobs   chan *fileOpTask          // 缓冲 32，FIFO 队列
    mu     sync.Mutex
    tasks  map[string]*fileOpTask    // task_id -> 任务
}

type fileOpTask struct {
    id, op, userScope string
    srcs []string
    dst  string
    autoRename bool
    startedAt time.Time
    ctx    context.Context
    cancel context.CancelFunc

    mu       sync.Mutex
    snapshot model.FileOpSnapshot
    results  []model.OpResult
    finished bool

    // 进度节流 + 速率计算（mu 保护）
    lastEmit   time.Time
    lastCopied int64
    lastTs     time.Time

    subs map[chan FileOpEvent]struct{}
}
```

### 常量

```go
const (
    fileOpQueueSize       = 32
    fileOpFinishedTTL     = 10 * time.Minute
    fileOpSpeedEMA        = 0.5
    fileOpEstimateTimeout = 5 * time.Second   // Start 阶段 TreeSize 估算独立短 deadline
)
var fileOpProgressInterval = 200 * time.Millisecond  // var 便于测试调小
```

### 错误 sentinel（独立，不复用 ErrBadOp）

```go
var (
    ErrFileOpNotFound = errors.New("fileop: task not found")
    ErrFileOpBusy     = errors.New("fileop: queue busy")
)
```

**必须为独立 sentinel**：若复用 `storage.ErrBadOp`，`errors.Is` 无法区分 not_found / busy / bad_request，handler 的 switch 会全部命中第一条（404），队列满与 bad_request 都被误映射为 404。

### `Start` — 发起任务

1. 校验 `srcs` 非空、`op` 合法；
2. `TreeSize` best-effort 估算 `totalBytes`：**用独立 5s deadline**（`context.WithTimeout(context.Background(), fileOpEstimateTimeout)`），与请求 ctx 解耦，避免大目录递归 stat 拖慢 202 响应或客户端提前断开中断估算；超时则置 0 不阻断；
3. 生成 `task_id`（`util.GenerateToken`），创建 `ctx+cancel`，注册到 `tasks`；
4. 非阻塞发送到 `jobs`：队列满则回滚注册、返回 `ErrFileOpBusy`；
5. 返回 `FileOpStartResult{task_id, op, total_items, total_bytes}`。

### 属主隔离

`userScope`（取自登录用户）不仅用于日志，`Cancel` / `Subscribe` / `Get` 均校验属主：

```go
func (s *FileOpService) Cancel(taskID, userScope string)      // 非属主静默不动作
func (s *FileOpService) Subscribe(taskID, userScope string)   // 非属主返回 nil（not-found 语义，不泄漏存在性）
func (s *FileOpService) Get(taskID, userScope string)         // 非属主返回 false
```

与上传会话的 `userScope` 隔离策略一致，防止跨用户取消 / 窃听他人任务进度与路径名。

### Worker（单一 goroutine，FIFO 串行）

`NewFileOpService` 启动 `go s.worker()`，循环 `for t := range s.jobs { s.runTask(t) }`。

### `runTask` -> `execTransfer` / `execDelete`

`execTransfer(t, isCopy)`：

```go
cleanedDst := CleanAPIPath(t.dst)
dstExists, dstIsDir := s.files.StatDir(ctx, cleanedDst)
single := len(t.srcs) == 1
pc, _ := s.files.backend.(storage.ProgressCopier)   // 类型断言探测

for i, src := range t.srcs {
    if ctx.Err() != nil { finishRemainingCanceled(t, i); finishTask(canceled); return }
    srcClean := CleanAPIPath(src)
    target, fail := s.files.TransferTarget(ctx, srcClean, cleanedDst, dstExists, dstIsDir, single, t.autoRename)
    if fail != nil { 记失败; emitItemDone; continue }
    // copy 与 move 均预检空间：跨盘 move 同样搬运字节；同盘 rename 不消耗空间，
    // CheckSpace 走 Usager 取同盘可用空间必充足，不会误拦；驱动无 Usager 则返回 nil。
    s.files.CheckSpace(ctx, srcClean, path.Dir(target))
    name, size := s.itemInfo(ctx, srcClean, target)
    s.startItem(t, i, name, size)
    if pc != nil {
        cb := func(copied int64) { s.reportProgress(t, i, copied, size) }
        execErr = isCopy ? pc.CopyWithProgress(ctx, srcClean, target, cb)
                        : pc.MoveWithProgress(ctx, srcClean, target, cb)
    } else {
        execErr = isCopy ? backend.Copy(ctx, srcClean, target) : backend.Move(ctx, srcClean, target)
    }
    // 先判 execErr 再判 ctx.Err，避免竞态：文件恰好写完（execErr==nil）但 ctx 同时
    // 取消时，应如实标成功而非 canceled。
    if execErr != nil {
        if ctx.Err() != nil { 记 canceled; emitItemDone; finishRemainingCanceled(t, i+1); finishTask(canceled); return }
        记失败; emitItemDone; continue
    }
    记成功; doneBytes += size; 更新快照 DoneItems/DoneBytes; emitItemDone
    if ctx.Err() != nil { finishRemainingCanceled(t, i+1); finishTask(canceled); return }  // 该项刚好完成、用户同时取消
}
finishTask(done, results)
```

`execDelete` 同构（无字节进度，仅项级 `item_done`），同样先判 `err` 再判 `ctx.Err` + `finishRemainingCanceled`。

### 取消语义

- **已完成项保留**（与 Windows 资源管理器一致）：取消只停止后续工作，不回滚已完整复制 / 删除的项。进行中的半成品由 `copyFile` 的 `os.Remove(dst)` 清理。
- **`finishRemainingCanceled(t, fromIndex)`**：为 `fromIndex..len(srcs)-1` 的未触达项补 `{OK:false, Error:"skipped"}` 结果，使 `len(results) == totalItems`——详情面板据此展示完整项列表。
- `runTask` 入口若 `ctx.Err()`（入队后被取消）同样调 `finishRemainingCanceled(t, 0)` + `finishTask(canceled)`，而非传 nil results。

### 进度节流 + 速率（`reportProgress`）

```go
func (s *FileOpService) reportProgress(t, index, copied, size) {
    t.mu.Lock()
    t.snapshot.CurCopied = copied
    // EMA 速率（基于 lastCopied/lastTs 快照）
    if !t.lastTs.IsZero() {
        dt := now - t.lastTs
        if dt > 0 && copied > t.lastCopied {
            inst := (copied - t.lastCopied) / dt
            t.snapshot.Speed = EMA(prev, inst, 0.5)
        }
    }
    if now - t.lastEmit < 200ms { 解锁返回 }   // 节流：只更新快照不推送
    t.lastEmit = now; t.lastCopied = copied; t.lastTs = now
    t.mu.Unlock()
    s.emit(t, FileOpEvent{Type: "item_progress", Index: index, Copied: copied})
}
```

节流期间仍更新快照（`CurCopied` 始终最新），但不推送事件、不更新速率基线，避免高频小包永远算不到速率。

### `emit` — 事件扇出

```go
func (s *FileOpService) emit(t, ev) {
    t.mu.Lock()
    ev.Snapshot = t.snapshot            // 统一锁内填充快照，避免调用方锁外读取竞态
    subs := t.subs
    t.mu.Unlock()
    for ch := range subs {
        select { case ch <- ev: default: }   // 非阻塞，订阅者缓冲满则丢进度
    }
}
```

### `Subscribe` — SSE 订阅

```go
func (s *FileOpService) Subscribe(taskID, userScope) (<-chan FileOpEvent, snapshot, unsub) {
    // 属主校验：非本人返回 nil（not-found 语义）
    // 取任务，建 buffered(64) channel，加入 subs
    // 若 finished：异步发一条 finished 后关闭 channel（results 在锁内拷贝进快照，避免锁外读竞态）
    // 否则返回 channel + unsub（从 subs 摘除）
}
```

### `finishTask`

设终态 `status`、清空当前项字段、`snapshot.Results = results`、`finished=true`、`finishedAt=now`，向所有订阅者推 `finished` 后 `close(ch)`，记 `logger.Info("fileop finished", ...)`。

**finished 事件不可丢弃**（否则慢客户端断线重连期间永远拿不到终态、前端卡在 running）。进度事件可丢，但 finished 必须入队——缓冲满时丢弃一条历史进度事件腾位（方案 B 排空兜底）：

```go
for ch := range subs {
    select {
    case ch <- ev:
    default:
        select { case <-ch: default: }   // 丢弃最旧进度事件腾位
        select { case ch <- ev: default: }
    }
    close(ch)
}
```

### `Sweep`

每分钟由 main 调用：清理 `finished && finishedAt < now-10min` 的任务。**按完成时间而非发起时间判定**——否则长任务（跑 >10min）一完成即被清，断线重连窗口名存实亡。未完成（queued/running）绝不清理。

### `fileOpTask` 结构

```go
type fileOpTask struct {
    // ...
    startedAt  time.Time
    finishedAt time.Time  // 终态写入时间，Sweep 据此判定 TTL
    // ...
}
```

---

## 6. Model DTO（`internal/model/fileop.go`，新建）

```go
const (
    FileOpCopy = "copy"; FileOpMove = "move"; FileOpDelete = "delete"
    FileOpQueued = "queued"; FileOpRunning = "running"
    FileOpDone = "done"; FileOpCanceled = "canceled"; FileOpFailed = "error"
)

type FileOpStartResult struct {
    TaskID string `json:"task_id"`
    Op string `json:"op"`
    TotalItems int `json:"total_items"`
    TotalBytes int64 `json:"total_bytes"`
}

type FileOpSnapshot struct {
    Op, Status string
    TotalItems int; TotalBytes, DoneBytes int64
    DoneItems int
    CurIndex int; CurName string; CurSize, CurCopied int64
    Speed int64
    Results []OpResult `json:"results,omitempty"`
    Error string `json:"error,omitempty"`
    StartedAt time.Time
}
```

---

## 7. Handler（`internal/handler/fileop.go`，新建）

```go
type FileOpHandler struct {
    ops    *service.FileOpService
    logger *slog.Logger
}
```

- `Copy` / `Move` / `Delete`：`decodeJSON` 解析请求体（与旧同步接口同结构），调 `ops.Start(ctx, op, opScope(r), ...)`，成功 `WriteJSON(202, {code:0, message:"accepted", data: res})`。
- `Cancel`：解析 `{task_id}`，调 `ops.Cancel(taskID, opScope(r))`（幂等 + 属主校验），返回 `{task_id, canceled: true}`。
- `opScope(r)`：从 `middleware.UserFromContext` 取当前用户名作为任务隔离维度。
- `failFileOpErr`：`ErrFileOpNotFound`→404/2017、`ErrFileOpBusy`→503/2018、`storage.ErrBadOp`→400/4000、其他复用 `failFileErr` 词表。三条分支因 sentinel 独立而正确分流。
- `Progress`（SSE）：
  ```go
  ch, snap, unsub := h.ops.Subscribe(taskID, opScope(r))   // 属主校验
  if ch == nil { 404 }
  defer unsub()
  w.Header().Set("Content-Type", "text/event-stream")
  w.Header().Set("Cache-Control", "no-cache")
  w.Header().Set("X-Accel-Buffering", "no")    // 关 nginx 缓冲
  writeSSE(w, {Type:"snapshot", Snapshot:snap}); flush
  for {
      select {
      case <-ctx.Done(): return              // 客户端断开
      case ev, ok := <-ch:
          if !ok { return }                  // 任务结束 channel 关闭
          writeSSE(w, ev); flush
          if ev.Type == "finished" { return }
      case <-time.After(15s):                // 心跳注释防代理超时
          fmt.Fprintf(w, ": keepalive\n\n"); flush
      }
  }
  ```

---

## 8. 错误码（`internal/handler/errors.go`）

```go
CodeFileOpNotFound = 2017
CodeFileOpBusy     = 2018
```

---

## 9. 路由与装配

### `internal/server/routes.go`

`Deps` 新增 `FileOps *service.FileOpService`。注册：

```go
fileOpHandler := handler.NewFileOpHandler(d.FileOps, d.Logger)
...
// SSE 只读长连接，仅全局限流
protected.Get("/fs/op/progress", fileOpHandler.Progress)
// 发起 / 取消走写限流 10/s
protected.Group(func(wr) {
    wr.Use(writeLimit)
    wr.Post("/fs/op/copy", fileOpHandler.Copy)
    wr.Post("/fs/op/move", fileOpHandler.Move)
    wr.Post("/fs/op/delete", fileOpHandler.Delete)
    wr.Post("/fs/op/cancel", fileOpHandler.Cancel)
})
```

### `cmd/flist/main.go`

```go
fileOpSvc := service.NewFileOpService(fileSvc, logger)
...
go runFileOpSweep(cleanupCtx, fileOpSvc, logger)   // 每分钟 Sweep
...
server.NewRouter(server.Deps{..., FileOps: fileOpSvc, ...})
```

`runFileOpSweep` 每分钟调 `ops.Sweep()`，清理过期已完成任务并记日志。

---

## 10. 前端类型（`frontend/src/types.ts`）

新增 `FileOpStatus`、`FileOpKind`、`FileOpSnapshot`、`FileOpEvent`、`FileOpStartResult`、`FileOpTask`、`FileOpItem`、`FileOpItemStatus`。

`FileOpTask` 含：
- `items?: FileOpItem[]` — 逐项状态（详情面板用），运行中由 `item_start`/`item_done` 事件重建，完成后由 `finished` 的 `results[]` 覆盖；
- `_es?: EventSource` — SSE 连接句柄，用于关闭。

`FileOpItem`：`{index, name, size, status, error?}`，`status` ∈ `pending|running|done|failed|canceled|skipped`。任务创建时即用 `srcs` 初始化完整 pending 列表（避免稀疏数组空洞导致 `filter`/`map` 访问 `undefined` 崩溃）。

---

## 11. 前端 API（`frontend/src/lib/api.ts`）

新增 `api.fs.op`：

```typescript
op: {
    async copy(src, dst, autoRename): Promise<FileOpStartResult>   // POST /api/fs/op/copy
    async move(src, dst, autoRename): Promise<FileOpStartResult>
    async delete(paths): Promise<FileOpStartResult>
    async cancel(taskId): Promise<void>                            // POST /api/fs/op/cancel
    progress(taskId): EventSource {                                // GET SSE，依赖同源 Cookie
        return new EventSource(`/api/fs/op/progress?id=${encodeURIComponent(taskId)}`);
    }
}
```

`mapFileOpStart` 把 snake_case 映射为 camelCase。

---

## 12. 前端 fileOpStore（`frontend/src/fileOpStore.ts`，新建）

### 状态

```typescript
interface FileOpState {
    tasks: FileOpTask[];
    panelOpen: boolean;
    startCopy / startMove / startDelete / cancelTask / removeTask / clearFinished / closePanel
}
```

### `startOp`

调 `api.fs.op.copy/move/delete` 获取 `task_id`，建 `FileOpTask`（status=queued）入列表并 `panelOpen=true`；发起失败则入一条 error 任务展示原因。随后 `subscribe`。

### `subscribe`

```typescript
const es = api.fs.op.progress(taskId);
patch({ _es: es });
es.onmessage = (ev) => {
    const evt = JSON.parse(ev.data);
    applyEvent(set, get, taskId, evt);     // 把 snapshot 字段拍进任务 + 维护 items[]
    if (evt.type === 'finished') { closeStream; unpersistTask; maybeRefresh; }
};
es.onerror = () => {
    if (isUnloading) return;               // 页面卸载期间不处理，避免误清 localStorage
    // 已终态则关闭避免无限重连
    // readyState===CLOSED（服务端 404）→ 标 error（不清 localStorage，由 removeTask/clearFinished 清）
    // 否则（CONNECTING）允许 EventSource 自动重连
};
```

### `applyEvent` — 事件应用 + items[] 维护

```typescript
// 快照字段拍进任务
patch(taskId, { status, totalItems, ... });

// 逐项 items[] 维护：
//   item_start  → items[index] = { name, size, status:'running' }
//   item_done   → items[index].status = ok ? 'done' : (skipped/canceled/failed)
//   finished    → 用 snap.results[] 覆盖 items（authoritative，含 skipped 项）
```

`items` 用 `t.items.map(i => ({...i}))` 浅拷贝后更新（避免 `[...sparse]` 展平空洞为 `undefined` 导致 `filter` 崩溃）。

### `maybeRefresh`（完成时按相关性刷新）

```typescript
const cur = useFsStore.getState().currentPath;
const rel =
    op === 'delete' ? srcs.some(p => p === cur || parentPath(p) === cur) :
    op === 'move'   ? cur === dst || srcs.some(p => p === cur || parentPath(p) === cur) :
                      cur === dst;   // copy
if (!rel) return;
if (searchOpen) runSearch(query); else refresh();
```

move / delete 的相关性包含 `src 本身路径`（用户停留在被移走的源目录时也需刷新），修正了早期只判 `parentPath` 的遗漏。

### localStorage 持久化（跨标签页 / 刷新恢复）

`flist:fileop:tasks` 存 `[{id, op, srcs, dst}]`（仅 task_id + 元数据，不含状态——状态由 SSE 恢复）：

- `startOp` 成功后 `persistTask`；
- `finished` 事件 / `removeTask` / `clearFinished` 时 `unpersistTask`；
- 模块加载时 `restorePersistedTasks` 读 localStorage → 为每个 id 创建占位任务（status=queued + 完整 pending items）→ `subscribe` → SSE snapshot 立即填充真实状态；
- 服务端 404（任务过期）→ `onerror` CLOSED → 标 error，用户可「清除已完成」清理。

**`isUnloading` 标记**：`beforeunload` / `pagehide` 时置 `true`，`onerror` 检测到则直接 return——防止页面刷新时浏览器关闭旧 EventSource 触发 `onerror` 误清 localStorage，导致新页面无法恢复进行中的任务。

### 循环依赖处理

`fileOpStore` 导入 `useFsStore`（用于 `maybeRefresh`），`fsStore` 导入 `useFileOpStore`（用于 paste/remove 委托）。ES module live bindings 使该循环可工作：双方都只在运行时（函数体内）调用对方 `getState()`，不在模块求值期访问，故无 TDZ 问题。

---

## 13. 前端 fsStore 委托（`frontend/src/fsStore.ts`）

### `paste`

```typescript
paste: async () => {
    const clip = get().clipboard;
    if (!clip || !clip.paths.length) return null;
    const dst = get().currentPath;
    if (clip.mode === 'copy') {
        void useFileOpStore.getState().startCopy(clip.paths, dst, true);
    } else {
        void useFileOpStore.getState().startMove(clip.paths, dst, true);
        set({ clipboard: null });        // 剪切发起即清剪贴板
    }
    return null;                          // 立即返回，不 await refresh
}
```

### `removePaths`

```typescript
removePaths: async (paths) => {
    if (!paths.length) return null;
    void useFileOpStore.getState().startDelete(paths);
    if (searchOpen && searchQuery) {      // 搜索态清失效选择并重搜
        set({ searchSelected: new Set(), searchAnchor: null });
        void get().runSearch(searchQuery);
    }
    return null;
}
```

旧 `api.fs.copy/move/remove` 在 `api.ts` 中保留（用户要求保留旧同步接口），前端不再调用但可用。

---

## 14. 前端 TransferPanel（`frontend/src/components/TransferPanel.tsx`）

### 面板合并

`open` / `active` / `totalCount` / `doneCount` 统计加入 `fileOp`；`closeAll` 调 `fileOp.clearFinished()`。任务列表顺序：下载在上 → 文件操作居中 → 上传在下。

### `FileOpRow`

- 项内字节进度条：`curSize > 0 ? curCopied/curSize : doneItems/totalItems`；
- 状态文字：`curPct% · 已复制/总字节 · 总 totPct% (doneItems/totalItems) · 速率/s`；
- queued 态不确定态进度条（`animate-indeterminate`）；
- canceled 态展示明细：`已取消 · 成功 N · 未完成 M`（从 `items[]` 统计）；
- done 态若有单项失败显示「N 项失败」；
- `FileOpStatusIcon` 按 op 着色：copy 蓝 / move 紫 / delete 橙；
- 运行中可取消（`Ban`），终态可移除（`X`）；
- **点击主体区域打开详情 Modal**（`onOpenDetail`）。

### `FileOpDetailModal`

点击任务行弹出（复用 `Modal` 组件，`maxWidth="lg"`）：

- 顶部汇总栏：成功 N · 失败 M · 未完成 K + 整体状态；
- 逐项列表：每行 `FileOpItemIcon`（✓/✗/⊘/⏳/—）+ basename + size + error 文本；
- 运行中由 `item_start`/`item_done` 事件重建的 `items[]` 展示；完成后由 `finished` 的 `results[]` 覆盖；
- 整体错误（如目标非法）单独展示。

---

## 15. 测试（`internal/service/fileop_test.go`）

| 测试 | 覆盖点 |
| --- | --- |
| `TestFileOpCopyProgress` | 复制两文件 → finished 携带 OpResult[]、DoneItems 正确、落盘成功 |
| `TestFileOpDelete` | 删除项级 item_done + finished |
| `TestFileOpCancel` | 取消请求使任务进入 canceled 终态 |
| `TestFileOpCopyProgressSlow` | 关闭节流后验证 item_progress 确实推送（字节回调链路） |
| `TestFileOpGetMissing` | 不存在任务 Get/Subscribe 返回 false/nil |
| `TestFileOpOwnerIsolation` | 非属主无法订阅 / 取消他人任务（not-found 语义，不泄漏存在性） |
| `TestFileOpQueueBusy` | `ErrFileOpBusy` / `ErrFileOpNotFound` 是独立 sentinel，不与 `ErrBadOp` 混淆 |
| `TestFileOpCancelBeforeRun` | 入队后被取消 → 所有项补 skipped、results 覆盖全部 totalItems、文件未删除 |
| `TestFileOpCancelResultsComplete` | 复制中途取消 → results 覆盖全部项、半成品被清理（不依赖取消到第几项） |

节流间隔设为 `var` 而非 `const`，便于 `TestFileOpCopyProgressSlow` 临时置 0 验证慢复制路径。

---

## 验证结果

```
go build ./...              # 通过
go vet ./...                # 通过
go test ./...               # 全绿（util/local/service 既有测试无回归）
npm run lint (tsc --noEmit) # 通过
make build                  # 前端打包进 web/dist 供 go:embed
```

手动验证：复制 2 大文件中途取消 → 详情面板显示「成功 1 · 未完成 1」、半成品清理；新标签页 / 刷新恢复进行中任务进度。

---

## review 关注点（已修复）

本轮 review 发现并修复的问题，供后续参考：

1. **错误码 sentinel 复用**（高）：`ErrFileOpNotFound`/`ErrFileOpBusy` 原复用 `storage.ErrBadOp`，handler switch 全部命中 404。已改为独立 sentinel。
2. **跨用户越权**（高）：`Cancel`/`Subscribe`/`Get` 无属主校验。已加 `userScope` 参数，非属主按 not-found 语义。
3. **Sweep TTL 用 startedAt**（中）：长任务完成后立即被清。已改为 `finishedAt`。
4. **finished 事件可丢弃**（中）：慢客户端拿不到终态。已改为方案 B 排空兜底保证入队。
5. **move 跨盘无 CheckSpace**（中）：目标盘满时复制中途失败。已对 move 也预检。
6. **TreeSize 无超时**（中）：大目录拖慢 202 响应。已套 5s 独立 deadline。
7. **取消竞态**（中）：`execErr==nil` 但 `ctx` 已取消时误标 canceled。已改为先判 `execErr`。
8. **前端稀疏数组崩溃**（中）：`[...items]` 展平空洞为 `undefined`，`filter` 崩溃白屏。已改为创建时初始化完整 pending 列表 + `map` 浅拷贝。
9. **前端 localStorage 误清**（中）：页面卸载 `onerror` 误清 localStorage 致新页面无法恢复。已加 `isUnloading` 标记。
10. **maybeRefresh 遗漏**（低）：move/delete 停留在被移走的源目录本身时不刷新。已加 `src === cur` 判定。
