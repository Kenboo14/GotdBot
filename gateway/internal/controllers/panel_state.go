package controllers

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type cachedMediaFile struct {
	Kind   string
	FileID string
}

type panelMediaBatch struct {
	ChatID    int64
	UserID    int64
	State     string
	SourceIDs []int
	Caption   string
	Entities  []models.MessageEntity
	Timer     *time.Timer
}

type broadcastMonitor struct {
	ChatID    int64
	MessageID int
}

// GetEffectiveUserID returns targetOwnerID if remote target is active, else userID
func (h *Handler) GetEffectiveUserID(chatID, userID int64) int64 {
	h.remoteMu.RLock()
	defer h.remoteMu.RUnlock()
	if h.remoteTargets == nil {
		return userID
	}
	targetID, ok := h.remoteTargets[chatID]
	if ok && targetID > 0 {
		return targetID
	}
	return userID
}

// SetActiveRemoteTarget sets or clears the remote management target for a chat
func (h *Handler) SetActiveRemoteTarget(chatID, targetID int64) {
	h.remoteMu.Lock()
	if h.remoteTargets == nil {
		h.remoteTargets = make(map[int64]int64)
	}
	if targetID <= 0 {
		delete(h.remoteTargets, chatID)
	} else {
		h.remoteTargets[chatID] = targetID
	}
	h.remoteMu.Unlock()
}

// GetActiveRemoteTarget retrieves the remote target ID if set
func (h *Handler) GetActiveRemoteTarget(chatID int64) (int64, bool) {
	h.remoteMu.RLock()
	defer h.remoteMu.RUnlock()
	if h.remoteTargets == nil {
		return 0, false
	}
	targetID, ok := h.remoteTargets[chatID]
	return targetID, ok && targetID > 0
}

// SetAccountName caches and persists an account name
func (h *Handler) SetAccountName(userID int64, name string) {
	if name == "" {
		return
	}
	h.accountNamesMu.Lock()
	if h.accountNames == nil {
		h.accountNames = make(map[int64]string)
	}
	h.accountNames[userID] = name
	h.accountNamesMu.Unlock()
	if h.db != nil {
		h.db.UpdateAccountName(context.Background(), userID, name)
	}
}

// GetAccountName returns the cached or persisted account name
func (h *Handler) GetAccountName(userID int64) string {
	h.accountNamesMu.RLock()
	name, ok := h.accountNames[userID]
	h.accountNamesMu.RUnlock()
	if ok && name != "" {
		return name
	}
	if h.db != nil {
		return h.db.GetAccountName(context.Background(), userID)
	}
	return ""
}

// sendProgress updates the single progress message in a chat, deleting the previous one
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

// clearProgress removes any tracked progress message in a chat
func (h *Handler) clearProgress(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.progressMu.Lock()
	id := h.progressMessages[chatID]
	delete(h.progressMessages, chatID)
	h.progressMu.Unlock()
	if id > 0 {
		_, _ = b.DeleteMessage(ctx, &botgotd.DeleteMessageParams{ChatID: chatID, MessageID: id})
	}
}

// setPanelPrompt records the message ID of an interactive input prompt
func (h *Handler) setPanelPrompt(chatID int64, messageID int) {
	h.promptMu.Lock()
	defer h.promptMu.Unlock()
	h.promptMessages[chatID] = messageID
}

// clearPanelPrompt deletes and removes tracking for an interactive prompt message
func (h *Handler) clearPanelPrompt(ctx context.Context, b *botgotd.Bot, chatID int64) {
	h.promptMu.Lock()
	id := h.promptMessages[chatID]
	delete(h.promptMessages, chatID)
	h.promptMu.Unlock()
	if id > 0 {
		_, _ = b.DeleteMessage(ctx, &botgotd.DeleteMessageParams{ChatID: chatID, MessageID: id})
	}
}

// trackDetailMessageIDs records detail message IDs both in memory and Redis cache
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

// clearAndGetDetailMessageIDs fetches and clears all tracked detail message IDs
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

// setPanelState sets or clears the current wizard state for a chat
func (h *Handler) setPanelState(chatID int64, state string) {
	h.panelStatesMu.Lock()
	defer h.panelStatesMu.Unlock()
	if state == "" {
		delete(h.panelStates, chatID)
	} else {
		h.panelStates[chatID] = state
	}
}

// getPanelState gets the current wizard state for a chat
func (h *Handler) getPanelState(chatID int64) string {
	h.panelStatesMu.RLock()
	defer h.panelStatesMu.RUnlock()
	return h.panelStates[chatID]
}
