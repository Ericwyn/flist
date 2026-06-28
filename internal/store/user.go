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
		`SELECT id, username, password, totp_secret, two_factor_enabled, created_at, updated_at FROM users WHERE username = ?`,
		username,
	))
}

// GetUserByID 按 ID 查询用户。
func (s *Store) GetUserByID(id int64) (*model.User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, username, password, totp_secret, two_factor_enabled, created_at, updated_at FROM users WHERE id = ?`,
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

// ErrUsernameTaken 表示目标用户名已被其他用户占用。
var ErrUsernameTaken = errors.New("username taken")

// UpdateUsername 更新用户名。若新用户名已被其他用户占用，返回 ErrUsernameTaken。
// 写连接为单连接（SetMaxOpenConns(1)），预查重与更新串行执行，无并发竞态。
func (s *Store) UpdateUsername(userID int64, username string) error {
	var existingID int64
	err := s.db.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&existingID)
	switch {
	case err == nil:
		if existingID != userID {
			return ErrUsernameTaken
		}
		// 用户名未变（指向自己），直接刷新 updated_at。
	case errors.Is(err, sql.ErrNoRows):
		// 无冲突，继续。
	default:
		return err
	}

	_, err = s.db.Exec(
		`UPDATE users SET username = ?, updated_at = ? WHERE id = ?`,
		username, time.Now().UTC(), userID,
	)
	return err
}

func (s *Store) scanUser(row *sql.Row) (*model.User, error) {
	var u model.User
	var twoFactorEnabled int
	err := row.Scan(&u.ID, &u.Username, &u.Password, &u.TOTPSecret, &twoFactorEnabled, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.TwoFactorEnabled = twoFactorEnabled != 0
	return &u, nil
}

// UpdateTOTP 更新用户的 TOTP 密钥与启用状态，并刷新 updated_at。
func (s *Store) UpdateTOTP(userID int64, secret string, enabled bool) error {
	var e int
	if enabled {
		e = 1
	}
	_, err := s.db.Exec(
		`UPDATE users SET totp_secret = ?, two_factor_enabled = ?, updated_at = ? WHERE id = ?`,
		secret, e, time.Now().UTC(), userID,
	)
	return err
}
