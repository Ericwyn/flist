//go:build windows

package util

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isCrossDevice 判断 rename 错误是否为跨卷（ERROR_NOT_SAME_DEVICE）。
func isCrossDevice(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}
