package databases

import (
	"context"
	"fmt"
	"log"
	"time"
)

type DataManager struct {
	sqlite *SQLiteClient
	mongo  *MongoClient
}

func NewDataManager(ctx context.Context, mongoURI, mongoDBName, sqlitePath string) (*DataManager, error) {
	var sClient *SQLiteClient
	var mClient *MongoClient
	var err error

	// Connect SQLite
	sClient, err = NewSQLiteClient(sqlitePath)
	if err != nil {
		log.Printf("⚠️ SQLite connection failed: %v", err)
	}

	// Connect MongoDB
	if mongoURI != "" {
		mClient, err = NewMongoClient(ctx, mongoURI, mongoDBName)
		if err != nil {
			log.Printf("⚠️ MongoDB connection failed: %v", err)
		}
	}

	if sClient == nil && mClient == nil {
		return nil, fmt.Errorf("both sqlite and mongodb are unavailable")
	}

	return &DataManager{sqlite: sClient, mongo: mClient}, nil
}

func (m *DataManager) HasActiveSession(ctx context.Context, ownerID int64) bool {
	if m.sqlite != nil {
		has, err := m.sqlite.HasSession(ctx, ownerID)
		if err == nil && has {
			return true
		}
	}
	if m.mongo != nil {
		has, err := m.mongo.HasSession(ctx, ownerID)
		if err == nil && has {
			return true
		}
	}
	return false
}

func (m *DataManager) GetUserConfig(ctx context.Context, ownerID int64) *UserConfig {
	// Try SQLite first for < 1ms read latency
	if m.sqlite != nil {
		cfg, err := m.sqlite.GetConfig(ctx, ownerID)
		if err == nil && cfg != nil {
			return cfg
		}
	}

	// Fallback to MongoDB
	if m.mongo != nil {
		cfg, err := m.mongo.GetConfig(ctx, ownerID)
		if err == nil && cfg != nil {
			// Sync into local SQLite
			if m.sqlite != nil {
				_ = m.sqlite.SaveConfig(ctx, cfg)
			}
			return cfg
		}
	}

	// Default config if none exists
	defaultCfg := &UserConfig{
		OwnerID:         ownerID,
		Type:            "Broadcast",
		Delay:           5,
		SkipFloodWait:   false,
		Timer:           "Belum diatur",
		Extra:           0,
		Mode:            "Normal",
		Safemode:        false,
		Status:          "Berhenti",
		MasaAktif:       "November 29, 2026 at 14:18",
		Notifikasi:      "Aktif · Kirim ke Saya",
		Email:           "Belum diatur",
		Admin:           "Belum diatur",
		Watermark:       "",
		TopicEnabled:    false,
		TindakanTerkini: "Siaran otomatis dimatikan",
		TindakanTerakhir: "-",
		UpdatedAt:       time.Now(),
	}

	// Save to MongoDB first, then sync to SQLite
	_ = m.SaveUserConfig(ctx, defaultCfg)
	return defaultCfg
}

// SaveUserConfig adheres to the rule: "setiap perubahan datanya disimpan di mongodb, lalu sqlite hanya menyinkronkan jika terjadi perubahan"
func (m *DataManager) SaveUserConfig(ctx context.Context, cfg *UserConfig) error {
	cfg.UpdatedAt = time.Now()

	// 1. Simpan ke MongoDB sebagai sumber data utama
	if m.mongo != nil {
		if err := m.mongo.SaveConfig(ctx, cfg); err != nil {
			log.Printf("⚠️ MongoDB SaveConfig error: %v", err)
		}
	}

	// 2. Sinkronkan perubahan ke SQLite lokal
	if m.sqlite != nil {
		if err := m.sqlite.SaveConfig(ctx, cfg); err != nil {
			log.Printf("⚠️ SQLite SyncConfig error: %v", err)
			return err
		}
	}
	return nil
}

