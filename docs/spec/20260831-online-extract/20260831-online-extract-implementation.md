# 在线解压功能实现细节

本文档对应 `20260831-online-extract-overview.md`，用于指导代码改造和 review。

## 改动范围

| 模块 | 文件 | 改动 |
| --- | --- | --- |
| 依赖 | `go.mod` / `go.sum` | 增加 `github.com/nwaples/rardecode/v2` |
| Model | `internal/model/fileop.go` | `FileOpExtract` 枚举 |
| Storage | `internal/storage/mux.go` | 实现并路由 `StreamWriter.OpenWrite` |
| Service | `internal/service/extract.go` | 新增格式识别、decoder、安全落盘、回滚 |
| Service | `internal/service/fileop.go` | Start 校验和 `execExtract` 分支 |
| Service | `internal/service/file.go` | 解压错误映射到稳定 error name |
| Handler | `internal/handler/fileop.go` | 新增 `Extract` handler |
| Routes | `internal/server/routes.go` | 注册 `/api/fs/op/extract` |
| 前端类型 | `frontend/src/types.ts` | `FileOpKind` 增加 `extract` |
| 前端 API | `frontend/src/lib/api.ts` | `api.fs.op.extract(path)` |
| 前端 Store | `frontend/src/fileOpStore.ts` | `startExtract`、恢复和刷新判断 |
| 前端路径 | `frontend/src/lib/path.ts` | 受支持扩展名判断 |
| 前端 UI | `FileBrowser.tsx` / `TransferPanel.tsx` | 右键入口、文案、图标、进度 |
| 测试 | service/server/storage/frontend 构建 | 覆盖格式、安全、回滚、协议 |

## 1. Model 与任务协议

```go
const (
    FileOpCopy    = "copy"
    FileOpMove    = "move"
    FileOpDelete  = "delete"
    FileOpExtract = "extract"
)
```

不改变 `FileOpStartResult`、`FileOpSnapshot`、`FileOpEvent` 的字段。extract 使用一个顶层 item：

```go
srcs       = []string{archivePath}
dst        = path.Dir(archivePath) // 仅用于前端刷新元数据
totalItems = 1
```

`results[0].Src` 是源压缩包路径。成功为 `OK=true`；失败的 `Error` 使用 overview 中的稳定错误名。

## 2. 发起接口

`FileOpHandler.Extract`：

```go
type opExtractRequest struct {
    Path string `json:"path"`
}

func (h *FileOpHandler) Extract(w http.ResponseWriter, r *http.Request) {
    // decode + 非空校验
    // h.ops.Start(ctx, model.FileOpExtract, scope, []string{path}, "", false)
    // 202 FileOpStartResult
}
```

`FileOpService.Start` 对 extract 做同步轻量预检：

1. 只允许一个源路径。
2. `Stat` 必须存在、是普通文件且不是不可达链接。
3. 扩展名必须由 `DetectArchiveFormat` 识别。
4. `totalBytes` 初始为压缩包文件大小。
5. 不在 HTTP 请求内打开并扫描归档，避免损坏大包拖慢 202。

## 3. 格式识别

```go
type archiveFormat string

const (
    archiveZIP   archiveFormat = "zip"
    archiveRAR   archiveFormat = "rar"
    archiveTarGZ archiveFormat = "tar.gz"
)

func detectArchiveFormat(name string) (archiveFormat, bool)
func archiveBaseName(name string) string
```

判断顺序必须先 `.tar.gz`，再 `.zip` / `.rar`；全部大小写不敏感。仅以用户要求的文件名为准，不增加 `.tgz` 等隐式格式。

## 4. Storage 写能力

`Mux` 增加：

```go
var _ StreamWriter = (*Mux)(nil)

func (m *Mux) OpenWrite(ctx context.Context, p string) (io.WriteCloser, error) {
    _, b, rel, err := m.route(p)
    if err != nil { return nil, err }
    if b == nil || rel == "/" { return nil, ErrBadOp }
    sw, ok := b.(StreamWriter)
    if !ok { return nil, ErrNotSupported }
    return sw.OpenWrite(ctx, rel)
}
```

这使 service 可以在不知道具体 mount/backend 的情况下，以统一 API 路径写 `/files/...` 或 `/drive/<id>/...`。

## 5. 临时根与最终目录

### 创建临时根

```text
parent = path.Dir(src)
temp   = parent + "/.flist-extract-" + taskID 前缀
```

使用 `Backend.Mkdir` 原子创建；极小概率冲突时重新生成 token。只记录成功创建的精确路径，清理时不得使用 glob 或宽泛目录。

### 最终命名

```go
func finalizeExtract(ctx, temp, parent, base string) (string, error)
```

按以下顺序尝试 `Backend.Move`：

