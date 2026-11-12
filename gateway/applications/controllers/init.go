package controllers

import (
	"assterisk/gateway/applications/botgotd"
	"assterisk/gateway/applications/broker"
	"assterisk/gateway/applications/databases"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
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
	cacheMediaMu     sync.RWMutex
	cacheMedia       map[int]cachedMediaFile
	detailMu         sync.Mutex
	detailMessages   map[int64][]int
}

type cachedMediaFile struct {
	Kind   string
	FileID string
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
		cacheMedia:       make(map[int]cachedMediaFile),
		detailMessages:   make(map[int64][]int),
	}
}

func (h *Handler) sendProgress(ctx context.Context, b *botgotd.Bot, chatID int64, params *botgotd.SendMessageParams) {
	h.progressMu.Lock()
	oldID := h.progressMessages[chatID]
	delete(h.progressMessages, chatID)
	h.progressMu.Unlock()
	if oldID > 0 {
		_, _ = b.DeleteMessage(ctx, &botgotd.DeleteMessageParams{ChatID: chatID, MessageID: oldID})
	}
	message, err := b.SendMessage(ctx, params)
	if err == nil && message != nil {
		h.progressMu.Lock()
		h.progressMessages[chatID] = message.ID
		h.progressMu.Unlock()
	}
}

func (h *Handler) clearProgress(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.progressMu.Lock()
	id := h.progressMessages[chatID]
	delete(h.progressMessages, chatID)
	h.progressMu.Unlock()
	if id > 0 {
		_, _ = b.DeleteMessage(ctx, &botgotd.DeleteMessageParams{ChatID: chatID, MessageID: id})
	}
}

func (h *Handler) trackDetailMessageIDs(ctx context.Context, chatID int64, ids []int) {
	h.detailMu.Lock()
	h.detailMessages[chatID] = ids
	h.detailMu.Unlock()

	if h.broker != nil && h.broker.Client != nil {
		if data, err := json.Marshal(ids); err == nil {
			_ = h.broker.Client.Set(ctx, fmt.Sprintf("panel:detail:msgids:%d", chatID), string(data), 30*time.Minute).Err()
		}
	}
}

func (h *Handler) clearAndGetDetailMessageIDs(ctx context.Context, chatID int64) []int {
	h.detailMu.Lock()
	ids := h.detailMessages[chatID]
	delete(h.detailMessages, chatID)
	h.detailMu.Unlock()

	if len(ids) == 0 && h.broker != nil && h.broker.Client != nil {
		if val, err := h.broker.Client.Get(ctx, fmt.Sprintf("panel:detail:msgids:%d", chatID)).Result(); err == nil && val != "" {
			_ = json.Unmarshal([]byte(val), &ids)
		}
	}
	if h.broker != nil && h.broker.Client != nil {
		_ = h.broker.Client.Del(ctx, fmt.Sprintf("panel:detail:msgids:%d", chatID)).Err()
	}
	return ids
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

func (m *Init) Register(ctx context.Context, b *botgotd.Bot, r *broker.Redis) {
	SetInlineRedis(r)
	h := NewHandler(r, m.DB)
	go startSessionEventWebSocket(ctx, r)

	// Listener dari ubot
	go r.Listener(ctx, b, "from:group:ubot", "from:worker:ubot", "from:ubot", h.SendMessage)

	// Register specific handlers before the catch-all state handler.
	registerBotCommands(b, h)
	b.RegisterHandler(botgotd.HandlerTypeCallbackQueryData, "panel:", botgotd.MatchTypePrefix, h.PanelCallback)
	b.RegisterHandlerMatchFunc(func(update *botgotd.Update) bool {
		if update == nil {
			return false
		}
		m := update.Message
		if m == nil {
			m = update.ChannelPost
		}
		if m == nil {
			return false
		}
		return m.Caption != "" || m.Animation != nil || m.Audio != nil || m.Document != nil || len(m.Photo) > 0 || m.Video != nil || m.VideoNote != nil || m.Voice != nil || m.Sticker != nil
	}, h.HandlePanelMediaUpdate)
	b.RegisterHandler(botgotd.HandlerTypeMessageText, "", botgotd.MatchTypePrefix, h.HandleMessage)
}
