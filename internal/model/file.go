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
