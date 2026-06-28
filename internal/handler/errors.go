package handler

import "net/http"

// 业务错误码（见 docs/2.phase0-详细设计.md §9，对齐 0.backend-design.md §11）。
const (
	CodeOK                 = 0
	CodeUnauthorized       = 1001
	CodeInvalidCredentials = 1002
	CodeAccountLocked      = 1003
	CodeWeakPassword       = 1004
	CodeInvalidUsername    = 1005
	CodeUsernameTaken      = 1006
	CodeTwoFactorRequired  = 1007
	CodeInvalidTOTP        = 1008
	CodeTOTPAlreadyEnabled = 1009
	CodeTOTPNotEnabled     = 1010
	CodePathNotFound       = 2001
	CodePathTraversal      = 2002
	CodePermissionDenied   = 2003
	CodeFileExists         = 2004
	CodeDiskFull           = 2005
	CodeNameInvalid        = 2006
	CodeNotAFile           = 2007
	CodeNotADir            = 2008
	CodeUploadTooLarge     = 2009
	CodeUploadNotFound     = 2010
	CodeUploadIncomplete   = 2011
	CodeFileModified       = 2012
	CodeUnsupportedMedia   = 2013
	CodeFileTooLarge       = 2014
	CodeReadonlyStorage    = 2015
	CodeInvalidRevision    = 2016
	CodeBookmarkExists     = 3001
	CodeBookmarkNotFound   = 3002
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
