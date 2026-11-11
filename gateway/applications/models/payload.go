package models

// GatewayToBackend adalah data yang dikirim ke Python
type PToService struct {
	ChatID   int64           `json:"chat_id"`
	UserID   int64           `json:"user_id"`
	OwnerID  int64           `json:"owner_id,omitempty"`
	Username string          `json:"username"`
	Text     string          `json:"text"`
	IsBot    bool            `json:"is_bot"`
	Type     string          `json:"type"`
	TaskID   string          `json:"task_id,omitempty"`
	Entities []MessageEntity `json:"entities,omitempty"`
	MediaCacheIDs []int `json:"media_cache_ids,omitempty"`
}

type MessageEntity struct {
	Type          string `json:"type"`
	Offset        int    `json:"offset"`
	Length        int    `json:"length"`
	URL           string `json:"url,omitempty"`
	Language      string `json:"language,omitempty"`
	CustomEmojiID string `json:"custom_emoji_id,omitempty"`
	UserID        int64  `json:"user_id,omitempty"`
}

// BackendToGateway adalah data balasan dari Python
type PFromService struct {
	ChatID   int64    `json:"chat_id"`
	Text     string   `json:"text"`
	State    string   `json:"state,omitempty"`
	Type     string   `json:"type,omitempty"`
	OwnerID  int64    `json:"owner_id,omitempty"`
	TaskID   string   `json:"task_id,omitempty"`
	Total    int      `json:"total,omitempty"`
	Progress int      `json:"progress,omitempty"`
	Success  int      `json:"success,omitempty"`
	Failed   int      `json:"failed,omitempty"`
	Errors   []string `json:"errors,omitempty"`
	MediaCacheID int `json:"media_cache_id,omitempty"`
}
