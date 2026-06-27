package util

import (
	"errors"
	"strings"
)

// ErrNameInvalid 表示文件 / 目录名不合法（非法字符、保留名、越界形态等）。
var ErrNameInvalid = errors.New("invalid file name")

// maxNameLen 为单个文件名的最大字节长度（多数文件系统的 NAME_MAX）。
const maxNameLen = 255

// ValidateName 校验单个文件 / 目录名（非路径）。先应用所有平台共有的规则，
// 再交由平台特定的 validateNamePlatform 做额外校验（见 name_linux.go / name_windows.go）。
func ValidateName(name string) error {
	if name == "" {
		return ErrNameInvalid
	}
	if len(name) > maxNameLen {
		return ErrNameInvalid
	}
	if name == "." || name == ".." {
		return ErrNameInvalid
	}
	// 路径分隔符与 NUL 在所有平台都非法（防止拼出多级路径或截断）。
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\x00') {
		return ErrNameInvalid
	}
	return validateNamePlatform(name)
}
