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

func NewSQLiteClient(path string) (*SQLiteClient, error) {
	if path == "" {
		path = "userbot.db"
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create dir for sqlite: %w", err)
		}
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(1 * time.Hour)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite: %w", err)
	}

	client := &SQLiteClient{DB: db}
	if err := client.initTables(); err != nil {
		return nil, fmt.Errorf("failed to init sqlite tables: %w", err)
	}

	log.Printf("📂 Gateway SQLite connected at: %s", path)
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

	for _, q := range queries {
		if _, err := s.DB.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteClient) HasSession(ctx context.Context, ownerID int64) (bool, error) {
	query := `SELECT COUNT(*) FROM sessions WHERE owner_id = ? OR chat_id = ? OR account_id = ?;`
	var count int
	err := s.DB.QueryRowContext(ctx, query, ownerID, ownerID, ownerID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *SQLiteClient) GetConfig(ctx context.Context, ownerID int64) (*UserConfig, error) {
	query := `SELECT owner_id, type, delay, skip_floodwait, timer, extra, mode, safemode, status,
		masa_aktif, notifikasi, email, admin, watermark, topic_enabled, tindakan_terkini, tindakan_terakhir, updated_at
		FROM userbot_configs WHERE owner_id = ? LIMIT 1;`

	var c UserConfig
	var sfw, sfm, tpe int
	err := s.DB.QueryRowContext(ctx, query, ownerID).Scan(
		&c.OwnerID, &c.Type, &c.Delay, &sfw, &c.Timer, &c.Extra, &c.Mode, &sfm, &c.Status,
		&c.MasaAktif, &c.Notifikasi, &c.Email, &c.Admin, &c.Watermark, &tpe, &c.TindakanTerkini, &c.TindakanTerakhir, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.SkipFloodWait = sfw == 1
	c.Safemode = sfm == 1
	c.TopicEnabled = tpe == 1
	return &c, nil
}

func (s *SQLiteClient) SaveConfig(ctx context.Context, c *UserConfig) error {
	query := `INSERT INTO userbot_configs (
		owner_id, type, delay, skip_floodwait, timer, extra, mode, safemode, status,
		masa_aktif, notifikasi, email, admin, watermark, topic_enabled, tindakan_terkini, tindakan_terakhir, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(owner_id) DO UPDATE SET
		type = excluded.type,
		delay = excluded.delay,
		skip_floodwait = excluded.skip_floodwait,
		timer = excluded.timer,
		extra = excluded.extra,
		mode = excluded.mode,
		safemode = excluded.safemode,
		status = excluded.status,
		masa_aktif = excluded.masa_aktif,
		notifikasi = excluded.notifikasi,
		email = excluded.email,
		admin = excluded.admin,
		watermark = excluded.watermark,
		topic_enabled = excluded.topic_enabled,
		tindakan_terkini = excluded.tindakan_terkini,
		tindakan_terakhir = excluded.tindakan_terakhir,
		updated_at = excluded.updated_at;`

	sfw := 0
	if c.SkipFloodWait {
		sfw = 1
	}
	sfm := 0
	if c.Safemode {
		sfm = 1
	}
	tpe := 0
	if c.TopicEnabled {
		tpe = 1
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = time.Now()
	}

	_, err := s.DB.ExecContext(ctx, query,
		c.OwnerID, c.Type, c.Delay, sfw, c.Timer, c.Extra, c.Mode, sfm, c.Status,
		c.MasaAktif, c.Notifikasi, c.Email, c.Admin, c.Watermark, tpe, c.TindakanTerkini, c.TindakanTerakhir, c.UpdatedAt,
	)
	return err
}

func (s *SQLiteClient) AddList(ctx context.Context, list *UserList) error {
	query := `INSERT INTO userbot_lists (mongo_id, owner_id, text, media_type, created_at) VALUES (?, ?, ?, ?, ?);`
	if list.CreatedAt.IsZero() {
		list.CreatedAt = time.Now()
	}
	res, err := s.DB.ExecContext(ctx, query, list.MongoID, list.OwnerID, list.Text, list.MediaType, list.CreatedAt)
	if err != nil {
		return err
	}
	list.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteClient) GetLists(ctx context.Context, ownerID int64) ([]UserList, error) {
	query := `SELECT id, mongo_id, owner_id, text, media_type, created_at FROM userbot_lists WHERE owner_id = ? ORDER BY id ASC;`
	rows, err := s.DB.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UserList
	for rows.Next() {
		var l UserList
		if err := rows.Scan(&l.ID, &l.MongoID, &l.OwnerID, &l.Text, &l.MediaType, &l.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, nil
}

func (s *SQLiteClient) CountLists(ctx context.Context, ownerID int64) (int, error) {
	query := `SELECT COUNT(*) FROM userbot_lists WHERE owner_id = ?;`
	var count int
	err := s.DB.QueryRowContext(ctx, query, ownerID).Scan(&count)
	return count, err
}

func (s *SQLiteClient) DeleteList(ctx context.Context, ownerID int64, id int64) error {
	query := `DELETE FROM userbot_lists WHERE owner_id = ? AND id = ?;`
	_, err := s.DB.ExecContext(ctx, query, ownerID, id)
	return err
}

func (s *SQLiteClient) DeleteListByMongoID(ctx context.Context, ownerID int64, mongoID string) error {
	query := `DELETE FROM userbot_lists WHERE owner_id = ? AND mongo_id = ?;`
	_, err := s.DB.ExecContext(ctx, query, ownerID, mongoID)
	return err
}

func (s *SQLiteClient) ClearLists(ctx context.Context, ownerID int64) error {
	query := `DELETE FROM userbot_lists WHERE owner_id = ?;`
	_, err := s.DB.ExecContext(ctx, query, ownerID)
	return err
}

func (s *SQLiteClient) AddGroup(ctx context.Context, g *UserGroup) error {
	query := `INSERT INTO userbot_groups (mongo_id, owner_id, target, title, chat_id, created_at) VALUES (?, ?, ?, ?, ?, ?);`
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now()
	}
	res, err := s.DB.ExecContext(ctx, query, g.MongoID, g.OwnerID, g.Target, g.Title, g.ChatID, g.CreatedAt)
	if err != nil {
		return err
	}
	g.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteClient) GetGroups(ctx context.Context, ownerID int64) ([]UserGroup, error) {
	query := `SELECT id, mongo_id, owner_id, target, title, chat_id, created_at FROM userbot_groups WHERE owner_id = ? ORDER BY id ASC;`
	rows, err := s.DB.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UserGroup
	for rows.Next() {
		var g UserGroup
		if err := rows.Scan(&g.ID, &g.MongoID, &g.OwnerID, &g.Target, &g.Title, &g.ChatID, &g.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, nil
}

func (s *SQLiteClient) CountGroups(ctx context.Context, ownerID int64) (int, error) {
	query := `SELECT COUNT(*) FROM userbot_groups WHERE owner_id = ?;`
	var count int
	err := s.DB.QueryRowContext(ctx, query, ownerID).Scan(&count)
	return count, err
}

func (s *SQLiteClient) DeleteGroup(ctx context.Context, ownerID int64, id int64) error {
	query := `DELETE FROM userbot_groups WHERE owner_id = ? AND id = ?;`
	_, err := s.DB.ExecContext(ctx, query, ownerID, id)
	return err
}

func (s *SQLiteClient) DeleteGroupByMongoID(ctx context.Context, ownerID int64, mongoID string) error {
	query := `DELETE FROM userbot_groups WHERE owner_id = ? AND mongo_id = ?;`
	_, err := s.DB.ExecContext(ctx, query, ownerID, mongoID)
	return err
}

func (s *SQLiteClient) ClearGroups(ctx context.Context, ownerID int64) error {
	query := `DELETE FROM userbot_groups WHERE owner_id = ?;`
	_, err := s.DB.ExecContext(ctx, query, ownerID)
	return err
}

func (s *SQLiteClient) AddTopic(ctx context.Context, t *UserTopic) error {
	query := `INSERT INTO userbot_topics (mongo_id, owner_id, chat_id, topic_id, title, created_at) VALUES (?, ?, ?, ?, ?, ?);`
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	res, err := s.DB.ExecContext(ctx, query, t.MongoID, t.OwnerID, t.ChatID, t.TopicID, t.Title, t.CreatedAt)
	if err != nil {
		return err
	}
	t.ID, _ = res.LastInsertId()
	return nil
}

func (s *SQLiteClient) GetTopics(ctx context.Context, ownerID int64) ([]UserTopic, error) {
	query := `SELECT id, mongo_id, owner_id, chat_id, topic_id, title, created_at FROM userbot_topics WHERE owner_id = ? ORDER BY id ASC;`
	rows, err := s.DB.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UserTopic
	for rows.Next() {
		var t UserTopic
		if err := rows.Scan(&t.ID, &t.MongoID, &t.OwnerID, &t.ChatID, &t.TopicID, &t.Title, &t.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, nil
}

func (s *SQLiteClient) CountTopics(ctx context.Context, ownerID int64) (int, error) {
	query := `SELECT COUNT(*) FROM userbot_topics WHERE owner_id = ?;`
	var count int
	err := s.DB.QueryRowContext(ctx, query, ownerID).Scan(&count)
	return count, err
}

func (s *SQLiteClient) ClearTopics(ctx context.Context, ownerID int64) error {
	query := `DELETE FROM userbot_topics WHERE owner_id = ?;`
	_, err := s.DB.ExecContext(ctx, query, ownerID)
	return err
}
