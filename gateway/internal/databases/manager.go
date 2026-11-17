package databases

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConfigUpdateEvent adalah payload yang dipublish ubot ke Redis Pub/Sub
// saat ada perubahan di jaseb_configurasi MongoDB.
type ConfigUpdateEvent struct {
	OwnerID int64 `json:"owner_id"`
}

// cloneJasebConfig membuat salinan mendalam (deep copy) dari JasebConfig
// termasuk slice Lists, Groups, dan Topics untuk mengeliminasi race condition saat mutasi.
func cloneJasebConfig(cfg *JasebConfig) *JasebConfig {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	if cfg.Lists != nil {
		cp.Lists = make([]int, len(cfg.Lists))
		copy(cp.Lists, cfg.Lists)
	}
	if cfg.Groups != nil {
		cp.Groups = make([]GroupEntry, len(cfg.Groups))
		copy(cp.Groups, cfg.Groups)
	}
	if cfg.Topics != nil {
		cp.Topics = make([]TopicEntry, len(cfg.Topics))
		copy(cp.Topics, cfg.Topics)
	}
	return &cp
}

type DataManager struct {
	sqlite          *SQLiteClient
	mu              sync.RWMutex
	mongo           *MongoClient
	mongoSaveSem    chan struct{}
	pendingMongoCfg map[int64]*JasebConfig
	inFlightMongo   map[int64]bool
}

func (dm *DataManager) setMongo(mc *MongoClient) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.mongo = mc
}

func (dm *DataManager) getMongo() *MongoClient {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.mongo
}

// NewDataManager creates a DataManager dengan SQLite (primary) dan MongoDB (backup, retry di background).
// bgCtx: parent context panjang untuk retry MongoDB.
// initCtx: context pendek hanya untuk inisialisasi SQLite.
func NewDataManager(bgCtx context.Context, initCtx context.Context, mongoURI, mongoDBName, sqlitePath string) (*DataManager, error) {
	dm := &DataManager{
		mongoSaveSem:    make(chan struct{}, 8),
		pendingMongoCfg: make(map[int64]*JasebConfig),
		inFlightMongo:   make(map[int64]bool),
	}

	// Connect SQLite (harus sukses — primary DB)
	sClient, err := NewSQLiteClient(sqlitePath)
	if err != nil {
		log.Printf("⚠️ SQLite connection failed: %v", err)
	}
	dm.sqlite = sClient

	// Connect MongoDB di background dengan retry
	if mongoURI != "" {
		go func() {
			mClient, retryErr := NewMongoClient(bgCtx, mongoURI, mongoDBName)
			if retryErr != nil {
				log.Printf("⚠️ MongoDB permanently failed (context cancelled): %v", retryErr)
				return
			}
			dm.setMongo(mClient)
			log.Printf("🍃 MongoDB is now active in DataManager (on-demand per-user cache)")
		}()
	}

	if sClient == nil && mongoURI == "" {
		return nil, fmt.Errorf("both sqlite and mongodb are unavailable")
	}

	_ = initCtx
	return dm, nil
}

// SyncOneFromMongoDB menarik 1 dokumen ownerID dari MongoDB ke SQLite.
// Dipanggil saat menerima event config:updated dari Redis Pub/Sub,
// atau saat user belum ada di cache SQLite lokal.
func (dm *DataManager) SyncOneFromMongoDB(ctx context.Context, ownerID int64) *JasebConfig {
	mg := dm.getMongo()
	if mg == nil {
		return nil
	}
	mongoCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cfg, err := mg.GetConfig(mongoCtx, ownerID)
	if err != nil || cfg == nil {
		if err != nil {
			log.Printf("⚠️ SyncOneFromMongoDB ownerID=%d: %v", ownerID, err)
		}
		return nil
	}
	if dm.sqlite != nil {
		if saveErr := dm.sqlite.SaveConfig(ctx, cfg); saveErr != nil {
			log.Printf("⚠️ SyncOneFromMongoDB ownerID=%d simpan SQLite: %v", ownerID, saveErr)
		} else {
			log.Printf("🔄 SyncOneFromMongoDB: ownerID=%d berhasil di-cache ke SQLite", ownerID)
		}
	}
	return cfg
}