```text
base
base (2)
base (3)
...
```

遇到 `ErrExists` 继续；其他错误立即返回。目录名整体加编号，不把名称中的点视为文件扩展名。

### 清理

`execExtract` 创建临时根后设置 defer：未成功提交时用独立、短超时的 `context.Background()` 清理，避免任务 ctx 已取消导致清理本身被跳过。

## 6. 条目路径校验

```go
func cleanArchiveEntry(name string) (string, error)
```

规则：

1. `strings.ReplaceAll(name, "\\", "/")`。
2. 空字符串、NUL、`/` 开头、盘符开头拒绝。
3. `path.Clean`。
4. `clean == ".." || strings.HasPrefix(clean, "../")` 拒绝。
5. 对各 segment 调用 `util.ValidateName`。
6. `path.Join(tempRoot, clean)` 后检查结果等于/前缀属于 `tempRoot + "/"`。

目录条目 `.` 可作为归档根占位跳过；普通文件名清理后为空则失败。

## 7. 公共落盘器

```go
type extractBudget struct {
    ctx       context.Context
    written   int64
    maxBytes  int64 // 0 表示后端无法提供预算
    onWritten func(total int64)
}

type budgetWriter struct {
    dst    io.Writer
    budget *extractBudget
}
```

`budgetWriter.Write`：

1. 写前检查 `ctx.Err()`。
2. 检查 `written + len(p)` 是否超过可用空间预算。
3. 写入 `StreamWriter`。
4. 累计实际写入字节。
5. 调用格式对应的进度回调（ZIP 用输出字节；TAR.GZ/RAR 的 UI 进度由输入 reader 上报，但 budget 仍只负责空间）。

单文件流程：

```text
ensure parent dirs
  -> StreamWriter.OpenWrite(target)
  -> io.CopyBuffer(budgetWriter, entryReader)
  -> Close 原子提交单文件
```

任一错误最终都会删除整个临时根，所以即使底层 writer 在 Close 时提交了当前部分文件，也不会泄漏到最终目录。

## 8. ZIP decoder

1. `storage.File` 只有 `ReadSeeker`；实现带 mutex 的 `readerAtFromSeeker`，供 `zip.NewReader` 使用。
2. 遍历中央目录，先完成所有 entry 路径校验、条目数检查和普通文件 uncompressed size 求和。
3. symlink mode 条目跳过。
4. 目录调用 `ensureDir`；普通文件 `Open` 后流式复制。
5. `CurSize/TotalBytes` 更新为普通文件 uncompressed size 总和，`CurCopied` 使用累计实际输出字节。
6. 不支持的 compression method、CRC 错误和内容损坏归一为 `archive_corrupt` 或 `unsupported_archive`。

## 9. TAR.GZ decoder

1. 源文件包一层 `countingProgressReader`。
2. `gzip.NewReader` -> `tar.NewReader` 顺序遍历。
3. `TypeDir` 创建目录；`TypeReg/TypeRegA` 流式写入。
4. symlink/hardlink/device/FIFO 等特殊类型跳过。
5. 进度为 `countingProgressReader.read / sourceSize`；每次 reader 读取和每个 entry 完成时更新。
6. gzip/tar header、checksum、unexpected EOF 错误归一为 `archive_corrupt`。

## 10. RAR decoder

```go
r, err := rardecode.NewReader(
    countingReader,
    rardecode.MaxDictionarySize(128<<20),
)
```

1. `Next()` 顺序遍历，兼容 solid archive 的顺序解码要求。
2. `FileHeader.IsDir` 创建目录。
3. `LinkType != LinkTypeNone` 的条目跳过。
4. 普通文件从 reader 本身流式复制。
5. 进度为底层压缩文件读取字节 / sourceSize。
6. `ErrArchiveEncrypted` / `ErrArchivedFileEncrypted` -> `archive_encrypted`。
7. `ErrMultiVolume` -> `archive_multivolume`。
8. 字典超限或不支持 decoder -> `unsupported_archive`。
9. 其余格式/校验错误 -> `archive_corrupt`。

## 11. FileOpService 执行

```go
func (s *FileOpService) execExtract(t *fileOpTask) {
    // stat + detect
    // startItem(index=0, sourceName, sourceSize)
    // create temp root
    // extract by format, reporting progress
    // canceled/error -> cleanup + terminal state
    // success -> Move temp to conflict-free final dir
    // DoneItems=1, DoneBytes=progress total, result OK
    // finishTask(done)
}
```

取消判断保持现有竞态语义：

1. decoder/io.Copy 返回错误且 ctx 已取消 -> canceled。
2. 解压和最终 Move 已成功、随后才收到 cancel -> 任务仍记 done。
3. 取消时结果包含 `{src, ok:false, error:"canceled"}`。

