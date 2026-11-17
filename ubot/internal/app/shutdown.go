package app

import (
	"assterisk/ubot/internal/databases"
	"context"
	"log"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"go.mongodb.org/mongo-driver/mongo"
)

func WaitShutdown(ctx context.Context, sqliteClient *databases.SQLiteClient, mClient *mongo.Client, redisClient *databases.Redis) {
	<-ctx.Done()

	log.Println("⚠️ Shutting down services...")

	gobot.DefaultPool().Close()

	if redisClient != nil && redisClient.Client != nil {
		if err := redisClient.Client.Close(); err != nil {
			log.Printf("Error closing Redis: %v", err)
		}
	}

	if sqliteClient != nil {
		if err := sqliteClient.Close(); err != nil {
			log.Printf("Error closing SQLite: %v", err)
		}
	}

	if mClient != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := mClient.Disconnect(shutdownCtx); err != nil {
			log.Printf("Error during MongoDB disconnect: %v", err)
		}
	}

	log.Println("✅ Shutdown complete.")
}
