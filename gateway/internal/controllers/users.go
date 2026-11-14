package controllers

import (
	"assterisk/gateway/internal/assets"
	"assterisk/gateway/internal/botgotd"
	"context"
	"strings"
)

func (h *Handler) User(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID
	userName := update.Message.From.FirstName
	if update.Message.From.LastName != "" {
		userName += " " + update.Message.From.LastName
	}
	cmd := strings.TrimSpace(update.Message.Text)

	// Periksa apakah sesi userbot telah aktif (di SQLite lokal atau MongoDB)
	hasActive := false
	if h.db != nil {
		hasActive = h.db.HasActiveSession(ctx, userID)
	}

	// Jika user sudah memiliki userbot aktif dan mengetik /start, tampilkan main control panel
	if hasActive && (cmd == "/start" || strings.HasPrefix(cmd, "/start")) {
		h.ShowMainMenu(ctx, b, chatID, userID, userName)
		return
	}

	// Jika sedang dalam wizard pemasangan aktif
	h.Sessions.mu.RLock()
	existingSession := h.Sessions.sessions[chatID]
	h.Sessions.mu.RUnlock()

	if existingSession != nil && existingSession.State != "waiting_create_choice" {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        assets.TxtPemasanganSedangBerjalan,
			ParseMode:   "HTML",
			ReplyMarkup: assets.CancelOnlyMarkup,
		})
		return
	}

	// Buat sesi baru dan tampilkan pilihan pemasangan
	h.Sessions.mu.Lock()
	h.Sessions.sessions[chatID] = &UserSession{ChatID: chatID, State: "waiting_create_choice"}
	h.Sessions.mu.Unlock()

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        assets.TxtStartGreeting(userName),
		ParseMode:   "HTML",
		ReplyMarkup: assets.CreateChoiceMarkup,
	})
}
