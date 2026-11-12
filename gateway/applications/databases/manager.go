package databases

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DataManager struct {
	sqlite *SQLiteClient
	mu     sync.RWMutex
	mongo  *MongoClient
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

// NewDataManager creates a DataManager with SQLite (required) and MongoDB (optional, retried in background).
// bgCtx: long-lived context for MongoDB retry loop (not subject to startup timeout).
// initCtx: short context for SQLite init; can share the timeout context.
func NewDataManager(bgCtx context.Context, initCtx context.Context, mongoURI, mongoDBName, sqlitePath string) (*DataManager, error) {
	dm := &DataManager{}

	// Connect SQLite (must succeed immediately)
	sClient, err := NewSQLiteClient(sqlitePath)
	if err != nil {
		log.Printf("⚠️ SQLite connection failed: %v", err)
	}
	dm.sqlite = sClient

	// Connect MongoDB in background with retry until success
	if mongoURI != "" {
		go func() {
			mClient, retryErr := NewMongoClient(bgCtx, mongoURI, mongoDBName)
			if retryErr != nil {
				log.Printf("⚠️ MongoDB permanently failed (context cancelled): %v", retryErr)
				return
			}
			dm.setMongo(mClient)
			log.Printf("🍃 MongoDB is now active in DataManager")
		}()
	}

	if sClient == nil && mongoURI == "" {
		return nil, fmt.Errorf("both sqlite and mongodb are unavailable")
	}

	_ = initCtx
	return dm, nil
}

func (m *DataManager) GetSQLiteDB() *sql.DB {
	if m != nil && m.sqlite != nil {
		return m.sqlite.DB
	}
	return nil
}

func (m *DataManager) HasActiveSession(ctx context.Context, ownerID int64) bool {
	// SQLite adalah satu-satunya sumber read — MongoDB hanya backup write
	if m.sqlite != nil {
		has, err := m.sqlite.HasSession(ctx, ownerID)
		if err == nil && has {
			return true
		}
	}
	return false
}

func (m *DataManager) GetUserConfig(ctx context.Context, ownerID int64) *JasebConfig {
	// Baca hanya dari SQLite — cepat, lokal, tidak ada network call.
	// MongoDB hanya digunakan sebagai backup write (async).
	if m.sqlite != nil {
		cfg, err := m.sqlite.GetConfig(ctx, ownerID)
		if err == nil && cfg != nil {
			return cfg
		}
	}

	// 3. Konfigurasi default jika belum terdaftar sama sekali
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
		MasaAktif:        "November 29, 2026 at 14:18",
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

// SaveUserConfig: simpan SQLite langsung (synchronous), MongoDB di goroutine background (non-blocking).
func (m *DataManager) SaveUserConfig(ctx context.Context, cfg *JasebConfig) error {
	cfg.UpdatedAt = time.Now()

	// 1. Simpan ke SQLite lokal dulu — langsung, tidak memblokir response panel
	if m.sqlite != nil {
		if err := m.sqlite.SaveConfig(ctx, cfg); err != nil {
			log.Printf("⚠️ SQLite SyncConfig error: %v", err)
			return err
		}
	}

	// 2. Simpan ke MongoDB di background — tidak memblokir response ke user
	if mg := m.getMongo(); mg != nil {
		cfgCopy := *cfg
		go func() {
			mongoCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := mg.SaveConfig(mongoCtx, &cfgCopy); err != nil {
				log.Printf("⚠️ MongoDB SaveConfig error: %v", err)
			}
		}()
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
