package databases

import "time"

type UserConfig struct {
	OwnerID         int64     `bson:"_id" json:"owner_id"`
	Type            string    `bson:"type" json:"type"`
	Delay           int       `bson:"delay" json:"delay"`
	SkipFloodWait   bool      `bson:"skip_floodwait" json:"skip_floodwait"`
	Timer           string    `bson:"timer" json:"timer"`
	Extra           int       `bson:"extra" json:"extra"`
	Mode            string    `bson:"mode" json:"mode"`
	Safemode        bool      `bson:"safemode" json:"safemode"`
	Status          string    `bson:"status" json:"status"`
	MasaAktif       string    `bson:"masa_aktif" json:"masa_aktif"`
	Notifikasi      string    `bson:"notifikasi" json:"notifikasi"`
	Email           string    `bson:"email" json:"email"`
	Admin           string    `bson:"admin" json:"admin"`
	Watermark       string    `bson:"watermark" json:"watermark"`
	TopicEnabled    bool      `bson:"topic_enabled" json:"topic_enabled"`
	TindakanTerkini string    `bson:"tindakan_terkini" json:"tindakan_terkini"`
	TindakanTerakhir string   `bson:"tindakan_terakhir" json:"tindakan_terakhir"`
	UpdatedAt       time.Time `bson:"updated_at" json:"updated_at"`
}

type UserList struct {
	ID        int64     `bson:"-" json:"id"`
	MongoID   string    `bson:"_id,omitempty" json:"mongo_id"`
	OwnerID   int64     `bson:"owner_id" json:"owner_id"`
	Text      string    `bson:"text" json:"text"`
	MediaType string    `bson:"media_type,omitempty" json:"media_type"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

type UserGroup struct {
	ID        int64     `bson:"-" json:"id"`
	MongoID   string    `bson:"_id,omitempty" json:"mongo_id"`
	OwnerID   int64     `bson:"owner_id" json:"owner_id"`
	Target    string    `bson:"target" json:"target"`
	Title     string    `bson:"title" json:"title"`
	ChatID    int64     `bson:"chat_id" json:"chat_id"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

type UserTopic struct {
	ID        int64     `bson:"-" json:"id"`
	MongoID   string    `bson:"_id,omitempty" json:"mongo_id"`
	OwnerID   int64     `bson:"owner_id" json:"owner_id"`
	ChatID    int64     `bson:"chat_id" json:"chat_id"`
	TopicID   int       `bson:"topic_id" json:"topic_id"`
	Title     string    `bson:"title" json:"title"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

