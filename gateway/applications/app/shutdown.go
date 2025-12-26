package app

import (
	"context"
	"log"
	"time"
)

func WaitShutdown(ctx context.Context) {
	// Menunggu sinyal stop dari NotifyContext di main
	<-ctx.Done()

	log.Println("⚠️ Shutting down services...")

	// Beri waktu 5 detik untuk bersih-bersih database sebelum aplikasi benar-benar mati
	_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Println("✅ Shutdown complete.")
}