// AddList: Simpan ke MongoDB -> sinkronkan ke SQLite
func (m *DataManager) AddList(ctx context.Context, ownerID int64, text string) (*UserList, error) {
	list := &UserList{
		OwnerID:   ownerID,
		Text:      text,
		CreatedAt: time.Now(),
	}

	// 1. Simpan ke MongoDB
	if m.mongo != nil {
		mongoID, err := m.mongo.AddList(ctx, list)
		if err != nil {
			log.Printf("⚠️ MongoDB AddList error: %v", err)
		} else {
			list.MongoID = mongoID
		}
	}

	// 2. Sinkronkan ke SQLite
	if m.sqlite != nil {
		if err := m.sqlite.AddList(ctx, list); err != nil {
			log.Printf("⚠️ SQLite AddList error: %v", err)
			return nil, err
		}
	}

	// Update list count in config
	m.updateCounts(ctx, ownerID)

	return list, nil
}

func (m *DataManager) GetLists(ctx context.Context, ownerID int64) []UserList {
	if m.sqlite != nil {
		lists, err := m.sqlite.GetLists(ctx, ownerID)
		if err == nil && len(lists) > 0 {
			return lists
		}
	}
	if m.mongo != nil {
		lists, err := m.mongo.GetLists(ctx, ownerID)
		if err == nil && len(lists) > 0 {
			// Sync to SQLite
			if m.sqlite != nil {
				for i := range lists {
					_ = m.sqlite.AddList(ctx, &lists[i])
				}
			}
			return lists
		}
	}
	return []UserList{}
}

func (m *DataManager) DeleteList(ctx context.Context, ownerID int64, id int64) error {
	var mongoID string
	if m.sqlite != nil {
		lists, _ := m.sqlite.GetLists(ctx, ownerID)
		for _, l := range lists {
			if l.ID == id {
				mongoID = l.MongoID
				break
			}
		}
	}

	// 1. Hapus di MongoDB
	if m.mongo != nil && mongoID != "" {
		_ = m.mongo.DeleteList(ctx, ownerID, mongoID)
	}

	// 2. Sinkronkan hapus di SQLite
	if m.sqlite != nil {
		_ = m.sqlite.DeleteList(ctx, ownerID, id)
	}

	m.updateCounts(ctx, ownerID)
	return nil
}

func (m *DataManager) ClearLists(ctx context.Context, ownerID int64) error {
	// 1. Bersihkan di MongoDB
	if m.mongo != nil {
		_ = m.mongo.ClearLists(ctx, ownerID)
	}

	// 2. Sinkronkan bersihkan di SQLite
	if m.sqlite != nil {
		_ = m.sqlite.ClearLists(ctx, ownerID)
	}

	m.updateCounts(ctx, ownerID)
	return nil
}

// AddGroup: Simpan ke MongoDB -> sinkronkan ke SQLite
func (m *DataManager) AddGroup(ctx context.Context, ownerID int64, target, title string, chatID int64) (*UserGroup, error) {
	group := &UserGroup{
		OwnerID:   ownerID,
		Target:    target,
		Title:     title,
		ChatID:    chatID,
		CreatedAt: time.Now(),
	}

	// 1. Simpan ke MongoDB
	if m.mongo != nil {
		mongoID, err := m.mongo.AddGroup(ctx, group)
		if err != nil {
			log.Printf("⚠️ MongoDB AddGroup error: %v", err)
		} else {
			group.MongoID = mongoID
		}
	}

	// 2. Sinkronkan ke SQLite
	if m.sqlite != nil {
		if err := m.sqlite.AddGroup(ctx, group); err != nil {
			log.Printf("⚠️ SQLite AddGroup error: %v", err)
			return nil, err
		}
	}

	m.updateCounts(ctx, ownerID)
	return group, nil
}

