package store

import (
	"database/sql"
	"errors"
	"time"

	"flist/internal/model"
)

// CreateSession 写入一条会话记录。id 为令牌哈希。
func (s *Store) CreateSession(id string, userID int64, expiresAt time.Time) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, user_id, expires_at, created_at, last_seen) VALUES (?, ?, ?, ?, ?)`,
		id, userID, expiresAt.UTC(), now, now,
	)
	return err
}

// GetSession 按令牌哈希查询会话。
func (s *Store) GetSession(id string) (*model.Session, error) {
	var sess model.Session
	err := s.db.QueryRow(
		`SELECT id, user_id, expires_at, created_at, last_seen FROM sessions WHERE id = ?`,
		id,
	).Scan(&sess.ID, &sess.UserID, &sess.ExpiresAt, &sess.CreatedAt, &sess.LastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// TouchSession 刷新会话的 last_seen。
func (s *Store) TouchSession(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET last_seen = ? WHERE id = ?`, time.Now().UTC(), id)
	return err
}

// DeleteSession 删除单个会话（登出）。
func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteUserSessionsExcept 删除某用户除 keepID 外的所有会话（改密后吊销其他会话）。
func (s *Store) DeleteUserSessionsExcept(userID int64, keepID string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ? AND id != ?`, userID, keepID)
	return err
}

// DeleteExpiredSessions 清理已过期的会话，返回删除条数。
func (s *Store) DeleteExpiredSessions(now time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now.UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
