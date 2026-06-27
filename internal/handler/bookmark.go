package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"flist/internal/middleware"
	"flist/internal/model"
	"flist/internal/service"
	"flist/internal/storage"
	"flist/internal/store"

	"github.com/go-chi/chi/v5"
)

// BookmarkHandler 处理收藏夹接口。
type BookmarkHandler struct {
	bookmarks *service.BookmarkService
	logger    *slog.Logger
}

// NewBookmarkHandler 构造收藏夹处理器。
func NewBookmarkHandler(bookmarks *service.BookmarkService, logger *slog.Logger) *BookmarkHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &BookmarkHandler{bookmarks: bookmarks, logger: logger}
}

// failBookmarkErr 将收藏夹服务 / 驱动层错误映射为统一错误响应。
func failBookmarkErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrBookmarkExists):
		Fail(w, http.StatusConflict, CodeBookmarkExists, "bookmark_exists")
	case errors.Is(err, service.ErrBookmarkNotFound):
		Fail(w, http.StatusNotFound, CodeBookmarkNotFound, "bookmark_not_found")
	case errors.Is(err, storage.ErrNotDir):
		Fail(w, http.StatusBadRequest, CodeNotADir, "not_a_dir")
	case errors.Is(err, storage.ErrNotFound):
		Fail(w, http.StatusNotFound, CodePathNotFound, "path_not_found")
	case errors.Is(err, storage.ErrTraversal):
		Fail(w, http.StatusBadRequest, CodePathTraversal, "path_traversal")
	case errors.Is(err, storage.ErrInvalidName):
		Fail(w, http.StatusBadRequest, CodeNameInvalid, "name_invalid")
	default:
		failInternal(w)
	}
}

// userID 从上下文取出当前用户 ID（认证中间件保证存在）。
func (h *BookmarkHandler) userID(r *http.Request) (int64, bool) {
	u := middleware.UserFromContext(r.Context())
	if u == nil {
		return 0, false
	}
	return u.ID, true
}

type bookmarkResponse struct {
	Bookmarks []model.Bookmark `json:"bookmarks"`
}

// List 处理 GET /api/bookmarks。
func (h *BookmarkHandler) List(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(r)
	if !ok {
		failUnauthorized(w)
		return
	}
	items, err := h.bookmarks.List(r.Context(), uid)
	if err != nil {
		failInternal(w)
		return
	}
	OK(w, bookmarkResponse{Bookmarks: items})
}

type createBookmarkRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Create 处理 POST /api/bookmarks。
func (h *BookmarkHandler) Create(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(r)
	if !ok {
		failUnauthorized(w)
		return
	}
	var req createBookmarkRequest
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Path) == "" {
		failBadRequest(w, "path required")
		return
	}
	bm, err := h.bookmarks.Create(r.Context(), uid, req.Name, req.Path)
	if err != nil {
		failBookmarkErr(w, err)
		return
	}
	OK(w, bm)
}

type updateBookmarkRequest struct {
	Name string `json:"name"`
}

// Update 处理 PUT /api/bookmarks/{id}（重命名）。
func (h *BookmarkHandler) Update(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(r)
	if !ok {
		failUnauthorized(w)
		return
	}
	id, err := parseID(r)
	if err != nil {
		failBadRequest(w, "invalid id")
		return
	}
	var req updateBookmarkRequest
	if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		failBadRequest(w, "name required")
		return
	}
	if err := h.bookmarks.Update(r.Context(), uid, id, req.Name); err != nil {
		failBookmarkErr(w, err)
		return
	}
	OK(w, nil)
}

// Delete 处理 DELETE /api/bookmarks/{id}。
func (h *BookmarkHandler) Delete(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(r)
	if !ok {
		failUnauthorized(w)
		return
	}
	id, err := parseID(r)
	if err != nil {
		failBadRequest(w, "invalid id")
		return
	}
	if err := h.bookmarks.Delete(r.Context(), uid, id); err != nil {
		failBookmarkErr(w, err)
		return
	}
	OK(w, nil)
}

type reorderRequest struct {
	Orders []struct {
		ID        int64 `json:"id"`
		SortOrder int   `json:"sort_order"`
	} `json:"orders"`
}

// Reorder 处理 PUT /api/bookmarks/reorder。
func (h *BookmarkHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(r)
	if !ok {
		failUnauthorized(w)
		return
	}
	var req reorderRequest
	if err := decodeJSON(w, r, &req); err != nil || len(req.Orders) == 0 {
		failBadRequest(w, "orders required")
		return
	}
	orders := make([]store.BookmarkOrder, 0, len(req.Orders))
	for _, o := range req.Orders {
		orders = append(orders, store.BookmarkOrder{ID: o.ID, SortOrder: o.SortOrder})
	}
	if err := h.bookmarks.Reorder(r.Context(), uid, orders); err != nil {
		failInternal(w)
		return
	}
	OK(w, nil)
}

// parseID 从 chi URL 参数解析整数 id。
func parseID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}
