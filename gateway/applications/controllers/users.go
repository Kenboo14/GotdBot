package controllers

import (
	"assterisk/gateway/applications/models"
	"context"

	"github.com/go-telegram/bot"
	t_models "github.com/go-telegram/bot/models"
)

func (h *Handler) User(ctx context.Context, b *bot.Bot, update *t_models.Update) {
	payload := models.PToService{
		ChatID:   update.Message.Chat.ID,
		UserID:   update.Message.From.ID,
		Username: update.Message.From.Username,
		Text:     update.Message.Text,
		Type:     "command", // Kita tandai sebagai command
	}
	h.broker.To(ctx, "to:users", payload)
}
