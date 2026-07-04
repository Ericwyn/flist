package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"flist/internal/model"
	"flist/internal/service/device"
)

// DeviceHandler 处理设备管理接口（列举 / 挂载 / 卸载）。
type DeviceHandler struct {
	svc    device.Service
	logger *slog.Logger
}

// NewDeviceHandler 构造设备处理器。svc 在非 Linux / 命令缺失时为不支持实现。
func NewDeviceHandler(svc device.Service, logger *slog.Logger) *DeviceHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DeviceHandler{svc: svc, logger: logger}
}

// toModel 把 service 层 Device 转为对外 model.Device（拼出 drive_path）。
func toModel(d device.Device) model.Device {
	return model.Device{
		Device:     d.Device,
		ID:         d.ID,
		Name:       d.Name,
		Label:      d.Label,
		FSType:     d.FSType,
		Size:       d.Size,
		Mounted:    d.Mounted,
		Mountpoint: d.Mountpoint,
		DrivePath:  "/drive/" + d.ID,
		Removable:  d.Removable,
		Readonly:   d.Readonly,
		System:     d.System,
	}
}

// failDeviceErr 将设备服务错误映射为统一错误响应。
func failDeviceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, device.ErrUnsupported):
		Fail(w, http.StatusNotImplemented, CodeDeviceUnsupported, "device_management_unsupported")
	case errors.Is(err, device.ErrNotFound):
		Fail(w, http.StatusNotFound, CodeDeviceNotFound, "device_not_found")
	case errors.Is(err, device.ErrBusy):
		Fail(w, http.StatusConflict, CodeDeviceBusy, "device_busy")
	case errors.Is(err, device.ErrForbidden):
		Fail(w, http.StatusForbidden, CodeDeviceForbidden, "device_mount_forbidden")
	case errors.Is(err, device.ErrInvalid):
		Fail(w, http.StatusBadRequest, CodeBadRequest, "invalid_device")
	case errors.Is(err, device.ErrCommand):
		Fail(w, http.StatusInternalServerError, CodeDeviceCommand, "device_command_failed")
	default:
		failInternal(w)
	}
}

// List 处理 GET /api/devices。
func (h *DeviceHandler) List(w http.ResponseWriter, r *http.Request) {
	if !h.svc.Supported() {
		// 平台不支持：返回 supported=false 空列表，前端据此隐藏入口（非错误）。
		OK(w, model.DeviceListResult{Supported: false, Devices: []model.Device{}})
		return
	}
	devs, err := h.svc.List(r.Context())
	if err != nil {
		failDeviceErr(w, err)
		return
	}
	items := make([]model.Device, 0, len(devs))
	for _, d := range devs {
		items = append(items, toModel(d))
	}
	OK(w, model.DeviceListResult{Supported: true, Devices: items})
}

type deviceRequest struct {
	Device string `json:"device"`
}

// Mount 处理 POST /api/devices/mount。
func (h *DeviceHandler) Mount(w http.ResponseWriter, r *http.Request) {
	dev, ok := h.decodeDevice(w, r)
	if !ok {
		return
	}
	res, err := h.svc.Mount(r.Context(), dev)
	if err != nil {
		failDeviceErr(w, err)
		return
	}
	OK(w, toModel(*res))
}

// Unmount 处理 POST /api/devices/unmount。
func (h *DeviceHandler) Unmount(w http.ResponseWriter, r *http.Request) {
	dev, ok := h.decodeDevice(w, r)
	if !ok {
		return
	}
	res, err := h.svc.Unmount(r.Context(), dev)
	if err != nil {
		failDeviceErr(w, err)
		return
	}
	OK(w, toModel(*res))
}

// decodeDevice 解析并初步校验 device 请求体（须以 /dev/ 开头；真正校验在 service 端重新 lsblk）。
func (h *DeviceHandler) decodeDevice(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req deviceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "device required")
		return "", false
	}
	dev := strings.TrimSpace(req.Device)
	if dev == "" || !strings.HasPrefix(dev, "/dev/") {
		failBadRequest(w, "invalid device")
		return "", false
	}
	return dev, true
}
