package middleware

import (
	"net/http"
	"path"
	"strings"
)

// PathGuard 兜底拦截请求中 path 类参数的明显越界形态（含 .. 或绝对路径）。
// 真正的安全边界由 handler 入口的 SafeResolve 保证，此处仅作前置防线。
// Phase 0 暂无 fs 接口，作为管道占位与未来阶段的统一拦截点。
func PathGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, key := range []string{"path", "src", "dst", "dir"} {
			for _, v := range r.URL.Query()[key] {
				if isSuspiciousPath(v) {
					writeError(w, http.StatusBadRequest, 2002, "path_traversal")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isSuspiciousPath 判断路径参数是否含越界形态。
func isSuspiciousPath(p string) bool {
	if p == "" {
		return false
	}
	// 以 / 开头是合法的「相对 root 的 API 路径」，故不拦截；
	// 但 Windows 盘符（C:\）与 UNC（\\）属于绝对路径，直接拒绝。
	if len(p) >= 2 && p[1] == ':' {
		return true // Windows 盘符
	}
	if strings.HasPrefix(p, `\\`) {
		return true // UNC
	}
	// 按「相对 root」清理（去掉前导分隔符再 Clean）：
	// 若结果仍以 .. 开头，说明试图越过 root（如 /../../etc），予以拦截；
	// 而 /docs/../a.txt 这类被同级目录抵消、未越界的 .. 则放行。
	rel := strings.TrimLeft(strings.ReplaceAll(p, `\`, "/"), "/")
	cleaned := path.Clean(rel)
	return cleaned == ".." || strings.HasPrefix(cleaned, "../")
}
