package util

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrPathTraversal 表示解析后的真实路径逃逸出 root 范围。
var ErrPathTraversal = errors.New("path traversal detected")

// ToAPIPath 将 OS 本地路径转换为以 / 分隔的 API 路径。
func ToAPIPath(p string) string {
	return filepath.ToSlash(p)
}

// FromAPIPath 将以 / 分隔的 API 路径转换为 OS 本地路径片段。
func FromAPIPath(p string) string {
	return filepath.FromSlash(p)
}

// CleanAPIPath 归一化 API 路径：补前导 /，消解 . 与 ..，越根的 .. 被钳制在根。
// 返回值总是以 / 开头且不以 / 结尾（根除外，返回 "/"）。
func CleanAPIPath(apiPath string) string {
	cleaned := path.Clean("/" + strings.TrimLeft(apiPath, "/"))
	return cleaned
}

// SafeResolve 将相对 root 的 API 路径解析为安全的 OS 本地绝对路径。
//
// 算法（见 0.backend-design.md 9.1）：
//  1. 清理 API 路径，消解 .. / . 与越根尝试
//  2. 与 rootReal 拼接得到候选本地路径
//  3. 解析符号链接得真实路径；目标不存在时（创建类操作）退化为「父目录解析 + basename」
//  4. 前缀检查：真实路径必须等于 rootReal 或以 rootReal+分隔符 开头
//
// 关键：前缀检查必须在符号链接解析之后，否则可借 symlink 逃逸。
// rootReal 必须是调用方在启动时通过 filepath.Abs + EvalSymlinks 解析并缓存的绝对真实路径。
func SafeResolve(rootReal, apiPath string) (string, error) {
	cleaned := CleanAPIPath(apiPath)
	full := filepath.Join(rootReal, FromAPIPath(cleaned))

	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		// 目标尚不存在（mkdir/touch/upload/copy/move 的目的地）：
		// 向上回溯到最近的存在祖先并解析，再拼回不存在的后缀。
		// 因 rootReal 必然存在，回溯最差止于 rootReal，不会越过它。
		resolved, err = resolveNonexistent(full)
		if err != nil {
			return "", err
		}
	}

	if !withinRoot(rootReal, resolved) {
		return "", ErrPathTraversal
	}
	return resolved, nil
}

// resolveNonexistent 处理目标路径尚不存在的情形：向上回溯到最近的存在祖先，
// 对其解析符号链接，再把沿途不存在的后缀原样拼接回去。后缀不存在故无 symlink 顾虑。
func resolveNonexistent(full string) (string, error) {
	var suffix []string
	cur := full
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", os.ErrNotExist // 抵达文件系统根仍未找到存在祖先
		}
		suffix = append([]string{filepath.Base(cur)}, suffix...)
		realParent, err := filepath.EvalSymlinks(parent)
		if err == nil {
			return filepath.Join(append([]string{realParent}, suffix...)...), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		cur = parent
	}
}

// withinRoot 判断 target 是否等于 root 或位于 root 之内。
func withinRoot(root, target string) bool {
	if target == root {
		return true
	}
	prefix := root
	if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
		prefix += string(os.PathSeparator)
	}
	return strings.HasPrefix(target, prefix)
}

// ResolveRoot 在启动时将用户提供的 root 标准化为绝对真实路径。
// 目录必须已存在，否则返回错误（避免误把不存在的路径当作可管理根）。
func ResolveRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("root is not a directory: " + abs)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return real, nil
}
