package databases

import (
	"context"
	"database/sql"
	"encoding/json"
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

func NewSQLiteClient(path string) (*SQLiteClient, error) {
	if path == "" {
		path = "userbot.db"
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create dir for sqlite: %w", err)
		}
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-2000)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(0)&_pragma=wal_autocheckpoint(1000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite: %w", err)
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
			active_token_sha TEXT DEFAULT '',
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS token_profiles (
			token_sha TEXT PRIMARY KEY,
			owner_chat_id INTEGER NOT NULL,
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
			notifikasi TEXT DEFAULT 'Aktif · Kirim ke Saya',
			email TEXT DEFAULT 'Belum diatur',
			admin TEXT DEFAULT 'Belum diatur',
			watermark TEXT DEFAULT '',
			watermark_cache_id INTEGER DEFAULT 0,
			topic_enabled INTEGER DEFAULT 0,
			lists TEXT DEFAULT '[]',
			groups TEXT DEFAULT '[]',
			topics TEXT DEFAULT '[]',
			topic_lists TEXT DEFAULT '[]',
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS tokens_master (
			token TEXT PRIMARY KEY,
			tier TEXT NOT NULL,
			duration_seconds INTEGER NOT NULL,
			is_shared INTEGER DEFAULT 0,
			claim_count INTEGER DEFAULT 0,
			created_by INTEGER NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			status TEXT DEFAULT 'active'
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
		`CREATE INDEX IF NOT EXISTS idx_licenses_owner ON user_licenses(owner_chat_id);`,
		`CREATE INDEX IF NOT EXISTS idx_licenses_userbot ON user_licenses(userbot_id);`,

		`CREATE TABLE IF NOT EXISTS data_backups (
			code TEXT PRIMARY KEY,
			owner_id INTEGER NOT NULL,
			lists TEXT DEFAULT '[]',
			groups TEXT DEFAULT '[]',
			topics TEXT DEFAULT '[]',
			topic_lists TEXT DEFAULT '[]',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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

	for _, q := range queries {
		if _, err := s.DB.Exec(q); err != nil {
			return err
		}
	}
	_, _ = s.DB.Exec("ALTER TABLE jaseb_configurasi ADD COLUMN account_name TEXT DEFAULT '';")
	_, _ = s.DB.Exec("ALTER TABLE jaseb_configurasi ADD COLUMN topic_lists TEXT DEFAULT '[]';")
	_, _ = s.DB.Exec("ALTER TABLE jaseb_configurasi ADD COLUMN delay_mode TEXT DEFAULT 'manual';")
	_, _ = s.DB.Exec("ALTER TABLE jaseb_configurasi ADD COLUMN delay_all_groups INTEGER DEFAULT 0;")
	_, _ = s.DB.Exec("ALTER TABLE jaseb_configurasi ADD COLUMN skip_floodwait_sec INTEGER DEFAULT 0;")
	_, _ = s.DB.Exec("ALTER TABLE jaseb_configurasi ADD COLUMN active_token_sha TEXT DEFAULT '';")
	_, _ = s.DB.Exec("CREATE INDEX IF NOT EXISTS idx_token_profiles_owner ON token_profiles(owner_chat_id);")
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
	query := `SELECT owner_id, type, delay, COALESCE(delay_mode, 'manual'), COALESCE(delay_all_groups, 0), skip_floodwait, COALESCE(skip_floodwait_sec, 0), timer, extra, mode, safemode, status,
		masa_aktif, notifikasi, email, admin, watermark, watermark_cache_id, topic_enabled,
		tindakan_terkini, tindakan_terakhir, lists, groups, topics, COALESCE(topic_lists, '[]'), COALESCE(active_token_sha, ''), updated_at
		FROM jaseb_configurasi WHERE owner_id = ? LIMIT 1;`

	var c JasebConfig
	var sfw, sfm, tpe int
	var listsJSON, groupsJSON, topicsJSON, topicListsJSON string

	err := s.DB.QueryRowContext(ctx, query, ownerID).Scan(
		&c.OwnerID, &c.Type, &c.Delay, &c.DelayMode, &c.DelayAllGroups, &sfw, &c.SkipFloodWaitSec, &c.Timer, &c.Extra, &c.Mode, &sfm, &c.Status,
		&c.MasaAktif, &c.Notifikasi, &c.Email, &c.Admin, &c.Watermark, &c.WatermarkCacheID, &tpe,
		&c.TindakanTerkini, &c.TindakanTerakhir, &listsJSON, &groupsJSON, &topicsJSON, &topicListsJSON, &c.ActiveTokenSHA, &c.UpdatedAt,
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
	if topicListsJSON != "" {
		_ = json.Unmarshal([]byte(topicListsJSON), &c.TopicLists)
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
	if c.TopicLists == nil {
		c.TopicLists = []TopicListEntry{}
	}

	return &c, nil
}

func (s *SQLiteClient) SaveConfig(ctx context.Context, c *JasebConfig) error {
	query := `INSERT INTO jaseb_configurasi (
		owner_id, type, delay, delay_mode, delay_all_groups, skip_floodwait, skip_floodwait_sec, timer, extra, mode, safemode, status,
		masa_aktif, notifikasi, email, admin, watermark, watermark_cache_id, topic_enabled,
		tindakan_terkini, tindakan_terakhir, lists, groups, topics, topic_lists, active_token_sha, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(owner_id) DO UPDATE SET
		type = excluded.type,
		delay = excluded.delay,
		delay_mode = excluded.delay_mode,
		delay_all_groups = excluded.delay_all_groups,
		skip_floodwait = excluded.skip_floodwait,
		skip_floodwait_sec = excluded.skip_floodwait_sec,
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
		topic_lists = excluded.topic_lists,
		active_token_sha = excluded.active_token_sha,
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
	delayMode := strings.TrimSpace(c.DelayMode)
	if delayMode == "" {
		delayMode = "manual"
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = time.Now()
	}

	listsBytes, _ := json.Marshal(c.Lists)
	groupsBytes, _ := json.Marshal(c.Groups)
	topicsBytes, _ := json.Marshal(c.Topics)
	topicListsBytes, _ := json.Marshal(c.TopicLists)

	err := s.execWithBusyRetry(ctx, func() error {
		_, execErr := s.DB.ExecContext(ctx, query,
			c.OwnerID, c.Type, c.Delay, delayMode, c.DelayAllGroups, sfw, c.SkipFloodWaitSec, c.Timer, c.Extra, c.Mode, sfm, c.Status,
			c.MasaAktif, c.Notifikasi, c.Email, c.Admin, c.Watermark, c.WatermarkCacheID, tpe,
			c.TindakanTerkini, c.TindakanTerakhir, string(listsBytes), string(groupsBytes), string(topicsBytes), string(topicListsBytes), c.ActiveTokenSHA, c.UpdatedAt,
		)
		return execErr
	})
	return err
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

func (s *SQLiteClient) SaveTokenMaster(ctx context.Context, tm *TokenMaster) error {
	query := `INSERT INTO tokens_master (token, tier, duration_seconds, is_shared, claim_count, created_by, created_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(token) DO UPDATE SET
			duration_seconds = excluded.duration_seconds,
			claim_count = excluded.claim_count,
			status = excluded.status;`
	isShared := 0
	if tm.IsShared {
		isShared = 1
	}
	if tm.CreatedAt.IsZero() {
		tm.CreatedAt = time.Now()
	}
	_, err := s.DB.ExecContext(ctx, query, tm.Token, tm.Tier, tm.DurationSeconds, isShared, tm.ClaimCount, tm.CreatedBy, tm.CreatedAt, tm.Status)
	return err
}

func (s *SQLiteClient) GetTokenMaster(ctx context.Context, token string) (*TokenMaster, error) {
	query := `SELECT token, tier, duration_seconds, is_shared, claim_count, created_by, created_at, status
		FROM tokens_master WHERE token = ? LIMIT 1;`
	var tm TokenMaster
	var isShared int
	err := s.DB.QueryRowContext(ctx, query, token).Scan(
		&tm.Token, &tm.Tier, &tm.DurationSeconds, &isShared, &tm.ClaimCount, &tm.CreatedBy, &tm.CreatedAt, &tm.Status,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tm.IsShared = isShared == 1
	return &tm, nil
}

func (s *SQLiteClient) GetActiveSharedFreeToken(ctx context.Context) (*TokenMaster, error) {
	query := `SELECT token, tier, duration_seconds, is_shared, claim_count, created_by, created_at, status
		FROM tokens_master WHERE is_shared = 1 AND status = 'active' ORDER BY created_at DESC LIMIT 1;`
	var tm TokenMaster
	var isShared int
	err := s.DB.QueryRowContext(ctx, query).Scan(
		&tm.Token, &tm.Tier, &tm.DurationSeconds, &isShared, &tm.ClaimCount, &tm.CreatedBy, &tm.CreatedAt, &tm.Status,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tm.IsShared = isShared == 1
	return &tm, nil
}

func (s *SQLiteClient) UpdateTokenMasterDuration(ctx context.Context, token string, durationSeconds int64) error {
	query := `UPDATE tokens_master SET duration_seconds = ? WHERE token = ?;`
	_, err := s.DB.ExecContext(ctx, query, durationSeconds, token)
	return err
}

func (s *SQLiteClient) IncrementTokenMasterClaim(ctx context.Context, token string) error {
	query := `UPDATE tokens_master SET claim_count = claim_count + 1 WHERE token = ?;`
	_, err := s.DB.ExecContext(ctx, query, token)
	return err
}

func (s *SQLiteClient) SaveUserLicense(ctx context.Context, ul *UserLicense) error {
	query := `INSERT INTO user_licenses (token_sha, master_token, owner_chat_id, userbot_id, tier, can_custom_watermark, expires_at, created_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(token_sha) DO UPDATE SET
			master_token = excluded.master_token,
			owner_chat_id = excluded.owner_chat_id,
			userbot_id = excluded.userbot_id,
			tier = excluded.tier,
			can_custom_watermark = excluded.can_custom_watermark,
			expires_at = excluded.expires_at,
			status = excluded.status;`
	canWM := 0
	if ul.CanCustomWatermark {
		canWM = 1
	}
	if ul.CreatedAt.IsZero() {
		ul.CreatedAt = time.Now()
	}
	_, err := s.DB.ExecContext(ctx, query, ul.TokenSHA, ul.MasterToken, ul.OwnerChatID, ul.UserbotID, ul.Tier, canWM, ul.ExpiresAt, ul.CreatedAt, ul.Status)
	return err
}

func (s *SQLiteClient) GetUserLicenseBySHA(ctx context.Context, tokenSHA string) (*UserLicense, error) {
	query := `SELECT token_sha, master_token, owner_chat_id, userbot_id, tier, can_custom_watermark, expires_at, created_at, status
		FROM user_licenses WHERE token_sha = ? LIMIT 1;`
	var ul UserLicense
	var canWM int
	err := s.DB.QueryRowContext(ctx, query, tokenSHA).Scan(
		&ul.TokenSHA, &ul.MasterToken, &ul.OwnerChatID, &ul.UserbotID, &ul.Tier, &canWM, &ul.ExpiresAt, &ul.CreatedAt, &ul.Status,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ul.CanCustomWatermark = canWM == 1
	return &ul, nil
}

func (s *SQLiteClient) GetActiveUserLicense(ctx context.Context, ownerChatID int64) (*UserLicense, error) {
	list, err := s.ListActiveUserLicenses(ctx, ownerChatID)
	if err != nil {
		return nil, err
	}
	activeSHA, _ := s.GetActiveTokenSHA(ctx, ownerChatID)
	return selectActiveLicense(list, activeSHA), nil
}

func (s *SQLiteClient) GetLatestUserLicense(ctx context.Context, ownerChatID int64) (*UserLicense, error) {
	query := `SELECT token_sha, master_token, owner_chat_id, userbot_id, tier, can_custom_watermark, expires_at, created_at, status
		FROM user_licenses WHERE owner_chat_id = ? OR userbot_id = ?
		ORDER BY expires_at DESC LIMIT 1;`
	var ul UserLicense
	var canWM int
	err := s.DB.QueryRowContext(ctx, query, ownerChatID, ownerChatID).Scan(
		&ul.TokenSHA, &ul.MasterToken, &ul.OwnerChatID, &ul.UserbotID, &ul.Tier, &canWM, &ul.ExpiresAt, &ul.CreatedAt, &ul.Status,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ul.CanCustomWatermark = canWM == 1
	return &ul, nil
}

func (s *SQLiteClient) ListExpiredUserLicenses(ctx context.Context, now time.Time) ([]*UserLicense, error) {
	query := `SELECT token_sha, master_token, owner_chat_id, userbot_id, tier, can_custom_watermark, expires_at, created_at, status
		FROM user_licenses WHERE status = 'active';`
	rows, err := s.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expired []*UserLicense
	for rows.Next() {
		var ul UserLicense
		var canWM int
		if scanErr := rows.Scan(
			&ul.TokenSHA, &ul.MasterToken, &ul.OwnerChatID, &ul.UserbotID, &ul.Tier, &canWM, &ul.ExpiresAt, &ul.CreatedAt, &ul.Status,
		); scanErr != nil {
			continue
		}
		ul.CanCustomWatermark = canWM == 1
		if !now.Before(ul.ExpiresAt) {
			copyUL := ul
			expired = append(expired, &copyUL)
		}
	}
	return expired, rows.Err()
}

func (s *SQLiteClient) MarkUserLicenseExpired(ctx context.Context, tokenSHA string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE user_licenses SET status = 'expired' WHERE token_sha = ?;`, tokenSHA)
	return err
}

func (s *SQLiteClient) MarkTokenMasterExpired(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE tokens_master SET status = 'expired' WHERE token = ? AND is_shared = 0;`, token)
	return err
}

func (s *SQLiteClient) HasUsedMasterToken(ctx context.Context, masterToken string) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM user_licenses WHERE master_token = ?;`, masterToken).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *SQLiteClient) RelinkUserData(ctx context.Context, oldOwnerID, newOwnerID int64) error {
	if oldOwnerID == 0 || newOwnerID == 0 || oldOwnerID == newOwnerID {
		return nil
	}
	var oldCfgCount int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM jaseb_configurasi WHERE owner_id = ?;`, oldOwnerID).Scan(&oldCfgCount); err == nil && oldCfgCount > 0 {
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM jaseb_configurasi WHERE owner_id = ?;`, newOwnerID)
		_, _ = s.DB.ExecContext(ctx, `UPDATE jaseb_configurasi SET owner_id = ? WHERE owner_id = ?;`, newOwnerID, oldOwnerID)
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE user_licenses SET owner_chat_id = ? WHERE owner_chat_id = ?;`, newOwnerID, oldOwnerID)
	_, _ = s.DB.ExecContext(ctx, `UPDATE user_licenses SET userbot_id = ? WHERE userbot_id = ?;`, newOwnerID, oldOwnerID)
	return nil
}

func (s *SQLiteClient) SupersedeOtherUserLicenses(ctx context.Context, ownerChatID int64, keepTokenSHA string) error {
	if ownerChatID == 0 {
		return nil
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE user_licenses SET status = 'superseded' WHERE (owner_chat_id = ? OR userbot_id = ?) AND status = 'active' AND token_sha != ?;`,
		ownerChatID, ownerChatID, keepTokenSHA,
	)
	return err
}

func (s *SQLiteClient) MarkUserLicenseStatus(ctx context.Context, tokenSHA, status string) error {
	if tokenSHA == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE user_licenses SET status = ? WHERE token_sha = ?;`, status, tokenSHA)
	return err
}

func (s *SQLiteClient) RevokeTokenMaster(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE tokens_master SET status = 'revoked' WHERE token = ?;`, token)
	return err
}

func (s *SQLiteClient) SaveDataBackup(ctx context.Context, b *DataBackupCode) error {
	if b == nil || b.Code == "" {
		return nil
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now()
	}
	listsBytes, _ := json.Marshal(b.Lists)
	groupsBytes, _ := json.Marshal(b.Groups)
	topicsBytes, _ := json.Marshal(b.Topics)
	topicListsBytes, _ := json.Marshal(b.TopicLists)
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO data_backups (code, owner_id, lists, groups, topics, topic_lists, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET
			owner_id = excluded.owner_id,
			lists = excluded.lists,
			groups = excluded.groups,
			topics = excluded.topics,
			topic_lists = excluded.topic_lists,
			created_at = excluded.created_at;`,
		b.Code, b.OwnerID, string(listsBytes), string(groupsBytes), string(topicsBytes), string(topicListsBytes), b.CreatedAt,
	)
	return err
}

func (s *SQLiteClient) GetDataBackup(ctx context.Context, code string) (*DataBackupCode, error) {
	var b DataBackupCode
	var listsJSON, groupsJSON, topicsJSON, topicListsJSON string
	err := s.DB.QueryRowContext(ctx,
		`SELECT code, owner_id, lists, groups, topics, COALESCE(topic_lists, '[]'), created_at FROM data_backups WHERE code = ? LIMIT 1;`,
		code,
	).Scan(&b.Code, &b.OwnerID, &listsJSON, &groupsJSON, &topicsJSON, &topicListsJSON, &b.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if listsJSON != "" {
		_ = json.Unmarshal([]byte(listsJSON), &b.Lists)
	}
	if groupsJSON != "" {
		_ = json.Unmarshal([]byte(groupsJSON), &b.Groups)
	}
	if topicsJSON != "" {
		_ = json.Unmarshal([]byte(topicsJSON), &b.Topics)
	}
	if topicListsJSON != "" {
		_ = json.Unmarshal([]byte(topicListsJSON), &b.TopicLists)
	}
	return &b, nil
}

type AdminAccountRow struct {
	OwnerID     int64
	Admin       string
	AccountName string
}

func (s *SQLiteClient) GetAllConfigsWithAdmin(ctx context.Context) ([]AdminAccountRow, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT owner_id, admin, account_name FROM jaseb_configurasi WHERE admin != 'Belum diatur' AND admin != '';`)
	if err != nil {
		fallbackRows, fErr := s.DB.QueryContext(ctx, `SELECT owner_id, admin FROM jaseb_configurasi WHERE admin != 'Belum diatur' AND admin != '';`)
		if fErr != nil {
			return nil, fErr
		}
		defer fallbackRows.Close()
		var list []AdminAccountRow
		for fallbackRows.Next() {
			var r AdminAccountRow
			if scanErr := fallbackRows.Scan(&r.OwnerID, &r.Admin); scanErr == nil {
				list = append(list, r)
			}
		}
		return list, nil
	}
	defer rows.Close()

	var list []AdminAccountRow
	for rows.Next() {
		var r AdminAccountRow
		if scanErr := rows.Scan(&r.OwnerID, &r.Admin, &r.AccountName); scanErr == nil {
			list = append(list, r)
		}
	}
	return list, nil
}

func (s *SQLiteClient) UpdateAccountName(ctx context.Context, ownerID int64, name string) error {
	if name == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE jaseb_configurasi SET account_name = ? WHERE owner_id = ?;`, name, ownerID)
	return err
}

func (s *SQLiteClient) GetAccountName(ctx context.Context, ownerID int64) string {
	var name string
	_ = s.DB.QueryRowContext(ctx, `SELECT account_name FROM jaseb_configurasi WHERE owner_id = ?;`, ownerID).Scan(&name)
	return name
}

func (s *SQLiteClient) Close() error {
	if s.DB != nil {
		return s.DB.Close()
	}
	return nil
}
