package app

import (
	"assterisk/gateway/internal/broker"
	"assterisk/gateway/internal/databases"
	"context"
	"log"
	"time"

	gobot "github.com/Kenboo14/GoBot"
)

func WaitShutdown(ctx context.Context, redis *broker.Redis, dbManager *databases.DataManager, pool *gobot.Pool) {
	// Menunggu sinyal stop dari NotifyContext di main
	<-ctx.Done()

	log.Println("⚠️ Shutting down services...")

	if pool != nil {
		pool.Close()
	}

	if redis != nil && redis.Client != nil {
		if err := redis.Client.Close(); err != nil {
			log.Printf("Error closing Redis: %v", err)
		}
	}

	if dbManager != nil {
		if err := dbManager.Close(); err != nil {
			log.Printf("Error closing database manager: %v", err)
		}
	}

	// Beri waktu 5 detik untuk bersih-bersih database sebelum aplikasi benar-benar mati
	_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Println("✅ Shutdown complete.")
}
