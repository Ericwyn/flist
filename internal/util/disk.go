package util

// Usage 返回 path 所在文件系统的总容量与可用字节数。
// 跨平台 build tag 实现：Linux 用 unix.Statfs，Windows 用 GetDiskFreeSpaceEx。
//
// free 取「非特权用户实际可写」的可用空间（Linux 的 Bavail / Windows 调用方配额可用），
// 比「文件系统空闲块」更贴近上传 / 复制前的真实可用判断。
func Usage(path string) (total, free uint64, err error) {
	return usage(path)
}
