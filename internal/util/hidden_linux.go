//go:build linux

package util

import (
	"io/fs"
	"strings"
)

// IsHidden 在 Linux 上以 "." 前缀判断隐藏文件。info 参数为接口统一保留，未使用。
func IsHidden(name string, _ fs.FileInfo) bool {
	return strings.HasPrefix(name, ".")
}
