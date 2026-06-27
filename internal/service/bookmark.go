// Package service 的收藏夹编排：CRUD + 排序 + 路径校验 + 失效检测。
//
// 收藏夹存「相对 root 的 API 路径」（root 迁移不失效、天然不越界）。BookmarkService
// 持有 storage.Backend 用于：创建时校验目标确为已存在目录；列表时逐条 Stat 填 Valid。
package service

import (
	"context"
	"errors"
	"path"
	"strings"

	"flist/internal/model"
	"flist/internal/storage"
	"flist/internal/store"
	"flist/internal/util"
)

// 收藏夹服务层错误，handler 据此映射错误码。
var (
	ErrBookmarkExists   = store.ErrBookmarkExists // 3001
	ErrBookmarkNotFound = errors.New("bookmark not found") // 3002
)

const maxBookmarkNameLen = 255

// BookmarkService 提供收藏夹编排。
type BookmarkService struct {
	store   *store.Store
	backend storage.Backend
}

// NewBookmarkService 构造收藏夹服务。backend 用于路径校验与失效检测。
func NewBookmarkService(st *store.Store, backend storage.Backend) *BookmarkService {
	return &BookmarkService{store: st, backend: backend}
}

// List 返回用户全部收藏，逐条经 backend.Stat 填充 Valid（目标仍存在且为目录）。
func (s *BookmarkService) List(ctx context.Context, userID int64) ([]model.Bookmark, error) {
	items, err := s.store.ListBookmarks(userID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Valid = s.pathIsDir(ctx, items[i].Path)
	}
	return items, nil
}

// Create 收藏一个目录。校验目标为已存在目录；name 缺省回落为 basename；
// 同一路径重复收藏返回 ErrBookmarkExists。
func (s *BookmarkService) Create(ctx context.Context, userID int64, name, apiPath string) (*model.Bookmark, error) {
	cleaned := util.CleanAPIPath(apiPath)

	info, err := s.backend.Stat(ctx, cleaned)
	if err != nil {
		return nil, err // NotFound / Traversal 等原样上抛，由 handler 映射
	}
	if info.Type != model.TypeDir {
		return nil, storage.ErrNotDir // 仅允许收藏目录
	}

	name = normalizeBookmarkName(name, cleaned)

	sortOrder, err := s.store.MaxBookmarkSort(userID)
	if err != nil {
		return nil, err
	}
	id, err := s.store.CreateBookmark(userID, name, cleaned, sortOrder+1)
	if err != nil {
		return nil, err // ErrBookmarkExists 原样上抛
	}
	created, err := s.store.GetBookmark(id, userID)
	if err != nil {
		return nil, err
	}
	created.Valid = true
	return created, nil
}

// Update 重命名收藏。不存在 / 非本人返回 ErrBookmarkNotFound。
func (s *BookmarkService) Update(_ context.Context, userID, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return storage.ErrInvalidName
	}
	if len(name) > maxBookmarkNameLen {
		return storage.ErrInvalidName
	}
	err := s.store.UpdateBookmarkName(id, userID, name)
	if errors.Is(err, store.ErrNotFound) {
		return ErrBookmarkNotFound
	}
	return err
}

// Delete 删除收藏。不存在 / 非本人返回 ErrBookmarkNotFound。
func (s *BookmarkService) Delete(_ context.Context, userID, id int64) error {
	err := s.store.DeleteBookmark(id, userID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrBookmarkNotFound
	}
	return err
}

// Reorder 批量调整排序。仅作用于归属本人的记录。
func (s *BookmarkService) Reorder(_ context.Context, userID int64, orders []store.BookmarkOrder) error {
	return s.store.ReorderBookmarks(userID, orders)
}

// pathIsDir 判断收藏路径当前是否仍指向一个存在的目录。
func (s *BookmarkService) pathIsDir(ctx context.Context, apiPath string) bool {
	info, err := s.backend.Stat(ctx, apiPath)
	return err == nil && info.Type == model.TypeDir
}

// normalizeBookmarkName 在 name 为空时回落为路径 basename（根回落为「我的文件」），
// 并截断到最大长度。
func normalizeBookmarkName(name, cleaned string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		if cleaned == "/" {
			name = "我的文件"
		} else {
			name = path.Base(cleaned)
		}
	}
	if len(name) > maxBookmarkNameLen {
		name = name[:maxBookmarkNameLen]
	}
	return name
}
