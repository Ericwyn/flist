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
	Base      string      `json:"base"`      // 搜索起点 API 路径
	Items     []SearchHit `json:"items"`
	Truncated bool        `json:"truncated"` // 命中达到上限被截断
	TimedOut  bool        `json:"timed_out"` // 遍历超时提前结束
}
