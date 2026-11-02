package databases

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteClient struct {
	DB *sql.DB
}

type SessionRecord struct {
	ChatID    int64
	OwnerID   int64
	AccountID int64
	Blob      string
	Prefix    string
	PrefixSet bool
	UpdatedAt time.Time
}

type BlacklistRecord struct {
	OwnerID   int64
	ChatType  string
	ChatID    int64
	CreatedAt time.Time
}

func NewSQLiteClient(path string) (*SQLiteClient, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory for sqlite: %w", err)
		}
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(1 * time.Hour)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	client := &SQLiteClient{DB: db}
	if err := client.initTables(); err != nil {
		return nil, fmt.Errorf("failed to initialize sqlite tables: %w", err)
	}

	log.Printf("📂 SQLite connected and initialized at: %s", path)
	return client, nil
}

func (s *SQLiteClient) initTables() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			chat_id INTEGER PRIMARY KEY,
			owner_id INTEGER NOT NULL,
			account_id INTEGER NOT NULL,
			session_blob TEXT NOT NULL,
			prefix TEXT DEFAULT '!',
			prefix_set INTEGER DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_account ON sessions(account_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_owner ON sessions(owner_id);`,

		`CREATE TABLE IF NOT EXISTS sudo_users (
			owner_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (owner_id, user_id)
		);`,

		`CREATE TABLE IF NOT EXISTS broadcast_blacklist (
			owner_id INTEGER NOT NULL,
			chat_type TEXT NOT NULL,
			chat_id INTEGER NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (owner_id, chat_type, chat_id)
		);`,
	}

	for _, query := range queries {
		if _, err := s.DB.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteClient) Close() error {
	if s.DB != nil {
		return s.DB.Close()
	}
	return nil
}

func (s *SQLiteClient) SaveSession(ctx context.Context, chatID, ownerID, accountID int64, blob, prefix string, prefixSet bool) error {
	query := `INSERT INTO sessions (chat_id, owner_id, account_id, session_blob, prefix, prefix_set, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(chat_id) DO UPDATE SET
			owner_id = excluded.owner_id,
			account_id = excluded.account_id,
			session_blob = excluded.session_blob,
			prefix = CASE WHEN excluded.prefix_set = 1 THEN excluded.prefix ELSE sessions.prefix END,
			prefix_set = CASE WHEN excluded.prefix_set = 1 THEN 1 ELSE sessions.prefix_set END,
			updated_at = CURRENT_TIMESTAMP;`

	pset := 0
	if prefixSet {
		pset = 1
	}
	_, err := s.DB.ExecContext(ctx, query, chatID, ownerID, accountID, blob, prefix, pset)
	return err
}

func (s *SQLiteClient) UpdatePrefix(ctx context.Context, chatID int64, prefix string) error {
	query := `UPDATE sessions SET prefix = ?, prefix_set = 1, updated_at = CURRENT_TIMESTAMP WHERE chat_id = ?;`
	_, err := s.DB.ExecContext(ctx, query, prefix, chatID)
	return err
}

func (s *SQLiteClient) GetSessionByChat(ctx context.Context, chatID int64) (*SessionRecord, error) {
	query := `SELECT chat_id, owner_id, account_id, session_blob, prefix, prefix_set, updated_at FROM sessions WHERE chat_id = ? LIMIT 1;`
	var r SessionRecord
	var pset int
	err := s.DB.QueryRowContext(ctx, query, chatID).Scan(&r.ChatID, &r.OwnerID, &r.AccountID, &r.Blob, &r.Prefix, &pset, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.PrefixSet = pset == 1
	return &r, nil
}

func (s *SQLiteClient) GetSessionByAccount(ctx context.Context, accountID int64) (*SessionRecord, error) {
	query := `SELECT chat_id, owner_id, account_id, session_blob, prefix, prefix_set, updated_at FROM sessions WHERE account_id = ? LIMIT 1;`
	var r SessionRecord
	var pset int
	err := s.DB.QueryRowContext(ctx, query, accountID).Scan(&r.ChatID, &r.OwnerID, &r.AccountID, &r.Blob, &r.Prefix, &pset, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.PrefixSet = pset == 1
	return &r, nil
}

func (s *SQLiteClient) HasSession(ctx context.Context, chatID, ownerID int64) (bool, error) {
	var count int
	query := `SELECT COUNT(1) FROM sessions WHERE (chat_id = ? OR owner_id = ?) AND account_id > 0;`
	err := s.DB.QueryRowContext(ctx, query, chatID, ownerID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *SQLiteClient) GetAllSessions(ctx context.Context) ([]SessionRecord, error) {
	query := `SELECT chat_id, owner_id, account_id, session_blob, prefix, prefix_set, updated_at FROM sessions WHERE account_id > 0;`
	rows, err := s.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SessionRecord
	for rows.Next() {
		var r SessionRecord
		var pset int
		if err := rows.Scan(&r.ChatID, &r.OwnerID, &r.AccountID, &r.Blob, &r.Prefix, &pset, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.PrefixSet = pset == 1
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *SQLiteClient) DeleteSession(ctx context.Context, chatID int64) error {
	query := `DELETE FROM sessions WHERE chat_id = ?;`
	_, err := s.DB.ExecContext(ctx, query, chatID)
	return err
}

func (s *SQLiteClient) CountSessions(ctx context.Context) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM sessions;`).Scan(&count)
	return count, err
}

