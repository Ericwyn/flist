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

	"flist/internal/model"
)

// 驱动层统一错误词表。各驱动须把自身的底层错误归一化为这些值，
// service / handler 据此用 errors.Is 映射对外错误码，无需感知具体驱动。
var (
	ErrNotFound     = errors.New("path not found")          // 2001
	ErrTraversal    = errors.New("path traversal detected") // 2002
	ErrForbidden    = errors.New("permission denied")       // 2003
	ErrExists       = errors.New("target already exists")   // 2004
	ErrInvalidName  = errors.New("invalid name")            // 2006
	ErrNotFile      = errors.New("not a regular file")      // 2007
	ErrNotDir       = errors.New("not a directory")         // 2008
	ErrBadOp        = errors.New("invalid operation")       // 4000
	ErrNotSupported = errors.New("operation not supported") // 由 Caps 决定，映射 4000
)

// Caps 声明驱动支持的能力。service 据此对不支持的操作直接返回 ErrNotSupported，
// 避免把「能力差异」散落到每个方法的实现里。
type Caps struct {
	Write     bool // mkdir / touch / move / remove
	Copy      bool // 复制（Phase 3）
	Upload    bool // 分片上传（Phase 4，配合可选 Uploader 接口）
	DiskUsage bool // 磁盘 / 配额用量（Phase 6，配合可选 Usager 接口）
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

// ErrStopWalk 是 Walk 回调用于提前结束遍历的哨兵错误（命中上限 / 超时），
// 驱动应识别它并停止遍历、且不把它当作真正的错误向上返回。
var ErrStopWalk = errors.New("stop walk")
