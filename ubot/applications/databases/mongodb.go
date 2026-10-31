package databases

import (
	"context"
	"log"
	"net/url"

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
	if err := c.Ping(ctx, nil); err != nil {
		return nil, err
	}

	displayURI := uri
	if parsed, parseErr := url.Parse(uri); parseErr == nil && parsed.User != nil {
		// Never print the MongoDB password to application or Docker logs.
		parsed.User = url.User(parsed.User.Username())
		displayURI = parsed.String()
	}
	log.Println("Connected to Mongo:", displayURI)

	return &MongoClient{Client: c, Database: c.Database(dbName)}, nil
}
