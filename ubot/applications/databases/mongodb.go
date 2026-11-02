package databases

import (
	"context"
	"log"
	"net/url"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoClient struct {
	Client   *mongo.Client
	Database *mongo.Database
}

func NewMongoClient(ctx context.Context, uri, dbName string) (*MongoClient, error) {
	if strings.TrimSpace(uri) == "" {
		log.Println("☁️ MongoDB Backup URI is empty. Offsite backup disabled.")
		return nil, nil
	}

	opt := options.Client().ApplyURI(uri)
	opt.SetMaxPoolSize(10)
	opt.SetMinPoolSize(2)
	opt.SetConnectTimeout(5 * time.Second)
	opt.SetServerSelectionTimeout(5 * time.Second)

	c, err := mongo.Connect(ctx, opt)
	if err != nil {
		log.Printf("⚠️ MongoDB Backup connect warning: %v (bypassing offsite backup)", err)
		return nil, nil
	}

	pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)
	defer pingCancel()
	if err := c.Ping(pingCtx, nil); err != nil {
		log.Printf("⚠️ MongoDB Backup ping failed: %v (bypassing offsite backup)", err)
		return nil, nil
	}

	displayURI := uri
	if parsed, parseErr := url.Parse(uri); parseErr == nil && parsed.User != nil {
		parsed.User = url.User(parsed.User.Username())
		displayURI = parsed.String()
	}
	log.Println("☁️ Connected to MongoDB Cloud Backup:", displayURI)

	return &MongoClient{Client: c, Database: c.Database(dbName)}, nil
}
