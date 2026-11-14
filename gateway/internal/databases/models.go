package databases

import "time"

type GroupEntry struct {
	Target     string `bson:"target" json:"target"`
	Title      string `bson:"title" json:"title"`
	ChatID     int64  `bson:"chat_id" json:"chat_id"`
	AccessHash int64  `bson:"access_hash" json:"access_hash"`
	PeerType   string `bson:"peer_type" json:"peer_type"`
}

type TopicEntry struct {
	ChatID  int64  `bson:"chat_id" json:"chat_id"`
	TopicID int    `bson:"topic_id" json:"topic_id"`
	Title   string `bson:"title" json:"title"`
}

type JasebConfig struct {
	OwnerID          int64        `bson:"_id" json:"owner_id"`
	Type             string       `bson:"type" json:"type"`
	Delay            int          `bson:"delay" json:"delay"`
	SkipFloodWait    bool         `bson:"skip_floodwait" json:"skip_floodwait"`
	Timer            string       `bson:"timer" json:"timer"`
	Extra            int          `bson:"extra" json:"extra"`
	Mode             string       `bson:"mode" json:"mode"`
	Safemode         bool         `bson:"safemode" json:"safemode"`
	Status           string       `bson:"status" json:"status"`
	MasaAktif        string       `bson:"masa_aktif" json:"masa_aktif"`
	Notifikasi       string       `bson:"notifikasi" json:"notifikasi"`
	Email            string       `bson:"email" json:"email"`
	Admin            string       `bson:"admin" json:"admin"`
	Watermark        string       `bson:"watermark" json:"watermark"`
	WatermarkCacheID int          `bson:"watermark_cache_id" json:"watermark_cache_id"`
	TopicEnabled     bool         `bson:"topic_enabled" json:"topic_enabled"`
	TindakanTerkini  string       `bson:"tindakan_terkini" json:"tindakan_terkini"`
	TindakanTerakhir string       `bson:"tindakan_terakhir" json:"tindakan_terakhir"`
	Lists            []int        `bson:"lists" json:"lists"` // Array of AiotdCaches message IDs (TEKS_CACHE reply)
	Groups           []GroupEntry `bson:"groups" json:"groups"`
	Topics           []TopicEntry `bson:"topics" json:"topics"`
	UpdatedAt        time.Time    `bson:"updated_at" json:"updated_at"`
}

// UserConfig alias for backward-compatibility
type UserConfig = JasebConfig

type UserGroup = GroupEntry
type UserTopic = TopicEntry

type TokenMaster struct {
	Token           string    `bson:"_id" json:"token"` // GOTD:<32_ALPHABET>
	Tier            string    `bson:"tier" json:"tier"` // Free | Pro | Pro++ | Vip
	DurationSeconds int64     `bson:"duration_seconds" json:"duration_seconds"`
	IsShared        bool      `bson:"is_shared" json:"is_shared"`
	ClaimCount      int       `bson:"claim_count" json:"claim_count"`
	CreatedBy       int64     `bson:"created_by" json:"created_by"`
	CreatedAt       time.Time `bson:"created_at" json:"created_at"`
	Status          string    `bson:"status" json:"status"` // active | revoked
}

type UserLicense struct {
	TokenSHA           string    `bson:"_id" json:"token_sha"` // GOTD_SHA:<hex>
	MasterToken        string    `bson:"master_token" json:"master_token"`
	OwnerChatID        int64     `bson:"owner_chat_id" json:"owner_chat_id"`
	UserbotID          int64     `bson:"userbot_id" json:"userbot_id"`
	Tier               string    `bson:"tier" json:"tier"` // Free | Pro | Pro++ | Vip
	CanCustomWatermark bool      `bson:"can_custom_watermark" json:"can_custom_watermark"`
	ExpiresAt          time.Time `bson:"expires_at" json:"expires_at"`
	CreatedAt          time.Time `bson:"created_at" json:"created_at"`
	Status             string    `bson:"status" json:"status"` // active | expired
}
