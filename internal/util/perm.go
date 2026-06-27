package util

import (
	"io/fs"
	"strconv"
)

// FormatMode 将文件权限位格式化为 4 位八进制字符串（如 "0644"）。
// 仅取低 9 位权限位，忽略类型与特殊位。
func FormatMode(mode fs.FileMode) string {
	perm := mode.Perm() // 低 9 位
	s := strconv.FormatUint(uint64(perm), 8)
	// 补足为 4 位（前导 0），与设计文档示例 "0644" 一致。
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}
