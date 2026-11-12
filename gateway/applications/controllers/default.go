package controllers

import (
	"assterisk/gateway/applications/botgotd"
	"assterisk/gateway/applications/models"
	"context"
)

// DefaultHandler untuk pesan teks yang tidak cocok dengan handler manapun
func (h *Handler) DefaultHandler(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
	if update.Message == nil {
		return
	}

	payload := models.PToService{
		ChatID:   update.Message.Chat.ID,
		UserID:   update.Message.From.ID,
		Username: update.Message.From.Username,
		Text:     update.Message.Text,
		Type:     "message",
	}
	h.broker.To(ctx, "to:default", payload)
}
