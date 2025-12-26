package models

// GatewayToBackend adalah data yang dikirim ke Python
type PToService struct {
	ChatID   int64  `json:"chat_id"`
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Text     string `json:"text"`
	IsBot    bool   `json:"is_bot"`
	Type     string `json:"type"`
}

// BackendToGateway adalah data balasan dari Python
type PFromService struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}
