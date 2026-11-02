package middleware

import (
	"context"
	"log"

	"github.com/gotd/td/tg"
)

// UserbotHandlerFunc defines a standard update handler for MTProto message updates.
type UserbotHandlerFunc func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewMessage) error

// LogUserbotMessage logs incoming MTProto message updates before forwarding them to the next handler.
func LogUserbotMessage(next UserbotHandlerFunc) UserbotHandlerFunc {
	return func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewMessage) error {
		if msg, ok := update.Message.(*tg.Message); ok {
			log.Printf("[userbot] update received msg_id=%d", msg.ID)
		}
		return next(ctx, entities, update)
	}
}
