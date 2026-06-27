package handler

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"flist/internal/middleware"
	"flist/internal/model"
	"flist/internal/service"
	"flist/internal/storage"
)

// searchTimeout 限制单次搜索遍历的最长耗时。
const searchTimeout = 30 * time.Second

// FileHandler 处理文件操作接口（只读 + 写）。
type FileHandler struct {
	files  *service.FileService
	logger *slog.Logger
}

// NewFileHandler 构造文件处理器。logger 用于写操作审计，为 nil 时回落到默认 logger。
func NewFileHandler(files *service.FileService, logger *slog.Logger) *FileHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &FileHandler{files: files, logger: logger}
}

// failFileErr 将驱动 / 服务层错误映射为统一错误响应（错误词表统一在 storage 包）。
func failFileErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrTraversal):
		Fail(w, http.StatusBadRequest, CodePathTraversal, "path_traversal")
	case errors.Is(err, storage.ErrInvalidName):
		Fail(w, http.StatusBadRequest, CodeNameInvalid, "name_invalid")
	case errors.Is(err, storage.ErrNotFound):
		Fail(w, http.StatusNotFound, CodePathNotFound, "path_not_found")
	case errors.Is(err, storage.ErrForbidden):
		Fail(w, http.StatusForbidden, CodePermissionDenied, "permission_denied")
	case errors.Is(err, storage.ErrExists):
		Fail(w, http.StatusConflict, CodeFileExists, "file_exists")
	case errors.Is(err, storage.ErrDiskFull):
		Fail(w, http.StatusInsufficientStorage, CodeDiskFull, "disk_full")
	case errors.Is(err, storage.ErrNotFile):
		Fail(w, http.StatusBadRequest, CodeNotAFile, "not_a_file")
	case errors.Is(err, storage.ErrNotDir):
		Fail(w, http.StatusBadRequest, CodeNotADir, "not_a_dir")
	case errors.Is(err, storage.ErrNotSupported):
		Fail(w, http.StatusBadRequest, CodeBadRequest, "not_supported")
	case errors.Is(err, storage.ErrBadOp):
		Fail(w, http.StatusBadRequest, CodeBadRequest, "bad_request")
	default:
		failInternal(w)
	}
}

// audit 记录一条写操作审计日志（结构化 slog）。
func (h *FileHandler) audit(r *http.Request, action, target, result string) {
	username := ""
	if u := middleware.UserFromContext(r.Context()); u != nil {
		username = u.Username
	}
	h.logger.Info("audit",
		slog.String("action", action),
		slog.String("path", target),
		slog.String("user", username),
		slog.String("result", result),
		slog.String("request_id", middleware.RequestIDFromContext(r.Context())),
		slog.String("ip", middleware.ClientIP(r)),
	)
}

func auditResult(ok bool) string {
	if ok {
		return "ok"
	}
	return "fail"
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

	res, err := h.files.List(r.Context(), apiPath, opts)
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
	info, err := h.files.Stat(r.Context(), apiPath)
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
	res, err := h.files.PreviewText(r.Context(), apiPath)
	if err != nil {
		failFileErr(w, err)
		return
	}
	OK(w, res)
}

// Download 处理 GET /api/fs/download，经 http.ServeContent 支持 Range / ETag。
//
// 注意：http.ServeContent 要求 target.File 可 Seek。本地驱动返回 *os.File 天然满足；
// 远程驱动须以「Range GET 惰性 reader」或「落临时文件」等方式提供可 Seek 语义。
func (h *FileHandler) Download(w http.ResponseWriter, r *http.Request) {
	apiPath := r.URL.Query().Get("path")
	if apiPath == "" {
		failBadRequest(w, "path required")
		return
	}

	target, err := h.files.OpenForDownload(r.Context(), apiPath)
	if err != nil {
		failFileErr(w, err)
		return
	}
	defer target.File.Close()

	name := target.Info.Name
	modTime := target.Info.ModTime

	// Content-Type 由扩展名推导，缺省交给 ServeContent 嗅探。
	if ct := mime.TypeByExtension(strings.ToLower(path.Ext(name))); ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	// ETag 由 size + modTime 派生，支持 If-None-Match。
	etag := `"` + strconv.FormatInt(target.Info.Size, 10) + "-" +
		strconv.FormatInt(modTime.UnixNano(), 10) + `"`
	w.Header().Set("ETag", etag)

	// 默认内联（便于媒体直链），download=1 时强制附件下载。
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+urlEncode(name))

	// ServeContent 自动处理 Range / If-Range / If-None-Match / Last-Modified。
	http.ServeContent(w, r, name, modTime, target.File)
}

