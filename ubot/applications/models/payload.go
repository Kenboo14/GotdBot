package models

// GatewayToBackend adalah data yang dikirim ke backend
type PToService struct {
	ChatID   int64   `json:"chat_id"`
	UserID   int64   `json:"user_id"`
	OwnerID  int64   `json:"owner_id,omitempty"`
	Username string  `json:"username"`
	Text     string  `json:"text"`
	IsBot    bool    `json:"is_bot"`
	Type     string  `json:"type"`
	TaskID   string  `json:"task_id,omitempty"`
	Target   []int64 `json:"target,omitempty"` // Target user IDs untuk broadcast
}

// BackendToGateway adalah data balasan dari backend
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
}

// UserMessage untuk broadcast ke multiple users
type UserMessage struct {
	UserID   int64  `json:"user_id"`
	ChatID   int64  `json:"chat_id"`
	Username string `json:"username"`
	Text     string `json:"text"`
}

// BroadcastRequest untuk request broadcast
type BroadcastRequest struct {
	UserID    int64   `json:"user_id"`
	ChatID    int64   `json:"chat_id"`
	Username  string  `json:"username"`
	Text      string  `json:"text"`
	TargetIDs []int64 `json:"target_ids"`
	Type      string  `json:"type"`
}

// BroadcastResponse response dari broadcast
type BroadcastResponse struct {
	TotalSent int64   `json:"total_sent"`
	Failed    int64   `json:"failed"`
	Errors    []Error `json:"errors,omitempty"`
}

type Error struct {
	UserID int64  `json:"user_id"`
	Error  string `json:"error"`
}
