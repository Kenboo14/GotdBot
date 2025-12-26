package databases

import (
	"context"
	"log"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoClient struct {
	Client     *mongo.Client
	Database   *mongo.Database
	Collection *mongo.Collection
}

func NewMongoClient(ctx context.Context, uri, dbName string) (*MongoClient, error) {

	opt := options.Client().ApplyURI(uri)
	opt.SetMaxPoolSize(20)
	opt.SetMinPoolSize(5)
	opt.SetRetryReads(false)
	opt.SetRetryWrites(false)

	c, err := mongo.Connect(ctx, opt)

	if err != nil {
		return nil, err
	}

	// Test Connections
	if err := c.Ping(ctx, nil); err != nil {
		return nil, err
	}

	log.Println("🔌 Connected to Mongo:", uri)

	return &MongoClient{Client: c, Database: c.Database(dbName)}, nil
}
