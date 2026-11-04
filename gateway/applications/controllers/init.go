package controllers

import (
	"assterisk/gateway/applications/broker"
	"assterisk/gateway/applications/databases"
	"context"
	"sync"

	"github.com/go-telegram/bot"
)

type Init struct {
	DB *databases.DataManager
}

type Handler struct {
	broker         *broker.Redis
	db             *databases.DataManager
	Sessions       *SessionManager
	broadcastMu    sync.Mutex
	broadcastTasks map[string]broadcastMonitor
	panelStatesMu  sync.RWMutex
	panelStates    map[int64]string
}

type broadcastMonitor struct {
	ChatID    int64
	MessageID int
}

func NewHandler(broker *broker.Redis, db *databases.DataManager) *Handler {
	return &Handler{
		broker:         broker,
		db:             db,
		Sessions:       NewSessionManager(),
		broadcastTasks: make(map[string]broadcastMonitor),
		panelStates:    make(map[int64]string),
	}
}

func (h *Handler) setPanelState(chatID int64, state string) {
	h.panelStatesMu.Lock()
	defer h.panelStatesMu.Unlock()
	if state == "" {
		delete(h.panelStates, chatID)
	} else {
		h.panelStates[chatID] = state
	}
}

func (h *Handler) getPanelState(chatID int64) string {
	h.panelStatesMu.RLock()
	defer h.panelStatesMu.RUnlock()
	return h.panelStates[chatID]
}

func (m *Init) Register(ctx context.Context, b *bot.Bot, r *broker.Redis) {
	SetInlineRedis(r)
	h := NewHandler(r, m.DB)

	// Listener dari ubot
	go r.Listener(ctx, b, "from:group:ubot", "from:worker:ubot", "from:ubot", h.SendMessage)

	// Register specific handlers before the catch-all state handler.
	registerBotCommands(b, h)
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, h.HandleMessage)
}
