package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"flist/internal/middleware"
	"flist/internal/model"
	"flist/internal/service"
	"flist/internal/storage"
)

// FileOpHandler 处理异步文件操作任务（copy/move/delete）的发起、进度订阅与取消。
type FileOpHandler struct {
	ops    *service.FileOpService
	logger *slog.Logger
}

// NewFileOpHandler 构造异步文件操作处理器。
func NewFileOpHandler(ops *service.FileOpService, logger *slog.Logger) *FileOpHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &FileOpHandler{ops: ops, logger: logger}
}

// opScope 取当前用户作为任务隔离维度。
func opScope(r *http.Request) string {
	if u := middleware.UserFromContext(r.Context()); u != nil {
		return u.Username
	}
	return ""
}

type opCopyMoveRequest struct {
	Src        []string `json:"src"`
	Dst        string   `json:"dst"`
	AutoRename bool     `json:"auto_rename"`
}

type opDeleteRequest struct {
	Paths []string `json:"paths"`
}

// failFileOpErr 将 FileOpService 错误映射为统一错误响应。
func failFileOpErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrFileOpNotFound):
		Fail(w, http.StatusNotFound, CodeFileOpNotFound, "fileop_not_found")
	case errors.Is(err, service.ErrFileOpBusy):
		Fail(w, http.StatusServiceUnavailable, CodeFileOpBusy, "fileop_busy")
	case errors.Is(err, storage.ErrBadOp):
		Fail(w, http.StatusBadRequest, CodeBadRequest, "bad_request")
	default:
		failFileErr(w, err) // 复用文件错误词表（path_traversal / not_found 等）
	}
}

// Copy 发起异步复制任务，立即返回 task_id（HTTP 202）。
func (h *FileOpHandler) Copy(w http.ResponseWriter, r *http.Request) {
	var req opCopyMoveRequest
	if err := decodeJSON(w, r, &req); err != nil || len(req.Src) == 0 || strings.TrimSpace(req.Dst) == "" {
		failBadRequest(w, "src and dst required")
		return
	}
	res, err := h.ops.Start(r.Context(), model.FileOpCopy, opScope(r), req.Src, req.Dst, req.AutoRename)
	if err != nil {
		failFileOpErr(w, err)
		return
	}
	WriteJSON(w, http.StatusAccepted, Envelope{Code: 0, Message: "accepted", Data: res})
}

// Move 发起异步移动任务。
func (h *FileOpHandler) Move(w http.ResponseWriter, r *http.Request) {
	var req opCopyMoveRequest
	if err := decodeJSON(w, r, &req); err != nil || len(req.Src) == 0 || strings.TrimSpace(req.Dst) == "" {
		failBadRequest(w, "src and dst required")
		return
	}
	res, err := h.ops.Start(r.Context(), model.FileOpMove, opScope(r), req.Src, req.Dst, req.AutoRename)
	if err != nil {
		failFileOpErr(w, err)
		return
	}
	WriteJSON(w, http.StatusAccepted, Envelope{Code: 0, Message: "accepted", Data: res})
}

// Delete 发起异步删除任务。
func (h *FileOpHandler) Delete(w http.ResponseWriter, r *http.Request) {
	var req opDeleteRequest
	if err := decodeJSON(w, r, &req); err != nil || len(req.Paths) == 0 {
		failBadRequest(w, "paths required")
		return
	}
	res, err := h.ops.Start(r.Context(), model.FileOpDelete, opScope(r), req.Paths, "", false)
	if err != nil {
		failFileOpErr(w, err)
		return
	}
	WriteJSON(w, http.StatusAccepted, Envelope{Code: 0, Message: "accepted", Data: res})
}

type opCancelRequest struct {
	TaskID string `json:"task_id"`
}

// Cancel 取消一个任务（幂等）。
func (h *FileOpHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	var req opCancelRequest
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.TaskID) == "" {
		failBadRequest(w, "task_id required")
		return
	}
	h.ops.Cancel(req.TaskID, opScope(r))
	OK(w, map[string]any{"task_id": req.TaskID, "canceled": true})
}

// Progress 以 SSE 订阅任务进度。任务不存在返回 404。
//
// 事件格式：data: {"type":"...","snapshot":{...},...}\n\n
// 终态事件 type=finished 后关闭流。任务已结束时立即推送一条 finished 后关闭。
func (h *FileOpHandler) Progress(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("id")
	if taskID == "" {
		failBadRequest(w, "id required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		failInternal(w)
		return
	}

	ch, snap, unsub := h.ops.Subscribe(taskID, opScope(r))
	if ch == nil {
		Fail(w, http.StatusNotFound, CodeFileOpNotFound, "fileop_not_found")
		return
	}
	defer unsub()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // 关闭 nginx 缓冲，确保实时推送
	w.WriteHeader(http.StatusOK)

	// 先推送一条当前快照，便于客户端立即渲染（含断线重连恢复）。
	writeSSE(w, service.FileOpEvent{Type: "snapshot", Snapshot: snap})
	flusher.Flush()

	// 客户端断开 / 上下文取消则退出循环。
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return // 任务结束，订阅 channel 已关闭
			}
			writeSSE(w, ev)
			flusher.Flush()
			if ev.Type == "finished" {
				return
			}
		case <-time.After(15 * time.Second):
			// 心跳注释，防止代理超时断开长连接。
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// writeSSE 写出一条 SSE 事件。
func writeSSE(w http.ResponseWriter, ev interface{}) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}
