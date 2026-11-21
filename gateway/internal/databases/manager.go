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
	if cfg.TopicLists != nil {
		cp.TopicLists = make([]TopicListEntry, len(cfg.TopicLists))
		for i, tl := range cfg.TopicLists {
			cpTL := tl
			if tl.Posts != nil {
				cpTL.Posts = make([]int, len(tl.Posts))
				copy(cpTL.Posts, tl.Posts)
			}
			if tl.LinkedTopics != nil {
				cpTL.LinkedTopics = make([]TopicLink, len(tl.LinkedTopics))
				copy(cpTL.LinkedTopics, tl.LinkedTopics)
			}
			cp.TopicLists[i] = cpTL
		}
	}
	return &cp
}

type DataManager struct {
	sqlite          *SQLiteClient
	mu              sync.RWMutex
	mongo           *MongoClient
	rdb             *redis.Client
	cfgCache        *boundedCache[int64, *JasebConfig]
	licCache        *boundedCache[int64, *UserLicense]
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

type DBHealth struct {
	SQLiteOK bool
	SQLiteErr string
	MongoConfigured bool
	MongoOK  bool
	MongoErr string
}

func (dm *DataManager) CheckHealth(ctx context.Context, mongoConfigured bool) DBHealth {
	h := DBHealth{MongoConfigured: mongoConfigured}
	if dm == nil {
		h.SQLiteErr = "DataManager nil"
		return h
	}
	if dm.sqlite != nil && dm.sqlite.DB != nil {
		if err := dm.sqlite.DB.PingContext(ctx); err != nil {
			h.SQLiteErr = err.Error()
		} else {
			h.SQLiteOK = true
		}
	} else {
		h.SQLiteErr = "SQLite belum diinisialisasi"
	}

	if mg := dm.getMongo(); mg != nil && mg.Client != nil {
		var lastErr error
		for attempt := 0; attempt < 2; attempt++ {
			pCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := mg.Client.Ping(pCtx, nil)
			cancel()
			if err == nil {
				h.MongoOK = true
				lastErr = nil
				break
			}
			lastErr = err
			if attempt == 0 {
				t := time.NewTimer(500 * time.Millisecond)
				select {
				case <-ctx.Done():
					t.Stop()
					h.MongoErr = lastErr.Error()
					return h
				case <-t.C:
				}
			}
		}
		if lastErr != nil {
			h.MongoErr = lastErr.Error()
		}
	} else if mongoConfigured {
		h.MongoErr = "MongoDB belum terhubung / fallback ke SQLite"
	}
	return h
}

// NewDataManager creates a DataManager dengan SQLite (primary) dan MongoDB (backup, retry di background).
// bgCtx: parent context panjang untuk retry MongoDB.
// initCtx: context pendek hanya untuk inisialisasi SQLite.
func NewDataManager(bgCtx context.Context, initCtx context.Context, mongoURI, mongoDBName, sqlitePath string) (*DataManager, error) {
	dm := &DataManager{
		cfgCache:        newBoundedCache[int64, *JasebConfig](maxGatewayCacheSize, cfgCacheTTL),
		licCache:        newBoundedCache[int64, *UserLicense](maxGatewayCacheSize, licCacheTTL),
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
	mongoCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
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
			dm.cacheSetConfig(ctx, cfg, false)
		}
	}
	return cfg
}

// WatchConfigUpdates subscribe ke Redis Pub/Sub channel "config:updated".
// Saat ubot mengubah MongoDB, ubot publish ownerID ke channel ini.
// Gateway lalu fetch ulang 1 doc user tersebut dari MongoDB dan update SQLite.
func (dm *DataManager) WatchConfigUpdates(ctx context.Context, rdb *redis.Client) {
	dm.AttachRedisCache(rdb)
	pubsub := rdb.Subscribe(ctx, "config:updated", cacheInvChannel)
	defer pubsub.Close()

	log.Printf("👂 Gateway watching Redis channels: config:updated, %s", cacheInvChannel)
	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if msg.Channel == cacheInvChannel {
				var inv cacheInvalidationEvent
				if json.Unmarshal([]byte(msg.Payload), &inv) == nil {
					dm.handleCacheInvalidation(inv)
				}
				continue
			}
			var evt ConfigUpdateEvent
			if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
				log.Printf("⚠️ config:updated bad payload: %v", err)
				continue
			}
			dm.cacheInvalidateConfig(ctx, evt.OwnerID, false)
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
		mongoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		has, err := mg.HasSession(mongoCtx, ownerID)
		if err == nil && has {
			return true
		}
	}
	return false
}

