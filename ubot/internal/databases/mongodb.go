package databases

import (
	"context"
	"log"
	"net/url"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
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

	const baseDelay = 3 * time.Second
	const maxDelay = 30 * time.Second
	const perAttemptTimeout = 10 * time.Second

	opt := options.Client().ApplyURI(uri)
	opt.SetMaxPoolSize(10)
	opt.SetMinPoolSize(2)
	opt.SetConnectTimeout(5 * time.Second)
	opt.SetServerSelectionTimeout(5 * time.Second)

	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return nil, nil // context dibatalkan, hentikan retry secara graceful
		}

		attemptCtx, cancel := context.WithTimeout(ctx, perAttemptTimeout)
		c, err := mongo.Connect(attemptCtx, opt)
		if err != nil {
			cancel()
			delay := time.Duration(attempt) * baseDelay
			if delay > maxDelay {
				delay = maxDelay
			}
			log.Printf("⚠️ MongoDB Backup connect attempt %d failed: %v. Retrying in %v...", attempt, err, delay)
			select {
			case <-ctx.Done():
				return nil, nil
			case <-time.After(delay):
			}
			continue
		}

		pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
		pingErr := c.Ping(pingCtx, nil)
		pingCancel()
		cancel()

		if pingErr != nil {
			_ = c.Disconnect(context.Background())
			delay := time.Duration(attempt) * baseDelay
			if delay > maxDelay {
				delay = maxDelay
			}
			log.Printf("⚠️ MongoDB Backup ping attempt %d failed: %v. Retrying in %v...", attempt, pingErr, delay)
			select {
			case <-ctx.Done():
				return nil, nil
			case <-time.After(delay):
			}
			continue
		}

		displayURI := uri
		if parsed, parseErr := url.Parse(uri); parseErr == nil && parsed.User != nil {
			parsed.User = url.User(parsed.User.Username())
			displayURI = parsed.String()
		}
		log.Printf("☁️ Connected to MongoDB Cloud Backup (attempt %d): %s", attempt, displayURI)

		mc := &MongoClient{Client: c, Database: c.Database(dbName)}
		mc.EnsureIndexes(context.Background())
		return mc, nil
	}
}

func (m *MongoClient) EnsureIndexes(ctx context.Context) {
	if m == nil || m.Database == nil {
		return
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	createIndexSafe := func(col *mongo.Collection, model mongo.IndexModel) {
		_, err := col.Indexes().CreateOne(ctxTimeout, model)
		if err != nil && !strings.Contains(err.Error(), "IndexOptionsConflict") && !strings.Contains(err.Error(), "already exists") {
			log.Printf("⚠️ Gagal membuat index %s: %v", col.Name(), err)
		}
	}

	// 1. userbot_sessions indexes (fast lookup by owner_id & account_id)
	colSessions := m.Database.Collection("userbot_sessions")
	createIndexSafe(colSessions, mongo.IndexModel{
		Keys: bson.D{{Key: "owner_id", Value: 1}, {Key: "account_id", Value: 1}},
	})
	createIndexSafe(colSessions, mongo.IndexModel{
		Keys: bson.D{{Key: "account_id", Value: 1}},
	})

	// 2. userbot_notes indexes (fast note lookup per owner)
	colNotes := m.Database.Collection("userbot_notes")
	createIndexSafe(colNotes, mongo.IndexModel{
		Keys: bson.D{{Key: "owner_id", Value: 1}, {Key: "name", Value: 1}},
	})

	// 3. userbot_sudo indexes
	colSudo := m.Database.Collection("userbot_sudo")
	createIndexSafe(colSudo, mongo.IndexModel{
		Keys: bson.D{{Key: "owner_id", Value: 1}, {Key: "user_id", Value: 1}},
	})

	log.Println("🍃 MongoDB indexes ensured (sessions, notes, sudo).")
}
