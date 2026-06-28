// Package storage 定义文件存储后端（驱动）的抽象。
//
// 设计目标：把「文件如何存取」与「业务编排」解耦。service 层只面向 Backend
// 接口做排序 / 分页 / 搜索匹配 / 预览嗅探 / 批量聚合等后端无关的编排；具体的
// 寻址、路径安全、符号链接、权限语义等由各驱动实现：
//
//   - local：本地文件系统（首个实现）
//   - webdav / 网盘：远期实现
//   - Mux：按挂载前缀把多个后端组合成一个统一命名空间（本地 + 多网盘并存）
//
// 所有接口入参均为「相对该后端根的 API 路径」（以 / 分隔、以 / 开头）。驱动负责
// 把 API 路径翻译为自身的寻址方式，并保证操作不逃逸出自身命名空间。
package storage

import (
	"context"
	"errors"
	"io"
	"time"

	"flist/internal/model"
)

// 驱动层统一错误词表。各驱动须把自身的底层错误归一化为这些值，
// service / handler 据此用 errors.Is 映射对外错误码，无需感知具体驱动。
var (
	ErrNotFound     = errors.New("path not found")          // 2001
	ErrTraversal    = errors.New("path traversal detected") // 2002
	ErrForbidden    = errors.New("permission denied")       // 2003
	ErrExists       = errors.New("target already exists")   // 2004
	ErrDiskFull     = errors.New("insufficient disk space") // 2005
	ErrInvalidName  = errors.New("invalid name")            // 2006
	ErrNotFile      = errors.New("not a regular file")      // 2007
	ErrNotDir       = errors.New("not a directory")         // 2008
	ErrBadOp        = errors.New("invalid operation")       // 4000
	ErrNotSupported = errors.New("operation not supported") // 由 Caps 决定，映射 4000

	// 文本编辑相关（文件编辑与路径级容量优化）。
	ErrFileModified     = errors.New("file modified since read")  // 2012：保存时 revision 不匹配
	ErrUnsupportedMedia = errors.New("not an editable text file") // 2013：非可编辑文本
	ErrFileTooLarge     = errors.New("file too large to edit")    // 2014：超过可编辑大小上限
	ErrReadonly         = errors.New("readonly storage")          // 2015：后端 / 文件只读
	ErrInvalidRev       = errors.New("invalid revision")          // 2016：expected_revision 缺失或非法
)

// Caps 声明驱动支持的能力。service 据此对不支持的操作直接返回 ErrNotSupported，
// 避免把「能力差异」散落到每个方法的实现里。
type Caps struct {
	Write     bool // mkdir / touch / move / remove
	Copy      bool // 复制（Phase 3）
	Upload    bool // 分片上传（Phase 4，配合可选 Uploader 接口）
	DiskUsage bool // 磁盘 / 配额用量（Phase 6，配合可选 Usager 接口）
	Edit      bool // 文本读取 / 保存（配合可选 ContentEditor 接口）
}

// File 是可下载 / 预览的文件句柄。必须可随机读（Seek）以支撑 HTTP Range。
//
// 本地驱动直接返回 *os.File；远程驱动需以「Range GET 支撑的惰性 reader」或
// 「先落临时文件再提供」等方式实现可 Seek 语义（见 docs 驱动抽象设计）。
type File interface {
	io.ReadSeeker
	io.Closer
}

// Backend 是文件存储驱动的核心接口。入参 p / src / dst 均为相对该后端根的 API 路径。
//
// 约定：
//   - 返回的 model.FileInfo 中，SymlinkTarget（如有）为相对该后端根的 API 路径；
//     组合驱动（Mux）负责在跨后端时改写为虚拟命名空间下的完整路径。
//   - List 返回的条目已按 showHidden 过滤；排序与分页由 service 负责（后端无关）。
//   - 写类操作在驱动不支持写时返回 ErrNotSupported（与 Caps.Write=false 一致）。
type Backend interface {
	// Name 返回驱动类型名（如 "local" / "webdav"），用于日志与诊断。
	Name() string
	// Capabilities 返回驱动能力集。
	Capabilities() Caps

	// Stat 返回单个文件 / 目录信息。
	Stat(ctx context.Context, p string) (*model.FileInfo, error)
	// List 返回目录下的条目（已按 showHidden 过滤，未排序未分页）。
	List(ctx context.Context, p string, showHidden bool) ([]model.FileInfo, error)
	// Open 打开普通文件供下载 / 预览，返回可随机读的句柄与元信息。
	// 调用方负责关闭返回的 File。
	Open(ctx context.Context, p string) (File, *model.FileInfo, error)

	// Mkdir 创建单层目录（父目录须存在，目标不存在）。
	Mkdir(ctx context.Context, p string) error
	// Create 创建空文件（不存在才创建，不 truncate 已有文件）。
	Create(ctx context.Context, p string) error
	// Move 移动 / 重命名 src 到 dst（落点须不存在，不覆盖）。
	Move(ctx context.Context, src, dst string) error
	// Remove 递归删除 p。
	Remove(ctx context.Context, p string) error
	// Copy 递归复制 src 到 dst（落点须不存在）。Caps.Copy=false 时返回 ErrNotSupported。
	Copy(ctx context.Context, src, dst string) error
}