// WatchConfigUpdates subscribe ke Redis Pub/Sub channel "config:updated".
// Saat ubot mengubah MongoDB, ubot publish ownerID ke channel ini.
// Gateway lalu fetch ulang 1 doc user tersebut dari MongoDB dan update SQLite.
func (dm *DataManager) WatchConfigUpdates(ctx context.Context, rdb *redis.Client) {
	const channel = "config:updated"
	pubsub := rdb.Subscribe(ctx, channel)
	defer pubsub.Close()

	log.Printf("👂 Gateway watching Redis channel: %s", channel)
	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var evt ConfigUpdateEvent
			if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
				log.Printf("⚠️ config:updated bad payload: %v", err)
				continue
			}
			go dm.SyncOneFromMongoDB(ctx, evt.OwnerID)
		}
	}
}

func (m *DataManager) GetSQLiteDB() *sql.DB {
	if m != nil && m.sqlite != nil {
		return m.sqlite.DB
	}
	return nil
}

func (m *DataManager) HasActiveSession(ctx context.Context, ownerID int64) bool {
	// 1. Cek SQLite dulu (lokal, instan)
	if m.sqlite != nil {
		has, err := m.sqlite.HasSession(ctx, ownerID)
		if err == nil && has {
			return true
		}
	}
	// 2. Fallback per-user ke MongoDB jika belum ada di SQLite
	if mg := m.getMongo(); mg != nil {
		mongoCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		has, err := mg.HasSession(mongoCtx, ownerID)
		if err == nil && has {
			return true
		}
	}
	return false
}

func (m *DataManager) GetUserConfig(ctx context.Context, ownerID int64) *JasebConfig {
	// 1. Cek SQLite lokal dulu (cache) — jika ada, langsung kembalikan (0 hit ke MongoDB)
	if m.sqlite != nil {
		cfg, err := m.sqlite.GetConfig(ctx, ownerID)
		if err == nil && cfg != nil {
			return cfg
		}
	}

	// 2. Cache miss di SQLite: ambil hanya untuk user ini dari MongoDB (1x hit per user)
	if cfg := m.SyncOneFromMongoDB(ctx, ownerID); cfg != nil {
		return cfg
	}

	// 3. Konfigurasi default jika belum terdaftar di MongoDB maupun SQLite
	defaultCfg := &JasebConfig{
		OwnerID:          ownerID,
		Type:             "Broadcast",
		Delay:            5,
		SkipFloodWait:    false,
		Timer:            "Belum diatur",
		Extra:            0,
		Mode:             "Normal",
		Safemode:         false,
		Status:           "Berhenti",
		MasaAktif:        "Belum diatur",
		Notifikasi:       "Aktif · Kirim ke Saya",
		Email:            "Belum diatur",
		Admin:            "Belum diatur",
		Watermark:        "",
		WatermarkCacheID: 0,
		TopicEnabled:     false,
		TindakanTerkini:  "Siaran otomatis dimatikan",
		TindakanTerakhir: "-",
		Lists:            []int{},
		Groups:           []GroupEntry{},
		Topics:           []TopicEntry{},
		UpdatedAt:        time.Now(),
	}

	_ = m.SaveUserConfig(ctx, defaultCfg)
	return defaultCfg
}

