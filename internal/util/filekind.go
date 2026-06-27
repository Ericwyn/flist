package util

import (
	"bytes"
	"path"
	"strings"
)

// Kind 表示供前端展示决策的文件大类。后端 list/stat 的 type 仍只区分 file/dir，
// kind 仅用于预览与图标选择。
type Kind string

const (
	KindFolder  Kind = "folder"
	KindText    Kind = "text"
	KindImage   Kind = "image"
	KindVideo   Kind = "video"
	KindAudio   Kind = "audio"
	KindUnknown Kind = "unknown"
)

// 扩展名集合（均为小写，含前导点）。
var (
	textExts = map[string]bool{
		".txt": true, ".md": true, ".markdown": true, ".log": true, ".csv": true,
		".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
		".conf": true, ".cfg": true, ".xml": true, ".html": true, ".htm": true,
		".css": true, ".scss": true, ".less": true, ".js": true, ".jsx": true,
		".ts": true, ".tsx": true, ".go": true, ".py": true, ".rb": true,
		".rs": true, ".java": true, ".c": true, ".h": true, ".cpp": true,
		".hpp": true, ".cc": true, ".sh": true, ".bash": true, ".zsh": true,
		".sql": true, ".env": true, ".gitignore": true, ".dockerfile": true,
		".properties": true, ".gradle": true, ".kt": true, ".swift": true,
		".php": true, ".pl": true, ".lua": true, ".r": true, ".m": true,
		".tex": true, ".rst": true, ".adoc": true, ".vue": true, ".svelte": true,
	}
	imageExts = map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
		".bmp": true, ".svg": true, ".ico": true, ".avif": true,
	}
	videoExts = map[string]bool{
		".mp4": true, ".webm": true, ".mkv": true, ".mov": true, ".avi": true,
		".m4v": true, ".mpeg": true, ".mpg": true, ".flv": true, ".wmv": true,
	}
	audioExts = map[string]bool{
		".mp3": true, ".wav": true, ".ogg": true, ".flac": true, ".aac": true,
		".m4a": true, ".opus": true, ".wma": true,
	}
)

// DetectKind 按文件名扩展名推导大类（不读文件内容）。
func DetectKind(name string) Kind {
	ext := strings.ToLower(path.Ext(name))
	switch {
	case textExts[ext]:
		return KindText
	case imageExts[ext]:
		return KindImage
	case videoExts[ext]:
		return KindVideo
	case audioExts[ext]:
		return KindAudio
	default:
		return KindUnknown
	}
}

// IsTextExt 判断扩展名是否属于已知文本类型。
func IsTextExt(name string) bool {
	return textExts[strings.ToLower(path.Ext(name))]
}

// SniffText 对内容前缀做二进制嗅探：含 NUL 字节视为二进制（非文本）。
// 空内容视为文本。
func SniffText(sample []byte) bool {
	return !bytes.Contains(sample, []byte{0})
}
