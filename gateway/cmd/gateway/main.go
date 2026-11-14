package main

import (
	"assterisk/gateway/internal/app"
	"context"
	"os"
	"os/signal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	app.Run(ctx)
}
