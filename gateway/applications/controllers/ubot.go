package controllers

import (
	"assterisk/gateway/applications/models"
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/go-telegram/bot"
	t_models "github.com/go-telegram/bot/models"
	"github.com/redis/go-redis/v9"
)

func (h *Handler) Ubot(ctx context.Context, b *bot.Bot, update *t_models.Update) {

	identifier := "ubot:broadcast"
	allowed, estimated, errLimit := h.broker.CheckLimit(ctx, identifier, 1, 5*time.Second)

	if errLimit != nil {
		log.Println("Redis Error:", errLimit)
		// Putuskan mau fail open (lanjut aja) atau fail close (error)
	}

	if !allowed {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   `Jangan Spam!!! Tunggu ` + estimated.String() + " Lagi",
		})

		return
	}

	payload := models.PToService{
		ChatID:   update.Message.Chat.ID,
		UserID:   update.Message.From.ID,
		Username: update.Message.From.Username,
		Text:     update.Message.Text,
		Type:     "command", // Kita tandai sebagai command
	}
	err := h.broker.To(ctx, "to:ubot:broadcast", payload)
	if err != nil {
		log.Printf("Redis Stream: %v", err)
	}
}

func (h *Handler) SendMessage(ctx context.Context, b *bot.Bot, msg redis.XMessage) {

	var dta map[string]interface{}
	json.Unmarshal([]byte(msg.Values["payload"].(string)), &dta)

	chat_id := int64(dta["chat_id"].(float64))
	text := dta["text"].(string)

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chat_id,
		Text:   text,
	})
}
