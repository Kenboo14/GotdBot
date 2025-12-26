package app

import (
	"assterisk/gateway/applications/broker"
	"assterisk/gateway/applications/config"
	"assterisk/gateway/applications/controllers"
	"context"
	"log"
	"time"

	"github.com/go-telegram/bot"
)

type Module interface {
	Register(context.Context, *bot.Bot, *broker.Redis)
}

func Run(ctx context.Context) {

	ctx_timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Config
	cfg, err := config.Load("configs.yaml")
	if err != nil {
		log.Fatal("failed to load config:", err)
	}

	// Redis / Message Broker
	redis, err := broker.NewRedis(ctx_timeout, cfg.Redis.Address, cfg.Redis.Password)
	if err != nil {
		log.Fatal("Failed Connect Redis:", err)
	}

	// Modules
	modules := []Module{
		&controllers.Init{},
	}

	opts := []bot.Option{
		bot.WithMiddlewares(),
	}
	// Telegram Bot
	b, err := bot.New(cfg.Telegram.BotToken, opts...)
	if err != nil {
		log.Fatal("❌ Critical: Gagal inisialisasi bot:", err)
	}

	go func() {
		for _, m := range modules {
			go m.Register(ctx, b, redis)
		}
	}()

	go WaitShutdown(ctx)

	b.Start(ctx)
}