// SaveUserConfig: simpan SQLite langsung (synchronous), MongoDB di goroutine background (non-blocking) dengan deep copy dan coalescing per-owner.
func (m *DataManager) SaveUserConfig(ctx context.Context, cfg *JasebConfig) error {
	cfg.UpdatedAt = time.Now()

	// 1. Simpan ke SQLite lokal dulu — langsung, tidak memblokir response panel
	if m.sqlite != nil {
		if err := m.sqlite.SaveConfig(ctx, cfg); err != nil {
			log.Printf("⚠️ SQLite SyncConfig error: %v", err)
			return err
		}
	}

	// 2. Simpan ke MongoDB di background — deep copy + per-owner coalescing
	if mg := m.getMongo(); mg != nil {
		cfgCopy := cloneJasebConfig(cfg)
		ownerID := cfgCopy.OwnerID

		m.mu.Lock()
		if m.pendingMongoCfg == nil {
			m.pendingMongoCfg = make(map[int64]*JasebConfig)
		}
		if m.inFlightMongo == nil {
			m.inFlightMongo = make(map[int64]bool)
		}
		m.pendingMongoCfg[ownerID] = cfgCopy

		if m.inFlightMongo[ownerID] {
			m.mu.Unlock()
			return nil
		}
		m.inFlightMongo[ownerID] = true
		m.mu.Unlock()

		go func(oid int64) {
			for {
				m.mu.Lock()
				curCfg, ok := m.pendingMongoCfg[oid]
				if !ok || curCfg == nil {
					delete(m.inFlightMongo, oid)
					m.mu.Unlock()
					return
				}
				delete(m.pendingMongoCfg, oid)
				m.mu.Unlock()

				currentMg := m.getMongo()
				if currentMg != nil {
					if m.mongoSaveSem != nil {
						m.mongoSaveSem <- struct{}{}
					}
					mongoCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					if err := currentMg.SaveConfig(mongoCtx, curCfg); err != nil {
						log.Printf("⚠️ MongoDB SaveConfig error (owner=%d): %v", oid, err)
					}
					cancel()
					if m.mongoSaveSem != nil {
						<-m.mongoSaveSem
					}
				}

				m.mu.Lock()
				if _, hasMore := m.pendingMongoCfg[oid]; !hasMore {
					delete(m.inFlightMongo, oid)
					m.mu.Unlock()
					return
				}
				m.mu.Unlock()
			}
		}(ownerID)
	}

	return nil
}

// AddListCacheID: Menyimpan ID pesan cache AiotdCaches ke jaseb_configurasi
func (m *DataManager) AddListCacheID(ctx context.Context, ownerID int64, cacheMsgID int) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	// Hindari duplikasi jika sudah ada
	for _, id := range cfg.Lists {
		if id == cacheMsgID {
			return nil
		}
	}
	cfg.Lists = append(cfg.Lists, cacheMsgID)
	return m.SaveUserConfig(ctx, cfg)
}

func (m *DataManager) GetLists(ctx context.Context, ownerID int64) []int {
	cfg := m.GetUserConfig(ctx, ownerID)
	return cfg.Lists
}

func (m *DataManager) DeleteListByIndex(ctx context.Context, ownerID int64, index int) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	if index < 0 || index >= len(cfg.Lists) {
		return fmt.Errorf("index out of range")
	}
	cfg.Lists = append(cfg.Lists[:index], cfg.Lists[index+1:]...)
	return m.SaveUserConfig(ctx, cfg)
}

func (m *DataManager) ClearLists(ctx context.Context, ownerID int64) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	cfg.Lists = []int{}
	return m.SaveUserConfig(ctx, cfg)
}

