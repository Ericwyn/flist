//go:build windows

package util

import "golang.org/x/sys/windows"

// usage 用 GetDiskFreeSpaceEx 获取磁盘用量。
// freeBytesAvailable 为调用方配额下的可用字节（贴近实际可写空间），
// totalNumberOfBytes 为该卷总字节数。
func usage(path string) (total, free uint64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeBytesAvailable, &totalNumberOfBytes, &totalNumberOfFreeBytes); err != nil {
		return 0, 0, err
	}
	return totalNumberOfBytes, freeBytesAvailable, nil
}
