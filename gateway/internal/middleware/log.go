package middleware

import (
	"assterisk/gateway/internal/botgotd"
	"context"
	"log"
)

func Chain(next botgotd.HandlerFunc) botgotd.HandlerFunc {
	return func(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
		if update.Message != nil && update.Message.From != nil {
			log.Printf("%d say: %s", update.Message.From.ID, update.Message.Text)
		}
		next(ctx, b, update)
	}
}

func Denial(next botgotd.HandlerFunc) botgotd.HandlerFunc {
	return func(ctx context.Context, b *botgotd.Bot, update *botgotd.Update) {
		if update.Message != nil && update.Message.From != nil {
			log.Printf("%d denial: %s", update.Message.From.ID, update.Message.Text)
		}
		next(ctx, b, update)
	}
}