func (m *DataManager) GetGroups(ctx context.Context, ownerID int64) []UserGroup {
	if m.sqlite != nil {
		groups, err := m.sqlite.GetGroups(ctx, ownerID)
		if err == nil && len(groups) > 0 {
			return groups
		}
	}
	if m.mongo != nil {
		groups, err := m.mongo.GetGroups(ctx, ownerID)
		if err == nil && len(groups) > 0 {
			// Sync to SQLite
			if m.sqlite != nil {
				for i := range groups {
					_ = m.sqlite.AddGroup(ctx, &groups[i])
				}
			}
			return groups
		}
	}
	return []UserGroup{}
}

func (m *DataManager) DeleteGroup(ctx context.Context, ownerID int64, id int64) error {
	var mongoID string
	if m.sqlite != nil {
		groups, _ := m.sqlite.GetGroups(ctx, ownerID)
		for _, g := range groups {
			if g.ID == id {
				mongoID = g.MongoID
				break
			}
		}
	}

	// 1. Hapus di MongoDB
	if m.mongo != nil && mongoID != "" {
		_ = m.mongo.DeleteGroup(ctx, ownerID, mongoID)
	}

	// 2. Sinkronkan hapus di SQLite
	if m.sqlite != nil {
		_ = m.sqlite.DeleteGroup(ctx, ownerID, id)
	}

	m.updateCounts(ctx, ownerID)
	return nil
}

func (m *DataManager) ClearGroups(ctx context.Context, ownerID int64) error {
	// 1. Bersihkan di MongoDB
	if m.mongo != nil {
		_ = m.mongo.ClearGroups(ctx, ownerID)
	}

	// 2. Sinkronkan bersihkan di SQLite
	if m.sqlite != nil {
		_ = m.sqlite.ClearGroups(ctx, ownerID)
	}

	m.updateCounts(ctx, ownerID)
	return nil
}

func (m *DataManager) AddTopic(ctx context.Context, ownerID int64, chatID int64, topicID int, title string) (*UserTopic, error) {
	topic := &UserTopic{
		OwnerID:   ownerID,
		ChatID:    chatID,
		TopicID:   topicID,
		Title:     title,
		CreatedAt: time.Now(),
	}

	if m.mongo != nil {
		mongoID, err := m.mongo.AddTopic(ctx, topic)
		if err != nil {
			log.Printf("⚠️ MongoDB AddTopic error: %v", err)
		} else {
			topic.MongoID = mongoID
		}
	}

	if m.sqlite != nil {
		_ = m.sqlite.AddTopic(ctx, topic)
	}

	return topic, nil
}

func (m *DataManager) GetTopics(ctx context.Context, ownerID int64) []UserTopic {
	if m.sqlite != nil {
		topics, err := m.sqlite.GetTopics(ctx, ownerID)
		if err == nil && len(topics) > 0 {
			return topics
		}
	}
	if m.mongo != nil {
		topics, err := m.mongo.GetTopics(ctx, ownerID)
		if err == nil && len(topics) > 0 {
			if m.sqlite != nil {
				for i := range topics {
					_ = m.sqlite.AddTopic(ctx, &topics[i])
				}
			}
			return topics
		}
	}
	return []UserTopic{}
}

func (m *DataManager) ClearTopics(ctx context.Context, ownerID int64) error {
	if m.mongo != nil {
		_ = m.mongo.ClearTopics(ctx, ownerID)
	}
	if m.sqlite != nil {
		_ = m.sqlite.ClearTopics(ctx, ownerID)
	}
	return nil
}

func (m *DataManager) CountGroupsAndLists(ctx context.Context, ownerID int64) (int, int) {
	groupCount := 0
	listCount := 0

	if m.sqlite != nil {
		groupCount, _ = m.sqlite.CountGroups(ctx, ownerID)
		listCount, _ = m.sqlite.CountLists(ctx, ownerID)
	}
	return groupCount, listCount
}

func (m *DataManager) updateCounts(ctx context.Context, ownerID int64) {
	gc, lc := m.CountGroupsAndLists(ctx, ownerID)
	cfg := m.GetUserConfig(ctx, ownerID)
	if cfg != nil {
		// Update and save
		_ = m.SaveUserConfig(ctx, cfg)
	}
	_ = gc
	_ = lc
}

