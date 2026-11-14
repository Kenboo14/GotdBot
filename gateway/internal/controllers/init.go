package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/broker"
	"assterisk/gateway/internal/databases"
	"context"
	"sync"
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
	promptMu         sync.Mutex
	promptMessages   map[int64]int
	remoteMu         sync.RWMutex
	remoteTargets    map[int64]int64 // chatID -> targetOwnerID
	accountNamesMu   sync.RWMutex
	accountNames     map[int64]string
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
		promptMessages:   make(map[int64]int),
		remoteTargets:    make(map[int64]int64),
		accountNames:     make(map[int64]string),
	}
}

func (m *Init) Register(ctx context.Context, b *botgotd.Bot, r *broker.Redis) {
	SetInlineRedis(r)
	h := NewHandler(r, m.DB)
	go startSessionEventWebSocket(ctx, r)
	go startTelemetryDashboard(ctx, b, r)

	// Listener dari ubot
	go r.Listener(ctx, b, "from:group:ubot", "from:worker:ubot", "from:ubot", h.SendMessage)

	// Register specific handlers before the catch-all state handler.
	registerBotCommands(b, h)
	b.RegisterHandler(botgotd.HandlerTypeCallbackQueryData, "panel:", botgotd.MatchTypePrefix, h.PanelCallback)
	b.RegisterHandler(botgotd.HandlerTypeCallbackQueryData, "ctrl:", botgotd.MatchTypePrefix, h.PanelCallback)
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