// AddGroup: Menambahkan target grup ke array groups di jaseb_configurasi
func (m *DataManager) AddGroup(ctx context.Context, ownerID int64, target, title string, chatID int64) error {
	cfg := m.GetUserConfig(ctx, ownerID)

	cleanTarget := strings.TrimSpace(target)
	cleanChatID := chatID
	peerType := "channel"

	if strings.HasPrefix(cleanTarget, "-100") {
		peerType = "channel"
		if id, err := strconv.ParseInt(strings.TrimPrefix(cleanTarget, "-100"), 10, 64); err == nil && id > 0 {
			cleanChatID = id
		}
	} else if strings.HasPrefix(cleanTarget, "-") {
		peerType = "chat"
		if id, err := strconv.ParseInt(strings.TrimPrefix(cleanTarget, "-"), 10, 64); err == nil && id > 0 {
			cleanChatID = id
		}
	} else if cleanChatID > 0 && cleanChatID < 1000000000 {
		peerType = "chat"
	}

	for _, g := range cfg.Groups {
		if g.Target == cleanTarget || (cleanChatID != 0 && g.ChatID == cleanChatID) {
			return nil // sudah ada
		}
	}
	cfg.Groups = append(cfg.Groups, GroupEntry{
		Target:   cleanTarget,
		Title:    title,
		ChatID:   cleanChatID,
		PeerType: peerType,
	})
	return m.SaveUserConfig(ctx, cfg)
}

func (m *DataManager) GetGroups(ctx context.Context, ownerID int64) []GroupEntry {
	cfg := m.GetUserConfig(ctx, ownerID)
	return cfg.Groups
}

func (m *DataManager) DeleteGroupByIndex(ctx context.Context, ownerID int64, index int) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	if index < 0 || index >= len(cfg.Groups) {
		return fmt.Errorf("index out of range")
	}
	cfg.Groups = append(cfg.Groups[:index], cfg.Groups[index+1:]...)
	return m.SaveUserConfig(ctx, cfg)
}

func (m *DataManager) ClearGroups(ctx context.Context, ownerID int64) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	cfg.Groups = []GroupEntry{}
	return m.SaveUserConfig(ctx, cfg)
}

// AddTopic: Menambahkan forum topic thread ke array topics di jaseb_configurasi
func (m *DataManager) AddTopic(ctx context.Context, ownerID int64, chatID int64, topicID int, title string) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	for _, t := range cfg.Topics {
		if t.ChatID == chatID && t.TopicID == topicID {
			return nil
		}
	}
	cfg.Topics = append(cfg.Topics, TopicEntry{
		ChatID:  chatID,
		TopicID: topicID,
		Title:   title,
	})
	return m.SaveUserConfig(ctx, cfg)
}

func (m *DataManager) GetTopics(ctx context.Context, ownerID int64) []TopicEntry {
	cfg := m.GetUserConfig(ctx, ownerID)
	return cfg.Topics
}

func (m *DataManager) ClearTopics(ctx context.Context, ownerID int64) error {
	cfg := m.GetUserConfig(ctx, ownerID)
	cfg.Topics = []TopicEntry{}
	return m.SaveUserConfig(ctx, cfg)
}

// CountGroupsAndLists mengembalikan jumlah grup dan list yang tersimpan di jaseb_configurasi
func (m *DataManager) CountGroupsAndLists(ctx context.Context, ownerID int64) (int, int) {
	cfg := m.GetUserConfig(ctx, ownerID)
	if cfg == nil {
		return 0, 0
	}
	return len(cfg.Groups), len(cfg.Lists)
}

func (m *DataManager) SaveTokenMaster(ctx context.Context, tm *TokenMaster) error {
	// 1. Simpan permanen di MongoDB
	if mg := m.getMongo(); mg != nil {
		if err := mg.SaveTokenMaster(ctx, tm); err != nil {
			log.Printf("⚠️ SaveTokenMaster MongoDB err: %v", err)
		}
	}
	// 2. Cache di SQLite jika ada
	if m.sqlite != nil {
		return m.sqlite.SaveTokenMaster(ctx, tm)
	}
	return nil
}

func (m *DataManager) GetTokenMaster(ctx context.Context, token string) (*TokenMaster, error) {
	// 1. Cek cache SQLite
	if m.sqlite != nil {
		tm, err := m.sqlite.GetTokenMaster(ctx, token)
		if err == nil && tm != nil {
			return tm, nil
		}
	}
	// 2. Jika belum ada di SQLite, tarik dari MongoDB permanen
	if mg := m.getMongo(); mg != nil {
		tm, err := mg.GetTokenMaster(ctx, token)
		if err == nil && tm != nil {
			// SQL hanya mengambil yang dibutuhkan user: simpan ke SQLite
			if m.sqlite != nil {
				_ = m.sqlite.SaveTokenMaster(ctx, tm)
			}
			return tm, nil
		}
	}
	return nil, nil
}

