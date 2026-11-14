package broadcast

import (
	"assterisk/ubot/internal/databases"
	"context"

	"go.mongodb.org/mongo-driver/mongo"
)

type Init struct{}

func (m *Init) Register(ctx context.Context, sqlite *databases.SQLiteClient, mongodb *mongo.Database, redis *databases.Redis) {
	repo := NewRepository(mongodb, redis)
	svc := NewService(repo)

	go redis.Listener(ctx, "to:group:ubot", "to:worker:ubot", "to:ubot:broadcast", svc.Broadcast)
}
