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
	// compressedExts 是「已压缩」的扩展名集合：这些格式内部已做熵编码，
	// 再用 deflate 二次压缩几乎无收益却白耗 CPU，打包时应以 Store 直存。
	// 注意 .wav 不在此列（PCM 无损可压），故不与 audioExts 简单复用。
	compressedExts = map[string]bool{
		// 归档 / 压缩容器
		".zip": true, ".gz": true, ".tgz": true, ".bz2": true, ".tbz": true,
		".xz": true, ".txz": true, ".7z": true, ".rar": true, ".zst": true,
		".lz": true, ".lzma": true, ".cab": true, ".arj": true,
		// 图片（已压缩）
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
		".avif": true, ".heic": true, ".heif": true, ".jxl": true,
		// 视频（已压缩）
		".mp4": true, ".m4v": true, ".mkv": true, ".mov": true, ".webm": true,
		".avi": true, ".flv": true, ".wmv": true, ".mpeg": true, ".mpg": true,
		".ts": true, ".m2ts": true,
		// 音频（已压缩）
		".mp3": true, ".aac": true, ".m4a": true, ".ogg": true, ".oga": true,
		".opus": true, ".flac": true, ".wma": true,
		// zip 容器型文档 / 包
		".docx": true, ".xlsx": true, ".pptx": true, ".odt": true, ".ods": true,
		".odp": true, ".epub": true, ".jar": true, ".war": true, ".apk": true,
		// 其他已压缩
		".pdf": true, ".dmg": true,
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

// IsCompressedExt 判断文件名扩展名是否属于「已压缩」格式（媒体 / 归档 / zip 容器文档等）。
// 打包下载据此选择 zip 压缩方式：已压缩用 Store 直存（不二次压缩白耗 CPU），其余用 Deflate。
func IsCompressedExt(name string) bool {
	return compressedExts[strings.ToLower(path.Ext(name))]
}

// SniffText 对内容前缀做二进制嗅探：含 NUL 字节视为二进制（非文本）。
// 空内容视为文本。
func SniffText(sample []byte) bool {
	return !bytes.Contains(sample, []byte{0})
}
