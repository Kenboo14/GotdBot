package app

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

func WaitShutdown(ctx context.Context, mClient *mongo.Client) {
	// Menunggu sinyal stop dari NotifyContext di main
	<-ctx.Done()

	log.Println("⚠️ Shutting down services...")

	// Beri waktu 5 detik untuk bersih-bersih database sebelum aplikasi benar-benar mati
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mClient.Disconnect(shutdownCtx); err != nil {
		log.Printf("Error during MongoDB disconnect: %v", err)
	}

	log.Println("✅ Shutdown complete.")
}