func (m *DataManager) GetActiveSharedFreeToken(ctx context.Context) (*TokenMaster, error) {
	// 1. Prioritaskan cek MongoDB agar sinkron lintas node
	if mg := m.getMongo(); mg != nil {
		tm, err := mg.GetActiveSharedFreeToken(ctx)
		if err == nil && tm != nil {
			if m.sqlite != nil {
				_ = m.sqlite.SaveTokenMaster(ctx, tm)
			}
			return tm, nil
		}
	}
	// 2. Fallback SQLite jika MongoDB offline
	if m.sqlite != nil {
		return m.sqlite.GetActiveSharedFreeToken(ctx)
	}
	return nil, nil
}

func (m *DataManager) UpdateTokenMasterDuration(ctx context.Context, token string, durationSeconds int64) error {
	// 1. Update permanen di MongoDB
	if mg := m.getMongo(); mg != nil {
		if err := mg.UpdateTokenMasterDuration(ctx, token, durationSeconds); err != nil {
			log.Printf("⚠️ UpdateTokenMasterDuration MongoDB err: %v", err)
		}
	}
	// 2. Update di SQLite lokal
	if m.sqlite != nil {
		return m.sqlite.UpdateTokenMasterDuration(ctx, token, durationSeconds)
	}
	return nil
}

func (m *DataManager) IncrementTokenMasterClaim(ctx context.Context, token string) error {
	// 1. Increment permanen di MongoDB
	if mg := m.getMongo(); mg != nil {
		if err := mg.IncrementTokenMasterClaim(ctx, token); err != nil {
			log.Printf("⚠️ IncrementTokenMasterClaim MongoDB err: %v", err)
		}
	}
	// 2. Increment di SQLite lokal
	if m.sqlite != nil {
		return m.sqlite.IncrementTokenMasterClaim(ctx, token)
	}
	return nil
}

func (m *DataManager) SaveUserLicense(ctx context.Context, ul *UserLicense) error {
	// 1. Simpan permanen di MongoDB
	if mg := m.getMongo(); mg != nil {
		if err := mg.SaveUserLicense(ctx, ul); err != nil {
			log.Printf("⚠️ SaveUserLicense MongoDB err: %v", err)
		}
	}
	// 2. Simpan lisensi user ke SQLite lokal
	if m.sqlite != nil {
		return m.sqlite.SaveUserLicense(ctx, ul)
	}
	return nil
}

func (m *DataManager) GetUserLicenseBySHA(ctx context.Context, tokenSHA string) (*UserLicense, error) {
	// 1. Cek SQLite lokal terlebih dahulu
	if m.sqlite != nil {
		ul, err := m.sqlite.GetUserLicenseBySHA(ctx, tokenSHA)
		if err == nil && ul != nil {
			return ul, nil
		}
	}
	// 2. Jika tidak ada di SQLite lokal (misal session baru / ganti node), tarik dari MongoDB
	if mg := m.getMongo(); mg != nil {
		ul, err := mg.GetUserLicenseBySHA(ctx, tokenSHA)
		if err == nil && ul != nil {
			// SQL hanya mengambil yang dibutuhkan user: cache lisensi ini ke SQLite lokal
			if m.sqlite != nil {
				_ = m.sqlite.SaveUserLicense(ctx, ul)
			}
			return ul, nil
		}
	}
	return nil, nil
}

