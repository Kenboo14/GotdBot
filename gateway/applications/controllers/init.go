package controllers

import (
	"assterisk/gateway/applications/broker"
	"context"
	"sync"

	"github.com/go-telegram/bot"
)

type Init struct{}

type Handler struct {
	broker         *broker.Redis
	Sessions       *SessionManager
	broadcastMu    sync.Mutex
	broadcastTasks map[string]broadcastMonitor
}

type broadcastMonitor struct {
	ChatID    int64
	MessageID int
}

func NewHandler(broker *broker.Redis) *Handler {
	return &Handler{
		broker:         broker,
		Sessions:       NewSessionManager(),
		broadcastTasks: make(map[string]broadcastMonitor),
	}
}

func (m *Init) Register(ctx context.Context, b *bot.Bot, r *broker.Redis) {
	SetInlineRedis(r)
	h := NewHandler(r)

	// Listener dari ubot
	go r.Listener(ctx, b, "from:group:ubot", "from:worker:ubot", "from:ubot", h.SendMessage)

	// Register specific handlers before the catch-all state handler.
	registerBotCommands(b, h)
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, h.HandleMessage)
}
