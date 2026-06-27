//go:build linux

package util

// validateNamePlatform 在 Linux 上无额外限制：公共规则（非空、长度、无 / 与 NUL、非 . / ..）已足够。
func validateNamePlatform(_ string) error {
	return nil
}
