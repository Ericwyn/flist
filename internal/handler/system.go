package handler

import (
	"net/http"

	"flist/internal/model"
	"flist/internal/service"
)

// SystemHandler 处理系统相关接口。
type SystemHandler struct {
	files *service.FileService
}

// NewSystemHandler 构造系统处理器。files 用于查询磁盘用量（Phase 6）。
func NewSystemHandler(files *service.FileService) *SystemHandler {
	return &SystemHandler{files: files}
}

// Health 处理 GET /api/system/health，无需认证。
func (h *SystemHandler) Health(w http.ResponseWriter, r *http.Request) {
	OK(w, map[string]any{"status": "ok"})
}

// Info 处理 GET /api/system/info，返回 root 所在文件系统的磁盘用量（Phase 6）。
func (h *SystemHandler) Info(w http.ResponseWriter, r *http.Request) {
	total, free, err := h.files.Usage(r.Context(), "/")
	if err != nil {
		failFileErr(w, err)
		return
	}
	var used uint64
	if total > free {
		used = total - free
	}
	OK(w, model.SystemInfo{
		DiskTotal: total,
		DiskUsed:  used,
		DiskFree:  free,
	})
}
