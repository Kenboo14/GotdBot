package app

import (
	"assterisk/ubot/applications/config"
	"assterisk/ubot/applications/databases"
	"assterisk/ubot/applications/modules/broadcast"
	"time"

	"context"
	"log"

	"go.mongodb.org/mongo-driver/mongo"
)

type Module interface {
	Register(context.Context, *mongo.Database, *databases.Redis)
}

func Run(ctx context.Context) {
	ctx_timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cfg, err := config.Load("configs.yaml")
	if err != nil {
		log.Fatal("failed to load config:", err)
	}

	// Database
	mongoClient, err := databases.NewMongoClient(ctx_timeout, cfg.Mongo.URI, cfg.Mongo.DBName)
	if err != nil {
		log.Fatal("Failed Connect Mongodb:", err)
	}

	redisClient, err := databases.NewRedis(ctx_timeout, cfg.Redis.Address, cfg.Redis.Password)
	if err != nil {
		log.Fatal("Failed Connect Redis:", err)
	}
	// Modules
	modules := []Module{
		&broadcast.Init{},
	}

	for _, m := range modules {
		m.Register(ctx, mongoClient.Database, redisClient)
	}

	WaitShutdown(ctx, mongoClient.Client)

}
