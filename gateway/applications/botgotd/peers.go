package botgotd

import (
	"database/sql"
	"log"
	"sync"

	"github.com/gotd/td/tg"
)

type PeerStorage struct {
	mu    sync.RWMutex
	users map[int64]int64 // userID -> accessHash
	chats map[int64]int64 // channelID -> accessHash
	db    *sql.DB
}

func NewPeerStorage(sqliteDB *sql.DB) *PeerStorage {
	ps := &PeerStorage{
		users: make(map[int64]int64),
		chats: make(map[int64]int64),
		db:    sqliteDB,
	}
	ps.initDB()
	return ps
}

func (ps *PeerStorage) initDB() {
	if ps.db == nil {
		return
	}
	query := `
	CREATE TABLE IF NOT EXISTS bot_peers (
		peer_id INTEGER PRIMARY KEY,
		peer_type TEXT NOT NULL,
		access_hash INTEGER NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := ps.db.Exec(query); err != nil {
		log.Printf("[botgotd] init peers table error: %v", err)
		return
	}

	// Preload into memory
	rows, err := ps.db.Query("SELECT peer_id, peer_type, access_hash FROM bot_peers")
	if err != nil {
		return
	}
	defer rows.Close()

	ps.mu.Lock()
	defer ps.mu.Unlock()
	for rows.Next() {
		var id, hash int64
		var pType string
		if err := rows.Scan(&id, &pType, &hash); err == nil {
			if pType == "channel" {
				ps.chats[id] = hash
			} else {
				ps.users[id] = hash
			}
		}
	}
}

func (ps *PeerStorage) SaveUser(userID, accessHash int64) {
	if userID <= 0 {
		return
	}
	ps.mu.Lock()
	ps.users[userID] = accessHash
	ps.mu.Unlock()

	if ps.db != nil {
		go func() {
			query := `INSERT INTO bot_peers (peer_id, peer_type, access_hash) VALUES (?, 'user', ?)
			ON CONFLICT(peer_id) DO UPDATE SET access_hash = excluded.access_hash, updated_at = CURRENT_TIMESTAMP;`
			_, _ = ps.db.Exec(query, userID, accessHash)
		}()
	}
}

func (ps *PeerStorage) SaveChannel(channelID, accessHash int64) {
	if channelID <= 0 {
		return
	}
	ps.mu.Lock()
	ps.chats[channelID] = accessHash
	ps.mu.Unlock()

	if ps.db != nil {
		go func() {
			query := `INSERT INTO bot_peers (peer_id, peer_type, access_hash) VALUES (?, 'channel', ?)
			ON CONFLICT(peer_id) DO UPDATE SET access_hash = excluded.access_hash, updated_at = CURRENT_TIMESTAMP;`
			_, _ = ps.db.Exec(query, channelID, accessHash)
		}()
	}
}

func (ps *PeerStorage) SaveChat(chatID int64) {
	if chatID <= 0 {
		return
	}
	ps.mu.Lock()
	// Store with 0 access hash — basic chat
	if _, ok := ps.users[-chatID]; !ok {
		ps.users[-chatID] = 0
	}
	ps.mu.Unlock()
}

func (ps *PeerStorage) GetInputPeer(chatID int64) tg.InputPeerClass {
	if chatID == 0 {
		return nil
	}

	// User
	if chatID > 0 {
		ps.mu.RLock()
		hash, ok := ps.users[chatID]
		ps.mu.RUnlock()
		if ok {
			return &tg.InputPeerUser{UserID: chatID, AccessHash: hash}
		}
		// Fallback minimal peer
		return &tg.InputPeerUser{UserID: chatID, AccessHash: 0}
	}

	// Channel / Supergroup (chatID negatif)
	realID := -chatID
	if realID > 1000000000000 {
		realID -= 1000000000000
	}
	ps.mu.RLock()
	hash, ok := ps.chats[realID]
	ps.mu.RUnlock()
	if ok {
		return &tg.InputPeerChannel{ChannelID: realID, AccessHash: hash}
	}
	return &tg.InputPeerChat{ChatID: realID}
}
