package store

import (
	"database/sql"
	"errors"
	"time"

	"flist/internal/model"
)

// ErrNotFound 表示查询的记录不存在。
var ErrNotFound = errors.New("record not found")

// CountUsers 返回用户总数，用于首次启动判断是否需要创建管理员。
func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser 创建用户，password 应为已哈希的值。返回新用户 ID。
func (s *Store) CreateUser(username, passwordHash string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO users (username, password) VALUES (?, ?)`,
		username, passwordHash,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetUserByUsername 按用户名查询用户。
func (s *Store) GetUserByUsername(username string) (*model.User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, username, password, created_at, updated_at FROM users WHERE username = ?`,
		username,
	))
}

// GetUserByID 按 ID 查询用户。
func (s *Store) GetUserByID(id int64) (*model.User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, username, password, created_at, updated_at FROM users WHERE id = ?`,
		id,
	))
}

// UpdatePassword 更新用户密码哈希，并刷新 updated_at。
func (s *Store) UpdatePassword(userID int64, passwordHash string) error {
	_, err := s.db.Exec(
		`UPDATE users SET password = ?, updated_at = ? WHERE id = ?`,
		passwordHash, time.Now().UTC(), userID,
	)
	return err
}

func (s *Store) scanUser(row *sql.Row) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Username, &u.Password, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