// Walker 是可选接口：驱动可提供高效的递归遍历（本地用 WalkDir、WebDAV 用
// PROPFIND Depth:infinity）。未实现时 service 退化为基于 List 的逐层递归。
//
// 回调的 relPath 为相对 root 的 API 路径片段（不以 / 开头），由 service 与搜索
// 起点拼接成完整路径；这样驱动无需知道自身在虚拟命名空间中的挂载位置。
type Walker interface {
	Walk(ctx context.Context, root string, showHidden bool, fn func(relPath string, info model.FileInfo) error) error
}

// Usager 是可选接口：返回路径所在存储的容量与可用空间（Phase 6 / 上传预检）。
type Usager interface {
	Usage(ctx context.Context, p string) (total, free uint64, err error)
}

// ContentEditor 是可选接口：驱动提供「完整读取可编辑文本 + 乐观锁保存」的能力
// （文件编辑与路径级容量优化）。与 Walker / Usager / Uploader 同构：service 用类型断言
// 探测，驱动不支持时上层返回 ErrNotSupported。与 Caps.Edit 成对出现。
//
// 与 Open（下载 / 预览）的区别：ReadText 完整读取并返回保存所需的 revision、编码与行尾
// 探测结果，且对非文本 / 超限文件显式拒绝；下载链路不感知这些编辑语义。
type ContentEditor interface {
	// ReadText 完整读取可编辑文本：解析路径、校验普通文件、大小上限（maxBytes>0 时生效）、
	// 文本嗅探，读取全部内容并计算 revision。非文本返回 ErrUnsupportedMedia，
	// 超限返回 ErrFileTooLarge。
	ReadText(ctx context.Context, p string, maxBytes int64) (*model.FileContentResult, error)
	// WriteText 以乐观锁保存文本：重新读取当前 revision，与 expected 不一致且 force=false 时
	// 返回 ErrFileModified；通过后写同目录临时文件并原子替换，返回新 revision。
	// content 原样写入，不做行尾 / 编码转换。
	WriteText(ctx context.Context, p string, content []byte, expected model.FileRevision, force bool) (*model.SaveContentResult, error)
}

// Uploader 是可选接口：驱动提供分片上传的物理存取（暂存 / 合并 / 清理）。
//
// 会话元数据（已收分片集合、文件指纹、user 归属、过期时间）由 service 的 UploadService
// 在内存维护；驱动只负责按 uploadID 把分片字节落盘，并在 complete 时按序拼接到目标。
// uploadID 由 service 生成的高熵随机 token，驱动可安全地用作暂存目录名（无路径注入）。
//
// 与 Walker / Usager 同构：service 用类型断言探测，驱动不支持时上层返回 ErrNotSupported。
type Uploader interface {
	// StageChunk 将 uploadID 的第 index 个分片写入暂存区（先写临时文件再 rename，
	// 同 index 重传幂等覆盖）。返回写入字节数。暂存目录惰性创建。
	StageChunk(ctx context.Context, uploadID string, index int, r io.Reader) (int64, error)
	// MergeUpload 按序拼接 uploadID 的 [0, totalChunks) 分片到 dst，成功后删除暂存区。
	// dst 为相对该后端根的 API 路径，驱动负责 SafeResolve + 文件名校验 + 父目录存在性。
	// overwrite=false 且 dst 已存在 → ErrExists；overwrite=true 则原子替换
	//（先写同目录临时文件再 rename，避免合并中途损坏既有文件）。
	MergeUpload(ctx context.Context, uploadID, dst string, totalChunks int, overwrite bool) error
	// AbortChunk 删除某次上传中单个分片的暂存文件（幂等）。用于分片大小校验失败时
	// 清掉写坏的分片，便于后续重传纠正。
	AbortChunk(uploadID string, index int) error
	// AbortUpload 删除 uploadID 的暂存区（幂等，不存在不报错）。
	AbortUpload(uploadID string) error
	// SweepStaging 删除最后修改时间早于 now-maxAge 的孤儿暂存目录，返回清理数量。
	SweepStaging(maxAge time.Duration) (int, error)
}

// ErrStopWalk 是 Walk 回调用于提前结束遍历的哨兵错误（命中上限 / 超时），
// 驱动应识别它并停止遍历、且不把它当作真正的错误向上返回。
var ErrStopWalk = errors.New("stop walk")
