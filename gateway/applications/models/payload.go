package models

// GatewayToBackend adalah data yang dikirim ke Python
type PToService struct {
	ChatID   int64  `json:"chat_id"`
	UserID   int64  `json:"user_id"`
	OwnerID  int64  `json:"owner_id,omitempty"`
	Username string `json:"username"`
	Text     string `json:"text"`
	IsBot    bool   `json:"is_bot"`
	Type     string `json:"type"`
	TaskID   string `json:"task_id,omitempty"`
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
}