type pathRequest struct {
	Path string `json:"path"`
}

type pathResponse struct {
	Path string `json:"path"`
}

// Mkdir 处理 POST /api/fs/mkdir。
func (h *FileHandler) Mkdir(w http.ResponseWriter, r *http.Request) {
	var req pathRequest
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Path) == "" {
		failBadRequest(w, "path required")
		return
	}
	resPath, err := h.files.Mkdir(r.Context(), req.Path)
	if err != nil {
		h.audit(r, "mkdir", req.Path, "fail")
		failFileErr(w, err)
		return
	}
	h.audit(r, "mkdir", resPath, "ok")
	OK(w, pathResponse{Path: resPath})
}

// Touch 处理 POST /api/fs/touch。
func (h *FileHandler) Touch(w http.ResponseWriter, r *http.Request) {
	var req pathRequest
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Path) == "" {
		failBadRequest(w, "path required")
		return
	}
	resPath, err := h.files.Touch(r.Context(), req.Path)
	if err != nil {
		h.audit(r, "touch", req.Path, "fail")
		failFileErr(w, err)
		return
	}
	h.audit(r, "touch", resPath, "ok")
	OK(w, pathResponse{Path: resPath})
}

type moveRequest struct {
	Src        []string `json:"src"`
	Dst        string   `json:"dst"`
	AutoRename bool     `json:"auto_rename"`
}

type opResultsResponse struct {
	Results []model.OpResult `json:"results"`
}

// Move 处理 POST /api/fs/move（批量，尽力而为）。
func (h *FileHandler) Move(w http.ResponseWriter, r *http.Request) {
	var req moveRequest
	if err := decodeJSON(w, r, &req); err != nil || len(req.Src) == 0 || strings.TrimSpace(req.Dst) == "" {
		failBadRequest(w, "src and dst required")
		return
	}
	results := h.files.Move(r.Context(), req.Src, req.Dst, req.AutoRename)
	for _, res := range results {
		h.audit(r, "move", res.Src+" -> "+req.Dst, auditResult(res.OK))
	}
	OK(w, opResultsResponse{Results: results})
}

type copyRequest struct {
	Src        []string `json:"src"`
	Dst        string   `json:"dst"`
	AutoRename bool     `json:"auto_rename"`
}

// Copy 处理 POST /api/fs/copy（批量，尽力而为）。
func (h *FileHandler) Copy(w http.ResponseWriter, r *http.Request) {
	var req copyRequest
	if err := decodeJSON(w, r, &req); err != nil || len(req.Src) == 0 || strings.TrimSpace(req.Dst) == "" {
		failBadRequest(w, "src and dst required")
		return
	}
	results := h.files.Copy(r.Context(), req.Src, req.Dst, req.AutoRename)
	for _, res := range results {
		h.audit(r, "copy", res.Src+" -> "+req.Dst, auditResult(res.OK))
	}
	OK(w, opResultsResponse{Results: results})
}

type deleteRequest struct {
	Paths []string `json:"paths"`
}

// Delete 处理 DELETE /api/fs/delete（批量，尽力而为）。
func (h *FileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	var req deleteRequest
	if err := decodeJSON(w, r, &req); err != nil || len(req.Paths) == 0 {
		failBadRequest(w, "paths required")
		return
	}
	results := h.files.Delete(r.Context(), req.Paths)
	for _, res := range results {
		h.audit(r, "delete", res.Src, auditResult(res.OK))
	}
	OK(w, opResultsResponse{Results: results})
}

// Search 处理 GET /api/fs/search。
func (h *FileHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	base := q.Get("path")
	if base == "" {
		base = "/"
	}
	query := strings.TrimSpace(q.Get("q"))
	if query == "" {
		failBadRequest(w, "q required")
		return
	}
	opts := service.SearchOptions{
		Recursive:  q.Get("recursive") != "false", // 默认递归
		ShowHidden: q.Get("show_hidden") == "true",
		Limit:      atoiDefault(q.Get("limit"), 0),
	}

	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()

	res, err := h.files.Search(ctx, base, query, opts)
	if err != nil {
		failFileErr(w, err)
		return
	}
	OK(w, res)
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
