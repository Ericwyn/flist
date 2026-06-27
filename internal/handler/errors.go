package handler

import "net/http"

// 业务错误码（见 docs/2.phase0-详细设计.md §9，对齐 0.backend-design.md §11）。
const (
	CodeOK                 = 0
	CodeUnauthorized       = 1001
	CodeInvalidCredentials = 1002
	CodeAccountLocked      = 1003
	CodeWeakPassword       = 1004
	CodePathNotFound       = 2001
	CodePathTraversal      = 2002
	CodePermissionDenied   = 2003
	CodeNotAFile           = 2007
	CodeNotADir            = 2008
	CodeBadRequest         = 4000
	CodeInternalError      = 9001
	CodeRateLimited        = 9002
)

// 便捷错误响应封装。
func failBadRequest(w http.ResponseWriter, msg string) {
	Fail(w, http.StatusBadRequest, CodeBadRequest, msg)
}

func failUnauthorized(w http.ResponseWriter) {
	Fail(w, http.StatusUnauthorized, CodeUnauthorized, "unauthorized")
}

func failInternal(w http.ResponseWriter) {
	Fail(w, http.StatusInternalServerError, CodeInternalError, "internal_error")
}
