package databases

import (
	"context"
	"database/sql"
	"encoding/json"
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

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-8000)&_pragma=temp_store(MEMORY)", path)
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

		`CREATE TABLE IF NOT EXISTS jaseb_configurasi (
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
			watermark_cache_id INTEGER DEFAULT 0,
			topic_enabled INTEGER DEFAULT 0,
			tindakan_terkini TEXT DEFAULT 'Siaran otomatis dimatikan',
			tindakan_terakhir TEXT DEFAULT '-',
			lists TEXT DEFAULT '[]',
			groups TEXT DEFAULT '[]',
			topics TEXT DEFAULT '[]',
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,
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

func (s *SQLiteClient) GetConfig(ctx context.Context, ownerID int64) (*JasebConfig, error) {
	query := `SELECT owner_id, type, delay, skip_floodwait, timer, extra, mode, safemode, status,
		masa_aktif, notifikasi, email, admin, watermark, watermark_cache_id, topic_enabled,
		tindakan_terkini, tindakan_terakhir, lists, groups, topics, updated_at
		FROM jaseb_configurasi WHERE owner_id = ? LIMIT 1;`

	var c JasebConfig
	var sfw, sfm, tpe int
	var listsJSON, groupsJSON, topicsJSON string

	err := s.DB.QueryRowContext(ctx, query, ownerID).Scan(
		&c.OwnerID, &c.Type, &c.Delay, &sfw, &c.Timer, &c.Extra, &c.Mode, &sfm, &c.Status,
		&c.MasaAktif, &c.Notifikasi, &c.Email, &c.Admin, &c.Watermark, &c.WatermarkCacheID, &tpe,
		&c.TindakanTerkini, &c.TindakanTerakhir, &listsJSON, &groupsJSON, &topicsJSON, &c.UpdatedAt,
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

	if listsJSON != "" {
		_ = json.Unmarshal([]byte(listsJSON), &c.Lists)
	}
	if groupsJSON != "" {
		_ = json.Unmarshal([]byte(groupsJSON), &c.Groups)
	}
	if topicsJSON != "" {
		_ = json.Unmarshal([]byte(topicsJSON), &c.Topics)
	}
	if c.Lists == nil {
		c.Lists = []int{}
	}
	if c.Groups == nil {
		c.Groups = []GroupEntry{}
	}
	if c.Topics == nil {
		c.Topics = []TopicEntry{}
	}

	return &c, nil
}

func (s *SQLiteClient) SaveConfig(ctx context.Context, c *JasebConfig) error {
	query := `INSERT INTO jaseb_configurasi (
		owner_id, type, delay, skip_floodwait, timer, extra, mode, safemode, status,
		masa_aktif, notifikasi, email, admin, watermark, watermark_cache_id, topic_enabled,
		tindakan_terkini, tindakan_terakhir, lists, groups, topics, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		watermark_cache_id = excluded.watermark_cache_id,
		topic_enabled = excluded.topic_enabled,
		tindakan_terkini = excluded.tindakan_terkini,
		tindakan_terakhir = excluded.tindakan_terakhir,
		lists = excluded.lists,
		groups = excluded.groups,
		topics = excluded.topics,
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

	listsBytes, _ := json.Marshal(c.Lists)
	groupsBytes, _ := json.Marshal(c.Groups)
	topicsBytes, _ := json.Marshal(c.Topics)

	_, err := s.DB.ExecContext(ctx, query,
		c.OwnerID, c.Type, c.Delay, sfw, c.Timer, c.Extra, c.Mode, sfm, c.Status,
		c.MasaAktif, c.Notifikasi, c.Email, c.Admin, c.Watermark, c.WatermarkCacheID, tpe,
		c.TindakanTerkini, c.TindakanTerakhir, string(listsBytes), string(groupsBytes), string(topicsBytes), c.UpdatedAt,
	)
	return err
}
