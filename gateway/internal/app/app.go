package app

import (
	"assterisk/gateway/internal/botgotd"
	"assterisk/gateway/internal/broker"
	"assterisk/gateway/internal/config"
	"assterisk/gateway/internal/controllers"
	"assterisk/gateway/internal/databases"
	"assterisk/gateway/internal/utils"
	"context"
	"log"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

type Module interface {
	Register(context.Context, *botgotd.Bot, *broker.Redis)
}

func Run(ctx context.Context) {
	utils.InitLogger("gateway")

	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 1. Config
	cfg, err := config.Load("configs.yaml")
	if err != nil {
		log.Fatal("failed to load config:", err)
	}

	// 2. Redis / Message Broker
	redis, err := broker.NewRedis(ctxTimeout, cfg.Redis.Address, cfg.Redis.Password, cfg.Redis.TLS)
	if err != nil {
		log.Fatal("Failed Connect Redis:", err)
	}

	// 3. Data Manager (MongoDB + SQLite Sync)
	// ctx        = parent context (long-lived, for MongoDB retry goroutine)
	// ctxTimeout = short 10s context (for immediate startup steps only)
	dbManager, err := databases.NewDataManager(ctx, ctxTimeout, cfg.Mongo.URI, cfg.Mongo.DBName, cfg.SQLite.Path)
	if err != nil {
		log.Printf("⚠️ DataManager warning: %v", err)
	}

	// 4. Peer Storage for MTProto AccessHash resolution
	var peerStorage *botgotd.PeerStorage
	if dbManager != nil && dbManager.GetSQLiteDB() != nil {
		peerStorage = botgotd.NewPeerStorage(dbManager.GetSQLiteDB())
	} else {
		peerStorage = botgotd.NewPeerStorage(nil)
	}

	// 5. Bot Token & Credentials
	token := cfg.Telegram.BotToken
	appID := cfg.Telegram.APIID
	appHash := cfg.Telegram.APIHash
	if appID == 0 {
		appID = 25033414
	}
	if appHash == "" {
		appHash = "c0b6f8537df9fa559e65a14a8e51a100"
	}

	// 6. Bot instance
	botInstance := botgotd.NewBot(nil, token, peerStorage)

	// 7. Register Modules
	modules := []Module{
		&controllers.Init{DB: dbManager},
	}
	for _, m := range modules {
		m.Register(ctx, botInstance, redis)
	}

	// 7a. Watch Redis "config:updated" — sync SQLite saat ubot ubah MongoDB
	if dbManager != nil {
		go dbManager.WatchConfigUpdates(ctx, redis.Client)
	}

	// Register Inline Query handler
	botInstance.RegisterHandler(botgotd.HandlerTypeInlineQuery, "", botgotd.MatchTypePrefix, controllers.HandleInline)

	// Wire MTProto update handlers
	botInstance.SetupUpdateHandlers()

	// 8. MTProto Telegram Client with retry
	opts := telegram.Options{
		UpdateHandler: botInstance.Dispatcher,
	}
	client := telegram.NewClient(appID, appHash, opts)
	botInstance.Client = client
	botInstance.API = tg.NewClient(client)

	go WaitShutdown(ctx)

	log.Println("🚀 Starting GotdBot MTProto Gateway...")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := client.Run(ctx, func(runCtx context.Context) error {
			status, err := client.Auth().Status(runCtx)
			if err != nil {
				return err
			}
			if !status.Authorized {
				log.Printf("🔑 Authenticating Bot via MTProto with token...")
				if _, err := client.Auth().Bot(runCtx, token); err != nil {
					return err
				}
			}

			self, err := client.Self(runCtx)
			if err == nil && self != nil {
				log.Printf("✅ [MTProto Bot] Authenticated as @%s (ID: %d)", self.Username, self.ID)
				peerStorage.SaveUser(self.ID, self.AccessHash)
			}

			// Keep client running
			<-runCtx.Done()
			return runCtx.Err()
		})

		if err != nil && ctx.Err() == nil {
			log.Printf("⚠️ MTProto client disconnected: %v; reconnecting in 5s...", err)
			time.Sleep(5 * time.Second)
		} else {
			break
		}
	}
}
