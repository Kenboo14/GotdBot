package controllers

import (
	"context"

	"github.com/go-telegram/bot"
	t_models "github.com/go-telegram/bot/models"
)

func (h *Handler) User(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	if update.Message == nil {
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: "Gunakan /create untuk memulai sesi atau /broadcast <pesan> untuk menguji worker."})
}
