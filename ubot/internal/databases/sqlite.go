package databases

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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

type UserbotSettings struct {
	ChatID    int64
	OwnerID   int64
	AccountID int64
	Prefix    string
	PrefixSet bool
	UpdatedAt time.Time
}

type BlacklistRecord struct {
	OwnerID   int64
	ChatType  string
	ChatID    int64
	PeerKind  string
	Title     string
	Username  string
	CreatedAt time.Time
}

type LogBlockRecord struct {
	OwnerID   int64
	UserID    int64
	Label     string
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

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-2000)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(0)&_pragma=wal_autocheckpoint(1000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	// Explicitly enforce WAL mode & high-concurrency PRAGMAs
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA busy_timeout = 10000;",
		"PRAGMA cache_size = -2000;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA mmap_size = 0;",
		"PRAGMA wal_autocheckpoint = 1000;",
	}
	for _, p := range pragmas {
		if _, pErr := db.Exec(p); pErr != nil {
			log.Printf("⚠️ SQLite pragma '%s' warning: %v", p, pErr)
		}
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

		`CREATE TABLE IF NOT EXISTS userbot_settings (
			chat_id INTEGER PRIMARY KEY,
			owner_id INTEGER DEFAULT 0,
			account_id INTEGER DEFAULT 0,
			prefix TEXT DEFAULT '!',
			prefix_set INTEGER DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_userbot_settings_account ON userbot_settings(account_id);`,

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

		`CREATE TABLE IF NOT EXISTS log_blocked_users (
			owner_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			label TEXT DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (owner_id, user_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_log_blocked_owner ON log_blocked_users(owner_id);`,

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

		`CREATE TABLE IF NOT EXISTS global_moderation (
			owner_id INTEGER NOT NULL,
			kind TEXT NOT NULL,
			user_id INTEGER NOT NULL,
			label TEXT DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (owner_id, kind, user_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_global_mod_owner ON global_moderation(owner_id, kind);`,

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

		`CREATE TABLE IF NOT EXISTS jaseb_configurasi (
			owner_id INTEGER PRIMARY KEY,
			type TEXT DEFAULT 'Broadcast',
			delay INTEGER DEFAULT 5,
			delay_mode TEXT DEFAULT 'manual',
			delay_all_groups INTEGER DEFAULT 0,
			skip_floodwait INTEGER DEFAULT 0,
			skip_floodwait_sec INTEGER DEFAULT 0,
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
			watermark_cache_id INTEGER DEFAULT 0,
			topic_enabled INTEGER DEFAULT 0,
			tindakan_terkini TEXT DEFAULT 'Siaran otomatis dimatikan',
			tindakan_terakhir TEXT DEFAULT '-',
			lists TEXT DEFAULT '[]',
			groups TEXT DEFAULT '[]',
			topics TEXT DEFAULT '[]',
			topic_lists TEXT DEFAULT '[]',
			broadcast_index INTEGER DEFAULT 0,
			broadcast_next_at INTEGER DEFAULT 0,
			active_token_sha TEXT DEFAULT '',
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

		`CREATE TABLE IF NOT EXISTS album_mappings (
			first_id INTEGER PRIMARY KEY,
			all_ids TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS userbot_peers (
			peer_id INTEGER PRIMARY KEY,
			peer_type TEXT NOT NULL,
			access_hash INTEGER NOT NULL,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS user_licenses (
			token_sha TEXT PRIMARY KEY,
			master_token TEXT NOT NULL,
			owner_chat_id INTEGER NOT NULL,
			userbot_id INTEGER NOT NULL,
			tier TEXT NOT NULL,
			can_custom_watermark INTEGER DEFAULT 0,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			status TEXT DEFAULT 'active'
		);`,

		`CREATE TABLE IF NOT EXISTS daily_send_quota (
			owner_id INTEGER NOT NULL,
			date_wib TEXT NOT NULL,
			sent_count INTEGER DEFAULT 0,
			PRIMARY KEY (owner_id, date_wib)
		);`,

		`CREATE TABLE IF NOT EXISTS worker_assignments (
			owner_id INTEGER PRIMARY KEY,
			worker_id TEXT NOT NULL,
			pool TEXT DEFAULT 'normal',
			heavy_score INTEGER DEFAULT 0,
			migrated_at INTEGER DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_worker_assignments_wid ON worker_assignments(worker_id);`,
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
	_, _ = s.DB.Exec(`ALTER TABLE jaseb_configurasi ADD COLUMN broadcast_index INTEGER DEFAULT 0;`)
	_, _ = s.DB.Exec(`ALTER TABLE jaseb_configurasi ADD COLUMN broadcast_next_at INTEGER DEFAULT 0;`)
	_, _ = s.DB.Exec(`ALTER TABLE jaseb_configurasi ADD COLUMN topic_lists TEXT DEFAULT '[]';`)
	_, _ = s.DB.Exec(`ALTER TABLE jaseb_configurasi ADD COLUMN delay_mode TEXT DEFAULT 'manual';`)
	_, _ = s.DB.Exec(`ALTER TABLE jaseb_configurasi ADD COLUMN delay_all_groups INTEGER DEFAULT 0;`)
	_, _ = s.DB.Exec(`ALTER TABLE jaseb_configurasi ADD COLUMN skip_floodwait_sec INTEGER DEFAULT 0;`)
	_, _ = s.DB.Exec(`ALTER TABLE jaseb_configurasi ADD COLUMN active_token_sha TEXT DEFAULT '';`)
	_, _ = s.DB.Exec(`ALTER TABLE broadcast_blacklist ADD COLUMN peer_kind TEXT DEFAULT '';`)
	_, _ = s.DB.Exec(`ALTER TABLE broadcast_blacklist ADD COLUMN title TEXT DEFAULT '';`)
	_, _ = s.DB.Exec(`ALTER TABLE broadcast_blacklist ADD COLUMN username TEXT DEFAULT '';`)
	_, _ = s.DB.Exec(`INSERT OR IGNORE INTO userbot_settings (chat_id, owner_id, account_id, prefix, prefix_set, updated_at)
		SELECT chat_id, owner_id, account_id, prefix, prefix_set, updated_at FROM sessions WHERE prefix_set = 1;`)

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
			account_id = CASE WHEN excluded.account_id > 0 THEN excluded.account_id ELSE sessions.account_id END,
			session_blob = excluded.session_blob,
			prefix = CASE WHEN excluded.prefix_set = 1 THEN excluded.prefix ELSE sessions.prefix END,
			prefix_set = CASE WHEN excluded.prefix_set = 1 THEN 1 ELSE sessions.prefix_set END,
			updated_at = CURRENT_TIMESTAMP;`

	pset := 0
	if prefixSet {
		pset = 1
	}
	return s.execWithBusyRetry(ctx, func() error {
		_, err := s.DB.ExecContext(ctx, query, chatID, ownerID, accountID, blob, prefix, pset)
		return err
	})
}

type WorkerAssignment struct {
	OwnerID    int64
	WorkerID   string
	Pool       string
	HeavyScore int
	MigratedAt int64
	UpdatedAt  time.Time
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "DATABASE IS LOCKED")
}

func (s *SQLiteClient) execWithBusyRetry(ctx context.Context, fn func() error) error {
	var err error
	backoffs := []time.Duration{40 * time.Millisecond, 80 * time.Millisecond, 160 * time.Millisecond}
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		err = fn()
		if err == nil || !isSQLiteBusyError(err) || attempt == len(backoffs) {
			return err
		}
		timer := time.NewTimer(backoffs[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func (s *SQLiteClient) GetWorkerAssignment(ctx context.Context, ownerID int64) (*WorkerAssignment, error) {
	if s == nil || s.DB == nil || ownerID == 0 {
		return nil, nil
	}
	var a WorkerAssignment
	err := s.DB.QueryRowContext(ctx,
		`SELECT owner_id, worker_id, COALESCE(pool, 'normal'), COALESCE(heavy_score, 0), COALESCE(migrated_at, 0), updated_at FROM worker_assignments WHERE owner_id = ? LIMIT 1;`,
		ownerID,
	).Scan(&a.OwnerID, &a.WorkerID, &a.Pool, &a.HeavyScore, &a.MigratedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get worker assignment: %w", err)
	}
	return &a, nil
}

func (s *SQLiteClient) SaveWorkerAssignment(ctx context.Context, a *WorkerAssignment) error {
	if s == nil || s.DB == nil || a == nil || a.OwnerID == 0 || strings.TrimSpace(a.WorkerID) == "" {
		return nil
	}
	pool := strings.ToLower(strings.TrimSpace(a.Pool))
	if pool == "" {
		pool = "normal"
	}
	now := time.Now()
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = now
	}
	return s.execWithBusyRetry(ctx, func() error {
		_, execErr := s.DB.ExecContext(ctx,
			`INSERT INTO worker_assignments (owner_id, worker_id, pool, heavy_score, migrated_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(owner_id) DO UPDATE SET
				worker_id = excluded.worker_id,
				pool = excluded.pool,
				heavy_score = excluded.heavy_score,
				migrated_at = excluded.migrated_at,
				updated_at = excluded.updated_at;`,
			a.OwnerID, strings.TrimSpace(a.WorkerID), pool, a.HeavyScore, a.MigratedAt, a.UpdatedAt,
		)
		if execErr != nil {
			return fmt.Errorf("save worker assignment: %w", execErr)
		}
		return nil
	})
}

func (s *SQLiteClient) ListWorkerAssignmentsByWorker(ctx context.Context, workerID string) ([]*WorkerAssignment, error) {
	if s == nil || s.DB == nil || strings.TrimSpace(workerID) == "" {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT owner_id, worker_id, COALESCE(pool, 'normal'), COALESCE(heavy_score, 0), COALESCE(migrated_at, 0), updated_at FROM worker_assignments WHERE worker_id = ? ORDER BY owner_id ASC;`,
		strings.TrimSpace(workerID),
	)
	if err != nil {
		return nil, fmt.Errorf("list worker assignments: %w", err)
	}
	defer rows.Close()
	var list []*WorkerAssignment
	for rows.Next() {
		var a WorkerAssignment
		if scanErr := rows.Scan(&a.OwnerID, &a.WorkerID, &a.Pool, &a.HeavyScore, &a.MigratedAt, &a.UpdatedAt); scanErr == nil {
			list = append(list, &a)
		}
	}
	return list, nil
}

func (s *SQLiteClient) UpdatePrefix(ctx context.Context, chatID int64, prefix string) error {
	query := `UPDATE sessions SET prefix = ?, prefix_set = 1, updated_at = CURRENT_TIMESTAMP WHERE chat_id = ?;`
	_, err := s.DB.ExecContext(ctx, query, prefix, chatID)
	return err
}

func (s *SQLiteClient) GetUserbotSettings(ctx context.Context, chatID int64) (*UserbotSettings, error) {
	query := `SELECT chat_id, owner_id, account_id, prefix, prefix_set, updated_at FROM userbot_settings WHERE chat_id = ? LIMIT 1;`
	var r UserbotSettings
	var pset int
	err := s.DB.QueryRowContext(ctx, query, chatID).Scan(&r.ChatID, &r.OwnerID, &r.AccountID, &r.Prefix, &pset, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.PrefixSet = pset == 1
	return &r, nil
}

func (s *SQLiteClient) GetUserbotSettingsByAccount(ctx context.Context, accountID int64) (*UserbotSettings, error) {
	if accountID == 0 {
		return nil, sql.ErrNoRows
	}
	query := `SELECT chat_id, owner_id, account_id, prefix, prefix_set, updated_at FROM userbot_settings WHERE account_id = ? AND prefix_set = 1 ORDER BY updated_at DESC LIMIT 1;`
	var r UserbotSettings
	var pset int
	err := s.DB.QueryRowContext(ctx, query, accountID).Scan(&r.ChatID, &r.OwnerID, &r.AccountID, &r.Prefix, &pset, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.PrefixSet = pset == 1
	return &r, nil
}

func (s *SQLiteClient) SaveUserbotPrefix(ctx context.Context, chatID, ownerID, accountID int64, prefix string, prefixSet bool) error {
	if chatID == 0 {
		return nil
	}
	pset := 0
	if prefixSet {
		pset = 1
	}
	query := `INSERT INTO userbot_settings (chat_id, owner_id, account_id, prefix, prefix_set, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(chat_id) DO UPDATE SET
			owner_id = CASE WHEN excluded.owner_id > 0 THEN excluded.owner_id ELSE userbot_settings.owner_id END,
			account_id = CASE WHEN excluded.account_id > 0 THEN excluded.account_id ELSE userbot_settings.account_id END,
			prefix = CASE WHEN excluded.prefix_set = 1 OR userbot_settings.prefix_set = 0 THEN excluded.prefix ELSE userbot_settings.prefix END,
			prefix_set = CASE WHEN excluded.prefix_set = 1 THEN 1 ELSE userbot_settings.prefix_set END,
			updated_at = CURRENT_TIMESTAMP;`
	_, err := s.DB.ExecContext(ctx, query, chatID, ownerID, accountID, prefix, pset)
	return err
}

func (s *SQLiteClient) RelinkUserbotData(ctx context.Context, oldChatID, newChatID, oldAccountID, newAccountID int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if newChatID != 0 {
		var src *UserbotSettings
		for _, candidate := range []int64{oldChatID, oldAccountID} {
			if candidate == 0 || candidate == newChatID {
				continue
			}
			var r UserbotSettings
			var pset int
			q := `SELECT chat_id, owner_id, account_id, prefix, prefix_set, updated_at FROM userbot_settings WHERE chat_id = ? LIMIT 1;`
			if scanErr := tx.QueryRowContext(ctx, q, candidate).Scan(&r.ChatID, &r.OwnerID, &r.AccountID, &r.Prefix, &pset, &r.UpdatedAt); scanErr == nil && pset == 1 {
				r.PrefixSet = true
				src = &r
				break
			}
		}
		if src == nil && oldAccountID != 0 {
			var r UserbotSettings
			var pset int
			q := `SELECT chat_id, owner_id, account_id, prefix, prefix_set, updated_at FROM userbot_settings WHERE account_id = ? AND prefix_set = 1 ORDER BY updated_at DESC LIMIT 1;`
			if scanErr := tx.QueryRowContext(ctx, q, oldAccountID).Scan(&r.ChatID, &r.OwnerID, &r.AccountID, &r.Prefix, &pset, &r.UpdatedAt); scanErr == nil && pset == 1 {
				r.PrefixSet = true
				src = &r
			}
		}
		if src != nil {
			accID := newAccountID
			if accID == 0 {
				accID = src.AccountID
			}
			_, _ = tx.ExecContext(ctx, `INSERT INTO userbot_settings (chat_id, owner_id, account_id, prefix, prefix_set, updated_at)
				VALUES (?, ?, ?, ?, 1, CURRENT_TIMESTAMP)
				ON CONFLICT(chat_id) DO UPDATE SET
					owner_id = excluded.owner_id,
					account_id = CASE WHEN excluded.account_id > 0 THEN excluded.account_id ELSE userbot_settings.account_id END,
					prefix = excluded.prefix,
					prefix_set = 1,
					updated_at = CURRENT_TIMESTAMP;`, newChatID, newChatID, accID, src.Prefix)
			_, _ = tx.ExecContext(ctx, `UPDATE sessions SET prefix = ?, prefix_set = 1, updated_at = CURRENT_TIMESTAMP WHERE chat_id = ?;`, src.Prefix, newChatID)
			if src.ChatID != 0 && src.ChatID != newChatID {
				_, _ = tx.ExecContext(ctx, `DELETE FROM userbot_settings WHERE chat_id = ?;`, src.ChatID)
			}
		}
	}

	moveOwnerData := func(fromID, toID int64) {
		if fromID == 0 || toID == 0 || fromID == toID {
			return
		}
		_, _ = tx.ExecContext(ctx, `INSERT OR IGNORE INTO sudo_users (owner_id, user_id, created_at) SELECT ?, user_id, created_at FROM sudo_users WHERE owner_id = ?;`, toID, fromID)
		_, _ = tx.ExecContext(ctx, `DELETE FROM sudo_users WHERE owner_id = ?;`, fromID)

		_, _ = tx.ExecContext(ctx, `INSERT OR IGNORE INTO broadcast_blacklist (owner_id, chat_type, chat_id, peer_kind, title, username, created_at) SELECT ?, chat_type, chat_id, peer_kind, title, username, created_at FROM broadcast_blacklist WHERE owner_id = ?;`, toID, fromID)
		_, _ = tx.ExecContext(ctx, `DELETE FROM broadcast_blacklist WHERE owner_id = ?;`, fromID)

		_, _ = tx.ExecContext(ctx, `INSERT OR IGNORE INTO userbot_notes (owner_id, name, text, entities, buttons, mode, media_type, media_data, cache_message_id, cache_channel, updated_at)
			SELECT ?, name, text, entities, buttons, mode, media_type, media_data, cache_message_id, cache_channel, updated_at FROM userbot_notes WHERE owner_id = ?;`, toID, fromID)
		_, _ = tx.ExecContext(ctx, `DELETE FROM userbot_notes WHERE owner_id = ?;`, fromID)

		_, _ = tx.ExecContext(ctx, `INSERT OR IGNORE INTO userbot_logs (owner_id, enabled, updated_at) SELECT ?, enabled, updated_at FROM userbot_logs WHERE owner_id = ?;`, toID, fromID)
		_, _ = tx.ExecContext(ctx, `DELETE FROM userbot_logs WHERE owner_id = ?;`, fromID)

		_, _ = tx.ExecContext(ctx, `INSERT OR IGNORE INTO log_blocked_users (owner_id, user_id, label, created_at) SELECT ?, user_id, label, created_at FROM log_blocked_users WHERE owner_id = ?;`, toID, fromID)
		_, _ = tx.ExecContext(ctx, `DELETE FROM log_blocked_users WHERE owner_id = ?;`, fromID)

		_, _ = tx.ExecContext(ctx, `UPDATE log_routes SET user_id = ? WHERE user_id = ?;`, toID, fromID)
	}

	if oldAccountID != 0 && newAccountID != 0 && oldAccountID != newAccountID {
		moveOwnerData(oldAccountID, newAccountID)
	} else if oldAccountID == 0 && oldChatID != 0 && newAccountID != 0 && oldChatID != newAccountID {
		moveOwnerData(oldChatID, newAccountID)
	}

	return tx.Commit()
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


func (s *SQLiteClient) AddLogBlock(ctx context.Context, ownerID, userID int64, label string) error {
	query := `INSERT INTO log_blocked_users (owner_id, user_id, label)
		VALUES (?, ?, ?)
		ON CONFLICT(owner_id, user_id) DO UPDATE SET
			label = CASE WHEN excluded.label != '' THEN excluded.label ELSE log_blocked_users.label END;`
	_, err := s.DB.ExecContext(ctx, query, ownerID, userID, label)
	return err
}

func (s *SQLiteClient) RemoveLogBlock(ctx context.Context, ownerID, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM log_blocked_users WHERE owner_id = ? AND user_id = ?;`, ownerID, userID)
	return err
}

func (s *SQLiteClient) ClearLogBlocks(ctx context.Context, ownerID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM log_blocked_users WHERE owner_id = ?;`, ownerID)
	return err
}

func (s *SQLiteClient) ListLogBlocks(ctx context.Context, ownerID int64) ([]LogBlockRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT owner_id, user_id, label, created_at FROM log_blocked_users WHERE owner_id = ? ORDER BY user_id ASC;`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []LogBlockRecord
	for rows.Next() {
		var r LogBlockRecord
		if err := rows.Scan(&r.OwnerID, &r.UserID, &r.Label, &r.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *SQLiteClient) IsLogBlocked(ctx context.Context, ownerID, userID int64) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM log_blocked_users WHERE owner_id = ? AND user_id = ?;`, ownerID, userID).Scan(&count)
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

func (s *SQLiteClient) SavePeer(ctx context.Context, peerID int64, peerType string, accessHash int64) error {
	if peerID == 0 || accessHash == 0 {
		return nil
	}
	if peerType == "" {
		peerType = "user"
	}
	query := `INSERT INTO userbot_peers (peer_id, peer_type, access_hash, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(peer_id) DO UPDATE SET access_hash = excluded.access_hash, updated_at = CURRENT_TIMESTAMP;`
	_, err := s.DB.ExecContext(ctx, query, peerID, peerType, accessHash)
	return err
}

func (s *SQLiteClient) GetPeerAccessHash(ctx context.Context, peerID int64) (int64, string, error) {
	if peerID == 0 {
		return 0, "", sql.ErrNoRows
	}
	query := `SELECT access_hash, peer_type FROM userbot_peers WHERE peer_id = ? LIMIT 1;`
	var hash int64
	var pType string
	err := s.DB.QueryRowContext(ctx, query, peerID).Scan(&hash, &pType)
	if err != nil {
		return 0, "", err
	}
	return hash, pType, nil
}

func (s *SQLiteClient) LoadAllPeers(ctx context.Context) (map[int64]int64, map[int64]int64, error) {
	users := make(map[int64]int64)
	channels := make(map[int64]int64)
	query := `SELECT peer_id, peer_type, access_hash FROM userbot_peers;`
	rows, err := s.DB.QueryContext(ctx, query)
	if err != nil {
		return users, channels, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, hash int64
		var pType string
		if err := rows.Scan(&id, &pType, &hash); err == nil && hash != 0 {
			if pType == "channel" {
				channels[id] = hash
			} else {
				users[id] = hash
			}
		}
	}
	return users, channels, rows.Err()
}

func (s *SQLiteClient) GetDailySendCount(ctx context.Context, ownerID int64, dateWIB string) (int, error) {
	if s == nil || s.DB == nil {
		return 0, nil
	}
	var count int
	err := s.DB.QueryRowContext(ctx,
		`SELECT sent_count FROM daily_send_quota WHERE owner_id = ? AND date_wib = ? LIMIT 1;`,
		ownerID, dateWIB,
	).Scan(&count)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get daily send count: %w", err)
	}
	return count, nil
}

func (s *SQLiteClient) IncrementDailySendCount(ctx context.Context, ownerID int64, dateWIB string) (int, error) {
	if s == nil || s.DB == nil {
		return 0, nil
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO daily_send_quota (owner_id, date_wib, sent_count) VALUES (?, ?, 1)
		ON CONFLICT(owner_id, date_wib) DO UPDATE SET sent_count = sent_count + 1;`,
		ownerID, dateWIB,
	)
	if err != nil {
		return 0, fmt.Errorf("increment daily send count: %w", err)
	}
	return s.GetDailySendCount(ctx, ownerID, dateWIB)
}

func (s *SQLiteClient) ListSudo(ctx context.Context, ownerID int64) ([]int64, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT user_id FROM sudo_users WHERE owner_id = ? ORDER BY user_id ASC;`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list sudo: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan sudo: %w", err)
		}
		ids = append(ids, uid)
	}
	return ids, rows.Err()
}

func (s *SQLiteClient) ClearSudo(ctx context.Context, ownerID int64) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, nil
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sudo_users WHERE owner_id = ?;`, ownerID)
	if err != nil {
		return 0, fmt.Errorf("clear sudo: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *SQLiteClient) AddGlobalMod(ctx context.Context, ownerID int64, kind string, userID int64, label string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	query := `INSERT INTO global_moderation (owner_id, kind, user_id, label)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(owner_id, kind, user_id) DO UPDATE SET
			label = CASE WHEN excluded.label != '' THEN excluded.label ELSE global_moderation.label END;`
	_, err := s.DB.ExecContext(ctx, query, ownerID, kind, userID, label)
	if err != nil {
		return fmt.Errorf("add global mod: %w", err)
	}
	return nil
}

func (s *SQLiteClient) RemoveGlobalMod(ctx context.Context, ownerID int64, kind string, userID int64) error {
	if s == nil || s.DB == nil {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM global_moderation WHERE owner_id = ? AND kind = ? AND user_id = ?;`, ownerID, kind, userID)
	if err != nil {
		return fmt.Errorf("remove global mod: %w", err)
	}
	return nil
}

func (s *SQLiteClient) ListGlobalMod(ctx context.Context, ownerID int64, kind string) ([]LogBlockRecord, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT owner_id, user_id, label, created_at FROM global_moderation WHERE owner_id = ? AND kind = ? ORDER BY user_id ASC;`, ownerID, kind)
	if err != nil {
		return nil, fmt.Errorf("list global mod: %w", err)
	}
	defer rows.Close()
	var records []LogBlockRecord
	for rows.Next() {
		var r LogBlockRecord
		if err := rows.Scan(&r.OwnerID, &r.UserID, &r.Label, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan global mod: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *SQLiteClient) IsGlobalMod(ctx context.Context, ownerID int64, kind string, userID int64) (bool, error) {
	if s == nil || s.DB == nil {
		return false, nil
	}
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM global_moderation WHERE owner_id = ? AND kind = ? AND user_id = ?;`, ownerID, kind, userID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check global mod: %w", err)
	}
	return count > 0, nil
}
