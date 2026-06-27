//go:build windows

package util

import "strings"

// windowsReserved 是 Windows 的保留设备名（不分大小写）。
var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// validateNamePlatform 施加 Windows 特定限制：禁危险字符、保留名、不可空格 / 点结尾。
func validateNamePlatform(name string) error {
	// 禁止字符：< > : " \ | ? * 以及控制字符（< 0x20）。
	if strings.ContainsAny(name, `<>:"\|?*`) {
		return ErrNameInvalid
	}
	for _, r := range name {
		if r < 0x20 {
			return ErrNameInvalid
		}
	}

	// 不可以空格或点结尾。
	last := name[len(name)-1]
	if last == ' ' || last == '.' {
		return ErrNameInvalid
	}

	// 保留名校验：取首个点之前的基名（如 CON.txt 仍非法）。
	base := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		base = name[:i]
	}
	if windowsReserved[strings.ToUpper(base)] {
		return ErrNameInvalid
	}
	return nil
}
