package app

import (
	"assterisk/gateway/applications/broker"
	"assterisk/gateway/applications/config"
	"assterisk/gateway/applications/controllers"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-telegram/bot"
	t_models "github.com/go-telegram/bot/models"
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
	redis, err := broker.NewRedis(ctx_timeout, cfg.Redis.Address, cfg.Redis.Password, cfg.Redis.TLS)
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

	for _, m := range modules {
		m.Register(ctx, b, redis)
	}

	go WaitShutdown(ctx)

	startPolling(ctx, b, controllers.HandleInline)
}

// startPolling owns the single getUpdates loop. go-telegram/bot v1.17 does
// not expose an InlineQuery handler type, so inline_query updates are handled
// by the supplied callback while all other updates use the library dispatcher.
func startPolling(ctx context.Context, b *bot.Bot, inlineHandler func(context.Context, *bot.Bot, *t_models.Update)) {
	client := &http.Client{Timeout: 65 * time.Second}
	offset := int64(0)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		params := url.Values{"timeout": {"50"}, "allowed_updates": {`["message","callback_query","inline_query"]`}}
		if offset > 0 {
			params.Set("offset", strconv.FormatInt(offset, 10))
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.telegram.org/bot"+b.Token()+"/getUpdates?"+params.Encode(), nil)
		if err != nil {
			log.Printf("Telegram polling request: %v", err)
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("Telegram polling: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		var envelope struct {
			OK     bool              `json:"ok"`
			Result []json.RawMessage `json:"result"`
		}
		err = json.NewDecoder(resp.Body).Decode(&envelope)
		resp.Body.Close()
		if err != nil || !envelope.OK {
			time.Sleep(3 * time.Second)
			continue
		}
		for _, raw := range envelope.Result {
			var update t_models.Update
			if json.Unmarshal(raw, &update) != nil {
				continue
			}
			if update.ID >= offset {
				offset = update.ID + 1
			}
			if update.InlineQuery != nil && inlineHandler != nil {
				inlineHandler(ctx, b, &update)
				continue
			}
			b.ProcessUpdate(ctx, &update)
		}
	}
}
