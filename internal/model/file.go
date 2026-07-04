package model

import "time"

// FileInfo 描述单个文件/目录条目，供 list/stat 返回。
type FileInfo struct {
	Name          string    `json:"name"`
	Type          string    `json:"type"` // "file" | "dir"
	Size          int64     `json:"size"` // 目录为 0
	Mode          string    `json:"mode"` // 八进制字符串，如 "0644"
	ModTime       time.Time `json:"mod_time"`
	IsSymlink     bool      `json:"is_symlink"`
	SymlinkTarget string    `json:"symlink_target,omitempty"` // 相对 root 的 API 路径，尽力解析
	Unreachable   bool      `json:"unreachable,omitempty"`    // 符号链接目标越界/不可达
}

// 文件类型常量。
const (
	TypeFile = "file"
	TypeDir  = "dir"
)

// ListResult 是目录列表接口的返回体。
type ListResult struct {
	Path     string     `json:"path"`
	Total    int        `json:"total"` // 过滤后、分页前的总条目数
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
	Items    []FileInfo `json:"items"`
}

// PreviewResult 是预览接口的返回体。
type PreviewResult struct {
	Type         string `json:"type"`          // text | binary | image | video | audio
	Content      string `json:"content"`       // 仅 text 时有值
	Truncated    bool   `json:"truncated"`     // 内容是否被截断
	Size         int64  `json:"size"`          // 文件总大小
	PreviewBytes int    `json:"preview_bytes"` // 预览读取上限
}

// OpResult 批量写操作（move / delete）的单条结果。
type OpResult struct {
	Src   string `json:"src"`             // 操作对象的 API 路径
	OK    bool   `json:"ok"`              // 是否成功
	Error string `json:"error,omitempty"` // 失败时的错误码名（如 "file_exists"）
}

// SearchHit 单条搜索命中。
type SearchHit struct {
	Path    string    `json:"path"` // 相对 root 的 API 路径（含文件名）
	Name    string    `json:"name"`
	Type    string    `json:"type"` // file | dir
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
}

// SearchResult 是搜索接口的返回体。
type SearchResult struct {
	Query     string      `json:"query"`
	Base      string      `json:"base"` // 搜索起点 API 路径
	Items     []SearchHit `json:"items"`
	Truncated bool        `json:"truncated"` // 命中达到上限被截断
	TimedOut  bool        `json:"timed_out"` // 遍历超时提前结束
}

// UploadInitResult 是分片上传初始化接口的返回体。
type UploadInitResult struct {
	UploadID    string `json:"upload_id"`
	ChunkSize   int64  `json:"chunk_size"`   // 服务端最终采用的分片大小（可能被归一）
	TotalChunks int    `json:"total_chunks"` // 总分片数
	Received    []int  `json:"received"`     // 已收分片索引（续传时非空，新会话为 []）
}

// UploadChunkResult 是分片上传接口的返回体。
type UploadChunkResult struct {
	Index    int `json:"index"`    // 本次确认落盘的分片索引
	Received int `json:"received"` // 已收分片总数（进度参考）
}

// UploadCompleteResult 是合并完成接口的返回体。
type UploadCompleteResult struct {
	Path    string `json:"path"`              // 落盘后的 API 路径
	Missing []int  `json:"missing,omitempty"` // 缺片时返回，便于前端补传
}

// SystemInfo 是 GET /api/system/info 的返回体。
//
// 自「文件编辑与路径级容量优化」起，磁盘容量字段移出本结构，统一由
// GET /api/fs/space?path=... 按当前路径所在存储返回；本结构只承载系统级信息。
type SystemInfo struct {
	OS               string    `json:"os"`                // 运行平台（runtime.GOOS）
	Arch             string    `json:"arch"`              // 体系结构（runtime.GOARCH）
	ServerTime       time.Time `json:"server_time"`       // 服务端当前时间
	DeviceManagement bool      `json:"device_management"` // 设备管理是否可用（Linux + lsblk/udisksctl 存在）
}

// FileRevision 是保存文本时用于乐观锁的不透明版本 token。
// 前端不解析其生成规则，保存时原样带回。
type FileRevision struct {
	Token string `json:"token"`
	Weak  bool   `json:"weak"` // 是否为弱校验（如基于 mtime/size 而非内容 hash）
}

// 文本行尾常量。
const (
	LineEndingLF    = "lf"
	LineEndingCRLF  = "crlf"
	LineEndingMixed = "mixed"
	LineEndingNone  = "none"
)

// 文本编码常量（首期仅支持 UTF-8 / UTF-8 BOM）。
const (
	EncodingUTF8    = "utf-8"
	EncodingUTF8BOM = "utf-8-bom"
)

// FileContentResult 是 GET /api/fs/content 的返回体（可编辑文本的完整内容）。
type FileContentResult struct {
	Path       string       `json:"path"`
	Name       string       `json:"name"`
	Size       int64        `json:"size"`
	MIME       string       `json:"mime"`
	Encoding   string       `json:"encoding"`
	LineEnding string       `json:"line_ending"`
	Content    string       `json:"content"`
	ModTime    time.Time    `json:"mod_time"`
	Revision   FileRevision `json:"revision"`
	Editable   bool         `json:"editable"`
	Readonly   bool         `json:"readonly"`
}

// SaveContentResult 是 PUT /api/fs/content 保存成功后的返回体。
type SaveContentResult struct {
	Path     string       `json:"path"`
	Size     int64        `json:"size"`
	ModTime  time.Time    `json:"mod_time"`
	Revision FileRevision `json:"revision"`
}

// SaveConflict 是保存冲突（409）时返回的 data 体，告知前端当前最新版本。
type SaveConflict struct {
	Path            string       `json:"path"`
	CurrentModTime  time.Time    `json:"current_mod_time"`
	CurrentRevision FileRevision `json:"current_revision"`
}

// MountInfo 描述命中的存储挂载点（单挂载透明模式下 name 为驱动名、prefix 为 "/"）。
type MountInfo struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
}

// SpaceUsage 是某路径所在存储的容量信息。supported=false 时其余字段无意义。
type SpaceUsage struct {
	Supported   bool    `json:"supported"`
	Total       uint64  `json:"total,omitempty"`
	Used        uint64  `json:"used,omitempty"`
	Free        uint64  `json:"free,omitempty"`
	Available   uint64  `json:"available,omitempty"`
	UsedPercent float64 `json:"used_percent,omitempty"`
}

// SpaceResult 是 GET /api/fs/space 的返回体（当前路径所在存储的容量）。
type SpaceResult struct {
	Path     string     `json:"path"`
	Mount    MountInfo  `json:"mount"`
	Space    SpaceUsage `json:"space"`
	Readonly bool       `json:"readonly"`
}
