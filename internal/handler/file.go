package handler

import (
	"errors"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"flist/internal/service"
	"flist/internal/util"
)

// FileHandler 处理只读文件操作接口。
type FileHandler struct {
	files *service.FileService
}

// NewFileHandler 构造文件处理器。
func NewFileHandler(files *service.FileService) *FileHandler {
	return &FileHandler{files: files}
}

// failFileErr 将服务层错误映射为统一错误响应。
func failFileErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, util.ErrPathTraversal):
		Fail(w, http.StatusBadRequest, CodePathTraversal, "path_traversal")
	case errors.Is(err, service.ErrNotFound):
		Fail(w, http.StatusNotFound, CodePathNotFound, "path_not_found")
	case errors.Is(err, service.ErrForbidden):
		Fail(w, http.StatusForbidden, CodePermissionDenied, "permission_denied")
	case errors.Is(err, service.ErrNotFile):
		Fail(w, http.StatusBadRequest, CodeNotAFile, "not_a_file")
	case errors.Is(err, service.ErrNotDir):
		Fail(w, http.StatusBadRequest, CodeNotADir, "not_a_dir")
	default:
		failInternal(w)
	}
}

// List 处理 GET /api/fs/list。
func (h *FileHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	apiPath := q.Get("path")
	if apiPath == "" {
		apiPath = "/"
	}

	opts := service.ListOptions{
		Sort:       normalizeSort(q.Get("sort")),
		Order:      normalizeOrder(q.Get("order")),
		ShowHidden: q.Get("show_hidden") == "true",
		Page:       atoiDefault(q.Get("page"), 1),
		PageSize:   atoiDefault(q.Get("page_size"), 0),
	}

	res, err := h.files.List(apiPath, opts)
	if err != nil {
		failFileErr(w, err)
		return
	}
	OK(w, res)
}

// Stat 处理 GET /api/fs/stat。
func (h *FileHandler) Stat(w http.ResponseWriter, r *http.Request) {
	apiPath := r.URL.Query().Get("path")
	if apiPath == "" {
		failBadRequest(w, "path required")
		return
	}
	info, err := h.files.Stat(apiPath)
	if err != nil {
		failFileErr(w, err)
		return
	}
	OK(w, info)
}

// Preview 处理 GET /api/fs/preview。
func (h *FileHandler) Preview(w http.ResponseWriter, r *http.Request) {
	apiPath := r.URL.Query().Get("path")
	if apiPath == "" {
		failBadRequest(w, "path required")
		return
	}
	res, err := h.files.PreviewText(apiPath)
	if err != nil {
		failFileErr(w, err)
		return
	}
	OK(w, res)
}

// Download 处理 GET /api/fs/download，经 http.ServeContent 支持 Range / ETag。
func (h *FileHandler) Download(w http.ResponseWriter, r *http.Request) {
	apiPath := r.URL.Query().Get("path")
	if apiPath == "" {
		failBadRequest(w, "path required")
		return
	}

	target, err := h.files.OpenForDownload(apiPath)
	if err != nil {
		failFileErr(w, err)
		return
	}
	defer target.File.Close()

	name := target.Info.Name()

	// Content-Type 由扩展名推导，缺省交给 ServeContent 嗅探。
	if ct := mime.TypeByExtension(strings.ToLower(path.Ext(name))); ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	// ETag 由 size + modTime 派生，支持 If-None-Match。
	etag := `"` + strconv.FormatInt(target.Info.Size(), 10) + "-" +
		strconv.FormatInt(target.ModTime.UnixNano(), 10) + `"`
	w.Header().Set("ETag", etag)

	// 默认内联（便于媒体直链），download=1 时强制附件下载。
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+urlEncode(name))

	// ServeContent 自动处理 Range / If-Range / If-None-Match / Last-Modified。
	http.ServeContent(w, r, name, target.ModTime, target.File)
}

func normalizeSort(s string) string {
	switch s {
	case "size", "mtime", "name":
		return s
	default:
		return "name"
	}
}

func normalizeOrder(s string) string {
	if s == "desc" {
		return "desc"
	}
	return "asc"
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// urlEncode 对文件名做 RFC 5987 兼容的百分号编码，仅保留安全字符。
func urlEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			const hex = "0123456789ABCDEF"
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}
