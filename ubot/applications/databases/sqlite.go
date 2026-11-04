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

type NoteRecord struct {
	OwnerID        int64     `json:"owner_id"`
	Name           string    `json:"name"`
	Text           string    `json:"text"`
	Entities       string    `json:"entities,omitempty"`
	Buttons        string    `json:"buttons,omitempty"`
	Mode           string    `json:"mode,omitempty"`
	MediaType      string    `json:"media_type,omitempty"`
	MediaData      string    `json:"media_data,omitempty"`
	CacheMessageID int       `json:"cache_message_id,omitempty"`
	CacheChannel   string    `json:"cache_channel,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
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

		`CREATE TABLE IF NOT EXISTS userbot_notes (
			owner_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			text TEXT NOT NULL,
			entities TEXT DEFAULT '',
			buttons TEXT DEFAULT '',
			mode TEXT DEFAULT 'text',
			media_type TEXT DEFAULT '',
			media_data TEXT DEFAULT '',
			cache_message_id INTEGER DEFAULT 0,
			cache_channel TEXT DEFAULT '',
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (owner_id, name)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_owner ON userbot_notes(owner_id);`,

		`CREATE TABLE IF NOT EXISTS userbot_logs (
			owner_id INTEGER PRIMARY KEY,
			enabled INTEGER DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS log_routes (
			log_bot_id INTEGER NOT NULL,
			log_message_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			source_chat_id INTEGER NOT NULL,
			source_message_id INTEGER NOT NULL,
			peer_type TEXT DEFAULT 'user',
			access_hash INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (log_bot_id, log_message_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_log_routes_user ON log_routes(user_id);`,

		`CREATE TABLE IF NOT EXISTS userbot_configs (
			owner_id INTEGER PRIMARY KEY,
			type TEXT DEFAULT 'Broadcast',
			delay INTEGER DEFAULT 5,
			skip_floodwait INTEGER DEFAULT 0,
			timer TEXT DEFAULT 'Belum diatur',
			extra INTEGER DEFAULT 0,
			mode TEXT DEFAULT 'Normal',
			safemode INTEGER DEFAULT 0,
			status TEXT DEFAULT 'Berhenti',
			masa_aktif TEXT DEFAULT 'November 29, 2026 at 14:18',
			notifikasi TEXT DEFAULT 'Aktif · Kirim ke Saya',
			email TEXT DEFAULT 'Belum diatur',
			admin TEXT DEFAULT 'Belum diatur',
			watermark TEXT DEFAULT '',
			topic_enabled INTEGER DEFAULT 0,
			tindakan_terkini TEXT DEFAULT 'Siaran otomatis dimatikan',
			tindakan_terakhir TEXT DEFAULT '-',
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS userbot_lists (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			mongo_id TEXT DEFAULT '',
			owner_id INTEGER NOT NULL,
			text TEXT NOT NULL,
			media_type TEXT DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_ub_lists_owner ON userbot_lists(owner_id);`,

		`CREATE TABLE IF NOT EXISTS userbot_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			mongo_id TEXT DEFAULT '',
			owner_id INTEGER NOT NULL,
			target TEXT NOT NULL,
			title TEXT DEFAULT '',
			chat_id INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_ub_groups_owner ON userbot_groups(owner_id);`,

		`CREATE TABLE IF NOT EXISTS userbot_topics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			mongo_id TEXT DEFAULT '',
			owner_id INTEGER NOT NULL,
			chat_id INTEGER DEFAULT 0,
			topic_id INTEGER DEFAULT 0,
			title TEXT DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_ub_topics_owner ON userbot_topics(owner_id);`,
	}

	for _, query := range queries {
		if _, err := s.DB.Exec(query); err != nil {
			return err
		}
	}

	// Safe column migration for existing userbot_notes tables
	_, _ = s.DB.Exec(`ALTER TABLE userbot_notes ADD COLUMN cache_message_id INTEGER DEFAULT 0;`)
	_, _ = s.DB.Exec(`ALTER TABLE userbot_notes ADD COLUMN cache_channel TEXT DEFAULT '';`)
	_, _ = s.DB.Exec(`ALTER TABLE log_routes ADD COLUMN peer_type TEXT DEFAULT 'user';`)
	_, _ = s.DB.Exec(`ALTER TABLE log_routes ADD COLUMN access_hash INTEGER DEFAULT 0;`)

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
	query := `SELECT COUNT(1) FROM sessions WHERE (chat_id = ? OR owner_id = ? OR account_id = ?) AND session_blob != '';`
	err := s.DB.QueryRowContext(ctx, query, chatID, ownerID, ownerID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *SQLiteClient) GetAllSessions(ctx context.Context) ([]SessionRecord, error) {
	query := `SELECT chat_id, owner_id, account_id, session_blob, prefix, prefix_set, updated_at FROM sessions WHERE session_blob != '';`
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

func (s *SQLiteClient) SaveNote(ctx context.Context, n NoteRecord) error {
	query := `INSERT INTO userbot_notes (owner_id, name, text, entities, buttons, mode, media_type, media_data, cache_message_id, cache_channel, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(owner_id, name) DO UPDATE SET
			text = excluded.text,
			entities = excluded.entities,
			buttons = excluded.buttons,
			mode = excluded.mode,
			media_type = excluded.media_type,
			media_data = excluded.media_data,
			cache_message_id = excluded.cache_message_id,
			cache_channel = excluded.cache_channel,
			updated_at = CURRENT_TIMESTAMP;`
	_, err := s.DB.ExecContext(ctx, query, n.OwnerID, n.Name, n.Text, n.Entities, n.Buttons, n.Mode, n.MediaType, n.MediaData, n.CacheMessageID, n.CacheChannel)
	return err
}

func (s *SQLiteClient) GetNote(ctx context.Context, ownerID int64, name string) (*NoteRecord, error) {
	query := `SELECT owner_id, name, text, entities, buttons, mode, media_type, media_data, cache_message_id, cache_channel, updated_at
		FROM userbot_notes WHERE owner_id = ? AND name = ? LIMIT 1;`
	var n NoteRecord
	err := s.DB.QueryRowContext(ctx, query, ownerID, name).Scan(
		&n.OwnerID, &n.Name, &n.Text, &n.Entities, &n.Buttons, &n.Mode, &n.MediaType, &n.MediaData, &n.CacheMessageID, &n.CacheChannel, &n.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *SQLiteClient) ListNotes(ctx context.Context, ownerID int64) ([]NoteRecord, error) {
	query := `SELECT owner_id, name, text, entities, buttons, mode, media_type, media_data, cache_message_id, cache_channel, updated_at
		FROM userbot_notes WHERE owner_id = ? ORDER BY name ASC;`
	rows, err := s.DB.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []NoteRecord
	for rows.Next() {
		var n NoteRecord
		if err := rows.Scan(&n.OwnerID, &n.Name, &n.Text, &n.Entities, &n.Buttons, &n.Mode, &n.MediaType, &n.MediaData, &n.CacheMessageID, &n.CacheChannel, &n.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, n)
	}
	return records, rows.Err()
}

func (s *SQLiteClient) DeleteNote(ctx context.Context, ownerID int64, name string) (bool, error) {
	query := `DELETE FROM userbot_notes WHERE owner_id = ? AND name = ?;`
	res, err := s.DB.ExecContext(ctx, query, ownerID, name)
	if err != nil {
		return false, err
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func (s *SQLiteClient) ClearNotes(ctx context.Context, ownerID int64) (int64, error) {
	query := `DELETE FROM userbot_notes WHERE owner_id = ?;`
	res, err := s.DB.ExecContext(ctx, query, ownerID)
	if err != nil {
		return 0, err
	}
	rows, _ := res.RowsAffected()
	return rows, nil
}

func (s *SQLiteClient) CountNotes(ctx context.Context, ownerID int64) (int, error) {
	var count int
	query := `SELECT COUNT(1) FROM userbot_notes WHERE owner_id = ?;`
	err := s.DB.QueryRowContext(ctx, query, ownerID).Scan(&count)
	return count, err
}


func (s *SQLiteClient) CountAllNotes(ctx context.Context) (int, error) {
	var count int
	query := `SELECT COUNT(1) FROM userbot_notes;`
	err := s.DB.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

func (s *SQLiteClient) ListAllNotes(ctx context.Context) ([]NoteRecord, error) {
	query := `SELECT owner_id, name, text, entities, buttons, mode, media_type, media_data, cache_message_id, cache_channel, updated_at
		FROM userbot_notes ORDER BY name ASC;`
	rows, err := s.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []NoteRecord
	for rows.Next() {
		var n NoteRecord
		if err := rows.Scan(&n.OwnerID, &n.Name, &n.Text, &n.Entities, &n.Buttons, &n.Mode, &n.MediaType, &n.MediaData, &n.CacheMessageID, &n.CacheChannel, &n.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, n)
	}
	return records, rows.Err()
}

type LogRouteRecord struct {
	LogBotID        int64
	LogMessageID    int
	UserID          int64
	SourceChatID    int64
	SourceMessageID int
	PeerType        string
	AccessHash      int64
	CreatedAt       time.Time
}

func (s *SQLiteClient) GetLogStatus(ctx context.Context, ownerID int64) (bool, error) {
	var enabled int
	query := `SELECT enabled FROM userbot_logs WHERE owner_id = ?;`
	err := s.DB.QueryRowContext(ctx, query, ownerID).Scan(&enabled)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return enabled == 1, nil
}

func (s *SQLiteClient) SetLogStatus(ctx context.Context, ownerID int64, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	query := `INSERT INTO userbot_logs (owner_id, enabled, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(owner_id) DO UPDATE SET enabled = excluded.enabled, updated_at = CURRENT_TIMESTAMP;`
	_, err := s.DB.ExecContext(ctx, query, ownerID, val)
	return err
}

func (s *SQLiteClient) SaveLogRoute(ctx context.Context, logBotID int64, logMsgID int, userID, sourceChatID int64, sourceMsgID int, peerType string, accessHash int64) error {
	if peerType == "" {
		peerType = "user"
	}
	query := `INSERT OR REPLACE INTO log_routes (log_bot_id, log_message_id, user_id, source_chat_id, source_message_id, peer_type, access_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP);`
	_, err := s.DB.ExecContext(ctx, query, logBotID, logMsgID, userID, sourceChatID, sourceMsgID, peerType, accessHash)
	return err
}

func (s *SQLiteClient) GetLogRoute(ctx context.Context, logBotID int64, logMsgID int) (*LogRouteRecord, error) {
	query := `SELECT log_bot_id, log_message_id, user_id, source_chat_id, source_message_id, COALESCE(peer_type, 'user'), COALESCE(access_hash, 0), created_at
		FROM log_routes WHERE (log_bot_id = ? OR ? = 0) AND log_message_id = ?
		ORDER BY created_at DESC LIMIT 1;`
	var r LogRouteRecord
	err := s.DB.QueryRowContext(ctx, query, logBotID, logBotID, logMsgID).Scan(
		&r.LogBotID, &r.LogMessageID, &r.UserID, &r.SourceChatID, &r.SourceMessageID, &r.PeerType, &r.AccessHash, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		// Fallback: cari berdasarkan log_message_id saja jika ID bot berbeda
		query2 := `SELECT log_bot_id, log_message_id, user_id, source_chat_id, source_message_id, COALESCE(peer_type, 'user'), COALESCE(access_hash, 0), created_at
			FROM log_routes WHERE log_message_id = ?
			ORDER BY created_at DESC LIMIT 1;`
		err2 := s.DB.QueryRowContext(ctx, query2, logMsgID).Scan(
			&r.LogBotID, &r.LogMessageID, &r.UserID, &r.SourceChatID, &r.SourceMessageID, &r.PeerType, &r.AccessHash, &r.CreatedAt,
		)
		if err2 == sql.ErrNoRows {
			return nil, nil
		}
		if err2 != nil {
			return nil, err2
		}
		return &r, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