func (s *SQLiteClient) IsSudo(ctx context.Context, ownerID, userID int64) (bool, error) {
	var count int
	query := `SELECT COUNT(1) FROM sudo_users WHERE owner_id = ? AND user_id = ?;`
	err := s.DB.QueryRowContext(ctx, query, ownerID, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *SQLiteClient) AddSudo(ctx context.Context, ownerID, userID int64) error {
	query := `INSERT OR IGNORE INTO sudo_users (owner_id, user_id) VALUES (?, ?);`
	_, err := s.DB.ExecContext(ctx, query, ownerID, userID)
	return err
}

func (s *SQLiteClient) RemoveSudo(ctx context.Context, ownerID, userID int64) error {
	query := `DELETE FROM sudo_users WHERE owner_id = ? AND user_id = ?;`
	_, err := s.DB.ExecContext(ctx, query, ownerID, userID)
	return err
}

func (s *SQLiteClient) AddBlacklist(ctx context.Context, ownerID int64, chatType string, chatID int64) error {
	query := `INSERT OR IGNORE INTO broadcast_blacklist (owner_id, chat_type, chat_id) VALUES (?, ?, ?);`
	_, err := s.DB.ExecContext(ctx, query, ownerID, chatType, chatID)
	return err
}

func (s *SQLiteClient) RemoveBlacklist(ctx context.Context, ownerID int64, chatType string, chatID int64) error {
	query := `DELETE FROM broadcast_blacklist WHERE owner_id = ? AND chat_type = ? AND chat_id = ?;`
	_, err := s.DB.ExecContext(ctx, query, ownerID, chatType, chatID)
	return err
}

func (s *SQLiteClient) ClearBlacklist(ctx context.Context, ownerID int64) error {
	query := `DELETE FROM broadcast_blacklist WHERE owner_id = ?;`
	_, err := s.DB.ExecContext(ctx, query, ownerID)
	return err
}

func (s *SQLiteClient) ListBlacklist(ctx context.Context, ownerID int64) ([]BlacklistRecord, error) {
	query := `SELECT owner_id, chat_type, chat_id, created_at FROM broadcast_blacklist WHERE owner_id = ?;`
	rows, err := s.DB.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []BlacklistRecord
	for rows.Next() {
		var r BlacklistRecord
		if err := rows.Scan(&r.OwnerID, &r.ChatType, &r.ChatID, &r.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *SQLiteClient) IsBlacklisted(ctx context.Context, ownerID int64, chatType string, chatID int64) (bool, error) {
	var count int
	query := `SELECT COUNT(1) FROM broadcast_blacklist WHERE owner_id = ? AND chat_type = ? AND chat_id = ?;`
	err := s.DB.QueryRowContext(ctx, query, ownerID, chatType, chatID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

