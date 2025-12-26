package controllers

import (
	"assterisk/gateway/applications/broker"
	"context"

	"github.com/go-telegram/bot"
)

type Init struct{}

type Handler struct {
	broker *broker.Redis
}

func NewHandler(broker *broker.Redis) *Handler {
	return &Handler{broker: broker}
}

func (m *Init) Register(ctx context.Context, b *bot.Bot, r *broker.Redis) {
	h := NewHandler(r)

	/*
	* Telegram router */

	// Listener
	go r.Listener(ctx, b, "from:group:ubot", "from:worker:ubot", "from:ubot", h.SendMessage)
	// User
	go b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, h.User)
	go b.RegisterHandler(bot.HandlerTypeMessageText, "/status", bot.MatchTypeExact, h.User)

	// Ubot
	go b.RegisterHandler(bot.HandlerTypeMessageText, "/broadcast", bot.MatchTypeExact, h.Ubot)
}