extract 是“单一事务型任务”，任一归档条目失败都以整体 `FileOpFailed` 结束；这与 copy/move 的逐项尽力而为语义不同。

## 12. 前端实现

### 类型与 API

```ts
export type FileOpKind = 'copy' | 'move' | 'delete' | 'extract';

api.fs.op.extract(path)
// POST /api/fs/op/extract { path }
```

### Store

```ts
startExtract(path: string): Promise<void>
```

复用 `startOp`；extract 分支调用新 API，`dst` 在本地元数据中保存为 `parentPath(path)`。localStorage 的 `PersistedTask.op` 自动兼容新增枚举值。

`maybeRefresh` 对 extract 的相关性判断：

```ts
t.op === 'extract' && cur === parentPath(t.srcs[0])
```

### 右键识别

`frontend/src/lib/path.ts`：

```ts
export function isExtractableName(name: string): boolean {
  const lower = name.toLowerCase();
  return lower.endsWith('.zip') || lower.endsWith('.rar') || lower.endsWith('.tar.gz');
}
```

`FileBrowser` 单文件菜单在“下载”后插入：

```tsx
if (isExtractableName(entry.name)) {
  fileItems.push({
    label: '解压',
    icon: <ArchiveRestore className="w-4 h-4" />,
    onClick: () => startExtract(joinPath(currentPath, entry.name)),
  });
}
```

搜索结果目前没有条目右键菜单，因此本期不新增搜索态入口。

### TransferPanel

仅扩展现有条件映射：

1. `fileOpTitle` / `opLabel`：extract -> “解压”。
2. `FileOpStatusIcon`：running extract -> `ArchiveRestore` 蓝色。
3. extract running label只展示百分比、进度阶段和速率；不宣称不同格式下的字节含义一致。
4. `curSize == 0` 时显示现有 `animate-indeterminate`，而不是 0% 空条。

不改变面板宽度、圆角、阴影、字体、颜色或其他布局。

## 13. 测试夹具策略

ZIP/TAR.GZ 在测试中由标准库动态生成。RAR 写入器不在依赖能力内，测试 helper 按公开的 RAR 4.x header 字段动态构造 `store` method 最小归档，避免提交不透明二进制 fixture，也不依赖系统命令。

测试不得依赖系统 `rar`/`unrar` 命令，否则 CI 和单二进制约束无法得到证明。

## 14. SSE 并发修正

实现期的 race detector 发现原 `FileOpService.emit` 在解锁后直接遍历订阅者 map，而新订阅可能同时写入。修正为在 `task.mu` 内复制 channel 切片，解锁后再非阻塞发送。该修正确保快速解压任务在“任务开始”和“前端刚建立 SSE”并发时不会触发 map 数据竞争。

## 15. 完成判定

只有以下证据全部成立才视为完成：

1. spec 两份文档存在且与代码一致。
2. 三种扩展名均能触发任务；三种 decoder 均有自动化成功用例。
3. 路径穿越、失败、取消均证明不会留下最终目录或任务临时根。
4. 同名目录冲突不修改原目录并产生 `(2)`。
5. SSE 快照能观察到 extract 进度并到达正确终态。
6. 右键入口只对受支持文件出现，TransferPanel 样式沿用现有实现。
7. `go test ./...`、`go test -race ./internal/service`、`go vet ./...` 与 `npm run build` 通过；若最新 main 自身存在与本功能无关的前端类型错误，需用基线对比记录，且单独校验本功能涉及的 TypeScript 文件。
8. 前端产物同步到 `web/dist`，单二进制构建可包含新 UI。

## 16. 本次实施验证结果

1. ZIP、TAR.GZ、RAR 成功解压测试通过。
2. 同名目录 `(2)`、路径穿越回滚、ZIP 符号链接跳过、损坏包回滚、中途取消清理、空间预算、无写流后端回滚均通过。
3. `/files` 单层 Mux、`/drive/<id>` 双层 Mux 与 Handler/SSE 集成测试通过。
4. `go test ./...`、`go test -race ./internal/service`、`go vet ./...` 通过。
5. Linux amd64、Windows amd64 交叉编译与完整 `make build` 通过，前端产物已同步到 `web/dist`。
6. `npm run build` 与本功能涉及文件的独立 TypeScript 校验通过。
7. 全量 `npm run lint` 仍被最新 `origin/main` 原有的 `frontend/src/components/DeviceManager.tsx:106` 类型错误阻断：`key` 不属于 `DeviceRowProps`；该文件与本次差异无关，基线内容已通过 `git show origin/main:...` 对比确认一致。
8. 基于实际 fetch 后的最新 `origin/main`（`81df7d9`）重新迁移实现，差异审计未带入旧分支中的磁盘管理草稿。
