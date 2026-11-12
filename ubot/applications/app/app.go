package app

import (
	"assterisk/ubot/applications/config"
	"assterisk/ubot/applications/databases"
	"assterisk/ubot/applications/modules/broadcast"
	"assterisk/ubot/applications/modules/userbot"
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

type Module interface {
	Register(context.Context, *databases.SQLiteClient, *mongo.Database, *databases.Redis)
}

func Run(ctx context.Context) {
	ctx_timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cfg, err := config.Load("configs.yaml")
	if err != nil {
		log.Fatal("failed to load config:", err)
	}

	// 1. Primary Database: Pure-Go SQLite
	sqliteClient, err := databases.NewSQLiteClient(cfg.Sqlite.Path)
	if err != nil {
		log.Fatal("Failed Connect SQLite:", err)
	}

	// 2. Offsite Backup Database: MongoDB Cloud (Optional, retried in background)
	var mongoClient *databases.MongoClient
	var mongoDB *mongo.Database
	if cfg.MongoBackup.URI != "" {
		mongoCh := make(chan *databases.MongoClient, 1)
		go func() {
			// ctx (parent, long-lived) agar retry tidak dipotong ctx_timeout 10s
			mClient, retryErr := databases.NewMongoClient(ctx, cfg.MongoBackup.URI, cfg.MongoBackup.DBName)
			if retryErr != nil {
				log.Printf("⚠️ Failed to initialize MongoDB backup (bypassing): %v", retryErr)
			}
			mongoCh <- mClient
		}()
		// Tunggu sebentar (non-blocking) agar startup cepat; MongoDB tersambung di background
		select {
		case mClient := <-mongoCh:
			if mClient != nil {
				mongoClient = mClient
				mongoDB = mClient.Database
			}
		case <-ctx_timeout.Done():
			// MongoDB belum siap saat startup — akan tersambung di goroutine background
			log.Printf("ℹ️ MongoDB not ready at startup, connecting in background...")
		}
	}

	// 3. Broker: Redis
	redisClient, err := databases.NewRedis(ctx_timeout, cfg.Redis.Address, cfg.Redis.Password, cfg.Redis.TLS)
	if err != nil {
		log.Fatal("Failed Connect Redis:", err)
	}

	// 4. Modules
	modules := []Module{
		&broadcast.Init{},
		&userbot.Init{Config: cfg},
	}

	for _, m := range modules {
		m.Register(ctx, sqliteClient, mongoDB, redisClient)
	}

	startPprof(ctx)

	var rawMongoClient *mongo.Client
	if mongoClient != nil {
		rawMongoClient = mongoClient.Client
	}
	WaitShutdown(ctx, sqliteClient, rawMongoClient)
}
