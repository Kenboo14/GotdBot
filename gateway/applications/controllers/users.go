package controllers

import (
	"assterisk/gateway/applications/botgotd"
	"context"
	"fmt"
	"html"
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

	if hasActive {
		if strings.HasPrefix(cmd, "/status") {
			h.ShowStatus(ctx, b, chatID, userID)
			return
		}
		h.ShowMainMenu(ctx, b, chatID, userID, userName)
		return
	}

	// Cek jika user sudah dalam proses pemasangan (session aktif)
	h.Sessions.mu.RLock()
	existingSession := h.Sessions.sessions[chatID]
	h.Sessions.mu.RUnlock()

	if existingSession != nil {
		b.SendMessage(ctx, &botgotd.SendMessageParams{
			ChatID:      chatID,
			Text:        "⏳ <b>Pemasangan sedang berlangsung.</b>\nLanjutkan proses atau ketik /cancel untuk membatalkan.",
			ParseMode:   "HTML",
			ReplyMarkup: cachedCancelMarkup,
		})
		return
	}

	// Buat sesi baru dan tampilkan pilihan pemasangan
	h.Sessions.mu.Lock()
	h.Sessions.sessions[chatID] = &UserSession{ChatID: chatID, State: "waiting_create_choice"}
	h.Sessions.mu.Unlock()

	greeting := fmt.Sprintf(
		"👋 <b>Selamat datang, %s!</b>\n"+
			"━━━━━━━━━━━━━━━━━━━━\n"+
			"<b>GotdBot • Userbot Manager</b>\n\n"+
			"• <b>🔑 Input Token</b> — Masukkan Voucher Master dari Owner atau Token SHA Backup Anda.\n"+
			"• <b>🚀 Buat Userbot</b> — Sambungkan ulang otomatis menggunakan lisensi aktif Anda.",
		html.EscapeString(userName),
	)

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:      chatID,
		Text:        greeting,
		ParseMode:   "HTML",
		ReplyMarkup: cachedCreateChoiceMarkup,
	})
}