func (m *DataManager) GetActiveUserLicense(ctx context.Context, ownerChatID int64) (*UserLicense, error) {
	// 1. Cek SQLite lokal terlebih dahulu
	if m.sqlite != nil {
		ul, err := m.sqlite.GetActiveUserLicense(ctx, ownerChatID)
		if err == nil && ul != nil {
			return ul, nil
		}
	}
	// 2. Jika tidak ada di SQLite lokal, tarik dari MongoDB permanen
	if mg := m.getMongo(); mg != nil {
		ul, err := mg.GetActiveUserLicense(ctx, ownerChatID)
		if err == nil && ul != nil {
			// SQL hanya mengambil yang dibutuhkan user: cache lisensi ini ke SQLite lokal
			if m.sqlite != nil {
				_ = m.sqlite.SaveUserLicense(ctx, ul)
			}
			return ul, nil
		}
	}
	return nil, nil
}

func (m *DataManager) GetLatestUserLicense(ctx context.Context, ownerChatID int64) (*UserLicense, error) {
	if m.sqlite != nil {
		ul, err := m.sqlite.GetLatestUserLicense(ctx, ownerChatID)
		if err == nil && ul != nil {
			return ul, nil
		}
	}
	if mg := m.getMongo(); mg != nil {
		ul, err := mg.GetLatestUserLicense(ctx, ownerChatID)
		if err == nil && ul != nil {
			if m.sqlite != nil {
				_ = m.sqlite.SaveUserLicense(ctx, ul)
			}
			return ul, nil
		}
	}
	return nil, nil
}

func (m *DataManager) ListExpiredUserLicenses(ctx context.Context, now time.Time) ([]*UserLicense, error) {
	seen := make(map[string]bool)
	var result []*UserLicense
	if m.sqlite != nil {
		list, err := m.sqlite.ListExpiredUserLicenses(ctx, now)
		if err == nil {
			for _, ul := range list {
				if ul != nil && !seen[ul.TokenSHA] {
					seen[ul.TokenSHA] = true
					result = append(result, ul)
				}
			}
		}
	}
	if mg := m.getMongo(); mg != nil {
		list, err := mg.ListExpiredUserLicenses(ctx, now)
		if err == nil {
			for _, ul := range list {
				if ul != nil && !seen[ul.TokenSHA] {
					seen[ul.TokenSHA] = true
					if m.sqlite != nil {
						_ = m.sqlite.SaveUserLicense(ctx, ul)
					}
					result = append(result, ul)
				}
			}
		}
	}
	return result, nil
}

func (m *DataManager) MarkUserLicenseExpired(ctx context.Context, tokenSHA string) error {
	if mg := m.getMongo(); mg != nil {
		if err := mg.MarkUserLicenseExpired(ctx, tokenSHA); err != nil {
			log.Printf("⚠️ MarkUserLicenseExpired MongoDB err: %v", err)
		}
	}
	if m.sqlite != nil {
		return m.sqlite.MarkUserLicenseExpired(ctx, tokenSHA)
	}
	return nil
}

func (m *DataManager) MarkTokenMasterExpired(ctx context.Context, token string) error {
	if mg := m.getMongo(); mg != nil {
		if err := mg.MarkTokenMasterExpired(ctx, token); err != nil {
			log.Printf("⚠️ MarkTokenMasterExpired MongoDB err: %v", err)
		}
	}
	if m.sqlite != nil {
		return m.sqlite.MarkTokenMasterExpired(ctx, token)
	}
	return nil
}

func (m *DataManager) HasUsedMasterToken(ctx context.Context, masterToken string) (bool, error) {
	if m.sqlite != nil {
		used, err := m.sqlite.HasUsedMasterToken(ctx, masterToken)
		if err == nil && used {
			return true, nil
		}
	}
	if mg := m.getMongo(); mg != nil {
		return mg.HasUsedMasterToken(ctx, masterToken)
	}
	return false, nil
}

