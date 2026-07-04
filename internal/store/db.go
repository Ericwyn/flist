package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store 封装 SQLite 连接与数据访问。
type Store struct {
	db *sql.DB
}

// schema 为幂等建表语句，Phase 0 无需版本化迁移。
const schema = `
CREATE TABLE IF NOT EXISTS users (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    username            TEXT NOT NULL UNIQUE,
    password            TEXT NOT NULL,
    totp_secret         TEXT DEFAULT '',
    two_factor_enabled  INTEGER DEFAULT 0,
    created_at          DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL,
    expires_at  DATETIME NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_seen   DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS bookmarks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL,
    name        TEXT NOT NULL,
    path        TEXT NOT NULL,
    sort_order  INTEGER DEFAULT 0,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_bookmarks_user ON bookmarks(user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_bookmarks_user_path ON bookmarks(user_id, path);

CREATE TABLE IF NOT EXISTS schema_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// Open 在 dataDir 下打开（或创建）SQLite 主库，开启 WAL + busy_timeout + 外键，
// 并将连接数限制为单连接以规避 database is locked。
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "data.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// 单进程服务，写连接单连接，规避并发写导致的 database is locked。
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// OpenWithDSN 用自定义 DSN 打开库，主要用于测试（如内存库）。
func OpenWithDSN(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	if err := s.migrateAddColumns(); err != nil {
		return fmt.Errorf("migrate add columns: %w", err)
	}
	if err := s.migrateBookmarkFilesPrefix(); err != nil {
		return fmt.Errorf("migrate bookmark files prefix: %w", err)
	}
	return nil
}

// metaGet 读取 schema_meta 中的值，不存在返回空串。
func (s *Store) metaGet(key string) string {
	var v string
	err := s.db.QueryRow(`SELECT value FROM schema_meta WHERE key = ?`, key).Scan(&v)
	if err != nil {
		return ""
	}
	return v
}

// migrateBookmarkFilesPrefix 为存量收藏路径加 /files 前缀（设备管理路径分层，A1 布局）。
//
// 幂等：以 schema_meta 版本标记控制，仅执行一次。安全：仅处理不以 /files 或 /drive
// 开头的路径，避免重复加前缀或误伤设备路径。根 "/" 收藏归一为 "/files"。
// 可逆：回滚脚本见 docs spec §5.4。
func (s *Store) migrateBookmarkFilesPrefix() error {
	const key = "migration_bookmark_files_prefix"
	if s.metaGet(key) == "1" {
		return nil // 已迁移，幂等跳过
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE bookmarks
		SET path = CASE WHEN path = '/' THEN '/files'
		                ELSE '/files' || path END
		WHERE path NOT LIKE '/files%' AND path NOT LIKE '/drive%'`); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO schema_meta(key, value) VALUES(?, ?)`, key, "1"); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateAddColumns 对已有数据库补列（CREATE TABLE IF NOT EXISTS 不会为已存在的表添加新列）。
func (s *Store) migrateAddColumns() error {
	rows, err := s.db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return fmt.Errorf("pragma table_info: %w", err)
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		existing[name] = true
	}

	if !existing["totp_secret"] {
		if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN totp_secret TEXT DEFAULT ''`); err != nil {
			return fmt.Errorf("add column totp_secret: %w", err)
		}
	}
	if !existing["two_factor_enabled"] {
		if _, err := s.db.Exec(`ALTER TABLE users ADD COLUMN two_factor_enabled INTEGER DEFAULT 0`); err != nil {
			return fmt.Errorf("add column two_factor_enabled: %w", err)
		}
	}
	return nil
}

// Close 关闭底层数据库连接。
func (s *Store) Close() error {
	return s.db.Close()
}
