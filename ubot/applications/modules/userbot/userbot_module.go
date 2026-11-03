package userbot

import (
	"assterisk/ubot/applications/config"
	"assterisk/ubot/applications/databases"
	"context"
	"encoding/base64"
	"log"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Init owns short-lived MTProto login clients. Persisted sessions are restored
// by the next runtime module; login state itself never leaves this process.
type Init struct{ Config *config.Config }

func (m *Init) Register(ctx context.Context, sqlite *databases.SQLiteClient, db *mongo.Database, redis *databases.Redis) {
	key, err := base64.StdEncoding.DecodeString(m.Config.Userbot.SessionEncryptionKey)
	if err != nil {
		log.Printf("userbot disabled: invalid encryption key: %v", err)
		return
	}
	if db != nil {
		ensureMongoIndexes(ctx, db)
	}
	service := NewService(ctx, sqlite, db, redis, m.Config.Userbot.APIID, m.Config.Userbot.APIHash, key, m.Config.Telegram.BotToken, m.Config.Telegram.LogBotToken)
	go service.Restore(ctx)
	go redis.Listener(ctx, "to:group:ubot", "to:worker:auth", "to:ubot:auth", service.Handle)
	go redis.Listener(ctx, "to:group:command", "to:worker:command", "to:ubot:command", service.HandleCommand)
}

func ensureMongoIndexes(ctx context.Context, db *mongo.Database) {
	_, _ = db.Collection("userbot_sudo").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "owner_id", Value: 1}, {Key: "user_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	_, _ = db.Collection("userbot_broadcast_blacklist").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "owner_id", Value: 1}, {Key: "type", Value: 1}, {Key: "chat_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	_, _ = db.Collection("userbot_sessions").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "account_id", Value: 1}}},
		{Keys: bson.D{{Key: "owner_id", Value: 1}}},
	})
	_, _ = db.Collection("userbot_log_media").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "owner_id", Value: 1}}},
		{Keys: bson.D{{Key: "log_bot_id", Value: 1}, {Key: "log_message_id", Value: 1}}},
		{Keys: bson.D{{Key: "source_chat_id", Value: 1}, {Key: "source_message_id", Value: 1}}},
	})
	_, _ = db.Collection("userbot_log_status").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "owner_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	_, _ = db.Collection("userbot_log_routes").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "log_message_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
}
