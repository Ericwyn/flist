//go:build windows

package util

import (
	"io/fs"
	"strings"
	"syscall"
)

// IsHidden 在 Windows 上读取 FILE_ATTRIBUTE_HIDDEN 属性判断隐藏文件；
// 无法取得底层属性时降级为 "." 前缀判断。
func IsHidden(name string, info fs.FileInfo) bool {
	if info != nil {
		if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
			if data.FileAttributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0 {
				return true
			}
			return false
		}
	}
	return strings.HasPrefix(name, ".")
}
