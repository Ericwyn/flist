//go:build linux

package util

import "golang.org/x/sys/unix"

// usage 用 statfs 获取磁盘用量。total = 总块数 * 块大小；
// free = 非特权用户可用块数（Bavail）* 块大小。
func usage(path string) (total, free uint64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize)
	total = st.Blocks * bsize
	free = st.Bavail * bsize
	return total, free, nil
}