func (m *DataManager) GetUserConfig(ctx context.Context, ownerID int64) *JasebConfig {
	if cached := m.cacheGetConfig(ctx, ownerID); cached != nil {
		return cached
	}
	// 1. Cek SQLite lokal dulu (cache) — jika ada, langsung kembalikan (0 hit ke MongoDB)
	if m.sqlite != nil {
		cfg, err := m.sqlite.GetConfig(ctx, ownerID)
		if err == nil && cfg != nil {
			m.cacheSetConfig(ctx, cfg, false)
			return cfg
		}
	}

	// 2. Cache miss di SQLite: ambil hanya untuk user ini dari MongoDB (1x hit per user)
	if cfg := m.SyncOneFromMongoDB(ctx, ownerID); cfg != nil {
		m.cacheSetConfig(ctx, cfg, false)
		return cfg
	}

	// 3. Konfigurasi default jika belum terdaftar di MongoDB maupun SQLite
	defaultCfg := &JasebConfig{
		OwnerID:          ownerID,
		Type:             "Broadcast",
		Delay:            5,
		DelayMode:        "manual",
		DelayAllGroups:   0,
		SkipFloodWait:    false,
		SkipFloodWaitSec: 0,
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
		TopicLists:       []TopicListEntry{},
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
		if cfg.ActiveTokenSHA != "" {
			if err := m.sqlite.SaveTokenProfile(ctx, TokenProfileFromConfig(cfg.ActiveTokenSHA, cfg)); err != nil {
				log.Printf("⚠️ SQLite SaveTokenProfile error: %v", err)
			}
		}
	}
	m.cacheSetConfig(ctx, cfg, true)

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
					mongoCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					if err := currentMg.SaveConfig(mongoCtx, curCfg); err != nil {
						log.Printf("⚠️ MongoDB SaveConfig error (owner=%d): %v", oid, err)
					}
					if curCfg.ActiveTokenSHA != "" {
						if err := currentMg.SaveTokenProfile(mongoCtx, TokenProfileFromConfig(curCfg.ActiveTokenSHA, curCfg)); err != nil {
							log.Printf("⚠️ MongoDB SaveTokenProfile error (owner=%d): %v", oid, err)
						}
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
	peerType := "chat"

	if strings.HasPrefix(cleanTarget, "-100") {
		peerType = "channel"
		if id, err := strconv.ParseInt(cleanTarget, 10, 64); err == nil && id < 0 {
			cleanChatID = id
		}
	} else if strings.HasPrefix(cleanTarget, "-") {
		peerType = "chat"
		if id, err := strconv.ParseInt(cleanTarget, 10, 64); err == nil && id < 0 {
			cleanChatID = id
		}
	} else if cleanChatID != 0 {
		chatIDStr := strconv.FormatInt(cleanChatID, 10)
		if strings.HasPrefix(chatIDStr, "-100") {
			peerType = "channel"
		} else if cleanChatID > 0 && cleanChatID >= 1000000000 && !strings.HasPrefix(cleanTarget, "@") {
			peerType = "channel"
			cleanChatID = ChannelBotID(cleanChatID)
			cleanTarget = strconv.FormatInt(cleanChatID, 10)
		}
	}

	for i, g := range cfg.Groups {
		if g.Target == cleanTarget || SameGroupID(g.ChatID, cleanChatID) {
			if title != "" && (g.Title == "" || g.Title == g.Target) {
				cfg.Groups[i].Title = title
				if g.PeerType == "" {
					cfg.Groups[i].PeerType = peerType
				}
				return m.SaveUserConfig(ctx, cfg)
			}
			return nil
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
		err := m.sqlite.SaveUserLicense(ctx, ul)
		if err == nil && ul != nil {
			m.cacheInvalidateLicense(ctx, ul.OwnerChatID, true)
		}
		return err
	}
	if ul != nil {
		m.cacheInvalidateLicense(ctx, ul.OwnerChatID, true)
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
	if mg := m.getMongo(); mg != nil {
		ul, err := mg.GetUserLicenseBySHA(ctx, tokenSHA)
		if err == nil && ul != nil {
			if m.sqlite != nil {
				_ = m.sqlite.SaveUserLicense(ctx, ul)
			}
			return ul, nil
		}
	}
	return nil, nil
}

func (m *DataManager) GetUserLicenseByOwnerAndMaster(ctx context.Context, ownerChatID int64, masterToken string) (*UserLicense, error) {
	if m.sqlite != nil {
		ul, err := m.sqlite.GetUserLicenseByOwnerAndMaster(ctx, ownerChatID, masterToken)
		if err == nil && ul != nil {
			return ul, nil
		}
	}
	if mg := m.getMongo(); mg != nil {
		ul, err := mg.GetUserLicenseByOwnerAndMaster(ctx, ownerChatID, masterToken)
		if err == nil && ul != nil {
			if m.sqlite != nil {
				_ = m.sqlite.SaveUserLicense(ctx, ul)
			}
			return ul, nil
		}
	}
	return nil, nil
}

func (m *DataManager) GetActiveUserLicense(ctx context.Context, ownerChatID int64) (*UserLicense, error) {
	if cached, hit := m.cacheGetActiveLicense(ctx, ownerChatID); hit && cached != nil {
		return cached, nil
	}
	// 1. Cek SQLite lokal terlebih dahulu
	if m.sqlite != nil {
		ul, err := m.sqlite.GetActiveUserLicense(ctx, ownerChatID)
		if err == nil && ul != nil {
			m.cacheSetActiveLicense(ctx, ownerChatID, ul)
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
			m.cacheSetActiveLicense(ctx, ownerChatID, ul)
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

func (m *DataManager) ListExpiredUserLicenses(ctx context.Context, now time.Time, includeRemote ...bool) ([]*UserLicense, error) {
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
	checkRemote := len(includeRemote) > 0 && includeRemote[0]
	if checkRemote {
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
	}
	return result, nil
}

func (m *DataManager) MarkUserLicenseExpired(ctx context.Context, tokenSHA string) error {
	var ownerChatID int64
	if ul, _ := m.GetUserLicenseBySHA(ctx, tokenSHA); ul != nil {
		ownerChatID = ul.OwnerChatID
	}
	if mg := m.getMongo(); mg != nil {
		if err := mg.MarkUserLicenseExpired(ctx, tokenSHA); err != nil {
			log.Printf("⚠️ MarkUserLicenseExpired MongoDB err: %v", err)
		}
	}
	if m.sqlite != nil {
		err := m.sqlite.MarkUserLicenseExpired(ctx, tokenSHA)
		m.cacheInvalidateLicense(ctx, ownerChatID, true)
		return err
	}
	m.cacheInvalidateLicense(ctx, ownerChatID, true)
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
		err := m.sqlite.RelinkUserData(ctx, oldOwnerID, newOwnerID)
		m.cacheInvalidateConfig(ctx, oldOwnerID, true)
		m.cacheInvalidateConfig(ctx, newOwnerID, true)
		m.cacheInvalidateLicense(ctx, oldOwnerID, true)
		m.cacheInvalidateLicense(ctx, newOwnerID, true)
		return err
	}
	m.cacheInvalidateConfig(ctx, oldOwnerID, true)
	m.cacheInvalidateConfig(ctx, newOwnerID, true)
	m.cacheInvalidateLicense(ctx, oldOwnerID, true)
	m.cacheInvalidateLicense(ctx, newOwnerID, true)
	return nil
}

func (m *DataManager) SupersedeOtherUserLicenses(ctx context.Context, ownerChatID int64, keepTokenSHA string) error {
	if mg := m.getMongo(); mg != nil {
		if err := mg.SupersedeOtherUserLicenses(ctx, ownerChatID, keepTokenSHA); err != nil {
			log.Printf("⚠️ SupersedeOtherUserLicenses MongoDB err: %v", err)
		}
	}
	if m.sqlite != nil {
		err := m.sqlite.SupersedeOtherUserLicenses(ctx, ownerChatID, keepTokenSHA)
		m.cacheInvalidateLicense(ctx, ownerChatID, true)
		return err
	}
	m.cacheInvalidateLicense(ctx, ownerChatID, true)
	return nil
}

func (m *DataManager) MarkUserLicenseStatus(ctx context.Context, tokenSHA, status string) error {
	var ownerChatID int64
	if ul, _ := m.GetUserLicenseBySHA(ctx, tokenSHA); ul != nil {
		ownerChatID = ul.OwnerChatID
	}
	if mg := m.getMongo(); mg != nil {
		if err := mg.MarkUserLicenseStatus(ctx, tokenSHA, status); err != nil {
			log.Printf("⚠️ MarkUserLicenseStatus MongoDB err: %v", err)
		}
	}
	if m.sqlite != nil {
		err := m.sqlite.MarkUserLicenseStatus(ctx, tokenSHA, status)
		m.cacheInvalidateLicense(ctx, ownerChatID, true)
		return err
	}
	m.cacheInvalidateLicense(ctx, ownerChatID, true)
	return nil
}

func (m *DataManager) RevokeTokenMaster(ctx context.Context, token string) error {
	if mg := m.getMongo(); mg != nil {
		if err := mg.RevokeTokenMaster(ctx, token); err != nil {
			log.Printf("⚠️ RevokeTokenMaster MongoDB err: %v", err)
		}
	}
	if m.sqlite != nil {
		return m.sqlite.RevokeTokenMaster(ctx, token)
	}
	return nil
}

func (m *DataManager) SaveDataBackup(ctx context.Context, b *DataBackupCode) error {
	if mg := m.getMongo(); mg != nil {
		if err := mg.SaveDataBackup(ctx, b); err != nil {
			log.Printf("⚠️ SaveDataBackup MongoDB err: %v", err)
		}
	}
	if m.sqlite != nil {
		return m.sqlite.SaveDataBackup(ctx, b)
	}
	return nil
}

func (m *DataManager) GetDataBackup(ctx context.Context, code string) (*DataBackupCode, error) {
	if m.sqlite != nil {
		b, err := m.sqlite.GetDataBackup(ctx, code)
		if err == nil && b != nil {
			return b, nil
		}
	}
	if mg := m.getMongo(); mg != nil {
		b, err := mg.GetDataBackup(ctx, code)
		if err == nil && b != nil {
			if m.sqlite != nil {
				_ = m.sqlite.SaveDataBackup(ctx, b)
			}
			return b, nil
		}
	}
	return nil, nil
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

func (m *DataManager) SaveTokenProfile(ctx context.Context, p *TokenProfile) error {
	if m == nil || p == nil || strings.TrimSpace(p.TokenSHA) == "" {
		return nil
	}
	cp := cloneTokenProfile(p)
	if m.sqlite != nil {
		if err := m.sqlite.SaveTokenProfile(ctx, cp); err != nil {
			return fmt.Errorf("save token profile sqlite: %w", err)
		}
	}
	if mg := m.getMongo(); mg != nil {
		if m.mongoSaveSem != nil {
			m.mongoSaveSem <- struct{}{}
		}
		go func(prof *TokenProfile) {
			if m.mongoSaveSem != nil {
				defer func() { <-m.mongoSaveSem }()
			}
			bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = mg.SaveTokenProfile(bgCtx, prof)
		}(cp)
	}
	return nil
}

func (m *DataManager) GetTokenProfile(ctx context.Context, tokenSHA string) (*TokenProfile, error) {
	if m == nil || strings.TrimSpace(tokenSHA) == "" {
		return nil, nil
	}
	if m.sqlite != nil {
		p, err := m.sqlite.GetTokenProfile(ctx, tokenSHA)
		if err == nil && p != nil {
			return p, nil
		}
	}
	if mg := m.getMongo(); mg != nil {
		p, err := mg.GetTokenProfile(ctx, tokenSHA)
		if err == nil && p != nil {
			if m.sqlite != nil {
				_ = m.sqlite.SaveTokenProfile(ctx, p)
			}
			return p, nil
		}
	}
	return nil, nil
}

func (m *DataManager) ListActiveUserLicenses(ctx context.Context, ownerChatID int64) ([]*UserLicense, error) {
	if m == nil {
		return nil, nil
	}
	if m.sqlite != nil {
		list, err := m.sqlite.ListActiveUserLicenses(ctx, ownerChatID)
		if err == nil && len(list) > 0 {
			return list, nil
		}
	}
	if mg := m.getMongo(); mg != nil {
		list, err := mg.ListActiveUserLicenses(ctx, ownerChatID)
		if err == nil && len(list) > 0 {
			if m.sqlite != nil {
				for _, lic := range list {
					_ = m.sqlite.SaveUserLicense(ctx, lic)
				}
			}
			return list, nil
		}
	}
	return nil, nil
}
