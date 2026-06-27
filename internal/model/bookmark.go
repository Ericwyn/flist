package model

import "time"

// Bookmark 对应 bookmarks 表，是用户收藏的目录快捷入口。
// Path 存「相对 root 的 API 路径」，root 迁移不失效、天然不越界（见 0.backend-design.md 6.3）。
type Bookmark struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	Valid     bool      `json:"valid"` // 运行时计算：目标仍存在且为目录；不入库
}
