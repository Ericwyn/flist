package model

import "time"

// 文件操作任务类型（异步 copy/move/delete）。
const (
	FileOpCopy   = "copy"
	FileOpMove   = "move"
	FileOpDelete = "delete"
)

// 文件操作任务状态。
const (
	FileOpQueued  = "queued"  // 已入队，等待全局串行槽
	FileOpRunning = "running" // 执行中
	FileOpDone    = "done"    // 全部完成（可能有单项失败，看 results）
	FileOpCanceled = "canceled" // 用户取消
	FileOpFailed   = "error"   // 整体失败（如目标非法）
)

// FileOpStartResult 是 POST /api/fs/op/{copy,move,delete} 立即返回的任务句柄。
type FileOpStartResult struct {
	TaskID      string `json:"task_id"`
	Op          string `json:"op"`
	TotalItems  int    `json:"total_items"`
	TotalBytes  int64  `json:"total_bytes"` // 估算总量，0 表示未知
}

// FileOpSnapshot 是 SSE 推送的任务快照（任一事件都携带当前快照字段，
// 便于客户端断线重连后用单条事件恢复 UI）。
type FileOpSnapshot struct {
	Op          string        `json:"op"`
	Status      string        `json:"status"`
	TotalItems  int           `json:"total_items"`
	TotalBytes  int64         `json:"total_bytes"`
	DoneItems   int           `json:"done_items"`
	DoneBytes   int64         `json:"done_bytes"`   // 已完成项的累计字节
	CurIndex    int           `json:"cur_index"`    // 当前处理的项索引，-1 表示无
	CurName     string        `json:"cur_name"`     // 当前项名（basename）
	CurSize     int64         `json:"cur_size"`     // 当前项总字节，0 表示未知
	CurCopied   int64         `json:"cur_copied"`   // 当前项已复制字节
	Speed       int64         `json:"speed"`        // 当前项瞬时速率 bytes/s（服务端 EMA 平滑）
	Results     []OpResult    `json:"results,omitempty"`  // 仅 task_done 携带
	Error       string        `json:"error,omitempty"`    // 整体失败时的错误码名
	StartedAt   time.Time     `json:"started_at"`
}
