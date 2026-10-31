package userbot

import (
	"assterisk/ubot/applications/config"
	"assterisk/ubot/applications/databases"
	"context"
	"encoding/base64"
	"log"

	"go.mongodb.org/mongo-driver/mongo"
)

// Init owns short-lived MTProto login clients. Persisted sessions are restored
// by the next runtime module; login state itself never leaves this process.
type Init struct{ Config *config.Config }

func (m *Init) Register(ctx context.Context, db *mongo.Database, redis *databases.Redis) {
	key, err := base64.StdEncoding.DecodeString(m.Config.Userbot.SessionEncryptionKey)
	if err != nil {
		log.Printf("userbot disabled: invalid encryption key: %v", err)
		return
	}
	service := NewService(ctx, db, redis, m.Config.Userbot.APIID, m.Config.Userbot.APIHash, key, m.Config.Telegram.BotToken)
	go service.Restore(ctx)
	go redis.Listener(ctx, "to:group:ubot", "to:worker:auth", "to:ubot:auth", service.Handle)
	go redis.Listener(ctx, "to:group:command", "to:worker:command", "to:ubot:command", service.HandleCommand)
}
