package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"flist/internal/model"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrBookmarkExists 表示同一用户已收藏该路径（唯一索引冲突）。
var ErrBookmarkExists = errors.New("bookmark exists")

// BookmarkOrder 是 reorder 的单条排序更新。
type BookmarkOrder struct {
	ID        int64
	SortOrder int
}

// ListBookmarks 返回某用户的全部收藏，按 sort_order, id 升序。
func (s *Store) ListBookmarks(userID int64) ([]model.Bookmark, error) {
	rows, err := s.db.Query(
		`SELECT id, name, path, sort_order, created_at FROM bookmarks WHERE user_id = ? ORDER BY sort_order ASC, id ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Bookmark, 0)
	for rows.Next() {
		var b model.Bookmark
		if err := rows.Scan(&b.ID, &b.Name, &b.Path, &b.SortOrder, &b.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return items, rows.Err()
}

// CreateBookmark 新增一条收藏。唯一索引 (user_id, path) 冲突时返回 ErrBookmarkExists。
func (s *Store) CreateBookmark(userID int64, name, path string, sortOrder int) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO bookmarks (user_id, name, path, sort_order, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, name, path, sortOrder, time.Now().UTC(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrBookmarkExists
		}
		return 0, err
	}
	return res.LastInsertId()
}

// GetBookmark 按 id 查询归属本人的收藏，不存在 / 非本人返回 ErrNotFound。
func (s *Store) GetBookmark(id, userID int64) (*model.Bookmark, error) {
	var b model.Bookmark
	err := s.db.QueryRow(
		`SELECT id, name, path, sort_order, created_at FROM bookmarks WHERE id = ? AND user_id = ?`,
		id, userID,
	).Scan(&b.ID, &b.Name, &b.Path, &b.SortOrder, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// UpdateBookmarkName 重命名归属本人的收藏。影响行数 0 视为 ErrNotFound（不存在 / 非本人）。
func (s *Store) UpdateBookmarkName(id, userID int64, name string) error {
	res, err := s.db.Exec(
		`UPDATE bookmarks SET name = ? WHERE id = ? AND user_id = ?`,
		name, id, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBookmark 删除归属本人的收藏。影响行数 0 视为 ErrNotFound。
func (s *Store) DeleteBookmark(id, userID int64) error {
	res, err := s.db.Exec(`DELETE FROM bookmarks WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderBookmarks 在单事务内批量更新 sort_order，仅作用于归属本人的记录。
func (s *Store) ReorderBookmarks(userID int64, orders []BookmarkOrder) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`UPDATE bookmarks SET sort_order = ? WHERE id = ? AND user_id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, o := range orders {
		if _, err := stmt.Exec(o.SortOrder, o.ID, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MaxBookmarkSort 返回某用户当前最大 sort_order（无记录时返回 0），用于新建时追加到末尾。
func (s *Store) MaxBookmarkSort(userID int64) (int, error) {
	var max sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(sort_order) FROM bookmarks WHERE user_id = ?`, userID).Scan(&max)
	if err != nil {
		return 0, err
	}
	if !max.Valid {
		return 0, nil
	}
	return int(max.Int64), nil
}

// isUniqueViolation 判断错误是否为 SQLite 唯一约束冲突。
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		code := se.Code()
		return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	// 兜底：按错误文本匹配（不同驱动版本的稳健性）。
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
