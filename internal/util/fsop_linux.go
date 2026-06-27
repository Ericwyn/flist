//go:build linux

package util

import (
	"errors"

	"golang.org/x/sys/unix"
)

// isCrossDevice 判断 rename 错误是否为跨分区（EXDEV）。
func isCrossDevice(err error) bool {
	return errors.Is(err, unix.EXDEV)
}
