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

	// Tampilan pesan bila sesi belum terpasang
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>👋 Halo, %s!</b>\n━━━━━━━━━━━━━━━━━━━━\n\n", html.EscapeString(userName)))
	sb.WriteString("⚠️ <b>Sesi Userbot Belum Terpasang</b>\n")
	sb.WriteString("Akun Telegram Anda belum terhubung ke sistem GotdBot.\n\n")
	sb.WriteString("Ketik <code>/create</code> untuk memulai proses pemasangan userbot.\n\n")
	sb.WriteString("<i>GotdBot • Userbot Manager</i>")

	b.SendMessage(ctx, &botgotd.SendMessageParams{
		ChatID:    chatID,
		Text:      sb.String(),
		ParseMode: "HTML",
	})
}
