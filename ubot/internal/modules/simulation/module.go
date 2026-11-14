package simulation

import (
	"assterisk/ubot/internal/databases"
	"context"

	"go.mongodb.org/mongo-driver/mongo"
)

type Init struct{}

func (m *Init) Register(ctx context.Context, sqlite *databases.SQLiteClient, db *mongo.Database, redis *databases.Redis) {
	if redis == nil || redis.Client == nil {
		return
	}
	engine := NewSimulationEngine(redis.Client)
	go engine.ListenControl(ctx)
}