func (m *DataManager) RelinkUserData(ctx context.Context, oldOwnerID, newOwnerID int64) error {
	// 1. Relink permanen di MongoDB
	if mg := m.getMongo(); mg != nil {
		if err := mg.RelinkUserData(ctx, oldOwnerID, newOwnerID); err != nil {
			log.Printf("⚠️ RelinkUserData MongoDB err: %v", err)
		}
	}
	// 2. Relink di SQLite lokal
	if m.sqlite != nil {
		return m.sqlite.RelinkUserData(ctx, oldOwnerID, newOwnerID)
	}
	return nil
}

type AccountControlInfo struct {
	OwnerID int64
	Name    string
	IsOwn   bool
}

func checkIsAdmin(adminStr string, userID int64, username string) bool {
	adminStr = strings.TrimSpace(adminStr)
	if adminStr == "" || adminStr == "Belum diatur" || strings.Contains(adminStr, "Belum diatur") {
		return false
	}
	targetIDStr := strconv.FormatInt(userID, 10)
	cleanUsername := strings.ToLower(strings.TrimPrefix(username, "@"))

	tokens := strings.FieldsFunc(adminStr, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';' || r == '\n' || r == '\t'
	})

	for _, token := range tokens {
		t := strings.TrimSpace(token)
		if t == "" {
			continue
		}
		if t == targetIDStr {
			return true
		}
		cleanToken := strings.ToLower(strings.TrimPrefix(t, "@"))
		if cleanUsername != "" && cleanToken == cleanUsername {
			return true
		}
	}
	return false
}

func (m *DataManager) UpdateAccountName(ctx context.Context, ownerID int64, name string) {
	if m == nil || m.sqlite == nil || name == "" {
		return
	}
	_ = m.sqlite.UpdateAccountName(ctx, ownerID, name)
}

func (m *DataManager) GetAccountName(ctx context.Context, ownerID int64) string {
	if m == nil || m.sqlite == nil {
		return ""
	}
	return m.sqlite.GetAccountName(ctx, ownerID)
}

func (m *DataManager) GetAccessibleAccounts(ctx context.Context, userID int64, username string) []AccountControlInfo {
	var results []AccountControlInfo
	seen := make(map[int64]bool)

	// 1. Akun milik user sendiri jika memiliki sesi aktif
	if m.HasActiveSession(ctx, userID) {
		name := m.GetAccountName(ctx, userID)
		if name == "" {
			name = "Akun Utama (Saya)"
		} else {
			name = "[Saya] " + name
		}
		results = append(results, AccountControlInfo{
			OwnerID: userID,
			Name:    name,
			IsOwn:   true,
		})
		seen[userID] = true
	}

	// 2. Akun orang lain yang menambahkan user ini sebagai admin
	if m.sqlite != nil {
		rows, err := m.sqlite.GetAllConfigsWithAdmin(ctx)
		if err == nil {
			for _, row := range rows {
				if seen[row.OwnerID] {
					continue
				}
				if checkIsAdmin(row.Admin, userID, username) {
					if m.HasActiveSession(ctx, row.OwnerID) {
						name := row.AccountName
						if name == "" {
							name = fmt.Sprintf("Akun [%d]", row.OwnerID)
						}
						results = append(results, AccountControlInfo{
							OwnerID: row.OwnerID,
							Name:    name,
							IsOwn:   false,
						})
						seen[row.OwnerID] = true
					}
				}
			}
		}
	}

	return results
}

// Close membersihkan koneksi SQLite dan MongoDB saat shutdown.
func (m *DataManager) Close() error {
	if m == nil {
		return nil
	}
	if m.sqlite != nil {
		if err := m.sqlite.Close(); err != nil {
			log.Printf("Error closing SQLite in DataManager: %v", err)
		}
	}
	m.mu.RLock()
	mc := m.mongo
	m.mu.RUnlock()
	if mc != nil && mc.Client != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mc.Client.Disconnect(shutdownCtx); err != nil {
			log.Printf("Error closing MongoDB in DataManager: %v", err)
		}
	}
	return nil
}
