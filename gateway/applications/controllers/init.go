package controllers

import (
	"assterisk/gateway/applications/broker"
	"assterisk/gateway/applications/databases"
	"context"
	"sync"

	"github.com/go-telegram/bot"
	t_models "github.com/go-telegram/bot/models"
)

type Init struct {
	DB *databases.DataManager
}

type Handler struct {
	broker           *broker.Redis
	db               *databases.DataManager
	Sessions         *SessionManager
	broadcastMu      sync.Mutex
	broadcastTasks   map[string]broadcastMonitor
	panelStatesMu    sync.RWMutex
	panelStates      map[int64]string
	progressMu       sync.Mutex
	progressMessages map[int64]int
	mediaMu          sync.Mutex
	mediaBatches     map[string]*panelMediaBatch
}

type panelMediaBatch struct {
		ChatID    int64
		UserID    int64
		State     string
		SourceIDs []int
}

type broadcastMonitor struct {
	ChatID    int64
	MessageID int
}

func NewHandler(broker *broker.Redis, db *databases.DataManager) *Handler {
	return &Handler{
		broker:           broker,
		db:               db,
		Sessions:         NewSessionManager(),
		broadcastTasks:   make(map[string]broadcastMonitor),
		panelStates:      make(map[int64]string),
		progressMessages: make(map[int64]int),
		mediaBatches:     make(map[string]*panelMediaBatch),
	}
}

func (h *Handler) sendProgress(ctx context.Context, b *bot.Bot, chatID int64, params *bot.SendMessageParams) {
	h.progressMu.Lock()
	oldID := h.progressMessages[chatID]
	delete(h.progressMessages, chatID)
	h.progressMu.Unlock()
	if oldID > 0 {
		_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: oldID})
	}
	message, err := b.SendMessage(ctx, params)
	if err == nil && message != nil {
		h.progressMu.Lock()
		h.progressMessages[chatID] = message.ID
		h.progressMu.Unlock()
	}
}

func (h *Handler) clearProgress(ctx context.Context, b *bot.Bot, chatID int64) {
	h.progressMu.Lock()
	id := h.progressMessages[chatID]
	delete(h.progressMessages, chatID)
	h.progressMu.Unlock()
	if id > 0 {
		_, _ = b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: id})
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
	go startSessionEventWebSocket(ctx, r)

	// Listener dari ubot
	go r.Listener(ctx, b, "from:group:ubot", "from:worker:ubot", "from:ubot", h.SendMessage)

	// Register specific handlers before the catch-all state handler.
	registerBotCommands(b, h)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "panel:", bot.MatchTypePrefix, h.PanelCallback)
	b.RegisterHandlerMatchFunc(func(update *t_models.Update) bool {
		if update == nil || update.Message == nil || update.Message.From == nil {
			return false
		}
		m := update.Message
		return m.Caption != "" || m.Animation != nil || m.Audio != nil || m.Document != nil || len(m.Photo) > 0 || m.Video != nil || m.VideoNote != nil || m.Voice != nil || m.Sticker != nil
	}, h.HandlePanelMedia)
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, h.HandleMessage)
}
