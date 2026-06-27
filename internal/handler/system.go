package handler

import "net/http"

// SystemHandler 处理系统相关接口。
type SystemHandler struct{}

// NewSystemHandler 构造系统处理器。
func NewSystemHandler() *SystemHandler {
	return &SystemHandler{}
}

// Health 处理 GET /api/system/health，无需认证。
func (h *SystemHandler) Health(w http.ResponseWriter, r *http.Request) {
	OK(w, map[string]any{"status": "ok"})
}
