package controllers

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/go-telegram/bot"
	t_models "github.com/go-telegram/bot/models"
)

func (h *Handler) User(ctx context.Context, b *bot.Bot, update *t_models.Update) {
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

	// Tampilan RichText bila sesi belum terpasang
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("👋 <b>Selamat Datang, <a href=\"tg://user?id=%d\">%s</a>!</b>\n\n", userID, html.EscapeString(userName)))
	sb.WriteString("<blockquote>⚠️ <b>Sesi Userbot Belum Aktif</b>\n")
	sb.WriteString("<i>Akun Telegram Anda belum terhubung dengan mesin GotdBot Userbot.</i></blockquote>\n\n")
	sb.WriteString("Silakan ketik <code>/create</code> untuk memasang dan mengaktifkan sesi userbot Anda, atau hubungi administrator untuk bantuan.")

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      sb.String(),
		ParseMode: "HTML",
	})
}
