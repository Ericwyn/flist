package handler

import (
	"net/http"
	"runtime"
	"time"

	"flist/internal/model"
	"flist/internal/service/device"
)

// SystemHandler 处理系统相关接口。
type SystemHandler struct {
	devices device.Service
}

// NewSystemHandler 构造系统处理器。devices 用于上报设备管理能力（可为 nil）。
func NewSystemHandler(devices device.Service) *SystemHandler {
	return &SystemHandler{devices: devices}
}

// Health 处理 GET /api/system/health，无需认证。
func (h *SystemHandler) Health(w http.ResponseWriter, r *http.Request) {
	OK(w, map[string]any{"status": "ok"})
}

// Info 处理 GET /api/system/info，返回纯系统级信息。
//
// 磁盘容量已移出本接口（见「文件编辑与路径级容量优化」），改由
// GET /api/fs/space?path=... 按当前路径所在存储返回。
func (h *SystemHandler) Info(w http.ResponseWriter, r *http.Request) {
	OK(w, model.SystemInfo{
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		ServerTime:       time.Now(),
		DeviceManagement: h.devices != nil && h.devices.Supported(),
	})
}
